//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// hbPut validates and stores a heartbeat with the given next tick — the
// caller-side contract the service layer implements (derivation never rides
// the store).
func hbPut(t *testing.T, ctx context.Context, s store.Store, wsID, agentID string, hb *domain.Heartbeat, next *time.Time) {
	t.Helper()
	if err := domain.ValidateHeartbeat(hb, time.Now().UTC()); err != nil {
		t.Fatalf("validate heartbeat: %v", err)
	}
	hb.NextTickAt = next
	if err := s.Heartbeats().PutHeartbeat(ctx, wsID, agentID, hb); err != nil {
		t.Fatalf("put heartbeat: %v", err)
	}
}

func TestIntegration_HeartbeatStore_PutGet(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := schSeedWorkspace(t, ctx, s, "hb-put", "")
	agent := schSeedAgent(t, ctx, s, ws, "atlas")
	user := &domain.User{Email: "hb-put@example.com", Name: "Creator"}
	if err := s.Users().Create(ctx, user); err != nil {
		t.Fatalf("unexpected create user error: %v", err)
	}
	st := s.Heartbeats()
	now := time.Now().UTC()

	// 1. Absence is (nil, nil), not an error.
	absent, err := st.GetHeartbeat(ctx, ws.ID, agent.ID)
	if err != nil || absent != nil {
		t.Fatalf("expected (nil, nil) for absent heartbeat, got (%v, %v)", absent, err)
	}

	// 2. Put with derived next tick: stored as given, identity assigned.
	next := now.Add(time.Hour)
	hb := &domain.Heartbeat{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		CreatedBy:   &user.ID,
		Prompt:      "Check the overnight deploy",
		Expr:        "*/15 * * * *",
		Delivery:    domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryCreatorDM},
		Enabled:     true,
	}
	hbPut(t, ctx, s, ws.ID, agent.ID, hb, &next)
	if hb.ID == "" || hb.CreatedAt.IsZero() {
		t.Fatalf("expected id and timestamps assigned, got %+v", hb)
	}

	got, err := st.GetHeartbeat(ctx, ws.ID, agent.ID)
	if err != nil || got == nil {
		t.Fatalf("unexpected get result: (%v, %v)", got, err)
	}
	if got.ID != hb.ID || got.Expr != "*/15 * * * *" || got.Prompt != "Check the overnight deploy" {
		t.Fatalf("unexpected roundtripped heartbeat: %+v", got)
	}
	if !got.Enabled || got.NextTickAt == nil || !got.NextTickAt.Equal(next) {
		t.Fatalf("expected enabled heartbeat with the given next tick, got %+v", got)
	}
	if got.Delivery.Type != domain.HeartbeatDeliveryCreatorDM || got.FailureStreak != 0 || got.LastTick != nil {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	if got.CreatedBy == nil || *got.CreatedBy != user.ID {
		t.Fatalf("expected created_by preserved, got %+v", got.CreatedBy)
	}

	// 3. Active hours and channel delivery survive the jsonb/nullable round
	// trip; an empty clock string reads back as NULL (24/7).
	ch := &domain.Heartbeat{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Prompt:      "p",
		Expr:        "0 9 * * *",
		Delivery:    domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryChannel, ChannelID: "ch-1"},
		Enabled:     false,
	}
	chStart, chEnd := "08:00", "22:00"
	ch.ActiveStart, ch.ActiveEnd = &chStart, &chEnd
	hbPut(t, ctx, s, ws.ID, agent.ID, ch, nil)
	got, err = st.GetHeartbeat(ctx, ws.ID, agent.ID)
	if err != nil || got == nil {
		t.Fatalf("unexpected get result: (%v, %v)", got, err)
	}
	if got.Delivery.Type != domain.HeartbeatDeliveryChannel || got.Delivery.ChannelID != "ch-1" {
		t.Fatalf("expected channel delivery preserved, got %+v", got.Delivery)
	}
	if got.ActiveStart == nil || got.ActiveEnd == nil || *got.ActiveStart != "08:00" || *got.ActiveEnd != "22:00" {
		t.Fatalf("expected active hours preserved, got %v–%v", got.ActiveStart, got.ActiveEnd)
	}

	// 4. Unknown workspace / agent references fail as NotFound (FK parity).
	orphan := &domain.Heartbeat{AgentID: "00000000-0000-0000-0000-000000000000", Expr: "0 9 * * *"}
	if err := s.Heartbeats().PutHeartbeat(ctx, ws.ID, orphan.AgentID, orphan); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown agent, got %v", err)
	}
	if err := s.Heartbeats().PutHeartbeat(ctx, "00000000-0000-0000-0000-000000000001", agent.ID, &domain.Heartbeat{Expr: "0 9 * * *"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}

	// 5. The default CHECK guards the delivery shape at the boundary: an
	// unvalidated raw type is rejected by chk_agent_heartbeats_delivery_type.
	if err := s.Heartbeats().PutHeartbeat(ctx, ws.ID, agent.ID, &domain.Heartbeat{Expr: "0 9 * * *", Delivery: domain.HeartbeatDelivery{Type: "email"}}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for unknown delivery type, got %v", err)
	}
}

