package heartbeat

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Digest sizing bounds (add-agent-heartbeat D9): the digest rides the
// persistent hb_ session's instruction every tick, so it stays compact —
// ~2KB overall, 120-char message previews, and a per-source line budget.
// The overall cap wins: when the composed lines exceed it the tail is cut
// with an explicit marker instead of silently dropping context.
const (
	digestMaxBytes         = 2048
	digestPreviewMaxRunes  = 120
	digestErrorMaxRunes    = 80
	digestMaxChannelLines  = 10
	digestMaxSchedulerRuns = 10
	digestTruncationMarker = "… (digest truncated)"
)

// DigestComposer composes the deterministic workspace-activity digest the
// heartbeat profile renders under "## Workspace activity" (add-agent-heartbeat
// D9): recent channel message previews and scheduler run outcomes since one
// absolute instant, so a tick only re-reads what happened since the previous
// one. Everything is composed in Go — no model involvement, no timestamps in
// the output — so identical inputs render byte-identical bodies.
//
// Store reads are best-effort context assembly, never run inputs: a read
// failure skips that source (logged) and can never fail a tick.
type DigestComposer struct {
	channels   store.ChannelStore
	schedulers store.SchedulerStore
	users      store.UserStore
	agents     store.AgentStore
	log        *slog.Logger
}

// NewDigestComposer constructs the composer over granular store ports,
// resolved non-nil by the composition root.
func NewDigestComposer(
	channels store.ChannelStore,
	schedulers store.SchedulerStore,
	users store.UserStore,
	agents store.AgentStore,
	log *slog.Logger,
) *DigestComposer {
	return &DigestComposer{
		channels:   channels,
		schedulers: schedulers,
		users:      users,
		agents:     agents,
		log:        log,
	}
}

// Compose returns the digest body for the workspace's activity since the
// absolute instant (the heartbeat's previous tick start, or its creation for
// the first tick — add-agent-heartbeat D9). The runner's profile renders the
// section header and its empty-digest fallback, so an empty body ("nothing
// happened") returns "" and the fallback wording appears.
func (c *DigestComposer) Compose(ctx context.Context, workspaceID string, since time.Time) string {
	lines := c.channelLines(ctx, workspaceID, since)
	lines = append(lines, c.schedulerLines(ctx, workspaceID, since)...)
	if len(lines) == 0 {
		return ""
	}
	return capDigest(strings.Join(lines, "\n"))
}

// channelLines renders the channel-message half: per workspace channel, the
// most recent messages at or after since as "author: preview" lines. The
// seq cursor reads ascending, so the composer reads the feed and keeps the
// newest window after since (CreatedAt filtering is client-side — seq and
// CreatedAt correlate, but CreatedAt is the contract).
func (c *DigestComposer) channelLines(ctx context.Context, workspaceID string, since time.Time) []string {
	channels, err := c.channels.ListChannels(ctx, workspaceID)
	if err != nil {
		c.log.WarnContext(ctx, "heartbeat: digest channel listing failed", "workspace_id", workspaceID, "error", err)
		return nil
	}
	names := newAuthorNames(ctx, workspaceID, c.users, c.agents)
	lines := make([]string, 0, len(channels))
	for _, ch := range channels {
		msgs, err := c.channels.ListChannelMessages(ctx, store.ListChannelMessagesParams{
			WorkspaceID: workspaceID,
			ChannelID:   ch.ID,
		})
		if err != nil {
			c.log.WarnContext(ctx, "heartbeat: digest channel read failed",
				"workspace_id", workspaceID, "channel_id", ch.ID, "error", err)
			continue
		}
		recent := make([]domain.ChannelMessage, 0, digestMaxChannelLines)
		for _, msg := range msgs {
			if msg.CreatedAt.Before(since) {
				continue
			}
			recent = append(recent, msg)
		}
		if len(recent) == 0 {
			continue
		}
		if len(recent) > digestMaxChannelLines {
			recent = recent[len(recent)-digestMaxChannelLines:]
		}
		for _, msg := range recent {
			lines = append(lines, "#"+channelLabel(ch)+" "+names.of(msg)+" "+preview(msg.Body))
		}
	}
	return lines
}

