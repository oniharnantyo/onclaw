package store

import (
	"context"
	"time"
)

// Memory trace origins — the MemoryTrace.Origin vocabulary (add-agent-memory
// design D12). Only user-origin traces count toward curation; the other
// origins record that an unattended or delegated turn recalled memory.
const (
	// MemoryOriginUser marks a trace from an interactive user turn.
	MemoryOriginUser = "user"
	// MemoryOriginScheduler marks a trace from a scheduled run.
	MemoryOriginScheduler = "scheduler"
	// MemoryOriginSubagent marks a trace from a child turn.
	MemoryOriginSubagent = "subagent"
	// MemoryOriginOther marks a trace from any other turn context.
	MemoryOriginOther = "other"
)

// MemoryTraceHit is one search result inside a recall trace: the note's
// workspace-relative path and its match score.
type MemoryTraceHit struct {
	Path  string  `json:"path"`
	Score float64 `json:"score"`
}

// MemoryTrace is one recorded memory_search invocation (design D6). It is
// append-only and is the sole signal source for the consolidation sweep. Hits
// carry paths and scores only — like every memory row, no note content.
type MemoryTrace struct {
	ID string
	// AgentID owns the trace; deleting the agent deletes it (FK cascade).
	AgentID string
	// SessionID and TurnID locate the turn that issued the search; empty
	// when the caller has no session context.
	SessionID string
	TurnID    string
	// Origin is one of the MemoryOrigin* constants.
	Origin string
	Query  string
	// Hits are the ranked results, best first.
	Hits      []MemoryTraceHit
	CreatedAt time.Time
}

// MemoryNoteState is one note's derived retention state (design D9): the
// scores the sweep maintains between sweeps. Name is the note's name from its
// frontmatter — the note's text lives in the workspace file, never here.
type MemoryNoteState struct {
	AgentID string
	Name    string
	// Retention is the note's survival potential Φ.
	Retention float64
	// RecallCount and DistinctQueries are the note's user-visible recall
	// statistics since first sight.
	RecallCount     int
	DistinctQueries int
	// LastRecall is nil for a note never recalled.
	LastRecall *time.Time
	// FirstSeen is when the sweep first ingested the note; the store
	// stamps it when zero.
	FirstSeen time.Time
}

// MemoryStaging is one promotion candidate's state (design D8/D10): a
// daily-note line the sweep has gated into the index. SourceRef is the line's
// workspace reference (e.g. "memory/2026-10-27.md#L12"); Signals is the
// candidate's opaque JSON signal document owned by the sweep (the Hook.Config
// pattern — the store persists it verbatim).
type MemoryStaging struct {
	AgentID   string
	SourceRef string
	Signals   string
	// FirstStaged is when the line was first staged; the store stamps it
	// when zero (the 30-day expiry counts from it).
	FirstStaged time.Time
	// Promoted flips once the line has been appended to the index.
	Promoted bool
}

// MemorySweep is one agent's consolidation sweep state (design D13): the
// ingest watermark plus the last run's heartbeat. Absence of a row is a
// normal state — the agent's memory has never been swept. Outcome and Counts
// are sweep-owned (a short outcome token and an opaque JSON counts document);
// the store does not interpret them.
type MemorySweep struct {
	AgentID string
	// Watermark is the ingest point the next sweep reads changes since;
	// zero until the first successful ingest.
	Watermark time.Time
	// LastStart and LastFinish are nil before the first sweep attempt.
	LastStart  *time.Time
	LastFinish *time.Time
	Outcome    string
	Counts     string
}

// MemoryStore holds an agent's derived memory state (add-agent-memory design
// D1): recall traces, per-note retention state, promotion staging, and sweep
// state. Every row is rebuildable from the workspace memory files plus the
// traces and is removed with its agent; no method stores note content.
type MemoryStore interface {
	// AppendTrace records one memory_search invocation, assigning ID and
	// stamping CreatedAt (UTC) when empty. An unknown agent_id violates the
	// foreign key, wrapped with the driver's error.
	AppendTrace(ctx context.Context, t MemoryTrace) (MemoryTrace, error)
	// ListTracesSince returns the agent's traces created at or after since,
	// oldest first. The result is never nil.
	ListTracesSince(ctx context.Context, agentID string, since time.Time) ([]MemoryTrace, error)
	// UpsertNoteState writes one note's retention state, keyed by
	// (agent, name): the row is written as provided, with a zero FirstSeen
	// stamped to now — read-modify-write callers preserve it naturally.
	UpsertNoteState(ctx context.Context, st MemoryNoteState) error
	// ListNoteStates returns every retention row of the agent ordered by
	// note name. The result is never nil.
	ListNoteStates(ctx context.Context, agentID string) ([]MemoryNoteState, error)
	// UpsertStaging writes one promotion candidate, keyed by (agent,
	// source ref): the row is written as provided, with a zero FirstStaged
	// stamped to now — read-modify-write callers preserve it naturally.
	UpsertStaging(ctx context.Context, st MemoryStaging) error
	// ListStaging returns every staging row of the agent ordered by source
	// reference. The result is never nil.
	ListStaging(ctx context.Context, agentID string) ([]MemoryStaging, error)
	// ExpireStaging removes the agent's staging rows first staged before
	// the cutoff (design D8's 30-day expiry) and returns how many went.
	ExpireStaging(ctx context.Context, agentID string, before time.Time) (int, error)
	// GetSweep returns the agent's sweep state; found is false when the
	// agent has never been swept.
	GetSweep(ctx context.Context, agentID string) (MemorySweep, bool, error)
	// PutSweep replaces the agent's sweep state (the heartbeat is a
	// whole-row rewrite); the row is created on first write.
	PutSweep(ctx context.Context, sw MemorySweep) error
}
