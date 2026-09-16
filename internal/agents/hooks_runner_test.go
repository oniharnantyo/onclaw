package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// hooksModel is a scripted agentic model: the first call emits one tool call
// (when tooled is set), later calls emit fixed text. It records every input
// so tests can assert exactly what the model received — e.g. that a blocked
// tool call surfaced as the block JSON tool result, or that a blocked prompt
// never reached it at all.
type hooksModel struct {
	mu       sync.Mutex
	calls    int
	tooled   bool
	toolName string
	toolArgs string
	final    string
	inputs   [][]*schema.AgenticMessage
}

func (m *hooksModel) Generate(ctx context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	m.mu.Lock()
	m.calls++
	m.inputs = append(m.inputs, input)
	call := m.calls
	tooled := m.tooled
	m.mu.Unlock()

	if tooled && call == 1 {
		return &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				{Type: schema.ContentBlockTypeFunctionToolCall, FunctionToolCall: &schema.FunctionToolCall{
					CallID:    "call-1",
					Name:      m.toolName,
					Arguments: m.toolArgs,
				}},
			},
		}, nil
	}
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: m.final}},
		},
	}, nil
}

func (m *hooksModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

func (m *hooksModel) callCount(t *testing.T) int {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// inputToolResultText returns the tool-result text the model received on the
// given call — where a pre_tool_use block must surface.
func (m *hooksModel) inputToolResultText(t *testing.T, call int) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if call < 1 || call > len(m.inputs) {
		t.Fatalf("model call %d not recorded (%d calls)", call, len(m.inputs))
	}
	var sb strings.Builder
	for _, msg := range m.inputs[call-1] {
		for _, res := range agenticToolResults(msg) {
			sb.WriteString(res.Result)
		}
	}
	return sb.String()
}

// setupHooksRunner seeds a workspace, agent (with the given allowlisted
// tools), user, provider, and workspace hooks, and wires the runner with the
// real hook dispatcher over the fake store. Extra opts configure further
// runner knobs (e.g. the channel ports for channel-run tests).
func setupHooksRunner(t *testing.T, tools []string, mdl *hooksModel, hookList ...*domain.WorkspaceHook) (store.Store, *Runner, *domain.Workspace, *domain.Agent, ExecRequest) {
	return setupHooksRunnerWithOpts(t, tools, mdl, nil, hookList...)
}

func setupHooksRunnerWithOpts(t *testing.T, tools []string, mdl *hooksModel, opts []RunnerOption, hookList ...*domain.WorkspaceHook) (store.Store, *Runner, *domain.Workspace, *domain.Agent, ExecRequest) {
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
	for _, h := range hookList {
		h.WorkspaceID = ws.ID
		if err := st.Hooks().CreateWorkspaceHook(ctx, h); err != nil {
			t.Fatalf("create hook %q: %v", h.Name, err)
		}
	}

	onClawDir := t.TempDir()
	runner := NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(), st.Memories(), st.AgentSessions(),
		[]byte("test-key-32-bytes-long-12345678"),
		onClawDir,
		WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (Model, error) {
			return mdl, nil
		}),
		WithInstructionComposer(stubComposer{}),
		WithHooks(agenthooks.NewDispatcher(st.Hooks(), agenthooks.NewRegistry())),
	)
	for _, opt := range opts {
		opt(runner)
	}

	ag := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
		Tools:       tools,
	}
	if err := st.Agents().Create(ctx, ag); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Seed the agent's on-disk workspace directory (the jail root).
	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(runner.onClawDir), ws.Slug, ag.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("seed agent dir: %v", err)
	}

	req := ExecRequest{
		WorkspaceID: ws.ID,
		AgentID:     ag.ID,
		SessionID:   "sess-" + ag.Slug,
		UserID:      user.ID,
		Input:       "do the thing",
	}
	return st, runner, ws, ag, req
}

func findPromptBlocked(events []TranscriptEvent) *TranscriptEvent {
	for i := range events {
		if events[i].Kind == TranscriptEventPromptBlocked {
			return &events[i]
		}
	}
	return nil
}

