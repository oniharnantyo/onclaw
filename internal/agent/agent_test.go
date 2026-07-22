package agent_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agent"
	"github.com/oniharnantyo/onclaw/internal/render"
	"github.com/oniharnantyo/onclaw/internal/store"
)

type fakeChatModel struct {
	generateFunc func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error)
	streamFunc   func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error)
}

func (f *fakeChatModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	if f.generateFunc != nil {
		return f.generateFunc(ctx, input, opts...)
	}
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "Default fake response"}),
		},
	}, nil
}

func (f *fakeChatModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	if f.streamFunc != nil {
		return f.streamFunc(ctx, input, opts...)
	}
	msg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "Default fake streaming response"}),
		},
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

func TestAssembleAndRunAgent_ReActLoop(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-agent-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workspace := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	userConfigDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(userConfigDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// Create a dummy file to read in workspace
	testFile := filepath.Join(workspace, "README.md")
	if err := os.WriteFile(testFile, []byte("Hello onclaw!"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Setup fake ChatModel to simulate a tool-calling loop
	modelCalls := 0
	respondMock := func(input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
		modelCalls++
		if modelCalls == 1 {
			// First call: trigger read_file tool call
			return &schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					{
						Type: schema.ContentBlockTypeFunctionToolCall,
						FunctionToolCall: &schema.FunctionToolCall{
							CallID:    "call_1",
							Name:      "read_file",
							Arguments: `{"file_path":"README.md"}`,
						},
					},
				},
			}, nil
		}
		// Second call: return final text answer
		return &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{Text: "Successfully read README.md. Content: Hello onclaw!"}),
			},
		}, nil
	}

	fm := &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
			return respondMock(input)
		},
		streamFunc: func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
			msg, err := respondMock(input)
			if err != nil {
				return nil, err
			}
			sr, sw := schema.Pipe[*schema.AgenticMessage](1)
			sw.Send(msg, nil)
			sw.Close()
			return sr, nil
		},
	}

	agentConf := &store.Agent{
		Name:          "test-react-agent",
		Provider:      "fake-prov",
		Tools:         "read_file,write_file", // test subset filtering
		MaxIterations: 5,
	}

	ctx := context.Background()
	opts := agent.NewTestAssembleOpts(t, func(o *agent.AssembleAgentOpts) {
		o.AgentConf = agentConf
		o.ChatModel = fm
		o.ReviewModel = fm
		o.Workspace = workspace
		o.UserConfigDir = userConfigDir
	})
	agentVal, err := agent.AssembleAgent(ctx, opts)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}

	var stdout bytes.Buffer
	it := agentVal.Run(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage("Read the README.md file please.")})
	tr := render.Text(&stdout)
	for {
		ev, ok := it.Next()
		if !ok {
			break
		}
		if ev.Message == nil {
			continue
		}
		if err := tr.Render(ev.Message); err != nil {
			t.Fatalf("failed to render: %v", err)
		}
	}
	if err := tr.Flush(); err != nil {
		t.Fatalf("failed to flush: %v", err)
	}
	if err := it.Err(); err != nil {
		t.Fatalf("failed to run agent: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Calling tool \"read_file\"") {
		t.Errorf("stdout does not contain tool call info, got: %q", output)
	}
	if !strings.Contains(output, "Successfully read README.md. Content: Hello onclaw!") {
		t.Errorf("stdout does not contain final response, got: %q", output)
	}
}

