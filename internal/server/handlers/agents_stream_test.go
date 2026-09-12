package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
)

// fakeStreamingRunner extends the history fake (agent_runs_test.go) with the
// run-tapper seam: it hands each subscriber a real agents.EventStream that the
// test feeds from the producer side, modeling a live run broadcasting to its
// taps. liveRun=false (the zero value) models no run in flight.
type fakeStreamingRunner struct {
	*fakeAgentRunRunner

	liveRun     bool
	subscribed  chan struct{}              // signaled once the handler attaches a tap (when non-nil)
	historyGate chan struct{}              // when non-nil, History blocks until closed
	historyReqs chan agents.HistoryRequest // buffered; records every History request

	mu      sync.Mutex
	nextID  uint64
	subs    map[uint64]*agents.EventStream
	subKeys []agents.RunKey
	unsubs  []struct {
		key   agents.RunKey
		subID uint64
	}
}

func newFakeStreamingRunner(history *agents.HistoryResult) *fakeStreamingRunner {
	return &fakeStreamingRunner{
		fakeAgentRunRunner: &fakeAgentRunRunner{history: history},
		subs:               map[uint64]*agents.EventStream{},
	}
}

func (f *fakeStreamingRunner) History(ctx context.Context, req agents.HistoryRequest) (*agents.HistoryResult, error) {
	if f.historyReqs != nil {
		f.historyReqs <- req
	}
	if f.historyGate != nil {
		<-f.historyGate
	}
	return f.fakeAgentRunRunner.History(ctx, req)
}

func (f *fakeStreamingRunner) SubscribeRun(key agents.RunKey) (uint64, *agents.EventStream, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.liveRun {
		return 0, nil, false
	}
	f.nextID++
	id := f.nextID
	f.subs[id] = agents.NewEventStream(64)
	f.subKeys = append(f.subKeys, key)
	if f.subscribed != nil {
		select {
		case f.subscribed <- struct{}{}:
		default:
		}
	}
	return id, f.subs[id], true
}

func (f *fakeStreamingRunner) UnsubscribeRun(key agents.RunKey, subID uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unsubs = append(f.unsubs, struct {
		key   agents.RunKey
		subID uint64
	}{key: key, subID: subID})
	delete(f.subs, subID)
}

// broadcast fans an event out to every live subscriber tap (the manager's
// Broadcast fan-out). Send is non-blocking: events buffer on the tap until
// the handler drains them.
func (f *fakeStreamingRunner) broadcast(ev *agents.TranscriptEvent) {
	for _, tap := range f.taps() {
		tap.Send(ev)
	}
}

// finishRun models run completion: every subscriber tap closes so Recv drains
// to EOF.
func (f *fakeStreamingRunner) finishRun() {
	for _, tap := range f.taps() {
		_ = tap.Close()
	}
}

func (f *fakeStreamingRunner) taps() []*agents.EventStream {
	f.mu.Lock()
	defer f.mu.Unlock()
	taps := make([]*agents.EventStream, 0, len(f.subs))
	for _, s := range f.subs {
		taps = append(taps, s)
	}
	return taps
}

func (f *fakeStreamingRunner) subscriberCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subs)
}

func (f *fakeStreamingRunner) unsubscribeCalls() []struct {
	key   agents.RunKey
	subID uint64
} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]struct {
		key   agents.RunKey
		subID uint64
	}(nil), f.unsubs...)
}

// newAgentStreamTestEnv builds the one-agent workspace with an agentHandlers
// wired to the supplied runner fake, and a gin engine exposing the session
// events route the way the router does (workspace context set, handler
// mounted directly).
func newAgentStreamTestEnv(t *testing.T, runner *fakeStreamingRunner) *gin.Engine {
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

	h := handlers.NewAgentHandlers(st.Agents(), st.Providers(), st.SessionEvents(), st.AgentSessions(), []byte("01234567890123456789012345678901"), nil, nil, nil, t.TempDir(), runner, runner)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Next()
	})
	r.GET("/workspaces/:ws/agents/:agent/sessions/:session/events", h.ListSessionEvents)
	return r
}

// sseFrames splits an SSE body into its data-frame payloads.
func sseFrames(t *testing.T, body string) []string {
	t.Helper()
	var frames []string
	for _, part := range strings.Split(body, "\n\n") {
		if part == "" {
			continue
		}
		if !strings.HasPrefix(part, "data: ") {
			t.Fatalf("malformed SSE frame %q in body:\n%s", part, body)
		}
		frames = append(frames, strings.TrimPrefix(part, "data: "))
	}
	return frames
}

func assertSSEHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("expected Cache-Control no-cache, got %q", got)
	}
	if got := w.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("expected X-Accel-Buffering no, got %q", got)
	}
}

func assertDoneLast(t *testing.T, frames []string) {
	t.Helper()
	if len(frames) == 0 || frames[len(frames)-1] != "[DONE]" {
		t.Fatalf("expected stream to end with [DONE], got frames %v", frames)
	}
}

