package scheduler

import "time"

// ScheduleInput is the write shape of a schedule: the five fields a user or
// an agent supplies. It carries no timestamps on purpose — the Wails binding
// surface never sends a Go time.Time (add-scheduler design D13).
type ScheduleInput struct {
	Name    string `json:"name"`
	AgentID string `json:"agentId"`
	Prompt  string `json:"prompt"`
	Cron    string `json:"cron"`
	Enabled bool   `json:"enabled"`
}

// ScheduleView is the read shape of a schedule. Description is the
// human-readable recurrence computed from Cron on every read so it can never
// disagree with the stored expression (one describer, design D11).
// NextFireAt and LastRunAt are null when unset.
type ScheduleView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AgentID     string `json:"agentId"`
	Prompt      string `json:"prompt"`
	Cron        string `json:"cron"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	// NextFireAt is the persisted due slot; nil when disabled or not yet
	// computed. LastRunAt is nil until the schedule's first fire or miss.
	NextFireAt *time.Time `json:"nextFireAt"`
	LastRunAt  *time.Time `json:"lastRunAt"`
	// LastRunStatus is "" (never fired) or one of the store.ScheduleRunStatus
	// constants (ran, failed, missed); LastRunError carries the failure
	// reason when the status is failed, and LastRunSessionID locates the
	// session a ran fire produced.
	LastRunStatus    string    `json:"lastRunStatus"`
	LastRunError     string    `json:"lastRunError"`
	LastRunSessionID string    `json:"lastRunSessionId"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// Change is what scheduler listeners receive (add-scheduler design D10): one
// event per mutation, fire, and miss. SessionID is set only when a run just
// created a session; fields not applicable to the change are empty strings.
type Change struct {
	ScheduleID string
	SessionID  string
	AgentID    string
}

// Listener is notified on every schedule mutation, fire, and miss. It is
// invoked synchronously after the change is committed and must not block.
type Listener func(Change)

// ValidationError reports a rejected schedule write, naming the offending
// field so both the editor and the scheduler tool can render the error inline.
// Field is one of "name", "agent", "prompt", "recurrence".
type ValidationError struct {
	// Field is the input field the rejection is about.
	Field string
	// Message is the human-readable reason, without the field name.
	Message string
	// ExistingID is the id of the schedule already holding the name, set
	// only for duplicate-name rejections.
	ExistingID string
}

// Error returns the rejection as "<field>: <message>", e.g.
// "name: a schedule named "Standup digest" already exists (id ab12cd34)".
func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
