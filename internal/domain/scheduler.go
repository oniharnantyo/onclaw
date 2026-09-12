package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler kinds.
const (
	SchedulerKindRecurring = "recurring"
	SchedulerKindOnce      = "once"
)

// Scheduler delivery target types.
const (
	SchedulerDeliveryThread  = "thread"
	SchedulerDeliveryChannel = "channel"
)

// Scheduler run trigger origins.
const (
	SchedulerTriggerScheduled = "scheduler"
	SchedulerTriggerManual    = "manual"
)

// Scheduler run terminal statuses.
const (
	SchedulerRunStatusRunning   = "running"
	SchedulerRunStatusCompleted = "completed"
	SchedulerRunStatusFailed    = "failed"
	SchedulerRunStatusCancelled = "cancelled"
	SchedulerRunStatusBlocked   = "blocked"
	SchedulerRunStatusMissed    = "missed"
)

// Scheduler run delivery outcomes.
const (
	SchedulerDeliveryDelivered  = "delivered"
	SchedulerDeliverySuppressed = "suppressed"
	SchedulerDeliveryFailed     = "failed"
)

// Scheduler represents a named standing order: one workspace agent fired on a
// recurrence (5-field cron in the workspace timezone) or at a one-shot
// instant, with a task prompt and a delivery target (integrate-scheduler D2,
// D12). The human label and next fire time are always derived, never stored
// from input.
type Scheduler struct {
	ID          string            `json:"id"`
	WorkspaceID string            `json:"workspace_id"`
	AgentID     string            `json:"agent_id"`
	CreatedBy   *string           `json:"created_by"` // nullable; ON DELETE SET NULL
	Name        string            `json:"name"`
	Prompt      string            `json:"prompt"`
	Kind        string            `json:"kind"`   // recurring | once
	Expr        string            `json:"expr"`   // standard 5-field cron; empty for once
	RunAt       *time.Time        `json:"run_at"` // once only
	Delivery    SchedulerDelivery `json:"delivery"`
	Enabled     bool              `json:"enabled"`
	NextRunAt   *time.Time        `json:"next_run_at"`
	LastRun     *SchedulerLastRun `json:"last_run"` // nil until first outcome
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// SchedulerDelivery is the run-result delivery target: the run's own thread
// (default) or a workspace channel the final reply is posted into
// (integrate-scheduler D8).
type SchedulerDelivery struct {
	Type      string `json:"type"` // thread (default) | channel
	ChannelID string `json:"channel_id,omitempty"`
}

// SchedulerLastRun is the outcome snapshot mirrored onto the scheduler when a
// run finishes (or a once scheduler is archived as missed).
type SchedulerLastRun struct {
	Status         string    `json:"status"`  // completed|failed|cancelled|blocked|missed
	Trigger        string    `json:"trigger"` // scheduler|manual
	StartedAt      time.Time `json:"started_at"`
	DurationMS     int64     `json:"duration_ms"`
	TokensUsed     int       `json:"tokens_used"`
	SessionID      string    `json:"session_id"`
	DeliveryStatus string    `json:"delivery_status"` // ""|delivered|suppressed|failed
	Error          string    `json:"error,omitempty"`
}

// SchedulerRun is the per-run record backing a scheduler's runs listing
// (integrate-scheduler design D7 "lightweight run-records view"): one row per
// execution, newest-first in listings, each linkable to its transcript via
// SessionID.
type SchedulerRun struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	SchedulerID    string    `json:"scheduler_id"`
	SessionID      string    `json:"session_id"`
	Trigger        string    `json:"trigger"`
	Status         string    `json:"status"`
	StartedAt      time.Time `json:"started_at"`
	DurationMS     int64     `json:"duration_ms"`
	TokensUsed     int       `json:"tokens_used"`
	DeliveryStatus string    `json:"delivery_status"`
	Error          string    `json:"error,omitempty"`
}

