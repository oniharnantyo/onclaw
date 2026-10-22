package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/store"
)

// LateTolerance is how far past its slot a due schedule may still fire
// (add-scheduler design D3): it absorbs tick jitter and brief laptop sleep.
// A due slot older than this — or a tick while the gateway is Paused — is
// marked missed instead of run late.
const LateTolerance = 2 * time.Minute

// maxMissedSteps caps the most-recent-slot iteration for a missed fire so a
// minutely schedule after a long downtime cannot spin unbounded (design D3).
const maxMissedSteps = 10000

// fireTimeLayout is how a fire time appears in a session title.
const fireTimeLayout = "2006-01-02 15:04"

// Service is the one cron brain (add-scheduler design D1): it owns schedule
// validation, next-fire computation, the tick state machine, and change
// notification. The Wails schedule service and the scheduler tool are thin
// clients over it, so they can never disagree.
type Service struct {
	store store.Store
	runs  Runs
	gw    GatewayState
	clock Clock

	mu        sync.Mutex
	listeners map[int]Listener
	nextID    int

	// loop is the gateway ticker attachment (ticker.go): nil until Attach
	// and again after Stop. A Service carries at most one attachment — the
	// composition root attaches it to the application's one gateway.
	loopMu sync.Mutex
	loop   *tickerLoop
}

// Option configures a Service at construction.
type Option func(*Service)

// WithClock overrides the service's clock. Production leaves the default
// (time.Now); tests inject a fixed clock for deterministic expectations.
func WithClock(c Clock) Option {
	return func(s *Service) { s.clock = c }
}

