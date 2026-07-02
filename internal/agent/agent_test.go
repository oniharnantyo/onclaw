package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
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
							Arguments: `{"path":"README.md"}`,
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
	agent, err := AssembleAgent(ctx, agentConf, fm, workspace, userConfigDir, "deny", nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}

	var stdout bytes.Buffer
	it := agent.Run(ctx, "Read the README.md file please.")
	tr := render.Text(&stdout)
	for {
		msg, ok := it.Next()
		if !ok {
			break
		}
		if err := tr.Render(msg); err != nil {
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
	agent, err := AssembleAgent(ctx, agentConf, fm, workspace, userConfigDir, "deny", nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}
	// Cancel context immediately
	cancel()

	it := agent.Run(ctx, "Hello")
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
	ag, err := AssembleAgent(ctx, agentConf, fm, workspace, userConfigDir, "deny", nil, 128000, dummyConvStore{}, 1, nil, nil, nil, "test", nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}
	if ag == nil {
		t.Fatal("expected non-nil agent")
	}

	// 2. Re-assemble with 64000 context window
	ag2, err := AssembleAgent(ctx, agentConf, fm, workspace, userConfigDir, "deny", nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to assemble agent second time: %v", err)
	}
	if ag2 == nil {
		t.Fatal("expected non-nil agent on second assembly")
	}
}

func TestSummarizationTrigger(t *testing.T) {
	tests := []struct {
		window   int
		expected int
	}{
		{128000, 102400},
		{64000, 51200},
		{0, 0},
	}
	for _, tc := range tests {
		got := summarizationTrigger(tc.window)
		if got != tc.expected {
			t.Errorf("summarizationTrigger(%d) = %d; want %d", tc.window, got, tc.expected)
		}
	}
}

type dummyConvStore struct{}

func (dummyConvStore) CreateConversation(ctx context.Context, agentName string) (int64, error) {
	return 1, nil
}
func (dummyConvStore) AppendMessage(ctx context.Context, conversationID int64, role string, messageJSON string) (int64, error) {
	return 1, nil
}
func (dummyConvStore) LoadHistory(ctx context.Context, conversationID int64) (*store.MessageRow, []*store.MessageRow, error) {
	return nil, nil, nil
}
func (dummyConvStore) ListMessages(ctx context.Context, conversationID int64) ([]*store.MessageRow, error) {
	return nil, nil
}
func (dummyConvStore) SaveSummary(ctx context.Context, conversationID int64, summaryMessageJSON string, coveredUntilSeq int64) error {
	return nil
}
func (dummyConvStore) ListConversations(ctx context.Context) ([]*store.ConversationRow, error) {
	return nil, nil
}

type mockEnabledChecker struct {
	disabled map[string]bool
}

func (m *mockEnabledChecker) Enabled(name string) bool {
	return !m.disabled[name]
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

	// 1. With all tools enabled
	agentConf := &store.Agent{
		Name: "test-global-enable",
	}
	ag, err := AssembleAgent(ctx, agentConf, fm, workspace, userConfigDir, "deny", nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}
	if len(ag.Tools) == 0 {
		t.Error("expected tools, got 0")
	}

	// 2. With read_file disabled globally
	mockStore := &mockToolRegistryStore{
		list: []*store.ToolRegistry{
			{Name: "read_file", Enabled: 0},
			{Name: "write_file", Enabled: 1},
			{Name: "list_dir", Enabled: 1},
			{Name: "shell", Enabled: 1},
		},
	}
	ag, err = AssembleAgent(ctx, agentConf, fm, workspace, userConfigDir, "deny", nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", mockStore, nil, nil)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}

	hasReadFile := false
	hasWriteFile := false
	for _, tl := range ag.Tools {
		info, _ := tl.Info(ctx)
		if info.Name == "read_file" {
			hasReadFile = true
		}
		if info.Name == "write_file" {
			hasWriteFile = true
		}
	}
	if hasReadFile {
		t.Error("expected read_file to be globally excluded, but it was present")
	}
	if !hasWriteFile {
		t.Error("expected write_file to be present")
	}

	// 3. Intersection with per-agent allowlist
	agentConf.Tools = "write_file,read_file" // agent only allows write_file and read_file
	ag, err = AssembleAgent(ctx, agentConf, fm, workspace, userConfigDir, "deny", nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", mockStore, nil, nil)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}
	var activeTools []string
	for _, tl := range ag.Tools {
		info, _ := tl.Info(ctx)
		activeTools = append(activeTools, info.Name)
	}
	if len(activeTools) != 1 || activeTools[0] != "write_file" {
		t.Errorf("expected effective tools to be exactly [write_file], got %v", activeTools)
	}

}
