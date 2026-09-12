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

// schedulerFixture seeds a workspace, provider, agent, and creator user —
// everything a scheduler row references.
type schedulerFixture struct {
	Store       store.Store
	WorkspaceID string
	AgentID     string
	CreatedBy   string
}

func seedSchedulerFixture(t *testing.T) schedulerFixture {
	t.Helper()
	ctx := context.Background()
	s := fake.New()

	u := &domain.User{Email: "creator@example.com", Name: "Creator"}
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
	return schedulerFixture{Store: s, WorkspaceID: ws.ID, AgentID: a.ID, CreatedBy: u.ID}
}

func newRecurringScheduler(f schedulerFixture, name, expr string) *domain.Scheduler {
	return &domain.Scheduler{
		WorkspaceID: f.WorkspaceID,
		AgentID:     f.AgentID,
		CreatedBy:   &f.CreatedBy,
		Name:        name,
		Prompt:      "Do the thing",
		Kind:        domain.SchedulerKindRecurring,
		Expr:        expr,
		Enabled:     true,
	}
}

// seedPausedOnce creates a disabled one-shot (future RunAt, required at
// create) and then enables it with a backdated instant through the update
// path — the only way to materialize a due row, since the store recomputes
// recurring next_run_at from now on every write.
func seedPausedOnce(t *testing.T, f schedulerFixture, name string, runAt time.Time, enabled bool) *domain.Scheduler {
	t.Helper()
	ctx := context.Background()
	future := time.Now().UTC().Add(time.Hour)
	s := &domain.Scheduler{
		WorkspaceID: f.WorkspaceID,
		AgentID:     f.AgentID,
		Name:        name,
		Prompt:      "Remind",
		Kind:        domain.SchedulerKindOnce,
		RunAt:       &future,
		Enabled:     false,
	}
	if err := f.Store.Schedulers().CreateScheduler(ctx, f.WorkspaceID, s); err != nil {
		t.Fatalf("create once %q: %v", name, err)
	}
	s.Enabled = enabled
	s.RunAt = &runAt
	if err := f.Store.Schedulers().UpdateScheduler(ctx, f.WorkspaceID, s); err != nil {
		t.Fatalf("backdate once %q: %v", name, err)
	}
	return s
}

func TestFakeSchedulerStore_CRUD(t *testing.T) {
	ctx := context.Background()
	f := seedSchedulerFixture(t)
	st := f.Store.Schedulers()

	// Create computes next_run_at from the expression.
	s := newRecurringScheduler(f, "morning-digest", "0 9 * * 1-5")
	if err := st.CreateScheduler(ctx, f.WorkspaceID, s); err != nil {
		t.Fatalf("create: %v", err)
	}
	if s.ID == "" || s.NextRunAt == nil {
		t.Fatalf("expected id and next_run_at, got %+v", s)
	}
	if s.Delivery.Type != domain.SchedulerDeliveryThread {
		t.Fatalf("expected delivery defaulted to thread, got %q", s.Delivery.Type)
	}

	// Duplicate name for the same agent conflicts.
	dup := newRecurringScheduler(f, "morning-digest", "0 9 * * 1-5")
	if err := st.CreateScheduler(ctx, f.WorkspaceID, dup); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate name, got %v", err)
	}

	// Unknown agent / workspace fail with NotFound (FK parity).
	unknownAgent := newRecurringScheduler(f, "x", "0 9 * * *")
	unknownAgent.AgentID = "other-agent"
	if err := st.CreateScheduler(ctx, f.WorkspaceID, unknownAgent); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown agent, got %v", err)
	}
	if err := st.CreateScheduler(ctx, "ws-none", newRecurringScheduler(f, "y", "0 9 * * *")); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}

	// Get.
	got, err := st.GetScheduler(ctx, f.WorkspaceID, s.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v, %v", got, err)
	}
	if got.Name != "morning-digest" || got.CreatedBy == nil || *got.CreatedBy != f.CreatedBy {
		t.Fatalf("unexpected scheduler: %+v", got)
	}

	// Get absent is (nil, nil) — unknown and foreign-scoped alike.
	absent, err := st.GetScheduler(ctx, f.WorkspaceID, "missing-id")
	if err != nil || absent != nil {
		t.Fatalf("expected (nil, nil) for absent scheduler, got (%v, %v)", absent, err)
	}
	foreign, err := st.GetScheduler(ctx, "ws-other", s.ID)
	if err != nil || foreign != nil {
		t.Fatalf("expected (nil, nil) for foreign workspace, got (%v, %v)", foreign, err)
	}

	// List is workspace-scoped.
	list, err := st.ListSchedulers(ctx, f.WorkspaceID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %d schedulers, err %v", len(list), err)
	}

	// Update: disable clears next_run_at; re-enabling recomputes it.
	s.Enabled = false
	if err := st.UpdateScheduler(ctx, f.WorkspaceID, s); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = st.GetScheduler(ctx, f.WorkspaceID, s.ID)
	if got.Enabled || got.NextRunAt != nil {
		t.Fatalf("expected paused scheduler without next_run_at, got %+v", got)
	}
	s.Enabled = true
	if err := st.UpdateScheduler(ctx, f.WorkspaceID, s); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = st.GetScheduler(ctx, f.WorkspaceID, s.ID)
	if !got.Enabled || got.NextRunAt == nil {
		t.Fatalf("expected re-enabled scheduler with next_run_at, got %+v", got)
	}

	// Update of a foreign-scoped id is NotFound.
	other := newRecurringScheduler(f, "other", "0 9 * * *")
	other.ID = s.ID
	if err := st.UpdateScheduler(ctx, "ws-other", other); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for foreign update, got %v", err)
	}

	// Delete.
	if err := st.DeleteScheduler(ctx, f.WorkspaceID, s.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err = st.GetScheduler(ctx, f.WorkspaceID, s.ID)
	if err != nil || got != nil {
		t.Fatalf("expected absent after delete, got (%v, %v)", got, err)
	}
	if err := st.DeleteScheduler(ctx, f.WorkspaceID, s.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on double delete, got %v", err)
	}
}

