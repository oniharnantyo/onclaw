// Package ingest is the neutral turn-ingest seam (add-skill-curation-from-traces
// D1): a bounded queue of turn-end jobs with per-session serialization that
// dispatches every job to each registered consumer with per-consumer fault
// isolation. The package is deliberately mechanical and dependency-free — it
// knows nothing about memory extraction, skill curation, or any other post-run
// work — so consumers never import each other, the runner never learns who
// consumes, and one consumer's failure cannot affect another's dispatch.
package ingest

import "context"

// Run origins carried on Job. The values mirror the ExecRequest Origin
// constants in internal/agents — duplicated as literals so neither package
// imports the other (the seam imports nothing internal). Empty behaves as a
// direct user chat.
const (
	OriginUser      = "user"
	OriginScheduler = "scheduler"
	OriginChannel   = "channel"
	OriginTelegram  = "telegram"
	OriginHeartbeat = "heartbeat"
)

// Job captures everything a consumer needs at enqueue time: the turn's
// identity coordinates, the triggering origin, the session shape driving
// consumer-side visibility ceilings, and the run-finish status. The raw
// material is never copied into the job — consumers load the session's
// events themselves through their own stores, so a consumer failure leaves
// the raw log untouched and reprocessing possible.
type Job struct {
	WorkspaceID string
	AgentID     string
	UserID      string
	SessionID   string
	TurnID      string

	// Origin is the finished run's origin: "user", "scheduler", "channel",
	// "telegram", or "heartbeat" (the ExecRequest Origin values).
	Origin string

	// HumanParticipants is the session-shape human count feeding
	// consumer-side visibility and participant rules. It is counted at
	// enqueue time, never re-derived from the transcript.
	HumanParticipants int

	// Status is the run-finish status literal ("completed"/"failed"). Both
	// statuses ingest — a failed run still contains a real user turn; the
	// field is attribution, never a filter.
	Status string
}

// Consumer receives one drained job. Ingest runs off the run hot path and
// must fail soft internally: an error return is logged and never fails a run
// nor the dispatch of the remaining consumers (the worker isolates both
// errors and panics per consumer).
type Consumer interface {
	Ingest(ctx context.Context, job Job) error
}
