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
// SchedulerStore implementation (integrate-scheduler 1.4). Mirrors the
// postgres adapter: due claims are evaluated under the store mutex (the
// fake's atomicity seam — the postgres adapter uses FOR UPDATE SKIP LOCKED),
// a once scheduler overdue beyond the grace window is archived as missed
// instead of fired, and next_run_at is always recomputed from the workspace
// timezone — never trusted from the input struct.
// -------------------------------------------------------------------------

// schedulerMissedGraceWindow is how long past its instant a once scheduler may
// still fire on catch-up; beyond it the claim archives the row as missed
// instead (kept in lockstep with the postgres adapter's constant).
const schedulerMissedGraceWindow = time.Hour

type schedulerStore struct {
	s *fakeStore
}

func cloneScheduler(s *domain.Scheduler) *domain.Scheduler {
	if s == nil {
		return nil
	}
	cp := *s
	if s.CreatedBy != nil {
		cb := *s.CreatedBy
		cp.CreatedBy = &cb
	}
	if s.RunAt != nil {
		t := *s.RunAt
		cp.RunAt = &t
	}
	if s.NextRunAt != nil {
		t := *s.NextRunAt
		cp.NextRunAt = &t
	}
	if s.LastRun != nil {
		lr := *s.LastRun
		cp.LastRun = &lr
	}
	return &cp
}

func cloneSchedulerRun(r *domain.SchedulerRun) *domain.SchedulerRun {
	if r == nil {
		return nil
	}
	cp := *r
	return &cp
}

// workspaceLocation resolves the workspace's IANA timezone for schedule
// interpretation, falling back to UTC for the impossible case of a missing or
// unparseable stored zone (workspace timezones are IANA-validated at the
// domain layer).
func (ss *schedulerStore) workspaceLocationLocked(workspaceID string) *time.Location {
	if w, ok := ss.s.workspaces[workspaceID]; ok {
		if loc, err := time.LoadLocation(w.Timezone); err == nil {
			return loc
		}
	}
	return time.UTC
}

// computeNextRunAt derives the stored next fire time from the scheduler
// shape: absent while paused/archived, the one-shot instant, or the next
// future occurrence of the expression in the workspace timezone.
func (ss *schedulerStore) computeNextRunAtLocked(s *domain.Scheduler, now time.Time) (*time.Time, error) {
	if !s.Enabled {
		return nil, nil
	}
	if s.Kind == domain.SchedulerKindOnce {
		t := s.RunAt.UTC()
		return &t, nil
	}
	return domain.NextRun(s.Expr, now, ss.workspaceLocationLocked(s.WorkspaceID))
}

