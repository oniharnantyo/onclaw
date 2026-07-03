package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/oniharnantyo/onclaw/internal/api"
	"log/slog"
	"net"
	"net/http"
	"os"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	"github.com/oniharnantyo/onclaw/internal/api/service"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/llm/adapter"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/skill"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"golang.org/x/crypto/bcrypt"
)

func TestToolsAPI(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Seed password
	kv := sqlite.NewKVStore(db)
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to generate bcrypt hash: %v", err)
	}
	_ = kv.Set(context.Background(), "web_password_hash", string(hash))

	// Setup api.Server
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	km := secrets.NewKeyManager([]byte("0123456789abcdef0123456789abcdef"))
	ps := sqlite.NewProfileStore(db)
	ss := sqlite.NewSecretStore(db)
	as := sqlite.NewAgentStore(db)
	ar := adapter.NewRegistry()
	adapter.DefaultAdapters(ar)
	mgr := llm.NewService(ps, ss, km, ar, as)

	convStore := sqlite.NewConversationStore(db)
	skillStore := sqlite.NewSkillStore(db)
	inst := skill.NewInstaller(skillStore, t.TempDir())
	hookStore := sqlite.NewHookStore(db)
	execStore := sqlite.NewHookExecutionStore(db)
	mcpStore := sqlite.NewMCPServerStore(db)

	toolRegistryStore := sqlite.NewToolRegistryStore(db)
	toolGroupConfigStore := sqlite.NewToolGroupConfigStore(db)

	svc := service.New(mgr, kv, convStore, nil, inst, logger, hookStore, execStore, mcpStore, nil, nil, toolRegistryStore, toolGroupConfigStore)
	server := api.NewServer(svc, logger)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	addr := ln.Addr().String()
	go func() {
		_ = server.Start(ln)
	}()
	defer ln.Close()

	client := newTestClient()
	loginURL := fmt.Sprintf("http://%s/api/login", addr)

	// 1. Verify unauthenticated gets 401
	toolsURL := fmt.Sprintf("http://%s/api/tools", addr)
	unauthResp, err := client.Get(toolsURL)
	if err != nil {
		t.Fatalf("GET unauth failed: %v", err)
	}
	unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", unauthResp.StatusCode)
	}

	// Login
	body, _ := json.Marshal(map[string]string{"password": "secret123"})
	resp, err := client.Post(loginURL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST login failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %v", resp.StatusCode)
	}

	// 2. GET /api/tools: list and check seeding/categories
	authResp, err := client.Get(toolsURL)
	if err != nil {
		t.Fatalf("GET auth failed: %v", err)
	}
	defer authResp.Body.Close()
	if authResp.StatusCode != http.StatusOK {
		t.Fatalf("GET tools failed: %d", authResp.StatusCode)
	}

	var categories []*service.ToolCategoryView
	if err := json.NewDecoder(authResp.Body).Decode(&categories); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	// Verify categories contain Filesystem and Shell
	foundFs := false
	foundShell := false
	for _, c := range categories {
		if c.Category == "Filesystem" {
			foundFs = true
			if c.Configurable {
				t.Error("Filesystem category should not be configurable by default")
			}
			// Should contain read_file, write_file, list_dir
			expected := map[string]bool{"read_file": true, "write_file": true, "list_dir": true}
			for _, tl := range c.Tools {
				if tl.Description == "" {
					t.Errorf("expected tool %s to have a non-empty description", tl.Name)
				}
				delete(expected, tl.Name)
			}
			if len(expected) > 0 {
				t.Errorf("missing tools in Filesystem category: %v", expected)
			}
		}
		if c.Category == "Shell" {
			foundShell = true
			if len(c.Tools) != 1 || c.Tools[0].Name != "shell" {
				t.Errorf("unexpected tools in Shell category: %v", c.Tools)
			}
		}
	}
	if !foundFs || !foundShell {
		t.Errorf("missing categories: Filesystem=%v, Shell=%v", foundFs, foundShell)
	}

	// 3. POST /api/tools/{name}/toggle
	toggleURL := fmt.Sprintf("http://%s/api/tools/shell/toggle", addr)
	toggleBody, _ := json.Marshal(service.ToggleToolInput{Enabled: false})
	toggleResp, err := client.Post(toggleURL, "application/json", bytes.NewReader(toggleBody))
	if err != nil {
		t.Fatalf("POST toggle failed: %v", err)
	}
	toggleResp.Body.Close()
	if toggleResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", toggleResp.StatusCode)
	}

	// Verify shell is disabled in list
	authResp2, _ := client.Get(toolsURL)
	var categories2 []*service.ToolCategoryView
	_ = json.NewDecoder(authResp2.Body).Decode(&categories2)
	authResp2.Body.Close()

	for _, c := range categories2 {
		if c.Category == "Shell" {
			if len(c.Tools) != 1 || c.Tools[0].Name != "shell" || c.Tools[0].Enabled {
				t.Errorf("expected shell tool to be disabled, got: %+v", c.Tools[0])
			}
		}
	}

	// 4. Register a dummy configurable category to test config GET/PUT
	dummyCat := "Browser"
	dummySchema := `{"type": "object"}`
	var loaded bool
	tools.RegisterConfig(dummyCat, dummySchema, func(ctx context.Context, cfg string) error {
		loaded = true
		if cfg == "invalid" {
			return fmt.Errorf("invalid config")
		}
		return nil
	}, func(ctx context.Context) (string, error) {
		return "{}", nil
	})

	// GET config (should return defaults: {})
	configURL := fmt.Sprintf("http://%s/api/tools/categories/%s/config", addr, dummyCat)
	cfgResp, err := client.Get(configURL)
	if err != nil {
		t.Fatalf("GET config failed: %v", err)
	}
	defer cfgResp.Body.Close()
	if cfgResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", cfgResp.StatusCode)
	}
	var configView service.CategoryConfigView
	_ = json.NewDecoder(cfgResp.Body).Decode(&configView)
	if configView.Category != dummyCat || configView.Config != "{}" {
		t.Errorf("expected default config, got: %+v", configView)
	}

	// PUT config (success case)
	validPutBody, _ := json.Marshal(service.PutCategoryConfigInput{Config: `{"headless": true}`})
	req, _ := http.NewRequest(http.MethodPut, configURL, bytes.NewReader(validPutBody))
	req.Header.Set("Content-Type", "application/json")
	putResp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT config failed: %v", err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", putResp.StatusCode)
	}
	if !loaded {
		t.Error("expected Load hook to be executed on PUT")
	}

	// Verify PUT persisted
	cfgResp2, _ := client.Get(configURL)
	var configView2 service.CategoryConfigView
	_ = json.NewDecoder(cfgResp2.Body).Decode(&configView2)
	cfgResp2.Body.Close()
	if configView2.Config != `{"headless": true}` {
		t.Errorf("expected config `{\"headless\": true}`, got %s", configView2.Config)
	}

	// PUT config (validation failure case)
	invalidPutBody, _ := json.Marshal(service.PutCategoryConfigInput{Config: "invalid"})
	req2, _ := http.NewRequest(http.MethodPut, configURL, bytes.NewReader(invalidPutBody))
	req2.Header.Set("Content-Type", "application/json")
	putResp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("PUT config invalid failed: %v", err)
	}
	putResp2.Body.Close()
	if putResp2.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got %d", putResp2.StatusCode)
	}

	// GET config for unknown category (should return 404)
	unknownConfigURL := fmt.Sprintf("http://%s/api/tools/categories/UnknownCategory/config", addr)
	unknownCfgResp, err := client.Get(unknownConfigURL)
	if err != nil {
		t.Fatalf("GET config unknown failed: %v", err)
	}
	unknownCfgResp.Body.Close()
	if unknownCfgResp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d", unknownCfgResp.StatusCode)
	}

	// PUT config for unknown category (should return 404)
	req3, _ := http.NewRequest(http.MethodPut, unknownConfigURL, bytes.NewReader(validPutBody))
	req3.Header.Set("Content-Type", "application/json")
	putResp3, err := client.Do(req3)
	if err != nil {
		t.Fatalf("PUT config unknown failed: %v", err)
	}
	putResp3.Body.Close()
	if putResp3.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d", putResp3.StatusCode)
	}
}