// New returns a scheduler Service over the schedule store, the agent runtime
// seam, and the gateway state read.
func New(st store.Store, runs Runs, gw GatewayState, opts ...Option) *Service {
	s := &Service{
		store:     st,
		runs:      runs,
		gw:        gw,
		clock:     time.Now,
		listeners: make(map[int]Listener),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create validates in and stores a new schedule, computing its next fire time
// from the recurrence when enabled. A rejected write returns a
// *ValidationError naming the offending field.
func (s *Service) Create(ctx context.Context, in ScheduleInput) (ScheduleView, error) {
	in, err := s.validate(ctx, in, "")
	if err != nil {
		return ScheduleView{}, err
	}

	row := store.Schedule{
		Name:    in.Name,
		AgentID: in.AgentID,
		Prompt:  in.Prompt,
		Cron:    in.Cron,
		Enabled: in.Enabled,
	}
	if in.Enabled {
		next, err := Next(in.Cron, s.now())
		if err != nil {
			return ScheduleView{}, &ValidationError{Field: "recurrence", Message: err.Error()}
		}
		row.NextFireAt = &next
	}

	if err := s.store.Schedules().Create(ctx, &row); err != nil {
		if verr := s.duplicateError(ctx, err, in.Name); verr != nil {
			return ScheduleView{}, verr
		}
		return ScheduleView{}, fmt.Errorf("create schedule: %w", err)
	}

	s.notify(Change{ScheduleID: row.ID, AgentID: row.AgentID})
	return toView(row), nil
}

// Update rewrites the schedule's five input fields (full replace) and
// recomputes its next fire time from the — possibly new — expression, or
// clears it when the schedule is disabled. Last-run state is untouched.
func (s *Service) Update(ctx context.Context, id string, in ScheduleInput) (ScheduleView, error) {
	existing, err := s.store.Schedules().ByID(ctx, id)
	if err != nil {
		return ScheduleView{}, fmt.Errorf("update schedule %s: %w", id, err)
	}

	in, err = s.validate(ctx, in, id)
	if err != nil {
		return ScheduleView{}, err
	}

	row := *existing
	row.Name = in.Name
	row.AgentID = in.AgentID
	row.Prompt = in.Prompt
	row.Cron = in.Cron
	row.Enabled = in.Enabled
	if in.Enabled {
		next, err := Next(in.Cron, s.now())
		if err != nil {
			return ScheduleView{}, &ValidationError{Field: "recurrence", Message: err.Error()}
		}
		row.NextFireAt = &next
	} else {
		row.NextFireAt = nil
	}

	if err := s.store.Schedules().Update(ctx, &row); err != nil {
		if verr := s.duplicateError(ctx, err, in.Name); verr != nil {
			return ScheduleView{}, verr
		}
		return ScheduleView{}, fmt.Errorf("update schedule %s: %w", id, err)
	}

	s.notify(Change{ScheduleID: row.ID, AgentID: row.AgentID})
	return toView(row), nil
}

// SetEnabled toggles the schedule. Enabling computes the next fire time from
// the recurrence; disabling clears it (the spec: toggling off clears the next
// fire time).
func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool) (ScheduleView, error) {
	existing, err := s.store.Schedules().ByID(ctx, id)
	if err != nil {
		return ScheduleView{}, fmt.Errorf("set schedule %s enabled: %w", id, err)
	}

	row := *existing
	row.Enabled = enabled
	if enabled {
		next, err := Next(row.Cron, s.now())
		if err != nil {
			return ScheduleView{}, fmt.Errorf("set schedule %s enabled: %w", id, err)
		}
		row.NextFireAt = &next
	} else {
		row.NextFireAt = nil
	}

	if err := s.store.Schedules().Update(ctx, &row); err != nil {
		return ScheduleView{}, fmt.Errorf("set schedule %s enabled: %w", id, err)
	}

	s.notify(Change{ScheduleID: row.ID, AgentID: row.AgentID})
	return toView(row), nil
}

// Delete removes the schedule; an unknown id wraps store.ErrNotFound.
func (s *Service) Delete(ctx context.Context, id string) error {
	row, err := s.store.Schedules().ByID(ctx, id)
	if err != nil {
		return fmt.Errorf("delete schedule %s: %w", id, err)
	}
	if err := s.store.Schedules().Delete(ctx, id); err != nil {
		return fmt.Errorf("delete schedule %s: %w", id, err)
	}

	s.notify(Change{ScheduleID: id, AgentID: row.AgentID})
	return nil
}

// Get returns the schedule's view; an unknown id wraps store.ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (ScheduleView, error) {
	row, err := s.store.Schedules().ByID(ctx, id)
	if err != nil {
		return ScheduleView{}, fmt.Errorf("get schedule %s: %w", id, err)
	}
	return toView(*row), nil
}

// List returns every schedule's view, in the store's (case-insensitive name)
// order.
func (s *Service) List(ctx context.Context) ([]ScheduleView, error) {
	rows, err := s.store.Schedules().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	views := make([]ScheduleView, 0, len(rows))
	for _, row := range rows {
		views = append(views, toView(row))
	}
	return views, nil
}

// Preview returns the recurrence's description and next three fires for the
// editor's live preview, evaluated from the service's clock. An invalid
// expression yields a Preview with OK false carrying the parse error.
func (s *Service) Preview(expr string) Preview {
	return PreviewAt(expr, s.now())
}

// RunNow runs the schedule on demand (add-scheduler design D14): the same
// fire path as a scheduled run, with the state machine bypassed — no gateway
// check, no enabled check, and the stored next fire time left untouched.
func (s *Service) RunNow(ctx context.Context, id string) (ScheduleView, error) {
	row, err := s.store.Schedules().ByID(ctx, id)
	if err != nil {
		return ScheduleView{}, fmt.Errorf("run schedule %s now: %w", id, err)
	}
	s.run(ctx, *row, s.now(), row.NextFireAt)
	return s.Get(ctx, id)
}

