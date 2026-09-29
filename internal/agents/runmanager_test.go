package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// manyDeltaModel streams the given number of distinct text deltas in one
// model call, producing more tap events than the EventStream buffer holds.
type manyDeltaModel struct {
	deltas int
	calls  int
	mu     sync.Mutex
}

func (m *manyDeltaModel) Generate(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: "final"}},
		},
	}, nil
}

func (m *manyDeltaModel) Stream(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	sr, sw := schema.Pipe[*schema.AgenticMessage](m.deltas)
	for i := 0; i < m.deltas; i++ {
		sw.Send(&schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: fmt.Sprintf("delta-%d", i)}},
			},
		}, nil)
	}
	sw.Close()
	return sr, nil
}

// gateModel blocks the first model call until release is closed (or the
// context is cancelled), letting tests hold a run in flight deterministically.
type gateModel struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
}

func newGateModel() *gateModel {
	return &gateModel{started: make(chan struct{}), release: make(chan struct{})}
}

func (m *gateModel) unblock() { close(m.release) }

func (m *gateModel) generate(ctx context.Context) (*schema.AgenticMessage, error) {
	m.mu.Lock()
	m.calls++
	first := m.calls == 1
	m.mu.Unlock()
	if first {
		close(m.started)
		select {
		case <-m.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: "ok"}},
		},
	}, nil
}

func (m *gateModel) Generate(ctx context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	return m.generate(ctx)
}

func (m *gateModel) Stream(ctx context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.generate(ctx)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

// setupLifecycleRunner seeds a workspace/agent (no tools) with the supplied
// model, for run-lifecycle tests. Extra options configure defaultable runner
// behavior (e.g. a custom tool registry) for individual tests.
func setupLifecycleRunner(t *testing.T, sessionID string, m model.BaseModel[*schema.AgenticMessage], opts ...RunnerOption) (*Runner, ExecRequest) {
	t.Helper()
	ctx := context.Background()
	st := fake.New()

	ws := &domain.Workspace{Slug: "acme", Name: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "prov"}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	user := &domain.User{Email: "u@example.com", Name: "U"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: "owner"}
	if err := st.Roles().Create(ctx, role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("create member: %v", err)
	}

	// NOTE: this go1.27.0 toolchain rejects variadic calls that mix literal
	// arguments with a slice spread (even for builtin append), so the options
	// are assembled into one slice and passed with a spread-only call.
	runnerOpts := []RunnerOption{
		WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (model.BaseModel[*schema.AgenticMessage], error) {
			return m, nil
		}),
		WithInstructionComposer(stubComposer{}),
	}
	runnerOpts = append(runnerOpts, opts...)

	memWorker, memSearch, memGate := newTestMemoryPipeline(st)
	runner := NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(),
		st.Memories(), st.AgentSessions(),
		st.GatewayLinks(), memWorker, memSearch, memGate,
		[]byte("test-key-32-bytes-long-12345678"),
		t.TempDir(),
		runnerOpts...)

	ag := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
		Temperature: 1.0,
	}
	if err := st.Agents().Create(ctx, ag); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(runner.onClawDir), ws.Slug, ag.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("seed agent dir: %v", err)
	}

	req := ExecRequest{
		WorkspaceID: ws.ID,
		AgentID:     ag.ID,
		SessionID:   sessionID,
		UserID:      user.ID,
		Input:       "hello",
	}
	return runner, req
}

// waitRunDone blocks until the run for key deregisters from the runner's
// manager (the run goroutine has fully unwound), or fails the test.
func waitRunDone(t *testing.T, r *Runner, key RunKey) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r.runMgr.mu.Lock()
		_, live := r.runMgr.live[key]
		r.runMgr.mu.Unlock()
		if !live {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("run did not deregister within 10s")
}

func runKey(req ExecRequest) RunKey {
	return RunKey{WorkspaceID: req.WorkspaceID, AgentID: req.AgentID, SessionID: req.SessionID}
}

