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
	"github.com/oniharnantyo/onclaw/internal/memory"
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
	agentVal, err := agent.AssembleAgent(ctx, agentConf, fm, fm, workspace, userConfigDir, "deny", nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, 0, nil, 3)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}

	var stdout bytes.Buffer
	it := agentVal.Run(ctx, "Read the README.md file please.")
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
	agentVal, err := agent.AssembleAgent(ctx, agentConf, fm, fm, workspace, userConfigDir, "deny", nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, 0, nil, 3)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}
	// Cancel context immediately
	cancel()

	it := agentVal.Run(ctx, "Hello")
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
	ag, err := agent.AssembleAgent(ctx, agentConf, fm, fm, workspace, userConfigDir, "deny", nil, 128000, dummyConvStore{}, 1, nil, nil, nil, "test", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, 0, nil, 3)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}
	if ag == nil {
		t.Fatal("expected non-nil agent")
	}

	// 2. Re-assemble with 64000 context window
	ag2, err := agent.AssembleAgent(ctx, agentConf, fm, fm, workspace, userConfigDir, "deny", nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, 0, nil, 3)
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
		got := agent.SummarizationTrigger(tc.window)
		if got != tc.expected {
			t.Errorf("summarizationTrigger(%d) = %d; want %d", tc.window, got, tc.expected)
		}
	}
}

type dummyConvStore struct{}

func (dummyConvStore) CreateConversation(ctx context.Context, agentName string) (int64, error) {
	return 1, nil
}
func (dummyConvStore) AppendTurn(ctx context.Context, convID int64, msgArrayJSON string, responseID string, previousResponseID string, model string, prompt int64, completion int64, total int64, question string, answer string) (int64, error) {
	return 1, nil
}
func (dummyConvStore) LoadHistory(ctx context.Context, conversationID int64) (*store.TurnRow, []*store.TurnRow, error) {
	return nil, nil, nil
}
func (dummyConvStore) ListTurns(ctx context.Context, conversationID int64) ([]*store.TurnRow, error) {
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

	// 0. Explicit: an empty allowlist must offer every globally-enabled tool (empty = all).
	mockAll := &mockToolRegistryStore{
		list: []*store.ToolRegistry{
			{Name: "read_file", Enabled: 1},
			{Name: "write_file", Enabled: 1},
			{Name: "list_dir", Enabled: 1},
			{Name: "shell", Enabled: 1},
		},
	}
	agentConfEmpty := &store.Agent{Name: "test-empty-allowlist"}
	agEmpty, err := agent.AssembleAgent(ctx, agentConfEmpty, fm, fm, workspace, userConfigDir, "deny", nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", mockAll, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, 0, nil, 3)
	if err != nil {
		t.Fatalf("failed to assemble agent: %v", err)
	}
	gotAll := make(map[string]bool)
	for _, tl := range agEmpty.Tools {
		info, _ := tl.Info(ctx)
		gotAll[info.Name] = true
	}
	// With no per-agent allowlist, every globally-enabled registry tool must be offered.
	for _, name := range []string{"read_file", "write_file", "list_dir", "shell"} {
		if !gotAll[name] {
			t.Errorf("empty allowlist should offer globally-enabled tool %q, but it was absent", name)
		}
	}

	// 1. With all tools enabled
	agentConf := &store.Agent{
		Name: "test-global-enable",
	}
	ag, err := agent.AssembleAgent(ctx, agentConf, fm, fm, workspace, userConfigDir, "deny", nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, 0, nil, 3)
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
	ag, err = agent.AssembleAgent(ctx, agentConf, fm, fm, workspace, userConfigDir, "deny", nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", mockStore, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, 0, nil, 3)
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
	ag, err = agent.AssembleAgent(ctx, agentConf, fm, fm, workspace, userConfigDir, "deny", nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", mockStore, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, 0, nil, 3)
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
	_, err := agent.AssembleAgent(ctx, agentConf, fm, fm, workspace, userConfigDir, "deny", nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, 0, nil, 3)
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
	_, err := agent.AssembleAgent(ctx, agentConf, fm, fm, workspace, userConfigDir, "deny", nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", mockStore, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, 0, nil, 3)
	if err == nil || !strings.Contains(err.Error(), "list tools for enabled checker") {
		t.Errorf("expected list tools error, got %v", err)
	}
}

type fakeMemoryStore struct {
	docs  []*memory.MemoryDocument
	cache map[string][]float32
}

func (f *fakeMemoryStore) IndexDocument(ctx context.Context, doc *memory.MemoryDocument, vector []float32) (int64, error) {
	f.docs = append(f.docs, doc)
	return int64(len(f.docs)), nil
}
func (f *fakeMemoryStore) SearchArchive(ctx context.Context, q *memory.ArchiveQuery) ([]*memory.MemoryHit, error) {
	return nil, nil
}
func (f *fakeMemoryStore) GetDocument(ctx context.Context, id int64) (*memory.MemoryDocument, error) {
	return nil, nil
}
func (f *fakeMemoryStore) DeleteDocument(ctx context.Context, id int64) error {
	return nil
}
func (f *fakeMemoryStore) GetCachedEmbedding(ctx context.Context, embeddingModel string, hash string) ([]float32, error) {
	return f.cache[hash], nil
}
func (f *fakeMemoryStore) PutCachedEmbedding(ctx context.Context, embeddingModel string, hash string, vec []float32) error {
	if f.cache == nil {
		f.cache = make(map[string][]float32)
	}
	f.cache[hash] = vec
	return nil
}

type fakeModelForSummary struct {
	response string
}

func (f *fakeModelForSummary) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{
				Type:             schema.ContentBlockTypeAssistantGenText,
				AssistantGenText: &schema.AssistantGenText{Text: f.response},
			},
		},
	}, nil
}

func (f *fakeModelForSummary) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return nil, fmt.Errorf("stream not implemented")
}

