package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
)

type mockMemoryStore struct {
	SearchFunc func(ctx context.Context, query *memory.ArchiveQuery) ([]*memory.MemoryHit, error)
}

func (m *mockMemoryStore) IndexDocument(ctx context.Context, doc *memory.MemoryDocument, vector []float32) (int64, error) {
	return 0, nil
}
func (m *mockMemoryStore) SearchArchive(ctx context.Context, query *memory.ArchiveQuery) ([]*memory.MemoryHit, error) {
	return m.SearchFunc(ctx, query)
}
func (m *mockMemoryStore) GetDocument(ctx context.Context, id int64) (*memory.MemoryDocument, error) {
	return nil, nil
}
func (m *mockMemoryStore) DeleteDocument(ctx context.Context, id int64) error { return nil }
func (m *mockMemoryStore) GetCachedEmbedding(ctx context.Context, hash string) ([]float32, error) {
	return nil, nil
}
func (m *mockMemoryStore) PutCachedEmbedding(ctx context.Context, hash string, vec []float32) error {
	return nil
}

func TestMemorySearchTool(t *testing.T) {
	ctx := context.Background()

	mockStore := &mockMemoryStore{
		SearchFunc: func(ctx context.Context, query *memory.ArchiveQuery) ([]*memory.MemoryHit, error) {
			if query.Query != "go coding" {
				return nil, nil
			}
			return []*memory.MemoryHit{
				{
					Document: &memory.MemoryDocument{
						Content: "Always write pure Go code without CGO.",
					},
					Score: 0.95,
				},
			}, nil
		},
	}

	scope := &tools.Scope{
		AgentName:   "test-agent",
		MemoryStore: mockStore,
	}

	reg := tools.GetRegistry()
	var searchTool tools.Tool
	for _, tl := range reg {
		if tl.Name() == "memory_search" {
			searchTool = tl
			break
		}
	}

	if searchTool == nil {
		t.Fatal("memory_search tool not found in registry")
	}

	invokable := searchTool.Build(scope)

	args, _ := json.Marshal(map[string]interface{}{"query": "go coding"})
	res, err := invokable.InvokableRun(ctx, string(args))
	if err != nil {
		t.Fatalf("invocation failed: %v", err)
	}

	if !strings.Contains(res, "Always write pure Go code without CGO.") {
		t.Errorf("expected search result to contain match, got: %q", res)
	}
}

func TestSessionSearchTool(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-session-search-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "onclaw.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	ctx := context.Background()

	// Seed some messages into the database
	convStore := sqlite.NewConversationStore(db)
	convID, err := convStore.CreateConversation(ctx, "test-agent")
	if err != nil {
		t.Fatalf("CreateConversation failed: %v", err)
	}

	_, err = convStore.AppendTurn(
		ctx,
		convID,
		`[{"role":"user","content_blocks":[{"type":"user_input_text","user_input_text":{"text":"We need to implement FTS5 indexing for our search tool."}}]}]`,
		"resp-1",
		"",
		"model-1",
		10, 20, 30,
		"We need to implement FTS5 indexing for our search tool.",
		"",
	)
	if err != nil {
		t.Fatalf("AppendTurn failed: %v", err)
	}

	scope := &tools.Scope{
		AgentName: "test-agent",
		Db:        db,
	}

	reg := tools.GetRegistry()
	var sessionTool tools.Tool
	for _, tl := range reg {
		if tl.Name() == "session_search" {
			sessionTool = tl
			break
		}
	}

	if sessionTool == nil {
		t.Fatal("session_search tool not found in registry")
	}

	invokable := sessionTool.Build(scope)

	args, _ := json.Marshal(map[string]interface{}{"query": "FTS5 indexing"})
	res, err := invokable.InvokableRun(ctx, string(args))
	if err != nil {
		t.Fatalf("invocation failed: %v", err)
	}

	if !strings.Contains(res, "We need to implement FTS5 indexing") {
		t.Errorf("expected session search result to contain match, got: %q", res)
	}
}

type mockToolGroupCfg struct {
	config string
}

func (m *mockToolGroupCfg) GetConfig(ctx context.Context, category string) (string, error) {
	return m.config, nil
}

func TestMemoryTool_WriteApprovalOn(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-memory-approval-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "onclaw.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	ctx := context.Background()
	stagedStore := sqlite.NewStagedWriteStore(db)

	toolCfg := &mockToolGroupCfg{
		config: `{"write_approval":true}`,
	}

	scope := &tools.Scope{
		AgentName:        "test-agent",
		ToolGroupCfg:     toolCfg,
		StagedWriteStore: stagedStore,
		Workspace:        tmpDir,
	}

	reg := tools.GetRegistry()
	var memTool tools.Tool
	for _, tl := range reg {
		if tl.Name() == "memory" {
			memTool = tl
			break
		}
	}
	if memTool == nil {
		t.Fatal("memory tool not found in registry")
	}

	invokable := memTool.Build(scope)

	args, _ := json.Marshal(map[string]interface{}{
		"op":      "add",
		"content": "Fact stored for approval",
	})
	res, err := invokable.InvokableRun(ctx, string(args))
	if err != nil {
		t.Fatalf("memory tool call failed: %v", err)
	}

	if !strings.Contains(res, "staged for approval") {
		t.Errorf("expected staging message, got: %q", res)
	}

	writes, err := stagedStore.ListStaged(ctx, "test-agent")
	if err != nil {
		t.Fatalf("ListStaged failed: %v", err)
	}
	if len(writes) != 1 {
		t.Fatalf("expected 1 staged write, got %d", len(writes))
	}
	if writes[0].Operation != "add" {
		t.Errorf("expected operation 'add', got %q", writes[0].Operation)
	}
	if writes[0].Content != "Fact stored for approval" {
		t.Errorf("expected content 'Fact stored for approval', got %q", writes[0].Content)
	}
	if writes[0].Status != "pending" {
		t.Errorf("expected status 'pending', got %q", writes[0].Status)
	}
}

func TestMemoryCoreTool(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-memory-core-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	scope := &tools.Scope{
		Workspace: tmpDir,
		CharLimit: 100,
	}

	reg := tools.GetRegistry()
	var memTool tools.Tool
	for _, tl := range reg {
		if tl.Name() == "memory" {
			memTool = tl
			break
		}
	}

	if memTool == nil {
		t.Fatal("memory tool not found in registry")
	}

	invokable := memTool.Build(scope)

	// Add memory
	args, _ := json.Marshal(map[string]interface{}{
		"op":      "add",
		"content": "Line 1 memory",
	})
	res, err := invokable.InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatalf("add memory failed: %v", err)
	}
	if !strings.Contains(res, "Line 1 memory") {
		t.Errorf("unexpected output: %q", res)
	}

	// Replace memory
	argsReplace, _ := json.Marshal(map[string]interface{}{
		"op":      "replace",
		"target":  "Line 1 memory",
		"content": "Line 1 updated",
	})
	res, err = invokable.InvokableRun(context.Background(), string(argsReplace))
	if err != nil {
		t.Fatalf("replace memory failed: %v", err)
	}
	if !strings.Contains(res, "Line 1 updated") {
		t.Errorf("unexpected output after replace: %q", res)
	}
}
