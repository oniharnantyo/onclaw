package skillcuration

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/ingest"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ---------------------------------------------------------------------------
// Synthetic event windows. Every fixture serializes through the same
// HumanReadableSerializer the ADK adapter persists with, so the tally reads
// exactly what production rows carry (the memory worker_test fixture
// pattern).
// ---------------------------------------------------------------------------

const (
	fixtureWorkspace = "ws-qual"
	fixtureAgent     = "agt-qual"
	fixtureSession   = "sess-qual"
)

var fixtureBase = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

// discardLog is the tests' silent logger.
func discardLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

// toolCall describes one call in a synthetic window.
type toolCall struct {
	name   string
	callID string
	err    bool // the end span carries Status "error"
}

// buildWindow renders one turn: the user prompt, the tool calls in order
// (each spanning two seconds, errors flagged on the end span), and the
// optional final assistant message. Event ids are "<prefix>-evt-N" — unique
// per window so several turns can share a session without the store's
// idempotent append dropping rows.
func buildWindow(t *testing.T, prefix, turnID string, calls []toolCall, finalAssistant bool) []domain.SessionEvent {
	t.Helper()
	seq := int64(0)
	at := fixtureBase
	rows := make([]domain.SessionEvent, 0, 2*len(calls)+2)

	add := func(ev *adk.SessionEvent[*schema.AgenticMessage]) {
		payload, err := (&schema.HumanReadableSerializer{}).Marshal(ev)
		if err != nil {
			t.Fatalf("serialize session event: %v", err)
		}
		seq++
		at = at.Add(time.Second)
		kind := ev.Kind
		if kind == "" {
			kind = adk.SessionEventMessage
		}
		rows = append(rows, domain.SessionEvent{
			SessionID:   fixtureSession,
			EventID:     fmt.Sprintf("%s-evt-%d", prefix, seq),
			TurnID:      ev.TurnID,
			Seq:         seq,
			Kind:        string(kind),
			Payload:     payload,
			OccurredAt:  ev.Timestamp,
			WorkspaceID: fixtureWorkspace,
		})
	}

	add(&adk.SessionEvent[*schema.AgenticMessage]{
		TurnID:    turnID,
		Timestamp: at.Add(time.Second),
		Message:   schema.UserAgenticMessage("run the procedure"),
	})
	for _, call := range calls {
		startAt := at.Add(time.Second)
		endAt := startAt.Add(2 * time.Second)
		startID := fmt.Sprintf("%s-evt-%d", prefix, seq+1) // the id add() will assign next
		add(&adk.SessionEvent[*schema.AgenticMessage]{
			TurnID:    turnID,
			Timestamp: startAt,
			Kind:      adk.SessionEventSpanToolCallStart,
			Span: &adk.SpanEvent{
				Kind:      adk.SpanKindTool,
				StartedAt: startAt,
				Tool:      &adk.ToolSpanMeta{ToolUseID: call.callID, Name: call.name},
			},
		})
		end := &adk.SessionEvent[*schema.AgenticMessage]{
			TurnID:    turnID,
			Timestamp: endAt,
			Kind:      adk.SessionEventSpanToolCallEnd,
			Span: &adk.SpanEvent{
				Kind:    adk.SpanKindTool,
				EndedAt: endAt,
				Tool: &adk.ToolSpanMeta{
					ToolUseID:            call.callID,
					Name:                 call.name,
					ToolCallStartEventID: startID,
				},
			},
		}
		if call.err {
			end.Span.Status = "error"
			end.Span.Err = "boom: " + call.callID
		}
		add(end)
	}
	if finalAssistant {
		add(&adk.SessionEvent[*schema.AgenticMessage]{
			TurnID:    turnID,
			Timestamp: at.Add(time.Second),
			Message: &schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					{AssistantGenText: &schema.AssistantGenText{Text: "done — the procedure completed."}},
				},
			},
		})
	}
	return rows
}

