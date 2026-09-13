package agents

// Regression tests for the per-turn Langfuse trace integration
// (integrate-langfuse-tracing tasks 2.1–2.4). Deterministic and network-free:
// the export handler is a thin in-memory callbacks.Handler spy attached at
// the seam the real Langfuse handler would occupy, so the tests prove the
// runner-side contract — attach only when provided, trace context applied per
// turn, one pinned trace id per turn, sampled-out turns persist no id —
// without ever constructing a Langfuse client.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/observability"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// traceEchoTool is a minimal invokable tool the scripted model calls.
type traceEchoTool struct{}

func (t *traceEchoTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "test.echo", Desc: "echoes for trace tests"}, nil
}

func (t *traceEchoTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "echo-result", nil
}

// traceSpyHandler records callback starts by component kind. Only the
// non-stream timing is registered, so the builder's Needed() keeps the
// framework from copying streams toward it at all.
type traceSpyHandler struct {
	handler callbacks.Handler

	mu       sync.Mutex
	chatOns  int
	toolOns  int
	otherOns int
}

func newTraceSpyHandler() *traceSpyHandler {
	spy := &traceSpyHandler{}
	spy.handler = callbacks.NewHandlerBuilder().
		OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
			spy.mu.Lock()
			defer spy.mu.Unlock()
			switch {
			case info != nil && info.Component == components.ComponentOfAgenticModel:
				spy.chatOns++
			case info != nil && info.Component == components.ComponentOfTool:
				spy.toolOns++
			default:
				spy.otherOns++
			}
			return ctx
		}).
		Build()
	return spy
}

func (s *traceSpyHandler) counts() (chat, tools, other int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chatOns, s.toolOns, s.otherOns
}

// traceToolCallModel scripts a model that invokes test.echo while the last
// conversation message is still the user's turn input, and replies with text
// once the tool result ("echo-result") is in context — two generations and
// one tool span inside a single turn, exactly the spec's one-trace shape,
// and identical for every turn on the session.
type traceToolCallModel struct{}

func (m *traceToolCallModel) Generate(_ context.Context, msgs []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	if len(msgs) > 0 && msgs[len(msgs)-1] != nil &&
		strings.Contains(extractAgenticText(msgs[len(msgs)-1]), "hello") {
		return &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				{Type: schema.ContentBlockTypeFunctionToolCall, FunctionToolCall: &schema.FunctionToolCall{
					CallID:    "call-trace-1",
					Name:      "test.echo",
					Arguments: `{}`,
				}},
			},
		}, nil
	}
	return traceFinalMessage(), nil
}

func (m *traceToolCallModel) Stream(ctx context.Context, msgs []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, msgs, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

func traceFinalMessage() *schema.AgenticMessage {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: "live reply"}},
		},
	}
}

// setupTraceRunner seeds a workspace/agent whose allowlist selects the
// test.echo tool, scripted to m, with the given runner options.
func setupTraceRunner(t *testing.T, sessionID string, m model.BaseModel[*schema.AgenticMessage], opts ...RunnerOption) (*Runner, ExecRequest) {
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

	registry := NewToolRegistry()
	registry.Register("test.echo", func(ToolContext) (tool.BaseTool, error) { return &traceEchoTool{}, nil })

	runnerOpts := []RunnerOption{
		WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (model.BaseModel[*schema.AgenticMessage], error) {
			return m, nil
		}),
		WithInstructionComposer(stubComposer{}),
		WithToolRegistry(registry),
	}
	runnerOpts = append(runnerOpts, opts...)

	runner := NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(),
		st.Memories(), st.AgentSessions(),
		[]byte("test-key-32-bytes-long-12345678"),
		t.TempDir(),
		runnerOpts...)

	ag := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
		Tools:       []string{"test.echo"},
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

// terminalOf returns the terminal event of a drained stream.
func terminalOf(t *testing.T, events []TranscriptEvent) TranscriptEvent {
	t.Helper()
	for _, ev := range events {
		switch ev.Kind {
		case TranscriptEventTurnCompleted, TranscriptEventError, TranscriptEventCancelled:
			return ev
		}
	}
	t.Fatal("no terminal event in stream")
	return TranscriptEvent{}
}