func TestIntegration_HeartbeatStore_ExactlyOnePerAgent(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := schSeedWorkspace(t, ctx, s, "hb-one", "")
	agent := schSeedAgent(t, ctx, s, ws, "atlas")
	user := &domain.User{Email: "hb-one@example.com", Name: "Creator"}
	if err := s.Users().Create(ctx, user); err != nil {
		t.Fatalf("unexpected create user error: %v", err)
	}
	st := s.Heartbeats()

	first := &domain.Heartbeat{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		CreatedBy:   &user.ID,
		Prompt:      "v1",
		Expr:        "*/15 * * * *",
		Delivery:    domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryCreatorDM},
		Enabled:     true,
	}
	hbPut(t, ctx, s, ws.ID, agent.ID, first, nil)

	// Replace: same row identity, replaced config and runtime state.
	second := &domain.Heartbeat{
		Prompt:        "v2",
		Expr:          "0 9 * * *",
		Delivery:      domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryCreatorDM},
		Enabled:       false,
		FailureStreak: 3,
		LastTick:      &domain.HeartbeatLastRun{Status: domain.HeartbeatRunStatusCompleted, SessionID: "hb_x"},
	}
	hbPut(t, ctx, s, ws.ID, agent.ID, second, nil)

	got, err := st.GetHeartbeat(ctx, ws.ID, agent.ID)
	if err != nil || got == nil {
		t.Fatalf("unexpected get result: (%v, %v)", got, err)
	}
	if got.ID != first.ID {
		t.Fatalf("expected replace in place (id %q), got new id %q", first.ID, got.ID)
	}
	if !got.CreatedAt.Equal(first.CreatedAt) || got.CreatedBy == nil || *got.CreatedBy != user.ID {
		t.Fatalf("expected immutable identity, got %+v", got)
	}
	if got.Prompt != "v2" || got.Expr != "0 9 * * *" || got.Enabled {
		t.Fatalf("expected replaced config, got %+v", got)
	}
	if got.FailureStreak != 3 || got.LastTick == nil || got.LastTick.Status != domain.HeartbeatRunStatusCompleted {
		t.Fatalf("expected runtime state persisted as given, got %+v", got)
	}

	// Delete removes the row; a second delete is NotFound; a fresh put
	// recreates one.
	if err := st.DeleteHeartbeat(ctx, ws.ID, agent.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	got, err = st.GetHeartbeat(ctx, ws.ID, agent.ID)
	if err != nil || got != nil {
		t.Fatalf("expected absent after delete, got (%v, %v)", got, err)
	}
	if err := st.DeleteHeartbeat(ctx, ws.ID, agent.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on double delete, got %v", err)
	}
	again := &domain.Heartbeat{Expr: "0 9 * * *", Delivery: domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryCreatorDM}, Enabled: true}
	hbPut(t, ctx, s, ws.ID, agent.ID, again, nil)
	if again.ID == "" || again.ID == first.ID {
		t.Fatalf("expected fresh identity on recreate, got %q", again.ID)
	}
}

