package fake_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// heartbeatFixture seeds a workspace, provider, agent, and creator user —
// everything a heartbeat row references.
type heartbeatFixture struct {
	Store       store.Store
	WorkspaceID string
	AgentID     string
	CreatedBy   string
}

func seedHeartbeatFixture(t *testing.T) heartbeatFixture {
	t.Helper()
	ctx := context.Background()
	s := fake.New()

	u := &domain.User{Email: "hb-creator@example.com", Name: "Creator"}
	if err := s.Users().Create(ctx, u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	ws := &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Name: "main", Type: "openai"}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	a := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  p.ID,
		Model:       "gpt",
	}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return heartbeatFixture{Store: s, WorkspaceID: ws.ID, AgentID: a.ID, CreatedBy: u.ID}
}

// newValidHeartbeat returns a heartbeat the domain validator accepts; the
// caller derives NextTickAt itself — the store persists it as given.
func newValidHeartbeat(f heartbeatFixture) *domain.Heartbeat {
	return &domain.Heartbeat{
		WorkspaceID: f.WorkspaceID,
		AgentID:     f.AgentID,
		CreatedBy:   &f.CreatedBy,
		Prompt:      "Check the overnight deploy",
		Expr:        "*/15 * * * *",
		Delivery:    domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryCreatorDM},
		Enabled:     true,
	}
}

// putDueHeartbeat validates, derives, and stores the heartbeat with a next
// tick one minute in the past — due at the passed now (the caller-side
// contract the service layer implements).
func putDueHeartbeat(t *testing.T, f heartbeatFixture, hb *domain.Heartbeat, now time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := domain.ValidateHeartbeat(hb, now); err != nil {
		t.Fatalf("validate heartbeat: %v", err)
	}
	if _, err := domain.NextRun(hb.Expr, now, time.UTC); err != nil {
		t.Fatalf("derive next tick: %v", err)
	}
	due := now.Add(-time.Minute)
	hb.NextTickAt = &due
	if err := f.Store.Heartbeats().PutHeartbeat(ctx, f.WorkspaceID, f.AgentID, hb); err != nil {
		t.Fatalf("put heartbeat: %v", err)
	}
}

func TestFakeHeartbeatStore_PutGetRoundtrip(t *testing.T) {
	ctx := context.Background()
	f := seedHeartbeatFixture(t)
	st := f.Store.Heartbeats()
	now := time.Now().UTC()

	// Get before any heartbeat: absence is (nil, nil), not an error.
	absent, err := st.GetHeartbeat(ctx, f.WorkspaceID, f.AgentID)
	if err != nil || absent != nil {
		t.Fatalf("expected (nil, nil) for absent heartbeat, got (%v, %v)", absent, err)
	}

	hb := newValidHeartbeat(f)
	if err := domain.ValidateHeartbeat(hb, now); err != nil {
		t.Fatalf("validate: %v", err)
	}
	next, err := domain.NextRun(hb.Expr, now, time.UTC)
	if err != nil {
		t.Fatalf("derive next tick: %v", err)
	}
	hb.NextTickAt = next
	if err := st.PutHeartbeat(ctx, f.WorkspaceID, f.AgentID, hb); err != nil {
		t.Fatalf("put: %v", err)
	}
	if hb.ID == "" || hb.CreatedAt.IsZero() {
		t.Fatalf("expected id and timestamps assigned on create, got %+v", hb)
	}

	got, err := st.GetHeartbeat(ctx, f.WorkspaceID, f.AgentID)
	if err != nil || got == nil {
		t.Fatalf("get: %v, %v", got, err)
	}
	if got.ID != hb.ID ||
		got.Prompt != hb.Prompt ||
		got.Expr != hb.Expr ||
		got.Delivery.Type != domain.HeartbeatDeliveryCreatorDM ||
		!got.Enabled ||
		got.NextTickAt == nil ||
		got.FailureStreak != 0 ||
		got.LastTick != nil {
		t.Fatalf("unexpected roundtripped heartbeat: %+v", got)
	}
	if got.CreatedBy == nil || *got.CreatedBy != f.CreatedBy {
		t.Fatalf("expected created_by preserved, got %+v", got.CreatedBy)
	}

	// A foreign workspace's agent reads as absent — no existence leak.
	foreign, err := st.GetHeartbeat(ctx, "ws-other", f.AgentID)
	if err != nil || foreign != nil {
		t.Fatalf("expected (nil, nil) for foreign workspace, got (%v, %v)", foreign, err)
	}

	// Unknown workspace / agent references fail with NotFound (FK parity).
	orphan := newValidHeartbeat(f)
	orphan.AgentID = "other-agent"
	if err := st.PutHeartbeat(ctx, f.WorkspaceID, orphan.AgentID, orphan); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown agent, got %v", err)
	}
	if err := st.PutHeartbeat(ctx, "ws-none", f.AgentID, newValidHeartbeat(f)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}

	// A nil payload is invalid.
	if err := st.PutHeartbeat(ctx, f.WorkspaceID, f.AgentID, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil heartbeat, got %v", err)
	}
}