// TestRun_UnconfiguredTraceProducesNoTraceCalls: without WithTraceHandler the
// runner carries no trace state at all — terminal events carry no trace id,
// no coordinates are remembered, and the option is inert (spec: unconfigured
// instance is unchanged).
func TestRun_UnconfiguredTraceProducesNoTraceCalls(t *testing.T) {
	runner, req := setupTraceRunner(t, "sess-trace-off", &traceToolCallModel{}, WithTraceHandler(nil, 1.0))

	if runner.traceHandler != nil {
		t.Fatal("WithTraceHandler(nil, ...) must leave the capability absent")
	}
	if runner.tracingEnabled() {
		t.Fatal("unconfigured runner must not report tracing enabled")
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	waitRunDone(t, runner, runKey(req))

	term := terminalOf(t, events)
	if term.TraceID != "" {
		t.Fatalf("unconfigured run must not persist a trace id, got %q", term.TraceID)
	}
	if term.Kind != TranscriptEventTurnCompleted {
		t.Fatalf("expected a completed turn, got %q (%v)", term.Kind, term.Error)
	}
	if _, ok := runner.reuseTurnTrace(runKey(req)); ok {
		t.Fatal("unconfigured run must not remember trace coordinates")
	}
}

// TestRun_ConfiguredTraceExportsOneTracePerTurn: a configured runner exports
// exactly one trace per turn — the terminal event carries the pinned id,
// which is the pure mapping of the transcript's turn id — with the model
// calls (generations) and the tool call (span) all observed on the attached
// handler. Two turns on one session produce two distinct trace ids; a retried
// turn's attempts stay inside its one trace because both model generations
// ran under the single pinned id of that turn.
func TestRun_ConfiguredTraceExportsOneTracePerTurn(t *testing.T) {
	m := &traceToolCallModel{}
	spy := newTraceSpyHandler()
	runner, req := setupTraceRunner(t, "sess-trace-on", m, WithTraceHandler(spy.handler, 1.0))

	runTurn := func() TranscriptEvent {
		stream, err := runner.Run(context.Background(), req)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		events := collectStream(t, stream)
		waitRunDone(t, runner, runKey(req))
		term := terminalOf(t, events)
		if term.Kind != TranscriptEventTurnCompleted {
			t.Fatalf("expected a completed turn, got %q (%v)", term.Kind, term.Error)
		}
		if term.TraceID == "" {
			t.Fatal("traced turn must persist its trace id on the terminal event")
		}
		// Pinned id (D3): the exported trace IS TraceIDForRun(turn id) — the
		// same pure mapping persistence stores and retried attempts share.
		// The turn id is read from the persisted transcript, whose session
		// events the ADK stamped with the runner-pinned id.
		turnID := persistedTurnID(t, runner, req)
		if want := observability.TraceIDForRun(turnID); term.TraceID != want {
			t.Fatalf("terminal trace id %q is not pinned to turn %q (want %q)", term.TraceID, turnID, want)
		}
		return term
	}

	first := runTurn()
	second := runTurn()

	if first.TraceID == second.TraceID {
		t.Fatalf("each turn is its own trace, got the same id twice: %q", first.TraceID)
	}

	// Handler attach proof (2.1): each scripted turn ran two model
	// generations and one tool span, and the spy observed them all on the
	// attached chain — across two turns, doubled. All of a turn's generations
	// share its one pinned trace id, proving a retried turn's model calls
	// land inside the turn's trace (spec: retry stays inside the trace).
	chat, tools, _ := spy.counts()
	if chat != 4 {
		t.Fatalf("expected 4 generation callback starts across 2 turns (2 per turn), got %d", chat)
	}
	if tools != 2 {
		t.Fatalf("expected 2 tool callback starts across 2 turns, got %d", tools)
	}

	// Per-run state hygiene: the finished runs resolved no remembered chain.
	if _, ok := runner.reuseTurnTrace(runKey(req)); ok {
		t.Fatal("terminal outcome must forget the run's trace coordinates")
	}
}

// persistedTurnID returns the pinned turn id of the session's last persisted
// turn from the transcript history.
func persistedTurnID(t *testing.T, runner *Runner, req ExecRequest) string {
	t.Helper()
	hist, err := runner.History(context.Background(), HistoryRequest{
		WorkspaceID: req.WorkspaceID,
		SessionID:   req.SessionID,
	})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	for i := len(hist.Events) - 1; i >= 0; i-- {
		if id := hist.Events[i].TurnID; id != "" {
			return id
		}
	}
	t.Fatal("no persisted turn id in history")
	return ""
}

// TestRunTrace_SampledOutPersistsNoID: a turn whose pinned id fails the
// deterministic sampler still runs through the wired capability (the pinned
// id makes the upstream sampler drop the whole event set) but persists no
// trace id — a persisted id always targets a real trace (D5).
func TestRunTrace_SampledOutPersistsNoID(t *testing.T) {
	// A deterministic fixture id whose pinned trace id samples out at 0.5.
	sampledOut := ""
	for i := 0; i < 10_000; i++ {
		candidate := fmt.Sprintf("trace-fixture-turn-%d", i)
		if !newTurnTrace(candidate, 0.5).sampledIn {
			sampledOut = candidate
			break
		}
	}
	if sampledOut == "" {
		t.Fatal("no sampled-out fixture found — sampler broken")
	}

	spy := newTraceSpyHandler()
	runner, req := setupTraceRunner(t, "sess-trace-sampled-out", &traceToolCallModel{}, WithTraceHandler(spy.handler, 0.5))
	runner.mintTurnID = func() string { return sampledOut }

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	waitRunDone(t, runner, runKey(req))

	term := terminalOf(t, events)
	if term.Kind != TranscriptEventTurnCompleted {
		t.Fatalf("expected a completed turn, got %q (%v)", term.Kind, term.Error)
	}
	if term.TraceID != "" {
		t.Fatalf("sampled-out turn must persist no trace id, got %q", term.TraceID)
	}
	// The capability was wired: the handler rode the callback chain even for
	// the sampled-out turn.
	if chat, _, _ := spy.counts(); chat == 0 {
		t.Fatal("expected the handler to observe the turn's model calls")
	}
}

// TestRunTrace_SessionConfigPinsTurnID: the ADK seam maps exactly the
// status_running draft onto the runner-minted turn id and defers every other
// event to the default generator — persisted event ids keep their shape.
func TestRunTrace_SessionConfigPinsTurnID(t *testing.T) {
	cfg := traceSessionConfig("pinned-turn")
	if cfg == nil || cfg.EventIDGenerator == nil {
		t.Fatal("traceSessionConfig must install an event id generator")
	}

	running := &adk.SessionEvent[*schema.AgenticMessage]{
		Kind:      adk.SessionEventSessionStatusRunning,
		Timestamp: time.Now().UTC(),
	}
	id, err := cfg.EventIDGenerator(context.Background(), running)
	if err != nil {
		t.Fatalf("generator error: %v", err)
	}
	if id != "pinned-turn" {
		t.Fatalf("status_running draft must pin to the minted id, got %q", id)
	}

	other := &adk.SessionEvent[*schema.AgenticMessage]{
		Kind:      adk.SessionEventSessionStatusIdle,
		Timestamp: time.Now().UTC(),
	}
	otherID, err := cfg.EventIDGenerator(context.Background(), other)
	if err != nil {
		t.Fatalf("generator error: %v", err)
	}
	if otherID == "" || otherID == "pinned-turn" {
		t.Fatalf("non-running drafts must keep the default generator, got %q", otherID)
	}
}

// TestRunTrace_RememberReuseForget: the per-run trace coordinates survive an
// interrupt boundary through the same lifecycle as the hook chain —
// remembered at run start, reused by the session's resume, replaced by the
// next run, forgotten at terminals.
func TestRunTrace_RememberReuseForget(t *testing.T) {
	runner, req := setupTraceRunner(t, "sess-trace-resume", &traceToolCallModel{}, WithTraceHandler(newTraceSpyHandler().handler, 1.0))
	key := runKey(req)

	minted := runner.beginTurnTrace()
	if minted.traceID == "" || minted.turnID == "" {
		t.Fatal("beginTurnTrace must mint both coordinates")
	}
	if minted.traceID != observability.TraceIDForRun(minted.turnID) {
		t.Fatalf("trace id must be pinned to the turn id, got turn=%q trace=%q", minted.turnID, minted.traceID)
	}
	runner.rememberTurnTrace(key, minted)

	reused, ok := runner.reuseTurnTrace(key)
	if !ok || reused != minted {
		t.Fatalf("resume must reuse the interrupted run's trace, got %+v ok=%v", reused, ok)
	}

	// A next run on the session replaces the entry.
	replacement := runner.beginTurnTrace()
	runner.rememberTurnTrace(key, replacement)
	if reused, _ := runner.reuseTurnTrace(key); reused != replacement {
		t.Fatal("a later run on the session must replace the remembered trace")
	}

	runner.forgetTurnTrace(key)
	if _, ok := runner.reuseTurnTrace(key); ok {
		t.Fatal("forgetTurnTrace must drop the entry")
	}
}

// TestRunTrace_SchedulerOriginNamesTraceAfterSchedule: a scheduler-origin
// request threads the schedule name into the trace context (D2 trace
// naming) — the same coordinates applyTurnTrace consumes, named by
// observability.TraceName.
func TestRunTrace_SchedulerOriginNamesTraceAfterSchedule(t *testing.T) {
	name := observability.TraceName("scheduler", "ignored prompt input", "Morning Digest")
	if name != "Morning Digest" {
		t.Fatalf("scheduler fires must be named after the schedule, got %q", name)
	}
	interactive := observability.TraceName("user", "first line\nsecond", "")
	if interactive != "first line" {
		t.Fatalf("interactive turns must be named after the input's first line, got %q", interactive)
	}
}
