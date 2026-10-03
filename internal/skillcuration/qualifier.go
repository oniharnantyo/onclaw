package skillcuration

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/ingest"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ConfigSource resolves the workspace's curation config at job time — the
// composition root wires ConfigForWorkspace over the tool-settings store, so
// a settings edit applies on the next job without rebuilding the consumer
// (the memory posture-func precedent).
type ConfigSource func(ctx context.Context, workspaceID string) Config

// Qualifier is the skill-curation consumer (add-skill-curation-from-traces
// 3.1, design D2): at each turn-end job it scans the run's transcript window
// and applies the five hard gates — computed entirely from existing
// persisted payload fields, with zero model calls, ever. The two-tier rule
// governs the outcome: EVERY run (gate-passing or not) is indexed into its
// similarity cluster as a membership row; ONLY gate-passing runs proceed to
// the candidate signal (the chip). A run failing any gate triggers nothing.
//
// The qualifier is fail-soft by construction: every failure returns as an
// error the ingest worker logs under this consumer's name — the raw session
// events stay untouched, the run that produced the turn is unaffected, and
// memory ingestion (the other consumer) still receives the same job.
//
// Import discipline: this package never imports internal/agents (the
// runner-side wiring that appends the chip event is task 7's, cloned from
// the runner's AppendMemoryChip) — the windows are read straight from the
// session-event store through the store port.
type Qualifier struct {
	sessions store.SessionEventStore
	config   ConfigSource
	clusters store.SkillCandidateStore
	chip     CandidateChipSink
	log      *slog.Logger
}

// QualifierOption configures the qualifier; every knob has a safe default.
type QualifierOption func(*Qualifier)

// WithCandidateChipSink wires the chip emission (the memory WithChipSink
// precedent: an optional capability, never a nil-able dependency). The
// composition root applies it only when the session-event seam exists;
// unwired, qualification still indexes and triggers proposal work — only
// the transcript signal is dropped.
func WithCandidateChipSink(fn CandidateChipSink) QualifierOption {
	return func(q *Qualifier) {
		q.chip = fn
	}
}

// NewQualifier constructs the qualifier from its granular dependencies:
// the raw session-event store the windows load from, the workspace config
// source, the candidate store the cluster memberships persist through, and
// the logger. The chip sink is the one optional capability, wired through
// WithCandidateChipSink (the memory pipeline's WithChipSink shape).
func NewQualifier(sessions store.SessionEventStore, config ConfigSource, clusters store.SkillCandidateStore, log *slog.Logger, opts ...QualifierOption) *Qualifier {
	q := &Qualifier{
		sessions: sessions,
		config:   config,
		clusters: clusters,
		log:      log,
	}
	for _, opt := range opts {
		opt(q)
	}
	return q
}

// The qualifier satisfies the ingest seam's consumer contract.
var _ ingest.Consumer = (*Qualifier)(nil)

// gateReport is the five-gate evaluation for one run — each field names the
// gate that must hold (all true = qualifies). Evaluated in spec order;
// no short-circuit so a report can name every failed gate in the log.
type gateReport struct {
	Completed      bool // ① the run finished successfully (job.Status)
	MinToolCalls   bool // ② total tool calls >= config minimum
	MinDistinct    bool // ③ distinct tools >= config minimum
	HasRecovery    bool // ④ at least one error→success recovery on a tool
	FinalAssistant bool // ⑤ the run ended with a final assistant message
}

// passed reports that every gate held.
func (g gateReport) passed() bool {
	return g.Completed && g.MinToolCalls && g.MinDistinct && g.HasRecovery && g.FinalAssistant
}

// failedGates names the failed gates for the log, in spec order.
func (g gateReport) failedGates() []string {
	var failed []string
	if !g.Completed {
		failed = append(failed, "completed")
	}
	if !g.MinToolCalls {
		failed = append(failed, "min_tool_calls")
	}
	if !g.MinDistinct {
		failed = append(failed, "min_distinct_tools")
	}
	if !g.HasRecovery {
		failed = append(failed, "recovery")
	}
	if !g.FinalAssistant {
		failed = append(failed, "final_assistant")
	}
	return failed
}

// evaluateGates applies the five hard gates over the run's tally with the
// workspace's thresholds (spec: "Qualification gates" — evaluated from
// existing session-event payloads without any model call).
func evaluateGates(job ingest.Job, tally runTally, cfg Config) gateReport {
	return gateReport{
		Completed:      job.Status == string(domain.SkillClusterRunCompleted),
		MinToolCalls:   tally.ToolCalls >= cfg.MinToolCalls,
		MinDistinct:    tally.DistinctTools >= cfg.MinDistinctTools,
		HasRecovery:    tally.Recoveries >= 1,
		FinalAssistant: tally.FinalAssistant,
	}
}