// qualifyingCalls is a 9-call window across 4 distinct tools with exactly
// one recovery (http.request fails once, then succeeds) — past every
// default gate.
func qualifyingCalls() []toolCall {
	ok := func(name, id string) toolCall { return toolCall{name: name, callID: id} }
	return []toolCall{
		ok("grafana.query", "c1"),
		ok("files.write", "c2"),
		{name: "http.request", callID: "c3", err: true},
		ok("http.request", "c4"), // the recovery
		ok("grafana.annotate", "c5"),
		ok("grafana.query", "c6"),
		ok("files.write", "c7"),
		ok("http.request", "c8"),
		ok("grafana.query", "c9"),
	}
}

// noRecoveryCalls is a 15-call window across 5 tools where every call
// succeeds first try (the spec's "Recovery shape is required" scenario,
// scaled up).
func noRecoveryCalls() []toolCall {
	tools := []string{"a.query", "b.write", "c.request", "d.annotate", "e.render"}
	calls := make([]toolCall, 0, 15)
	for i := 0; i < 15; i++ {
		calls = append(calls, toolCall{name: tools[i%len(tools)], callID: fmt.Sprintf("c%d", i+1)})
	}
	return calls
}

// boundaryCalls renders n successful-tool calls cycling four distinct tools
// with one same-tool recovery up front, so only the tool-call-count gate can
// decide the outcome.
func boundaryCalls(n int) []toolCall {
	tools := []string{"a.query", "b.write", "c.request", "d.annotate"}
	calls := make([]toolCall, 0, n)
	for i := 0; i < n; i++ {
		call := toolCall{name: tools[i%len(tools)], callID: fmt.Sprintf("c%d", i+1)}
		if i == 0 {
			call.err = true // tools[0] fails...
		}
		calls = append(calls, call)
	}
	if n >= 2 {
		// ...and tools[0] retries first: a same-tool recovery, always.
		calls[1] = toolCall{name: tools[0], callID: "c-retry"}
	}
	return calls
}

// newFixture creates the fake store with the workspace and agent the
// cluster-run writes require, the qualifier over cfg, and a recording chip
// sink.
func newFixture(t *testing.T, cfg Config) (context.Context, store.Store, *Qualifier, *[]SkillCandidatePayload) {
	return newFixtureWithLog(t, cfg, discardLog())
}