type fakeKVStore struct {
	data map[string]string
}

func (f *fakeKVStore) Get(ctx context.Context, key string) (string, error) {
	v, ok := f.data[key]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return v, nil
}
func (f *fakeKVStore) Set(ctx context.Context, key, value string) error {
	if f.data == nil {
		f.data = make(map[string]string)
	}
	f.data[key] = value
	return nil
}
func (f *fakeKVStore) Delete(ctx context.Context, key string) error {
	delete(f.data, key)
	return nil
}

type recordingConvStore struct {
	dummyConvStore
	lastSummary    string
	lastCoveredSeq int64
	saveSummaryErr error
}

func (r *recordingConvStore) SaveSummary(ctx context.Context, conversationID int64, summaryMessageJSON string, coveredUntilSeq int64) error {
	if r.saveSummaryErr != nil {
		return r.saveSummaryErr
	}
	r.lastSummary = summaryMessageJSON
	r.lastCoveredSeq = coveredUntilSeq
	return nil
}

func TestHandleSummarization_ExtractAndFlushCalled(t *testing.T) {
	ctx := context.Background()
	ms := &fakeMemoryStore{}
	kv := &fakeKVStore{data: map[string]string{}}
	model := &fakeModelForSummary{response: "- User prefers Go\n- Project uses SQLite"}
	conv := &recordingConvStore{}

	discardedMsg := schema.UserAgenticMessage("I need help with X")
	discardedMsg.Extra = map[string]interface{}{"_onclaw_seq": int64(1)}
	summaryMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "Summary of the discussion"}),
		},
	}

	before := adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{discardedMsg},
	}
	after := adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{summaryMsg},
	}

	_, err := agent.HandleSummarization(ctx, agent.HandleSummarizationParams{
		Before:         before,
		After:          after,
		ChatModel:      model,
		MemoryStore:    ms,
		Embedder:       nil,
		KVStore:        kv,
		AgentName:      "test-agent",
		ConversationID: 42,
		ConvStore:      conv,
	})
	if err != nil {
		t.Fatalf("HandleSummarization returned error: %v", err)
	}
	if len(ms.docs) == 0 {
		t.Error("expected at least one document to be indexed via ExtractAndFlush")
	}
	if conv.lastSummary == "" {
		t.Error("expected SaveSummary to have been called")
	}
	if conv.lastCoveredSeq != 1 {
		t.Errorf("expected coveredUntilSeq=1, got %d", conv.lastCoveredSeq)
	}
}

func TestHandleSummarization_MemoryStoreNil(t *testing.T) {
	ctx := context.Background()
	model := &fakeModelForSummary{response: "- fact: test"}
	conv := &recordingConvStore{}

	discardedMsg := schema.UserAgenticMessage("help")
	summaryMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "summary here"}),
		},
	}

	before := adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{discardedMsg},
	}
	after := adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{summaryMsg},
	}

	_, err := agent.HandleSummarization(ctx, agent.HandleSummarizationParams{
		Before:         before,
		After:          after,
		ChatModel:      model,
		MemoryStore:    nil,
		Embedder:       nil,
		KVStore:        nil,
		AgentName:      "test-agent",
		ConversationID: 42,
		ConvStore:      conv,
	})
	if err != nil {
		t.Fatalf("HandleSummarization returned error: %v", err)
	}
	if conv.lastSummary == "" {
		t.Error("expected SaveSummary to have been called even without memoryStore")
	}
}

