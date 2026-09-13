package agents

// Per-turn Langfuse trace export (integrate-langfuse-tracing tasks 2.1–2.3,
// design D1–D3/D5). The capability exists only when the composition root
// applied WithTraceHandler with a non-nil handler (D1): an unconfigured
// runner minting, pinning, and exporting nothing — every seam below short
// circuits on the nil handler.
//
// The trace id is pinned, not captured: the exported trace carries exactly
// observability.TraceIDForRun(turnID) because ApplyTraceContext stamps the
// id onto the run context via langfuse.WithID, and the same pure mapping is
// what run-record persistence stores (D3). Consequences:
//   - a model call retried inside the turn, and an approval resume of the
//     interrupted turn, land inside the same trace (spec: a retried turn's
//     attempts stay inside one trace);
//   - the deterministic upstream sampler and the local SampledIn gate agree
//     for every id, so a sampled-out turn exports nothing AND persists no
//     trace id — a persisted id always targets a real trace (D5).
//
// Turn-id pinning (the ADK seam): the ADK mints a run's turn id from the
// session status_running control event it writes at run start
// (state.turnID = runningEvent.EventID). traceSessionConfig installs an
// EventIDGenerator — the ADK's sanctioned "map a draft to a business-side
// ID" hook — that pins exactly that draft to the runner-minted id, keeping
// DefaultSessionEventIDGenerator for every other event. The transcript's
// turn id, the exported trace id, and the persisted id therefore all derive
// from one value.

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/observability"
)

// runTrace carries one turn's trace coordinates from run start to its
// terminal events: the runner-minted turn id (pinned into the ADK session),
// the derived trace id, and the sampling decision that gates persistence.
type runTrace struct {
	turnID    string
	traceID   string
	sampledIn bool
}

// persistedID returns the trace id terminal events carry: the pinned id only
// when the turn sampled in (D5). A nil trace — capability absent — persists
// nothing.
func (t *runTrace) persistedID() string {
	if t == nil || !t.sampledIn {
		return ""
	}
	return t.traceID
}

// WithTraceHandler attaches the optional Langfuse export capability
// (integrate-langfuse-tracing D1): h observes the turn's model calls as
// generations and tool calls as spans on the eino callback chain
// (adk.WithCallbacks); sampleRate drives the local SampledIn persistence
// gate and must be the TraceHandler's configured rate so persistence and
// export agree. Applying the option with a nil handler is a no-op — the
// unconfigured runner is byte-identical to a pre-tracing deployment.
func WithTraceHandler(h callbacks.Handler, sampleRate float64) RunnerOption {
	return func(r *Runner) {
		if h == nil {
			return
		}
		r.traceHandler = h
		r.traceSampleRate = sampleRate
	}
}

// tracingEnabled reports whether the export capability was wired. The nil
// handler is the absent capability (D1), not a defensive guard: without the
// option no trace work happens at all.
func (r *Runner) tracingEnabled() bool {
	return r.traceHandler != nil
}

// newTurnTrace derives one turn's trace coordinates from a minted turn id
// and the configured sample rate (D3/D5).
func newTurnTrace(turnID string, sampleRate float64) runTrace {
	traceID := observability.TraceIDForRun(turnID)
	return runTrace{
		turnID:    turnID,
		traceID:   traceID,
		sampledIn: observability.SampledIn(traceID, sampleRate),
	}
}

// beginTurnTrace mints the run's turn id and derives its trace coordinates.
func (r *Runner) beginTurnTrace() runTrace {
	return newTurnTrace(r.mintTurnID(), r.traceSampleRate)
}

// rememberTurnTrace stores the run's trace coordinates for a later approval
// Resume of the same session — the resumed turn continues the interrupted
// turn's checkpoint and therefore its turn id and trace (spec: retry stays
// inside the trace). Same lifecycle as the per-run hook chain (D4): replaced
// by the next run on the session, forgotten at terminal outcomes; a resume
// in a fresh process finds nothing and resolves a new trace.
func (r *Runner) rememberTurnTrace(key RunKey, trace runTrace) {
	r.traceMu.Lock()
	defer r.traceMu.Unlock()
	if r.traceRuns == nil {
		r.traceRuns = make(map[RunKey]runTrace)
	}
	r.traceRuns[key] = trace
}

// reuseTurnTrace returns the remembered trace of the interrupted run.
func (r *Runner) reuseTurnTrace(key RunKey) (runTrace, bool) {
	r.traceMu.Lock()
	defer r.traceMu.Unlock()
	trace, ok := r.traceRuns[key]
	return trace, ok
}

// forgetTurnTrace drops the run's trace coordinates at a terminal outcome.
func (r *Runner) forgetTurnTrace(key RunKey) {
	r.traceMu.Lock()
	defer r.traceMu.Unlock()
	delete(r.traceRuns, key)
}

// traceSessionConfig pins the ADK's turn id to the runner-minted one: the
// status_running control event the ADK writes at run start is its sole
// turn-id mint, and the EventIDGenerator is the sanctioned seam for mapping
// a draft onto a business-side id. Every other event keeps the default
// generator, so persisted event ids are unchanged in shape and uniqueness.
func traceSessionConfig(turnID string) *adk.SessionConfig[*schema.AgenticMessage] {
	return &adk.SessionConfig[*schema.AgenticMessage]{
		EventIDGenerator: func(ctx context.Context, event *adk.SessionEvent[*schema.AgenticMessage]) (string, error) {
			if event != nil && event.Kind == adk.SessionEventSessionStatusRunning {
				return turnID, nil
			}
			return adk.DefaultSessionEventIDGenerator(ctx, event)
		},
	}
}

// applyTurnTrace stamps the run context with the turn's attribution (D2):
// session/user identity, origin/agent/workspace tags, turn/workspace/agent/
// origin metadata, the pinned trace id, and the trace name (schedule name
// for scheduler fires, the input's first line otherwise). Runs for every
// traced turn regardless of the sampling decision — the pinned id makes the
// upstream sampler drop a sampled-out turn's whole event set atomically.
func (r *Runner) applyTurnTrace(ctx context.Context, req ExecRequest, trace runTrace) context.Context {
	return observability.ApplyTraceContext(ctx, observability.TraceContext{
		SessionID:    req.SessionID,
		UserID:       req.UserID,
		WorkspaceID:  req.WorkspaceID,
		AgentID:      req.AgentID,
		TurnID:       trace.turnID,
		Origin:       req.Origin,
		Input:        req.Input,
		ScheduleName: req.ScheduleName,
	})
}

// traceRunOptions builds the adk run options that attach the export handler
// to the turn's callback chain (task 2.1). Empty when the capability is
// absent — the composed stack runs exactly as before.
func (r *Runner) traceRunOptions() []adk.AgentRunOption {
	if r.traceHandler == nil {
		return nil
	}
	return []adk.AgentRunOption{adk.WithCallbacks(r.traceHandler)}
}
