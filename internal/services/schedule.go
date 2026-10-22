package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/scheduler"
)

// ScheduleService is the bound service for the schedule editor (add-scheduler
// design D1): a thin client over the scheduler's cron brain, so it can never
// disagree with the scheduler tool's validation or descriptions. Every
// schedule change — the service's own mutations and the fires and misses the
// scheduler makes on its own — is forwarded to the emitter as
// EventSchedulesChanged (design D10).
type ScheduleService struct {
	sched   *scheduler.Service
	emitter Emitter
}

// NewScheduleService returns the schedule service over sched, subscribing to
// its change notifications and forwarding every one through emitter.
func NewScheduleService(sched *scheduler.Service, emitter Emitter) *ScheduleService {
	s := &ScheduleService{sched: sched, emitter: emitter}
	sched.Subscribe(s.onChange)
	return s
}

// List returns every schedule's view, in the store's (case-insensitive name)
// order. The result is empty (not nil) when no schedules exist.
func (s *ScheduleService) List(ctx context.Context) ([]scheduler.ScheduleView, error) {
	views, err := s.sched.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	return views, nil
}

// Get returns one schedule's view; an unknown id wraps store.ErrNotFound.
func (s *ScheduleService) Get(ctx context.Context, id string) (scheduler.ScheduleView, error) {
	view, err := s.sched.Get(ctx, id)
	if err != nil {
		return scheduler.ScheduleView{}, fmt.Errorf("get schedule: %w", err)
	}
	return view, nil
}

// Create validates and stores a new schedule. A rejected write returns the
// *scheduler.ValidationError itself (unwrapped, so the frontend can split
// "<field>: <message>" for inline field errors); every other failure is
// wrapped.
func (s *ScheduleService) Create(ctx context.Context, in scheduler.ScheduleInput) (scheduler.ScheduleView, error) {
	view, err := s.sched.Create(ctx, in)
	if err != nil {
		return scheduler.ScheduleView{}, s.wrap("create schedule", err)
	}
	return view, nil
}

// Update rewrites a schedule's input fields; like Create, a rejected write
// returns the unwrapped *scheduler.ValidationError.
func (s *ScheduleService) Update(ctx context.Context, id string, in scheduler.ScheduleInput) (scheduler.ScheduleView, error) {
	view, err := s.sched.Update(ctx, id, in)
	if err != nil {
		return scheduler.ScheduleView{}, s.wrap("update schedule", err)
	}
	return view, nil
}

// SetEnabled toggles the schedule; enabling computes its next fire time,
// disabling clears it.
func (s *ScheduleService) SetEnabled(ctx context.Context, id string, enabled bool) (scheduler.ScheduleView, error) {
	view, err := s.sched.SetEnabled(ctx, id, enabled)
	if err != nil {
		return scheduler.ScheduleView{}, s.wrap("set schedule enabled", err)
	}
	return view, nil
}

// Delete removes the schedule; an unknown id wraps store.ErrNotFound.
func (s *ScheduleService) Delete(ctx context.Context, id string) error {
	if err := s.sched.Delete(ctx, id); err != nil {
		return s.wrap("delete schedule", err)
	}
	return nil
}

// RunNow runs the schedule on demand (design D14): the same fire path as a
// scheduled run, bypassing the gateway state, the enabled flag, and the
// stored next fire time.
func (s *ScheduleService) RunNow(ctx context.Context, id string) (scheduler.ScheduleView, error) {
	view, err := s.sched.RunNow(ctx, id)
	if err != nil {
		return scheduler.ScheduleView{}, s.wrap("run schedule", err)
	}
	return view, nil
}

// Preview returns the recurrence's description and next three fires for the
// editor's live preview. An invalid expression yields a Preview with OK
// false carrying the parse error — not a Go error.
func (s *ScheduleService) Preview(_ context.Context, expr string) (scheduler.Preview, error) {
	return s.sched.Preview(expr), nil
}

// wrap rewrites one scheduler failure for the binding surface: a
// *scheduler.ValidationError is returned as itself (the frontend splits its
// "<field>: <message>" form), anything else is wrapped with the operation's
// context.
func (s *ScheduleService) wrap(op string, err error) error {
	var verr *scheduler.ValidationError
	if errors.As(err, &verr) {
		return verr
	}
	return fmt.Errorf("%s: %w", op, err)
}

// onChange forwards one scheduler change to the emitter. The scheduler
// invokes listeners synchronously and requires them not to block, so the
// emit runs on its own goroutine (the GatewayService pattern).
func (s *ScheduleService) onChange(ch scheduler.Change) {
	go s.emitter.Emit(EventSchedulesChanged, SchedulesChangedEvent{
		ScheduleID: ch.ScheduleID,
		SessionID:  ch.SessionID,
		AgentID:    ch.AgentID,
	})
}
