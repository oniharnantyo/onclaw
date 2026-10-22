package store

import (
	"context"
	"time"
)

// ScheduleStore persists recurring agent schedules (add-scheduler design D3):
// each row binds an agent and a prompt to a five-field cron expression and
// carries the persisted NextFireAt slot the scheduler's tick advances.
type ScheduleStore interface {
	// Create stores a new schedule, assigning ID and stamping the
	// timestamps (UTC) when empty. Names are unique case-insensitively: a
	// duplicate returns ErrDuplicate.
	Create(ctx context.Context, s *Schedule) error
	// ByID returns the schedule with the given ID; ErrNotFound when absent.
	ByID(ctx context.Context, id string) (*Schedule, error)
	// ByName returns the schedule with the given name, matched
	// case-insensitively; ErrNotFound when absent.
	ByName(ctx context.Context, name string) (*Schedule, error)
	// List returns every schedule ordered by name, case-insensitive. The
	// result is never nil.
	List(ctx context.Context) ([]Schedule, error)
	// ListDue returns the enabled schedules whose NextFireAt is set and at
	// or before now, ordered by NextFireAt. The result is never nil.
	ListDue(ctx context.Context, now time.Time) ([]Schedule, error)
	// Update rewrites an existing schedule's definition (name, agent,
	// prompt, cron, enabled, next fire) and stamps UpdatedAt (UTC);
	// ErrNotFound when absent, ErrDuplicate when the name collides with
	// another schedule's case-insensitively. The last-run fields are not
	// written.
	Update(ctx context.Context, s *Schedule) error
	// Delete removes a schedule; ErrNotFound when absent.
	Delete(ctx context.Context, id string) error
	// RecordFire writes the schedule's last-run state and its advanced
	// NextFireAt in one update (design D3: mark-and-advance before the run
	// starts, so a crash mid-fire can never double-fire); ErrNotFound when
	// absent. A nil fire.NextFireAt clears the stored slot.
	RecordFire(ctx context.Context, id string, fire ScheduleFire) error
}