func TestFakeHeartbeatStore_ExactlyOnePerAgent(t *testing.T) {
	ctx := context.Background()
	f := seedHeartbeatFixture(t)
	st := f.Store.Heartbeats()
	now := time.Now().UTC()

	first := newValidHeartbeat(f)
	if err := domain.ValidateHeartbeat(first, now); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := st.PutHeartbeat(ctx, f.WorkspaceID, f.AgentID, first); err != nil {
		t.Fatalf("first put: %v", err)
	}

	// A replace keeps the row's identity: same id, same created_at, same
	// creator, updated config.
	second := newValidHeartbeat(f)
	second.Prompt = "Rewritten checklist"
	second.Expr = "0 9 * * *"
	second.LastTick = &domain.HeartbeatLastRun{Status: domain.HeartbeatRunStatusCompleted, SessionID: "hb_x"}
	second.FailureStreak = 3
	if err := domain.ValidateHeartbeat(second, now); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := st.PutHeartbeat(ctx, f.WorkspaceID, f.AgentID, second); err != nil {
		t.Fatalf("second put: %v", err)
	}

	got, err := st.GetHeartbeat(ctx, f.WorkspaceID, f.AgentID)
	if err != nil || got == nil {
		t.Fatalf("get: %v, %v", got, err)
	}
	if got.ID != first.ID {
		t.Fatalf("expected replace in place (id %q), got new id %q", first.ID, got.ID)
	}
	if !got.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("expected immutable created_at, got %v vs %v", got.CreatedAt, first.CreatedAt)
	}
	if got.CreatedBy == nil || *got.CreatedBy != f.CreatedBy {
		t.Fatalf("expected immutable created_by, got %+v", got.CreatedBy)
	}
	if got.Prompt != "Rewritten checklist" || got.Expr != "0 9 * * *" {
		t.Fatalf("expected replaced config, got %+v", got)
	}
	if got.LastTick == nil || got.FailureStreak != 3 {
		t.Fatalf("expected runtime state persisted as given, got %+v", got)
	}

	// Delete removes the row; a second delete is NotFound.
	if err := st.DeleteHeartbeat(ctx, f.WorkspaceID, f.AgentID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err = st.GetHeartbeat(ctx, f.WorkspaceID, f.AgentID)
	if err != nil || got != nil {
		t.Fatalf("expected absent after delete, got (%v, %v)", got, err)
	}
	if err := st.DeleteHeartbeat(ctx, f.WorkspaceID, f.AgentID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on double delete, got %v", err)
	}
}

// addFixtureAgent creates another provider+agent pair in the fixture's
// workspace (heartbeats are exactly-one-per-agent, so claim scenarios span
// agents) and returns its ID.
func addFixtureAgent(t *testing.T, f heartbeatFixture, slug string) string {
	t.Helper()
	ctx := context.Background()
	p := &domain.ProviderConfig{WorkspaceID: f.WorkspaceID, Name: "main-" + slug, Type: "openai"}
	if err := f.Store.Providers().Create(ctx, p); err != nil {
		t.Fatalf("seed provider %s: %v", slug, err)
	}
	a := &domain.Agent{WorkspaceID: f.WorkspaceID, Slug: slug, Name: "Agent " + slug, ProviderID: p.ID, Model: "gpt"}
	if err := f.Store.Agents().Create(ctx, a); err != nil {
		t.Fatalf("seed agent %s: %v", slug, err)
	}
	return a.ID
}

