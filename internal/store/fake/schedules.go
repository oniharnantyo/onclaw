package fake

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

type scheduleStore struct{ s *Store }

func (f scheduleStore) Create(_ context.Context, sched *store.Schedule) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if err := f.nameConflict("create schedule", sched.ID, sched.Name); err != nil {
		return err
	}
	if sched.ID == "" {
		sched.ID = nextID()
	}
	stamp(&sched.CreatedAt, &sched.UpdatedAt)
	f.s.schedules[sched.ID] = cloneSchedule(*sched)
	return nil
}

func (f scheduleStore) ByID(_ context.Context, id string) (*store.Schedule, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	sched, ok := f.s.schedules[id]
	if !ok {
		return nil, fmt.Errorf("schedule %s: %w", id, store.ErrNotFound)
	}
	out := cloneSchedule(sched)
	return &out, nil
}

// ByName matches case-insensitively, mirroring the sqlite adapter's NOCASE
// name column.
func (f scheduleStore) ByName(_ context.Context, name string) (*store.Schedule, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	for _, sched := range f.s.schedules {
		if strings.EqualFold(sched.Name, name) {
			out := cloneSchedule(sched)
			return &out, nil
		}
	}
	return nil, fmt.Errorf("schedule %s: %w", name, store.ErrNotFound)
}

func (f scheduleStore) List(_ context.Context) ([]store.Schedule, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	out := make([]store.Schedule, 0, len(f.s.schedules))
	for _, sched := range f.s.schedules {
		out = append(out, cloneSchedule(sched))
	}
	sort.Slice(out, func(i, j int) bool {
		if strings.ToLower(out[i].Name) != strings.ToLower(out[j].Name) {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (f scheduleStore) ListDue(_ context.Context, now time.Time) ([]store.Schedule, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	out := make([]store.Schedule, 0)
	for _, sched := range f.s.schedules {
		if sched.Enabled && sched.NextFireAt != nil && !sched.NextFireAt.After(now) {
			out = append(out, cloneSchedule(sched))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].NextFireAt.Equal(*out[j].NextFireAt) {
			return out[i].NextFireAt.Before(*out[j].NextFireAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (f scheduleStore) Update(_ context.Context, sched *store.Schedule) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	stored, ok := f.s.schedules[sched.ID]
	if !ok {
		return fmt.Errorf("schedule %s: %w", sched.ID, store.ErrNotFound)
	}
	if err := f.nameConflict("update schedule "+sched.ID, sched.ID, sched.Name); err != nil {
		return err
	}
	sched.CreatedAt = stored.CreatedAt
	sched.UpdatedAt = nowUTC()
	f.s.schedules[sched.ID] = cloneSchedule(*sched)
	return nil
}

func (f scheduleStore) Delete(_ context.Context, id string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if _, ok := f.s.schedules[id]; !ok {
		return fmt.Errorf("schedule %s: %w", id, store.ErrNotFound)
	}
	delete(f.s.schedules, id)
	return nil
}

// RecordFire writes the last-run state and the advanced slot in one update,
// mirroring the sqlite adapter's single UPDATE (design D3). A nil
// fire.NextFireAt clears the stored slot.
func (f scheduleStore) RecordFire(_ context.Context, id string, fire store.ScheduleFire) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	stored, ok := f.s.schedules[id]
	if !ok {
		return fmt.Errorf("schedule %s: %w", id, store.ErrNotFound)
	}
	at := fire.At.UTC()
	stored.LastRunAt = &at
	stored.LastRunStatus = fire.Status
	stored.LastRunError = fire.Error
	stored.LastRunSessionID = fire.SessionID
	stored.NextFireAt = cloneTimePtr(fire.NextFireAt)
	stored.UpdatedAt = nowUTC()
	f.s.schedules[id] = stored
	return nil
}

// nameConflict rejects a name already held by another schedule
// case-insensitively, mirroring the sqlite adapter's unique index on the
// NOCASE name column. op prefixes the ErrDuplicate wrap. The caller must
// hold f.s.mu (the store mutex is not reentrant).
func (f scheduleStore) nameConflict(op, id, name string) error {
	for otherID, other := range f.s.schedules {
		if otherID != id && strings.EqualFold(other.Name, name) {
			return fmt.Errorf("%s: %w", op, store.ErrDuplicate)
		}
	}
	return nil
}

// deleteSchedules removes every schedule of the agent — the cascade on the
// agents foreign key, mirroring sqlite's ON DELETE CASCADE (add-scheduler
// design D9). The caller must hold s.mu (the store mutex is not reentrant).
func (s *Store) deleteSchedules(agentID string) {
	for id, sched := range s.schedules {
		if sched.AgentID == agentID {
			delete(s.schedules, id)
		}
	}
}

// cloneSchedule copies a schedule value, normalizing the nullable times to
// UTC through fresh pointers so the stored value never aliases a caller's
// time (mirroring the sqlite adapter's formatTime(t.UTC()) encoding).
func cloneSchedule(in store.Schedule) store.Schedule {
	in.NextFireAt = cloneTimePtr(in.NextFireAt)
	in.LastRunAt = cloneTimePtr(in.LastRunAt)
	return in
}

// cloneTimePtr copies a *time.Time, normalizing to UTC; nil stays nil.
func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}