// schedulerLines renders the scheduler-outcome half: per workspace scheduler,
// the newest runs at or after since with their outcome, errors emphasized —
// a failed run's excerpt is the one thing an ambient tick must not miss.
func (c *DigestComposer) schedulerLines(ctx context.Context, workspaceID string, since time.Time) []string {
	schedulers, err := c.schedulers.ListSchedulers(ctx, workspaceID)
	if err != nil {
		c.log.WarnContext(ctx, "heartbeat: digest scheduler listing failed", "workspace_id", workspaceID, "error", err)
		return nil
	}
	lines := make([]string, 0, len(schedulers))
	for _, sched := range schedulers {
		runs, _, err := c.schedulers.ListSchedulerRuns(ctx, workspaceID, sched.ID, digestMaxSchedulerRuns, 0)
		if err != nil {
			c.log.WarnContext(ctx, "heartbeat: digest scheduler runs read failed",
				"workspace_id", workspaceID, "scheduler_id", sched.ID, "error", err)
			continue
		}
		for _, run := range runs {
			if run.StartedAt.Before(since) {
				continue
			}
			line := "[scheduler] " + sched.Name + ": " + run.Status
			if run.Error != "" {
				line += " — ERROR: " + excerpt(run.Error, digestErrorMaxRunes)
			}
			lines = append(lines, line)
		}
	}
	return lines
}

// channelLabel picks the channel's human label: slug (the #ops convention)
// falling back to the name, then the raw id.
func channelLabel(ch domain.Channel) string {
	switch {
	case ch.Slug != "":
		return ch.Slug
	case ch.Name != "":
		return ch.Name
	default:
		return ch.ID
	}
}

// authorNames caches display-name resolution for one Compose pass: authors
// resolve through the user/agent stores when cheap, falling back to the raw
// id. Agent authors carry an explicit marker — the digest must not read as a
// second human voice.
type authorNames struct {
	ctx       context.Context
	workspace string
	users     store.UserStore
	agents    store.AgentStore
	memo      map[string]string
}

func newAuthorNames(ctx context.Context, workspaceID string, users store.UserStore, agentsStore store.AgentStore) *authorNames {
	return &authorNames{ctx: ctx, workspace: workspaceID, users: users, agents: agentsStore, memo: make(map[string]string)}
}

func (a *authorNames) of(msg domain.ChannelMessage) string {
	switch msg.AuthorType {
	case domain.ChannelMemberTypeUser:
		return a.user(msg.AuthorUserID)
	case domain.ChannelMemberTypeAgent:
		return a.agent(msg.AuthorAgentID)
	default:
		return "unknown"
	}
}

func (a *authorNames) user(id string) string {
	if name, ok := a.memo["u:"+id]; ok {
		return name
	}
	name := id
	if u, err := a.users.ByID(a.ctx, id); err == nil && u.Name != "" {
		name = u.Name
	}
	a.memo["u:"+id] = name
	return name
}

func (a *authorNames) agent(id string) string {
	if name, ok := a.memo["a:"+id]; ok {
		return name
	}
	name := id
	if ag, err := a.agents.ByID(a.ctx, a.workspace, id); err == nil && ag.Name != "" {
		name = ag.Name
	}
	name += " (agent)"
	a.memo["a:"+id] = name
	return name
}

// preview folds a message body onto one digest line, truncated to
// digestPreviewMaxRunes with an ellipsis — previews only; the transcript
// holds the full text.
func preview(body string) string {
	body = strings.Join(strings.Fields(body), " ")
	return excerpt(body, digestPreviewMaxRunes)
}

// excerpt truncates to at most max runes, marking a cut with an ellipsis.
func excerpt(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max-1]) + "…"
}

// capDigest enforces the ~2KB overall budget: over-budget bodies cut at a
// rune boundary with an explicit truncation marker, never silently.
func capDigest(body string) string {
	if len(body) <= digestMaxBytes {
		return body
	}
	budget := digestMaxBytes - len(digestTruncationMarker) - 1
	for budget > 0 && body[budget]&0xC0 == 0x80 {
		// Walk back off a split multi-byte rune so body[:budget] ends on a
		// rune boundary (at most three steps).
		budget--
	}
	return strings.TrimRight(body[:budget], "\n") + "\n" + digestTruncationMarker
}