// TestRun_UnwatchedStreamDoesNotStallRun: the request context is cancelled
// right after start and nobody consumes the tap; the run must still complete
// and persist, with tap drops counted instead of the run wedging.
func TestRun_UnwatchedStreamDoesNotStallRun(t *testing.T) {
	m := &manyDeltaModel{deltas: 200}
	runner, req := setupLifecycleRunner(t, "sess-unwatched", m)

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	cancel() // request returns immediately; run must be detached

	waitRunDone(t, runner, runKey(req))

	// The run goroutine has fully unwound (deregistration is observed after
	// goroutine exit), so the call count is safely readable here.
	if m.calls != 1 {
		t.Fatalf("expected 1 model call, got %d", m.calls)
	}
	if got := stream.Dropped(); got == 0 {
		t.Fatal("expected the unwatched tap to drop overflow events")
	}

	hist, err := runner.History(context.Background(), HistoryRequest{
		WorkspaceID: req.WorkspaceID,
		SessionID:   req.SessionID,
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	completed := 0
	persistedLast := ""
	for _, e := range hist.Events {
		if e.Kind == TranscriptEventMessageCompleted && e.Message != nil && strings.Contains(e.Message.Content, "delta-199") {
			completed++
			persistedLast = e.Message.Content
		}
	}
	if completed == 0 {
		t.Fatalf("expected persisted assistant message containing the last delta, got %+v", hist.Events)
	}
	if !strings.Contains(persistedLast, "delta-199") {
		t.Fatalf("persisted content incomplete: %q", persistedLast)
	}
}

// TestRun_AttachedConsumerLossless: an attached consumer that keeps up
// receives every delta in order plus the terminal event, with zero drops. The
// delta count stays comfortably below the tap buffer (128) so the assertion
// is deterministic — scheduling lag alone can never overflow it. The drop-new
// path for a slow or absent consumer is covered by
// TestRun_UnwatchedStreamDoesNotStallRun, which also proves dropped tap
// events remain recoverable from history.
func TestRun_AttachedConsumerLossless(t *testing.T) {
	const n = 100
	m := &manyDeltaModel{deltas: n}
	runner, req := setupLifecycleRunner(t, "sess-attached", m)

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	waitRunDone(t, runner, runKey(req))

	deltas := 0
	for _, e := range events {
		if e.Kind == TranscriptEventTextDelta {
			want := fmt.Sprintf("delta-%d", deltas)
			if e.TextDelta != want {
				t.Fatalf("delta %d out of order: got %q, want %q", deltas, e.TextDelta, want)
			}
			deltas++
		}
	}
	if deltas != n {
		t.Fatalf("expected %d text deltas, got %d", n, deltas)
	}
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed, got %+v", events)
	}
	if got := stream.Dropped(); got != 0 {
		t.Fatalf("attached consumer must see zero drops, got %d", got)
	}
}

// TestRun_ConcurrentRunOnLiveSessionConflicts: a second Run on a live session
// is rejected with a conflict sentinel; after completion the session runs again.
func TestRun_ConcurrentRunOnLiveSessionConflicts(t *testing.T) {
	m := newGateModel()
	runner, req := setupLifecycleRunner(t, "sess-conflict", m)

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	select {
	case <-m.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first run never reached the model")
	}

	_, err = runner.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected conflict error for concurrent run on live session")
	}
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("error must wrap domain.ErrConflict, got %v", err)
	}

	m.unblock()
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("expected first run to complete, got %+v", events)
	}
	waitRunDone(t, runner, runKey(req))

	second, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("run after completion must be allowed: %v", err)
	}
	if ev := collectStream(t, second); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected second run to complete, got %+v", ev)
	}
	waitRunDone(t, runner, runKey(req))
}

// TestCancelRun: explicit cancel unwinds a live run through the ADK safe-point
// path (Cancelled event after the in-flight tool call completes and is
// recorded); unknown or completed keys report false. A run whose model turns
// a final answer has no safe point left to cancel at, so the fixture uses a
// tool call — the last cancellation point before the run ends.
func TestCancelRun(t *testing.T) {
	blocking := &gateBlockingTool{entered: make(chan struct{}), release: make(chan struct{})}
	reg := NewToolRegistry()
	reg.Register("test.block", func(ToolContext) (tool.BaseTool, error) { return blocking, nil })

	m := &gateToolCallModel{}
	runner, req := setupLifecycleRunner(t, "sess-cancel", m, WithToolRegistry(reg))
	req.AllowedTools = []string{"test.block"}
	key := runKey(req)

	if runner.CancelRun(key.WorkspaceID, key.AgentID, key.SessionID) {
		t.Fatal("CancelRun on unknown key must return false")
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	select {
	case <-blocking.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("run never reached the tool call")
	}

	if !runner.CancelRun(key.WorkspaceID, key.AgentID, key.SessionID) {
		t.Fatal("CancelRun on live run must return true")
	}

	// Release the in-flight tool: the cancel fires at the after-tool-calls
	// safe point, ending the run with the Cancelled event.
	close(blocking.release)

	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventCancelled) {
		t.Fatalf("expected cancelled event after explicit cancel, got %+v", events)
	}
	waitRunDone(t, runner, key)

	if runner.CancelRun(key.WorkspaceID, key.AgentID, key.SessionID) {
		t.Fatal("CancelRun on completed run must return false")
	}
}

