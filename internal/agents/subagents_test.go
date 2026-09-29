package agents

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/backgroundtask"
	backgroundlocal "github.com/cloudwego/eino/adk/backgroundtask/local"
	einofs "github.com/cloudwego/eino/adk/filesystem"
	fsmw "github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// invokableDummyTool is a real invokable registry tool for tests that drive
// turns through the ToolsNode (dummyTool in agent_test.go only carries Info).
type invokableDummyTool struct {
	name string
}

func (d *invokableDummyTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: d.name,
		Desc: "invokable dummy tool for testing",
	}, nil
}

func (d *invokableDummyTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "ok", nil
}

// agenticToolResultText walks a message's function_tool_result blocks and
// returns their text — where a tool result (like the canonical hook block JSON)
// reaches the model.
func agenticToolResultText(msg *schema.AgenticMessage) string {
	if msg == nil {
		return ""
	}
	var sb strings.Builder
	for _, block := range msg.ContentBlocks {
		if block == nil || block.FunctionToolResult == nil {
			continue
		}
		for _, c := range block.FunctionToolResult.Content {
			if c != nil && c.Type == schema.FunctionToolResultContentBlockTypeText && c.Text != nil {
				sb.WriteString(c.Text.Text)
			}
		}
	}
	return sb.String()
}

// memOutputStore is a minimal in-memory einofs.AppendOpener. Composition never
// touches it (Compose is pure) and these tests never launch real background
// work — the store exists only to satisfy the wired dependency.
type memOutputStore struct{}

func (memOutputStore) OpenAppend(_ context.Context, _ *einofs.OpenAppendRequest) (io.WriteCloser, error) {
	return nil, errors.New("memOutputStore: appends are not expected in these tests")
}

// newTestSubagentBackground builds a real background lane the way the per-run
// runner will (add-agent-subagents-background task 3.2): in-memory Manager,
// executor registry, and a Local Runner with the foreground timer disabled
// (design.md D11 policy pin) — allocation only, no I/O.
func newTestSubagentBackground(t *testing.T) *SubagentBackgroundConfig {
	t.Helper()
	ctx := context.Background()
	mgr, err := backgroundtask.New(ctx, nil)
	if err != nil {
		t.Fatalf("background task manager: %v", err)
	}
	registry := backgroundtask.NewExecutorRegistry()
	timerOff := 0
	runner, err := backgroundlocal.New(&backgroundlocal.Config{
		Manager:             mgr,
		Executors:           registry,
		ForegroundTimeoutMs: &timerOff,
	})
	if err != nil {
		t.Fatalf("background local runner: %v", err)
	}
	return &SubagentBackgroundConfig{Runner: runner, OutputStore: memOutputStore{}, OutputDir: ".tasks"}
}

// newDeclaredSubagent builds a minimal real subagent instance for Config.SubAgents.
func newDeclaredSubagent(t *testing.T, name, description string) adk.TypedAgent[*schema.AgenticMessage] {
	t.Helper()
	a, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](context.Background(), &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:        name,
		Description: description,
		Instruction: "declared subagent for tests",
		Model:       &dummyModel{},
	})
	if err != nil {
		t.Fatalf("declared subagent %q: %v", name, err)
	}
	return a
}

