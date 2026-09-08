package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// fakeAgentRunRunner models the runner seam for the run-cancel and detached
// execution handler tests: transcript history, pending approvals, approval
// resume, and run cancellation.
type fakeAgentRunRunner struct {
	mu sync.Mutex

	history    *agents.HistoryResult
	historyErr error

	pending    *agents.ApprovalPayload
	pendingErr error

	resumeStream *agents.EventStream
	resumeErr    error

	cancelResult bool
	cancelCalls  [][3]string // (workspaceID, agentID, sessionID) per call
}

func (f *fakeAgentRunRunner) History(ctx context.Context, req agents.HistoryRequest) (*agents.HistoryResult, error) {
	return f.history, f.historyErr
}

func (f *fakeAgentRunRunner) PendingApproval(ctx context.Context, workspaceID, sessionID string) (*agents.ApprovalPayload, error) {
	return f.pending, f.pendingErr
}

func (f *fakeAgentRunRunner) Resume(ctx context.Context, req agents.ExecRequest, approval *agents.ApprovalPayload, approved bool) (*agents.EventStream, error) {
	return f.resumeStream, f.resumeErr
}

func (f *fakeAgentRunRunner) CancelRun(workspaceID, agentID, sessionID string) bool {
	f.mu.Lock()
	f.cancelCalls = append(f.cancelCalls, [3]string{workspaceID, agentID, sessionID})
	f.mu.Unlock()
	return f.cancelResult
}

// newAgentRunTestEnv builds a one-agent workspace with an agentHandlers wired
// to the supplied runner fake, and a gin engine exposing the cancel route the
// way the router does (workspace context set, handler mounted directly).
func newAgentRunTestEnv(t *testing.T, runner *fakeAgentRunRunner) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	st, ws := newAgentRunWorkspace(t)
	agent := &domain.Agent{
		ID:          "agent-atlas-1",
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		Role:        "ops",
		Brief:       "Runs things",
		ProviderID:  "prov-fake",
		Model:       "fake-model",
	}
	if err := st.Agents().Create(context.Background(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	h := handlers.NewAgentHandlers(st.Agents(), st.Providers(), st.SessionEvents(), []byte("01234567890123456789012345678901"), nil, nil, nil, t.TempDir(), runner, runner)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Next()
	})
	r.POST("/workspaces/:ws/agents/:agent/sessions/:session/runs/:turn/cancel", h.CancelRun)
	return r
}