func TestAssembleAndRunAgent_Cancellation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-cancel-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workspace := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	userConfigDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(userConfigDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	fm := &fakeChatModel{
		streamFunc: func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
			// Simulate long-running inference that respects cancellation
			sr, sw := schema.Pipe[*schema.AgenticMessage](1)
			go func() {
				select {
				case <-ctx.Done():
					sw.Send(nil, ctx.Err())
				case <-time.After(1 * time.Second):
					sw.Send(&schema.AgenticMessage{
						Role: schema.AgenticRoleTypeAssistant,
						ContentBlocks: []*schema.ContentBlock{
							schema.NewContentBlock(&schema.AssistantGenText{Text: "Finished"}),
						},
					}, nil)
				}
				sw.Close()
			}()
			return sr, nil
		},
	}

	agentConf := &store.Agent{
		Name: "test-cancel-agent",
	}

	ctx, cancel := context.WithCancel(context.Background())
	opts := agent.NewTestAssembleOpts(t, func(o *agent.AssembleAgentOpts) {
		o.AgentConf = agentConf
		o.ChatModel = fm
		o.ReviewModel = fm
		o.Workspace = workspace
		o.UserConfigDir = userConfigDir
	})
	agentVal, err := agent.AssembleAgent(ctx, opts)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}
	// Cancel context immediately
	cancel()

	it := agentVal.Run(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage("Hello")})
	for {
		_, ok := it.Next()
		if !ok {
			break
		}
	}
	err = it.Err()
	if err == nil {
		t.Error("expected run to fail with cancellation error, got nil")
	}
}

func TestAssembleAgent_ContextWindowTrigger(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(workspace, 0755)
	userConfigDir := filepath.Join(tmpDir, "config")
	_ = os.MkdirAll(userConfigDir, 0755)

	agentConf := &store.Agent{
		Name: "test-trigger-agent",
	}

	fm := &fakeChatModel{}
	ctx := context.Background()

	// 1. Compile and resolve with 128000 context window (verifies 80% logic runs)
	opts1 := agent.NewTestAssembleOpts(t, func(o *agent.AssembleAgentOpts) {
		o.AgentConf = agentConf
		o.ChatModel = fm
		o.ReviewModel = fm
		o.Workspace = workspace
		o.UserConfigDir = userConfigDir
		o.ContextWindow = 128000
	})
	ag, err := agent.AssembleAgent(ctx, opts1)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}
	if ag == nil {
		t.Fatal("expected non-nil agent")
	}

	// 2. Re-assemble with 64000 context window
	opts2 := agent.NewTestAssembleOpts(t, func(o *agent.AssembleAgentOpts) {
		o.AgentConf = agentConf
		o.ChatModel = fm
		o.ReviewModel = fm
		o.Workspace = workspace
		o.UserConfigDir = userConfigDir
	})
	ag2, err := agent.AssembleAgent(ctx, opts2)
	if err != nil {
		t.Fatalf("failed to assemble agent second time: %v", err)
	}
	if ag2 == nil {
		t.Fatal("expected non-nil agent on second assembly")
	}
}


type mockToolRegistryStore struct {
	list []*store.ToolRegistry
}

func (m *mockToolRegistryStore) ListTools(ctx context.Context) ([]*store.ToolRegistry, error) {
	return m.list, nil
}
func (m *mockToolRegistryStore) GetTool(ctx context.Context, name string) (*store.ToolRegistry, error) {
	return nil, nil
}
func (m *mockToolRegistryStore) UpsertTool(ctx context.Context, t *store.ToolRegistry) error {
	return nil
}
func (m *mockToolRegistryStore) ToggleTool(ctx context.Context, name string, enabled bool) error {
	return nil
}