func TestIntegration_HeartbeatStore_ClaimFiresOnceAndAdvances(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := schSeedWorkspace(t, ctx, s, "hb-claim", "")
	st := s.Heartbeats()
	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-time.Minute)
	futureAt := now.Add(time.Hour)

	// Exactly one heartbeat per agent, so each claim state gets its own
	// agent: one due enabled, one disabled, one future.
	dueAgent := schSeedAgent(t, ctx, s, ws, "due")
	disabledAgent := schSeedAgent(t, ctx, s, ws, "paused")
	futureAgent := schSeedAgent(t, ctx, s, ws, "future")

	due := &domain.Heartbeat{Prompt: "due", Expr: "*/15 * * * *", Delivery: domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryCreatorDM}, Enabled: true}
	hbPut(t, ctx, s, ws.ID, dueAgent.ID, due, &past)
	disabled := &domain.Heartbeat{Prompt: "paused", Expr: "*/15 * * * *", Delivery: domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryCreatorDM}, Enabled: false}
	hbPut(t, ctx, s, ws.ID, disabledAgent.ID, disabled, &past)
	future := &domain.Heartbeat{Prompt: "future", Expr: "*/15 * * * *", Delivery: domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryCreatorDM}, Enabled: true}
	hbPut(t, ctx, s, ws.ID, futureAgent.ID, future, &futureAt)

	claims, err := st.ClaimDueHeartbeats(ctx, now, 10)
	if err != nil {
		t.Fatalf("unexpected claim error: %v", err)
	}
	if len(claims) != 1 || claims[0].Heartbeat.ID != due.ID {
		t.Fatalf("expected only the due enabled heartbeat claimed, got %+v", claims)
	}
	claimed := claims[0].Heartbeat
	want, err := domain.NextRun(claimed.Expr, now, time.UTC)
	if err != nil {
		t.Fatalf("derive expected next: %v", err)
	}
	if claimed.NextTickAt == nil || !claimed.NextTickAt.Equal(*want) || !claimed.NextTickAt.After(now) {
		t.Fatalf("expected next_tick_at advanced to %v, got %v", want, claimed.NextTickAt)
	}

	// The consumed occurrence cannot be claimed again.
	claims, err = st.ClaimDueHeartbeats(ctx, now, 10)
	if err != nil || len(claims) != 0 {
		t.Fatalf("expected empty follow-up claim, got %v (%v)", claims, err)
	}

	// Persisted state carries the advance.
	got, err := st.GetHeartbeat(ctx, ws.ID, dueAgent.ID)
	if err != nil || got == nil || got.ID != due.ID {
		t.Fatalf("unexpected get: (%v, %v)", got, err)
	}
	if got.NextTickAt == nil || !got.NextTickAt.Equal(*want) {
		t.Fatalf("expected persisted advance to %v, got %v", want, got.NextTickAt)
	}
}

func TestIntegration_HeartbeatStore_ClaimExactlyOnceUnderConcurrency(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := schSeedWorkspace(t, ctx, s, "hb-race", "")
	agent := schSeedAgent(t, ctx, s, ws, "atlas")
	st := s.Heartbeats()
	now := time.Now().UTC().Truncate(time.Second)

	hb := &domain.Heartbeat{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Prompt:      "p",
		Expr:        "*/15 * * * *",
		Delivery:    domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryCreatorDM},
		Enabled:     true,
	}
	past := now.Add(-time.Minute)
	hbPut(t, ctx, s, ws.ID, agent.ID, hb, &past)

	// Two simultaneous claimers race over the same due row: SKIP LOCKED must
	// partition them so the tick is claimed exactly once (add-agent-heartbeat
	// D14 "two ticks do not double-fire").
	const claimers = 2
	barrier := make(chan struct{})
	var wg sync.WaitGroup
	results := make([][]store.HeartbeatClaim, claimers)
	errs := make([]error, claimers)
	for i := 0; i < claimers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-barrier
			results[idx], errs[idx] = st.ClaimDueHeartbeats(ctx, now, 10)
		}(i)
	}
	close(barrier)
	wg.Wait()

	total := 0
	for i := 0; i < claimers; i++ {
		if errs[i] != nil {
			t.Fatalf("claimer %d failed: %v", i, errs[i])
		}
		for _, c := range results[i] {
			total++
			if c.Heartbeat.ID != hb.ID {
				t.Fatalf("claimed unexpected heartbeat: %+v", c.Heartbeat)
			}
			if c.Heartbeat.NextTickAt == nil || !c.Heartbeat.NextTickAt.After(now) {
				t.Fatalf("expected claimed row advanced into the future, got %+v", c.Heartbeat)
			}
		}
	}
	if total != 1 {
		t.Fatalf("expected exactly 1 claim across claimers, got %d", total)
	}
}