// putHeartbeatAt validates and stores the heartbeat with an explicit next
// tick — the caller-side contract the service layer implements.
func putHeartbeatAt(t *testing.T, f heartbeatFixture, agentID string, hb *domain.Heartbeat, next *time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := domain.ValidateHeartbeat(hb, time.Now().UTC()); err != nil {
		t.Fatalf("validate heartbeat: %v", err)
	}
	hb.NextTickAt = next
	if err := f.Store.Heartbeats().PutHeartbeat(ctx, f.WorkspaceID, agentID, hb); err != nil {
		t.Fatalf("put heartbeat: %v", err)
	}
}

func TestFakeHeartbeatStore_ClaimDueFiresOnceAndAdvances(t *testing.T) {
	ctx := context.Background()
	f := seedHeartbeatFixture(t)
	st := f.Store.Heartbeats()

	now := time.Date(2026, time.September, 10, 10, 7, 0, 0, time.UTC)
	dueAt := now.Add(-time.Minute)
	futureAt := now.Add(time.Hour)

	// Exactly one heartbeat per agent, so each claim state gets its own
	// agent: one due enabled, one disabled, one future.
	disabledAgent := addFixtureAgent(t, f, "paused")
	futureAgent := addFixtureAgent(t, f, "future")

	due := newValidHeartbeat(f)
	putHeartbeatAt(t, f, f.AgentID, due, &dueAt)
	disabled := newValidHeartbeat(f)
	disabled.Enabled = false
	putHeartbeatAt(t, f, disabledAgent, disabled, &dueAt)
	future := newValidHeartbeat(f)
	putHeartbeatAt(t, f, futureAgent, future, &futureAt)

	claims, err := st.ClaimDueHeartbeats(ctx, now, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claims) != 1 || claims[0].Heartbeat.ID != due.ID {
		t.Fatalf("expected only the due enabled heartbeat claimed, got %+v", claims)
	}
	wantNext, err := domain.NextRun("*/15 * * * *", now, time.UTC)
	if err != nil {
		t.Fatalf("derive expected next: %v", err)
	}
	claimed := claims[0].Heartbeat
	if claimed.NextTickAt == nil || !claimed.NextTickAt.Equal(*wantNext) {
		t.Fatalf("expected next_tick_at advanced to %v, got %v", wantNext, claimed.NextTickAt)
	}
	if !claimed.NextTickAt.After(now) {
		t.Fatal("expected advanced next_tick_at strictly in the future")
	}

	// The consumed occurrence cannot be claimed again — the second pass
	// sees nothing due (double-fire guard, add-agent-heartbeat D14).
	claims, err = st.ClaimDueHeartbeats(ctx, now, 10)
	if err != nil || len(claims) != 0 {
		t.Fatalf("expected empty follow-up claim, got %v (%v)", claims, err)
	}

	// A limit caps the batch: two fresh due rows yield exactly one claim.
	c := newValidHeartbeat(f)
	cAgent := addFixtureAgent(t, f, "c")
	putHeartbeatAt(t, f, cAgent, c, &dueAt)
	d := newValidHeartbeat(f)
	dAgent := addFixtureAgent(t, f, "d")
	putHeartbeatAt(t, f, dAgent, d, &dueAt)
	claims, err = st.ClaimDueHeartbeats(ctx, now, 1)
	if err != nil || len(claims) != 1 {
		t.Fatalf("expected exactly 1 claim under limit, got %v (%v)", claims, err)
	}
	if claims[0].Heartbeat.ID != c.ID && claims[0].Heartbeat.ID != d.ID {
		t.Fatalf("claimed an unexpected heartbeat: %+v", claims[0])
	}
	// The other one is still due.
	claims, err = st.ClaimDueHeartbeats(ctx, now, 10)
	if err != nil || len(claims) != 1 {
		t.Fatalf("expected the second due heartbeat, got %v (%v)", claims, err)
	}

	// Limit <= 0 claims nothing.
	claims, err = st.ClaimDueHeartbeats(ctx, now, 0)
	if err != nil || len(claims) != 0 {
		t.Fatalf("expected empty claim for limit 0, got %v (%v)", claims, err)
	}
}

