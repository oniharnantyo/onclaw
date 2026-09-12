//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// schSeedWorkspace creates a workspace (optionally setting its timezone).
func schSeedWorkspace(t *testing.T, ctx context.Context, s store.Store, slug, timezone string) *domain.Workspace {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "Scheduler WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}
	if timezone != "" {
		ws.Timezone = timezone
		if err := s.Workspaces().Update(ctx, ws); err != nil {
			t.Fatalf("unexpected update workspace error: %v", err)
		}
	}
	return ws
}

// schSeedAgent creates a workspace-scoped agent for scheduler bindings.
func schSeedAgent(t *testing.T, ctx context.Context, s store.Store, ws *domain.Workspace, slug string) *domain.Agent {
	t.Helper()
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI " + slug, Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("unexpected create provider error: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: slug, Name: "Agent " + slug, ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("unexpected create agent error: %v", err)
	}
	return a
}

// schBackdate rewrites schedule instants directly — the store recomputes
// next_run_at on every write, so tests reach due states through raw SQL.
func schBackdate(t *testing.T, ctx context.Context, schemaDSN, name string, instant time.Time) {
	t.Helper()
	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("connect for backdate: %v", err)
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx,
		`UPDATE schedulers SET next_run_at = $2 WHERE name = $1`,
		name, instant,
	)
	if err != nil {
		t.Fatalf("backdate %q: %v", name, err)
	}
}

// schBackdateRunAt rewrites a once scheduler's target instant directly.
func schBackdateRunAt(t *testing.T, ctx context.Context, schemaDSN, name string, instant time.Time) {
	t.Helper()
	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("connect for backdate: %v", err)
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx,
		`UPDATE schedulers SET run_at = $2 WHERE name = $1`,
		name, instant,
	)
	if err != nil {
		t.Fatalf("backdate run_at %q: %v", name, err)
	}
}

