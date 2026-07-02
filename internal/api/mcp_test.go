package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/llm/adapter"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/skill"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"golang.org/x/crypto/bcrypt"
)

func TestMCPAPI(t *testing.T) {
	db, dbCleanup := setupTestDB(t)
	defer dbCleanup()

	// Seed web password for authentication
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt hash failed: %v", err)
	}
	_, err = db.Exec("INSERT OR REPLACE INTO preferences (key, value) VALUES ('web_password_hash', ?)", string(hash))
	if err != nil {
		t.Fatalf("seed password failed: %v", err)
	}

	// Tracks calls to Reload
	var reloadCalls int64
	reloadFn := func() {
		atomic.AddInt64(&reloadCalls, 1)
	}

	// Mock testMCP function
	testMCPFn := func(ctx context.Context, srv *store.MCPServer) ([]string, error) {
		if srv.Name == "test-fail" {
			return nil, fmt.Errorf("connection failed mock")
		}
		return []string{"mock-tool1", "mock-tool2"}, nil
	}

	// Setup Server
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	km := secrets.NewKeyManager([]byte("0123456789abcdef0123456789abcdef"))
	ps := sqlite.NewProfileStore(db)
	ss := sqlite.NewSecretStore(db)
	as := sqlite.NewAgentStore(db)
	ar := adapter.NewRegistry()
	adapter.DefaultAdapters(ar)
	mgr := llm.NewService(ps, ss, km, ar, as)
	kv := sqlite.NewKVStore(db)
	convStore := sqlite.NewConversationStore(db)
	skillStore := sqlite.NewSkillStore(db)
	inst := skill.NewInstaller(skillStore, t.TempDir())
	hookStore := sqlite.NewHookStore(db)
	execStore := sqlite.NewHookExecutionStore(db)
	mcpStore := sqlite.NewMCPServerStore(db)

	svc := service.New(mgr, kv, convStore, nil, inst, logger, hookStore, execStore, mcpStore, reloadFn, testMCPFn)
	s := NewServer(svc, logger)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	addr := ln.Addr().String()
	go func() {
		_ = s.Start(ln)
	}()
	defer ln.Close()

	client := newTestClient()
	loginURL := fmt.Sprintf("http://%s/api/login", addr)
	body, _ := json.Marshal(map[string]string{"password": "secret123"})
	resp, err := client.Post(loginURL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST login failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %v", resp.StatusCode)
	}

	baseMCPURL := fmt.Sprintf("http://%s/api/mcp", addr)

	// 1. Create a stdio server via POST /api/mcp
	srvInput := service.MCPServerInput{
		Name:      "test-stdio",
		Transport: "stdio",
		Command:   "node",
		Args:      `["arg1", "arg2"]`,
		Env:       `{"API_KEY":"secret-val","DEBUG":"true"}`,
		Enabled:   true,
	}
	b, _ := json.Marshal(srvInput)
	resp, err = client.Post(baseMCPURL, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST /api/mcp failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201 Created, got %d", resp.StatusCode)
	}

	var createdView service.MCPServerView
	if err := json.NewDecoder(resp.Body).Decode(&createdView); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if createdView.Name != "test-stdio" {
		t.Errorf("expected name 'test-stdio', got %q", createdView.Name)
	}
	if createdView.Env != `{"API_KEY":"***","DEBUG":"***"}` {
		t.Errorf("expected env values redacted, got %q", createdView.Env)
	}
	if !createdView.Enabled {
		t.Errorf("expected enabled true")
	}

	// Verify Reload was called
	if atomic.LoadInt64(&reloadCalls) != 1 {
		t.Errorf("expected reload to be called 1 time, got %d", atomic.LoadInt64(&reloadCalls))
	}

	// 2. Get server via GET /api/mcp/{name}
	resp, err = client.Get(fmt.Sprintf("%s/test-stdio", baseMCPURL))
	if err != nil {
		t.Fatalf("GET /api/mcp/test-stdio failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
	var getView service.MCPServerView
	if err := json.NewDecoder(resp.Body).Decode(&getView); err != nil {
		t.Fatalf("failed to decode get response: %v", err)
	}
	if getView.Env != `{"API_KEY":"***","DEBUG":"***"}` {
		t.Errorf("expected env redacted, got %q", getView.Env)
	}

	// 3. Update server via PUT /api/mcp/{name}
	srvInput.Command = "nodemon"
	b, _ = json.Marshal(srvInput)
	req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/test-stdio", baseMCPURL), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/mcp/test-stdio failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
	if atomic.LoadInt64(&reloadCalls) != 2 {
		t.Errorf("expected reload to be called 2 times, got %d", atomic.LoadInt64(&reloadCalls))
	}

	// 4. Toggle disabled
	toggleInput := service.ToggleMCPServerInput{Enabled: false}
	b, _ = json.Marshal(toggleInput)
	resp, err = client.Post(fmt.Sprintf("%s/test-stdio/toggle", baseMCPURL), "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST /api/mcp/test-stdio/toggle failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
	if atomic.LoadInt64(&reloadCalls) != 3 {
		t.Errorf("expected reload to be called 3 times, got %d", atomic.LoadInt64(&reloadCalls))
	}

	// Verify disabled in DB/Get
	resp, err = client.Get(fmt.Sprintf("%s/test-stdio", baseMCPURL))
	if err != nil {
		t.Fatalf("GET /api/mcp/test-stdio failed: %v", err)
	}
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&getView)
	if getView.Enabled {
		t.Errorf("expected enabled to be false after toggle")
	}

	// 5. Validation rejections
	badInputs := []struct {
		input service.MCPServerInput
		msg   string
	}{
		{
			input: service.MCPServerInput{Name: "bad", Transport: "invalid"},
			msg:   "bad transport",
		},
		{
			input: service.MCPServerInput{Name: "bad", Transport: "stdio", Command: ""},
			msg:   "missing command",
		},
		{
			input: service.MCPServerInput{Name: "bad", Transport: "http", URL: ""},
			msg:   "missing url",
		},
		{
			input: service.MCPServerInput{Name: "bad", Transport: "stdio", Command: "node", Args: "not-array"},
			msg:   "invalid args JSON",
		},
	}
	for _, tc := range badInputs {
		b, _ = json.Marshal(tc.input)
		resp, err = client.Post(baseMCPURL, "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("POST /api/mcp failed: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for %s, got %d", tc.msg, resp.StatusCode)
		}
	}

	// 6. Test MCP endpoint /api/mcp/test
	testInput := service.MCPServerInput{
		Name:      "test-run",
		Transport: "stdio",
		Command:   "node",
		Enabled:   true,
	}
	b, _ = json.Marshal(testInput)
	resp, err = client.Post(fmt.Sprintf("%s/test", baseMCPURL), "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST /api/mcp/test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
	var testResult map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&testResult); err != nil {
		t.Fatalf("failed to decode test response: %v", err)
	}
	tools, ok := testResult["tools"].([]interface{})
	if !ok || len(tools) != 2 {
		t.Errorf("expected 2 tools, got %v", testResult["tools"])
	}

	// 7. Remove server
	req, _ = http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/test-stdio", baseMCPURL), nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/mcp/test-stdio failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204 NoContent, got %d", resp.StatusCode)
	}
	if atomic.LoadInt64(&reloadCalls) != 4 {
		t.Errorf("expected reload to be called 4 times, got %d", atomic.LoadInt64(&reloadCalls))
	}
}