func (ss *schedulerStore) CreateScheduler(ctx context.Context, workspaceID string, s *domain.Scheduler) error {
	if s == nil {
		return domain.ErrInvalid
	}
	now := time.Now().UTC()
	s.WorkspaceID = workspaceID
	if err := domain.ValidateScheduler(s, now, true); err != nil {
		return err
	}

	ss.s.mu.Lock()
	defer ss.s.mu.Unlock()

	if _, exists := ss.s.workspaces[workspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	agent, exists := ss.s.agents[s.AgentID]
	if !exists || agent.WorkspaceID != workspaceID {
		return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
	}
	if s.CreatedBy != nil && *s.CreatedBy != "" {
		if _, exists := ss.s.users[*s.CreatedBy]; !exists {
			return fmt.Errorf("%w: user not found", domain.ErrNotFound)
		}
	}

	// UNIQUE (workspace_id, agent_id, name).
	for _, other := range ss.s.schedulers {
		if other.WorkspaceID == workspaceID && other.AgentID == s.AgentID && other.Name == s.Name {
			return fmt.Errorf("%w: scheduler name %q already taken in this workspace for this agent", domain.ErrConflict, s.Name)
		}
	}

	if s.ID != "" {
		if _, exists := ss.s.schedulers[s.ID]; exists {
			return fmt.Errorf("%w: scheduler with id %q already exists", domain.ErrConflict, s.ID)
		}
	} else {
		s.ID = uuid.NewString()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = now
	}
	next, err := ss.computeNextRunAtLocked(s, now)
	if err != nil {
		return err
	}
	s.NextRunAt = next

	ss.s.schedulers[s.ID] = cloneScheduler(s)
	return nil
}

func (ss *schedulerStore) GetScheduler(ctx context.Context, workspaceID, id string) (*domain.Scheduler, error) {
	if workspaceID == "" || id == "" {
		return nil, nil
	}

	ss.s.mu.RLock()
	defer ss.s.mu.RUnlock()

	// Absence — including a foreign workspace's id — reads as (nil, nil).
	s, exists := ss.s.schedulers[id]
	if !exists || s.WorkspaceID != workspaceID {
		return nil, nil
	}
	return cloneScheduler(s), nil
}

func (ss *schedulerStore) ListSchedulers(ctx context.Context, workspaceID string) ([]domain.Scheduler, error) {
	if workspaceID == "" {
		return []domain.Scheduler{}, nil
	}

	ss.s.mu.RLock()
	defer ss.s.mu.RUnlock()

	schedulers := make([]domain.Scheduler, 0)
	for _, s := range ss.s.schedulers {
		if s.WorkspaceID == workspaceID {
			schedulers = append(schedulers, *cloneScheduler(s))
		}
	}
	sort.Slice(schedulers, func(i, j int) bool {
		if schedulers[i].CreatedAt.Equal(schedulers[j].CreatedAt) {
			return schedulers[i].ID < schedulers[j].ID
		}
		return schedulers[i].CreatedAt.Before(schedulers[j].CreatedAt)
	})
	return schedulers, nil
}

func (ss *schedulerStore) UpdateScheduler(ctx context.Context, workspaceID string, s *domain.Scheduler) error {
	if s == nil || s.ID == "" {
		return domain.ErrInvalid
	}
	now := time.Now().UTC()
	s.WorkspaceID = workspaceID
	if err := domain.ValidateScheduler(s, now, false); err != nil {
		return err
	}

	ss.s.mu.Lock()
	defer ss.s.mu.Unlock()

	existing, exists := ss.s.schedulers[s.ID]
	if !exists || existing.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}

	agent, exists := ss.s.agents[s.AgentID]
	if !exists || agent.WorkspaceID != workspaceID {
		return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
	}

	// UNIQUE (workspace_id, agent_id, name), excluding this row.
	for _, other := range ss.s.schedulers {
		if other.ID != s.ID && other.WorkspaceID == workspaceID && other.AgentID == s.AgentID && other.Name == s.Name {
			return fmt.Errorf("%w: scheduler name %q already taken in this workspace for this agent", domain.ErrConflict, s.Name)
		}
	}

	s.CreatedBy = existing.CreatedBy
	s.CreatedAt = existing.CreatedAt
	s.UpdatedAt = now
	next, err := ss.computeNextRunAtLocked(s, now)
	if err != nil {
		return err
	}
	s.NextRunAt = next

	ss.s.schedulers[s.ID] = cloneScheduler(s)
	return nil
}

func (ss *schedulerStore) DeleteScheduler(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	ss.s.mu.Lock()
	defer ss.s.mu.Unlock()

	s, exists := ss.s.schedulers[id]
	if !exists || s.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}

	delete(ss.s.schedulers, id)
	// Run records die with the scheduler (ON DELETE CASCADE).
	for runID, run := range ss.s.schedulerRuns {
		if run.SchedulerID == id {
			delete(ss.s.schedulerRuns, runID)
		}
	}
	return nil
}