// ValidateScheduler validates a scheduler for persistence. When creating, a
// one-shot RunAt must lie strictly in the future relative to now; on update a
// carried-past RunAt is accepted so unchanged one-shots do not fail. The
// struct is normalized in place: name is trimmed, delivery defaults to thread,
// a thread delivery clears ChannelID, and recurring/once schedule fields are
// mutually exclusive. Name uniqueness is a store/handler concern, validated
// here nowhere (integrate-scheduler D12: validation lives in the domain
// layer, not DB constraints).
func ValidateScheduler(s *Scheduler, now time.Time, creating bool) error {
	if s == nil {
		return fmt.Errorf("%w: scheduler is required", ErrInvalid)
	}

	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return fmt.Errorf("%w: scheduler name is required", ErrInvalid)
	}
	if strings.TrimSpace(s.Prompt) == "" {
		return fmt.Errorf("%w: scheduler prompt is required", ErrInvalid)
	}

	switch s.Kind {
	case SchedulerKindRecurring:
		expr := strings.TrimSpace(s.Expr)
		if expr == "" {
			return fmt.Errorf("%w: cron expression is required for a recurring scheduler", ErrInvalid)
		}
		if _, err := cron.ParseStandard(expr); err != nil {
			return fmt.Errorf("%w: invalid cron expression %q: %v", ErrInvalid, expr, err)
		}
		s.Expr = expr
		s.RunAt = nil
	case SchedulerKindOnce:
		if s.RunAt == nil {
			return fmt.Errorf("%w: run_at is required for a one-shot scheduler", ErrInvalid)
		}
		s.Expr = ""
		if creating && !s.RunAt.After(now) {
			return fmt.Errorf("%w: run_at must be in the future for a one-shot scheduler", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: scheduler kind %q must be %q or %q", ErrInvalid, s.Kind, SchedulerKindRecurring, SchedulerKindOnce)
	}

	switch s.Delivery.Type {
	case "", SchedulerDeliveryThread:
		s.Delivery.Type = SchedulerDeliveryThread
		s.Delivery.ChannelID = ""
	case SchedulerDeliveryChannel:
		if strings.TrimSpace(s.Delivery.ChannelID) == "" {
			return fmt.Errorf("%w: channel_id is required for channel delivery", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: delivery type %q must be %q or %q", ErrInvalid, s.Delivery.Type, SchedulerDeliveryThread, SchedulerDeliveryChannel)
	}

	return nil
}

// NextRun returns the next instant the expression fires strictly after after,
// evaluated in loc (the workspace timezone). Parse failures wrap the offending
// expression in an ErrInvalid error.
//
// The standard parser leaves SpecSchedule.Location as time.Local, and
// SpecSchedule.Next then evaluates the fields in the passed time's own
// location — so converting `after` into loc is what anchors the computation
// to the workspace timezone. An explicit TZ= prefix inside the expression
// overrides the workspace timezone, matching crontab semantics.
func NextRun(expr string, after time.Time, loc *time.Location) (*time.Time, error) {
	if loc == nil {
		loc = time.UTC
	}
	expr = strings.TrimSpace(expr)
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cron expression %q: %v", ErrInvalid, expr, err)
	}
	next := sched.Next(after.In(loc))
	if next.IsZero() {
		return nil, fmt.Errorf("%w: cron expression %q never fires", ErrInvalid, expr)
	}
	return &next, nil
}

// cronStarBit mirrors robfig/cron's internal marker for explicitly-unbounded
// fields ("*"), set as the top bit of the field mask.
const cronStarBit uint64 = 1 << 63

// schedulerWeekdayNames maps cron day-of-week values (0=Sunday) to short
// labels used in human-readable schedule descriptions.
var schedulerWeekdayNames = [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// HumanLabel derives the human-readable schedule description ("09:00 ·
// Mon–Fri") for the common expression shapes: daily at a fixed time, weekly on
// selected weekdays, monthly on day N, and hourly at :MM. Anything else
// (custom) returns the raw expression, trimmed.
func HumanLabel(expr string) string {
	raw := strings.TrimSpace(expr)
	sched, err := cron.ParseStandard(raw)
	if err != nil {
		return raw
	}
	spec, ok := sched.(*cron.SpecSchedule)
	if !ok {
		return raw
	}

	// Fixed wall-clock time of day?
	hour, hourOK := cronSingleBit(spec.Hour, 0, 23)
	minute, minuteOK := cronSingleBit(spec.Minute, 0, 59)
	if hourOK && minuteOK {
		clock := fmt.Sprintf("%02d:%02d", hour, minute)
		switch {
		case cronEveryValue(spec.Month, 1, 12) && cronEveryValue(spec.Dom, 1, 31) && cronEveryValue(spec.Dow, 0, 6):
			return clock + " · Daily"
		case cronEveryValue(spec.Month, 1, 12) && cronEveryValue(spec.Dom, 1, 31):
			days := cronSetBits(spec.Dow, 0, 6)
			if len(days) > 0 {
				return clock + " · " + weekdayLabel(days)
			}
		case cronEveryValue(spec.Month, 1, 12) && cronEveryValue(spec.Dow, 0, 6):
			days := cronSetBits(spec.Dom, 1, 31)
			if len(days) == 1 {
				return fmt.Sprintf("%s · Monthly on day %d", clock, days[0])
			}
		}
		return raw
	}

	// Hourly at :MM?
	minutes := cronSetBits(spec.Minute, 0, 59)
	if len(minutes) == 1 &&
		cronEveryValue(spec.Hour, 0, 23) &&
		cronEveryValue(spec.Dom, 1, 31) &&
		cronEveryValue(spec.Dow, 0, 6) &&
		cronEveryValue(spec.Month, 1, 12) {
		return fmt.Sprintf("Hourly at :%02d", minutes[0])
	}

	return raw
}

// weekdayLabel renders a sorted day-of-week set: a single contiguous run of
// two or more days collapses to "Mon–Fri", anything else comma-joins the
// names.
func weekdayLabel(days []int) string {
	contiguous := len(days) >= 2
	for i := 1; i < len(days) && contiguous; i++ {
		if days[i] != days[i-1]+1 {
			contiguous = false
		}
	}
	if contiguous {
		return schedulerWeekdayNames[days[0]] + "–" + schedulerWeekdayNames[days[len(days)-1]]
	}
	names := make([]string, len(days))
	for i, d := range days {
		names[i] = schedulerWeekdayNames[d]
	}
	return strings.Join(names, ", ")
}

// cronSingleBit returns the single value encoded in mask (ignoring the star
// bit) when exactly one bit in [minBit, maxBit] is set.
func cronSingleBit(mask uint64, minBit, maxBit uint) (int, bool) {
	bits := cronSetBits(mask, minBit, maxBit)
	if len(bits) != 1 {
		return 0, false
	}
	return bits[0], true
}

// cronSetBits enumerates the set bits of mask within [minBit, maxBit] in
// ascending order, ignoring the star marker.
func cronSetBits(mask uint64, minBit, maxBit uint) []int {
	var out []int
	mask &^= cronStarBit
	for b := minBit; b <= maxBit; b++ {
		if mask&(1<<b) != 0 {
			out = append(out, int(b))
		}
	}
	return out
}

// cronEveryValue reports whether mask is an explicitly unbounded field ("*"):
// the star bit is set and every value in range is present. Step expressions
// like "*/2" carry the star bit but not every value, so they are not treated
// as unrestricted.
func cronEveryValue(mask uint64, minBit, maxBit uint) bool {
	if mask&cronStarBit == 0 {
		return false
	}
	mask &^= cronStarBit
	for b := minBit; b <= maxBit; b++ {
		if mask&(1<<b) == 0 {
			return false
		}
	}
	return true
}
