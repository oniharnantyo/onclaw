package agents

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Channel composition constants (integrate-agent-channels D8/D16).
const (
	// ChannelTailLimit is the catch-up tail size composed into channel runs.
	ChannelTailLimit = 30
	// channelColdSummonNote is prepended to the catch-up tail when the agent
	// authored none of its messages (a cold summon).
	channelColdSummonNote = "You have not spoken in this channel yet."
)

// ChannelContext supplies the channel roster/tail reads the runner composes
// channel runs from (integrate-agent-channels D8). It is a narrow
// consumer-side port: the channel chokepoint satisfies it over the channel
// store; tests use fakes. Every method carries a workspaceID predicate —
// tenant isolation is the implementation's job, as everywhere else.
type ChannelContext interface {
	// GetChannel resolves the channel within the workspace; an unknown id
	// surfaces the store's not-found error.
	GetChannel(ctx context.Context, workspaceID, channelID string) (domain.Channel, error)
	// ListChannelMembers returns the roster in add order.
	ListChannelMembers(ctx context.Context, workspaceID, channelID string) ([]domain.ChannelMember, error)
	// ChannelTail returns the last limit feed messages, oldest→newest. It MAY
	// include the message that triggered the run; the composer drops a
	// trailing message whose attributed text equals the turn input's
	// attributed portion (renderChannelCatchUp).
	ChannelTail(ctx context.Context, workspaceID, channelID string, limit int) ([]domain.ChannelMessage, error)
	// ChannelMessagesAfter is the backward (before-)cursor read backing the
	// channel.history tool: up to limit messages strictly OLDER than
	// beforeSeq, oldest→newest. beforeSeq 0 selects from the newest end of
	// the feed, so the first page is the newest limit messages. The name
	// reads "messages after a point in time" — the cursor bounds the NEW side;
	// paging moves toward older messages.
	ChannelMessagesAfter(ctx context.Context, workspaceID, channelID string, beforeSeq int64, limit int) ([]domain.ChannelMessage, error)
	// ActiveWorkSession returns the channel's active (open or paused) work
	// session (channel-teams D8), or (nil, nil) when the channel has none.
	// It backs the CHANNEL.md session block; a read failure fails the run
	// like every other composition read.
	ActiveWorkSession(ctx context.Context, workspaceID, channelID string) (*domain.WorkSession, error)
}

// ChannelFeed receives agent posts (the channel.post tool and future speaking
// paths) into the room through the single message chokepoint
// (integrate-agent-channels D2/D9): the chokepoint persists, resolves
// mentions, applies the silence policy and caps, and fans runs out.
type ChannelFeed interface {
	PostFromAgent(ctx context.Context, workspaceID, channelID, agentID, body string) (domain.ChannelMessage, error)
}

// ChannelHandles resolves feed-author handles for attributed output
// (CHANNEL.md roster, catch-up tail, channel.history lines). The runner
// provides the implementation over its user/agent stores; tools receive it
// through ToolContext.
//
// Handle rules (shared with the channel chokepoint's mention resolution):
//   - agents: the agent slug — unique per workspace and kebab-case by
//     validation, so it is the canonical mentionable form;
//   - humans: the lowercase display name with spaces replaced by dashes
//     ("Sarah Chen" → "sarah-chen").
type ChannelHandles interface {
	UserHandle(ctx context.Context, userID string) (string, error)
	AgentHandle(ctx context.Context, workspaceID, agentID string) (string, error)
}

// runnerChannelHandles is the runner's ChannelHandles implementation over its
// user and agent stores, with a per-instance cache: one instance lives for a
// single resolve/compose pass, so the cache never goes stale across roster
// edits.
type runnerChannelHandles struct {
	users        store.UserStore
	agents       store.AgentStore
	mu           sync.Mutex
	userHandles  map[string]string
	agentHandles map[string]string
}

// newRunnerChannelHandles builds a per-run handle resolver.
func newRunnerChannelHandles(users store.UserStore, agents store.AgentStore) *runnerChannelHandles {
	return &runnerChannelHandles{
		users:        users,
		agents:       agents,
		userHandles:  make(map[string]string),
		agentHandles: make(map[string]string),
	}
}

// UserHandle implements ChannelHandles.
func (h *runnerChannelHandles) UserHandle(ctx context.Context, userID string) (string, error) {
	h.mu.Lock()
	handle, ok := h.userHandles[userID]
	h.mu.Unlock()
	if ok {
		return handle, nil
	}
	user, err := h.users.ByID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("resolve user handle: %w", err)
	}
	handle = humanChannelHandle(user.Name)
	h.mu.Lock()
	h.userHandles[userID] = handle
	h.mu.Unlock()
	return handle, nil
}

// AgentHandle implements ChannelHandles.
func (h *runnerChannelHandles) AgentHandle(ctx context.Context, workspaceID, agentID string) (string, error) {
	h.mu.Lock()
	handle, ok := h.agentHandles[agentID]
	h.mu.Unlock()
	if ok {
		return handle, nil
	}
	agent, err := h.agents.ByID(ctx, workspaceID, agentID)
	if err != nil {
		return "", fmt.Errorf("resolve agent handle: %w", err)
	}
	h.mu.Lock()
	h.agentHandles[agentID] = agent.Slug
	h.mu.Unlock()
	return agent.Slug, nil
}