func TestFakeSchedulerStore_OnceShapeAndValidation(t *testing.T) {
	ctx := context.Background()
	f := seedSchedulerFixture(t)
	st := f.Store.Schedulers()

	future := time.Now().UTC().Add(time.Hour)
	s := &domain.Scheduler{
		WorkspaceID: f.WorkspaceID,
		AgentID:     f.AgentID,
		Name:        "launch",
		Prompt:      "Remind",
		Kind:        domain.SchedulerKindOnce,
		RunAt:       &future,
		Enabled:     true,
	}
	if err := st.CreateScheduler(ctx, f.WorkspaceID, s); err != nil {
		t.Fatalf("create once: %v", err)
	}
	if s.NextRunAt == nil || !s.NextRunAt.Equal(future) {
		t.Fatalf("expected next_run_at = run_at, got %v", s.NextRunAt)
	}

	// Invalid expression is rejected at the store boundary.
	bad := newRecurringScheduler(f, "broken", "at nine")
	if err := st.CreateScheduler(ctx, f.WorkspaceID, bad); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for bad expr, got %v", err)
	}
}

func TestFakeSchedulerStore_ClaimRecurringReschedules(t *testing.T) {
	ctx := context.Background()
	f := seedSchedulerFixture(t)
	st := f.Store.Schedulers()

	now := time.Now().UTC().Truncate(time.Minute)
	s := newRecurringScheduler(f, "daily", "0 9 * * *")
	if err := st.CreateScheduler(ctx, f.WorkspaceID, s); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Two days later the stored occurrence is overdue: the claim fires it
	// once and lands the next occurrence strictly in the future.
	later := now.Add(48 * time.Hour)
	claims, err := st.ClaimDueSchedulers(ctx, later, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claims) != 1 || claims[0].Missed || claims[0].Scheduler.ID != s.ID {
		t.Fatalf("expected the recurring scheduler claimed, got %+v", claims)
	}
	claimed := claims[0].Scheduler
	if !claimed.Enabled || claimed.NextRunAt == nil || !claimed.NextRunAt.After(later) {
		t.Fatalf("expected rescheduled future occurrence, got %+v", claimed)
	}

	// Overlapping-tick duplicate claim: an immediate second claim finds
	// nothing due — the occurrence was consumed.
	claims, err = st.ClaimDueSchedulers(ctx, later, 10)
	if err != nil || len(claims) != 0 {
		t.Fatalf("expected no double fire, got %v (%v)", claims, err)
	}

	// An overdue row fires exactly once even after restart catch-up — the
	// next occurrence is always in the future, never replayed back-to-back.
	muchLater := later.Add(72 * time.Hour)
	claims, err = st.ClaimDueSchedulers(ctx, muchLater, 10)
	if err != nil || len(claims) != 1 {
		t.Fatalf("expected catch-up fire once, got %v (%v)", claims, err)
	}
	if !claims[0].Scheduler.NextRunAt.After(muchLater) {
		t.Fatalf("expected next occurrence after claim time, got %v", claims[0].Scheduler.NextRunAt)
	}
}