// Tick runs one pass of the due state machine (add-scheduler design D3) at
// now. Every enabled schedule whose stored next fire time is due either
// fires (gateway Running, within LateTolerance), is marked missed (overdue
// beyond tolerance, or the gateway Paused — recording the most recent missed
// slot), or waits (within tolerance but the gateway not yet Running). After
// the due pass, every enabled schedule with a future next fire time has it
// recomputed from its expression, so DST and local-timezone changes self-heal
// instead of leaving a stale persisted time. Per-row errors are logged and
// skipped; Tick returns an error only when the store listing fails.
func (s *Service) Tick(ctx context.Context, now time.Time) error {
	due, err := s.store.Schedules().ListDue(ctx, now)
	if err != nil {
		return fmt.Errorf("scheduler tick: list due schedules: %w", err)
	}
	for _, row := range due {
		s.tickDue(ctx, row, now)
	}

	// Second pass: self-heal future slots against the expression. Listed
	// after the due pass so rows just fired or missed compare against their
	// freshly advanced slot.
	rows, err := s.store.Schedules().List(ctx)
	if err != nil {
		return fmt.Errorf("scheduler tick: list schedules: %w", err)
	}
	for _, row := range rows {
		s.healFuture(ctx, row, now)
	}
	return nil
}

// Subscribe registers fn to be called on every schedule mutation, fire, and
// miss (add-scheduler design D10) and returns a function that unregisters
// it. The returned cancel function is safe to call more than once and safe
// for concurrent use. Listeners are invoked synchronously but outside the
// service lock, like gateway.Manager's state listeners.
func (s *Service) Subscribe(fn Listener) (cancel func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextID
	s.nextID++
	s.listeners[id] = fn

	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.listeners, id)
		})
	}
}

// notify snapshots the listeners under the lock and invokes them outside it,
// so a listener may safely call back into the service.
func (s *Service) notify(ch Change) {
	s.mu.Lock()
	listeners := make([]Listener, 0, len(s.listeners))
	for _, fn := range s.listeners {
		listeners = append(listeners, fn)
	}
	s.mu.Unlock()

	for _, fn := range listeners {
		fn(ch)
	}
}

// now is the service's current time, in the clock's location — local time in
// production.
func (s *Service) now() time.Time { return s.clock() }

// validate applies the spec's rejection order to a write: name, prompt,
// agent (present, then existing), recurrence, duplicate name. It returns the
// input with name, prompt, and cron trimmed. excludeID is the id of the
// schedule being updated, whose own name does not count as a duplicate.
func (s *Service) validate(ctx context.Context, in ScheduleInput, excludeID string) (ScheduleInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Prompt = strings.TrimSpace(in.Prompt)
	in.Cron = strings.TrimSpace(in.Cron)

	if in.Name == "" {
		return in, &ValidationError{Field: "name", Message: "name is required"}
	}
	if in.Prompt == "" {
		return in, &ValidationError{Field: "prompt", Message: "prompt is required"}
	}
	if in.AgentID == "" {
		return in, &ValidationError{Field: "agent", Message: "agent is required"}
	}
	if _, err := s.store.Agents().ByID(ctx, in.AgentID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return in, &ValidationError{Field: "agent", Message: fmt.Sprintf("agent %s does not exist", in.AgentID)}
		}
		return in, fmt.Errorf("check agent %s: %w", in.AgentID, err)
	}
	if _, err := ParseFiveField(in.Cron); err != nil {
		return in, &ValidationError{Field: "recurrence", Message: err.Error()}
	}
	if existing, err := s.store.Schedules().ByName(ctx, in.Name); err == nil && existing != nil && existing.ID != excludeID {
		return in, &ValidationError{
			Field:      "name",
			Message:    fmt.Sprintf("a schedule named %q already exists (id %s)", existing.Name, existing.ID),
			ExistingID: existing.ID,
		}
	}
	return in, nil
}

// duplicateError maps a late store.ErrDuplicate — the race fallback when the
// pre-write check passed but the store's unique index still rejects — into
// the same duplicate-name ValidationError shape. It returns nil for any
// other error.
func (s *Service) duplicateError(ctx context.Context, err error, name string) error {
	if !errors.Is(err, store.ErrDuplicate) {
		return nil
	}
	if existing, gerr := s.store.Schedules().ByName(ctx, name); gerr == nil && existing != nil {
		return &ValidationError{
			Field:      "name",
			Message:    fmt.Sprintf("a schedule named %q already exists (id %s)", existing.Name, existing.ID),
			ExistingID: existing.ID,
		}
	}
	return &ValidationError{Field: "name", Message: fmt.Sprintf("a schedule named %q already exists", name)}
}