func TestFakeHeartbeatStore_ClaimAdvancesInWorkspaceTimezone(t *testing.T) {
	ctx := context.Background()
	f := seedHeartbeatFixture(t)
	st := f.Store.Heartbeats()

	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	ws, _ := f.Store.Workspaces().ByID(ctx, f.WorkspaceID)
	ws.Timezone = "Asia/Jakarta"
	if err := f.Store.Workspaces().Update(ctx, ws); err != nil {
		t.Fatalf("set workspace tz: %v", err)
	}

	// 02:30 UTC = 09:30 Jakarta; the 09:00 Jakarta slot already passed, so
	// the claim advances to tomorrow's 09:00 Jakarta (02:00 UTC).
	now := time.Date(2026, time.September, 10, 2, 30, 0, 0, time.UTC)
	hb := newValidHeartbeat(f)
	hb.Expr = "0 9 * * *"
	putDueHeartbeat(t, f, hb, now)

	claims, err := st.ClaimDueHeartbeats(ctx, now, 10)
	if err != nil || len(claims) != 1 {
		t.Fatalf("expected one claim, got %v (%v)", claims, err)
	}
	want, err := domain.NextRun(hb.Expr, now, jakarta)
	if err != nil {
		t.Fatalf("derive expected: %v", err)
	}
	if claims[0].Heartbeat.NextTickAt == nil || !claims[0].Heartbeat.NextTickAt.Equal(*want) {
		t.Fatalf("expected workspace-tz advance to %v, got %v", want, claims[0].Heartbeat.NextTickAt)
	}
}

func TestFakeHeartbeatStore_ApplyHeartbeatOutcome(t *testing.T) {
	ctx := context.Background()
	f := seedHeartbeatFixture(t)
	st := f.Store.Heartbeats()
	now := time.Now().UTC()

	hb := newValidHeartbeat(f)
	putDueHeartbeat(t, f, hb, now)

	// Blocked leaves the streak untouched (policy grief, not failure).
	paused, err := st.ApplyHeartbeatOutcome(ctx, f.WorkspaceID, hb.ID, domain.HeartbeatRunStatusBlocked)
	if err != nil || paused {
		t.Fatalf("expected blocked to be a no-op, got paused=%v err=%v", paused, err)
	}
	got, _ := st.GetHeartbeat(ctx, f.WorkspaceID, f.AgentID)
	if got.FailureStreak != 0 {
		t.Fatalf("expected streak untouched by blocked, got %d", got.FailureStreak)
	}

	// Skipped likewise leaves the streak untouched.
	if _, err := st.ApplyHeartbeatOutcome(ctx, f.WorkspaceID, hb.ID, domain.HeartbeatRunStatusSkipped); err != nil {
		t.Fatalf("apply skipped: %v", err)
	}
	got, _ = st.GetHeartbeat(ctx, f.WorkspaceID, f.AgentID)
	if got.FailureStreak != 0 {
		t.Fatalf("expected streak untouched by skipped, got %d", got.FailureStreak)
	}

	// Completed resets the streak.
	if _, err := st.ApplyHeartbeatOutcome(ctx, f.WorkspaceID, hb.ID, domain.HeartbeatRunStatusFailed); err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	got, _ = st.GetHeartbeat(ctx, f.WorkspaceID, f.AgentID)
	if got.FailureStreak != 1 {
		t.Fatalf("expected streak 1 after failure, got %d", got.FailureStreak)
	}
	if _, err := st.ApplyHeartbeatOutcome(ctx, f.WorkspaceID, hb.ID, domain.HeartbeatRunStatusCompleted); err != nil {
		t.Fatalf("apply completed: %v", err)
	}
	got, _ = st.GetHeartbeat(ctx, f.WorkspaceID, f.AgentID)
	if got.FailureStreak != 0 {
		t.Fatalf("expected streak reset by completed, got %d", got.FailureStreak)
	}

	// Five consecutive failures auto-pause (add-agent-heartbeat D12).
	for i := 0; i < 4; i++ {
		paused, err := st.ApplyHeartbeatOutcome(ctx, f.WorkspaceID, hb.ID, domain.HeartbeatRunStatusFailed)
		if err != nil || paused {
			t.Fatalf("failure %d: expected no pause yet, got paused=%v err=%v", i+1, paused, err)
		}
	}
	paused, err = st.ApplyHeartbeatOutcome(ctx, f.WorkspaceID, hb.ID, domain.HeartbeatRunStatusFailed)
	if err != nil || !paused {
		t.Fatalf("fifth failure expected paused=true, got paused=%v err=%v", paused, err)
	}
	got, _ = st.GetHeartbeat(ctx, f.WorkspaceID, f.AgentID)
	if got.Enabled || got.NextTickAt != nil || got.FailureStreak != 5 {
		t.Fatalf("expected auto-paused heartbeat with streak retained, got %+v", got)
	}

	// A paused heartbeat is never claimed again.
	claims, err := st.ClaimDueHeartbeats(ctx, now.Add(time.Hour), 10)
	if err != nil || len(claims) != 0 {
		t.Fatalf("expected paused heartbeat to stay quiet, got %v (%v)", claims, err)
	}

	// Unknown / foreign-scoped heartbeat is NotFound.
	if _, err := st.ApplyHeartbeatOutcome(ctx, f.WorkspaceID, "nope", domain.HeartbeatRunStatusFailed); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown heartbeat, got %v", err)
	}
	if _, err := st.ApplyHeartbeatOutcome(ctx, "ws-other", hb.ID, domain.HeartbeatRunStatusFailed); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for foreign heartbeat, got %v", err)
	}
}

