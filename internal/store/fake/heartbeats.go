package fake

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// -------------------------------------------------------------------------
// HeartbeatStore implementation (add-agent-heartbeat 2.1). Mirrors the
// postgres adapter: due claims are evaluated under the store mutex (the
// fake's atomicity seam — the postgres adapter uses FOR UPDATE SKIP LOCKED)
// and next_tick_at is always recomputed from the workspace timezone — never
// trusted from the input struct.
// -------------------------------------------------------------------------

// heartbeatAutoPauseStreak is the failure-streak length at which a heartbeat
// auto-pauses (add-agent-heartbeat D12); kept in lockstep with the postgres
// adapter's constant.
const heartbeatAutoPauseStreak = 5

type heartbeatStore struct {
	s *fakeStore
}

func cloneHeartbeat(hb *domain.Heartbeat) *domain.Heartbeat {
	if hb == nil {
		return nil
	}
	cp := *hb
	if hb.CreatedBy != nil {
		cb := *hb.CreatedBy
		cp.CreatedBy = &cb
	}
	if hb.ActiveStart != nil {
		s := *hb.ActiveStart
		cp.ActiveStart = &s
	}
	if hb.ActiveEnd != nil {
		e := *hb.ActiveEnd
		cp.ActiveEnd = &e
	}
	if hb.NextTickAt != nil {
		t := *hb.NextTickAt
		cp.NextTickAt = &t
	}
	if hb.LastTick != nil {
		lt := *hb.LastTick
		cp.LastTick = &lt
	}
	return &cp
}

func cloneHeartbeatRun(r *domain.HeartbeatRun) *domain.HeartbeatRun {
	if r == nil {
		return nil
	}
	cp := *r
	return &cp
}

// heartbeatAgentKey is the fake's exactly-one-per-agent uniqueness key,
// mirroring the schema's UNIQUE (workspace_id, agent_id) pair.
func heartbeatAgentKey(workspaceID, agentID string) string {
	return workspaceID + ":" + agentID
}

// workspaceLocation resolves the workspace's IANA timezone for tick
// interpretation, falling back to UTC for the impossible case of a missing or
// unparseable stored zone (workspace timezones are IANA-validated at the
// domain layer).
func (hs *heartbeatStore) workspaceLocationLocked(workspaceID string) *time.Location {
	if w, ok := hs.s.workspaces[workspaceID]; ok {
		if loc, err := time.LoadLocation(w.Timezone); err == nil {
			return loc
		}
	}
	return time.UTC
}

// GetHeartbeat returns the agent's heartbeat; absence — including a foreign
// workspace's agent — reads as (nil, nil).
func (hs *heartbeatStore) GetHeartbeat(ctx context.Context, workspaceID, agentID string) (*domain.Heartbeat, error) {
	if workspaceID == "" || agentID == "" {
		return nil, nil
	}

	hs.s.mu.RLock()
	defer hs.s.mu.RUnlock()

	id, exists := hs.s.heartbeatsByAgent[heartbeatAgentKey(workspaceID, agentID)]
	if !exists {
		return nil, nil
	}
	return cloneHeartbeat(hs.s.heartbeats[id]), nil
}