func newAgentRunWorkspace(t *testing.T) (store.Store, *domain.Workspace) {
	t.Helper()
	st := storefake.New()
	ws := &domain.Workspace{ID: "ws-run-test", Slug: "run-ws", Name: "Run Workspace"}
	if err := st.Workspaces().Create(context.Background(), ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	prov := &domain.ProviderConfig{ID: "prov-fake", WorkspaceID: ws.ID, Type: "fake", Name: "Fake"}
	if err := st.Providers().Create(context.Background(), prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	// Seed the persisted session (sess-1) the cancel tests address.
	if err := st.SessionEvents().AppendEvents(context.Background(), ws.ID, []domain.SessionEvent{{
		SessionID:   "sess-1",
		EventID:     "evt-1",
		TurnID:      "turn-0",
		Kind:        "message",
		OccurredAt:  time.Now().UTC(),
		WorkspaceID: ws.ID,
	}}); err != nil {
		t.Fatalf("append session event: %v", err)
	}
	return st, ws
}

func TestAgentRuns_CancelLiveRun(t *testing.T) {
	runner := &fakeAgentRunRunner{
		history: &agents.HistoryResult{
			Events: []agents.TranscriptEvent{{Kind: "message"}},
		},
		cancelResult: true,
	}
	r := newAgentRunTestEnv(t, runner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workspaces/run-ws/agents/atlas/sessions/sess-1/runs/turn-1/cancel", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on live-run cancel, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Cancelled bool `json:"cancelled"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !res.Cancelled {
		t.Errorf("expected cancelled=true, got %s", w.Body.String())
	}

	if len(runner.cancelCalls) != 1 {
		t.Fatalf("expected exactly one CancelRun call, got %d", len(runner.cancelCalls))
	}
	if call := runner.cancelCalls[0]; call != [3]string{"ws-run-test", "agent-atlas-1", "sess-1"} {
		t.Errorf("CancelRun addressed the wrong run, got %v", call)
	}
}

func TestAgentRuns_CancelNoLiveRunConflicts(t *testing.T) {
	runner := &fakeAgentRunRunner{
		history: &agents.HistoryResult{
			Events: []agents.TranscriptEvent{{Kind: "message"}},
		},
		cancelResult: false, // no live run for the session
	}
	r := newAgentRunTestEnv(t, runner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workspaces/run-ws/agents/atlas/sessions/sess-1/runs/turn-1/cancel", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 when no live run, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAgentRuns_CancelUnknownAgent(t *testing.T) {
	runner := &fakeAgentRunRunner{}
	r := newAgentRunTestEnv(t, runner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workspaces/run-ws/agents/ghost/sessions/sess-1/runs/turn-1/cancel", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown agent, got %d: %s", w.Code, w.Body.String())
	}
	if len(runner.cancelCalls) != 0 {
		t.Errorf("CancelRun must not be reached for an unknown agent")
	}
}

func TestAgentRuns_CancelUnknownSession(t *testing.T) {
	runner := &fakeAgentRunRunner{
		// Empty transcript: an unknown session and a foreign session are
		// indistinguishable — both are not-found, mirroring the /v1 binding.
		history: &agents.HistoryResult{Events: []agents.TranscriptEvent{}},
	}
	r := newAgentRunTestEnv(t, runner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workspaces/run-ws/agents/atlas/sessions/nope/runs/turn-1/cancel", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown session, got %d: %s", w.Code, w.Body.String())
	}
	if len(runner.cancelCalls) != 0 {
		t.Errorf("CancelRun must not be reached for an unknown session")
	}
}

// seamTurn models a detached turn inside the fake runner: it sends one event
// into the returned stream and "persists" it once the test releases it. If a
// handler tied the turn to its request (the pre-fix bug) or cancelled the
// returned stream, the send would fail and the event would never land.
type seamTurn struct {
	stream  *agents.EventStream
	release chan struct{}
	done    chan struct{}

	mu        sync.Mutex
	persisted []agents.TranscriptEvent
}

func (s *seamTurn) run(event agents.TranscriptEvent) {
	go func() {
		defer close(s.done)
		<-s.release
		if s.stream.Send(&event) {
			s.mu.Lock()
			s.persisted = append(s.persisted, event)
			s.mu.Unlock()
		}
		s.stream.Close()
	}()
}

func (s *seamTurn) persistedEvents() []agents.TranscriptEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]agents.TranscriptEvent(nil), s.persisted...)
}

func waitTurnSettled(t *testing.T, done <-chan struct{}, wantDone bool, msg string) {
	t.Helper()
	select {
	case <-done:
		if !wantDone {
			t.Fatal(msg)
		}
	case <-time.After(2 * time.Second):
		if wantDone {
			t.Fatal(msg + " (timed out)")
		}
	}
}