// stubAgentCancel satisfies the manager's agentCancel parameter in manager
// unit tests, which exercise registration/drain wiring, not the ADK machine.
func stubAgentCancel(...adk.AgentCancelOption) (*adk.CancelHandle, bool) {
	return nil, false
}

// gateToolCallModel issues one tool call on its first model call and a final
// text answer afterwards, so a run parks inside tool execution. Like
// gateModel, later calls abort with the context error when the run's context
// is cancelled — the safe point after the in-flight tool call.
type gateToolCallModel struct {
	mu    sync.Mutex
	calls int
}

func (m *gateToolCallModel) generate(ctx context.Context) (*schema.AgenticMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.calls++
	first := m.calls == 1
	m.mu.Unlock()
	if first {
		return &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				{Type: schema.ContentBlockTypeFunctionToolCall, FunctionToolCall: &schema.FunctionToolCall{
					CallID:    "call-1",
					Name:      "test.block",
					Arguments: `{}`,
				}},
			},
		}, nil
	}
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: "ok"}},
		},
	}, nil
}

func (m *gateToolCallModel) Generate(ctx context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	return m.generate(ctx)
}

func (m *gateToolCallModel) Stream(ctx context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.generate(ctx)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

// gateBlockingTool models a long-running tool: it signals entry and then
// blocks until the test releases it (returning normally — the in-flight call
// completes and is recorded at the cancel safe point) or the run's context is
// cancelled (the call aborts cleanly with the context error).
type gateBlockingTool struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *gateBlockingTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "test.block", Desc: "blocks until released or cancelled"}, nil
}

func (b *gateBlockingTool) InvokableRun(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
	b.once.Do(func() { close(b.entered) })
	select {
	case <-b.release:
		return "block-result", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// TestRun_CancelBetweenToolCalls: cancelling while a tool call is executing
// (tasks 4.2/4.3, spec "Cancel between tool calls") — the run is cancelled
// through the ADK agent-level cancel machine (adk.WithCancel armed per run),
// so the in-flight call completes and is recorded, the durable cancel marker
// (adk SessionEventCancel) is persisted and History maps it to a persisted
// TranscriptEventCancelled, and a follow-up run on the same session replays a
// consistent history and completes. Reverting the adk.WithCancel wiring makes
// this fail: a plain context cancel persists a session error instead of the
// marker.
func TestRun_CancelBetweenToolCalls(t *testing.T) {
	blocking := &gateBlockingTool{entered: make(chan struct{}), release: make(chan struct{})}
	reg := NewToolRegistry()
	reg.Register("test.block", func(ToolContext) (tool.BaseTool, error) { return blocking, nil })

	m := &gateToolCallModel{}
	runner, req := setupLifecycleRunner(t, "sess-cancel-tool", m, WithToolRegistry(reg))
	req.AllowedTools = []string{"test.block"}
	key := runKey(req)

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	select {
	case <-blocking.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("run never reached the tool call")
	}

	if !runner.CancelRun(key.WorkspaceID, key.AgentID, key.SessionID) {
		t.Fatal("CancelRun on live run must return true")
	}

	// Release the in-flight tool call: the cancel takes effect at the
	// after-tool-calls safe point — the call completes and is recorded, and
	// the run ends before the next model call.
	close(blocking.release)

	// The stream reports the tool call lifecycle (started AND finished — the
	// in-flight call completed instead of wedging or dangling) and then the
	// cancellation.
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventToolCallStarted) {
		t.Fatalf("expected tool_call_started before the cancel, got %+v", events)
	}
	if !hasKind(events, TranscriptEventToolCallFinished) {
		t.Fatalf("expected in-flight tool call to complete and be recorded, got %+v", events)
	}
	if !hasKind(events, TranscriptEventCancelled) {
		t.Fatalf("expected cancelled event after cancel during tool execution, got %+v", events)
	}
	waitRunDone(t, runner, key)
	if runner.CancelRun(key.WorkspaceID, key.AgentID, key.SessionID) {
		t.Fatal("cancelled run must no longer be live")
	}

	// The persisted history must contain the durable cancel marker on the
	// cancelled turn (History maps adk SessionEventCancel → cancelled), with
	// no dangling tool-call records, so the next execution on the thread
	// replays a consistent log.
	hist, err := runner.History(context.Background(), HistoryRequest{
		WorkspaceID: req.WorkspaceID,
		SessionID:   req.SessionID,
	})
	if err != nil {
		t.Fatalf("History after cancel: %v", err)
	}
	if len(hist.Events) == 0 {
		t.Fatal("expected persisted events for the cancelled turn")
	}
	cancelledTurn := ""
	started, finished := 0, 0
	for _, e := range hist.Events {
		switch {
		case e.Kind == TranscriptEventCancelled && cancelledTurn == "":
			cancelledTurn = e.TurnID
		case e.Kind == TranscriptEventToolCallStarted:
			started++
		case e.Kind == TranscriptEventToolCallFinished:
			finished++
		}
	}
	if cancelledTurn == "" {
		t.Fatalf("expected persisted cancel marker in history, got %+v", hist.Events)
	}
	if started != finished {
		t.Fatalf("dangling tool-call records after cancel: %d started, %d finished", started, finished)
	}

	// A subsequent execution on the thread starts from the marked history and
	// completes; history afterwards spans both turns and still shows the
	// cancel marker on the cancelled turn.
	second, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("follow-up run on cancelled session: %v", err)
	}
	if ev := collectStream(t, second); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected follow-up run to complete, got %+v", ev)
	}
	waitRunDone(t, runner, key)

	hist, err = runner.History(context.Background(), HistoryRequest{
		WorkspaceID: req.WorkspaceID,
		SessionID:   req.SessionID,
	})
	if err != nil {
		t.Fatalf("History after follow-up: %v", err)
	}
	turns := map[string]bool{}
	markerOnCancelledTurn := false
	for _, e := range hist.Events {
		if e.TurnID != "" {
			turns[e.TurnID] = true
		}
		if e.Kind == TranscriptEventCancelled && e.TurnID == cancelledTurn {
			markerOnCancelledTurn = true
		}
	}
	if len(turns) < 2 {
		t.Fatalf("expected history to span both turns, got %d turn(s)", len(turns))
	}
	if !markerOnCancelledTurn {
		t.Fatalf("expected the follow-up history to show the cancel marker on turn %q, got %+v", cancelledTurn, hist.Events)
	}
}

