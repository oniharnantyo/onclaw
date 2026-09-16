package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Heartbeat delivery target types: the creator's paired gateway DMs by
// default, or an explicit workspace channel (add-agent-heartbeat D8).
const (
	HeartbeatDeliveryCreatorDM = "creator_dm"
	HeartbeatDeliveryChannel   = "channel"
)

// Heartbeat run trigger origins.
const (
	HeartbeatTriggerTick   = "tick"
	HeartbeatTriggerManual = "manual"
)

// Heartbeat run terminal statuses. Heartbeat has `skipped` (guard outcomes:
// empty checklist, outside active hours, busy agent — add-agent-heartbeat
// D12) and no `missed` (there is no once kind; a catch-up tick fires once and
// reschedules without replaying, add-agent-heartbeat D14).
const (
	HeartbeatRunStatusRunning   = "running"
	HeartbeatRunStatusCompleted = "completed"
	HeartbeatRunStatusFailed    = "failed"
	HeartbeatRunStatusCancelled = "cancelled"
	HeartbeatRunStatusBlocked   = "blocked"
	HeartbeatRunStatusSkipped   = "skipped"
)

// Heartbeat run delivery outcomes (add-agent-heartbeat D7/D8): a silent tick
// completes as suppressed, a report is delivered, and a delivery failure is
// recorded on the run without failing it.
const (
	HeartbeatDeliveryStatusDelivered  = "delivered"
	HeartbeatDeliveryStatusSuppressed = "suppressed"
	HeartbeatDeliveryStatusFailed     = "failed"
)

// heartbeatMinCadence is the fastest allowed tick cadence: every tick appends
// to the agent's persistent hb_ session, so sub-five-minute expressions would
// run the token rent up unattended (add-agent-heartbeat D4).
const heartbeatMinCadence = 5 * time.Minute