func TestIntegration_SchedulerStore_CRUD(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := schSeedWorkspace(t, ctx, s, "sch-crud", "")
	agent := schSeedAgent(t, ctx, s, ws, "atlas")
	user := &domain.User{Email: "sch-crud@example.com", Name: "Creator"}
	if err := s.Users().Create(ctx, user); err != nil {
		t.Fatalf("unexpected create user error: %v", err)
	}
	st := s.Schedulers()

	// 1. Create a recurring scheduler: next_run_at is derived, not trusted.
	sched := &domain.Scheduler{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		CreatedBy:   &user.ID,
		Name:        "morning-digest",
		Prompt:      "Summarize overnight activity",
		Kind:        domain.SchedulerKindRecurring,
		Expr:        "0 9 * * 1-5",
		Enabled:     true,
	}
	if err := st.CreateScheduler(ctx, ws.ID, sched); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if sched.ID == "" {
		t.Fatal("expected scheduler ID to be assigned")
	}
	if sched.NextRunAt == nil || !sched.NextRunAt.After(time.Now().UTC()) {
		t.Fatalf("expected derived future next_run_at, got %v", sched.NextRunAt)
	}
	if sched.Delivery.Type != domain.SchedulerDeliveryThread || sched.Delivery.ChannelID != "" {
		t.Fatalf("expected delivery defaulted to thread, got %+v", sched.Delivery)
	}

	// 2. Duplicate name for the same agent is a conflict; the store
	// pre-checks and the unique constraint is the backstop.
	dup := &domain.Scheduler{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Name:        "morning-digest",
		Prompt:      "again",
		Kind:        domain.SchedulerKindRecurring,
		Expr:        "0 10 * * *",
		Enabled:     true,
	}
	if err := st.CreateScheduler(ctx, ws.ID, dup); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate name, got %v", err)
	}

	// 3. Invalid expression is rejected before any SQL runs.
	bad := &domain.Scheduler{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Name:        "broken",
		Prompt:      "p",
		Kind:        domain.SchedulerKindRecurring,
		Expr:        "at nine",
		Enabled:     true,
	}
	if err := st.CreateScheduler(ctx, ws.ID, bad); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for bad expr, got %v", err)
	}

	// 4. Unknown workspace / agent references fail as NotFound (FK parity).
	if err := st.CreateScheduler(ctx, "00000000-0000-0000-0000-000000000000", sched); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}
	orphan := &domain.Scheduler{
		WorkspaceID: ws.ID,
		AgentID:     "00000000-0000-0000-0000-000000000000",
		Name:        "orphan",
		Prompt:      "p",
		Kind:        domain.SchedulerKindRecurring,
		Expr:        "0 9 * * *",
		Enabled:     true,
	}
	if err := st.CreateScheduler(ctx, ws.ID, orphan); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown agent, got %v", err)
	}

	// 5. One-shot: next_run_at mirrors run_at; kind shape is enforced.
	future := time.Now().UTC().Add(time.Hour)
	once := &domain.Scheduler{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Name:        "launch",
		Prompt:      "Remind",
		Kind:        domain.SchedulerKindOnce,
		RunAt:       &future,
		Enabled:     true,
	}
	if err := st.CreateScheduler(ctx, ws.ID, once); err != nil {
		t.Fatalf("unexpected once create error: %v", err)
	}
	if once.NextRunAt == nil || !once.NextRunAt.Equal(future) {
		t.Fatalf("expected next_run_at = run_at, got %v", once.NextRunAt)
	}

	// 6. Get / (nil, nil) for absent and foreign scopes.
	got, err := st.GetScheduler(ctx, ws.ID, sched.ID)
	if err != nil || got == nil {
		t.Fatalf("unexpected get result: (%v, %v)", got, err)
	}
	if got.CreatedBy == nil || *got.CreatedBy != user.ID {
		t.Fatalf("expected created_by preserved, got %+v", got.CreatedBy)
	}
	absent, err := st.GetScheduler(ctx, ws.ID, "00000000-0000-0000-0000-000000000001")
	if err != nil || absent != nil {
		t.Fatalf("expected (nil, nil) for absent scheduler, got (%v, %v)", absent, err)
	}
	foreign, err := st.GetScheduler(ctx, "00000000-0000-0000-0000-000000000002", sched.ID)
	if err != nil || foreign != nil {
		t.Fatalf("expected (nil, nil) for foreign workspace, got (%v, %v)", foreign, err)
	}

	// 7. List is workspace-scoped.
	list, err := st.ListSchedulers(ctx, ws.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("expected 2 schedulers, got %d (%v)", len(list), err)
	}
	foreignList, err := st.ListSchedulers(ctx, "00000000-0000-0000-0000-000000000002")
	if err != nil || len(foreignList) != 0 {
		t.Fatalf("expected empty foreign list, got %d (%v)", len(foreignList), err)
	}

	// 8. Update: pause clears next_run_at; re-enable recomputes from now;
	// created_at/created_by stay immutable.
	pausedAt := got.CreatedAt
	got.Enabled = false
	if err := st.UpdateScheduler(ctx, ws.ID, got); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	if got.NextRunAt != nil {
		t.Fatalf("expected cleared next_run_at while paused, got %v", got.NextRunAt)
	}
	reloaded, _ := st.GetScheduler(ctx, ws.ID, sched.ID)
	if reloaded.Enabled || reloaded.NextRunAt != nil {
		t.Fatalf("expected paused row without next occurrence, got %+v", reloaded)
	}
	reloaded.Enabled = true
	if err := st.UpdateScheduler(ctx, ws.ID, reloaded); err != nil {
		t.Fatalf("unexpected re-enable error: %v", err)
	}
	if reloaded.NextRunAt == nil || !reloaded.NextRunAt.After(time.Now().UTC()) {
		t.Fatalf("expected recomputed future next_run_at, got %v", reloaded.NextRunAt)
	}
	if !reloaded.CreatedAt.Equal(pausedAt) || reloaded.CreatedBy == nil || *reloaded.CreatedBy != user.ID {
		t.Fatalf("expected immutable created_at/created_by, got %+v", reloaded)
	}

	// 9. Delete removes the row; a second delete is NotFound.
	if err := st.DeleteScheduler(ctx, ws.ID, once.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if err := st.DeleteScheduler(ctx, ws.ID, once.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on double delete, got %v", err)
	}
}