// newFixtureWithLog is newFixture with the test's own logger — the decision-
// log test hands a capturing handler in.
func newFixtureWithLog(t *testing.T, cfg Config, log *slog.Logger) (context.Context, store.Store, *Qualifier, *[]SkillCandidatePayload) {
	t.Helper()
	ctx := context.Background()
	st := storefake.New()
	if err := st.Workspaces().Create(ctx, &domain.Workspace{ID: fixtureWorkspace, Slug: "qual", Name: "Qual"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := st.Agents().Create(ctx, &domain.Agent{ID: fixtureAgent, WorkspaceID: fixtureWorkspace, Slug: "atlas", Name: "Atlas"}); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	sink := &[]SkillCandidatePayload{}
	q := NewQualifier(st.SessionEvents(), func(context.Context, string) Config { return cfg }, st.SkillCandidates(), log,
		WithCandidateChipSink(func(_ context.Context, _ ingest.Job, payload SkillCandidatePayload) {
			*sink = append(*sink, payload)
		}))
	return ctx, st, q, sink
}

func fixtureJob(status, turnID string) ingest.Job {
	return ingest.Job{
		WorkspaceID:       fixtureWorkspace,
		AgentID:           fixtureAgent,
		UserID:            "usr-1",
		SessionID:         fixtureSession,
		TurnID:            turnID,
		Origin:            ingest.OriginUser,
		HumanParticipants: 1,
		Status:            status,
	}
}

// membershipByTurn finds the job's membership row. The store exposes
// cluster-scoped listings, so the test recomputes the cluster key with the
// same function the qualifier used — the key over the job turn's tallied
// tool sequence (identical narrowing semantics, including the stale-turn
// fallback to the whole window).
func membershipByTurn(t *testing.T, st store.Store, job ingest.Job) domain.SkillClusterRun {
	t.Helper()
	rows, err := st.SessionEvents().LoadEvents(context.Background(), store.LoadSessionEventsParams{
		WorkspaceID: job.WorkspaceID,
		SessionID:   job.SessionID,
	})
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	key := ClusterKey(job.WorkspaceID, job.AgentID, tallyRun(turnEvents(rows, job.TurnID)).OrderedTools)
	runs, err := st.SkillCandidates().ListClusterRunsByCluster(context.Background(), job.WorkspaceID, key)
	if err != nil {
		t.Fatalf("list cluster runs: %v", err)
	}
	for _, run := range runs {
		if run.TurnID == job.TurnID && run.SessionID == job.SessionID {
			return run
		}
	}
	t.Fatalf("no membership row for turn %q under cluster %q", job.TurnID, key)
	return domain.SkillClusterRun{}
}

// ---------------------------------------------------------------------------
// Gate evaluation (pure table over the tally).
// ---------------------------------------------------------------------------

func TestEvaluateGates(t *testing.T) {
	cfg := DefaultConfig()
	completed := ingest.Job{Status: "completed"}
	failed := ingest.Job{Status: "failed"}

	passing := tallyRun(buildWindow(t, "g", "turn-1", qualifyingCalls(), true))
	trivial := tallyRun(buildWindow(t, "g", "turn-1", []toolCall{
		{name: "a.query", callID: "c1"},
		{name: "b.write", callID: "c2"},
	}, true))
	allFirstTry := tallyRun(buildWindow(t, "g", "turn-1", noRecoveryCalls(), true))
	noFinalAssistant := tallyRun(buildWindow(t, "g", "turn-1", qualifyingCalls(), false))
	// http.request errors once and is never retried: every later success
	// lands on a different tool name, so no same-tool recovery exists.
	errorNeverRetried := tallyRun(buildWindow(t, "g", "turn-1", func() []toolCall {
		calls := qualifyingCalls()
		calls[3] = toolCall{name: "grafana.annotate", callID: "c4"}
		calls[7] = toolCall{name: "files.write", callID: "c8"}
		return calls
	}(), true))

	tests := []struct {
		name  string
		job   ingest.Job
		tally runTally
		want  bool
		fails []string
	}{
		{"qualifying run passes all five gates", completed, passing, true, nil},
		{"trivial two-call run fails minimums and recovery", completed, trivial, false, []string{"min_tool_calls", "recovery"}},
		{"all-first-try success lacks recovery", completed, allFirstTry, false, []string{"recovery"}},
		{"failed run fails the completed gate", failed, passing, false, []string{"completed"}},
		{"missing final assistant fails gate five", completed, noFinalAssistant, false, []string{"final_assistant"}},
		{"error followed by a different tool is no recovery", completed, errorNeverRetried, false, []string{"recovery"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := evaluateGates(tt.job, tt.tally, cfg)
			if report.passed() != tt.want {
				t.Errorf("passed = %v (failed gates %v), want %v", report.passed(), report.failedGates(), tt.want)
			}
			if got := report.failedGates(); len(got) != len(tt.fails) {
				t.Errorf("failed gates = %v, want %v", got, tt.fails)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Recovery matcher (unit, straight over the tally).
// ---------------------------------------------------------------------------

func TestRecoveryMatcher(t *testing.T) {
	tests := []struct {
		name      string
		calls     []toolCall
		recover   int
		errResult int
	}{
		{
			name: "error then success on the same tool recovers once",
			calls: []toolCall{
				{name: "http.request", callID: "c1", err: true},
				{name: "http.request", callID: "c2"},
			},
			recover:   1,
			errResult: 1,
		},
		{
			name: "error followed by a different tool recovers none",
			calls: []toolCall{
				{name: "http.request", callID: "c1", err: true},
				{name: "files.write", callID: "c2"},
			},
			recover:   0,
			errResult: 1,
		},
		{
			name: "repeated errors then one success recover once",
			calls: []toolCall{
				{name: "http.request", callID: "c1", err: true},
				{name: "http.request", callID: "c2", err: true},
				{name: "http.request", callID: "c3"},
			},
			recover:   1,
			errResult: 2,
		},
		{
			name: "two error-success cycles on one tool recover twice",
			calls: []toolCall{
				{name: "http.request", callID: "c1", err: true},
				{name: "http.request", callID: "c2"},
				{name: "http.request", callID: "c3", err: true},
				{name: "http.request", callID: "c4"},
			},
			recover:   2,
			errResult: 2,
		},
		{
			name: "success first try recovers none",
			calls: []toolCall{
				{name: "http.request", callID: "c1"},
				{name: "files.write", callID: "c2"},
			},
			recover: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tally := tallyRun(buildWindow(t, "rm", "turn-1", tt.calls, false))
			if tally.Recoveries != tt.recover {
				t.Errorf("Recoveries = %d, want %d", tally.Recoveries, tt.recover)
			}
			if tally.ErrorResults != tt.errResult {
				t.Errorf("ErrorResults = %d, want %d", tally.ErrorResults, tt.errResult)
			}
			if tally.ToolCalls != len(tt.calls) {
				t.Errorf("ToolCalls = %d, want %d", tally.ToolCalls, len(tt.calls))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// End-to-end Ingest over the fake store.
// ---------------------------------------------------------------------------

func TestQualifierIngest(t *testing.T) {
	t.Run("qualifying run indexes and emits the chip", func(t *testing.T) {
		ctx, st, q, sink := newFixture(t, DefaultConfig())
		rows := buildWindow(t, "q", "turn-1", qualifyingCalls(), true)
		if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, rows); err != nil {
			t.Fatalf("append events: %v", err)
		}
		job := fixtureJob("completed", "turn-1")

		if err := q.Ingest(ctx, job); err != nil {
			t.Fatalf("Ingest: %v", err)
		}

		wantCluster := ClusterKey(fixtureWorkspace, fixtureAgent, toolNames(qualifyingCalls()))
		run := membershipByTurn(t, st, job)
		if !run.Qualifying {
			t.Errorf("qualifying run indexed as non-qualifying: %+v", run)
		}
		if run.ClusterID != wantCluster {
			t.Errorf("ClusterID = %q, want %q", run.ClusterID, wantCluster)
		}
		if run.RunStatus != domain.SkillClusterRunCompleted {
			t.Errorf("RunStatus = %q, want completed", run.RunStatus)
		}
		if run.ToolCalls != 9 || run.DistinctTools != 4 || run.Recoveries != 1 || run.ErrorResults != 1 {
			t.Errorf("tally lost in the row: %+v", run)
		}
		if run.TotalToolLatencyMS <= 0 {
			t.Errorf("TotalToolLatencyMS = %d, want the span pairs' two seconds each", run.TotalToolLatencyMS)
		}
		if run.WindowEndEventID != rows[len(rows)-1].EventID {
			t.Errorf("WindowEndEventID = %q, want the final assistant's %q", run.WindowEndEventID, rows[len(rows)-1].EventID)
		}
		if len(*sink) != 1 {
			t.Fatalf("chip emissions = %d, want 1", len(*sink))
		}
		payload := (*sink)[0]
		if payload.WorkspaceID != job.WorkspaceID || payload.AgentID != job.AgentID ||
			payload.SessionID != job.SessionID || payload.RunID != job.TurnID {
			t.Errorf("chip coordinates wrong: %+v", payload)
		}
		if payload.Cluster != wantCluster {
			t.Errorf("chip cluster = %q, want %q", payload.Cluster, wantCluster)
		}
		if payload.SkillName != "" {
			t.Errorf("SkillName must be empty at qualification time, got %q", payload.SkillName)
		}
	})

	t.Run("trivial two-call run indexes without triggering", func(t *testing.T) {
		ctx, st, q, sink := newFixture(t, DefaultConfig())
		if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, buildWindow(t, "q", "turn-1", []toolCall{
			{name: "a.query", callID: "c1"},
			{name: "b.write", callID: "c2"},
		}, true)); err != nil {
			t.Fatalf("append events: %v", err)
		}

		if err := q.Ingest(ctx, fixtureJob("completed", "turn-1")); err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		if len(*sink) != 0 {
			t.Errorf("trivial run emitted %d chips, want 0", len(*sink))
		}
		run := membershipByTurn(t, st, fixtureJob("completed", "turn-1"))
		if run.Qualifying {
			t.Errorf("trivial run indexed as qualifying: %+v", run)
		}
	})

	t.Run("fifteen calls across five tools all first-try success does not qualify", func(t *testing.T) {
		ctx, st, q, sink := newFixture(t, DefaultConfig())
		if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, buildWindow(t, "q", "turn-1", noRecoveryCalls(), true)); err != nil {
			t.Fatalf("append events: %v", err)
		}

		if err := q.Ingest(ctx, fixtureJob("completed", "turn-1")); err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		if len(*sink) != 0 {
			t.Errorf("no-recovery run emitted %d chips, want 0", len(*sink))
		}
		run := membershipByTurn(t, st, fixtureJob("completed", "turn-1"))
		if run.Qualifying || run.ToolCalls != 15 || run.DistinctTools != 5 {
			t.Errorf("run should index as a non-qualifying 15-call 5-tool member: %+v", run)
		}
	})

	t.Run("threshold config change 8 to 20 flips a nine-call run", func(t *testing.T) {
		ctx, st, q, sink := newFixture(t, DefaultConfig())
		if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, buildWindow(t, "q", "turn-1", qualifyingCalls(), true)); err != nil {
			t.Fatalf("append events: %v", err)
		}
		job := fixtureJob("completed", "turn-1")

		if err := q.Ingest(ctx, job); err != nil {
			t.Fatalf("Ingest at 8: %v", err)
		}
		if len(*sink) != 1 {
			t.Fatalf("nine-call run should qualify at the default threshold, got %d chips", len(*sink))
		}

		strict := DefaultConfig()
		strict.MinToolCalls = 20
		strictQ := NewQualifier(st.SessionEvents(), func(context.Context, string) Config { return strict }, st.SkillCandidates(), discardLog())
		if err := strictQ.Ingest(ctx, job); err != nil {
			t.Fatalf("Ingest at 20: %v", err)
		}
		if len(*sink) != 1 {
			t.Errorf("raised threshold triggered again, want no new chip")
		}
		// The re-index rewrote the row in place (one row per run), now
		// non-qualifying under the strict threshold.
		run := membershipByTurn(t, st, job)
		if run.Qualifying {
			t.Errorf("nine-call run still qualifying at threshold 20: %+v", run)
		}
	})

	t.Run("boundary: exactly the minimum qualifies, one below does not", func(t *testing.T) {
		for _, tc := range []struct {
			calls     int
			qualifies bool
		}{
			{7, false},
			{8, true},
		} {
			t.Run(fmt.Sprintf("%d calls", tc.calls), func(t *testing.T) {
				ctx, st, q, sink := newFixture(t, DefaultConfig())
				if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, buildWindow(t, "q", "turn-1", boundaryCalls(tc.calls), true)); err != nil {
					t.Fatalf("append events: %v", err)
				}
				if err := q.Ingest(ctx, fixtureJob("completed", "turn-1")); err != nil {
					t.Fatalf("Ingest: %v", err)
				}
				got := len(*sink)
				if tc.qualifies && got != 1 {
					t.Errorf("boundary run should qualify, got %d chips", got)
				}
				if !tc.qualifies && got != 0 {
					t.Errorf("sub-boundary run should not qualify, got %d chips", got)
				}
			})
		}
	})

	t.Run("failed run joins the cluster without triggering", func(t *testing.T) {
		ctx, st, q, sink := newFixture(t, DefaultConfig())
		if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, buildWindow(t, "q", "turn-1", qualifyingCalls(), true)); err != nil {
			t.Fatalf("append events: %v", err)
		}

		if err := q.Ingest(ctx, fixtureJob("failed", "turn-1")); err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		if len(*sink) != 0 {
			t.Errorf("failed run emitted %d chips, want 0", len(*sink))
		}
		run := membershipByTurn(t, st, fixtureJob("failed", "turn-1"))
		if run.Qualifying {
			t.Errorf("failed run indexed as qualifying: %+v", run)
		}
		if run.RunStatus != domain.SkillClusterRunFailed {
			t.Errorf("RunStatus = %q, want failed", run.RunStatus)
		}
	})

	t.Run("tally scopes to the triggering turn", func(t *testing.T) {
		ctx, st, q, sink := newFixture(t, DefaultConfig())
		// A previous turn holds a qualifying-shaped window; the triggering
		// turn is trivial. Only the triggering turn may count.
		rows := buildWindow(t, "prev", "turn-0", qualifyingCalls(), true)
		rows = append(rows, buildWindow(t, "cur", "turn-1", []toolCall{
			{name: "a.query", callID: "t1"},
			{name: "b.write", callID: "t2"},
		}, true)...)
		for i := len(rows) - 1; i >= len(rows)-3; i-- { // the triggering turn sorts last
			rows[i].Seq += 100
		}
		if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, rows); err != nil {
			t.Fatalf("append events: %v", err)
		}

		if err := q.Ingest(ctx, fixtureJob("completed", "turn-1")); err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		if len(*sink) != 0 {
			t.Errorf("previous turn's qualifying shape leaked into this turn's tally")
		}
		run := membershipByTurn(t, st, fixtureJob("completed", "turn-1"))
		if run.Qualifying || run.ToolCalls != 2 {
			t.Errorf("turn scoping failed: %+v", run)
		}
	})

	t.Run("stale turn id falls back to the whole window", func(t *testing.T) {
		ctx, st, q, sink := newFixture(t, DefaultConfig())
		if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, buildWindow(t, "q", "turn-1", qualifyingCalls(), true)); err != nil {
			t.Fatalf("append events: %v", err)
		}

		// A terminal path that minted a fresh turn id: no row carries it.
		job := fixtureJob("completed", "turn-minted")
		if err := q.Ingest(ctx, job); err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		run := membershipByTurn(t, st, job)
		if !run.Qualifying {
			t.Errorf("fallback window should still be attributable and qualify: %+v", run)
		}
		if len(*sink) != 1 {
			t.Errorf("fallback qualification emitted %d chips, want 1", len(*sink))
		}
	})

	t.Run("unwired chip sink drops the signal quietly", func(t *testing.T) {
		ctx, st, _, _ := newFixture(t, DefaultConfig())
		q := NewQualifier(st.SessionEvents(), func(context.Context, string) Config { return DefaultConfig() }, st.SkillCandidates(), discardLog())
		if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, buildWindow(t, "q", "turn-1", qualifyingCalls(), true)); err != nil {
			t.Fatalf("append events: %v", err)
		}

		if err := q.Ingest(ctx, fixtureJob("completed", "turn-1")); err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		run := membershipByTurn(t, st, fixtureJob("completed", "turn-1"))
		if !run.Qualifying {
			t.Errorf("qualification must survive an unwired sink: %+v", run)
		}
	})

	t.Run("empty window indexes a non-qualifying member", func(t *testing.T) {
		ctx, st, q, sink := newFixture(t, DefaultConfig())
		if err := q.Ingest(ctx, fixtureJob("completed", "turn-1")); err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		if len(*sink) != 0 {
			t.Errorf("empty window emitted %d chips, want 0", len(*sink))
		}
		run := membershipByTurn(t, st, fixtureJob("completed", "turn-1"))
		if run.Qualifying || run.ToolCalls != 0 {
			t.Errorf("empty window must index as a non-qualifying member: %+v", run)
		}
	})

	t.Run("unresolvable store failure fails soft into the worker", func(t *testing.T) {
		ctx := context.Background()
		st := storefake.New()
		q := NewQualifier(st.SessionEvents(), func(context.Context, string) Config { return DefaultConfig() }, st.SkillCandidates(), discardLog())
		// No workspace row: the session-events read still succeeds with an
		// empty window, but the membership write cannot resolve the agent —
		// the error returns for the worker's fault-isolated log.
		err := q.Ingest(ctx, fixtureJob("completed", "turn-1"))
		if err == nil {
			t.Fatalf("Ingest on an unresolvable workspace should error for the worker to log")
		}
	})
}

// ---------------------------------------------------------------------------
// Cluster gate over the store (the convergence read the proposer calls).
// ---------------------------------------------------------------------------

// differentShapeCalls qualifies too — 9 calls, 3 distinct tools, one
// recovery — but through a different tool family, so it hashes to its own
// cluster.
func differentShapeCalls() []toolCall {
	ok := func(name, id string) toolCall { return toolCall{name: name, callID: id} }
	return []toolCall{
		ok("x.render", "d1"),
		ok("y.transform", "d2"),
		{name: "z.commit", callID: "d3", err: true},
		ok("z.commit", "d4"), // the recovery
		ok("x.render", "d5"),
		ok("y.transform", "d6"),
		ok("z.commit", "d7"),
		ok("x.render", "d8"),
		ok("y.transform", "d9"),
	}
}

func TestQualifierConvergence(t *testing.T) {
	ctx, st, _, _ := newFixture(t, DefaultConfig())
	// A fresh qualifier without the chip option: convergence reads stay
	// silent; chips are covered in TestQualifierIngest.
	q := NewQualifier(st.SessionEvents(), func(context.Context, string) Config { return DefaultConfig() }, st.SkillCandidates(), discardLog())

	appendTurn := func(t *testing.T, sessionID, turnID string, calls []toolCall) {
		t.Helper()
		rows := buildWindow(t, sessionID, turnID, calls, true)
		for i := range rows {
			rows[i].SessionID = sessionID
			rows[i].Seq += int64(len(sessionID)) * 100 // deterministic, session-distinct ordering
		}
		if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, rows); err != nil {
			t.Fatalf("append events: %v", err)
		}
	}
	jobFor := func(sessionID, turnID string) ingest.Job {
		job := fixtureJob("completed", turnID)
		job.SessionID = sessionID
		return job
	}

	cluster := ClusterKey(fixtureWorkspace, fixtureAgent, toolNames(qualifyingCalls()))
	appendTurn(t, fixtureSession, "turn-1", qualifyingCalls())

	// First qualifying run: the cluster opens without proposing.
	if err := q.Ingest(ctx, jobFor(fixtureSession, "turn-1")); err != nil {
		t.Fatalf("Ingest first run: %v", err)
	}
	converged, err := q.HasConvergence(ctx, fixtureWorkspace, cluster)
	if err != nil {
		t.Fatalf("HasConvergence: %v", err)
	}
	if converged {
		t.Errorf("one qualifying run must hold the gate open (1 < ClusterMinimum 2)")
	}

	// A second, identical-shape run in another session converges the family.
	appendTurn(t, "sess-qual-2", "turn-2", qualifyingCalls())
	if err := q.Ingest(ctx, jobFor("sess-qual-2", "turn-2")); err != nil {
		t.Fatalf("Ingest second run: %v", err)
	}
	converged, err = q.HasConvergence(ctx, fixtureWorkspace, cluster)
	if err != nil {
		t.Fatalf("HasConvergence: %v", err)
	}
	if !converged {
		t.Errorf("two qualifying runs meet ClusterMinimum 2; gate stayed closed")
	}

	// A qualifying but different-shaped run lives in its own cluster and
	// does not converge that family's gate on its own.
	otherCluster := ClusterKey(fixtureWorkspace, fixtureAgent, toolNames(differentShapeCalls()))
	if otherCluster == cluster {
		t.Fatalf("different shapes must hash to different clusters")
	}
	appendTurn(t, "sess-qual-3", "turn-3", differentShapeCalls())
	if err := q.Ingest(ctx, jobFor("sess-qual-3", "turn-3")); err != nil {
		t.Fatalf("Ingest third run: %v", err)
	}
	converged, err = q.HasConvergence(ctx, fixtureWorkspace, otherCluster)
	if err != nil {
		t.Fatalf("HasConvergence: %v", err)
	}
	if converged {
		t.Errorf("a different family's first qualifying run cannot converge its own cluster")
	}

	// A failed run of the first family joins as contrast evidence — counted
	// in the cluster, never toward the gate.
	failedJob := jobFor("sess-qual-4", "turn-4")
	failedJob.Status = "failed"
	appendTurn(t, "sess-qual-4", "turn-4", qualifyingCalls())
	if err := q.Ingest(ctx, failedJob); err != nil {
		t.Fatalf("Ingest failed run: %v", err)
	}
	contrast, err := ClusterContrastRuns(ctx, st.SkillCandidates(), fixtureWorkspace, cluster)
	if err != nil {
		t.Fatalf("ClusterContrastRuns: %v", err)
	}
	if len(contrast) != 1 || contrast[0].TurnID != "turn-4" {
		t.Errorf("contrast evidence = %+v, want exactly the failed turn-4 member", contrast)
	}
	count, err := ClusterQualifyingCount(ctx, st.SkillCandidates(), fixtureWorkspace, cluster)
	if err != nil {
		t.Fatalf("ClusterQualifyingCount: %v", err)
	}
	if count != 2 {
		t.Errorf("qualifying count = %d, want 2 (the failed member never counts)", count)
	}
}

func toolNames(calls []toolCall) []string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.name)
	}
	return names
}

