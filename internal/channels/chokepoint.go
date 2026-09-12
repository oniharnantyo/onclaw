// Package channels implements the channel message chokepoint and SSE hub
// (OpenSpec change integrate-agent-channels, design D2–D4, D7, D9, D11).
//
// The Chokepoint is the single message pipeline — persist → resolve mentions
// vs the roster → silence policy (tiers) → loop caps → fan-out runs →
// broadcast. It is the only path that mints agent runs from channel traffic
// and the only place the caps bind. Every utterance enters here: human REST
// posts, agent auto-posted finals, and the channel.post tool.
package channels

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Loop-cap constants (design D4/D16). All code constants in v1.
const (
	// maxChainDepth caps the summon chain: hops from the root message. Runs
	// are minted only from messages at or below this depth.
	maxChainDepth = 3
)

// Author identifies who posts through the chokepoint. Type is "user" or
// "agent"; exactly one of UserID / AgentID is set.
type Author struct {
	Type    string
	UserID  string
	AgentID string
}

// runLink is the (channel, agent)-scoped state of the agent's currently
// executing channel run. It carries what agent posts need at post time —
// the deterministic session id, the discovered turn id, the initiating
// user, the chain coordinates the auto-post propagates, and the work
// session the run bills against (used to detect a session that closed
// mid-run: the drain's auto-post still lands, but summons nothing).
type runLink struct {
	sessionID     string
	turnID        string // discovered from stream events; "" until known
	userID        string // chain-initiating user (ExecRequest.Validate requires one)
	rootMessageID string // chain root the run executes under ("" = fresh chain)
	chainDepth    int    // the run's depth = the triggering message's depth
	workSessionID string // work session the run was minted for ("" = none)
}

// The chokepoint satisfies the runner's consumer-side channel ports: the
// composition root passes it to agents.WithChannelContext and
// agents.WithChannelFeed.
var (
	_ agents.ChannelContext = (*Chokepoint)(nil)
	_ agents.ChannelFeed    = (*Chokepoint)(nil)
)

// Chokepoint is the single message pipeline (design D2). It is safe for
// concurrent use.
type Chokepoint struct {
	channels store.ChannelStore
	runner   RunSubmitter
	hub      *Hub
	decider  SilenceDecider
	handles  agents.ChannelHandles
	// sessions backs the work-session branch (channel-teams D1/D3). Always
	// non-nil: the default no-op store reads every channel as session-less
	// (v1 behavior), so an unwired chokepoint is a defaulted option, not a
	// nil check at the point of use.
	sessions store.WorkSessionStore
	// watchdogIdle is the stall period after which the watchdog summons the
	// facilitator once per idle period (channel-teams D11: 10 min default).
	watchdogIdle time.Duration

	linksMu sync.Mutex
	// links maps channelID:agentID to the active run's link. Entries are
	// registered at submit and unregistered at drain end; agent posts between
	// the two (channel.post mid-run, the auto-posted final) read them for
	// session stamping and chain propagation.
	links map[string]*runLink

	// watchdogMu guards the per-work-session stall timers (open sessions
	// only; closed and paused sessions carry no timer).
	watchdogMu     sync.Mutex
	watchdogTimers map[string]*time.Timer
}

// ChokepointOption configures the defaultable behavior knobs of a
// chokepoint.
type ChokepointOption func(*Chokepoint)

// WithDeciderModelFactory wires the silence decider's model source, making
// NewChokepoint construct the eino-backed decider internally (one tiny LLM
// call per untagged message and agent member, the agent's own provider).
// Default: a decider that never engages — untagged messages go unanswered,
// per design D3 — until the composition root wires the real factory.
func WithDeciderModelFactory(f DeciderModelFactory) ChokepointOption {
	return func(c *Chokepoint) {
		if f != nil {
			c.decider = newEinoSilenceDecider(f)
		}
	}
}