func TestIntegration_SchedulerStore_WorkspaceTimezone(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := schSeedWorkspace(t, ctx, s, "sch-tz", "Asia/Jakarta")
	agent := schSeedAgent(t, ctx, s, ws, "beacon")
	st := s.Schedulers()

	sched := &domain.Scheduler{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Name:        "jakarta-morning",
		Prompt:      "p",
		Kind:        domain.SchedulerKindRecurring,
		Expr:        "0 9 * * *",
		Enabled:     true,
	}
	if err := st.CreateScheduler(ctx, ws.ID, sched); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	// 09:00 Asia/Jakarta is 02:00 UTC.
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	want, err := domain.NextRun(sched.Expr, time.Now().UTC(), jakarta)
	if err != nil {
		t.Fatalf("unexpected NextRun error: %v", err)
	}
	if sched.NextRunAt == nil || !sched.NextRunAt.Equal(*want) {
		t.Fatalf("expected next occurrence %v in workspace tz, got %v", want, sched.NextRunAt)
	}
}

func TestIntegration_SchedulerStore_ClaimExactlyOnceUnderConcurrency(t *testing.T) {
	s, schemaDSN, ctx := setupTestSchema(t)
	ws := schSeedWorkspace(t, ctx, s, "sch-race", "")
	agent := schSeedAgent(t, ctx, s, ws, "atlas")
	st := s.Schedulers()
	now := time.Now().UTC().Truncate(time.Second)

	// Seed 5 due recurring rows (backdated past the claim instant).
	const dueCount = 5
	for i := 0; i < dueCount; i++ {
		sched := &domain.Scheduler{
			WorkspaceID: ws.ID,
			AgentID:     agent.ID,
			Name:        fmt.Sprintf("due-%d", i),
			Prompt:      "p",
			Kind:        domain.SchedulerKindRecurring,
			Expr:        "0 9 * * *",
			Enabled:     true,
		}
		if err := st.CreateScheduler(ctx, ws.ID, sched); err != nil {
			t.Fatalf("unexpected create error: %v", err)
		}
		schBackdate(t, ctx, schemaDSN, sched.Name, now.Add(-time.Minute))
	}

	// Two simultaneous claimers race over the same due rows: SKIP LOCKED
	// must partition them so each row is claimed exactly once, and no
	// claimer may double-fire a row.
	const claimers = 2
	barrier := make(chan struct{})
	var wg sync.WaitGroup
	results := make([][]store.SchedulerClaim, claimers)
	errs := make([]error, claimers)
	for i := 0; i < claimers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-barrier
			results[idx], errs[idx] = st.ClaimDueSchedulers(ctx, now, dueCount)
		}(i)
	}
	close(barrier)
	wg.Wait()

	seen := make(map[string]int)
	total := 0
	for i := 0; i < claimers; i++ {
		if errs[i] != nil {
			t.Fatalf("claimer %d failed: %v", i, errs[i])
		}
		for _, c := range results[i] {
			if c.Missed {
				t.Fatalf("recurring row must never be missed: %+v", c)
			}
			seen[c.Scheduler.ID]++
			total++
			if c.Scheduler.NextRunAt == nil || !c.Scheduler.NextRunAt.After(now) {
				t.Fatalf("expected claimed row rescheduled into the future, got %+v", c.Scheduler)
			}
		}
	}
	if total != dueCount {
		t.Fatalf("expected exactly %d claims across claimers, got %d", dueCount, total)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("scheduler %s claimed %d times — double fire", id, n)
		}
	}

	// The consumed occurrences cannot be claimed again by the next tick.
	claims, err := st.ClaimDueSchedulers(ctx, now, dueCount)
	if err != nil || len(claims) != 0 {
		t.Fatalf("expected empty follow-up claim, got %v (%v)", claims, err)
	}
}