func TestCompose_SubagentValidation(t *testing.T) {
	ctx := context.Background()

	validConfig := func() *Config {
		return &Config{
			Name:             "delegating-agent",
			Description:      "agent with delegation",
			Instruction:      "You can delegate.",
			ChatModel:        &dummyModel{},
			SubagentsEnabled: true,
			Background:       newTestSubagentBackground(t),
		}
	}

	tests := []struct {
		name          string
		modify        func(*Config) *Config
		wantErrSubstr string
	}{
		{
			name: "enabled with suppression and zero declared subagents",
			modify: func(c *Config) *Config {
				c.WithoutGeneralSubAgent = true
				c.SubAgents = nil
				return c
			},
			wantErrSubstr: "general-purpose subagent or at least one declared subagent",
		},
		{
			name: "declared subagent with empty name",
			modify: func(c *Config) *Config {
				c.SubAgents = []adk.TypedAgent[*schema.AgenticMessage]{newDeclaredSubagent(t, "", "research delegate")}
				return c
			},
			wantErrSubstr: "name is required",
		},
		{
			name: "declared subagent with whitespace name",
			modify: func(c *Config) *Config {
				c.SubAgents = []adk.TypedAgent[*schema.AgenticMessage]{newDeclaredSubagent(t, "   \t\n", "research delegate")}
				return c
			},
			wantErrSubstr: "name is required",
		},
		{
			name: "declared subagent with empty description",
			modify: func(c *Config) *Config {
				c.SubAgents = []adk.TypedAgent[*schema.AgenticMessage]{newDeclaredSubagent(t, "researcher", "")}
				return c
			},
			wantErrSubstr: "description is required",
		},
		{
			name: "duplicate declared subagent names",
			modify: func(c *Config) *Config {
				c.SubAgents = []adk.TypedAgent[*schema.AgenticMessage]{
					newDeclaredSubagent(t, "researcher", "research delegate"),
					newDeclaredSubagent(t, "researcher", "second researcher"),
				}
				return c
			},
			wantErrSubstr: "duplicate subagent name",
		},
		{
			name: "nil declared subagent",
			modify: func(c *Config) *Config {
				c.SubAgents = []adk.TypedAgent[*schema.AgenticMessage]{nil}
				return c
			},
			wantErrSubstr: "is nil",
		},
		{
			name: "background lane missing runner",
			modify: func(c *Config) *Config {
				b := newTestSubagentBackground(t)
				b.Runner = nil
				c.Background = b
				return c
			},
			wantErrSubstr: "requires runner, output store, and output dir",
		},
		{
			name: "background lane missing output store",
			modify: func(c *Config) *Config {
				b := newTestSubagentBackground(t)
				b.OutputStore = nil
				c.Background = b
				return c
			},
			wantErrSubstr: "requires runner, output store, and output dir",
		},
		{
			name: "background lane missing output dir",
			modify: func(c *Config) *Config {
				b := newTestSubagentBackground(t)
				b.OutputDir = ""
				c.Background = b
				return c
			},
			wantErrSubstr: "requires runner, output store, and output dir",
		},
		{
			name: "filesystem background without shell",
			modify: func(c *Config) *Config {
				c.Filesystem = &FilesystemConfig{AgentDir: t.TempDir(), Background: &fsmw.BackgroundConfig{}}
				return c
			},
			wantErrSubstr: "shell capability",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.modify(validConfig())
			agent, err := Compose(ctx, cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErrSubstr)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantErrSubstr)) {
				t.Fatalf("error %q does not contain expected substring %q", err.Error(), tt.wantErrSubstr)
			}
			if agent != nil {
				t.Fatalf("expected nil agent on validation failure, got %v", agent)
			}
		})
	}

	t.Run("capability off is a no-op despite subagent fields", func(t *testing.T) {
		// Zero-value semantics (task 2.1): with the enable signal unset the
		// runner never wires the capability, so every subagent field is
		// ignored and composition is byte-identical to the baseline.
		cfg := &Config{
			Name:                   "plain-agent",
			Description:            "no delegation",
			Instruction:            "You cannot delegate.",
			ChatModel:              &dummyModel{},
			SubAgents:              []adk.TypedAgent[*schema.AgenticMessage]{newDeclaredSubagent(t, "researcher", "research delegate")},
			WithoutGeneralSubAgent: true,
		}
		handlers, err := buildMiddlewares(ctx, cfg)
		if err != nil {
			t.Fatalf("buildMiddlewares failed: %v", err)
		}
		if len(handlers) != 3 {
			t.Fatalf("expected baseline 3 handlers (patchtoolcalls, attachments, tool-error-result), got %d", len(handlers))
		}
		if _, err := Compose(ctx, cfg); err != nil {
			t.Fatalf("Compose failed: %v", err)
		}
	})
}