// TestRunManager_ActiveRunSessionIDs: the D3 enumeration collects the session
// ids of live runs for exactly the requested workspace+agent pair, excludes
// runs finished under the same key, and returns an empty slice — never nil —
// when nothing is live (agent-session-index task 3.1).
func TestRunManager_ActiveRunSessionIDs(t *testing.T) {
	m := newRunManager(context.Background(), 0)

	if ids := m.ActiveRunSessionIDs("ws", "ag"); ids == nil || len(ids) != 0 {
		t.Fatalf("empty manager must yield an empty (non-nil) slice, got %#v", ids)
	}

	h1, err := m.start(RunKey{"ws", "ag", "s1"}, stubAgentCancel)
	if err != nil {
		t.Fatalf("start s1: %v", err)
	}
	if _, err := m.start(RunKey{"ws", "ag", "s2"}, stubAgentCancel); err != nil {
		t.Fatalf("start s2: %v", err)
	}
	// Same agent in a different workspace, and a different agent in the same
	// workspace: neither may leak into the enumeration.
	if _, err := m.start(RunKey{"other-ws", "ag", "s3"}, stubAgentCancel); err != nil {
		t.Fatalf("start s3: %v", err)
	}
	if _, err := m.start(RunKey{"ws", "other-ag", "s4"}, stubAgentCancel); err != nil {
		t.Fatalf("start s4: %v", err)
	}

	got := map[string]bool{}
	for _, id := range m.ActiveRunSessionIDs("ws", "ag") {
		got[id] = true
	}
	if len(got) != 2 || !got["s1"] || !got["s2"] {
		t.Fatalf("expected {s1 s2} live for ws/ag, got %v", got)
	}

	// A finished run drops out of the enumeration even before its manager
	// deregistration lands (the liveness rule reads the done channel).
	h1.finish()
	<-h1.done
	got = map[string]bool{}
	for _, id := range m.ActiveRunSessionIDs("ws", "ag") {
		got[id] = true
	}
	if len(got) != 1 || !got["s2"] {
		t.Fatalf("expected only {s2} after s1 finished, got %v", got)
	}
}