// Ingest runs one turn-end job: load the run's window, tally it, index the
// cluster membership (always — the two-tier rule), and on qualification emit
// the candidate signal through the wired sink. The method makes no model
// call on any path.
func (q *Qualifier) Ingest(ctx context.Context, job ingest.Job) error {
	cfg := q.config(ctx, job.WorkspaceID)
	tally, err := q.windowTally(ctx, job)
	if err != nil {
		return err
	}

	report := evaluateGates(job, tally, cfg)
	qualifying := report.passed()
	cluster := ClusterKey(job.WorkspaceID, job.AgentID, tally.OrderedTools)

	// The decision log (one structured line per evaluation, Info — the
	// failing path used to log at Debug, invisible at the server's default
	// level, so runs that failed the gates vanished silently). Carries the
	// outcome, the gates that failed (empty when it qualified), and the
	// tally against the workspace's thresholds, so "why is no skill being
	// proposed" is answerable from the log alone.
	q.log.InfoContext(ctx, "skillcuration: skill qualification decision",
		"workspace_id", job.WorkspaceID, "agent_id", job.AgentID,
		"session_id", job.SessionID, "turn_id", job.TurnID,
		"cluster_id", cluster, "qualified", qualifying,
		"failed_gates", report.failedGates(), "run_status", job.Status,
		"tool_calls", tally.ToolCalls, "min_tool_calls", cfg.MinToolCalls,
		"distinct_tools", tally.DistinctTools, "min_distinct_tools", cfg.MinDistinctTools,
		"recoveries", tally.Recoveries, "final_assistant", tally.FinalAssistant)

	// Tier one — index (every run, pass or fail; failed runs join as
	// non-qualifying contrast evidence, spec: "Failed runs join clusters
	// without triggering").
	membership := &domain.SkillClusterRun{
		WorkspaceID:        job.WorkspaceID,
		AgentID:            job.AgentID,
		ClusterID:          cluster,
		SessionID:          job.SessionID,
		TurnID:             job.TurnID,
		RunStatus:          domain.SkillClusterRunStatus(job.Status),
		Origin:             job.Origin,
		Qualifying:         qualifying,
		ToolCalls:          tally.ToolCalls,
		DistinctTools:      tally.DistinctTools,
		Recoveries:         tally.Recoveries,
		ErrorResults:       tally.ErrorResults,
		TotalToolLatencyMS: tally.TotalToolLatency.Milliseconds(),
		WindowEndEventID:   tally.WindowEndEventID,
	}
	if err := q.clusters.IndexClusterRun(ctx, membership); err != nil {
		return err
	}

	// Tier two — trigger (only gate-passing runs; a failed gate spends no
	// model call and schedules no candidate work — the decision log above
	// already named why).
	if !qualifying {
		return nil
	}

	if q.chip != nil {
		q.chip(ctx, job, SkillCandidatePayload{
			WorkspaceID: job.WorkspaceID,
			AgentID:     job.AgentID,
			SessionID:   job.SessionID,
			RunID:       job.TurnID,
			Cluster:     cluster,
		})
	}
	return nil
}

// HasConvergence reports the "Cluster gate before drafting" read for one
// cluster: the store's qualifying-run count meets the workspace's
// ClusterMinimum. The proposer (task 5) calls this before drafting; a
// first-ever qualifying run holds the gate open without proposing
// (count 1 < default minimum 2).
func (q *Qualifier) HasConvergence(ctx context.Context, workspaceID, clusterID string) (bool, error) {
	count, err := ClusterQualifyingCount(ctx, q.clusters, workspaceID, clusterID)
	if err != nil {
		return false, err
	}
	return ClusterGateOpen(count, q.config(ctx, workspaceID).ClusterMinimum), nil
}

// windowTally loads the run's transcript window and tallies it. The window
// is the triggering turn's persisted events (the ingest job's turn id is
// the runner-owned scope every event of the turn carries); a window with no
// row carrying the turn id (a terminal path that minted a fresh turn id, a
// stale pointer) falls back to the whole session log — the material is
// still attributable and must not be silently skipped (the memory
// turnEvents precedent, mirrored).
func (q *Qualifier) windowTally(ctx context.Context, job ingest.Job) (runTally, error) {
	events, err := q.sessions.LoadEvents(ctx, store.LoadSessionEventsParams{
		WorkspaceID: job.WorkspaceID,
		SessionID:   job.SessionID,
	})
	if err != nil {
		return runTally{}, fmt.Errorf("skillcuration: load run window: %w", err)
	}
	return tallyRun(turnEvents(events, job.TurnID)), nil
}

// turnEvents narrows the window to the triggering turn's rows; a window
// with no row carrying the turn id yields the whole window.
func turnEvents(events []domain.SessionEvent, turnID string) []domain.SessionEvent {
	var turn []domain.SessionEvent
	for _, ev := range events {
		if ev.TurnID == turnID {
			turn = append(turn, ev)
		}
	}
	if len(turn) > 0 {
		return turn
	}
	return events
}