// TestCompose_SubagentsCompositionStaysPure pins Compose's purity with the
// capability fully wired (task 2.4): constructing the delegation tool, the
// general-purpose clone, and the background lane is allocation-only — the
// watched agent dir must not gain a single entry (no .tasks dir, no transcript,
// no output file).
func TestCompose_SubagentsCompositionStaysPure(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	cfg := &Config{
		Name:             "pure-delegator",
		Description:      "purity guard",
		Instruction:      "You can delegate.",
		ChatModel:        &dummyModel{},
		Filesystem:       &FilesystemConfig{AgentDir: dir},
		SubagentsEnabled: true,
		SubAgents:        []adk.TypedAgent[*schema.AgenticMessage]{newDeclaredSubagent(t, "researcher", "research delegate")},
		Background:       newTestSubagentBackground(t),
	}

	if _, err := Compose(ctx, cfg); err != nil {
		t.Fatalf("Compose failed: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read watched dir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("composition wrote to the agent dir: %v", names)
	}
}

// toolCapturingModel records the tool surface the model sees on every call and
// answers with plain text, so one turn is enough to observe a composed agent's
// effective tool list.
type toolCapturingModel struct {
	mu    sync.Mutex
	calls int
	names [][]string
}

func (m *toolCapturingModel) Generate(_ context.Context, _ []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	o := model.GetCommonOptions(&model.Options{}, opts...)
	names := make([]string, 0, len(o.Tools))
	for _, ti := range o.Tools {
		if ti != nil {
			names = append(names, ti.Name)
		}
	}
	m.mu.Lock()
	m.calls++
	m.names = append(m.names, names)
	m.mu.Unlock()
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: "ok"}},
		},
	}, nil
}

func (m *toolCapturingModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

// lastCallTools returns the tool names the model saw on its most recent call.
func (m *toolCapturingModel) lastCallTools(t *testing.T) []string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls == 0 {
		t.Fatal("model was never called")
	}
	return m.names[m.calls-1]
}

