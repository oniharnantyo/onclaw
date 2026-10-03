package agents

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/ingest"
	"github.com/oniharnantyo/onclaw/internal/skillcuration"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ---------------------------------------------------------------------------
// The skill-curation chip (add-skill-curation-from-traces, task 7): the
// runner's CandidateChipSink persists the x.skill_candidate session event and
// the hydrated History renders it as the live kind — scheduled and heartbeat
// origins emit nothing (the memory chip's origin rule, cloned).
// ---------------------------------------------------------------------------

// appendChipWorld is the memory-chip test's world: the hooks runner over the
// fake store.
func appendChipWorld(t *testing.T) (context.Context, store.Store, *Runner, *domain.Workspace, *domain.Agent, ExecRequest) {
	t.Helper()
	st, runner, ws, ag, req := setupHooksRunner(t, &hooksModel{final: "ok"})
	return context.Background(), st, runner, ws, ag, req
}

// TestRunner_AppendSkillCandidateChipPersistsAndStreams mirrors the memory
// chip test: the chip persists as an application-owned session event and the
// hydrated History renders it identically — while scheduled and heartbeat
// origins emit nothing.
func TestRunner_AppendSkillCandidateChipPersistsAndStreams(t *testing.T) {
	ctx, st, runner, ws, ag, req := appendChipWorld(t)

	payload := skillcuration.SkillCandidatePayload{
		WorkspaceID: ws.ID,
		AgentID:     ag.ID,
		SessionID:   req.SessionID,
		RunID:       "turn-1",
		Cluster:     "cl-x",
	}
	job := ingest.Job{
		WorkspaceID: ws.ID,
		AgentID:     ag.ID,
		UserID:      req.UserID,
		SessionID:   req.SessionID,
		TurnID:      "turn-1",
		Origin:      OriginUser,
		Status:      hookRunStatusCompleted,
	}
	runner.AppendSkillCandidateChip(ctx, job, payload)

	// Hydrated view: the chip renders exactly like the live event.
	history, err := runner.History(ctx, HistoryRequest{WorkspaceID: ws.ID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	var chip *TranscriptEvent
	for i := range history.Events {
		if history.Events[i].Kind == TranscriptEventSkillCandidate {
			chip = &history.Events[i]
			break
		}
	}
	if chip == nil || chip.SkillCandidate == nil {
		t.Fatalf("expected the hydrated skill_candidate chip, got %+v", history.Events)
	}
	if chip.SkillCandidate.RunID != "turn-1" || chip.SkillCandidate.Cluster != "cl-x" {
		t.Fatalf("chip payload round-trip drifted: %+v", chip.SkillCandidate)
	}
	if chip.TurnID != "turn-1" {
		t.Fatalf("chip must hydrate under its turn: %+v", chip)
	}

	// The durable row is the application-owned session event.
	rows, err := st.SessionEvents().LoadEvents(ctx, store.LoadSessionEventsParams{WorkspaceID: ws.ID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.Kind == string(skillcuration.SessionEventKindSkillCandidate) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a persisted %q session event", skillcuration.SessionEventKindSkillCandidate)
	}

	// Scheduled and heartbeat runs never emit chips.
	scheduled := job
	scheduled.Origin = OriginScheduler
	scheduled.TurnID = "turn-sched"
	runner.AppendSkillCandidateChip(ctx, scheduled, payload)
	heartbeat := job
	heartbeat.Origin = OriginHeartbeat
	heartbeat.TurnID = "turn-hb"
	runner.AppendSkillCandidateChip(ctx, heartbeat, payload)

	history, err = runner.History(ctx, HistoryRequest{WorkspaceID: ws.ID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	chips := 0
	for i := range history.Events {
		if history.Events[i].Kind == TranscriptEventSkillCandidate {
			chips++
			if history.Events[i].TurnID == "turn-sched" || history.Events[i].TurnID == "turn-hb" {
				t.Fatalf("unattended origins must not emit chips: %+v", history.Events[i])
			}
		}
	}
	if chips != 1 {
		t.Fatalf("expected exactly one chip (scheduled/heartbeat suppressed), got %d", chips)
	}
}

// chipWindow builds one qualifying run's persisted window (the qualifier's
// five gates: completed, nine tool calls across four tools, one error→
// success recovery, a final assistant message) and appends it to the
// session.
func chipWindow(t *testing.T, ctx context.Context, st store.Store, workspaceID, sessionID, turnID string) {
	t.Helper()
	serializer := &schema.HumanReadableSerializer{}
	seq := int64(0)
	at := time.Unix(1_700_000_000, 0)

	appendRow := func(ev *adk.SessionEvent[*schema.AgenticMessage], kind adk.SessionEventKind) {
		t.Helper()
		payload, err := serializer.Marshal(ev)
		if err != nil {
			t.Fatalf("serialize session event: %v", err)
		}
		seq++
		if kind == "" {
			kind = adk.SessionEventMessage
		}
		row := domain.SessionEvent{
			SessionID:   sessionID,
			EventID:     fmt.Sprintf("%s-evt-%d", turnID, seq),
			TurnID:      turnID,
			Seq:         seq,
			Kind:        string(kind),
			Payload:     payload,
			OccurredAt:  ev.Timestamp,
			WorkspaceID: workspaceID,
		}
		if err := st.SessionEvents().AppendEvents(ctx, workspaceID, []domain.SessionEvent{row}); err != nil {
			t.Fatalf("append event: %v", err)
		}
	}

	appendRow(&adk.SessionEvent[*schema.AgenticMessage]{
		TurnID:    turnID,
		Timestamp: at.Add(time.Second),
		Message:   schema.UserAgenticMessage("run the procedure"),
	}, "")
	calls := []struct {
		name string
		id   string
		err  bool
	}{
		{"grafana.query", "c1", false},
		{"files.write", "c2", false},
		{"http.request", "c3", true},
		{"http.request", "c4", false}, // the recovery
		{"grafana.annotate", "c5", false},
		{"grafana.query", "c6", false},
		{"files.write", "c7", false},
		{"http.request", "c8", false},
		{"grafana.query", "c9", false},
	}
	for _, call := range calls {
		startAt := at.Add(time.Second)
		endAt := startAt.Add(2 * time.Second)
		appendRow(&adk.SessionEvent[*schema.AgenticMessage]{
			TurnID:    turnID,
			Timestamp: startAt,
			Kind:      adk.SessionEventSpanToolCallStart,
			Span: &adk.SpanEvent{
				Kind:      adk.SpanKindTool,
				StartedAt: startAt,
				Tool:      &adk.ToolSpanMeta{ToolUseID: call.id, Name: call.name},
			},
		}, adk.SessionEventSpanToolCallStart)
		end := &adk.SessionEvent[*schema.AgenticMessage]{
			TurnID:    turnID,
			Timestamp: endAt,
			Kind:      adk.SessionEventSpanToolCallEnd,
			Span: &adk.SpanEvent{
				Kind:      adk.SpanKindTool,
				EndedAt:   endAt,
				StartedAt: startAt,
				Tool:      &adk.ToolSpanMeta{ToolUseID: call.id, Name: call.name},
			},
		}
		if call.err {
			end.Span.Status = "error"
			end.Span.Err = "boom: " + call.id
		}
		appendRow(end, adk.SessionEventSpanToolCallEnd)
	}
	appendRow(&adk.SessionEvent[*schema.AgenticMessage]{
		TurnID:    turnID,
		Timestamp: at.Add(time.Second),
		Message: &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				{AssistantGenText: &schema.AssistantGenText{Text: "done — the procedure completed."}},
			},
		},
	}, "")
}

// TestRunner_SkillCandidateChipEndToEnd walks the full chip chain over the
// REAL qualifier: seed a qualifying window → the qualifier ingests the job →
// the sink closure calls the runner's AppendSkillCandidateChip → the hydrated
// History renders the chip (qualifier → sink → runner append → history
// hydration).
func TestRunner_SkillCandidateChipEndToEnd(t *testing.T) {
	ctx, st, runner, ws, ag, req := appendChipWorld(t)
	chipWindow(t, ctx, st, ws.ID, req.SessionID, "turn-qual")

	qualifier := skillcuration.NewQualifier(
		st.SessionEvents(),
		func(context.Context, string) skillcuration.Config { return skillcuration.DefaultConfig() },
		st.SkillCandidates(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		skillcuration.WithCandidateChipSink(func(ctx context.Context, job ingest.Job, payload skillcuration.SkillCandidatePayload) {
			runner.AppendSkillCandidateChip(ctx, job, payload)
		}),
	)
	job := ingest.Job{
		WorkspaceID: ws.ID,
		AgentID:     ag.ID,
		UserID:      req.UserID,
		SessionID:   req.SessionID,
		TurnID:      "turn-qual",
		Origin:      OriginUser,
		Status:      string(domain.SkillClusterRunCompleted),
	}
	if err := qualifier.Ingest(ctx, job); err != nil {
		t.Fatalf("qualifier ingest: %v", err)
	}

	history, err := runner.History(ctx, HistoryRequest{WorkspaceID: ws.ID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	for i := range history.Events {
		ev := history.Events[i]
		if ev.Kind == TranscriptEventSkillCandidate {
			if ev.SkillCandidate == nil || ev.SkillCandidate.RunID != "turn-qual" || ev.TurnID != "turn-qual" {
				t.Fatalf("chip payload drifted: %+v", ev)
			}
			if ev.SkillCandidate.SessionID != req.SessionID || ev.SkillCandidate.AgentID != ag.ID {
				t.Fatalf("chip coordinates drifted: %+v", ev.SkillCandidate)
			}
			return
		}
	}
	t.Fatalf("expected the hydrated skill_candidate chip after qualification, got %+v", history.Events)
}

// ---------------------------------------------------------------------------
// The curated-skill outcome telemetry (design D6 v1 run-level proxy): the
// terminal seam classifies the turn's observed skill loads.
// ---------------------------------------------------------------------------

// outcomeRecorder is the SkillOutcomeRecorder test double: outcomes land on
// a buffered channel.
type outcomeRecorder struct {
	ch chan string
}

func newOutcomeRecorder() *outcomeRecorder {
	return &outcomeRecorder{ch: make(chan string, 16)}
}

func (r *outcomeRecorder) record(_ context.Context, _, _, skillName string, helpful bool) {
	verdict := "helpful"
	if !helpful {
		verdict = "harmful"
	}
	r.ch <- skillName + "=" + verdict
}

func (r *outcomeRecorder) wait(t *testing.T) string {
	t.Helper()
	select {
	case got := <-r.ch:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("the recorder was never invoked")
		return ""
	}
}

func (r *outcomeRecorder) assertQuiet(t *testing.T) {
	t.Helper()
	select {
	case got := <-r.ch:
		t.Fatalf("the recorder must not fire, got %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestRunner_SkillOutcomesClassifiedAtTerminal drives the terminal seam
// directly: completed runs classify helpful, failed runs harmful, cancelled
// runs never classify, duplicate loads dedupe, and unparseable arguments are
// inert.
func TestRunner_SkillOutcomesClassifiedAtTerminal(t *testing.T) {
	rec := newOutcomeRecorder()
	_, runner, ws, ag, req := worldWithOutcomeRecorder(t, rec)
	ctx := context.Background()

	// Completed: helpful, deduped across two call ids.
	runner.recordSkillOutcomes(ctx, req, hookRunStatusCompleted, map[string]string{
		"call-1": `{"skill":"deploy-rollback"}`,
		"call-2": `{"skill":"deploy-rollback"}`,
		"call-3": `{"skill":"log-rotation"}`,
	})
	if got := rec.wait(t); got != "deploy-rollback=helpful" {
		t.Errorf("first outcome = %q, want deploy-rollback=helpful", got)
	}
	if got := rec.wait(t); got != "log-rotation=helpful" {
		t.Errorf("second outcome = %q, want log-rotation=helpful (deduped)", got)
	}
	rec.assertQuiet(t)

	// Failed: harmful.
	runner.recordSkillOutcomes(ctx, req, hookRunStatusFailed, map[string]string{
		"call-1": `{"skill":"deploy-rollback"}`,
	})
	if got := rec.wait(t); got != "deploy-rollback=harmful" {
		t.Errorf("failed outcome = %q, want deploy-rollback=harmful", got)
	}

	// Cancelled: never classified.
	runner.recordSkillOutcomes(ctx, req, hookRunStatusCancelled, map[string]string{
		"call-1": `{"skill":"deploy-rollback"}`,
	})
	rec.assertQuiet(t)

	// Unparseable or nameless arguments are inert.
	runner.recordSkillOutcomes(ctx, req, hookRunStatusCompleted, map[string]string{
		"call-1": `not json`,
		"call-2": `{"skill":"  "}`,
	})
	rec.assertQuiet(t)

	// Coordinates ride the request.
	_ = ws
	_ = ag
}

// worldWithOutcomeRecorder builds the hooks runner with the recorder wired.
func worldWithOutcomeRecorder(t *testing.T, rec *outcomeRecorder) (store.Store, *Runner, *domain.Workspace, *domain.Agent, ExecRequest) {
	t.Helper()
	return setupHooksRunnerWithOpts(t, &hooksModel{final: "ok"}, []RunnerOption{
		WithSkillOutcomeRecorder(rec.record),
	})
}
