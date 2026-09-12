package channels

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Work-session constants (channel-teams D11). All code constants; the idle
// period is injectable for tests via WithWatchdogIdle.
const (
	// defaultWatchdogIdle is the stall period after which an open session
	// summons its facilitator — once per idle period.
	defaultWatchdogIdle = 10 * time.Minute
	// kickoffAuthorTypeNote marks the synthetic facilitator summons.
)

// nopWorkSessionStore is the default WorkSessionStore: every channel reads
// as session-less (v1 behavior) and session creation is refused — the
// WithWorkSessionStore "absent = v1 behavior" default, implemented as a
// no-op store rather than nil checks at the point of use.
type nopWorkSessionStore struct{}

func (nopWorkSessionStore) CreateWorkSession(context.Context, string, *domain.WorkSession) error {
	return fmt.Errorf("%w: work session store not wired", domain.ErrInvalid)
}

func (nopWorkSessionStore) GetWorkSession(context.Context, string, string) (*domain.WorkSession, error) {
	return nil, domain.ErrNotFound
}

func (nopWorkSessionStore) ActiveWorkSession(context.Context, string, string) (*domain.WorkSession, error) {
	return nil, nil
}

func (nopWorkSessionStore) ListWorkSessions(context.Context, string, string) ([]domain.WorkSession, error) {
	return []domain.WorkSession{}, nil
}

func (nopWorkSessionStore) PauseWorkSession(context.Context, string, string, domain.WorkSessionPauseReason) (*domain.WorkSession, error) {
	return nil, domain.ErrNotFound
}

func (nopWorkSessionStore) ResumeWorkSession(context.Context, string, string) (*domain.WorkSession, error) {
	return nil, domain.ErrNotFound
}

func (nopWorkSessionStore) ConsumeWorkSessionHop(context.Context, string, string) (*domain.WorkSession, error) {
	return nil, domain.ErrNotFound
}

func (nopWorkSessionStore) CloseWorkSession(context.Context, string, string, string) (*domain.WorkSession, error) {
	return nil, domain.ErrNotFound
}

func (nopWorkSessionStore) ListOpenWorkSessions(context.Context) ([]domain.WorkSession, error) {
	return []domain.WorkSession{}, nil
}

// sessionIDOf maps an optional session to its id ("" when nil).
func sessionIDOf(session *domain.WorkSession) string {
	if session == nil {
		return ""
	}
	return session.ID
}

// activeSession resolves the channel's non-closed work session, or nil when
// the channel is session-less. A store read failure degrades to session-less
// (logged): posting must not fail on a session lookup, and the degraded v1
// path's extra summons are bounded by the store recovering.
func (c *Chokepoint) activeSession(ctx context.Context, workspaceID, channelID string) *domain.WorkSession {
	session, err := c.sessions.ActiveWorkSession(ctx, workspaceID, channelID)
	if err != nil {
		slog.WarnContext(ctx, "channels: work session lookup failed; treating channel as session-less",
			"channel_id", channelID, "error", err)
		return nil
	}
	return session
}

// ActiveWorkSession returns the channel's active (non-closed) work session,
// or (nil, nil) when the channel is session-less. It satisfies the extended
// agents.ChannelContext — session awareness rides the channel document
// (channel-teams D8) through the same port as the roster and tail reads.
func (c *Chokepoint) ActiveWorkSession(ctx context.Context, workspaceID, channelID string) (*domain.WorkSession, error) {
	return c.sessions.ActiveWorkSession(ctx, workspaceID, channelID)
}

// facilitatorOf returns the channel's facilitator roster row, if any
// (channel-teams D2: at most one).
func facilitatorOf(members []domain.ChannelMember) *domain.ChannelMember {
	for i := range members {
		if members[i].Role == domain.ChannelMemberRoleFacilitator {
			return &members[i]
		}
	}
	return nil
}

// hasHumanMention reports whether any resolved mention targets a human
// member — the human-gate trigger (channel-teams D4).
func hasHumanMention(mentions []domain.Mention) bool {
	for _, m := range mentions {
		if m.Type == domain.ChannelMemberTypeUser {
			return true
		}
	}
	return false
}