// The decision log: one Info line per evaluation naming the outcome, the
// gates that failed, and the tally against the workspace's thresholds. The
// failing path used to log at Debug — invisible at the server's default
// level — so a run that failed the gates vanished silently.
func TestQualifierDecisionLog(t *testing.T) {
	capture := func() (*bytes.Buffer, *slog.Logger) {
		buf := &bytes.Buffer{}
		return buf, slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}

	t.Run("a gate failure logs qualified=false naming the failed gate", func(t *testing.T) {
		buf, log := capture()
		ctx, st, q, _ := newFixtureWithLog(t, DefaultConfig(), log)
		// noRecoveryCalls passes every gate except the required recovery.
		if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, buildWindow(t, "q", "turn-1", noRecoveryCalls(), true)); err != nil {
			t.Fatalf("append events: %v", err)
		}

		if err := q.Ingest(ctx, fixtureJob("completed", "turn-1")); err != nil {
			t.Fatalf("Ingest: %v", err)
		}

		line := buf.String()
		for _, want := range []string{
			"msg=\"skillcuration: skill qualification decision\"",
			"qualified=false",
			"failed_gates=[recovery]",
			"tool_calls=15",
			"min_tool_calls=8",
		} {
			if !strings.Contains(line, want) {
				t.Errorf("decision log missing %s:\n%s", want, line)
			}
		}
	})

	t.Run("a qualifying run logs qualified=true with empty failed gates", func(t *testing.T) {
		buf, log := capture()
		ctx, st, q, sink := newFixtureWithLog(t, DefaultConfig(), log)
		if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, buildWindow(t, "q", "turn-1", qualifyingCalls(), true)); err != nil {
			t.Fatalf("append events: %v", err)
		}

		if err := q.Ingest(ctx, fixtureJob("completed", "turn-1")); err != nil {
			t.Fatalf("Ingest: %v", err)
		}

		line := buf.String()
		for _, want := range []string{"qualified=true", "failed_gates=[]", "recoveries=1"} {
			if !strings.Contains(line, want) {
				t.Errorf("decision log missing %s:\n%s", want, line)
			}
		}
		if len(*sink) != 1 {
			t.Errorf("chip emissions = %d, want 1 — the log must not replace the trigger", len(*sink))
		}
	})
}