// TestRunManager_Drain: in-flight runs that finish within the window are left
// alone; stragglers are cancelled; new starts are rejected once draining.
func TestRunManager_Drain(t *testing.T) {
	t.Run("in-flight run finishes within window", func(t *testing.T) {
		m := newRunManager(context.Background(), 0)
		k1 := RunKey{"ws", "ag", "s1"}
		h1, err := m.start(k1, stubAgentCancel)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		go func() {
			time.Sleep(20 * time.Millisecond)
			h1.finish()
		}()

		done := make(chan struct{})
		go func() {
			m.drain(2 * time.Second)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("drain did not return for an in-flight run finishing in time")
		}
		select {
		case <-h1.ctx.Done():
			t.Fatal("run that drained in time must not be cancelled")
		default:
		}

		if _, err := m.start(RunKey{"ws", "ag", "s2"}, stubAgentCancel); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("start during drain must wrap domain.ErrConflict, got %v", err)
		}
	})

	t.Run("straggler is cancelled", func(t *testing.T) {
		m := newRunManager(context.Background(), 0)
		h, err := m.start(RunKey{"ws", "ag", "s-straggler"}, stubAgentCancel)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		m.drain(30 * time.Millisecond)
		select {
		case <-h.ctx.Done():
		default:
			t.Fatal("straggler run must be cancelled after the drain window")
		}
	})

	t.Run("concurrent start conflicts and cancel addressing", func(t *testing.T) {
		m := newRunManager(context.Background(), 0)
		key := RunKey{"ws", "ag", "s-conflict"}
		if _, err := m.start(key, stubAgentCancel); err != nil {
			t.Fatalf("first start: %v", err)
		}
		if _, err := m.start(key, stubAgentCancel); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("second start on live session must wrap domain.ErrConflict, got %v", err)
		}
		if m.cancel(RunKey{"ws", "ag", "unknown"}) {
			t.Fatal("cancel of unknown key must return false")
		}
		if !m.cancel(key) {
			t.Fatal("cancel of live run must return true")
		}
	})
}

// ---- Live subscriber broadcast (reattach & catch-up) ----

// modelGate parks one model call: hold signals arrival on reached and then
// blocks until release, so tests attach live subscribers at a deterministic
// point of a run — before any event is broadcast.
type modelGate struct {
	reached chan struct{}
	release chan struct{}
}

func newModelGate() *modelGate {
	return &modelGate{reached: make(chan struct{}), release: make(chan struct{})}
}

// arrived blocks until the gated model call entered hold.
func (g *modelGate) arrived() { <-g.reached }

// unblock releases the parked model call.
func (g *modelGate) unblock() { close(g.release) }

func (g *modelGate) hold(ctx context.Context) {
	close(g.reached)
	select {
	case <-g.release:
	case <-ctx.Done():
	}
}

// gatedModel wraps another model and parks selected model calls on gates:
// call n (1-based) waits on gates[n] when present; calls without a gate
// proceed unblocked.
type gatedModel struct {
	inner model.BaseModel[*schema.AgenticMessage]
	gates map[int]*modelGate
	mu    sync.Mutex
	calls int
}

func newGatedModel(inner model.BaseModel[*schema.AgenticMessage], gates ...*modelGate) *gatedModel {
	m := &gatedModel{inner: inner, gates: make(map[int]*modelGate, len(gates))}
	for i, g := range gates {
		m.gates[i+1] = g
	}
	return m
}

func (m *gatedModel) park(ctx context.Context) {
	m.mu.Lock()
	m.calls++
	g := m.gates[m.calls]
	m.mu.Unlock()
	if g != nil {
		g.hold(ctx)
	}
}

func (m *gatedModel) Generate(ctx context.Context, in []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	m.park(ctx)
	return m.inner.Generate(ctx, in, opts...)
}

func (m *gatedModel) Stream(ctx context.Context, in []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	m.park(ctx)
	return m.inner.Stream(ctx, in, opts...)
}

// assertSameTranscript asserts got carries exactly the same event kinds and
// text deltas as want, in order.
func assertSameTranscript(t *testing.T, name string, got, want []TranscriptEvent) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d events, want %d", name, len(got), len(want))
	}
	for i := range got {
		if got[i].Kind != want[i].Kind || got[i].TextDelta != want[i].TextDelta {
			t.Fatalf("%s: event %d = %s/%q, want %s/%q",
				name, i, got[i].Kind, got[i].TextDelta, want[i].Kind, want[i].TextDelta)
		}
	}
}