func TestAssembleAgent_GlobalToolEnable(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(workspace, 0755)
	userConfigDir := filepath.Join(tmpDir, "config")
	_ = os.MkdirAll(userConfigDir, 0755)

	fm := &fakeChatModel{}
	ctx := context.Background()

	// 0. Explicit: an empty allowlist must offer every globally-enabled tool (empty = all).
	// Use tools that are always registered regardless of memory-store wiring in tests
	// (web_search/web_fetch), since the memory_* tools are filtered out when no
	// memory stores are provided.
	mockAll := &mockToolRegistryStore{
		list: []*store.ToolRegistry{
			{Name: "web_search", Enabled: 1},
			{Name: "web_fetch", Enabled: 1},
		},
	}
	agentConfEmpty := &store.Agent{Name: "test-empty-allowlist"}
	optsEmpty := agent.NewTestAssembleOpts(t, func(o *agent.AssembleAgentOpts) {
		o.AgentConf = agentConfEmpty
		o.ChatModel = fm
		o.ReviewModel = fm
		o.Workspace = workspace
		o.UserConfigDir = userConfigDir
		o.ToolRegistryStore = mockAll
	})
	agEmpty, err := agent.AssembleAgent(ctx, optsEmpty)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}
	gotAll := make(map[string]bool)
	for _, tl := range agEmpty.Tools {
		info, _ := tl.Info(ctx)
		gotAll[info.Name] = true
	}
	// With no per-agent allowlist, every globally-enabled registry tool must be offered.
	for _, name := range []string{"web_search", "web_fetch"} {
		if !gotAll[name] {
			t.Errorf("empty allowlist should offer globally-enabled tool %q, but it was absent", name)
		}
	}

	// 1. With all tools enabled
	agentConf := &store.Agent{
		Name: "test-global-enable",
	}
	opts := agent.NewTestAssembleOpts(t, func(o *agent.AssembleAgentOpts) {
		o.AgentConf = agentConf
		o.ChatModel = fm
		o.ReviewModel = fm
		o.Workspace = workspace
		o.UserConfigDir = userConfigDir
	})
	ag, err := agent.AssembleAgent(ctx, opts)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}
	if len(ag.Tools) == 0 {
		t.Error("expected tools, got 0")
	}

	// 2. With memory_search disabled globally. Factory tools are filtered by the
	// registry enable flag inside tools.Builtin; the filesystem tools are injected
	// separately by the Eino middleware and so are never part of ag.Tools.
	mockStore := &mockToolRegistryStore{
		list: []*store.ToolRegistry{
			{Name: "web_search", Enabled: 0},
			{Name: "web_fetch", Enabled: 1},
		},
	}
	optsMock := agent.NewTestAssembleOpts(t, func(o *agent.AssembleAgentOpts) {
		o.AgentConf = agentConf
		o.ChatModel = fm
		o.ReviewModel = fm
		o.Workspace = workspace
		o.UserConfigDir = userConfigDir
		o.ToolRegistryStore = mockStore
	})
	ag, err = agent.AssembleAgent(ctx, optsMock)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}

	hasWebSearch := false
	hasWebFetch := false
	for _, tl := range ag.Tools {
		info, _ := tl.Info(ctx)
		if info.Name == "web_search" {
			hasWebSearch = true
		}
		if info.Name == "web_fetch" {
			hasWebFetch = true
		}
	}
	if hasWebSearch {
		t.Error("expected web_search to be globally excluded, but it was present")
	}
	if !hasWebFetch {
		t.Error("expected web_fetch to be present")
	}

	// 3. Intersection with per-agent allowlist
	agentConf.Tools = "web_search,web_fetch" // agent only allows these two factory tools
	optsMockAllow := agent.NewTestAssembleOpts(t, func(o *agent.AssembleAgentOpts) {
		o.AgentConf = agentConf
		o.ChatModel = fm
		o.ReviewModel = fm
		o.Workspace = workspace
		o.UserConfigDir = userConfigDir
		o.ToolRegistryStore = mockStore
	})
	ag, err = agent.AssembleAgent(ctx, optsMockAllow)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}
	var activeTools []string
	for _, tl := range ag.Tools {
		info, _ := tl.Info(ctx)
		activeTools = append(activeTools, info.Name)
	}
	// web_search is disabled globally, so the allowlist resolves to [web_fetch].
	if len(activeTools) != 1 || activeTools[0] != "web_fetch" {
		t.Errorf("expected effective tools to be exactly [web_fetch], got %v", activeTools)
	}

}

type mockToolGroupConfigStore struct {
	config *store.ToolGroupConfig
	err    error
}

func (m *mockToolGroupConfigStore) GetConfig(ctx context.Context, category string) (*store.ToolGroupConfig, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.config, nil
}

func (m *mockToolGroupConfigStore) PutConfig(ctx context.Context, category string, config string) error {
	return nil
}

func (m *mockToolGroupConfigStore) UpsertConfig(ctx context.Context, cfg *store.ToolGroupConfig) error {
	return nil
}