func TestHandleSummarization_NoSummaryMsg(t *testing.T) {
	ctx := context.Background()
	conv := &recordingConvStore{}

	beforeMsg := schema.UserAgenticMessage("hello")

	before := adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{beforeMsg},
	}
	after := adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{beforeMsg},
	}

	_, err := agent.HandleSummarization(ctx, agent.HandleSummarizationParams{
		Before:    before,
		After:     after,
		ConvStore: conv,
	})
	if err != nil {
		t.Fatalf("HandleSummarization should not error on noop: %v", err)
	}
	if conv.lastSummary != "" {
		t.Error("expected no SaveSummary call when there is no new message")
	}
}

func TestHandleSummarization_MaxSeqFromExtra(t *testing.T) {
	ctx := context.Background()
	model := &fakeModelForSummary{response: "- fact: important"}
	conv := &recordingConvStore{}

	msg1 := schema.UserAgenticMessage("first")
	msg1.Extra = map[string]interface{}{"_onclaw_seq": int64(5)}
	msg2 := schema.UserAgenticMessage("second")
	msg2.Extra = map[string]interface{}{"_onclaw_seq": int64(10)}
	summaryMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "final summary"}),
		},
	}

	before := adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{msg1, msg2},
	}
	after := adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{summaryMsg},
	}

	_, err := agent.HandleSummarization(ctx, agent.HandleSummarizationParams{
		Before:      before,
		After:       after,
		ChatModel:   model,
		MemoryStore: nil,
		ConvStore:   conv,
	})
	if err != nil {
		t.Fatalf("HandleSummarization returned error: %v", err)
	}
	if conv.lastCoveredSeq != 10 {
		t.Errorf("expected maxSeq=10 (highest _onclaw_seq), got %d", conv.lastCoveredSeq)
	}
}

func TestHandleSummarization_SaveSummaryError(t *testing.T) {
	ctx := context.Background()
	conv := &recordingConvStore{saveSummaryErr: fmt.Errorf("db error")}

	discardedMsg := schema.UserAgenticMessage("hello")
	summaryMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "summary"}),
		},
	}

	before := adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{discardedMsg},
	}
	after := adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{summaryMsg},
	}

	_, err := agent.HandleSummarization(ctx, agent.HandleSummarizationParams{
		Before:      before,
		After:       after,
		ChatModel:   &fakeModelForSummary{},
		MemoryStore: nil,
		ConvStore:   conv,
	})
	if err == nil || !strings.Contains(err.Error(), "db error") {
		t.Errorf("expected 'db error', got %v", err)
	}
}

func TestEventIterator_EdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("prior error", func(t *testing.T) {
		err := errors.New("prior error")
		it := agent.NewEventIterator(ctx, nil, nil, err, nil)
		msg, ok := it.Next()
		if ok || msg != nil {
			t.Errorf("expected false, nil, got %v, %v", ok, msg)
		}
		if it.Err() != err {
			t.Errorf("expected err %v, got %v", err, it.Err())
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		it := agent.NewEventIterator(cctx, nil, nil, nil, nil)
		msg, ok := it.Next()
		if ok || msg != nil {
			t.Errorf("expected false, nil, got %v, %v", ok, msg)
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
		msg, ok := it.Next()
		if ok || msg != nil {
			t.Errorf("expected false, nil, got %v, %v", ok, msg)
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

		msg, ok := it.Next()
		if ok || msg != nil {
			t.Errorf("expected false, nil, got %v, %v", ok, msg)
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

		msg, ok := it.Next()
		if ok || msg != nil {
			t.Errorf("expected false, nil, got %v, %v", ok, msg)
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

		msg, ok := it.Next()
		if !ok || msg != expectedMsg {
			t.Errorf("expected true, %v, got %v, %v", expectedMsg, ok, msg)
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
		msg, ok := it.Next()
		if !ok || msg != expectedMsg {
			t.Errorf("expected true, %v, got %v, %v", expectedMsg, ok, msg)
		}

		// Next call should drain/finish since stream is EOF and gen is closed
		msg2, ok2 := it.Next()
		if ok2 || msg2 != nil {
			t.Errorf("expected false, nil, got %v, %v", ok2, msg2)
		}
	})
}