// humanChannelHandle derives a human member's @handle from the display name:
// lowercase, spaces → dashes. This is the SAME rule the channel chokepoint
// applies when resolving @mentions against the roster — keep them in lockstep
// (integrate-agent-channels 3.1/4.1).
func humanChannelHandle(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"))
}

// channelAuthorHandle resolves the @handle for a feed message's author.
func channelAuthorHandle(ctx context.Context, handles ChannelHandles, msg domain.ChannelMessage) (string, error) {
	switch msg.AuthorType {
	case domain.ChannelMemberTypeUser:
		return handles.UserHandle(ctx, msg.AuthorUserID)
	case domain.ChannelMemberTypeAgent:
		return handles.AgentHandle(ctx, msg.WorkspaceID, msg.AuthorAgentID)
	default:
		return "", fmt.Errorf("unknown channel author type %q", msg.AuthorType)
	}
}

// channelMemberHandle resolves the @handle for one roster row.
func channelMemberHandle(ctx context.Context, handles ChannelHandles, m domain.ChannelMember) (string, error) {
	switch m.MemberType {
	case domain.ChannelMemberTypeUser:
		return handles.UserHandle(ctx, m.UserID)
	case domain.ChannelMemberTypeAgent:
		return handles.AgentHandle(ctx, m.WorkspaceID, m.AgentID)
	default:
		return "", fmt.Errorf("unknown channel member type %q", m.MemberType)
	}
}

// composeChannelDocs builds the channel composition layers (design D8): the
// L1 CHANNEL.md virtual doc and the L2 catch-up tail. Called only for
// channel runs (ExecRequest.ChannelID set); read failures fail the run — a
// context gate must not silently disappear.
func (r *Runner) composeChannelDocs(ctx context.Context, req ExecRequest, domainAgent *domain.Agent) ([]string, error) {
	channel, err := r.channelContext.GetChannel(ctx, req.WorkspaceID, req.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("load channel: %w", err)
	}
	members, err := r.channelContext.ListChannelMembers(ctx, req.WorkspaceID, req.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("load channel members: %w", err)
	}
	tail, err := r.channelContext.ChannelTail(ctx, req.WorkspaceID, req.ChannelID, ChannelTailLimit)
	if err != nil {
		return nil, fmt.Errorf("load channel tail: %w", err)
	}

	handles := newRunnerChannelHandles(r.users, r.agents)
	doc, err := renderChannelDoc(ctx, handles, channel, members, domainAgent.Slug)
	if err != nil {
		return nil, err
	}
	// Session context rides the channel document (channel-teams D8): the
	// engagement state of the channel's active work session, appended to the
	// L1 doc. No session — or a failing read (which fails the run, like every
	// composition read) — leaves the doc untouched.
	session, err := r.channelContext.ActiveWorkSession(ctx, req.WorkspaceID, req.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("load active work session: %w", err)
	}
	if session != nil {
		doc += "\n\n" + renderWorkSessionBlock(*session, isChannelFacilitator(members, domainAgent.ID))
	}
	catchUp, err := renderChannelCatchUp(ctx, handles, channel, tail, req)
	if err != nil {
		return nil, err
	}
	return []string{doc, catchUp}, nil
}

// isChannelFacilitator reports whether the given agent's roster row carries
// the facilitator role (channel-teams D2). Missing membership is never
// facilitator.
func isChannelFacilitator(members []domain.ChannelMember, agentID string) bool {
	for _, m := range members {
		if m.MemberType == domain.ChannelMemberTypeAgent && m.AgentID == agentID {
			return m.Role == domain.ChannelMemberRoleFacilitator
		}
	}
	return false
}

// renderWorkSessionBlock renders the CHANNEL.md `## Work session` block
// (channel-teams D8) in the pinned shape: goal, status, hops remaining, and
// the shared project space pointer; the facilitator gets its closing power
// announced. Rendered only when ActiveWorkSession returned a session.
func renderWorkSessionBlock(session domain.WorkSession, facilitator bool) string {
	var sb strings.Builder
	sb.WriteString("## Work session\n")
	fmt.Fprintf(&sb, "Goal: %s\n", session.Goal)
	fmt.Fprintf(&sb, "Status: %s\n", workSessionStatusText(session))
	fmt.Fprintf(&sb, "Hops remaining: %d of %d\n", session.Budget-session.HopsUsed, session.Budget)
	sb.WriteString("Shared project space: /project (PLAN.md is the tracker — claim your area before writing)\n")
	if facilitator {
		sb.WriteString("You are the facilitator: you may close this session with session.close once the goal is met.")
	}
	return strings.TrimSpace(sb.String())
}