func hasTool(t *testing.T, names []string, want string) bool {
	t.Helper()
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func runOneTurn(t *testing.T, a adk.TypedAgent[*schema.AgenticMessage], input string) {
	t.Helper()
	iter := a.Run(context.Background(), &adk.TypedAgentInput[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{schema.UserAgenticMessage(input)},
	})
	for {
		event, ok := iter.Next()
		if !ok {
			return
		}
		if event != nil && event.Err != nil {
			t.Fatalf("turn failed: %v", event.Err)
		}
	}
}

// TestCompose_SubagentToolSurface pins the delegation tool surface (tasks 2.2
// and 2.3): the parent gains `agent` (and the control tools only with the
// background lane), the clone mirrors the business tools without any of them.
func TestCompose_SubagentToolSurface(t *testing.T) {
	ctx := context.Background()

	baseConfig := func(m model.BaseModel[*schema.AgenticMessage]) *Config {
		return &Config{
			Name:             "surface-agent",
			Description:      "tool surface",
			Instruction:      "You can delegate.",
			ChatModel:        m,
			Tools:            []tool.BaseTool{&invokableDummyTool{name: "custom.action"}},
			Filesystem:       &FilesystemConfig{AgentDir: t.TempDir()},
			SubagentsEnabled: true,
		}
	}

	t.Run("parent gains agent plus control tools with the background lane", func(t *testing.T) {
		m := &toolCapturingModel{}
		cfg := baseConfig(m)
		cfg.Background = newTestSubagentBackground(t)
		agent, err := Compose(ctx, cfg)
		if err != nil {
			t.Fatalf("Compose failed: %v", err)
		}
		runOneTurn(t, agent, "hello")

		names := m.lastCallTools(t)
		for _, want := range []string{"agent", "task_output", "task_stop", "custom.action", "read_file"} {
			if !hasTool(t, names, want) {
				t.Fatalf("expected %q in the parent tool surface, got %v", want, names)
			}
		}
	})

	t.Run("foreground-only delegation exposes no control tools", func(t *testing.T) {
		m := &toolCapturingModel{}
		cfg := baseConfig(m)
		agent, err := Compose(ctx, cfg)
		if err != nil {
			t.Fatalf("Compose failed: %v", err)
		}
		runOneTurn(t, agent, "hello")

		names := m.lastCallTools(t)
		if !hasTool(t, names, "agent") {
			t.Fatalf("expected %q in the parent tool surface, got %v", "agent", names)
		}
		for _, banned := range []string{"task_output", "task_stop"} {
			if hasTool(t, names, banned) {
				t.Fatalf("foreground-only delegation must not expose %q, got %v", banned, names)
			}
		}
	})

	t.Run("capability off exposes no agent tool", func(t *testing.T) {
		m := &toolCapturingModel{}
		cfg := baseConfig(m)
		cfg.SubagentsEnabled = false
		agent, err := Compose(ctx, cfg)
		if err != nil {
			t.Fatalf("Compose failed: %v", err)
		}
		runOneTurn(t, agent, "hello")

		names := m.lastCallTools(t)
		if hasTool(t, names, "agent") || hasTool(t, names, "task_output") || hasTool(t, names, "task_stop") {
			t.Fatalf("capability off must expose no delegation tools, got %v", names)
		}
	})

	t.Run("clone mirrors business tools without delegation or control tools", func(t *testing.T) {
		m := &toolCapturingModel{}
		cfg := baseConfig(m)
		cfg.Background = newTestSubagentBackground(t)

		cloneHandlers, err := buildHandlers(ctx, cfg, middlewareScope{})
		if err != nil {
			t.Fatalf("clone handler pass failed: %v", err)
		}
		clone, err := buildGeneralPurposeSubagent(ctx, cfg, cloneHandlers)
		if err != nil {
			t.Fatalf("build clone failed: %v", err)
		}
		if clone.Name(ctx) != GeneralPurposeSubagentName {
			t.Fatalf("clone name = %q, want %q", clone.Name(ctx), GeneralPurposeSubagentName)
		}
		if clone.Description(ctx) != generalPurposeSubagentDescription {
			t.Fatalf("clone description = %q, want the research-delegate description", clone.Description(ctx))
		}
		runOneTurn(t, clone, "research this")

		names := m.lastCallTools(t)
		for _, want := range []string{"custom.action", "read_file"} {
			if !hasTool(t, names, want) {
				t.Fatalf("expected %q in the clone tool surface, got %v", want, names)
			}
		}
		for _, banned := range []string{"agent", "task_output", "task_stop"} {
			if hasTool(t, names, banned) {
				t.Fatalf("clone must not expose %q (delegation cannot recurse), got %v", banned, names)
			}
		}
	})
}

// hookBlockModel plays the delegation attempt (task 5.5): turn 1 calls `agent`,
// turn 2 reads the tool result and finishes the turn. It records every model
// input so the test can assert the canonical block JSON reached the model and
// the child agent never ran.
type hookBlockModel struct {
	mu     sync.Mutex
	calls  int
	inputs [][]*schema.AgenticMessage
}

func (m *hookBlockModel) Generate(_ context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	m.mu.Lock()
	m.calls++
	m.inputs = append(m.inputs, append([]*schema.AgenticMessage(nil), input...))
	calls := m.calls
	m.mu.Unlock()
	if calls == 1 {
		return &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				{Type: schema.ContentBlockTypeFunctionToolCall, FunctionToolCall: &schema.FunctionToolCall{
					CallID:    "call-1",
					Name:      "agent",
					Arguments: `{"subagent_type":"general-purpose","prompt":"research the flaky test","description":"research task"}`,
				}},
			},
		}, nil
	}
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: "delegation blocked; standing by"}},
		},
	}, nil
}