func TestToolGroupCfgWrapper(t *testing.T) {
	ctx := context.Background()

	// Case 1: Store is nil
	w1 := &agent.ToolGroupCfgWrapper{Store: nil}
	cfg1, err := w1.GetConfig(ctx, "Browser")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg1 != "{}" {
		t.Errorf("expected '{}', got %q", cfg1)
	}

	// Case 2: Store returns sql.ErrNoRows
	mockStore2 := &mockToolGroupConfigStore{err: sql.ErrNoRows}
	w2 := &agent.ToolGroupCfgWrapper{Store: mockStore2}
	cfg2, err := w2.GetConfig(ctx, "Browser")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg2 != "{}" {
		t.Errorf("expected '{}', got %q", cfg2)
	}

	// Case 3: Store returns other error
	mockStore3 := &mockToolGroupConfigStore{err: errors.New("db error")}
	w3 := &agent.ToolGroupCfgWrapper{Store: mockStore3}
	_, err = w3.GetConfig(ctx, "Browser")
	if err == nil || !strings.Contains(err.Error(), "db error") {
		t.Errorf("expected db error, got %v", err)
	}

	// Case 4: Store success
	mockStore4 := &mockToolGroupConfigStore{
		config: &store.ToolGroupConfig{Config: `{"key":"val"}`},
	}
	w4 := &agent.ToolGroupCfgWrapper{Store: mockStore4}
	cfg4, err := w4.GetConfig(ctx, "Browser")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg4 != `{"key":"val"}` {
		t.Errorf("expected config JSON, got %q", cfg4)
	}
}

func TestAssembleAgent_ErrorPaths(t *testing.T) {
	ctx := context.Background()
	fm := &fakeChatModel{}

	tmpDir := t.TempDir()
	workspace := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(workspace, 0755)

	// 1. LoadPersonaContext fails (USER.md is a directory, leading to EISDIR read error)
	userConfigDir := filepath.Join(tmpDir, "config")
	_ = os.MkdirAll(userConfigDir, 0755)
	badUserFile := filepath.Join(userConfigDir, "USER.md")
	_ = os.MkdirAll(badUserFile, 0755) // Create directory instead of file

	agentConf := &store.Agent{Name: "test-err-agent"}
	optsErr := agent.NewTestAssembleOpts(t, func(o *agent.AssembleAgentOpts) {
		o.AgentConf = agentConf
		o.ChatModel = fm
		o.ReviewModel = fm
		o.Workspace = workspace
		o.UserConfigDir = userConfigDir
	})
	_, err := agent.AssembleAgent(ctx, optsErr)
	if err == nil || !strings.Contains(err.Error(), "load persona context") {
		t.Errorf("expected load persona context error, got %v", err)
	}

	// Clean up for next test
	_ = os.RemoveAll(badUserFile)
}

type mockFailedToolRegistryStore struct {
	store.ToolRegistryStore
}

func (m *mockFailedToolRegistryStore) ListTools(ctx context.Context) ([]*store.ToolRegistry, error) {
	return nil, errors.New("list tools error")
}