// workSessionStatusText renders the session's engagement state: `open`,
// `paused (awaiting-human)`, `paused (budget-exhausted)` — the pause reason
// is the stored value — or `closed` (defensive: ActiveWorkSession returns
// active sessions only).
func workSessionStatusText(session domain.WorkSession) string {
	switch session.Status {
	case domain.WorkSessionOpen:
		return string(domain.WorkSessionOpen)
	case domain.WorkSessionClosed:
		return string(domain.WorkSessionClosed)
	case domain.WorkSessionPaused:
		return string(domain.WorkSessionPaused) + " (" + string(session.PauseReason) + ")"
	default:
		return string(session.Status)
	}
}

// renderChannelDoc renders the L1 CHANNEL.md virtual doc (design D8) in the
// pinned shape. Empty purpose/conventions/specializations omit their lines —
// the columns default to ” and blank headings are noise, not content.
func renderChannelDoc(ctx context.Context, handles ChannelHandles, channel domain.Channel, members []domain.ChannelMember, selfHandle string) (string, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Channel: #%s — %s\n", channel.Slug, channel.Name)
	if strings.TrimSpace(channel.Purpose) != "" {
		fmt.Fprintf(&sb, "\nPurpose: %s\n", channel.Purpose)
	}
	if len(members) > 0 {
		sb.WriteString("\nMembers:\n")
		for _, m := range members {
			handle, err := channelMemberHandle(ctx, handles, m)
			if err != nil {
				return "", fmt.Errorf("render channel roster: %w", err)
			}
			line := fmt.Sprintf("  - @%s (%s)", handle, m.MemberType)
			if strings.TrimSpace(m.Specialization) != "" {
				line += " — " + m.Specialization
			}
			sb.WriteString(line + "\n")
		}
	}
	fmt.Fprintf(&sb, "\nYou are @%s.\n", selfHandle)
	if strings.TrimSpace(channel.Conventions) != "" {
		sb.WriteString("\n## Conventions\n")
		sb.WriteString(strings.TrimRight(channel.Conventions, "\n"))
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String()), nil
}

// renderChannelCatchUp renders the L2 catch-up tail (design D8): the last
// ChannelTailLimit feed messages verbatim, oldest→newest, each line attributed
// `@handle: body`, agent lines annotated with a run-summary one-liner. The
// triggering message MAY still be the tail's last entry (the chokepoint's
// ChannelTail may include it): it is dropped when its attributed text exactly
// equals the turn input's attributed portion (`[#slug] ` prefix stripped) —
// the trigger itself arrives as the turn input (L3). A cold summon — the
// agent authored none of the remaining messages — prepends the
// you-have-not-spoken note.
func renderChannelCatchUp(ctx context.Context, handles ChannelHandles, channel domain.Channel, tail []domain.ChannelMessage, req ExecRequest) (string, error) {
	lines := make([]string, 0, len(tail))
	for _, msg := range tail {
		line, err := channelMessageLine(ctx, handles, msg)
		if err != nil {
			return "", fmt.Errorf("render channel tail: %w", err)
		}
		lines = append(lines, line)
	}
	if req.Input != "" && len(lines) > 0 {
		trigger := strings.TrimPrefix(req.Input, "[#"+channel.Slug+"] ")
		if lines[len(lines)-1] == trigger {
			lines = lines[:len(lines)-1]
		}
	}

	cold := true
	for _, msg := range tail {
		if msg.AuthorType == domain.ChannelMemberTypeAgent && msg.AuthorAgentID == req.AgentID {
			cold = false
			break
		}
	}

	var sb strings.Builder
	sb.WriteString("# Channel catch-up\n")
	if cold {
		sb.WriteString("\n" + channelColdSummonNote + "\n")
	}
	if len(lines) > 0 {
		sb.WriteString("\n" + strings.Join(lines, "\n") + "\n")
	}
	return strings.TrimSpace(sb.String()), nil
}

// channelMessageLine renders one attributed feed line: `@handle: body`, agent
// lines annotated with the run-summary one-liner when one is present (D9).
// Bodies render verbatim — a multi-line body occupies multiple lines.
func channelMessageLine(ctx context.Context, handles ChannelHandles, msg domain.ChannelMessage) (string, error) {
	handle, err := channelAuthorHandle(ctx, handles, msg)
	if err != nil {
		return "", err
	}
	line := "@" + handle + ": " + msg.Body
	if msg.AuthorType == domain.ChannelMemberTypeAgent && msg.RunSummary != nil {
		line += " (" + runSummaryOneLiner(handle, msg.RunSummary) + ")"
	}
	return line, nil
}

// runSummaryOneLiner renders the D9 run footprint carried on an agent-authored
// feed message: `@atlas — grafana.query ×2 · files.write ×1 · 1.2s`. Tool
// names sort for determinism; the duration is seconds with one decimal.
func runSummaryOneLiner(handle string, summary *domain.RunSummary) string {
	names := make([]string, 0, len(summary.Tools))
	for name := range summary.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names)+1)
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s ×%d", name, summary.Tools[name]))
	}
	parts = append(parts, fmt.Sprintf("%.1fs", float64(summary.DurationMS)/1000))
	return fmt.Sprintf("@%s — %s", handle, strings.Join(parts, " · "))
}
