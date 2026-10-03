package domain

import (
	"fmt"
	"time"
)

// SkillClusterRunStatus is the run-finish attribution carried on a cluster
// membership row. The values mirror the run statuses the turn-ingest job
// carries ("completed"/"failed") — both statuses index; only completed runs
// can qualify.
type SkillClusterRunStatus string

const (
	// SkillClusterRunCompleted is a run that finished its turn successfully.
	SkillClusterRunCompleted SkillClusterRunStatus = "completed"
	// SkillClusterRunFailed is a run whose turn ended in an error. Failed
	// runs join clusters as non-qualifying contrast evidence (spec: "Failed
	// runs join clusters without triggering").
	SkillClusterRunFailed SkillClusterRunStatus = "failed"
)

// ValidSkillClusterRunStatus reports whether status is one of the two run
// statuses a membership row may carry.
func ValidSkillClusterRunStatus(status SkillClusterRunStatus) bool {
	switch status {
	case SkillClusterRunCompleted, SkillClusterRunFailed:
		return true
	}
	return false
}

// SkillClusterRun is one run's membership in a similarity cluster
// (add-skill-curation-from-traces 3.1): the two-tier rule persists every
// ingested run — gate-passing or not — as a row here, so clusters survive
// restarts and the proposer's cluster gate counts qualifying members from
// the store, never from process memory. The row is the evidence pointer
// (session/turn + window end event) plus the structured tally the gates
// computed, so later stages (wiki sampling, scoring, contrast rendering)
// never re-scan raw events. One row per run: re-indexing the same
// (workspace, session, turn) rewrites it.
type SkillClusterRun struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	// ClusterID is the v1 deterministic shape key (workspace + agent +
	// ordered tool-name signature hashed). A store-level string, not a
	// foreign key: clusters live in the curation pipeline, not in a table.
	ClusterID string `json:"cluster_id"`
	SessionID string `json:"session_id"`
	// TurnID is the run: the ingest job's turn id, and the transcript
	// window's turn scope.
	TurnID    string               `json:"turn_id"`
	RunStatus SkillClusterRunStatus `json:"run_status"`
	// Origin is the run's origin literal ("user", "scheduler", ...); the
	// nightly scorer down-weights unattended origins.
	Origin string `json:"origin"`
	// Qualifying marks a gate-passing run — a cluster member that counted
	// toward the proposal minimum. Failed and gate-failing runs stay
	// non-qualifying members (contrast evidence, never triggers).
	Qualifying bool `json:"qualifying"`
	// The structured tally the qualifier computed over the run's window,
	// stored so scoring and sampling never re-parse raw events.
	ToolCalls          int   `json:"tool_calls"`
	DistinctTools      int   `json:"distinct_tools"`
	Recoveries         int   `json:"recoveries"`
	ErrorResults       int   `json:"error_results"`
	TotalToolLatencyMS int64 `json:"total_tool_latency_ms"`
	// WindowEndEventID cites the last session event of the run's window —
	// the evidence pointer back into session_events (every stored artifact
	// carries its evidence chain from birth, design Goals).
	WindowEndEventID string    `json:"window_end_event_id,omitempty"`
	IndexedAt        time.Time `json:"indexed_at"`
}

// Validate checks the membership row's write shape: the scope tuple and
// cluster linkage are present and the run status is a known value.
func (r *SkillClusterRun) Validate() error {
	if r == nil {
		return ErrInvalid
	}
	if r.WorkspaceID == "" || r.AgentID == "" || r.ClusterID == "" {
		return fmt.Errorf("%w: skill cluster run needs workspace, agent, and cluster", ErrInvalid)
	}
	if r.SessionID == "" || r.TurnID == "" {
		return fmt.Errorf("%w: skill cluster run needs session and turn", ErrInvalid)
	}
	if !ValidSkillClusterRunStatus(r.RunStatus) {
		return fmt.Errorf("%w: unknown skill cluster run status %q", ErrInvalid, r.RunStatus)
	}
	return nil
}