func TestHooks_BlockedToolCallContinuesRun(t *testing.T) {
	mdl := &hooksModel{tooled: true, toolName: "read_file", toolArgs: `{"path":"/tmp/notes.md"}`, final: "I could not read the file, but here is the plan."}
	hook := blockedToolHook(t, "policy-guard", []string{"read_file"})
	_, runner, ws, ag, req := setupHooksRunner(t, []string{"read_file"}, mdl, hook)

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)

	// The block is tool data, never a run failure or a human interrupt (D3).
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the run to continue to turn_completed, got %+v", events)
	}
	if hasKind(events, TranscriptEventError) || hasKind(events, TranscriptEventApprovalRequired) || hasKind(events, TranscriptEventPromptBlocked) {
		t.Fatalf("block must not surface as error/approval/prompt_blocked: %+v", events)
	}
	// The model read the block result and adapted: two calls, the second fed
	// the canonical block JSON as the tool result.
	if got := mdl.callCount(t); got != 2 {
		t.Fatalf("model calls = %d, want 2", got)
	}
	wantBlock := agenthooks.BlockToolResult("policy-guard", "blocked by policy")
	if got := mdl.inputToolResultText(t, 2); got != wantBlock {
		t.Fatalf("tool result fed to the model = %q, want canonical %q", got, wantBlock)
	}
	// The blocked call is visible in the transcript as a normal tool card.
	var finished *ToolResultPayload
	for i := range events {
		if events[i].Kind == TranscriptEventToolCallFinished && events[i].ToolResult != nil {
			finished = events[i].ToolResult
		}
	}
	if finished == nil || finished.CallID != "call-1" {
		t.Fatalf("expected a tool_call_finished for call-1, got %+v", events)
	}

	// Hydrated transcript shows the blocked call identically: the tool result
	// join carries the block JSON (design.md D18).
	hist, err := runner.History(context.Background(), HistoryRequest{WorkspaceID: ws.ID, AgentID: ag.ID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	var hydratedResult *ToolResultPayload
	for _, ev := range hist.Events {
		if ev.Kind == TranscriptEventToolCallFinished && ev.ToolResult != nil && ev.ToolResult.CallID == "call-1" {
			hydratedResult = ev.ToolResult
		}
	}
	if hydratedResult == nil || hydratedResult.Result != wantBlock {
		t.Fatalf("hydrated tool result = %+v, want the block JSON %q", hydratedResult, wantBlock)
	}
	if !hasKind(hist.Events, TranscriptEventTurnCompleted) {
		t.Fatalf("hydrated transcript must end in turn_completed, got %+v", hist.Events)
	}
}

func TestHooks_BlockedPromptNeverReachesModel(t *testing.T) {
	mdl := &hooksModel{final: "should never be produced"}
	hook := &domain.WorkspaceHook{
		HookBase: domain.HookBase{
			Name:        "prompt-gate",
			Event:       domain.HookEventUserPromptSubmit,
			Matcher:     "*",
			HandlerType: domain.HookHandlerCommand,
			Config:      mustHookJSON(t, map[string]any{"command": "sh", "args": []string{"-c", "echo prompt denied by policy >&2; exit 2"}}),
			TimeoutMS:   5000,
			OnFailure:   domain.HookFailureAllow,
			Enabled:     true,
		},
	}
	_, runner, ws, ag, req := setupHooksRunner(t, nil, mdl, hook)

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)

	// Well-formed terminal (D6): notice + turn_completed, zero model calls.
	if mdl.callCount(t) != 0 {
		t.Fatal("a blocked prompt must never reach the model")
	}
	blocked := findPromptBlocked(events)
	if blocked == nil || blocked.PromptBlocked == nil {
		t.Fatalf("expected a prompt_blocked event, got %+v", events)
	}
	if blocked.PromptBlocked.Hook != "prompt-gate" || blocked.PromptBlocked.Reason != "prompt denied by policy" {
		t.Fatalf("prompt_blocked payload = %+v", blocked.PromptBlocked)
	}
	if len(events) < 2 || events[len(events)-1].Kind != TranscriptEventTurnCompleted {
		t.Fatalf("prompt_blocked must be followed by turn_completed, got %+v", events)
	}

	// Reload: the notice persists and renders identically (D6/D18).
	hist, err := runner.History(context.Background(), HistoryRequest{WorkspaceID: ws.ID, AgentID: ag.ID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist.Events) != 2 {
		t.Fatalf("hydrated events = %+v, want [prompt_blocked turn_completed]", hist.Events)
	}
	if hist.Events[0].Kind != TranscriptEventPromptBlocked ||
		hist.Events[0].PromptBlocked == nil ||
		hist.Events[0].PromptBlocked.Hook != "prompt-gate" ||
		hist.Events[0].PromptBlocked.Reason != "prompt denied by policy" {
		t.Fatalf("hydrated notice = %+v", hist.Events[0])
	}
	if hist.Events[1].Kind != TranscriptEventTurnCompleted {
		t.Fatalf("hydrated terminal = %+v, want turn_completed", hist.Events[1])
	}
}

func TestHooks_OriginGatesPromptSubmit(t *testing.T) {
	mdl := &hooksModel{final: "ok"}
	hook := &domain.WorkspaceHook{
		HookBase: domain.HookBase{
			Name:        "scheduler-gate",
			Event:       domain.HookEventUserPromptSubmit,
			Matcher:     "scheduler",
			HandlerType: domain.HookHandlerCommand,
			Config:      mustHookJSON(t, map[string]any{"command": "sh", "args": []string{"-c", "echo no scheduled runs on weekends >&2; exit 2"}}),
			TimeoutMS:   5000,
			OnFailure:   domain.HookFailureAllow,
			Enabled:     true,
		},
	}
	// The heartbeat gate (add-agent-heartbeat D11): origin heartbeat joins the
	// origin-matched events, so a heartbeat tick is blockable exactly like a
	// scheduler fire — and neither gate matches the other's origin.
	heartbeatHook := &domain.WorkspaceHook{
		HookBase: domain.HookBase{
			Name:        "heartbeat-gate",
			Event:       domain.HookEventUserPromptSubmit,
			Matcher:     "heartbeat",
			HandlerType: domain.HookHandlerCommand,
			Config:      mustHookJSON(t, map[string]any{"command": "sh", "args": []string{"-c", "echo no heartbeat checks at night >&2; exit 2"}}),
			TimeoutMS:   5000,
			OnFailure:   domain.HookFailureAllow,
			Enabled:     true,
		},
	}
	st, runner, _, ag, req := setupHooksRunner(t, nil, mdl, hook, heartbeatHook)

	// Origin scheduler: the hook's matcher selects scheduler → blocked before
	// the model.
	schedulerReq := req
	schedulerReq.SessionID = "sess-scheduler"
	schedulerReq.Origin = OriginScheduler
	stream, err := runner.Run(context.Background(), schedulerReq)
	if err != nil {
		t.Fatalf("Run (scheduler): %v", err)
	}
	schedulerEvents := collectStream(t, stream)
	if findPromptBlocked(schedulerEvents) == nil || mdl.callCount(t) != 0 {
		t.Fatalf("scheduler-origin run must be blocked before the model, got %+v", schedulerEvents)
	}

	// Origin heartbeat: the heartbeat gate's matcher selects heartbeat →
	// blocked before the model, same as scheduler (add-agent-heartbeat D11).
	hbReq := req
	hbReq.SessionID = "sess-heartbeat"
	hbReq.Origin = OriginHeartbeat
	stream, err = runner.Run(context.Background(), hbReq)
	if err != nil {
		t.Fatalf("Run (heartbeat): %v", err)
	}
	hbEvents := collectStream(t, stream)
	blocked := findPromptBlocked(hbEvents)
	if blocked == nil || blocked.PromptBlocked == nil || blocked.PromptBlocked.Hook != "heartbeat-gate" {
		t.Fatalf("heartbeat-origin run must be blocked by the heartbeat gate before the model, got %+v", hbEvents)
	}
	if mdl.callCount(t) != 0 {
		t.Fatal("a heartbeat-origin block must never reach the model")
	}

	// Origin user (default): neither gate matches → normal turn.
	userReq := req
	userReq.SessionID = "sess-user"
	stream, err = runner.Run(context.Background(), userReq)
	if err != nil {
		t.Fatalf("Run (user): %v", err)
	}
	userEvents := collectStream(t, stream)
	if !hasKind(userEvents, TranscriptEventTurnCompleted) || findPromptBlocked(userEvents) != nil {
		t.Fatalf("user-origin run must complete normally, got %+v", userEvents)
	}
	if got := mdl.callCount(t); got != 1 {
		t.Fatalf("model calls = %d, want 1", got)
	}
	_ = st
	_ = ag
}

func TestHooks_ApprovalResumeDoesNotRefirePreToolUse(t *testing.T) {
	countFile := filepath.Join(t.TempDir(), "pre_tool_use.counts")
	mdl := &hooksModel{tooled: true, toolName: ReservedShellTool, toolArgs: `{"command":"rm -rf /"}`, final: "cleaned up"}
	hook := &domain.WorkspaceHook{
		HookBase: domain.HookBase{
			Name:        "shell-auditor",
			Event:       domain.HookEventPreToolUse,
			Matcher:     ReservedShellTool,
			HandlerType: domain.HookHandlerCommand,
			Config:      mustHookJSON(t, map[string]any{"command": "sh", "args": []string{"-c", "echo x >> " + countFile + "; exit 0"}}),
			TimeoutMS:   5000,
			OnFailure:   domain.HookFailureAllow,
			Enabled:     true,
		},
	}
	st, runner, _, _, req := setupHooksRunner(t, []string{ReservedShellTool}, mdl, hook)

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventApprovalRequired) {
		t.Fatalf("expected the approval interrupt, got %+v", events)
	}
	var approval *ApprovalPayload
	for i := range events {
		if events[i].Approval != nil {
			approval = events[i].Approval
		}
	}
	if approval == nil {
		t.Fatal("missing approval payload")
	}

	resumeStream, err := runner.Resume(context.Background(), req, approval, true)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	resumeEvents := collectStream(t, resumeStream)
	if !hasKind(resumeEvents, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed after approval, got %+v", resumeEvents)
	}

	// D4: pre_tool_use evaluated the call exactly once — the pre-approval
	// evaluation stands on the approved re-execution, with no second audit
	// row. The middleware caches the decision per CallID for the run.
	raw, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatalf("hook never executed: %v", err)
	}
	if lines := strings.Count(string(raw), "x"); lines != 1 {
		t.Fatalf("pre_tool_use fired %d times, want exactly 1 (no double evaluation across resume)", lines)
	}
	execs, err := st.Hooks().ListHookExecutions(context.Background(), req.WorkspaceID, nil, 10)
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	preRows := 0
	for _, e := range execs {
		if e.Event == domain.HookEventPreToolUse {
			preRows++
		}
	}
	if preRows != 1 {
		t.Fatalf("pre_tool_use audit rows = %d, want 1", preRows)
	}
}