// Heartbeat is one agent's opt-in proactive wakeup (add-agent-heartbeat D1):
// a periodic ambient tick in which the agent reviews its HEARTBEAT checklist
// plus workspace activity and either reports what needs attention or stays
// silent. Exactly one heartbeat exists per agent. The next tick time is
// always derived, never stored from input (add-agent-heartbeat D4).
type Heartbeat struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	AgentID     string  `json:"agent_id"`
	CreatedBy   *string `json:"created_by"` // nullable; ON DELETE SET NULL
	// Prompt is the HEARTBEAT checklist (add-agent-heartbeat D3). It may be
	// empty or whitespace: an empty checklist means skip-until-edited, with
	// no model call.
	Prompt string `json:"prompt"`
	// Expr is a standard 5-field cron expression evaluated in the workspace
	// timezone (add-agent-heartbeat D4).
	Expr        string            `json:"expr"`
	ActiveStart *string           `json:"active_start,omitempty"` // nullable "HH:MM" workspace-tz window; both nil = 24/7
	ActiveEnd   *string           `json:"active_end,omitempty"`
	Delivery    HeartbeatDelivery `json:"delivery"`
	Enabled     bool              `json:"enabled"`
	// NextTickAt is derived state: computed by the caller in the workspace
	// timezone and advanced by the store at claim time — never accepted from
	// request input (add-agent-heartbeat D4, D14).
	NextTickAt *time.Time `json:"next_tick_at"`
	// LastTick is the outcome snapshot mirrored onto the heartbeat when a run
	// finishes; nil until the first outcome.
	LastTick      *HeartbeatLastRun `json:"last_tick,omitempty"`
	FailureStreak int               `json:"failure_streak"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// HeartbeatDelivery is the report delivery target: the creator's paired
// gateway DMs (default) or a workspace channel the report is posted into
// (add-agent-heartbeat D8).
type HeartbeatDelivery struct {
	Type      string `json:"type"` // creator_dm (default) | channel
	ChannelID string `json:"channel_id,omitempty"`
}

// HeartbeatLastRun is the outcome snapshot mirrored onto the heartbeat when a
// tick finishes. SessionID is always the agent's shared hb_ session — every
// tick appends to one persistent transcript (add-agent-heartbeat D2).
type HeartbeatLastRun struct {
	Status         string    `json:"status"`  // completed|failed|cancelled|blocked|skipped
	Trigger        string    `json:"trigger"` // tick|manual
	StartedAt      time.Time `json:"started_at"`
	DurationMS     int64     `json:"duration_ms"`
	TokensUsed     int       `json:"tokens_used"`
	SessionID      string    `json:"session_id"`
	DeliveryStatus string    `json:"delivery_status"` // ""|delivered|suppressed|failed
	Error          string    `json:"error,omitempty"`
}

// HeartbeatRun is the per-tick record backing a heartbeat's runs listing
// (add-agent-heartbeat D15): one row per execution, newest-first in
// listings, each linkable to the shared session transcript via SessionID.
type HeartbeatRun struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	HeartbeatID string `json:"heartbeat_id"`
	// AgentID is denormalized for listing ergonomics, mirroring
	// workspace_id on scheduler_runs (house tenant rule).
	AgentID        string    `json:"agent_id"`
	SessionID      string    `json:"session_id"`
	Trigger        string    `json:"trigger"`
	Status         string    `json:"status"`
	StartedAt      time.Time `json:"started_at"`
	DurationMS     int64     `json:"duration_ms"`
	TokensUsed     int       `json:"tokens_used"`
	DeliveryStatus string    `json:"delivery_status"`
	Error          string    `json:"error,omitempty"`
	// TraceID is the run's pinned Langfuse trace id, empty when the turn
	// sampled out or predates tracing (000050 precedent) — the runs surface
	// composes the deep link from it.
	TraceID string `json:"trace_id,omitempty"`
}

// ValidateHeartbeat validates a heartbeat for persistence (add-agent-heartbeat
// D3–D5). The checklist is not required: an empty (or whitespace) prompt is
// valid and means skip-until-edited. The expression must be a standard 5-field
// cron whose successive firings are at least five minutes apart. Active hours
// are a start/end "HH:MM" pair in the workspace timezone — both or neither;
// an equal pair is rejected because a zero-width window never fires. The
// struct is normalized in place: the prompt and expression are trimmed,
// delivery defaults to creator_dm (which clears ChannelID), and NextTickAt is
// zeroed — it is a derived field (add-agent-heartbeat D4), so the caller
// recomputes it after validation and never passes it in.
func ValidateHeartbeat(hb *Heartbeat, now time.Time) error {
	if hb == nil {
		return fmt.Errorf("%w: heartbeat is required", ErrInvalid)
	}

	// Whitespace folds away so the empty-checklist skip decision is a plain
	// empty-string check.
	hb.Prompt = strings.TrimSpace(hb.Prompt)

	expr := strings.TrimSpace(hb.Expr)
	if expr == "" {
		return fmt.Errorf("%w: cron expression is required", ErrInvalid)
	}
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return fmt.Errorf("%w: invalid cron expression %q: %v", ErrInvalid, expr, err)
	}
	hb.Expr = expr

	// Cadence floor (add-agent-heartbeat D4): the gap between two successive
	// firings bounds the tick rate of a persistent session, so it must stay
	// at or above the five-minute floor.
	first := sched.Next(now)
	if first.IsZero() {
		return fmt.Errorf("%w: cron expression %q never fires", ErrInvalid, expr)
	}
	second := sched.Next(first)
	if second.IsZero() {
		return fmt.Errorf("%w: cron expression %q never fires twice", ErrInvalid, expr)
	}
	if second.Sub(first) < heartbeatMinCadence {
		return fmt.Errorf("%w: cron expression %q fires faster than every 5m", ErrInvalid, expr)
	}

	switch {
	case hb.ActiveStart == nil && hb.ActiveEnd == nil:
		// 24/7 — the default window (add-agent-heartbeat D5).
	case hb.ActiveStart == nil || hb.ActiveEnd == nil:
		return fmt.Errorf("%w: active hours require both active_start and active_end", ErrInvalid)
	default:
		startH, startM, okStart := parseHeartbeatClock(*hb.ActiveStart)
		endH, endM, okEnd := parseHeartbeatClock(*hb.ActiveEnd)
		if !okStart || !okEnd {
			return fmt.Errorf("%w: active hours must be HH:MM (24h)", ErrInvalid)
		}
		// Zero-width window rejected (add-agent-heartbeat D5): start == end
		// would put every instant outside the window — an always-skipped
		// footgun — so the save fails naming the active-hours fields.
		if startH == endH && startM == endM {
			return fmt.Errorf("%w: active hours must not be equal: %s–%s never fires", ErrInvalid, *hb.ActiveStart, *hb.ActiveEnd)
		}
		start := strings.TrimSpace(*hb.ActiveStart)
		end := strings.TrimSpace(*hb.ActiveEnd)
		hb.ActiveStart, hb.ActiveEnd = &start, &end
	}

	switch hb.Delivery.Type {
	case "", HeartbeatDeliveryCreatorDM:
		hb.Delivery.Type = HeartbeatDeliveryCreatorDM
		hb.Delivery.ChannelID = ""
	case HeartbeatDeliveryChannel:
		if strings.TrimSpace(hb.Delivery.ChannelID) == "" {
			return fmt.Errorf("%w: channel_id is required for channel delivery", ErrInvalid)
		}
		hb.Delivery.ChannelID = strings.TrimSpace(hb.Delivery.ChannelID)
	default:
		return fmt.Errorf("%w: delivery type %q must be %q or %q", ErrInvalid, hb.Delivery.Type, HeartbeatDeliveryCreatorDM, HeartbeatDeliveryChannel)
	}

	// Derived runtime state is never accepted from input (add-agent-heartbeat
	// D4): the caller recomputes the next tick after validation.
	hb.NextTickAt = nil
	return nil
}

// parseHeartbeatClock parses a strict 24-hour "HH:MM" wall-clock string.
func parseHeartbeatClock(v string) (int, int, bool) {
	v = strings.TrimSpace(v)
	if len(v) != 5 || v[2] != ':' {
		return 0, 0, false
	}
	hh, hhOK := twoDigits(v[:2])
	mm, mmOK := twoDigits(v[3:])
	if !hhOK || !mmOK || hh > 23 || mm > 59 {
		return 0, 0, false
	}
	return hh, mm, true
}

// twoDigits parses exactly two ASCII decimal digits.
func twoDigits(s string) (int, bool) {
	if len(s) != 2 || s[0] < '0' || s[0] > '9' || s[1] < '0' || s[1] > '9' {
		return 0, false
	}
	return int(s[0]-'0')*10 + int(s[1]-'0'), true
}