// TestAgents_ListSessionEventsStreamReplayCompleted verifies stream=true on a
// session with no active run: the committed history replays as SSE data
// frames and the stream ends with [DONE].
func TestAgents_ListSessionEventsStreamReplayCompleted(t *testing.T) {
	runner := newFakeStreamingRunner(&agents.HistoryResult{
		Events: []agents.TranscriptEvent{
			{ID: "evt-1", Kind: agents.TranscriptEventTurnStarted, TurnID: "turn-1"},
			{ID: "evt-2", Kind: agents.TranscriptEventTextDelta, TurnID: "turn-1", TextDelta: "Hel"},
			{ID: "evt-3", Kind: agents.TranscriptEventTurnCompleted, TurnID: "turn-1"},
		},
	})
	r := newAgentStreamTestEnv(t, runner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/run-ws/agents/atlas/sessions/sess-1/events?stream=true", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertSSEHeaders(t, w)

	frames := sseFrames(t, w.Body.String())
	assertDoneLast(t, frames)
	if len(frames) != 4 {
		t.Fatalf("expected 3 event frames + [DONE], got %d: %v", len(frames), frames)
	}
	// No run is live: the run_active status frame must not appear.
	if strings.Contains(w.Body.String(), string(agents.TranscriptEventRunActive)) {
		t.Errorf("run_active must not be emitted without a live run, got:\n%s", w.Body.String())
	}
	wantIDs := []string{"evt-1", "evt-2", "evt-3"}
	for i, wantID := range wantIDs {
		var ev agents.TranscriptEvent
		if err := json.Unmarshal([]byte(frames[i]), &ev); err != nil {
			t.Fatalf("frame %d is not event JSON: %v (%q)", i, err, frames[i])
		}
		if ev.ID != wantID {
			t.Errorf("frame %d: expected event id %q, got %q", i, wantID, ev.ID)
		}
	}

	// No run was live: no tap was ever attached or released.
	if calls := runner.unsubscribeCalls(); len(calls) != 0 {
		t.Errorf("expected no unsubscribe calls without a live run, got %v", calls)
	}
}

// TestAgents_ListSessionEventsStreamLiveRunToCompletion verifies the two-phase
// handover: history replays first, live tap events follow to the terminal
// marker, events present on both paths are deduped, and the tap is released
// when the handler returns.
func TestAgents_ListSessionEventsStreamLiveRunToCompletion(t *testing.T) {
	runner := newFakeStreamingRunner(&agents.HistoryResult{
		Events: []agents.TranscriptEvent{
			{ID: "evt-1", Kind: agents.TranscriptEventTurnStarted, TurnID: "turn-1"},
			{ID: "evt-2", Kind: agents.TranscriptEventTextDelta, TurnID: "turn-1", TextDelta: "committed"},
		},
	})
	runner.liveRun = true
	runner.subscribed = make(chan struct{}, 1)
	r := newAgentStreamTestEnv(t, runner)

	// The scripted run emits while the handler sits between the tap and the
	// history replay: evt-2 is already committed (must be deduped), evt-3 is
	// new, evt-4 is the terminal marker.
	go func() {
		<-runner.subscribed
		runner.broadcast(&agents.TranscriptEvent{ID: "evt-2", Kind: agents.TranscriptEventTextDelta, TurnID: "turn-1", TextDelta: "committed"})
		runner.broadcast(&agents.TranscriptEvent{ID: "evt-3", Kind: agents.TranscriptEventMessageCompleted, TurnID: "turn-1"})
		runner.broadcast(&agents.TranscriptEvent{ID: "evt-4", Kind: agents.TranscriptEventTurnCompleted, TurnID: "turn-1"})
		runner.finishRun()
	}()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/run-ws/agents/atlas/sessions/sess-1/events?stream=true", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertSSEHeaders(t, w)

	frames := sseFrames(t, w.Body.String())
	assertDoneLast(t, frames)
	if len(frames) != 6 {
		t.Fatalf("expected run_active, evt-1, evt-2, evt-3, evt-4 + [DONE], got %v", frames)
	}

	var events []agents.TranscriptEvent
	for i, frame := range frames[:len(frames)-1] {
		var ev agents.TranscriptEvent
		if err := json.Unmarshal([]byte(frame), &ev); err != nil {
			t.Fatalf("frame %d is not event JSON: %v (%q)", i, err, frame)
		}
		events = append(events, ev)
	}
	// The synthetic run_active frame leads the stream so a reconnected page
	// flips to its running state before any replay or live event lands.
	if events[0].Kind != agents.TranscriptEventRunActive || events[0].ID != "" {
		t.Errorf("expected a leading run_active status frame, got kind %q id %q", events[0].Kind, events[0].ID)
	}
	wantIDs := []string{"evt-1", "evt-2", "evt-3", "evt-4"}
	for i, wantID := range wantIDs {
		if events[i+1].ID != wantID {
			t.Errorf("frame %d: expected event id %q, got %q", i+1, wantID, events[i+1].ID)
		}
	}
	// The live evt-2 duplicate of the committed event must appear exactly once.
	if got := strings.Count(w.Body.String(), `"evt-2"`); got != 1 {
		t.Errorf("expected evt-2 exactly once (deduped), got %d occurrences", got)
	}

	// Disconnect hygiene: the tap is released with the exact key it was
	// attached under.
	calls := runner.unsubscribeCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one unsubscribe, got %v", calls)
	}
	wantKey := agents.RunKey{WorkspaceID: "ws-run-test", AgentID: "agent-atlas-1", SessionID: "sess-1"}
	if calls[0].key != wantKey {
		t.Errorf("unsubscribe addressed the wrong run, got %v", calls[0].key)
	}
	if calls[0].subID != 1 {
		t.Errorf("expected unsubscribe of sub 1, got %d", calls[0].subID)
	}
	if runner.subscriberCount() != 0 {
		t.Errorf("expected no leaked subscribers, got %d", runner.subscriberCount())
	}
}

