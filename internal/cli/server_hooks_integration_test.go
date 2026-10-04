//go:build integration

package cli

// Integration regression pin for fix-hooks-dispatcher-unwired: a workspace
// pre_tool_use command hook stored in the database MUST gate a real /v1 turn
// driven through the PRODUCTION composition path — the runner assembled by
// assembleServerRuntime, never a test-local agents.NewRunner (a test-local
// option list is exactly the drift class that left the dispatcher unwired).
//
// The scenario (design.md D3, behavioral): seed an enabled workspace hook
// whose command handler exits 2 with stderr against the built-in write_file
// tool, point an openai-compatible provider at a scripted stub (the same
// trick scripts/v1smoke/stub_llm.py uses for the smoke suite), drive a
// POST /v1/responses turn whose model reply invokes the tool, and assert
//   (a) the tool result carries the canonical {"blocked_by_hook":true,...}
//       payload,
//   (b) the tool's side effect (the file write) never happened,
//   (c) a hooks-execution audit row records the block.
//
// On the unwired tree this fails with the empty-chain signature: the tool
// succeeds and writes the file, and the audit records zero rows.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/config"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/postgres"
)

// hookTestBaseDSN resolves the integration DSN (the setupTestSchema pattern).
func hookTestBaseDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = "postgres://localhost:5432/postgres?sslmode=disable"
	}
	return dsn
}

// hookTestConfig boots an isolated per-test schema (migrated, dropped at
// cleanup — never the dev database) and returns the composition config plus
// the store the assembler opened.
func hookTestConfig(t *testing.T, ctx context.Context) (config.Config, string) {
	t.Helper()
	baseDSN := hookTestBaseDSN(t)

	conn, err := pgx.Connect(ctx, baseDSN)
	if err != nil {
		t.Skipf("skipping integration test: database connection failed: %v", err)
	}
	defer conn.Close(ctx)

	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random schema suffix: %v", err)
	}
	schemaName := "hookwire_" + hex.EncodeToString(b)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	separator := "?"
	if strings.Contains(baseDSN, "?") {
		separator = "&"
	}
	schemaDSN := fmt.Sprintf("%s%ssearch_path=%s,public", baseDSN, separator, schemaName)

	migrator := postgres.NewMigrator(schemaDSN)
	if err := migrator.Up(); err != nil {
		_, _ = conn.Exec(ctx, "DROP SCHEMA "+schemaName+" CASCADE")
		t.Fatalf("run migrations: %v", err)
	}

	t.Cleanup(func() {
		cleanupConn, err := pgx.Connect(context.Background(), baseDSN)
		if err == nil {
			_, _ = cleanupConn.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schemaName+" CASCADE")
			_ = cleanupConn.Close(context.Background())
		}
	})

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("random encryption key: %v", err)
	}

	cfg := config.Config{
		DatabaseURL:          schemaDSN,
		ListenAddr:           "127.0.0.1:0",
		JWTSecret:            "hooktest-secret-key-that-is-32-bytes!",
		EncryptionKey:        hex.EncodeToString(key),
		TokenTTL:             time.Hour,
		DataDir:              t.TempDir(),
		CacheDir:             t.TempDir(),
		OnClawDir:            t.TempDir(),
		StorageDriver:        "local",
		RunDrainWindow:       2 * time.Second,
		SchedulerTick:        time.Hour,
		SchedulerRunTimeout:  time.Minute,
		HeartbeatTick:        time.Hour,
		HeartbeatRunTimeout:  time.Minute,
		HeartbeatConcurrency: 1,
		HooksCommandEnabled:  true,
		HooksScriptEnabled:   true,
	}
	return cfg, schemaDSN
}