// dropAgentMentions removes agent mentions from the resolved list — they
// render as plain text (the v1 suppression semantics: the suppressed mention
// leaves the list, the persisted body keeps the raw token).
func dropAgentMentions(mentions []domain.Mention) []domain.Mention {
	kept := mentions[:0]
	for _, m := range mentions {
		if m.Type == domain.ChannelMemberTypeAgent {
			continue
		}
		kept = append(kept, m)
	}
	return kept
}

// PostKickoff opens a work session (channel-teams D1): the human-only,
// explicit kickoff — a flag on the posted message, never parsed from text.
// The kickoff message persists with is_kickoff=true and the session's id, a
// session rooted at it is minted with the default hop budget, and the
// facilitator is summoned to plan, consuming hop 1. The channel must have a
// facilitator and no non-closed session.
//
// Mention resolution is informational here: the kickoff's summons IS the
// facilitator (D2 — the facilitator plans and tags the first specialist), so
// resolved mentions ride the message for the feed but mint no runs. A human
// facilitator is allowed (their powers reduce to closing): the session opens
// but no kickoff run is minted and hop 1 stays unspent.
func (c *Chokepoint) PostKickoff(ctx context.Context, workspaceID, channelID string, author Author, body string) (domain.ChannelMessage, *domain.WorkSession, error) {
	authorType, err := normalizeAuthor(author)
	if err != nil {
		return domain.ChannelMessage{}, nil, err
	}
	if authorType != domain.ChannelMemberTypeUser {
		return domain.ChannelMessage{}, nil, fmt.Errorf("%w: kickoff requires a human author", domain.ErrInvalid)
	}
	if strings.TrimSpace(body) == "" {
		return domain.ChannelMessage{}, nil, fmt.Errorf("%w: body cannot be empty", domain.ErrInvalid)
	}

	channel, err := c.channels.ChannelByID(ctx, workspaceID, channelID)
	if err != nil {
		return domain.ChannelMessage{}, nil, fmt.Errorf("load channel: %w", err)
	}
	members, err := c.channels.ListChannelMembers(ctx, workspaceID, channelID)
	if err != nil {
		return domain.ChannelMessage{}, nil, fmt.Errorf("load channel members: %w", err)
	}
	facilitator := facilitatorOf(members)
	if facilitator == nil {
		return domain.ChannelMessage{}, nil, fmt.Errorf("%w: channel has no facilitator", domain.ErrInvalid)
	}
	if session := c.activeSession(ctx, workspaceID, channelID); session != nil {
		return domain.ChannelMessage{}, nil, fmt.Errorf("%w: session %q is %q", domain.ErrWorkSessionActive, session.ID, session.Status)
	}

	handlesByRef := c.resolveRosterHandles(ctx, members)
	mentions := resolveMentions(parseMentionTokens(body), handleIndex(handlesByRef, members))

	msg := domain.ChannelMessage{
		WorkspaceID:  workspaceID,
		ChannelID:    channelID,
		AuthorType:   authorType,
		AuthorUserID: author.UserID,
		Body:         body,
		Mentions:     mentions,
		IsKickoff:    true,
	}
	if err := c.channels.InsertChannelMessage(ctx, &msg); err != nil {
		return domain.ChannelMessage{}, nil, fmt.Errorf("persist kickoff message: %w", err)
	}

	session := &domain.WorkSession{
		WorkspaceID:   workspaceID,
		ChannelID:     channelID,
		RootMessageID: msg.ID,
		Goal:          body,
		Status:        domain.WorkSessionOpen,
		Budget:        domain.DefaultWorkSessionBudget,
	}
	if err := c.sessions.CreateWorkSession(ctx, workspaceID, session); err != nil {
		return msg, nil, fmt.Errorf("create work session: %w", err)
	}

	// The kickoff message is stamped after the session row exists — the
	// schema's FKs point both ways (session.root_message_id at the message,
	// message.work_session_id at the session).
	if err := c.channels.SetChannelMessageWorkSession(ctx, workspaceID, channelID, msg.ID, &session.ID); err != nil {
		slog.WarnContext(ctx, "channels: kickoff message session stamp failed",
			"channel_id", channelID, "work_session_id", session.ID, "error", err)
	}
	msg.WorkSessionID = &session.ID

	c.hub.Broadcast(channelID, newMessagePostedEvent(msg))
	c.hub.Broadcast(channelID, newSessionUpdatedEvent(*session))

	// Summon the facilitator to plan (agent facilitators only), consuming
	// hop 1 — the kickoff summon is the session's first billable hop (D8:
	// hop accounting is exact).
	if facilitator.MemberType == domain.ChannelMemberTypeAgent {
		if updated, err := c.sessions.ConsumeWorkSessionHop(ctx, workspaceID, session.ID); err == nil {
			*session = *updated
			authorHandle := authorHandleOf(handlesByRef, authorType, author)
			c.submitRun(ctx, submitRequest{
				channel:       channel,
				agentID:       facilitator.AgentID,
				trigger:       msg,
				authorHandle:  authorHandle,
				userID:        author.UserID,
				workSessionID: session.ID,
			})
		} else {
			slog.WarnContext(ctx, "channels: kickoff hop consumption failed; facilitator not summoned",
				"channel_id", channelID, "work_session_id", session.ID, "error", err)
		}
	} else {
		slog.InfoContext(ctx, "channels: human facilitator; kickoff mints no run",
			"channel_id", channelID, "work_session_id", session.ID)
	}
	c.rearmWatchdog(channel, session, msg.CreatedAt)

	return msg, session, nil
}

