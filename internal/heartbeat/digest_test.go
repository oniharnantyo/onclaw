package heartbeat

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// digestFixture carries the seeded ids the digest tests assert against.
type digestFixture struct {
	st        store.Store
	wsID      string
	agentID   string
	userID    string
	creatorID string
}

// newDigestFixture seeds the workspace, creator, provider, and agent every
// store reference requires.
func newDigestFixture(t *testing.T) digestFixture {
	t.Helper()
	ctx := context.Background()
	st := fake.New()

	u := &domain.User{Email: "creator@example.com", Name: "Creator"}
	if err := st.Users().Create(ctx, u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	ws := &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Name: "main", Type: "openai"}
	if err := st.Providers().Create(ctx, p); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: p.ID, Model: "gpt"}
	if err := st.Agents().Create(ctx, a); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return digestFixture{st: st, wsID: ws.ID, agentID: a.ID, userID: u.ID, creatorID: u.ID}
}

func (f digestFixture) composer(t *testing.T) *DigestComposer {
	t.Helper()
	return NewDigestComposer(f.st.Channels(), f.st.Schedulers(), f.st.Users(), f.st.Agents(), discardLogger())
}

func (f digestFixture) seedChannel(t *testing.T, name, slug string) domain.Channel {
	t.Helper()
	ch := &domain.Channel{WorkspaceID: f.wsID, Name: name, Slug: slug}
	if err := f.st.Channels().CreateChannel(context.Background(), ch); err != nil {
		t.Fatalf("seed channel %q: %v", slug, err)
	}
	return *ch
}

func (f digestFixture) seedAgentMessage(t *testing.T, channelID, body string, at time.Time) {
	t.Helper()
	msg := &domain.ChannelMessage{
		WorkspaceID:   f.wsID,
		ChannelID:     channelID,
		AuthorType:    domain.ChannelMemberTypeAgent,
		AuthorAgentID: f.agentID,
		Body:          body,
		CreatedAt:     at,
	}
	if err := f.st.Channels().InsertChannelMessage(context.Background(), msg); err != nil {
		t.Fatalf("seed message: %v", err)
	}
}

func (f digestFixture) seedSchedulerWithRun(t *testing.T, name string, status string, runErr string, startedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	sched := &domain.Scheduler{
		WorkspaceID: f.wsID,
		AgentID:     f.agentID,
		CreatedBy:   &f.creatorID,
		Name:        name,
		Prompt:      "Do the thing",
		Kind:        domain.SchedulerKindRecurring,
		Expr:        "0 12 * * *",
		Enabled:     true,
	}
	if err := f.st.Schedulers().CreateScheduler(ctx, f.wsID, sched); err != nil {
		t.Fatalf("seed scheduler %q: %v", name, err)
	}
	run := &domain.SchedulerRun{
		WorkspaceID: f.wsID,
		SchedulerID: sched.ID,
		SessionID:   "sched-seed",
		StartedAt:   startedAt,
	}
	if err := f.st.Schedulers().StartSchedulerRun(ctx, run); err != nil {
		t.Fatalf("seed scheduler run: %v", err)
	}
	if err := f.st.Schedulers().FinishSchedulerRun(ctx, f.wsID, run.ID, status, 10, 5, "", runErr, ""); err != nil {
		t.Fatalf("finish seeded scheduler run: %v", err)
	}
}

// TestDigestEmpty: no channels and no scheduler activity since the instant —
// the composer returns "" so the runner's fallback wording renders (D9).
func TestDigestEmpty(t *testing.T) {
	f := newDigestFixture(t)
	if got := f.composer(t).Compose(context.Background(), f.wsID, baseTime); got != "" {
		t.Fatalf("empty workspace digest: got %q, want \"\"", got)
	}
}

// TestDigestChannelPreviewsAndSinceFilter covers the channel half: messages
// at or after since appear as "#slug author: preview" lines with authors
// resolved through the store (agent authors marked), messages before since
// are excluded, whitespace folds, and over-long previews truncate at 120
// runes with an ellipsis.
func TestDigestChannelPreviewsAndSinceFilter(t *testing.T) {
	f := newDigestFixture(t)
	ch := f.seedChannel(t, "Incidents", "incidents")

	f.seedAgentMessage(t, ch.ID, "stale before the window", baseTime.Add(-time.Minute))
	f.seedAgentMessage(t, ch.ID, strings.Repeat("a", 200), baseTime)
	f.seedAgentMessage(t, ch.ID, "multi\nline\tbody  folds", baseTime.Add(time.Minute))

	got := f.composer(t).Compose(context.Background(), f.wsID, baseTime)

	if !strings.Contains(got, "#incidents Atlas (agent) ") {
		t.Fatalf("digest must carry the channel slug and resolved agent author, got:\n%s", got)
	}
	if strings.Contains(got, "stale before the window") {
		t.Fatalf("messages before since must be excluded, got:\n%s", got)
	}
	// Preview truncation: 200 runes cut to 119 + ellipsis.
	if !strings.Contains(got, strings.Repeat("a", 119)+"…") || strings.Contains(got, strings.Repeat("a", 120)) {
		t.Fatalf("preview must truncate to 120 runes with an ellipsis, got:\n%s", got)
	}
	if !strings.Contains(got, "multi line body folds") {
		t.Fatalf("whitespace must fold onto one line, got:\n%s", got)
	}
}