func TestFakeHeartbeatStore_RunRecords(t *testing.T) {
	ctx := context.Background()
	f := seedHeartbeatFixture(t)
	st := f.Store.Heartbeats()
	now := time.Now().UTC()

	hb := newValidHeartbeat(f)
	putDueHeartbeat(t, f, hb, now)
	sessionID := "hb_" + f.AgentID

	// Start three runs with staggered starts.
	start := now.Add(-10 * time.Minute)
	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		run := &domain.HeartbeatRun{
			WorkspaceID: f.WorkspaceID,
			HeartbeatID: hb.ID,
			AgentID:     f.AgentID,
			SessionID:   sessionID,
			Trigger:     domain.HeartbeatTriggerTick,
			StartedAt:   start.Add(time.Duration(i) * time.Minute),
		}
		if err := st.StartHeartbeatRun(ctx, run); err != nil {
			t.Fatalf("start run %d: %v", i, err)
		}
		if run.ID == "" || run.Status != domain.HeartbeatRunStatusRunning {
			t.Fatalf("expected running run with id, got %+v", run)
		}
		ids = append(ids, run.ID)
	}

	// The heartbeat must exist within the run's workspace.
	crossRun := &domain.HeartbeatRun{
		WorkspaceID: "ws-other",
		HeartbeatID: hb.ID,
		SessionID:   sessionID,
		Trigger:     domain.HeartbeatTriggerManual,
		StartedAt:   start,
	}
	if err := st.StartHeartbeatRun(ctx, crossRun); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-workspace run, got %v", err)
	}

	// Finishing a run of a foreign workspace is NotFound.
	if err := st.FinishHeartbeatRun(ctx, "ws-other", ids[0], domain.HeartbeatRunStatusCompleted, 0, 0, "", "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for foreign finish, got %v", err)
	}

	// Finish one and verify the last_tick mirror.
	if err := st.FinishHeartbeatRun(ctx, f.WorkspaceID, ids[1], domain.HeartbeatRunStatusCompleted, 1500, 42, domain.HeartbeatDeliveryStatusSuppressed, "", "tr-hb-1"); err != nil {
		t.Fatalf("finish: %v", err)
	}
	got, _ := st.GetHeartbeat(ctx, f.WorkspaceID, f.AgentID)
	if got.LastTick == nil {
		t.Fatal("expected last_tick mirrored after finish")
	}
	if got.LastTick.Status != domain.HeartbeatRunStatusCompleted ||
		got.LastTick.DurationMS != 1500 ||
		got.LastTick.TokensUsed != 42 ||
		got.LastTick.DeliveryStatus != domain.HeartbeatDeliveryStatusSuppressed ||
		got.LastTick.Trigger != domain.HeartbeatTriggerTick ||
		got.LastTick.SessionID != sessionID {
		t.Fatalf("unexpected last_tick: %+v", got.LastTick)
	}

	// Listing is newest-first with the total; the finished row carries its
	// trace id, unfinished rows none.
	runs, total, err := st.ListHeartbeatRuns(ctx, f.WorkspaceID, hb.ID, 2, 0)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if total != 3 || len(runs) != 2 {
		t.Fatalf("expected page of 2 with total 3, got %d/%d", len(runs), total)
	}
	if !runs[0].StartedAt.After(runs[1].StartedAt) {
		t.Fatalf("expected newest-first ordering, got %v then %v", runs[0].StartedAt, runs[1].StartedAt)
	}
	for _, r := range runs {
		if r.ID == ids[1] && r.TraceID != "tr-hb-1" {
			t.Fatalf("expected the finished run to carry its trace id, got %q", r.TraceID)
		}
		if r.ID != ids[1] && r.TraceID != "" {
			t.Fatalf("unfinished run must carry no trace id, got %q", r.TraceID)
		}
	}
	page2, total2, err := st.ListHeartbeatRuns(ctx, f.WorkspaceID, hb.ID, 2, 2)
	if err != nil || total2 != 3 || len(page2) != 1 {
		t.Fatalf("offset page: %d runs, total %d, err %v", len(page2), total2, err)
	}

	// Workspace isolation: runs are invisible from another workspace.
	foreignRuns, foreignTotal, err := st.ListHeartbeatRuns(ctx, "ws-other", hb.ID, 10, 0)
	if err != nil || foreignTotal != 0 || len(foreignRuns) != 0 {
		t.Fatalf("expected empty foreign listing, got %d/%d (%v)", len(foreignRuns), foreignTotal, err)
	}

	// Finishing an unknown run is NotFound.
	if err := st.FinishHeartbeatRun(ctx, f.WorkspaceID, "nope", domain.HeartbeatRunStatusFailed, 0, 0, "", "boom", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound finishing unknown run, got %v", err)
	}
}