func TestFakeSchedulerStore_ClaimOnceFiresAndArchives(t *testing.T) {
	ctx := context.Background()
	f := seedSchedulerFixture(t)
	st := f.Store.Schedulers()

	now := time.Now().UTC()
	due := now.Add(-30 * time.Second) // inside the grace window
	once := seedPausedOnce(t, f, "once", due, true)

	claims, err := st.ClaimDueSchedulers(ctx, now, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claims) != 1 || claims[0].Missed {
		t.Fatalf("expected the once scheduler claimed (not missed), got %+v", claims)
	}
	if claims[0].Scheduler.ID != once.ID {
		t.Fatalf("claimed wrong scheduler: %+v", claims[0].Scheduler)
	}
	// Fired once → archived.
	if claims[0].Scheduler.Enabled || claims[0].Scheduler.NextRunAt != nil {
		t.Fatalf("expected once scheduler archived after fire, got %+v", claims[0].Scheduler)
	}

	// Archived rows never come back.
	claims, err = st.ClaimDueSchedulers(ctx, now.Add(time.Hour), 10)
	if err != nil || len(claims) != 0 {
		t.Fatalf("expected archived once to stay quiet, got %v (%v)", claims, err)
	}
}

func TestFakeSchedulerStore_ClaimOncePastGraceIsMissed(t *testing.T) {
	ctx := context.Background()
	f := seedSchedulerFixture(t)
	st := f.Store.Schedulers()

	now := time.Now().UTC()
	stale := now.Add(-2 * time.Hour) // beyond the 1h grace window
	missed := seedPausedOnce(t, f, "stale", stale, true)

	claims, err := st.ClaimDueSchedulers(ctx, now, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claims) != 1 || !claims[0].Missed || claims[0].Scheduler.ID != missed.ID {
		t.Fatalf("expected stale once claimed as missed, got %+v", claims)
	}
	claimed := claims[0].Scheduler
	if claimed.Enabled || claimed.NextRunAt != nil {
		t.Fatalf("expected stale once archived, got %+v", claimed)
	}
	if claimed.LastRun == nil ||
		claimed.LastRun.Status != domain.SchedulerRunStatusMissed ||
		claimed.LastRun.Trigger != domain.SchedulerTriggerScheduled {
		t.Fatalf("expected last_run status missed, got %+v", claimed.LastRun)
	}

	// A missed row is never claimed twice.
	claims, err = st.ClaimDueSchedulers(ctx, now.Add(time.Hour), 10)
	if err != nil || len(claims) != 0 {
		t.Fatalf("expected no further claims, got %v (%v)", claims, err)
	}
}

func TestFakeSchedulerStore_ClaimLimitAndPausedSkip(t *testing.T) {
	ctx := context.Background()
	f := seedSchedulerFixture(t)
	st := f.Store.Schedulers()

	now := time.Now().UTC()
	paused := newRecurringScheduler(f, "paused", "0 9 * * *")
	paused.Enabled = false
	if err := st.CreateScheduler(ctx, f.WorkspaceID, paused); err != nil {
		t.Fatalf("create paused: %v", err)
	}
	a := seedPausedOnce(t, f, "once-a", now.Add(-time.Minute), true)
	b := seedPausedOnce(t, f, "once-b", now.Add(-time.Minute), true)

	// Paused schedulers are skipped; limit caps the batch.
	claims, err := st.ClaimDueSchedulers(ctx, now, 1)
	if err != nil || len(claims) != 1 {
		t.Fatalf("expected exactly 1 claim under limit, got %v (%v)", claims, err)
	}
	if claims[0].Scheduler.ID != a.ID && claims[0].Scheduler.ID != b.ID {
		t.Fatalf("claimed an unexpected scheduler: %+v", claims[0])
	}
	// The other one is still due.
	claims, err = st.ClaimDueSchedulers(ctx, now, 10)
	if err != nil || len(claims) != 1 {
		t.Fatalf("expected the second once due, got %v (%v)", claims, err)
	}
}