// tickDue decides one due row's fate and acts on it: fire, miss, or wait.
func (s *Service) tickDue(ctx context.Context, row store.Schedule, now time.Time) {
	if row.NextFireAt == nil || row.NextFireAt.After(now) {
		return // defensive: ListDue should never yield a non-due row
	}
	slot := *row.NextFireAt

	// The advanced slot every outcome writes; a broken stored expression is
	// logged and the row left for the next tick rather than mis-advanced.
	nextAfter, err := Next(row.Cron, now)
	if err != nil {
		slog.WarnContext(ctx, "scheduler: due schedule has an invalid recurrence", "schedule_id", row.ID, "error", err)
		return
	}

	switch state := s.gw.State(); {
	case state == gateway.StateRunning && now.Sub(slot) <= LateTolerance:
		s.run(ctx, row, slot.In(now.Location()), &nextAfter)
	case now.Sub(slot) > LateTolerance || state == gateway.StatePaused:
		s.miss(ctx, row, slot, now, nextAfter)
	default:
		// Within tolerance but the gateway is not Running yet — transient
		// around boot. Leave the row due; it re-evaluates next tick.
	}
}

// miss records a skipped fire (add-scheduler design D3): the most recent
// recurrence slot at or before now is stamped as the missed fire time and the
// next fire advances past now, so the schedule never runs late — it skips.
func (s *Service) miss(ctx context.Context, row store.Schedule, storedSlot time.Time, now time.Time, nextAfter time.Time) {
	slot := mostRecentSlot(row.Cron, storedSlot, now)
	fire := store.ScheduleFire{
		At:         slot,
		Status:     store.ScheduleRunStatusMissed,
		NextFireAt: &nextAfter,
	}
	if err := s.store.Schedules().RecordFire(ctx, row.ID, fire); err != nil {
		slog.WarnContext(ctx, "scheduler: record missed fire failed", "schedule_id", row.ID, "error", err)
		return
	}
	s.notify(Change{ScheduleID: row.ID, AgentID: row.AgentID})
}

// mostRecentSlot walks the recurrence forward from storedSlot to the last
// slot at or before now (the missed fire the schedule should show), capped at
// maxMissedSteps for minutely expressions after long downtime. A broken
// expression falls back to the stored slot.
func mostRecentSlot(cron string, storedSlot, now time.Time) time.Time {
	schedule, err := ParseFiveField(cron)
	if err != nil {
		return storedSlot
	}
	slot := storedSlot
	for i := 0; i < maxMissedSteps; i++ {
		next := schedule.Next(slot)
		if next.After(now) {
			return slot
		}
		slot = next
	}
	return slot
}

// healFuture recomputes one enabled schedule's future next fire time from its
// expression, rewriting the stored slot when they disagree (DST and
// local-timezone self-heal, design D3). Due and disabled rows are untouched.
func (s *Service) healFuture(ctx context.Context, row store.Schedule, now time.Time) {
	if !row.Enabled || row.NextFireAt == nil || !row.NextFireAt.After(now) {
		return
	}
	nextAfter, err := Next(row.Cron, now)
	if err != nil {
		slog.WarnContext(ctx, "scheduler: schedule has an invalid recurrence", "schedule_id", row.ID, "error", err)
		return
	}
	if nextAfter.Equal(*row.NextFireAt) {
		return
	}
	row.NextFireAt = &nextAfter
	if err := s.store.Schedules().Update(ctx, &row); err != nil {
		slog.WarnContext(ctx, "scheduler: next-fire self-heal failed", "schedule_id", row.ID, "error", err)
	}
}