// CloseWorkSession terminates the channel's active work session
// (channel-teams D1/D2): facilitator-only, with a stored summary. The close
// posts a closing feed message as the facilitator agent tagged with the
// closed session id — deliberately outside the summon pipeline, so it fans
// nothing out. After close the channel is session-less and subsequent
// traffic follows v1 rules.
func (c *Chokepoint) CloseWorkSession(ctx context.Context, workspaceID, channelID, agentID, summary string) (*domain.WorkSession, error) {
	if agentID == "" {
		return nil, fmt.Errorf("%w: closing agent id is required", domain.ErrInvalid)
	}

	// The channel load doubles as the tenant check: a channel outside the
	// workspace is indistinguishable from an unknown id.
	if _, err := c.channels.ChannelByID(ctx, workspaceID, channelID); err != nil {
		return nil, fmt.Errorf("load channel: %w", err)
	}
	members, err := c.channels.ListChannelMembers(ctx, workspaceID, channelID)
	if err != nil {
		return nil, fmt.Errorf("load channel members: %w", err)
	}
	facilitator := facilitatorOf(members)
	if facilitator == nil || facilitator.MemberType != domain.ChannelMemberTypeAgent || facilitator.AgentID != agentID {
		return nil, domain.ErrNotFacilitator
	}

	session := c.activeSession(ctx, workspaceID, channelID)
	if session == nil {
		return nil, fmt.Errorf("%w: channel has no active work session", domain.ErrNotFound)
	}
	closed, err := c.sessions.CloseWorkSession(ctx, workspaceID, session.ID, summary)
	if err != nil {
		return nil, fmt.Errorf("close work session: %w", err)
	}
	c.cancelWatchdog(closed.ID)

	// The closing message: summary as the body (a blank summary falls back
	// to a plain line so the feed always carries the close), no resolved
	// mentions — minting nothing by construction.
	body := summary
	if strings.TrimSpace(body) == "" {
		body = "Work session closed."
	}
	msg := domain.ChannelMessage{
		WorkspaceID:   workspaceID,
		ChannelID:     channelID,
		AuthorType:    domain.ChannelMemberTypeAgent,
		AuthorAgentID: agentID,
		Body:          body,
		Mentions:      []domain.Mention{},
	}
	wsid := closed.ID
	msg.WorkSessionID = &wsid
	if err := c.channels.InsertChannelMessage(ctx, &msg); err != nil {
		slog.WarnContext(ctx, "channels: closing message persist failed",
			"channel_id", channelID, "work_session_id", closed.ID, "error", err)
	} else {
		c.hub.Broadcast(channelID, newMessagePostedEvent(msg))
	}
	c.hub.Broadcast(channelID, newSessionUpdatedEvent(*closed))

	return closed, nil
}