// WithSilenceDecider overrides the whole decider (the composition-root
// escape hatch; tests use newChokepointWithDecider instead).
func WithSilenceDecider(decider SilenceDecider) ChokepointOption {
	return func(c *Chokepoint) {
		if decider != nil {
			c.decider = decider
		}
	}
}

// WithChannelHandles wires the roster's @handle resolver — agents by slug,
// humans by the dashed lowercase display name (the agents.ChannelHandles
// rules, shared with the runner's composition). Default: a resolver that
// resolves nothing, so every @token stays plain text.
func WithChannelHandles(handles agents.ChannelHandles) ChokepointOption {
	return func(c *Chokepoint) {
		if handles != nil {
			c.handles = handles
		}
	}
}

// WithWorkSessionStore wires the work-session store backing the session
// branch (channel-teams D1/D3). Default: a no-op store under which every
// channel reads as session-less and the chokepoint behaves exactly as v1 —
// the same default-then-wire pattern as the silence decider.
func WithWorkSessionStore(ws store.WorkSessionStore) ChokepointOption {
	return func(c *Chokepoint) {
		if ws != nil {
			c.sessions = ws
		}
	}
}

// WithWatchdogIdle overrides the stall watchdog's idle period (default 10
// minutes, channel-teams D11); tests inject a short period.
func WithWatchdogIdle(d time.Duration) ChokepointOption {
	return func(c *Chokepoint) {
		if d > 0 {
			c.watchdogIdle = d
		}
	}
}