func (m *hookBlockModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

func (m *hookBlockModel) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// finalInput returns the turn-2 model input: the transcript the block result
// landed in.
func (m *hookBlockModel) finalInput() []*schema.AgenticMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.inputs) == 0 {
		return nil
	}
	return m.inputs[len(m.inputs)-1]
}

// TestSubagents_HookBlockStopsDelegationRunContinues pins the hooks interplay
// (task 5.5, design.md D3): the parent's pre_tool_use gate wraps the `agent`
// tool call, so a matching hook stops the delegation before it starts, the
// canonical block JSON returns as a successful tool result the model reads,
// and the run continues to a normal finish — never a run failure.
func TestSubagents_HookBlockStopsDelegationRunContinues(t *testing.T) {
	ctx := context.Background()
	mdl := &hookBlockModel{}

	cfg := &Config{
		Name:             "hooked-delegator",
		Description:      "hook gate on delegation",
		Instruction:      "You can delegate.",
		ChatModel:        mdl,
		SubagentsEnabled: true,
		Background:       newTestSubagentBackground(t),
	}
	_, _, chain := resolveHookChain(t, blockedToolHook(t, "policy-guard", []string{"agent"}))
	cfg.Hooks = chain
	cfg.HooksBase = &agenthooks.Event{}

	agent, err := Compose(ctx, cfg)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	// Production wiring: the ADK session adapter backs both checkpoint and
	// session stores, and the managed agent tool resolves the runner session
	// id from the same runner.
	fakeStore := fake.New()
	sessionAdapter := NewADKSessionAdapter(
		fakeStore.SessionEvents(), fakeStore.SessionCheckpoints(), "ws-hooks")
	runner := adk.NewTypedRunner[*schema.AgenticMessage](adk.TypedRunnerConfig[*schema.AgenticMessage]{
		Agent:           agent,
		CheckPointStore: sessionAdapter,
		SessionID:       "sess-hook-block",
		SessionStore:    sessionAdapter,
	})

	events := drainEvents(t, runner.Query(ctx, "delegate the research"))

	for _, e := range events {
		if e != nil && e.Err != nil {
			t.Fatalf("a hook block must never fail the run, got %v", e.Err)
		}
	}

	// The delegation never started: exactly the parent's two model calls —
	// turn 1 (the `agent` tool call) and turn 2 (reading the block result) —
	// with no child-run calls in between.
	if got := mdl.callCount(); got != 2 {
		t.Fatalf("expected exactly two parent model calls (no child run), got %d", got)
	}

	// The canonical block JSON returned as a successful tool result.
	found := false
	for _, msg := range mdl.finalInput() {
		if strings.Contains(agenticToolResultText(msg), `"blocked_by_hook":true`) {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the canonical block JSON as the call-1 tool result in the turn-2 model input")
	}

	// The run continued to a normal finish with the turn-2 answer.
	last := ""
	for _, e := range events {
		if e != nil && e.Output != nil && e.Output.MessageOutput != nil && e.Output.MessageOutput.Message != nil {
			if txt := extractAgenticText(e.Output.MessageOutput.Message); txt != "" {
				last = txt
			}
		}
	}
	if last != "delegation blocked; standing by" {
		t.Fatalf("run did not finish with the turn-2 answer, got %q", last)
	}
}

// inputTexts concatenates every message's text in a model input — the
// full-message view the reminder assertions need (agenticToolResultText only
// sees tool-result blocks).
func inputTexts(input []*schema.AgenticMessage) string {
	var sb strings.Builder
	for _, msg := range input {
		sb.WriteString(extractAgenticText(msg))
		sb.WriteString("\n")
	}
	return sb.String()
}

// TestCompose_SubagentSuppressionOffersDeclaredOnly pins the suppression
// contract (spec agent-subagents "Suppressed by configuration"): with the
// capability wired, WithoutGeneralSubAgent set, and one declared subagent,
// the composed surface exposes the `agent` tool and advertises ONLY the
// declared type — the general-purpose clone is neither built nor offered —
// while delegation to the declared type still runs end to end.
func TestCompose_SubagentSuppressionOffersDeclaredOnly(t *testing.T) {
	ctx := context.Background()
	mdl := &subagentsRunnerModel{
		parentScript: func(ctx context.Context, call int, input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
			if call == 1 {
				return scriptedToolCall("p-call-1", "agent",
					`{"subagent_type":"researcher","prompt":"`+delegationMarker+` research the flaky test","description":"research task"}`), nil
			}
			return textMessage("delegation done"), nil
		},
		childScript: func(ctx context.Context, call int, input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
			return textMessage("declared child report"), nil
		},
	}

	// The declared subagent shares the scripted model so the child lane's
	// answer is observable (parent and child route by the delegation marker).
	declared, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:        "researcher",
		Description: "research delegate",
		Instruction: "declared subagent for tests",
		Model:       mdl,
	})
	if err != nil {
		t.Fatalf("declared subagent: %v", err)
	}

	cfg := &Config{
		Name:                   "suppressed-delegator",
		Description:            "suppression tool surface",
		Instruction:            "You can delegate.",
		ChatModel:              mdl,
		SubagentsEnabled:       true,
		WithoutGeneralSubAgent: true,
		SubAgents:              []adk.TypedAgent[*schema.AgenticMessage]{declared},
	}
	agent, err := Compose(ctx, cfg)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	// Production wiring (mirrors TestSubagents_HookBlockStopsDelegationRunContinues):
	// the ADK session adapter backs both checkpoint and session stores.
	fakeStore := fake.New()
	sessionAdapter := NewADKSessionAdapter(
		fakeStore.SessionEvents(), fakeStore.SessionCheckpoints(), "ws-suppression")
	runner := adk.NewTypedRunner[*schema.AgenticMessage](adk.TypedRunnerConfig[*schema.AgenticMessage]{
		Agent:           agent,
		CheckPointStore: sessionAdapter,
		SessionID:       "sess-suppression",
		SessionStore:    sessionAdapter,
	})

	events := drainEvents(t, runner.Query(ctx, "delegate the research"))
	for _, e := range events {
		if e != nil && e.Err != nil {
			t.Fatalf("run failed: %v", e.Err)
		}
	}

	// The composed surface exposes the `agent` tool.
	surface := mdl.callNames(t, false, 1)
	if !hasTool(t, surface, "agent") {
		t.Fatalf("expected the agent tool in the suppressed surface, got %v", surface)
	}

	// The advertised types are the declared one ONLY: the mid-conversation
	// reminder lists the researcher and no general-purpose clone appears
	// anywhere in the model input.
	turn1 := inputTexts(mdl.callInput(t, false, 1))
	if !strings.Contains(turn1, "Available agent types for the Agent tool:") {
		t.Fatalf("turn-1 input carries no agent-types reminder: %q", turn1)
	}
	if !strings.Contains(turn1, "- researcher: research delegate") {
		t.Fatalf("turn-1 reminder does not list the declared type: %q", turn1)
	}
	if strings.Contains(turn1, "general-purpose") {
		t.Fatalf("suppressed run must not advertise general-purpose, got %q", turn1)
	}

	// Delegation to the declared type still runs: the child's final report
	// returned as the tool result the parent read on turn 2.
	turn2 := inputToolResultTexts(mdl.callInput(t, false, 2))
	if !strings.Contains(turn2, "declared child report") {
		t.Fatalf("delegation to the declared type did not run the child, got %q", turn2)
	}
	if gotParent, gotChild := mdl.laneCallCount(false), mdl.laneCallCount(true); gotParent != 2 || gotChild != 1 {
		t.Fatalf("lane calls parent=%d child=%d, want parent 2 and child 1", gotParent, gotChild)
	}
}