func TestIntegration_SchedulerStore_OverdueRecurringFiresOnceAndReschedules(t *testing.T) {
	s, schemaDSN, ctx := setupTestSchema(t)
	ws := schSeedWorkspace(t, ctx, s, "sch-catchup", "")
	agent := schSeedAgent(t, ctx, s, ws, "atlas")
	st := s.Schedulers()
	now := time.Now().UTC().Truncate(time.Second)

	sched := &domain.Scheduler{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Name:        "nightly",
		Prompt:      "p",
		Kind:        domain.SchedulerKindRecurring,
		Expr:        "0 3 * * *",
		Enabled:     true,
	}
	if err := st.CreateScheduler(ctx, ws.ID, sched); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	// Simulate server downtime: the stored occurrence is several hours old.
	schBackdate(t, ctx, schemaDSN, sched.Name, now.Add(-6*time.Hour))

	// Startup recovery fires exactly once and lands the next occurrence in
	// the future — missed occurrences are never replayed back-to-back.
	claims, err := st.ClaimDueSchedulers(ctx, now, 10)
	if err != nil {
		t.Fatalf("unexpected claim error: %v", err)
	}
	if len(claims) != 1 || claims[0].Scheduler.ID != sched.ID {
		t.Fatalf("expected one catch-up claim, got %+v", claims)
	}
	if claims[0].Scheduler.NextRunAt == nil || !claims[0].Scheduler.NextRunAt.After(now) {
		t.Fatalf("expected future next_run_at after catch-up, got %v", claims[0].Scheduler.NextRunAt)
	}
	if claims[0].Scheduler.NextRunAt.Sub(now) > 24*time.Hour {
		t.Fatalf("expected the next single occurrence, not a burst, got %v", claims[0].Scheduler.NextRunAt)
	}

	// A second overlapping tick claims nothing.
	claims, err = st.ClaimDueSchedulers(ctx, now, 10)
	if err != nil || len(claims) != 0 {
		t.Fatalf("expected no double fire, got %v (%v)", claims, err)
	}
}