// hookStubLLM is the scripted OpenAI-compatible chat-completions backend: the
// first model call of a HOOKTEST turn replies with a write_file tool call;
// once a tool result is on the wire it replies with plain text. Everything
// else (memory-curation side-calls) gets plain text.
func hookStubLLM(t *testing.T, toolCallIssued *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		lastUser := ""
		sawToolResult := false
		for _, m := range req.Messages {
			if m.Role == "tool" {
				sawToolResult = true
			}
			if m.Role == "user" {
				lastUser = string(m.Content)
			}
		}

		reply := func(kind, payload string) {
			model := req.Model
			if model == "" {
				model = "stub-1"
			}
			cid := "chatcmpl_hooktest"
			if !req.Stream {
				var msg map[string]any
				if kind == "tool" {
					msg = map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{
						"id": "call_hooktest_1", "type": "function",
						"function": map[string]any{"name": "write_file", "arguments": payload},
					}}}
				} else {
					msg = map[string]any{"role": "assistant", "content": payload}
				}
				resp := map[string]any{
					"id": cid, "object": "chat.completion", "created": 0, "model": model,
					"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": msg}},
					"usage":   map[string]any{"prompt_tokens": 12, "completion_tokens": 9, "total_tokens": 21},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			chunk := func(delta map[string]any, finish any, usage any) {
				c := map[string]any{"id": cid, "object": "chat.completion.chunk", "created": 0, "model": model,
					"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
				if usage != nil {
					c["usage"] = usage
				}
				_, _ = w.Write([]byte("data: " + mustJSON(c) + "\n\n"))
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
			chunk(map[string]any{"role": "assistant"}, nil, nil)
			if kind == "tool" {
				chunk(map[string]any{"tool_calls": []any{map[string]any{
					"index": 0, "id": "call_hooktest_1", "type": "function",
					"function": map[string]any{"name": "write_file", "arguments": ""},
				}}}, nil, nil)
				chunk(map[string]any{"tool_calls": []any{map[string]any{
					"index": 0, "function": map[string]any{"arguments": payload},
				}}}, nil, nil)
			} else {
				for i := 0; i < len(payload); i += 8 {
					end := i + 8
					if end > len(payload) {
						end = len(payload)
					}
					chunk(map[string]any{"content": payload[i:end]}, nil, nil)
				}
			}
			chunk(map[string]any{}, "stop", map[string]any{"prompt_tokens": 12, "completion_tokens": 9, "total_tokens": 21})
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		}

		switch {
		case sawToolResult:
			reply("text", "done")
		case strings.Contains(lastUser, "HOOKTEST"):
			*toolCallIssued++
			args := mustJSON(map[string]any{"file_path": "/workspace/hooked-by-model.txt", "content": "the hook must stop me"})
			reply("tool", args)
		default:
			reply("text", "stub idle reply")
		}
	}))
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestIntegration_HooksGateV1Turn pins the serving runner's hook chain: the
// composition-root runner (the one RuntimeOptions ships) resolves stored
// workspace hooks and pre_tool_use blocks land as the canonical tool result.
func TestIntegration_HooksGateV1Turn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	cfg, _ := hookTestConfig(t, ctx)

	toolCallIssued := 0
	stub := hookStubLLM(t, &toolCallIssued)
	defer stub.Close()

	rt, err := assembleServerRuntime(ctx, cfg)
	if err != nil {
		t.Fatalf("assemble server runtime: %v", err)
	}
	t.Cleanup(func() { _ = rt.stop() })
	t.Cleanup(rt.close)

	st := rt.st

	// Seed: workspace, owner trio, provider (openai-compatible at the stub),
	// agent, on-disk jail, web.search entries (the default registry's only
	// construction-failing tool), and the blocking pre_tool_use hook.
	ws := &domain.Workspace{Slug: "hookws", Name: "Hook WS", Timezone: "UTC"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	user := &domain.User{Email: "hook-owner@example.com", Name: "Owner"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: "owner"}
	if err := st.Roles().Create(ctx, role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("add member: %v", err)
	}

	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai-compatible", Name: "Stub LLM", BaseURL: stub.URL, Enabled: true}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	agent := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  prov.ID,
		Model:       "stub-model",
		Temperature: 0.5,
	}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(cfg.OnClawDir), ws.Slug, agent.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("seed agent dir: %v", err)
	}

	if err := st.ToolSettings().Upsert(ctx, &domain.WorkspaceToolSetting{
		WorkspaceID: ws.ID,
		ToolKey:     "web.search",
		Enabled:     true,
		Config: map[string]any{
			"entries": []any{
				map[string]any{"name": "Tavily fixture", "provider": "tavily", "api_key": "tvly-v1-fixture-key"},
			},
		},
	}); err != nil {
		t.Fatalf("seed web.search tool settings: %v", err)
	}

	hook := &domain.WorkspaceHook{
		WorkspaceID: ws.ID,
		HookBase: domain.HookBase{
			Name:        "policy-guard",
			Event:       domain.HookEventPreToolUse,
			Matcher:     "write_file",
			HandlerType: domain.HookHandlerCommand,
			Config:      json.RawMessage(`{"command":"sh","args":["-c","echo blocked by policy >&2; exit 2"]}`),
			TimeoutMS:   domain.DefaultHookTimeoutMS,
			OnFailure:   domain.HookFailureAllow,
			Enabled:     true,
		},
	}
	if err := st.Hooks().CreateWorkspaceHook(ctx, hook); err != nil {
		t.Fatalf("create workspace hook: %v", err)
	}

	// Serve the assembled router over a real HTTP listener and drive the turn
	// exactly like an API client would.
	ts := httptest.NewServer(rt.srv.Handler)
	defer ts.Close()

	plaintext, _, err := services.NewAPIKeyService(st.APIKeys()).Create(ctx, ws.ID, "hooktest", user.ID)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"model": agent.Slug,
		"input": "HOOKTEST write the file hooked-by-model.txt",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+plaintext)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/responses: %v", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/responses = %d: %s", resp.StatusCode, respBody)
	}
	if toolCallIssued == 0 {
		t.Fatalf("stub never issued the write_file tool call — the turn never reached the tool lane (response: %s)", respBody)
	}

	// (a) The write_file tool result IS the canonical block payload
	// (byte-exact, per D3: every seam emits the same shape).
	var wire struct {
		Output []struct {
			Type   string `json:"type"`
			Name   string `json:"name"`
			Result string `json:"result"`
		} `json:"output"`
	}
	if err := json.Unmarshal(respBody, &wire); err != nil {
		t.Fatalf("decode /v1/responses body: %v", err)
	}
	toolResult := ""
	for _, item := range wire.Output {
		if item.Type == "onclaw.function_call_output" && item.Name == "write_file" {
			toolResult = item.Result
		}
	}
	if want := agenthooks.BlockToolResult("policy-guard", "blocked by policy"); toolResult != want {
		t.Errorf("write_file tool result = %q, want the canonical block payload %q — the hook chain resolved empty (response: %s)", toolResult, want, respBody)
	}

	// (b) The wrapped tool's side effect never happened.
	target := filepath.Join(agentDir, "hooked-by-model.txt")
	if _, err := os.Stat(target); err == nil {
		t.Errorf("write_file side effect occurred: %s exists — the blocked tool executed", target)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat side-effect target: %v", err)
	}

	// (c) The hooks-execution audit records the delivery.
	if err := waitForAudit(t, st, ws.ID, func(execs []domain.HookExecution) bool {
		for _, e := range execs {
			if e.HookName == "policy-guard" && e.Event == domain.HookEventPreToolUse && e.Decision == "block" {
				return true
			}
		}
		return false
	}); err != nil {
		execRows := countHookExecutions(t, st, ws.ID)
		t.Errorf("no hooks-execution audit row for the policy-guard block (%d total execution rows in the workspace): %v", execRows, err)
	}
}

// waitForAudit polls briefly for the audit row; the pre_tool_use delivery is
// synchronous inside the turn, so this converges immediately when wired.
func waitForAudit(t *testing.T, st store.Store, workspaceID string, pred func([]domain.HookExecution) bool) error {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		execs, err := st.Hooks().ListHookExecutions(context.Background(), workspaceID, nil, 0)
		if err != nil {
			lastErr = err
		} else if pred(execs) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("audit row never appeared")
}

func countHookExecutions(t *testing.T, st store.Store, workspaceID string) int {
	t.Helper()
	execs, err := st.Hooks().ListHookExecutions(context.Background(), workspaceID, nil, 0)
	if err != nil {
		return -1
	}
	return len(execs)
}