// broadcastWant returns the primary tap's events minus turn_started: the
// drain loop's broadcast fan-out begins after that primary-only opener.
func broadcastWant(primary []TranscriptEvent) []TranscriptEvent {
	var want []TranscriptEvent
	for _, e := range primary {
		if e.Kind != TranscriptEventTurnStarted {
			want = append(want, e)
		}
	}
	return want
}

// TestRun_SubscribersReceiveBroadcastEvents: several subscribers attach to a
// live run and each receives every broadcast event, in order, losslessly;
// after the run ends their streams are closed and new subscriptions are
// refused.
func TestRun_SubscribersReceiveBroadcastEvents(t *testing.T) {
	const n = 50 // below the 128-event subscriber buffer: lossless is deterministic
	gate := newModelGate()
	dm := &manyDeltaModel{deltas: n}
	m := newGatedModel(dm, gate)
	runner, req := setupLifecycleRunner(t, "sess-fanout", m)
	key := runKey(req)

	primary, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	gate.arrived() // parked before any event is broadcast

	const subs = 3
	subStreams := make([]*EventStream, 0, subs)
	for i := 0; i < subs; i++ {
		id, s, ok := runner.SubscribeRun(key)
		if !ok {
			t.Fatalf("subscriber %d: run not live", i)
		}
		if id == 0 || s == nil {
			t.Fatalf("subscriber %d: bad subscription handle (id=%d, stream=%v)", i, id, s)
		}
		subStreams = append(subStreams, s)
	}

	gate.unblock()
	want := broadcastWant(collectStream(t, primary))
	waitRunDone(t, runner, key)

	if dm.calls != 1 {
		t.Fatalf("expected 1 model call, got %d", dm.calls)
	}
	for i, s := range subStreams {
		got := collectStream(t, s) // returns once the run closed the subscriber
		assertSameTranscript(t, fmt.Sprintf("subscriber %d", i), got, want)
		deltas := 0
		for _, e := range got {
			if e.Kind == TranscriptEventTextDelta {
				deltas++
			}
		}
		if deltas != n {
			t.Fatalf("subscriber %d: got %d deltas, want %d", i, deltas, n)
		}
		if !hasKind(got, TranscriptEventTurnCompleted) {
			t.Fatalf("subscriber %d: missing turn_completed, got %+v", i, got)
		}
		if d := s.Dropped(); d != 0 {
			t.Fatalf("subscriber %d: lossless attach must not drop, got %d drops", i, d)
		}
	}

	if _, _, ok := runner.SubscribeRun(key); ok {
		t.Fatal("SubscribeRun must refuse after the run finished")
	}
}

// TestRun_SubscriberUnsubscribeIsolation: unsubscribing one subscriber closes
// only its own stream — the run and the other subscribers are unaffected;
// unknown keys and unknown subscription IDs are no-ops.
func TestRun_SubscriberUnsubscribeIsolation(t *testing.T) {
	const n = 20
	gate := newModelGate()
	m := newGatedModel(&manyDeltaModel{deltas: n}, gate)
	runner, req := setupLifecycleRunner(t, "sess-unsub", m)
	key := runKey(req)

	primary, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	gate.arrived()

	idA, streamA, ok := runner.SubscribeRun(key)
	if !ok {
		t.Fatal("subscriber A: run not live")
	}
	_, streamB, ok := runner.SubscribeRun(key)
	if !ok {
		t.Fatal("subscriber B: run not live")
	}

	// Unknown key and unknown subscription ID are no-ops: neither may close
	// the live subscriptions or disturb the run.
	runner.UnsubscribeRun(RunKey{WorkspaceID: "other-ws", AgentID: "other-ag", SessionID: "other-sess"}, idA)
	runner.UnsubscribeRun(key, 424242)

	runner.UnsubscribeRun(key, idA)

	gate.unblock()
	want := broadcastWant(collectStream(t, primary))
	waitRunDone(t, runner, key)

	// A was detached by the unsubscribe: its stream is closed and drains to
	// EOF immediately, having received nothing further.
	if ev, err := streamA.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("unsubscribed stream must be closed (io.EOF), got event=%+v err=%v", ev, err)
	}

	// B still receives every broadcast event, in order, losslessly.
	gotB := collectStream(t, streamB)
	assertSameTranscript(t, "subscriber B", gotB, want)
	if d := streamB.Dropped(); d != 0 {
		t.Fatalf("subscriber B: got %d drops, want 0", d)
	}
}