// TestDigestSchedulerOutcomesEmphasizeErrors covers the scheduler half:
// outcomes render as "name: status" lines and failed runs carry an
// emphasized ERROR excerpt.
func TestDigestSchedulerOutcomesEmphasizeErrors(t *testing.T) {
	f := newDigestFixture(t)
	f.seedSchedulerWithRun(t, "nightly", domain.SchedulerRunStatusCompleted, "", baseTime)
	f.seedSchedulerWithRun(t, "backup", domain.SchedulerRunStatusFailed, "connection reset after 30s of silence", baseTime)
	f.seedSchedulerWithRun(t, "stale", domain.SchedulerRunStatusFailed, "before the window", baseTime.Add(-time.Hour))

	got := f.composer(t).Compose(context.Background(), f.wsID, baseTime)

	if !strings.Contains(got, "[scheduler] nightly: completed") {
		t.Fatalf("completed run outcome missing, got:\n%s", got)
	}
	if !strings.Contains(got, "[scheduler] backup: failed — ERROR: connection reset after 30s of silence") {
		t.Fatalf("failed run must carry an emphasized error excerpt, got:\n%s", got)
	}
	if strings.Contains(got, "before the window") {
		t.Fatalf("runs before since must be excluded, got:\n%s", got)
	}
}

// TestDigestErrorExcerptTruncates: a long error excerpt cuts at 80 runes.
func TestDigestErrorExcerptTruncates(t *testing.T) {
	f := newDigestFixture(t)
	f.seedSchedulerWithRun(t, "nightly", domain.SchedulerRunStatusFailed, strings.Repeat("e", 200), baseTime)

	got := f.composer(t).Compose(context.Background(), f.wsID, baseTime)
	if !strings.Contains(got, strings.Repeat("e", 79)+"…") || strings.Contains(got, strings.Repeat("e", 80)) {
		t.Fatalf("error excerpt must truncate to 80 runes, got:\n%s", got)
	}
}

// TestDigestCappedAround2KB: an overflowing workspace cuts the body at the
// ~2KB budget with an explicit truncation marker, and never splits a rune.
func TestDigestCappedAround2KB(t *testing.T) {
	f := newDigestFixture(t)
	for i := 0; i < 6; i++ {
		ch := f.seedChannel(t, strings.Repeat("c", 12)+"-"+string(rune('a'+i)), strings.Repeat("c", 12)+"-"+string(rune('a'+i)))
		for j := 0; j < 12; j++ {
			f.seedAgentMessage(t, ch.ID, strings.Repeat("m", digestPreviewMaxRunes), baseTime)
		}
	}

	got := f.composer(t).Compose(context.Background(), f.wsID, baseTime)

	if !strings.HasSuffix(got, digestTruncationMarker) {
		t.Fatalf("over-budget digest must end with the truncation marker, got suffix %q", got[max(0, len(got)-60):])
	}
	if len(got) > digestMaxBytes {
		t.Fatalf("capped digest exceeds the budget: %d bytes", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatal("capped digest split a multi-byte rune")
	}
	if !strings.Contains(got, "#"+strings.Repeat("c", 12)+"-a") {
		t.Fatal("capped digest must retain its earliest composed lines")
	}
}

// TestDigestForeignWorkspaceIsEmpty: the digest only reads the addressed
// workspace — another workspace's channels and schedulers never leak in.
func TestDigestForeignWorkspaceIsEmpty(t *testing.T) {
	f := newDigestFixture(t)
	other := newDigestFixture(t)
	ch := other.seedChannel(t, "Incidents", "incidents")
	other.seedAgentMessage(t, ch.ID, "foreign activity", baseTime)

	if got := f.composer(t).Compose(context.Background(), f.wsID, baseTime); got != "" {
		t.Fatalf("foreign workspace activity leaked: %q", got)
	}
}