// run executes one fire of row for the fire at slot (add-scheduler design
// D7): it marks the fire and advances the next slot BEFORE starting the run,
// so a crash mid-fire can never double-fire — the worst case is a ran row
// with no session. advanceTo is written as the schedule's next fire slot by
// every RecordFire call; nil leaves the stored slot untouched (RunNow,
// design D14). The outcome — including the session id when a run started —
// is recorded and announced to listeners.
func (s *Service) run(ctx context.Context, row store.Schedule, slot time.Time, advanceTo *time.Time) string {
	// Mark-and-advance before the run (design D3). If the row vanished
	// mid-tick, firing anyway could double-fire — abort instead.
	mark := store.ScheduleFire{At: slot, Status: store.ScheduleRunStatusRan, NextFireAt: advanceTo}
	if err := s.store.Schedules().RecordFire(ctx, row.ID, mark); err != nil {
		slog.WarnContext(ctx, "scheduler: mark fire failed, run aborted", "schedule_id", row.ID, "error", err)
		return ""
	}

	// The agent may have been deleted since the schedule was written.
	if _, err := s.store.Agents().ByID(ctx, row.AgentID); err != nil {
		reason := err.Error()
		if errors.Is(err, store.ErrNotFound) {
			reason = "agent not found"
		}
		return s.recordFailure(ctx, row, slot, advanceTo, reason, "")
	}

	title := row.Name + " — " + slot.Format(fireTimeLayout)
	sessionID, err := s.runs.Create(ctx, row.AgentID, title)
	if err != nil {
		return s.recordFailure(ctx, row, slot, advanceTo, err.Error(), "")
	}
	if err := s.runs.Submit(ctx, sessionID, row.Prompt); err != nil {
		return s.recordFailure(ctx, row, slot, advanceTo, err.Error(), sessionID)
	}

	done := store.ScheduleFire{At: slot, Status: store.ScheduleRunStatusRan, SessionID: sessionID, NextFireAt: advanceTo}
	if err := s.store.Schedules().RecordFire(ctx, row.ID, done); err != nil {
		// The run itself started; the row's bookkeeping failure is logged,
		// not fatal.
		slog.WarnContext(ctx, "scheduler: record fire failed", "schedule_id", row.ID, "error", err)
	}
	s.notify(Change{ScheduleID: row.ID, SessionID: sessionID, AgentID: row.AgentID})
	return sessionID
}

// recordFailure writes a failed last-run outcome (the run could not start)
// and announces it. sessionID is kept when a session was created before the
// failure, so the partial run stays reachable.
func (s *Service) recordFailure(ctx context.Context, row store.Schedule, slot time.Time, advanceTo *time.Time, reason, sessionID string) string {
	fire := store.ScheduleFire{
		At:         slot,
		Status:     store.ScheduleRunStatusFailed,
		Error:      reason,
		SessionID:  sessionID,
		NextFireAt: advanceTo,
	}
	if err := s.store.Schedules().RecordFire(ctx, row.ID, fire); err != nil {
		slog.WarnContext(ctx, "scheduler: record failed fire", "schedule_id", row.ID, "error", err)
	}
	s.notify(Change{ScheduleID: row.ID, SessionID: sessionID, AgentID: row.AgentID})
	return sessionID
}

// toView projects a stored schedule onto the read shape, computing the
// recurrence description on the way out so it always matches the expression.
func toView(row store.Schedule) ScheduleView {
	return ScheduleView{
		ID:               row.ID,
		Name:             row.Name,
		AgentID:          row.AgentID,
		Prompt:           row.Prompt,
		Cron:             row.Cron,
		Description:      Describe(row.Cron),
		Enabled:          row.Enabled,
		NextFireAt:       row.NextFireAt,
		LastRunAt:        row.LastRunAt,
		LastRunStatus:    row.LastRunStatus,
		LastRunError:     row.LastRunError,
		LastRunSessionID: row.LastRunSessionID,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}