func TestIntegration_HeartbeatStore_ClaimAdvancesInWorkspaceTimezone(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := schSeedWorkspace(t, ctx, s, "hb-tz", "Asia/Jakarta")
	agent := schSeedAgent(t, ctx, s, ws, "beacon")
	st := s.Heartbeats()

	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	// 02:30 UTC = 09:30 Jakarta; the 09:00 Jakarta slot already passed, so
	// the claim advances to tomorrow's 09:00 Jakarta (02:00 UTC).
	now := time.Date(2026, time.September, 10, 2, 30, 0, 0, time.UTC)
	hb := &domain.Heartbeat{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Prompt:      "p",
		Expr:        "0 9 * * *",
		Delivery:    domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryCreatorDM},
		Enabled:     true,
	}
	past := now.Add(-time.Hour)
	hbPut(t, ctx, s, ws.ID, agent.ID, hb, &past)

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

func TestIntegration_HeartbeatStore_ApplyOutcome(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := schSeedWorkspace(t, ctx, s, "hb-streak", "")
	agent := schSeedAgent(t, ctx, s, ws, "atlas")
	st := s.Heartbeats()
	now := time.Now().UTC()

	hb := &domain.Heartbeat{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Prompt:      "p",
		Expr:        "*/15 * * * *",
		Delivery:    domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryCreatorDM},
		Enabled:     true,
	}
	past := now.Add(-time.Minute)
	hbPut(t, ctx, s, ws.ID, agent.ID, hb, &past)

	// Blocked leaves the streak untouched (policy grief, not failure).
	paused, err := st.ApplyHeartbeatOutcome(ctx, ws.ID, hb.ID, domain.HeartbeatRunStatusBlocked)
	if err != nil || paused {
		t.Fatalf("expected blocked to be a no-op, got paused=%v err=%v", paused, err)
	}
	if _, err := st.ApplyHeartbeatOutcome(ctx, ws.ID, hb.ID, domain.HeartbeatRunStatusSkipped); err != nil {
		t.Fatalf("apply skipped: %v", err)
	}
	got, _ := st.GetHeartbeat(ctx, ws.ID, agent.ID)
	if got.FailureStreak != 0 || !got.Enabled || got.NextTickAt == nil {
		t.Fatalf("expected guard statuses to leave the row untouched, got %+v", got)
	}

	// One failure increments; completed resets.
	if _, err := st.ApplyHeartbeatOutcome(ctx, ws.ID, hb.ID, domain.HeartbeatRunStatusFailed); err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if _, err := st.ApplyHeartbeatOutcome(ctx, ws.ID, hb.ID, domain.HeartbeatRunStatusCompleted); err != nil {
		t.Fatalf("apply completed: %v", err)
	}
	got, _ = st.GetHeartbeat(ctx, ws.ID, agent.ID)
	if got.FailureStreak != 0 {
		t.Fatalf("expected streak reset by completed, got %d", got.FailureStreak)
	}

	// Five consecutive failures auto-pause (add-agent-heartbeat D12).
	for i := 0; i < 4; i++ {
		paused, err := st.ApplyHeartbeatOutcome(ctx, ws.ID, hb.ID, domain.HeartbeatRunStatusFailed)
		if err != nil || paused {
			t.Fatalf("failure %d: expected no pause yet, got paused=%v err=%v", i+1, paused, err)
		}
	}
	paused, err = st.ApplyHeartbeatOutcome(ctx, ws.ID, hb.ID, domain.HeartbeatRunStatusFailed)
	if err != nil || !paused {
		t.Fatalf("fifth failure expected paused=true, got paused=%v err=%v", paused, err)
	}
	got, _ = st.GetHeartbeat(ctx, ws.ID, agent.ID)
	if got.Enabled || got.NextTickAt != nil || got.FailureStreak != 5 {
		t.Fatalf("expected auto-paused heartbeat with streak retained, got %+v", got)
	}

	// A paused heartbeat is never claimed again.
	claims, err := st.ClaimDueHeartbeats(ctx, now.Add(time.Hour), 10)
	if err != nil || len(claims) != 0 {
		t.Fatalf("expected paused heartbeat to stay quiet, got %v (%v)", claims, err)
	}

	// Unknown / foreign-scoped heartbeat is NotFound.
	if _, err := st.ApplyHeartbeatOutcome(ctx, ws.ID, "00000000-0000-0000-0000-000000000009", domain.HeartbeatRunStatusFailed); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown heartbeat, got %v", err)
	}
}