func TestAssembleAgent_ListToolsError(t *testing.T) {
	ctx := context.Background()
	fm := &fakeChatModel{}
	tmpDir := t.TempDir()
	workspace := filepath.Join(tmpDir, "workspace")
	_ = os.MkdirAll(workspace, 0755)
	userConfigDir := filepath.Join(tmpDir, "config")
	_ = os.MkdirAll(userConfigDir, 0755)

	agentConf := &store.Agent{Name: "test-err-agent"}
	mockStore := &mockFailedToolRegistryStore{}
	optsErrList := agent.NewTestAssembleOpts(t, func(o *agent.AssembleAgentOpts) {
		o.AgentConf = agentConf
		o.ChatModel = fm
		o.ReviewModel = fm
		o.Workspace = workspace
		o.UserConfigDir = userConfigDir
		o.ToolRegistryStore = mockStore
	})
	_, err := agent.AssembleAgent(ctx, optsErrList)
	if err == nil || !strings.Contains(err.Error(), "list tools for enabled checker") {
		t.Errorf("expected list tools error, got %v", err)
	}
}
func TestEventIterator_EdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("prior error", func(t *testing.T) {
		err := errors.New("prior error")
		it := agent.NewEventIterator(ctx, nil, nil, err, nil)
		ev, ok := it.Next()
		if ok || ev.Message != nil {
			t.Errorf("expected false, nil, got %v, %+v", ok, ev)
		}
		if it.Err() != err {
			t.Errorf("expected err %v, got %v", err, it.Err())
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		it := agent.NewEventIterator(cctx, nil, nil, nil, nil)
		ev, ok := it.Next()
		if ok || ev.Message != nil {
			t.Errorf("expected false, nil, got %v, %+v", ok, ev)
		}
		if it.Err() != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", it.Err())
		}
	})

	t.Run("currentStream read error", func(t *testing.T) {
		sr, sw := schema.Pipe[*schema.AgenticMessage](1)
		streamErr := errors.New("stream error")
		sw.Send(nil, streamErr)
		sw.Close()

		it := agent.NewEventIterator(ctx, nil, sr, nil, nil)
		ev, ok := it.Next()
		if ok || ev.Message != nil {
			t.Errorf("expected false, nil, got %v, %+v", ok, ev)
		}
		if it.Err() != streamErr {
			t.Errorf("expected %v, got %v", streamErr, it.Err())
		}
	})

	t.Run("event error and onTurnError callback", func(t *testing.T) {
		iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
		eventErr := errors.New("event error")

		var turnErr error
		onTurnError := func(err error) {
			turnErr = err
		}

		it := agent.NewEventIterator(ctx, iter, nil, nil, onTurnError)

		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
			Err: eventErr,
		})
		gen.Close()

		ev, ok := it.Next()
		if ok || ev.Message != nil {
			t.Errorf("expected false, nil, got %v, %+v", ok, ev)
		}
		if turnErr != eventErr {
			t.Errorf("expected turnErr %v, got %v", eventErr, turnErr)
		}
		if it.Err() != eventErr {
			t.Errorf("expected Err() %v, got %v", eventErr, it.Err())
		}
	})

	t.Run("event interrupted", func(t *testing.T) {
		iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()

		it := agent.NewEventIterator(ctx, iter, nil, nil, nil)

		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
			Action: &adk.AgentAction{
				Interrupted: &adk.InterruptInfo{},
			},
		})
		gen.Close()

		ev, ok := it.Next()
		if ok || ev.Message != nil {
			t.Errorf("expected false, nil, got %v, %+v", ok, ev)
		}
	})

	t.Run("event message output success", func(t *testing.T) {
		iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()

		it := agent.NewEventIterator(ctx, iter, nil, nil, nil)

		expectedMsg := &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
		}

		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
			Output: &adk.TypedAgentOutput[*schema.AgenticMessage]{
				MessageOutput: &adk.TypedMessageVariant[*schema.AgenticMessage]{
					Message: expectedMsg,
				},
			},
		})
		gen.Close()

		ev, ok := it.Next()
		if !ok || ev.Message != expectedMsg {
			t.Errorf("expected true, %v, got %v, %+v", expectedMsg, ok, ev)
		}
	})

	t.Run("event message output streaming success", func(t *testing.T) {
		iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()

		it := agent.NewEventIterator(ctx, iter, nil, nil, nil)

		expectedMsg := &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
		}

		sr, sw := schema.Pipe[*schema.AgenticMessage](1)
		sw.Send(expectedMsg, nil)
		sw.Close()

		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
			Output: &adk.TypedAgentOutput[*schema.AgenticMessage]{
				MessageOutput: &adk.TypedMessageVariant[*schema.AgenticMessage]{
					IsStreaming:   true,
					MessageStream: sr,
				},
			},
		})
		gen.Close()

		// Read the streamed message
		ev, ok := it.Next()
		if !ok || ev.Message != expectedMsg {
			t.Errorf("expected true, %v, got %v, %+v", expectedMsg, ok, ev)
		}

		// Next call should drain/finish since stream is EOF and gen is closed
		ev2, ok2 := it.Next()
		if ok2 || ev2.Message != nil {
			t.Errorf("expected false, nil, got %v, %+v", ok2, ev2)
		}
	})
}