// NewChokepoint creates the production chokepoint from its three positional
// dependencies: the channel store, the run submitter (*agents.Runner
// satisfies it), and the SSE hub. Defaultable behavior knobs are options.
func NewChokepoint(cs store.ChannelStore, runner RunSubmitter, hub *Hub, opts ...ChokepointOption) *Chokepoint {
	c := &Chokepoint{
		channels:       cs,
		runner:         runner,
		hub:            hub,
		decider:        declineSilenceDecider{},
		handles:        nopChannelHandles{},
		sessions:       nopWorkSessionStore{},
		watchdogIdle:   defaultWatchdogIdle,
		links:          make(map[string]*runLink),
		watchdogTimers: make(map[string]*time.Timer),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// newChokepointWithDecider is the package-private constructor tests use to
// inject a fake decider without going through the options.
func newChokepointWithDecider(cs store.ChannelStore, runner RunSubmitter, hub *Hub, decider SilenceDecider) *Chokepoint {
	c := NewChokepoint(cs, runner, hub)
	c.decider = decider
	return c
}

// nopChannelHandles is the default handle resolver: nothing resolves, so
// mentions stay plain text (the runner's default no-op dispatcher pattern).
type nopChannelHandles struct{}

func (nopChannelHandles) UserHandle(context.Context, string) (string, error) {
	return "", fmt.Errorf("%w: channel handles not wired", domain.ErrNotFound)
}

func (nopChannelHandles) AgentHandle(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("%w: channel handles not wired", domain.ErrNotFound)
}

// Post runs one utterance through the pipeline (design D2) and returns the
// persisted feed message. HTTP handlers, agent auto-posts, and the
// channel.post tool all enter here.
//
// The persist/resolve/broadcast phases are synchronous; the silence policy
// and fan-out phase is synchronous too — bounded by the decider timeout —
// while the minted runs themselves drain on their own goroutines.
//
// Mention resolution note: mentions resolve against the roster BEFORE the
// insert, so the persisted row and the message_posted broadcast carry the
// same resolved list (clients dedup the stream against the REST load) — the
// observable result of the design's persist-then-update sequence in one
// atomic write.
func (c *Chokepoint) Post(ctx context.Context, workspaceID, channelID string, author Author, body string) (domain.ChannelMessage, error) {
	authorType, err := normalizeAuthor(author)
	if err != nil {
		return domain.ChannelMessage{}, err
	}
	if strings.TrimSpace(body) == "" {
		return domain.ChannelMessage{}, fmt.Errorf("%w: body cannot be empty", domain.ErrInvalid)
	}

	channel, err := c.channels.ChannelByID(ctx, workspaceID, channelID)
	if err != nil {
		return domain.ChannelMessage{}, fmt.Errorf("load channel: %w", err)
	}
	members, err := c.channels.ListChannelMembers(ctx, workspaceID, channelID)
	if err != nil {
		return domain.ChannelMessage{}, fmt.Errorf("load channel members: %w", err)
	}

	handlesByRef := c.resolveRosterHandles(ctx, members)
	index := handleIndex(handlesByRef, members)

	// Chain bookkeeping (D4): an agent posting through an active run replies
	// within that run's chain — the auto-posted final and channel.post both
	// propagate the chain root and take depth+1. Everyone else starts a
	// fresh chain at depth 0.
	link, hasLink := c.lookupLink(channelID, authorRefID(authorType, author))
	msg := domain.ChannelMessage{
		WorkspaceID:   workspaceID,
		ChannelID:     channelID,
		AuthorType:    authorType,
		AuthorUserID:  author.UserID,
		AuthorAgentID: author.AgentID,
		Body:          body,
		Mentions:      []domain.Mention{},
	}
	var chainRoot string
	if hasLink {
		chainRoot = link.rootMessageID
		msg.ChainDepth = link.chainDepth + 1
		sessionID, turnID := link.sessionID, link.turnID
		msg.SessionID = &sessionID
		if turnID != "" {
			msg.TurnID = &turnID
		}
	}
	if chainRoot != "" {
		msg.RootMessageID = &chainRoot
	}

	// Work-session branch (channel-teams D3): a single branch at the
	// chokepoint. In-session traffic stamps the session on the message and
	// swaps the bounds — the v1 chain-depth cap and no-re-summon rule are
	// suspended, mentions summon deterministically against the hop budget.
	// Session-less traffic follows v1 unchanged. A drain whose work session
	// closed mid-run still posts its final text, but summons nothing and its
	// mentions render plain.
	session := c.activeSession(ctx, workspaceID, channelID)
	var drainedClosed *domain.WorkSession
	if session == nil && hasLink && link.workSessionID != "" {
		if closed, err := c.sessions.GetWorkSession(ctx, workspaceID, link.workSessionID); err == nil &&
			closed.Status == domain.WorkSessionClosed {
			drainedClosed = closed
		}
	}

	// A human post resumes a paused session (D4) BEFORE the message is
	// handled: the resumed session then applies to this very post — its
	// mentions summon, its untagged text goes to the deciders.
	if session != nil && authorType == domain.ChannelMemberTypeUser && session.Status == domain.WorkSessionPaused {
		if resumed, err := c.sessions.ResumeWorkSession(ctx, workspaceID, session.ID); err == nil {
			session = resumed
			c.hub.Broadcast(channelID, newSessionUpdatedEvent(*session))
		} else {
			slog.WarnContext(ctx, "channels: work session resume failed",
				"channel_id", channelID, "work_session_id", session.ID, "error", err)
		}
	}

	inSession := session != nil
	drainingClosed := drainedClosed != nil
	if inSession {
		wsid := session.ID
		msg.WorkSessionID = &wsid
	} else if drainingClosed {
		wsid := drainedClosed.ID
		msg.WorkSessionID = &wsid
	}

	// No-re-summon-within-chain: which agents did this chain already summon?
	// v1 only — in-session traffic has the rule suspended (D3), and a
	// closed-session drain post summons nothing anyway.
	var prior map[string]struct{}
	mentions := resolveMentions(parseMentionTokens(body), index)
	if !inSession && !drainingClosed {
		prior = c.chainSummonedAgents(ctx, workspaceID, channelID, chainRoot)
		mentions = suppressMentions(ctx, channelID, chainRoot, mentions, prior)
	}

	// Human gate (D4) and paused-session suppression shape the persisted
	// mentions: an agent-authored post in an OPEN session that tags a human
	// pauses the session and suppresses every summon from the message — its
	// agent mentions drop to plain text while the human mention stays
	// resolved (the in-app awaiting badge keys off it). While paused, an
	// agent post summons nothing and its agent mentions render plain. A
	// closed-session drain post renders every mention plain.
	var (
		sessionSuppressed bool
		humanGate         bool
	)
	if inSession && authorType == domain.ChannelMemberTypeAgent {
		switch session.Status {
		case domain.WorkSessionPaused:
			mentions = dropAgentMentions(mentions)
			sessionSuppressed = true
		default: // open
			if hasHumanMention(mentions) {
				humanGate = true
				mentions = dropAgentMentions(mentions)
				sessionSuppressed = true
			}
		}
	}
	if drainingClosed {
		mentions = nil
		sessionSuppressed = true
	}
	msg.Mentions = mentions

	authorHandle := authorHandleOf(handlesByRef, authorType, author)

	if err := c.channels.InsertChannelMessage(ctx, &msg); err != nil {
		return domain.ChannelMessage{}, fmt.Errorf("persist channel message: %w", err)
	}
	c.hub.Broadcast(channelID, newMessagePostedEvent(msg))

	// The gate pause lands after the post stands: the message is the fact,
	// the pause is its consequence.
	if humanGate {
		if paused, err := c.sessions.PauseWorkSession(ctx, workspaceID, session.ID, domain.WorkSessionPauseAwaitingHuman); err == nil {
			session = paused
			c.hub.Broadcast(channelID, newSessionUpdatedEvent(*session))
			c.cancelWatchdog(session.ID)
		} else {
			slog.WarnContext(ctx, "channels: work session human-gate pause failed",
				"channel_id", channelID, "work_session_id", session.ID, "error", err)
		}
	}

	sc := summonContext{
		channel:      channel,
		members:      members,
		handlesByRef: handlesByRef,
		msg:          msg,
		mentions:     mentions,
		prior:        prior,
		depth:        msg.ChainDepth,
		authorType:   authorType,
		author:       author,
		authorHandle: authorHandle,
		link:         link,
		hasLink:      hasLink,
		session:      session,
	}
	switch {
	case sessionSuppressed:
		// No summons at all from this message (paused, human gate, or a
		// closed-session drain landing late) — logged at the suppression
		// sites; the post itself always stands.
	case inSession:
		c.runSessionSummons(ctx, sc)
	default:
		c.runSummons(ctx, sc)
	}

	// Session activity re-arms the stall watchdog (open sessions only).
	if inSession && session != nil && session.Status == domain.WorkSessionOpen {
		c.rearmWatchdog(channel, session, msg.CreatedAt)
	}
	return msg, nil
}

// PostFromAgent is the agent speaking path (design D9): the channel.post
// tool — and any future agent speaking path — posts through the same
// pipeline, as the agent. Satisfies agents.ChannelFeed.
func (c *Chokepoint) PostFromAgent(ctx context.Context, workspaceID, channelID, agentID, body string) (domain.ChannelMessage, error) {
	return c.Post(ctx, workspaceID, channelID, Author{
		Type:    string(domain.ChannelMemberTypeAgent),
		AgentID: agentID,
	}, body)
}

// ----- agents.ChannelContext: roster/tail reads for composeAgent -----

// GetChannel implements agents.ChannelContext.
func (c *Chokepoint) GetChannel(ctx context.Context, workspaceID, channelID string) (domain.Channel, error) {
	channel, err := c.channels.ChannelByID(ctx, workspaceID, channelID)
	if err != nil {
		return domain.Channel{}, err
	}
	return *channel, nil
}

// ListChannelMembers implements agents.ChannelContext.
func (c *Chokepoint) ListChannelMembers(ctx context.Context, workspaceID, channelID string) ([]domain.ChannelMember, error) {
	return c.channels.ListChannelMembers(ctx, workspaceID, channelID)
}

// ChannelTail implements agents.ChannelContext: the last limit feed
// messages, oldest→newest. It MAY include the message that triggered the
// run — the composer drops a trailing attributed-equal line.
func (c *Chokepoint) ChannelTail(ctx context.Context, workspaceID, channelID string, limit int) ([]domain.ChannelMessage, error) {
	rows, err := c.listFeed(ctx, workspaceID, channelID)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	return rows, nil
}

// ChannelMessagesAfter implements agents.ChannelContext: the backward
// (before-)cursor read backing the channel.history tool — up to limit
// messages strictly OLDER than beforeSeq, oldest→newest; beforeSeq 0
// selects from the newest end of the feed.
func (c *Chokepoint) ChannelMessagesAfter(ctx context.Context, workspaceID, channelID string, beforeSeq int64, limit int) ([]domain.ChannelMessage, error) {
	rows, err := c.listFeed(ctx, workspaceID, channelID)
	if err != nil {
		return nil, err
	}
	if beforeSeq > 0 {
		older := rows[:0]
		for _, msg := range rows {
			if msg.Seq < beforeSeq {
				older = append(older, msg)
			}
		}
		rows = older
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	return rows, nil
}

// listFeed reads the whole channel feed, oldest→newest. v1 trade-off: the
// store port exposes only the forward cursor, so tail/chain/window reads
// scan the feed; feeds are small in v1 and the reads are per-post bounded.
func (c *Chokepoint) listFeed(ctx context.Context, workspaceID, channelID string) ([]domain.ChannelMessage, error) {
	rows, err := c.channels.ListChannelMessages(ctx, store.ListChannelMessagesParams{
		WorkspaceID: workspaceID,
		ChannelID:   channelID,
	})
	if err != nil {
		return nil, fmt.Errorf("list channel messages: %w", err)
	}
	return rows, nil
}

// ----- pipeline internals -----

// normalizeAuthor validates the Author into a member type.
func normalizeAuthor(author Author) (domain.ChannelMemberType, error) {
	switch author.Type {
	case string(domain.ChannelMemberTypeUser):
		if author.UserID == "" {
			return "", fmt.Errorf("%w: user author requires user_id", domain.ErrInvalid)
		}
		return domain.ChannelMemberTypeUser, nil
	case string(domain.ChannelMemberTypeAgent):
		if author.AgentID == "" {
			return "", fmt.Errorf("%w: agent author requires agent_id", domain.ErrInvalid)
		}
		return domain.ChannelMemberTypeAgent, nil
	default:
		return "", fmt.Errorf("%w: author type %q must be %q or %q", domain.ErrInvalid,
			author.Type, domain.ChannelMemberTypeUser, domain.ChannelMemberTypeAgent)
	}
}

func authorRefID(authorType domain.ChannelMemberType, author Author) string {
	if authorType == domain.ChannelMemberTypeAgent {
		return author.AgentID
	}
	return author.UserID
}

// resolveRosterHandles maps each roster member's reference id to its @handle
// through the wired directory. Members that fail to resolve are omitted —
// their @tokens stay plain text.
func (c *Chokepoint) resolveRosterHandles(ctx context.Context, members []domain.ChannelMember) map[string]string {
	resolved := make(map[string]string, len(members))
	for _, m := range members {
		var (
			handle string
			err    error
		)
		switch m.MemberType {
		case domain.ChannelMemberTypeUser:
			handle, err = c.handles.UserHandle(ctx, m.UserID)
		case domain.ChannelMemberTypeAgent:
			handle, err = c.handles.AgentHandle(ctx, m.WorkspaceID, m.AgentID)
		}
		if err != nil || handle == "" {
			continue
		}
		resolved[m.RefID()] = handle
	}
	return resolved
}

// authorHandleOf picks the feed author's @handle, falling back to the raw
// reference id when the directory cannot resolve it (attribution must not
// be empty — the run input line needs a name).
func authorHandleOf(handlesByRef map[string]string, authorType domain.ChannelMemberType, author Author) string {
	if handle, ok := handlesByRef[authorRefID(authorType, author)]; ok {
		return handle
	}
	return authorRefID(authorType, author)
}

// chainSummonedAgents collects the agents the chain rooted at rootID already
// summoned: agent authors and resolved agent mentions across the root row
// and its replies (design D4: store lookup on root_message_id). A fresh
// chain (rootID "") has no prior. A read failure degrades to no prior —
// posting must not fail on a suppression lookup; the duplicate summons the
// degraded path allows are bounded by the one-run-per-session guard.
func (c *Chokepoint) chainSummonedAgents(ctx context.Context, workspaceID, channelID, rootID string) map[string]struct{} {
	prior := make(map[string]struct{})
	if rootID == "" {
		return prior
	}
	rows, err := c.listFeed(ctx, workspaceID, channelID)
	if err != nil {
		slog.WarnContext(ctx, "channels: chain lookup failed; suppression degraded",
			"channel_id", channelID, "root_message_id", rootID, "error", err)
		return prior
	}
	for _, msg := range rows {
		inChain := msg.ID == rootID || (msg.RootMessageID != nil && *msg.RootMessageID == rootID)
		if !inChain {
			continue
		}
		if msg.AuthorType == domain.ChannelMemberTypeAgent {
			prior[msg.AuthorAgentID] = struct{}{}
		}
		for _, mention := range msg.Mentions {
			if mention.Type == domain.ChannelMemberTypeAgent {
				prior[mention.ID] = struct{}{}
			}
		}
	}
	return prior
}

// suppressMentions drops agent mentions already summoned in the chain —
// they are not re-triggered; the suppressed mention renders as plain text
// (it leaves the resolved list, so the persisted body keeps the raw token),
// and every suppression is logged (design D4). Human mentions and fresh
// agent mentions pass through.
func suppressMentions(ctx context.Context, channelID, rootID string, mentions []domain.Mention, prior map[string]struct{}) []domain.Mention {
	if len(prior) == 0 {
		return mentions
	}
	kept := make([]domain.Mention, 0, len(mentions))
	for _, mention := range mentions {
		if mention.Type == domain.ChannelMemberTypeAgent {
			if _, summoned := prior[mention.ID]; summoned {
				slog.InfoContext(ctx, "channels: summon suppressed; agent already summoned in chain",
					"channel_id", channelID, "root_message_id", rootID,
					"agent_id", mention.ID, "handle", mention.Handle)
				continue
			}
		}
		kept = append(kept, mention)
	}
	return kept
}

// summonContext bundles what the silence policy phase needs per post.
type summonContext struct {
	channel      *domain.Channel
	members      []domain.ChannelMember
	handlesByRef map[string]string
	msg          domain.ChannelMessage
	mentions     []domain.Mention
	prior        map[string]struct{}
	depth        int
	authorType   domain.ChannelMemberType
	author       Author
	authorHandle string
	link         runLink
	hasLink      bool
	// session is the channel's active work session the message posted under
	// (channel-teams D3); nil outside sessions. When set, every minted run
	// bills one hop before submission — the budget-exhaustion pause path
	// replaces further summons.
	session *domain.WorkSession
}

// runSummons applies the silence policy tiers (design D3) under the loop
// caps (D4) and fans runs out. Mentioned agents summon deterministically —
// no decider; untagged messages put every agent member's decider on the
// message in parallel, capped at two responders (first two electing win).
func (c *Chokepoint) runSummons(ctx context.Context, sc summonContext) {
	// Caps bind regardless of tier: past the chain depth cap nothing summons
	// — the message still posts, its mentions stay resolved (D4).
	if sc.depth > maxChainDepth {
		slog.InfoContext(ctx, "channels: chain depth cap reached; no summons",
			"channel_id", sc.channel.ID, "root_message_id", chainRootOf(sc.msg), "chain_depth", sc.depth)
		return
	}

	mentioned := agentIDsOf(sc.mentions)
	if len(mentioned) > 0 {
		// Tier 1: deterministic summons, in mention order. The author never
		// summons itself.
		for _, agentID := range mentioned {
			if agentID == authorRefID(sc.authorType, sc.author) {
				continue
			}
			c.hub.Broadcast(sc.channel.ID, newSummonDecidedEvent(sc.channel.ID, agentID, true, "summoned by @mention"))
			c.submitRun(ctx, submitRequest{
				channel:      sc.channel,
				agentID:      agentID,
				trigger:      sc.msg,
				authorHandle: sc.authorHandle,
				userID:       sc.runUserID(),
			})
		}
		return
	}

	// Tier 2: observe-decide. Every agent member decides on the untagged
	// message in parallel — minus the author and anyone this chain already
	// summoned (D4 binds regardless of tier).
	candidates := make([]domain.ChannelMember, 0, len(sc.members))
	authorID := authorRefID(sc.authorType, sc.author)
	for _, m := range sc.members {
		if m.MemberType != domain.ChannelMemberTypeAgent {
			continue
		}
		if m.AgentID == authorID {
			continue
		}
		if _, summoned := sc.prior[m.AgentID]; summoned {
			continue
		}
		candidates = append(candidates, m)
	}
	if len(candidates) == 0 {
		return
	}
	c.observeDecide(ctx, sc, candidates)
}

// runUserID resolves the ExecRequest.UserID a minted run executes under:
// the acting human for human triggers; the chain's initiating user for
// agent re-triggers (the runner requires a real workspace member on every
// run — ExecRequest.Validate + load). An agent post with no active link has
// no initiating user; its run is rejected at validation and surfaced as a
// failed run event.
func (sc summonContext) runUserID() string {
	if sc.authorType == domain.ChannelMemberTypeUser {
		return sc.author.UserID
	}
	if sc.hasLink {
		return sc.link.userID
	}
	return ""
}

// chainRootOf returns a message's chain root: its own id when it is the
// root, its root reference otherwise.
func chainRootOf(msg domain.ChannelMessage) string {
	if msg.RootMessageID != nil {
		return *msg.RootMessageID
	}
	return msg.ID
}

// observeDecide runs the parallel decider race with the responder cap.
func (c *Chokepoint) observeDecide(ctx context.Context, sc summonContext, candidates []domain.ChannelMember) {
	tail, err := c.deciderTail(ctx, sc)
	if err != nil {
		// A tail read failure silences the deciders for this message (soft
		// gate): the post stands, nobody is summoned, the failure is logged.
		slog.WarnContext(ctx, "channels: decider tail read failed; no summons",
			"channel_id", sc.channel.ID, "error", err)
		return
	}

	type outcome struct {
		agentID  string
		decision Decision
		err      error
	}
	results := make(chan outcome, len(candidates))
	for _, member := range candidates {
		// The considering indicator goes out before the decider resolves;
		// summon_decided (engage or decline) closes it later.
		c.hub.Broadcast(sc.channel.ID, newSummonConsideringEvent(sc.channel.ID, member.AgentID))
		go func(member domain.ChannelMember) {
			decideCtx, cancel := context.WithTimeout(ctx, deciderTimeout)
			defer cancel()
			decision, err := c.decider.Decide(decideCtx, DecideInput{
				WorkspaceID:   sc.channel.WorkspaceID,
				ChannelID:     sc.channel.ID,
				AgentID:       member.AgentID,
				AgentHandle:   sc.handlesByRef[member.RefID()],
				Channel:       *sc.channel,
				Members:       sc.members,
				MemberHandles: sc.handlesByRef,
				Tail:          tail,
				Message:       sc.msg,
			})
			results <- outcome{agentID: member.AgentID, decision: decision, err: err}
		}(member)
	}

	engaged := 0
	for range candidates {
		out := <-results
		if out.err != nil {
			// Silent-on-failure (D3): no summons, log only — no SSE frame.
			logDeciderFailure(ctx, sc.channel.ID, out.agentID, out.err)
			continue
		}
		if !out.decision.Engage {
			logDecline(ctx, sc.channel.ID, out.agentID, out.decision.Reason)
			c.hub.Broadcast(sc.channel.ID, newSummonDecidedEvent(sc.channel.ID, out.agentID, false, out.decision.Reason))
			continue
		}
		if engaged >= untaggedResponderCap {
			slog.InfoContext(ctx, "channels: untagged responder cap reached; engagement suppressed",
				"channel_id", sc.channel.ID, "agent_id", out.agentID)
			c.hub.Broadcast(sc.channel.ID, newSummonDecidedEvent(sc.channel.ID, out.agentID, false, "responder cap reached"))
			continue
		}
		// In-session engagement bills one hop BEFORE the run is minted;
		// an exhausted budget pauses the session and suppresses the
		// remaining elections (channel-teams D1/D3).
		if sc.session != nil {
			updated, ok := c.billSessionHop(ctx, sc.channel, sc.session)
			if !ok {
				break
			}
			sc.session = updated
		}
		engaged++
		c.hub.Broadcast(sc.channel.ID, newSummonDecidedEvent(sc.channel.ID, out.agentID, true, out.decision.Reason))
		c.submitRun(ctx, submitRequest{
			channel:       sc.channel,
			agentID:       out.agentID,
			trigger:       sc.msg,
			authorHandle:  sc.authorHandle,
			userID:        sc.runUserID(),
			workSessionID: sessionIDOf(sc.session),
		})
	}
}

// deciderTail renders the decider's catch-up tail: the last
// deciderTailMessages messages excluding the trigger itself, oldest→newest.
func (c *Chokepoint) deciderTail(ctx context.Context, sc summonContext) ([]domain.ChannelMessage, error) {
	rows, err := c.listFeed(ctx, sc.channel.WorkspaceID, sc.channel.ID)
	if err != nil {
		return nil, err
	}
	filtered := rows[:0]
	for _, msg := range rows {
		if msg.ID != sc.msg.ID {
			filtered = append(filtered, msg)
		}
	}
	if len(filtered) > deciderTailMessages {
		filtered = filtered[len(filtered)-deciderTailMessages:]
	}
	return filtered, nil
}

// ----- active run link registry -----

func linkKey(channelID, agentID string) string {
	return channelID + ":" + agentID
}

func (c *Chokepoint) registerLink(channelID, agentID string, link *runLink) {
	c.linksMu.Lock()
	defer c.linksMu.Unlock()
	c.links[linkKey(channelID, agentID)] = link
}

// unregisterLink removes the link only if it is still the registered one —
// a newer run on the same deterministic session must not be deleted by an
// older drain finishing late.
func (c *Chokepoint) unregisterLink(channelID, agentID string, link *runLink) {
	c.linksMu.Lock()
	defer c.linksMu.Unlock()
	if c.links[linkKey(channelID, agentID)] == link {
		delete(c.links, linkKey(channelID, agentID))
	}
}

func (c *Chokepoint) lookupLink(channelID, agentID string) (runLink, bool) {
	c.linksMu.Lock()
	defer c.linksMu.Unlock()
	link, ok := c.links[linkKey(channelID, agentID)]
	if !ok {
		return runLink{}, false
	}
	return *link, true
}

// setLinkTurn records the run's turn id the first time the stream reveals
// one, so posts racing the discovery still stamp it when they come later.
func (c *Chokepoint) setLinkTurn(link *runLink, turnID string) {
	c.linksMu.Lock()
	defer c.linksMu.Unlock()
	if link.turnID == "" {
		link.turnID = turnID
	}
}