// -------------------------------------------------------------------------
// In-session summon policy (channel-teams D3)
// -------------------------------------------------------------------------

// runSessionSummons is the in-session summon phase: the v1 chain-depth cap
// and no-re-summon rule are SUSPENDED, so mentioned agents summon
// deterministically (each minted run bills one hop) and untagged messages
// still go through the v1 silence deciders (the responder cap remains; each
// electing agent bills a hop).
func (c *Chokepoint) runSessionSummons(ctx context.Context, sc summonContext) {
	mentioned := agentIDsOf(sc.mentions)
	if len(mentioned) > 0 {
		for _, agentID := range mentioned {
			// The author never summons itself.
			if agentID == authorRefID(sc.authorType, sc.author) {
				continue
			}
			updated, ok := c.billSessionHop(ctx, sc.channel, sc.session)
			if !ok {
				return // budget paused; no further summons from this message
			}
			sc.session = updated
			c.hub.Broadcast(sc.channel.ID, newSummonDecidedEvent(sc.channel.ID, agentID, true, "summoned by @mention"))
			c.submitRun(ctx, submitRequest{
				channel:       sc.channel,
				agentID:       agentID,
				trigger:       sc.msg,
				authorHandle:  sc.authorHandle,
				userID:        sc.runUserID(),
				workSessionID: sessionIDOf(sc.session),
			})
		}
		return
	}

	// Tier 2 untagged: the v1 decider flow, with hop billing on engagement.
	candidates := make([]domain.ChannelMember, 0, len(sc.members))
	authorID := authorRefID(sc.authorType, sc.author)
	for _, m := range sc.members {
		if m.MemberType != domain.ChannelMemberTypeAgent || m.AgentID == authorID {
			continue
		}
		candidates = append(candidates, m)
	}
	if len(candidates) == 0 {
		return
	}
	c.observeDecide(ctx, sc, candidates)
}

// billSessionHop consumes one hop for an in-session run and returns the
// updated session. When the hop cannot be consumed (budget spent, or a raced
// transition), the budget-exhaustion pause path runs — pause, broadcast, ONE
// free facilitator status summons — and ok=false tells the caller to mint no
// further runs. Hop accounting stays exact: the store's guarded UPDATE is
// the single billing point (channel-teams D1/D8).
func (c *Chokepoint) billSessionHop(ctx context.Context, channel *domain.Channel, session *domain.WorkSession) (*domain.WorkSession, bool) {
	updated, err := c.sessions.ConsumeWorkSessionHop(ctx, channel.WorkspaceID, session.ID)
	if err == nil {
		return updated, true
	}
	if !errors.Is(err, domain.ErrWorkSessionHopUnavailable) && !errors.Is(err, domain.ErrWorkSessionClosed) && !errors.Is(err, domain.ErrWorkSessionNotOpen) {
		// Store failure: fail soft — this run is not minted, log only.
		slog.WarnContext(ctx, "channels: hop consumption failed; run not minted",
			"channel_id", channel.ID, "work_session_id", session.ID, "error", err)
		return nil, false
	}
	if errors.Is(err, domain.ErrWorkSessionHopUnavailable) {
		c.pauseForBudgetExhaustion(ctx, channel, session)
	}
	return nil, false
}