// TestAgentRuns_ApprovalResumeOutlivesRequest reproduces the detached-resume
// regression at the handler seam: the approval response returns before the
// resumed turn completes, the handler neither blocks on nor cancels the
// returned stream, and the turn still finishes and persists its events.
func TestAgentRuns_ApprovalResumeOutlivesRequest(t *testing.T) {
	runner := &fakeAgentRunRunner{
		pending: &agents.ApprovalPayload{InterruptID: "int-1", Command: "ls"},
	}

	turn := &seamTurn{
		stream:  agents.NewEventStream(16),
		release: make(chan struct{}),
		done:    make(chan struct{}),
	}
	// Resume models the detached manager-owned turn: the stream is handed
	// back immediately and the turn only finishes when released.
	runner.resumeStream = turn.stream
	turn.run(agents.TranscriptEvent{Kind: "message", TurnID: "turn-2"})

	st, ws := newAgentRunWorkspace(t)
	agent := &domain.Agent{
		ID: "agent-atlas-1", WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas",
		Role: "ops", Brief: "Runs things", ProviderID: "prov-fake", Model: "fake-model",
	}
	if err := st.Agents().Create(context.Background(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	h := handlers.NewAgentHandlers(st.Agents(), st.Providers(), st.SessionEvents(), []byte("01234567890123456789012345678901"), nil, nil, nil, t.TempDir(), runner, runner)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.UserContextKey, &domain.User{ID: "user-1", Email: "approver@example.com", Name: "Approver"})
		c.Next()
	})
	r.POST("/workspaces/:ws/agents/:agent/sessions/:session/approvals/:interruptID", h.ResolveApproval)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workspaces/run-ws/agents/atlas/sessions/sess-1/approvals/int-1", strings.NewReader(`{"approved":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from approval resolution, got %d: %s", w.Code, w.Body.String())
	}

	// The response returned before the resumed turn completed: the handler
	// did not block on the stream.
	waitTurnSettled(t, turn.done, false, "handler blocked until the resumed turn completed")

	// The turn still finishes and persists its event after the response.
	close(turn.release)
	waitTurnSettled(t, turn.done, true, "resumed turn never completed after release")
	if got := turn.persistedEvents(); len(got) != 1 || got[0].Kind != "message" {
		t.Fatalf("expected the resumed turn's event to persist, got %v", got)
	}

	// The handler never cancelled the returned stream: the turn's event is
	// still readable from the view (a Cancel/Close by the handler would have
	// made the late Send a drop).
	ev, err := turn.stream.Recv()
	if err != nil {
		t.Fatalf("expected the turn's event on the abandoned stream, got %v", err)
	}
	if ev.Kind != "message" {
		t.Errorf("unexpected event kind %q", ev.Kind)
	}
	if _, err := turn.stream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after the single event, got %v", err)
	}
}

// TestAgentRuns_FireAndForgetStartDoesNotCancel models the future exec-start
// handler seam (task 3.2): a handler that starts a run, abandons the returned
// stream, and responds immediately must not block on or cancel the run — the
// turn completes and persists on its own.
func TestAgentRuns_FireAndForgetStartDoesNotCancel(t *testing.T) {
	turn := &seamTurn{
		stream:  agents.NewEventStream(16),
		release: make(chan struct{}),
		done:    make(chan struct{}),
	}
	// runStart models Runner.Run: the stream is handed back immediately and
	// the turn finishes only when released.
	runStart := func(ctx context.Context, req agents.ExecRequest) (*agents.EventStream, error) {
		turn.run(agents.TranscriptEvent{Kind: "message", TurnID: "turn-1"})
		return turn.stream, nil
	}

	// Minimal inline exec-start handler of the shape the future exec route
	// will use: start, abandon the stream, respond.
	startHandler := func(c *gin.Context) {
		stream, err := runStart(c.Request.Context(), agents.ExecRequest{})
		if err != nil {
			handlers.RespondError(c, err)
			return
		}
		_ = stream // abandoned on purpose: fire-and-forget start
		handlers.RespondOK(c, gin.H{"started": true})
	}

	r := gin.New()
	r.POST("/start", startHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/start", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from start, got %d: %s", w.Code, w.Body.String())
	}

	// The handler returned without waiting for the run.
	waitTurnSettled(t, turn.done, false, "handler blocked until the run completed")

	// The abandoned run completes and persists despite the stream never
	// being consumed by the handler.
	close(turn.release)
	waitTurnSettled(t, turn.done, true, "run never completed after release")
	if got := turn.persistedEvents(); len(got) != 1 || got[0].Kind != "message" {
		t.Fatalf("expected the run's event to persist, got %v", got)
	}
}