func TestFakeHeartbeatStore_AgentDeleteCascades(t *testing.T) {
	ctx := context.Background()
	f := seedHeartbeatFixture(t)
	st := f.Store.Heartbeats()
	now := time.Now().UTC()

	hb := newValidHeartbeat(f)
	putDueHeartbeat(t, f, hb, now)
	run := &domain.HeartbeatRun{
		WorkspaceID: f.WorkspaceID,
		HeartbeatID: hb.ID,
		AgentID:     f.AgentID,
		SessionID:   "hb_" + f.AgentID,
		Trigger:     domain.HeartbeatTriggerManual,
		StartedAt:   now,
	}
	if err := st.StartHeartbeatRun(ctx, run); err != nil {
		t.Fatalf("start run: %v", err)
	}

	if err := f.Store.Agents().Delete(ctx, f.WorkspaceID, f.AgentID); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	got, err := st.GetHeartbeat(ctx, f.WorkspaceID, f.AgentID)
	if err != nil || got != nil {
		t.Fatalf("expected heartbeat cascaded away, got (%v, %v)", got, err)
	}
	_, total, err := st.ListHeartbeatRuns(ctx, f.WorkspaceID, hb.ID, 10, 0)
	if err != nil || total != 0 {
		t.Fatalf("expected tick records cascaded away, got total %d (%v)", total, err)
	}
}