// PutHeartbeat create-or-replaces the agent's single heartbeat (design D1:
// exactly one per agent — the byAgent map key enforces it naturally). The
// store persists the struct as given; derivation is caller-side. Identity
// fields (id, created_by, created_at) are immutable on replace and written
// back so the caller's struct stays faithful.
func (hs *heartbeatStore) PutHeartbeat(ctx context.Context, workspaceID, agentID string, hb *domain.Heartbeat) error {
	if hb == nil || workspaceID == "" || agentID == "" {
		return domain.ErrInvalid
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	// FK parity: workspace, agent, and creator must exist.
	if _, exists := hs.s.workspaces[workspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	agent, exists := hs.s.agents[agentID]
	if !exists || agent.WorkspaceID != workspaceID {
		return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
	}
	if hb.CreatedBy != nil && *hb.CreatedBy != "" {
		if _, exists := hs.s.users[*hb.CreatedBy]; !exists {
			return fmt.Errorf("%w: user not found", domain.ErrNotFound)
		}
	}

	now := time.Now().UTC()
	key := heartbeatAgentKey(workspaceID, agentID)
	if existingID, exists := hs.s.heartbeatsByAgent[key]; exists {
		// Replace in place: the row keeps its identity.
		existing := hs.s.heartbeats[existingID]
		hb.ID = existing.ID
		hb.CreatedBy = existing.CreatedBy
		hb.CreatedAt = existing.CreatedAt
		hb.UpdatedAt = now
		hb.WorkspaceID = workspaceID
		hb.AgentID = agentID
		hs.s.heartbeats[existingID] = cloneHeartbeat(hb)
		return nil
	}

	hb.WorkspaceID = workspaceID
	hb.AgentID = agentID
	if hb.ID != "" {
		if _, exists := hs.s.heartbeats[hb.ID]; exists {
			return fmt.Errorf("%w: heartbeat with id %q already exists", domain.ErrConflict, hb.ID)
		}
	} else {
		hb.ID = uuid.NewString()
	}
	if hb.CreatedAt.IsZero() {
		hb.CreatedAt = now
	}
	if hb.UpdatedAt.IsZero() {
		hb.UpdatedAt = now
	}
	hs.s.heartbeats[hb.ID] = cloneHeartbeat(hb)
	hs.s.heartbeatsByAgent[key] = hb.ID
	return nil
}

// DeleteHeartbeat removes the agent's heartbeat; its tick records die with it
// (ON DELETE CASCADE). An absent heartbeat is domain.ErrNotFound.
func (hs *heartbeatStore) DeleteHeartbeat(ctx context.Context, workspaceID, agentID string) error {
	if workspaceID == "" || agentID == "" {
		return domain.ErrNotFound
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	id, exists := hs.s.heartbeatsByAgent[heartbeatAgentKey(workspaceID, agentID)]
	if !exists {
		return domain.ErrNotFound
	}

	delete(hs.s.heartbeatsByAgent, heartbeatAgentKey(workspaceID, agentID))
	delete(hs.s.heartbeats, id)
	// Run records die with the heartbeat (ON DELETE CASCADE).
	for runID, run := range hs.s.heartbeatRuns {
		if run.HeartbeatID == id {
			delete(hs.s.heartbeatRuns, runID)
		}
	}
	return nil
}

// ClaimDueHeartbeats claims due enabled heartbeats under the store mutex and
// advances each row's next_tick_at at claim time (design D14): the next
// occurrence is computed in the workspace timezone via domain.NextRun, so a
// catch-up after downtime fires once and reschedules without replaying.
func (hs *heartbeatStore) ClaimDueHeartbeats(ctx context.Context, now time.Time, limit int) ([]store.HeartbeatClaim, error) {
	if limit <= 0 {
		return []store.HeartbeatClaim{}, nil
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	var due []*domain.Heartbeat
	for _, hb := range hs.s.heartbeats {
		if hb.Enabled && hb.NextTickAt != nil && !hb.NextTickAt.After(now) {
			due = append(due, hb)
		}
	}

	// Oldest due first, id as the determinism tiebreak — mirrors the
	// postgres claim's ORDER BY.
	sort.Slice(due, func(i, j int) bool {
		if due[i].NextTickAt.Equal(*due[j].NextTickAt) {
			return due[i].ID < due[j].ID
		}
		return due[i].NextTickAt.Before(*due[j].NextTickAt)
	})
	if len(due) > limit {
		due = due[:limit]
	}

	claims := make([]store.HeartbeatClaim, 0, len(due))
	for _, hb := range due {
		next, err := domain.NextRun(hb.Expr, now, hs.workspaceLocationLocked(hb.WorkspaceID))
		if err != nil {
			return nil, err
		}
		hb.NextTickAt = next
		hb.UpdatedAt = now
		claims = append(claims, store.HeartbeatClaim{Heartbeat: cloneHeartbeat(hb)})
	}
	return claims, nil
}

// StartHeartbeatRun inserts a tick record with status running. The heartbeat
// must exist within the run's workspace (FK parity).
func (hs *heartbeatStore) StartHeartbeatRun(ctx context.Context, run *domain.HeartbeatRun) error {
	if run == nil || run.WorkspaceID == "" || run.HeartbeatID == "" || run.SessionID == "" {
		return domain.ErrInvalid
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	hb, exists := hs.s.heartbeats[run.HeartbeatID]
	if !exists || hb.WorkspaceID != run.WorkspaceID {
		return fmt.Errorf("%w: heartbeat not found in workspace", domain.ErrNotFound)
	}

	if run.ID != "" {
		if _, exists := hs.s.heartbeatRuns[run.ID]; exists {
			return fmt.Errorf("%w: heartbeat run with id %q already exists", domain.ErrConflict, run.ID)
		}
	} else {
		run.ID = uuid.NewString()
	}
	run.Status = domain.HeartbeatRunStatusRunning
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}

	hs.s.heartbeatRuns[run.ID] = cloneHeartbeatRun(run)
	return nil
}

// FinishHeartbeatRun writes the tick outcome and mirrors it into the
// heartbeat's last_tick snapshot atomically (the postgres adapter does both
// writes in one transaction).
func (hs *heartbeatStore) FinishHeartbeatRun(ctx context.Context, workspaceID, runID string, status string, durationMS int64, tokensUsed int, deliveryStatus string, errMsg string, traceID string) error {
	if workspaceID == "" || runID == "" {
		return domain.ErrNotFound
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	run, exists := hs.s.heartbeatRuns[runID]
	if !exists || run.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}

	run.Status = status
	run.DurationMS = durationMS
	run.TokensUsed = tokensUsed
	run.DeliveryStatus = deliveryStatus
	run.Error = errMsg
	run.TraceID = traceID

	if hb, exists := hs.s.heartbeats[run.HeartbeatID]; exists {
		hb.LastTick = &domain.HeartbeatLastRun{
			Status:         status,
			Trigger:        run.Trigger,
			StartedAt:      run.StartedAt,
			DurationMS:     durationMS,
			TokensUsed:     tokensUsed,
			SessionID:      run.SessionID,
			DeliveryStatus: deliveryStatus,
			Error:          errMsg,
		}
		hb.UpdatedAt = time.Now().UTC()
	}
	return nil
}

// ApplyHeartbeatOutcome applies the failure-streak accounting atomically
// (design D12): completed resets the streak, failed increments it —
// auto-pausing the heartbeat at five consecutive failures — and every other
// status (blocked policy grief, cancelled, skipped guards) leaves it
// untouched.
func (hs *heartbeatStore) ApplyHeartbeatOutcome(ctx context.Context, workspaceID, heartbeatID string, status string) (bool, error) {
	if workspaceID == "" || heartbeatID == "" {
		return false, domain.ErrNotFound
	}

	hs.s.mu.Lock()
	defer hs.s.mu.Unlock()

	hb, exists := hs.s.heartbeats[heartbeatID]
	if !exists || hb.WorkspaceID != workspaceID {
		return false, domain.ErrNotFound
	}

	paused := false
	switch status {
	case domain.HeartbeatRunStatusCompleted:
		hb.FailureStreak = 0
	case domain.HeartbeatRunStatusFailed:
		hb.FailureStreak++
		if hb.FailureStreak >= heartbeatAutoPauseStreak {
			paused = true
			hb.Enabled = false
			hb.NextTickAt = nil
		}
	}
	hb.UpdatedAt = time.Now().UTC()
	return paused, nil
}

// ListHeartbeatRuns returns the heartbeat's tick records newest-first with
// the total across all pages.
func (hs *heartbeatStore) ListHeartbeatRuns(ctx context.Context, workspaceID, heartbeatID string, limit, offset int) ([]domain.HeartbeatRun, int, error) {
	if workspaceID == "" || heartbeatID == "" {
		return []domain.HeartbeatRun{}, 0, nil
	}

	hs.s.mu.RLock()
	defer hs.s.mu.RUnlock()

	matches := make([]domain.HeartbeatRun, 0)
	for _, run := range hs.s.heartbeatRuns {
		if run.WorkspaceID == workspaceID && run.HeartbeatID == heartbeatID {
			matches = append(matches, *cloneHeartbeatRun(run))
		}
	}
	// Newest first, id as the determinism tiebreak.
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].StartedAt.Equal(matches[j].StartedAt) {
			return matches[i].ID > matches[j].ID
		}
		return matches[i].StartedAt.After(matches[j].StartedAt)
	})
	total := len(matches)
	if offset > 0 {
		if offset >= total {
			return []domain.HeartbeatRun{}, total, nil
		}
		matches = matches[offset:]
	}
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, total, nil
}