func TestIntegration_SchedulerStore_MissedOnceGraceWindow(t *testing.T) {
	s, schemaDSN, ctx := setupTestSchema(t)
	ws := schSeedWorkspace(t, ctx, s, "sch-missed", "")
	agent := schSeedAgent(t, ctx, s, ws, "atlas")
	st := s.Schedulers()
	now := time.Now().UTC().Truncate(time.Second)

	// Create demands a future instant, so the backdate to -2h (beyond the 1h
	// grace window) happens through raw SQL afterwards.
	farFuture := now.Add(2 * time.Hour)
	stale := &domain.Scheduler{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Name:        "stale-reminder",
		Prompt:      "p",
		Kind:        domain.SchedulerKindOnce,
		RunAt:       &farFuture,
		Enabled:     true,
	}
	if err := st.CreateScheduler(ctx, ws.ID, stale); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	// The reminder's instant passed two hours ago while the server was down
	// — beyond the 1h grace window.
	schBackdateRunAt(t, ctx, schemaDSN, stale.Name, now.Add(-2*time.Hour))

	claims, err := st.ClaimDueSchedulers(ctx, now, 10)
	if err != nil {
		t.Fatalf("unexpected claim error: %v", err)
	}
	if len(claims) != 1 || !claims[0].Missed || claims[0].Scheduler.ID != stale.ID {
		t.Fatalf("expected the stale once scheduler claimed as missed, got %+v", claims)
	}
	claimed := claims[0].Scheduler
	if claimed.Enabled || claimed.NextRunAt != nil {
		t.Fatalf("expected archived scheduler, got %+v", claimed)
	}
	if claimed.LastRun == nil ||
		claimed.LastRun.Status != domain.SchedulerRunStatusMissed ||
		claimed.LastRun.Trigger != domain.SchedulerTriggerScheduled {
		t.Fatalf("expected last_run status missed, got %+v", claimed.LastRun)
	}

	// Persisted state: archived with a missed last_run.
	got, err := st.GetScheduler(ctx, ws.ID, stale.ID)
	if err != nil || got == nil {
		t.Fatalf("unexpected get result: (%v, %v)", got, err)
	}
	if got.Enabled || got.NextRunAt != nil || got.LastRun == nil || got.LastRun.Status != domain.SchedulerRunStatusMissed {
		t.Fatalf("expected persisted archived row, got %+v", got)
	}

	// Inside the grace window the once scheduler fires and archives instead.
	// Create demands a future instant, so the backdate to -1min (inside the
	// 1h grace window) happens through raw SQL afterwards.
	future := now.Add(time.Hour)
	inside := &domain.Scheduler{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Name:        "fresh-reminder",
		Prompt:      "p",
		Kind:        domain.SchedulerKindOnce,
		RunAt:       &future,
		Enabled:     true,
	}
	if err := st.CreateScheduler(ctx, ws.ID, inside); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	schBackdateRunAt(t, ctx, schemaDSN, inside.Name, now.Add(-time.Minute))
	claims, err = st.ClaimDueSchedulers(ctx, now, 10)
	if err != nil {
		t.Fatalf("unexpected claim error: %v", err)
	}
	if len(claims) != 1 || claims[0].Missed || claims[0].Scheduler.ID != inside.ID {
		t.Fatalf("expected the fresh once scheduler fired (not missed), got %+v", claims)
	}
	if claims[0].Scheduler.Enabled || claims[0].Scheduler.NextRunAt != nil {
		t.Fatalf("expected once scheduler archived after firing, got %+v", claims[0].Scheduler)
	}
}