func TestHooks_ObserverSlownessAndBreakageLeaveRunUntouched(t *testing.T) {
	mdl := &hooksModel{final: "done"}
	// A run_finished observer that oversleeps its budget and one that cannot
	// even start: both are detached (D5) — the run must neither wait for nor
	// fail on them.
	slow := &domain.WorkspaceHook{
		HookBase: domain.HookBase{
			Name:        "slow-notify",
			Event:       domain.HookEventRunFinished,
			Matcher:     "*",
			HandlerType: domain.HookHandlerCommand,
			Config:      mustHookJSON(t, map[string]any{"command": "sleep", "args": []string{"5"}}),
			TimeoutMS:   5000,
			OnFailure:   domain.HookFailureAllow,
			Enabled:     true,
		},
	}
	broken := &domain.WorkspaceHook{
		HookBase: domain.HookBase{
			Name:        "dead-notify",
			Event:       domain.HookEventRunStarted,
			Matcher:     "*",
			HandlerType: domain.HookHandlerCommand,
			Config:      mustHookJSON(t, map[string]any{"command": "/nonexistent/onclaw/no-such-binary"}),
			TimeoutMS:   5000,
			OnFailure:   domain.HookFailureAllow,
			Enabled:     true,
		},
	}
	st, runner, _, _, req := setupHooksRunner(t, nil, mdl, slow, broken)

	started := time.Now()
	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	elapsed := time.Since(started)

	if !hasKind(events, TranscriptEventTurnCompleted) || hasKind(events, TranscriptEventError) {
		t.Fatalf("run must complete normally despite broken observers, got %+v", events)
	}
	// Detached: the run never waited for the sleeping observer's budget.
	if elapsed >= 4*time.Second {
		t.Fatalf("run took %v — the slow observer blocked it", elapsed)
	}
	if got := mdl.callCount(t); got != 1 {
		t.Fatalf("model calls = %d, want 1", got)
	}

	// The broken run_started delivery is recorded as a failed execution and
	// never disturbs the run; the sleeping one eventually times out on its
	// own budget, also as a failure row (D5/D16).
	deadline := time.Now().Add(3 * time.Second)
	for {
		execs, err := st.Hooks().ListHookExecutions(context.Background(), req.WorkspaceID, nil, 10)
		if err != nil {
			t.Fatalf("list executions: %v", err)
		}
		found := false
		for _, e := range execs {
			if e.Event == domain.HookEventRunStarted && e.Decision == agenthooks.DecisionFailure {
				found = true
			}
		}
		if found {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected a failed run_started execution row, got %+v", execs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