func TestAssembleAndRunAgent_ToolResultInEventStream(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-tool-result-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workspace := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	userConfigDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(userConfigDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	testFile := filepath.Join(workspace, "README.md")
	if err := os.WriteFile(testFile, []byte("Hello onclaw!"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	modelCalls := 0
	respondMock := func(input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
		modelCalls++
		if modelCalls == 1 {
			return &schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					{
						Type: schema.ContentBlockTypeFunctionToolCall,
						FunctionToolCall: &schema.FunctionToolCall{
							CallID:    "call_1",
							Name:      "read_file",
							Arguments: `{"file_path":"README.md"}`,
						},
					},
				},
			}, nil
		}
		return &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{Text: "Successfully read README.md. Content: Hello onclaw!"}),
			},
		}, nil
	}

	fm := &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
			return respondMock(input)
		},
		streamFunc: func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
			msg, err := respondMock(input)
			if err != nil {
				return nil, err
			}
			sr, sw := schema.Pipe[*schema.AgenticMessage](1)
			sw.Send(msg, nil)
			sw.Close()
			return sr, nil
		},
	}

	agentConf := &store.Agent{
		Name:          "test-react-agent",
		Provider:      "fake-prov",
		Tools:         "read_file,write_file",
		MaxIterations: 5,
	}

	ctx := context.Background()
	opts := agent.NewTestAssembleOpts(t, func(o *agent.AssembleAgentOpts) {
		o.AgentConf = agentConf
		o.ChatModel = fm
		o.ReviewModel = fm
		o.Workspace = workspace
		o.UserConfigDir = userConfigDir
	})
	agentVal, err := agent.AssembleAgent(ctx, opts)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}

	it := agentVal.Run(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage("Read the README.md file please.")})
	var collected []*schema.AgenticMessage
	for {
		ev, ok := it.Next()
		if !ok {
			break
		}
		if ev.Message != nil {
			collected = append(collected, ev.Message)
		}
	}
	if err := it.Err(); err != nil {
		t.Fatalf("failed to run agent: %v", err)
	}

	hasToolResult := false
	for _, msg := range collected {
		for _, block := range msg.ContentBlocks {
			if block.Type == schema.ContentBlockTypeFunctionToolResult || block.FunctionToolResult != nil {
				hasToolResult = true
			}
		}
	}

	if !hasToolResult {
		t.Errorf("expected FunctionToolResult in event stream, but none was found. Messages: %+v", collected)
	}
}

func TestAssembleAgent_DescriptionDecoupledFromSystemPrompt(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-agent-desc-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workspace := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	userConfigDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(userConfigDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	descText := "A specialized assistant for testing description decoupling"
	var receivedMessages []*schema.AgenticMessage

	fm := &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
			receivedMessages = input
			return &schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.AssistantGenText{Text: "Response"}),
				},
			}, nil
		},
		streamFunc: func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
			receivedMessages = input
			sr, sw := schema.Pipe[*schema.AgenticMessage](1)
			sw.Send(&schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.AssistantGenText{Text: "Stream Response"}),
				},
			}, nil)
			sw.Close()
			return sr, nil
		},
	}

	agentConf := &store.Agent{
		Name:        "test-desc-agent",
		Provider:    "fake-prov",
		Description: descText,
	}

	ctx := context.Background()
	opts := agent.NewTestAssembleOpts(t, func(o *agent.AssembleAgentOpts) {
		o.AgentConf = agentConf
		o.ChatModel = fm
		o.ReviewModel = fm
		o.Workspace = workspace
		o.UserConfigDir = userConfigDir
	})

	agentVal, err := agent.AssembleAgent(ctx, opts)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}

	it := agentVal.Run(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage("Hello")})
	for {
		if _, ok := it.Next(); !ok {
			break
		}
	}

	foundGrounding := false
	foundDescInSystemPrompt := false

	for _, msg := range receivedMessages {
		if msg.Role == schema.AgenticRoleTypeSystem {
			msgStr := fmt.Sprintf("%+v", msg)
			if strings.Contains(msgStr, "Your active workspace directory is:") {
				foundGrounding = true
			}
			if descText != "" && strings.Contains(msgStr, descText) {
				foundDescInSystemPrompt = true
			}
		}
	}

	if !foundGrounding {
		t.Error("expected system message to contain workspace grounding")
	}
	if foundDescInSystemPrompt {
		t.Error("expected agent description NOT to be included in the system prompt instruction")
	}
}