// TestRun_SlowSubscriberDoesNotStallRun: a subscriber that never drains while
// the run is live absorbs at most one buffer of events and counts the rest as
// drops; the run completes and a concurrently-draining subscriber is
// unaffected, with every fanned-out event accounted for per subscriber.
func TestRun_SlowSubscriberDoesNotStallRun(t *testing.T) {
	const n = 300 // far beyond the 128-event subscriber buffer
	gate := newModelGate()
	dm := &manyDeltaModel{deltas: n}
	m := newGatedModel(dm, gate)
	runner, req := setupLifecycleRunner(t, "sess-slow-sub", m)
	key := runKey(req)

	// The primary tap is deliberately left unconsumed here: completion is
	// proven via persisted history, so this test only observes subscribers.
	_, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	gate.arrived()

	_, slow, ok := runner.SubscribeRun(key)
	if !ok {
		t.Fatal("slow subscriber: run not live")
	}
	_, fast, ok := runner.SubscribeRun(key)
	if !ok {
		t.Fatal("healthy subscriber: run not live")
	}

	var fastEvents []TranscriptEvent
	fastDone := make(chan struct{})
	go func() {
		defer close(fastDone)
		for {
			ev, err := fast.Recv()
			if err != nil {
				return
			}
			fastEvents = append(fastEvents, *ev)
		}
	}()

	// slow is never drained while the run is live.
	gate.unblock()
	waitRunDone(t, runner, key)
	<-fastDone

	// The run completed and persisted the full transcript despite the stalled
	// subscriber.
	if dm.calls != 1 {
		t.Fatalf("expected 1 model call, got %d", dm.calls)
	}
	hist, err := runner.History(context.Background(), HistoryRequest{
		WorkspaceID: req.WorkspaceID,
		SessionID:   req.SessionID,
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	persisted := false
	for _, e := range hist.Events {
		if e.Kind == TranscriptEventMessageCompleted && e.Message != nil && strings.Contains(e.Message.Content, "delta-299") {
			persisted = true
		}
	}
	if !persisted {
		t.Fatalf("run did not persist the full transcript, got %+v", hist.Events)
	}

	// Exactly n deltas + message_completed + turn_completed were fanned out
	// per subscriber, and every one of them is either delivered or counted as
	// a drop. Drop-new keeps the earliest events, so the stalled subscriber
	// buffered the stream's opening and dropped the overflow.
	const total = n + 2
	slowGot := collectStream(t, slow) // closed when the run finished
	if len(slowGot) == 0 {
		t.Fatal("stalled subscriber must still receive the buffered prefix")
	}
	if slowGot[0].Kind != TranscriptEventTextDelta || slowGot[0].TextDelta != "delta-0" {
		t.Fatalf("stalled subscriber prefix out of order: got %s/%q", slowGot[0].Kind, slowGot[0].TextDelta)
	}
	if got := len(slowGot) + slow.Dropped(); got != total {
		t.Fatalf("slow subscriber accounted for %d events, want %d", got, total)
	}
	if slow.Dropped() == 0 {
		t.Fatal("stalled subscriber must have counted drops")
	}
	if got := len(fastEvents) + fast.Dropped(); got != total {
		t.Fatalf("healthy subscriber accounted for %d events, want %d", got, total)
	}
}

// TestRun_CancelledRunClosesSubscribers: a subscriber attached to a run that
// exits drainAgentEvents through the cancel short-circuit still receives the
// cancelled event and drains to EOF when the run unwinds.
func TestRun_CancelledRunClosesSubscribers(t *testing.T) {
	blocking := &gateBlockingTool{entered: make(chan struct{}), release: make(chan struct{})}
	reg := NewToolRegistry()
	reg.Register("test.block", func(ToolContext) (tool.BaseTool, error) { return blocking, nil })

	m := &gateToolCallModel{}
	runner, req := setupLifecycleRunner(t, "sess-sub-cancel", m, WithToolRegistry(reg))
	req.AllowedTools = []string{"test.block"}
	key := runKey(req)

	_, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	select {
	case <-blocking.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("run never reached the tool call")
	}

	_, sub, ok := runner.SubscribeRun(key)
	if !ok {
		t.Fatal("subscriber: run not live")
	}

	if !runner.CancelRun(key.WorkspaceID, key.AgentID, key.SessionID) {
		t.Fatal("CancelRun on live run must return true")
	}
	close(blocking.release)

	events := collectStream(t, sub) // returns once the cancelled run closed the subscriber
	if !hasKind(events, TranscriptEventCancelled) {
		t.Fatalf("expected the cancelled event to be broadcast, got %+v", events)
	}
	waitRunDone(t, runner, key)
	if _, _, ok := runner.SubscribeRun(key); ok {
		t.Fatal("SubscribeRun must refuse after the cancelled run finished")
	}
}

// setupGatedApprovalRunner seeds the approval-flow rig (shell exposed —
// default-on under the denylist, autonomy=approval) wired to the supplied
// model, for interrupt-path subscriber tests.
func setupGatedApprovalRunner(t *testing.T, sessionID string, m model.BaseModel[*schema.AgenticMessage]) (*Runner, ExecRequest) {
	t.Helper()
	ctx := context.Background()
	st := fake.New()

	ws := &domain.Workspace{Slug: "acme", Name: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "prov"}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	user := &domain.User{Email: "u@example.com", Name: "U"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: "owner"}
	if err := st.Roles().Create(ctx, role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("create member: %v", err)
	}

	memWorker, memSearch, memGate := newTestMemoryPipeline(st)
	runner := NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(),
		st.Memories(), st.AgentSessions(),
		st.GatewayLinks(), memWorker, memSearch, memGate,
		[]byte("test-key-32-bytes-long-12345678"),
		t.TempDir(),
		WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (model.BaseModel[*schema.AgenticMessage], error) {
			return m, nil
		}),
		WithInstructionComposer(stubComposer{}),
	)

	ag := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
		Temperature: 1.0,
		Autonomy:    domain.AutonomyApproval,
	}
	if err := st.Agents().Create(ctx, ag); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(runner.onClawDir), ws.Slug, ag.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("seed agent dir: %v", err)
	}

	req := ExecRequest{
		WorkspaceID: ws.ID,
		AgentID:     ag.ID,
		SessionID:   sessionID,
		UserID:      user.ID,
		Input:       "clean up the build directory",
	}
	return runner, req
}