func (ss *schedulerStore) ClaimDueSchedulers(ctx context.Context, now time.Time, limit int) ([]store.SchedulerClaim, error) {
	if limit <= 0 {
		return []store.SchedulerClaim{}, nil
	}

	ss.s.mu.Lock()
	defer ss.s.mu.Unlock()

	type candidate struct {
		s     *domain.Scheduler
		dueAt time.Time
	}
	var due []candidate
	for _, s := range ss.s.schedulers {
		if !s.Enabled {
			continue
		}
		switch s.Kind {
		case domain.SchedulerKindRecurring:
			if s.NextRunAt != nil && !s.NextRunAt.After(now) {
				due = append(due, candidate{s: s, dueAt: *s.NextRunAt})
			}
		case domain.SchedulerKindOnce:
			if s.RunAt != nil && !s.RunAt.After(now) {
				due = append(due, candidate{s: s, dueAt: *s.RunAt})
			}
		}
	}

	// Oldest due first, id as the determinism tiebreak — mirrors the
	// postgres claim's ORDER BY.
	sort.Slice(due, func(i, j int) bool {
		if due[i].dueAt.Equal(due[j].dueAt) {
			return due[i].s.ID < due[j].s.ID
		}
		return due[i].dueAt.Before(due[j].dueAt)
	})
	if len(due) > limit {
		due = due[:limit]
	}

	claims := make([]store.SchedulerClaim, 0, len(due))
	for _, c := range due {
		s := c.s
		missed := false
		if s.Kind == domain.SchedulerKindOnce && now.Sub(*s.RunAt) > schedulerMissedGraceWindow {
			// Overdue beyond the grace window: archive as missed, never fire.
			missed = true
			s.Enabled = false
			s.NextRunAt = nil
			started := s.RunAt.UTC()
			s.LastRun = &domain.SchedulerLastRun{
				Status:    domain.SchedulerRunStatusMissed,
				Trigger:   domain.SchedulerTriggerScheduled,
				StartedAt: started,
			}
		} else {
			// Fire once: recurring reschedules to the next future occurrence
			// at claim time; once archives after firing.
			s.Enabled = s.Kind == domain.SchedulerKindRecurring
			next, err := ss.computeNextRunAtLocked(s, now)
			if err != nil {
				return nil, err
			}
			s.NextRunAt = next
		}
		s.UpdatedAt = now
		claims = append(claims, store.SchedulerClaim{Scheduler: cloneScheduler(s), Missed: missed})
	}
	return claims, nil
}

func (ss *schedulerStore) StartSchedulerRun(ctx context.Context, run *domain.SchedulerRun) error {
	if run == nil || run.WorkspaceID == "" || run.SchedulerID == "" || run.SessionID == "" {
		return domain.ErrInvalid
	}

	ss.s.mu.Lock()
	defer ss.s.mu.Unlock()

	scheduler, exists := ss.s.schedulers[run.SchedulerID]
	if !exists || scheduler.WorkspaceID != run.WorkspaceID {
		return fmt.Errorf("%w: scheduler not found in workspace", domain.ErrNotFound)
	}

	if run.ID != "" {
		if _, exists := ss.s.schedulerRuns[run.ID]; exists {
			return fmt.Errorf("%w: scheduler run with id %q already exists", domain.ErrConflict, run.ID)
		}
	} else {
		run.ID = uuid.NewString()
	}
	run.Status = domain.SchedulerRunStatusRunning
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}

	ss.s.schedulerRuns[run.ID] = cloneSchedulerRun(run)
	return nil
}

func (ss *schedulerStore) FinishSchedulerRun(ctx context.Context, workspaceID, runID string, status string, durationMS int64, tokensUsed int, deliveryStatus string, errMsg string, traceID string) error {
	if workspaceID == "" || runID == "" {
		return domain.ErrNotFound
	}

	ss.s.mu.Lock()
	defer ss.s.mu.Unlock()

	run, exists := ss.s.schedulerRuns[runID]
	if !exists || run.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}

	run.Status = status
	run.DurationMS = durationMS
	run.TokensUsed = tokensUsed
	run.DeliveryStatus = deliveryStatus
	run.Error = errMsg
	run.TraceID = traceID

	// Mirror the outcome into the scheduler's last_run atomically (the
	// postgres adapter does both writes in one transaction).
	if scheduler, exists := ss.s.schedulers[run.SchedulerID]; exists {
		scheduler.LastRun = &domain.SchedulerLastRun{
			Status:         status,
			Trigger:        run.Trigger,
			StartedAt:      run.StartedAt,
			DurationMS:     durationMS,
			TokensUsed:     tokensUsed,
			SessionID:      run.SessionID,
			DeliveryStatus: deliveryStatus,
			Error:          errMsg,
		}
		scheduler.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (ss *schedulerStore) ListSchedulerRuns(ctx context.Context, workspaceID, schedulerID string, limit, offset int) ([]domain.SchedulerRun, int, error) {
	if workspaceID == "" || schedulerID == "" {
		return []domain.SchedulerRun{}, 0, nil
	}

	ss.s.mu.RLock()
	defer ss.s.mu.RUnlock()

	matches := make([]domain.SchedulerRun, 0)
	for _, run := range ss.s.schedulerRuns {
		if run.WorkspaceID == workspaceID && run.SchedulerID == schedulerID {
			matches = append(matches, *cloneSchedulerRun(run))
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
			return []domain.SchedulerRun{}, total, nil
		}
		matches = matches[offset:]
	}
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, total, nil
}