// pauseForBudgetExhaustion pauses the session as budget-exhausted and
// notifies the facilitator with ONE free status summons — the notification
// exception that does not consume a hop (channel-teams D1). A raced pause
// (ErrWorkSessionNotOpen / ErrWorkSessionClosed) is skipped: the first
// transition wins.
func (c *Chokepoint) pauseForBudgetExhaustion(ctx context.Context, channel *domain.Channel, session *domain.WorkSession) {
	paused, err := c.sessions.PauseWorkSession(ctx, channel.WorkspaceID, session.ID, domain.WorkSessionPauseBudgetExhausted)
	if err != nil {
		slog.InfoContext(ctx, "channels: budget pause skipped (already handled)",
			"channel_id", channel.ID, "work_session_id", session.ID, "error", err)
		return
	}
	slog.InfoContext(ctx, "channels: work session budget exhausted; paused for facilitator status",
		"channel_id", channel.ID, "work_session_id", session.ID, "hops_used", paused.HopsUsed)
	c.hub.Broadcast(channel.ID, newSessionUpdatedEvent(*paused))
	c.cancelWatchdog(session.ID)

	input := fmt.Sprintf("[#%s] (budget) The work session has used its full hop budget (%d). Post a status for the humans, route the next step, ask a human to resume, or close the session.",
		channel.Slug, paused.Budget)
	c.summonFacilitatorNotify(ctx, channel, paused, input)
}

// summonFacilitatorNotify mints the facilitator's notification run — the
// budget-exhaustion status and the stall watchdog. It bills NO hop (the
// notification exception) and roots the run at the session's kickoff message
// so the auto-posted reply lands in the session feed. A human facilitator
// cannot be summoned (their powers reduce to closing, D2).
func (c *Chokepoint) summonFacilitatorNotify(ctx context.Context, channel *domain.Channel, session *domain.WorkSession, input string) {
	members, err := c.channels.ListChannelMembers(ctx, channel.WorkspaceID, channel.ID)
	if err != nil {
		slog.WarnContext(ctx, "channels: facilitator notification roster read failed",
			"channel_id", channel.ID, "error", err)
		return
	}
	facilitator := facilitatorOf(members)
	if facilitator == nil || facilitator.MemberType != domain.ChannelMemberTypeAgent {
		slog.InfoContext(ctx, "channels: no agent facilitator to notify",
			"channel_id", channel.ID, "work_session_id", session.ID)
		return
	}
	c.submitRun(ctx, submitRequest{
		channel:       channel,
		agentID:       facilitator.AgentID,
		userID:        c.sessionInitiatingUser(ctx, channel, session),
		workSessionID: session.ID,
		input:         input,
		rootMessageID: session.RootMessageID,
		chainDepth:    0,
	})
}

// sessionInitiatingUser resolves the workspace member a session notification
// run executes under: the kickoff message's human author (the session's
// originating human). An unresolvable author yields "" — the run is rejected
// at validation and surfaces as a failed run event, never a silent post.
func (c *Chokepoint) sessionInitiatingUser(ctx context.Context, channel *domain.Channel, session *domain.WorkSession) string {
	rows, err := c.listFeed(ctx, channel.WorkspaceID, channel.ID)
	if err != nil {
		slog.WarnContext(ctx, "channels: kickoff author lookup failed",
			"channel_id", channel.ID, "work_session_id", session.ID, "error", err)
		return ""
	}
	for _, msg := range rows {
		if msg.ID == session.RootMessageID && msg.AuthorType == domain.ChannelMemberTypeUser {
			return msg.AuthorUserID
		}
	}
	return ""
}

// -------------------------------------------------------------------------
// Stall watchdog (channel-teams D2/D11): one facilitator summons per idle
// period, armed per open session, re-armed by session messages.
// -------------------------------------------------------------------------

// StartWatchdog blocks until ctx is done, arming a stall timer for every
// session that is open at boot. Per open session, idleness longer than the
// idle period since the session's last feed message fires ONE watchdog
// summons to the facilitator (a run, not a feed message) that bills one hop
// — a budget-exhausted session takes the pause path instead. The timer does
// not re-arm on firing: the next session message re-arms it.
func (c *Chokepoint) StartWatchdog(ctx context.Context) {
	sessions, err := c.sessions.ListOpenWorkSessions(ctx)
	if err != nil {
		slog.WarnContext(ctx, "channels: watchdog boot scan failed", "error", err)
	} else {
		for i := range sessions {
			session := &sessions[i]
			if session.Status != domain.WorkSessionOpen {
				continue // paused sessions carry no timer
			}
			channel, err := c.channels.ChannelByID(ctx, session.WorkspaceID, session.ChannelID)
			if err != nil {
				continue
			}
			anchor := c.lastSessionMessageAt(ctx, session)
			c.rearmWatchdog(channel, session, anchor)
		}
	}

	<-ctx.Done()
	c.stopAllWatchdogs()
}