// TestRun_InterruptedRunClosesSubscribers: the approval-interrupt
// short-circuit in drainAgentEvents broadcasts approval_required to attached
// subscribers and closes them when the run ends, without a terminal event; a
// resumed turn fans out and closes its subscribers the same way.
func TestRun_InterruptedRunClosesSubscribers(t *testing.T) {
	runGate := newModelGate()
	resumeGate := newModelGate()
	m := newGatedModel(newScriptedToolCallModel(), runGate, resumeGate)
	runner, req := setupGatedApprovalRunner(t, "sess-sub-interrupt", m)
	key := runKey(req)

	primary, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	runGate.arrived()

	_, sub, ok := runner.SubscribeRun(key)
	if !ok {
		t.Fatal("subscriber: run not live")
	}
	runGate.unblock()

	runEvents := collectStream(t, primary)
	if !hasKind(runEvents, TranscriptEventApprovalRequired) {
		t.Fatalf("expected approval_required, got %+v", runEvents)
	}
	waitRunDone(t, runner, key)

	// The subscriber saw the interrupt broadcast and was closed by the
	// short-circuit exit: no terminal event, EOF after drain.
	subEvents := collectStream(t, sub)
	if !hasKind(subEvents, TranscriptEventApprovalRequired) {
		t.Fatalf("expected approval_required broadcast to the subscriber, got %+v", subEvents)
	}
	if hasKind(subEvents, TranscriptEventTurnCompleted) || hasKind(subEvents, TranscriptEventError) || hasKind(subEvents, TranscriptEventCancelled) {
		t.Fatalf("interrupt short-circuit must not broadcast a terminal event, got %+v", subEvents)
	}
	var approval *ApprovalPayload
	for i := range subEvents {
		if subEvents[i].Approval != nil {
			approval = subEvents[i].Approval
		}
	}
	if approval == nil || approval.InterruptID == "" {
		t.Fatalf("approval payload incomplete: %+v", approval)
	}

	// The resumed turn fans out too: attach while it is parked at its gate,
	// release, and observe the completion broadcast and the subscriber close.
	resumePrimary, err := runner.Resume(context.Background(), req, approval, false)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	resumeGate.arrived()

	_, resumeSub, ok := runner.SubscribeRun(key)
	if !ok {
		t.Fatal("resume subscriber: run not live")
	}
	resumeGate.unblock()

	resumeEvents := collectStream(t, resumePrimary)
	if !hasKind(resumeEvents, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed after denial, got %+v", resumeEvents)
	}
	got := collectStream(t, resumeSub)
	if !hasKind(got, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed broadcast on the resumed run, got %+v", got)
	}
	waitRunDone(t, runner, key)
}