func TestIntegration_HeartbeatStore_RunRecords(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := schSeedWorkspace(t, ctx, s, "hb-runs", "")
	agent := schSeedAgent(t, ctx, s, ws, "atlas")
	other := schSeedWorkspace(t, ctx, s, "hb-runs-other", "")
	st := s.Heartbeats()

	hb := &domain.Heartbeat{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Prompt:      "p",
		Expr:        "*/15 * * * *",
		Delivery:    domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryCreatorDM},
		Enabled:     true,
	}
	hbPut(t, ctx, s, ws.ID, agent.ID, hb, nil)
	sessionID := "hb_" + agent.ID

	// StartHeartbeatRun inserts with status running and assigns identity.
	base := time.Now().UTC().Add(-10 * time.Minute)
	runIDs := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		run := &domain.HeartbeatRun{
			WorkspaceID: ws.ID,
			HeartbeatID: hb.ID,
			AgentID:     agent.ID,
			SessionID:   sessionID,
			Trigger:     domain.HeartbeatTriggerTick,
			StartedAt:   base.Add(time.Duration(i) * time.Minute),
		}
		if err := st.StartHeartbeatRun(ctx, run); err != nil {
			t.Fatalf("unexpected start run error: %v", err)
		}
		if run.ID == "" || run.Status != domain.HeartbeatRunStatusRunning {
			t.Fatalf("expected running run with id, got %+v", run)
		}
		runIDs = append(runIDs, run.ID)
	}

	// The heartbeat must exist within the run's workspace.
	crossRun := &domain.HeartbeatRun{
		WorkspaceID: other.ID,
		HeartbeatID: hb.ID,
		SessionID:   sessionID,
		Trigger:     domain.HeartbeatTriggerManual,
		StartedAt:   base,
	}
	if err := st.StartHeartbeatRun(ctx, crossRun); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-workspace run, got %v", err)
	}

	// FinishHeartbeatRun writes the outcome and mirrors last_tick atomically.
	if err := st.FinishHeartbeatRun(ctx, ws.ID, runIDs[1], domain.HeartbeatRunStatusCompleted, 1500, 42, domain.HeartbeatDeliveryStatusSuppressed, "", "tr-hb-1"); err != nil {
		t.Fatalf("unexpected finish error: %v", err)
	}
	got, _ := st.GetHeartbeat(ctx, ws.ID, agent.ID)
	if got.LastTick == nil {
		t.Fatal("expected last_tick mirrored after finish")
	}
	if got.LastTick.Status != domain.HeartbeatRunStatusCompleted ||
		got.LastTick.DurationMS != 1500 ||
		got.LastTick.TokensUsed != 42 ||
		got.LastTick.DeliveryStatus != domain.HeartbeatDeliveryStatusSuppressed ||
		got.LastTick.Trigger != domain.HeartbeatTriggerTick ||
		got.LastTick.SessionID != sessionID {
		t.Fatalf("unexpected mirrored last_tick: %+v", got.LastTick)
	}

	// The finished row carries its trace id; unfinished rows stay empty
	// (000050 precedent).
	listed, _, err := st.ListHeartbeatRuns(ctx, ws.ID, hb.ID, 10, 0)
	if err != nil {
		t.Fatalf("unexpected list runs error: %v", err)
	}
	traced := false
	for _, r := range listed {
		if r.ID == runIDs[1] {
			if r.TraceID != "tr-hb-1" {
				t.Fatalf("expected the finished run to carry its trace id, got %q", r.TraceID)
			}
			traced = true
		} else if r.TraceID != "" {
			t.Fatalf("unfinished run must carry no trace id, got %q", r.TraceID)
		}
	}
	if !traced {
		t.Fatal("finished run missing from the listing")
	}

	// Finishing an unknown run is NotFound.
	if err := st.FinishHeartbeatRun(ctx, ws.ID, "00000000-0000-0000-0000-000000000003", domain.HeartbeatRunStatusFailed, 0, 0, "", "boom", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound finishing unknown run, got %v", err)
	}

	// Listing is newest-first with a total across pages.
	runs, total, err := st.ListHeartbeatRuns(ctx, ws.ID, hb.ID, 2, 0)
	if err != nil {
		t.Fatalf("unexpected list runs error: %v", err)
	}
	if total != 3 || len(runs) != 2 {
		t.Fatalf("expected page of 2 with total 3, got %d/%d", len(runs), total)
	}
	if !runs[0].StartedAt.After(runs[1].StartedAt) {
		t.Fatalf("expected newest-first ordering, got %v then %v", runs[0].StartedAt, runs[1].StartedAt)
	}
	page2, total2, err := st.ListHeartbeatRuns(ctx, ws.ID, hb.ID, 2, 2)
	if err != nil || total2 != 3 || len(page2) != 1 {
		t.Fatalf("expected offset page of 1 with total 3, got %d/%d (%v)", len(page2), total2, err)
	}

	// Workspace isolation: foreign scopes see nothing.
	foreignRuns, foreignTotal, err := st.ListHeartbeatRuns(ctx, other.ID, hb.ID, 10, 0)
	if err != nil || foreignTotal != 0 || len(foreignRuns) != 0 {
		t.Fatalf("expected empty foreign run listing, got %d/%d (%v)", len(foreignRuns), foreignTotal, err)
	}
}