func TestFakeSchedulerStore_RunRecords(t *testing.T) {
	ctx := context.Background()
	f := seedSchedulerFixture(t)
	st := f.Store.Schedulers()

	s := newRecurringScheduler(f, "digest", "0 9 * * *")
	if err := st.CreateScheduler(ctx, f.WorkspaceID, s); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Start three runs with staggered starts.
	start := time.Now().UTC().Add(-10 * time.Minute)
	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		run := &domain.SchedulerRun{
			WorkspaceID: f.WorkspaceID,
			SchedulerID: s.ID,
			SessionID:   "sched_" + s.ID + "_" + time.Now().Format("20060102") + string(rune('a'+i)),
			Trigger:     domain.SchedulerTriggerScheduled,
			StartedAt:   start.Add(time.Duration(i) * time.Minute),
		}
		if err := st.StartSchedulerRun(ctx, run); err != nil {
			t.Fatalf("start run %d: %v", i, err)
		}
		if run.Status != domain.SchedulerRunStatusRunning {
			t.Fatalf("expected running status on insert, got %q", run.Status)
		}
		ids = append(ids, run.ID)
	}

	// Finishing a run of a foreign workspace is NotFound.
	if err := st.FinishSchedulerRun(ctx, "ws-other", ids[0], domain.SchedulerRunStatusCompleted, 0, 0, "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for foreign finish, got %v", err)
	}

	// Finish one and verify the last_run mirror.
	if err := st.FinishSchedulerRun(ctx, f.WorkspaceID, ids[1], domain.SchedulerRunStatusCompleted, 1500, 42, domain.SchedulerDeliveryDelivered, ""); err != nil {
		t.Fatalf("finish: %v", err)
	}
	got, _ := st.GetScheduler(ctx, f.WorkspaceID, s.ID)
	if got.LastRun == nil {
		t.Fatal("expected last_run mirrored after finish")
	}
	if got.LastRun.Status != domain.SchedulerRunStatusCompleted ||
		got.LastRun.DurationMS != 1500 ||
		got.LastRun.TokensUsed != 42 ||
		got.LastRun.DeliveryStatus != domain.SchedulerDeliveryDelivered ||
		got.LastRun.Trigger != domain.SchedulerTriggerScheduled ||
		got.LastRun.SessionID == "" {
		t.Fatalf("unexpected last_run: %+v", got.LastRun)
	}

	// Listing is newest-first with the total.
	runs, total, err := st.ListSchedulerRuns(ctx, f.WorkspaceID, s.ID, 2, 0)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total 3, got %d", total)
	}
	if len(runs) != 2 {
		t.Fatalf("expected page of 2, got %d", len(runs))
	}
	if !runs[0].StartedAt.After(runs[1].StartedAt) {
		t.Fatalf("expected newest-first ordering, got %v then %v", runs[0].StartedAt, runs[1].StartedAt)
	}

	// Offset pagination.
	page2, total2, err := st.ListSchedulerRuns(ctx, f.WorkspaceID, s.ID, 2, 2)
	if err != nil || total2 != 3 || len(page2) != 1 {
		t.Fatalf("offset page: %d runs, total %d, err %v", len(page2), total2, err)
	}

	// Workspace isolation: runs are invisible from another workspace.
	foreignRuns, foreignTotal, err := st.ListSchedulerRuns(ctx, "ws-other", s.ID, 10, 0)
	if err != nil || foreignTotal != 0 || len(foreignRuns) != 0 {
		t.Fatalf("expected empty foreign listing, got %d/%d (%v)", len(foreignRuns), foreignTotal, err)
	}

	// Finishing an unknown run is NotFound.
	if err := st.FinishSchedulerRun(ctx, f.WorkspaceID, "nope", domain.SchedulerRunStatusFailed, 0, 0, "", "boom"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound finishing unknown run, got %v", err)
	}
}

func TestFakeSchedulerStore_AgentDeleteCascades(t *testing.T) {
	ctx := context.Background()
	f := seedSchedulerFixture(t)
	st := f.Store.Schedulers()

	s := newRecurringScheduler(f, "doomed", "0 9 * * *")
	if err := st.CreateScheduler(ctx, f.WorkspaceID, s); err != nil {
		t.Fatalf("create: %v", err)
	}
	run := &domain.SchedulerRun{
		WorkspaceID: f.WorkspaceID,
		SchedulerID: s.ID,
		SessionID:   "sched_" + s.ID + "_1",
		Trigger:     domain.SchedulerTriggerManual,
		StartedAt:   time.Now().UTC(),
	}
	if err := st.StartSchedulerRun(ctx, run); err != nil {
		t.Fatalf("start run: %v", err)
	}

	if err := f.Store.Agents().Delete(ctx, f.WorkspaceID, f.AgentID); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	got, err := st.GetScheduler(ctx, f.WorkspaceID, s.ID)
	if err != nil || got != nil {
		t.Fatalf("expected scheduler cascaded away, got (%v, %v)", got, err)
	}
	_, total, err := st.ListSchedulerRuns(ctx, f.WorkspaceID, s.ID, 10, 0)
	if err != nil || total != 0 {
		t.Fatalf("expected runs cascaded away, got total %d (%v)", total, err)
	}
}