func TestIntegration_SchedulerStore_RunRecords(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := schSeedWorkspace(t, ctx, s, "sch-runs", "")
	agent := schSeedAgent(t, ctx, s, ws, "atlas")
	other := schSeedWorkspace(t, ctx, s, "sch-runs-other", "")
	st := s.Schedulers()

	sched := &domain.Scheduler{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Name:        "digest",
		Prompt:      "p",
		Kind:        domain.SchedulerKindRecurring,
		Expr:        "0 9 * * *",
		Enabled:     true,
	}
	if err := st.CreateScheduler(ctx, ws.ID, sched); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	// StartSchedulerRun inserts with status running and assigns identity.
	base := time.Now().UTC().Add(-10 * time.Minute)
	runIDs := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		run := &domain.SchedulerRun{
			WorkspaceID: ws.ID,
			SchedulerID: sched.ID,
			SessionID:   fmt.Sprintf("sched_%s_%d", sched.ID, base.Add(time.Duration(i)*time.Minute).Unix()),
			Trigger:     domain.SchedulerTriggerScheduled,
			StartedAt:   base.Add(time.Duration(i) * time.Minute),
		}
		if err := st.StartSchedulerRun(ctx, run); err != nil {
			t.Fatalf("unexpected start run error: %v", err)
		}
		if run.ID == "" || run.Status != domain.SchedulerRunStatusRunning {
			t.Fatalf("expected running run with id, got %+v", run)
		}
		runIDs = append(runIDs, run.ID)
	}

	// The scheduler must exist within the run's workspace.
	crossRun := &domain.SchedulerRun{
		WorkspaceID: other.ID,
		SchedulerID: sched.ID,
		SessionID:   "sched_cross",
		Trigger:     domain.SchedulerTriggerManual,
		StartedAt:   base,
	}
	if err := st.StartSchedulerRun(ctx, crossRun); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-workspace run, got %v", err)
	}

	// FinishSchedulerRun writes the outcome and mirrors last_run atomically.
	if err := st.FinishSchedulerRun(ctx, ws.ID, runIDs[1], domain.SchedulerRunStatusCompleted, 1500, 42, domain.SchedulerDeliveryDelivered, ""); err != nil {
		t.Fatalf("unexpected finish error: %v", err)
	}
	got, _ := st.GetScheduler(ctx, ws.ID, sched.ID)
	if got.LastRun == nil {
		t.Fatal("expected last_run mirrored after finish")
	}
	if got.LastRun.Status != domain.SchedulerRunStatusCompleted ||
		got.LastRun.DurationMS != 1500 ||
		got.LastRun.TokensUsed != 42 ||
		got.LastRun.DeliveryStatus != domain.SchedulerDeliveryDelivered ||
		got.LastRun.Trigger != domain.SchedulerTriggerScheduled ||
		got.LastRun.SessionID == "" {
		t.Fatalf("unexpected mirrored last_run: %+v", got.LastRun)
	}

	// Finishing an unknown run is NotFound.
	if err := st.FinishSchedulerRun(ctx, ws.ID, "00000000-0000-0000-0000-000000000003", domain.SchedulerRunStatusFailed, 0, 0, "", "boom"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound finishing unknown run, got %v", err)
	}

	// Listing is newest-first with a total across pages.
	runs, total, err := st.ListSchedulerRuns(ctx, ws.ID, sched.ID, 2, 0)
	if err != nil {
		t.Fatalf("unexpected list runs error: %v", err)
	}
	if total != 3 || len(runs) != 2 {
		t.Fatalf("expected page of 2 with total 3, got %d/%d", len(runs), total)
	}
	if !runs[0].StartedAt.After(runs[1].StartedAt) {
		t.Fatalf("expected newest-first ordering, got %v then %v", runs[0].StartedAt, runs[1].StartedAt)
	}
	page2, total2, err := st.ListSchedulerRuns(ctx, ws.ID, sched.ID, 2, 2)
	if err != nil || total2 != 3 || len(page2) != 1 {
		t.Fatalf("expected offset page of 1 with total 3, got %d/%d (%v)", len(page2), total2, err)
	}

	// Workspace isolation: foreign scopes see nothing.
	foreignRuns, foreignTotal, err := st.ListSchedulerRuns(ctx, other.ID, sched.ID, 10, 0)
	if err != nil || foreignTotal != 0 || len(foreignRuns) != 0 {
		t.Fatalf("expected empty foreign run listing, got %d/%d (%v)", len(foreignRuns), foreignTotal, err)
	}
}

func TestIntegration_SchedulerStore_AgentDeleteCascades(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := schSeedWorkspace(t, ctx, s, "sch-cascade", "")
	agent := schSeedAgent(t, ctx, s, ws, "doomed")
	st := s.Schedulers()

	sched := &domain.Scheduler{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Name:        "doomed-schedule",
		Prompt:      "p",
		Kind:        domain.SchedulerKindRecurring,
		Expr:        "0 9 * * *",
		Enabled:     true,
	}
	if err := st.CreateScheduler(ctx, ws.ID, sched); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	run := &domain.SchedulerRun{
		WorkspaceID: ws.ID,
		SchedulerID: sched.ID,
		SessionID:   "sched_" + sched.ID + "_1",
		Trigger:     domain.SchedulerTriggerManual,
		StartedAt:   time.Now().UTC(),
	}
	if err := st.StartSchedulerRun(ctx, run); err != nil {
		t.Fatalf("unexpected start run error: %v", err)
	}

	if err := s.Agents().Delete(ctx, ws.ID, agent.ID); err != nil {
		t.Fatalf("unexpected agent delete error: %v", err)
	}
	got, err := st.GetScheduler(ctx, ws.ID, sched.ID)
	if err != nil || got != nil {
		t.Fatalf("expected scheduler cascaded away, got (%v, %v)", got, err)
	}
	_, total, err := st.ListSchedulerRuns(ctx, ws.ID, sched.ID, 10, 0)
	if err != nil || total != 0 {
		t.Fatalf("expected run records cascaded away, got total %d (%v)", total, err)
	}
}