func TestIntegration_HeartbeatStore_AgentDeleteCascades(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := schSeedWorkspace(t, ctx, s, "hb-cascade", "")
	agent := schSeedAgent(t, ctx, s, ws, "doomed")
	st := s.Heartbeats()

	hb := &domain.Heartbeat{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		Prompt:      "p",
		Expr:        "*/15 * * * *",
		Delivery:    domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryCreatorDM},
		Enabled:     true,
	}
	hbPut(t, ctx, s, ws.ID, agent.ID, hb, nil)
	run := &domain.HeartbeatRun{
		WorkspaceID: ws.ID,
		HeartbeatID: hb.ID,
		AgentID:     agent.ID,
		SessionID:   "hb_" + agent.ID,
		Trigger:     domain.HeartbeatTriggerManual,
		StartedAt:   time.Now().UTC(),
	}
	if err := st.StartHeartbeatRun(ctx, run); err != nil {
		t.Fatalf("unexpected start run error: %v", err)
	}

	if err := s.Agents().Delete(ctx, ws.ID, agent.ID); err != nil {
		t.Fatalf("unexpected agent delete error: %v", err)
	}
	got, err := st.GetHeartbeat(ctx, ws.ID, agent.ID)
	if err != nil || got != nil {
		t.Fatalf("expected heartbeat cascaded away, got (%v, %v)", got, err)
	}
	_, total, err := st.ListHeartbeatRuns(ctx, ws.ID, hb.ID, 10, 0)
	if err != nil || total != 0 {
		t.Fatalf("expected tick records cascaded away, got total %d (%v)", total, err)
	}
}