// TestAgents_ListSessionEventsStreamClientDisconnect verifies that a client
// going away mid-run ends the handler and releases the subscriber instead of
// leaking the tap.
func TestAgents_ListSessionEventsStreamClientDisconnect(t *testing.T) {
	runner := newFakeStreamingRunner(&agents.HistoryResult{
		Events: []agents.TranscriptEvent{
			{ID: "evt-1", Kind: agents.TranscriptEventTurnStarted, TurnID: "turn-1"},
		},
	})
	runner.liveRun = true
	runner.subscribed = make(chan struct{}, 1)
	gate := make(chan struct{})
	runner.historyGate = gate
	r := newAgentStreamTestEnv(t, runner)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	var w *httptest.ResponseRecorder
	go func() {
		defer close(done)
		w = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/workspaces/run-ws/agents/atlas/sessions/sess-1/events?stream=true", nil).WithContext(ctx)
		r.ServeHTTP(w, req)
	}()

	<-runner.subscribed // the handler attached the tap
	cancel()            // the browser goes away mid-run
	close(gate)         // release history so the handler reaches the pump loop
	<-done

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertSSEHeaders(t, w)
	assertDoneLast(t, sseFrames(t, w.Body.String()))

	calls := runner.unsubscribeCalls()
	if len(calls) != 1 {
		t.Fatalf("expected the abandoned connection's subscriber to be released, got %v", calls)
	}
	if calls[0].subID != 1 {
		t.Errorf("expected unsubscribe of sub 1, got %d", calls[0].subID)
	}
	if runner.subscriberCount() != 0 {
		t.Errorf("expected no leaked subscribers after disconnect, got %d", runner.subscriberCount())
	}
}

// TestAgents_ListSessionEventsStreamAfterCursor verifies the after cursor is
// forwarded to history and events at/before it are not replayed.
func TestAgents_ListSessionEventsStreamAfterCursor(t *testing.T) {
	runner := newFakeStreamingRunner(&agents.HistoryResult{
		// Models the store's cursor filter: only evt-9 is after the cursor.
		Events: []agents.TranscriptEvent{
			{ID: "evt-9", Kind: agents.TranscriptEventMessageCompleted, TurnID: "turn-2"},
		},
	})
	runner.historyReqs = make(chan agents.HistoryRequest, 1)
	r := newAgentStreamTestEnv(t, runner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/run-ws/agents/atlas/sessions/sess-1/events?stream=true&after=evt-8", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case got := <-runner.historyReqs:
		if got.After != "evt-8" {
			t.Errorf("expected the after cursor to reach history, got %q", got.After)
		}
	default:
		t.Fatal("expected the handler to query history")
	}

	frames := sseFrames(t, w.Body.String())
	assertDoneLast(t, frames)
	if len(frames) != 2 {
		t.Fatalf("expected only the post-cursor event + [DONE], got %v", frames)
	}
	var ev agents.TranscriptEvent
	if err := json.Unmarshal([]byte(frames[0]), &ev); err != nil {
		t.Fatalf("frame is not event JSON: %v", err)
	}
	if ev.ID != "evt-9" {
		t.Errorf("expected only evt-9 after the cursor, got %q", ev.ID)
	}
	if strings.Contains(w.Body.String(), "evt-8") {
		t.Errorf("the at-cursor event evt-8 must not be replayed, got:\n%s", w.Body.String())
	}
}

// TestAgents_ListSessionEventsStreamUnknownAgentIsJSONError verifies the
// not-found envelope is written before the stream switches to SSE.
func TestAgents_ListSessionEventsStreamUnknownAgentIsJSONError(t *testing.T) {
	runner := newFakeStreamingRunner(&agents.HistoryResult{Events: []agents.TranscriptEvent{}})
	r := newAgentStreamTestEnv(t, runner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/run-ws/agents/ghost/sessions/sess-1/events?stream=true", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown agent, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got == "text/event-stream" {
		t.Errorf("error responses must stay JSON, got SSE headers")
	}
	if strings.Contains(w.Body.String(), "data:") {
		t.Errorf("no SSE frames may precede the error envelope, got:\n%s", w.Body.String())
	}
	if runner.subscriberCount() != 0 {
		t.Errorf("no tap may be attached for an unknown agent, got %d", runner.subscriberCount())
	}
}