// rearmWatchdog (re)arms the stall timer for one open session; the anchor is
// the session's last activity (a feed message's creation time). Paused or
// closed sessions never carry a timer.
func (c *Chokepoint) rearmWatchdog(channel *domain.Channel, session *domain.WorkSession, anchor time.Time) {
	if session == nil || session.Status != domain.WorkSessionOpen {
		return
	}
	c.watchdogMu.Lock()
	defer c.watchdogMu.Unlock()
	if timer, exists := c.watchdogTimers[session.ID]; exists {
		timer.Stop()
	}

	remaining := c.watchdogIdle - time.Since(anchor)
	if remaining < 0 {
		remaining = 0
	}
	c.watchdogTimers[session.ID] = time.AfterFunc(remaining, func() {
		c.onWatchdogFire(channel.WorkspaceID, channel.ID, session.ID)
	})
}

// cancelWatchdog stops and forgets a session's stall timer (pause and close).
func (c *Chokepoint) cancelWatchdog(workSessionID string) {
	c.watchdogMu.Lock()
	defer c.watchdogMu.Unlock()
	if timer, exists := c.watchdogTimers[workSessionID]; exists {
		timer.Stop()
		delete(c.watchdogTimers, workSessionID)
	}
}

func (c *Chokepoint) stopAllWatchdogs() {
	c.watchdogMu.Lock()
	defer c.watchdogMu.Unlock()
	for id, timer := range c.watchdogTimers {
		timer.Stop()
		delete(c.watchdogTimers, id)
	}
}

// onWatchdogFire runs one idle-period watchdog: re-validates the session,
// bills one hop (an exhausted budget takes the pause path), and summons the
// facilitator once — the timer does not re-arm until the next session
// message.
func (c *Chokepoint) onWatchdogFire(workspaceID, channelID, workSessionID string) {
	// Detached context: the watchdog outlives any request that armed it.
	ctx := context.WithoutCancel(context.Background())

	session, err := c.sessions.GetWorkSession(ctx, workspaceID, workSessionID)
	if err != nil || session.Status != domain.WorkSessionOpen {
		return // closed or paused since arming: nothing to watch
	}
	channel, err := c.channels.ChannelByID(ctx, workspaceID, channelID)
	if err != nil {
		return
	}
	slog.InfoContext(ctx, "channels: work session idle; watchdog summoning facilitator",
		"channel_id", channelID, "work_session_id", workSessionID)

	if _, ok := c.billSessionHop(ctx, channel, session); !ok {
		return // paused for budget exhaustion (the pause path notified instead)
	}

	input := fmt.Sprintf("[#%s] (watchdog) The work session has been idle for %dm. Review /project/PLAN.md and route the next step, ask the humans, or close the session.",
		channel.Slug, int(c.watchdogIdle.Minutes()))
	c.summonFacilitatorNotify(ctx, channel, session, input)
}

// lastSessionMessageAt finds the session's most recent feed message — the
// idle anchor. A session whose kickoff message cannot be found anchors at
// the session's own creation.
func (c *Chokepoint) lastSessionMessageAt(ctx context.Context, session *domain.WorkSession) time.Time {
	rows, err := c.listFeed(ctx, session.WorkspaceID, session.ChannelID)
	if err != nil {
		return session.CreatedAt
	}
	var newest time.Time
	for _, msg := range rows {
		if msg.WorkSessionID == nil || *msg.WorkSessionID != session.ID {
			continue
		}
		if newest.IsZero() || msg.CreatedAt.After(newest) {
			newest = msg.CreatedAt
		}
	}
	if newest.IsZero() {
		return session.CreatedAt
	}
	return newest
}
