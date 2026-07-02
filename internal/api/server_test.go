package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agent"
	"github.com/oniharnantyo/onclaw/internal/api/auth"
	"github.com/oniharnantyo/onclaw/internal/api/service"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/llm/adapter"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/skill"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"golang.org/x/crypto/bcrypt"
)

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite db: %v", err)
	}

	if err := sqlite.Migrate(db); err != nil {
		db.Close()
		t.Fatalf("sqlite migrate: %v", err)
	}

	cleanup := func() {
		db.Close()
	}
	return db, cleanup
}

func setupTestServer(t *testing.T, db *sql.DB, resolveFn ResolveAndAssembleFunc) (*Server, string, func()) {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// We need a dummy KeyManager
	km := secrets.NewKeyManager([]byte("0123456789abcdef0123456789abcdef"))

	ps := sqlite.NewProfileStore(db)
	ss := sqlite.NewSecretStore(db)
	as := sqlite.NewAgentStore(db)
	ar := adapter.NewRegistry()
	adapter.DefaultAdapters(ar)
	mgr := llm.NewService(ps, ss, km, ar, as)

	kv := sqlite.NewKVStore(db)
	convStore := sqlite.NewConversationStore(db)

	if resolveFn == nil {
		resolveFn = func(ctx context.Context, agentName, providerName, modelName, reasoning, workspacePath string, convID int64) (AssembledAgent, string, error) {
			return nil, "", fmt.Errorf("resolve agent not implemented in tests")
		}
	}

	skillStore := sqlite.NewSkillStore(db)
	tempHome, err := os.MkdirTemp("", "onclaw-server-test-home")
	if err != nil {
		t.Fatalf("failed to create temp home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tempHome) })
	inst := skill.NewInstaller(skillStore, tempHome)

	hookStore := sqlite.NewHookStore(db)
	execStore := sqlite.NewHookExecutionStore(db)
	mcpStore := sqlite.NewMCPServerStore(db)
	svc := service.New(mgr, kv, convStore, resolveFn, inst, logger, hookStore, execStore, mcpStore, nil, nil)
	s := NewServer(svc, logger)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}

	addr := ln.Addr().String()

	go func() {
		_ = s.Start(ln)
	}()

	cleanup := func() {
		ln.Close()
	}

	return s, addr, cleanup
}

type csrfRoundTripper struct {
	base http.RoundTripper
}

func (c *csrfRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Origin") == "" {
		req.Header.Set("Origin", "http://"+req.Host)
	}
	return c.base.RoundTrip(req)
}

func newTestClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar: jar,
		Transport: &csrfRoundTripper{
			base: http.DefaultTransport,
		},
	}
}

func TestWebHealth(t *testing.T) {
	db, dbCleanup := setupTestDB(t)
	defer dbCleanup()

	_, addr, srvCleanup := setupTestServer(t, db, nil)
	defer srvCleanup()

	resp, err := http.Get(fmt.Sprintf("http://%s/api/health", addr))
	if err != nil {
		t.Fatalf("GET health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var data map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode JSON failed: %v", err)
	}
	if data["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", data["status"])
	}
}

func TestWebAuth(t *testing.T) {
	db, dbCleanup := setupTestDB(t)
	defer dbCleanup()

	// Seed web password
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt hash failed: %v", err)
	}
	_, err = db.Exec("INSERT OR REPLACE INTO preferences (key, value) VALUES ('web_password_hash', ?)", string(hash))
	if err != nil {
		t.Fatalf("seed password failed: %v", err)
	}

	_, addr, srvCleanup := setupTestServer(t, db, nil)
	defer srvCleanup()

	client := newTestClient()

	apiURL := fmt.Sprintf("http://%s/api/providers", addr)

	// 1. Get providers (should fail with 401)
	resp, err := client.Get(apiURL)
	if err != nil {
		t.Fatalf("GET providers failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected unauthorized, got %d", resp.StatusCode)
	}

	// 2. Perform Login with wrong password (should fail with 401)
	loginURL := fmt.Sprintf("http://%s/api/login", addr)
	body, _ := json.Marshal(map[string]string{"password": "wrongpassword"})
	resp, err = client.Post(loginURL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST login failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected login to fail, got %d", resp.StatusCode)
	}

	// 3. Login with correct password (should succeed)
	body, _ = json.Marshal(map[string]string{"password": "secret123"})
	resp, err = client.Post(loginURL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST login failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected login status 200, got %d", resp.StatusCode)
	}

	// Check that we got a session cookie
	u, _ := url.Parse(loginURL)
	cookies := client.Jar.Cookies(u)
	hasCookie := false
	for _, c := range cookies {
		if c.Name == auth.SessionCookieName {
			hasCookie = true
		}
	}
	if !hasCookie {
		t.Errorf("session cookie not found in jar after login")
	}

	// 4. Hit protected API again (should succeed)
	resp, err = client.Get(apiURL)
	if err != nil {
		t.Fatalf("GET providers failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 after authentication, got %d", resp.StatusCode)
	}

	// 5. Logout
	logoutURL := fmt.Sprintf("http://%s/api/logout", addr)
	resp, err = client.Post(logoutURL, "application/json", nil)
	if err != nil {
		t.Fatalf("POST logout failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 after logout, got %d", resp.StatusCode)
	}

	// 6. Protected API should fail again
	resp, err = client.Get(apiURL)
	if err != nil {
		t.Fatalf("GET providers failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401 after logout, got %d", resp.StatusCode)
	}
}

func TestWebProvidersCRUD(t *testing.T) {
	db, dbCleanup := setupTestDB(t)
	defer dbCleanup()

	// Seed web password
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt hash failed: %v", err)
	}
	_, err = db.Exec("INSERT OR REPLACE INTO preferences (key, value) VALUES ('web_password_hash', ?)", string(hash))
	if err != nil {
		t.Fatalf("seed password failed: %v", err)
	}

	_, addr, srvCleanup := setupTestServer(t, db, nil)
	defer srvCleanup()

	client := newTestClient()

	// Login
	loginURL := fmt.Sprintf("http://%s/api/login", addr)
	body, _ := json.Marshal(map[string]string{"password": "secret123"})
	resp, _ := client.Post(loginURL, "application/json", bytes.NewReader(body))
	resp.Body.Close()

	providersURL := fmt.Sprintf("http://%s/api/providers", addr)

	// List initially (should be empty)
	resp, err = client.Get(providersURL)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	var list []service.ProviderView
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 0 {
		t.Errorf("expected 0 providers, got %d", len(list))
	}

	// Create provider
	input := service.ProfileInput{
		Name:         "openai-test",
		ProviderType: "openai",
		APIBase:      "https://api.openai.com/v1",
		Enabled:      true,
	}
	body, _ = json.Marshal(input)
	resp, err = client.Post(providersURL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}

	// Get individual provider
	getURL := fmt.Sprintf("http://%s/api/providers/openai-test", addr)
	resp, err = client.Get(getURL)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	var p service.ProviderView
	_ = json.NewDecoder(resp.Body).Decode(&p)
	resp.Body.Close()
	if p.Name != "openai-test" || p.ProviderType != "openai" || !p.Enabled {
		t.Errorf("unexpected profile data retrieved: %+v", p)
	}

	// Secret status (initially false)
	secretURL := fmt.Sprintf("http://%s/api/providers/openai-test/secret", addr)
	resp, err = client.Get(secretURL)
	if err != nil {
		t.Fatalf("get secret status failed: %v", err)
	}
	var secStatus service.SecretStatus
	_ = json.NewDecoder(resp.Body).Decode(&secStatus)
	resp.Body.Close()
	if secStatus.Set {
		t.Errorf("expected secret to be unset")
	}

	// Set secret
	secReq, _ := json.Marshal(service.SetSecretInput{APIKey: "sk-test-key12345"})
	resp, err = client.Post(secretURL, "application/json", bytes.NewReader(secReq))
	if err != nil {
		t.Fatalf("set secret failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Secret status (now true with hint)
	resp, err = client.Get(secretURL)
	if err != nil {
		t.Fatalf("get secret status failed: %v", err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&secStatus)
	resp.Body.Close()
	if !secStatus.Set {
		t.Errorf("expected secret to be set")
	}
	if secStatus.Hint != "...2345" {
		t.Errorf("expected hint to end in 2345, got %q", secStatus.Hint)
	}

	// Update provider
	input.Enabled = false
	body, _ = json.Marshal(input)
	req, _ := http.NewRequest(http.MethodPut, getURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Get again and check enabled is false
	resp, _ = client.Get(getURL)
	_ = json.NewDecoder(resp.Body).Decode(&p)
	resp.Body.Close()
	if p.Enabled {
		t.Errorf("expected enabled to be false after update")
	}

	// Delete provider
	req, _ = http.NewRequest(http.MethodDelete, getURL, nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Get again should return 404
	resp, _ = client.Get(getURL)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", resp.StatusCode)
	}
}

func TestWebAgentsCRUD(t *testing.T) {
	db, dbCleanup := setupTestDB(t)
	defer dbCleanup()

	// Seed web password
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt hash failed: %v", err)
	}
	_, err = db.Exec("INSERT OR REPLACE INTO preferences (key, value) VALUES ('web_password_hash', ?)", string(hash))
	if err != nil {
		t.Fatalf("seed password failed: %v", err)
	}

	_, addr, srvCleanup := setupTestServer(t, db, nil)
	defer srvCleanup()

	client := newTestClient()

	// Login
	loginURL := fmt.Sprintf("http://%s/api/login", addr)
	body, _ := json.Marshal(map[string]string{"password": "secret123"})
	resp, _ := client.Post(loginURL, "application/json", bytes.NewReader(body))
	resp.Body.Close()

	agentsURL := fmt.Sprintf("http://%s/api/agents", addr)

	// Create Agent
	input := service.AgentInput{
		Name:            "coding-agent",
		Provider:        "openai",
		Model:           "gpt-4",
		SystemPrompt:    "You are a coder",
		ReasoningEffort: "medium",
		MaxIterations:   5,
		IsDefault:       true,
	}
	body, _ = json.Marshal(input)
	resp, err = client.Post(agentsURL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}

	// List agents
	resp, err = client.Get(agentsURL)
	if err != nil {
		t.Fatalf("list agents failed: %v", err)
	}
	var list []service.AgentView
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(list))
	}
	a := list[0]
	if a.Name != "coding-agent" || !a.IsDefault || a.SystemPrompt != "You are a coder" {
		t.Errorf("unexpected agent data: %+v", a)
	}

	// Update agent
	input.SystemPrompt = "You are a senior coder"
	body, _ = json.Marshal(input)
	getURL := fmt.Sprintf("http://%s/api/agents/coding-agent", addr)
	req, _ := http.NewRequest(http.MethodPut, getURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("update agent failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Get agent
	resp, err = client.Get(getURL)
	if err != nil {
		t.Fatalf("get agent failed: %v", err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&a)
	resp.Body.Close()
	if a.SystemPrompt != "You are a senior coder" {
		t.Errorf("expected updated prompt, got %s", a.SystemPrompt)
	}

	// Delete agent
	req, _ = http.NewRequest(http.MethodDelete, getURL, nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("delete agent failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Get agent should return 404
	resp, _ = client.Get(getURL)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", resp.StatusCode)
	}
}

func TestWebConversations(t *testing.T) {
	db, dbCleanup := setupTestDB(t)
	defer dbCleanup()

	// Seed web password
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt hash failed: %v", err)
	}
	_, err = db.Exec("INSERT OR REPLACE INTO preferences (key, value) VALUES ('web_password_hash', ?)", string(hash))
	if err != nil {
		t.Fatalf("seed password failed: %v", err)
	}

	// Seed some conversations and messages
	cStore := sqlite.NewConversationStore(db)
	ctx := context.Background()
	convID, _ := cStore.CreateConversation(ctx, "test-agent")
	_, _ = cStore.AppendMessage(ctx, convID, "user", `{"role":"user","content":"Hello"}`)
	_, _ = cStore.AppendMessage(ctx, convID, "assistant", `{"role":"assistant","content":"Hi"}`)

	_, addr, srvCleanup := setupTestServer(t, db, nil)
	defer srvCleanup()

	client := newTestClient()

	// Login
	loginURL := fmt.Sprintf("http://%s/api/login", addr)
	body, _ := json.Marshal(map[string]string{"password": "secret123"})
	resp, _ := client.Post(loginURL, "application/json", bytes.NewReader(body))
	resp.Body.Close()

	// List conversations
	convURL := fmt.Sprintf("http://%s/api/conversations", addr)
	resp, err = client.Get(convURL)
	if err != nil {
		t.Fatalf("list conversations failed: %v", err)
	}
	var list []store.ConversationRow
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(list))
	}
	if list[0].MessageCount != 2 || list[0].AgentName != "test-agent" {
		t.Errorf("unexpected conversation data: %+v", list[0])
	}

	// Get messages
	msgURL := fmt.Sprintf("http://%s/api/conversations/%d/messages", addr, convID)
	resp, err = client.Get(msgURL)
	if err != nil {
		t.Fatalf("get messages failed: %v", err)
	}
	var messages []store.MessageRow
	_ = json.NewDecoder(resp.Body).Decode(&messages)
	resp.Body.Close()
	if len(messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(messages))
	}
}

func TestWebStaticSPA(t *testing.T) {
	db, dbCleanup := setupTestDB(t)
	defer dbCleanup()

	_, addr, srvCleanup := setupTestServer(t, db, nil)
	defer srvCleanup()

	// Test getting a static file (the fallback index.html is mapped to / index.html)
	resp, err := http.Get(fmt.Sprintf("http://%s/", addr))
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if !bytes.Contains(body, []byte(`id="root"`)) {
		t.Errorf("expected body to contain 'id=\"root\"', got: %s", string(body))
	}

	// Test SPA fallback (getting /any-random-route should return index.html too)
	resp2, err := http.Get(fmt.Sprintf("http://%s/agents/edit/master", addr))
	if err != nil {
		t.Fatalf("GET /agents/edit/master failed: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp2.StatusCode)
	}
	if !bytes.Contains(body2, []byte(`id="root"`)) {
		t.Errorf("expected body to contain fallback index, got: %s", string(body2))
	}
}

type mockEventIterator struct {
	msgs  []*schema.AgenticMessage
	index int
}

func (m *mockEventIterator) Next() (*schema.AgenticMessage, bool) {
	if m.index >= len(m.msgs) {
		return nil, false
	}
	msg := m.msgs[m.index]
	m.index++
	return msg, true
}

func (m *mockEventIterator) Err() error {
	return nil
}

type mockAgent struct {
	iterator agent.EventIterator
}

func (m *mockAgent) Run(ctx context.Context, userInput string) agent.EventIterator {
	return m.iterator
}

func TestWebSSEChat(t *testing.T) {
	db, dbCleanup := setupTestDB(t)
	defer dbCleanup()

	// Seed web password
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt hash failed: %v", err)
	}
	_, err = db.Exec("INSERT OR REPLACE INTO preferences (key, value) VALUES ('web_password_hash', ?)", string(hash))
	if err != nil {
		t.Fatalf("seed password failed: %v", err)
	}

	mockMsgs := []*schema.AgenticMessage{
		{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{Text: "Hello,"}),
			},
		},
		{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{Text: " world!"}),
			},
		},
	}

	resolveFn := func(ctx context.Context, agentName, providerName, modelName, reasoning, workspacePath string, convID int64) (AssembledAgent, string, error) {
		return &mockAgent{
			iterator: &mockEventIterator{msgs: mockMsgs},
		}, "/tmp/workspace", nil
	}

	_, addr, srvCleanup := setupTestServer(t, db, resolveFn)
	defer srvCleanup()

	client := newTestClient()

	// Login
	loginURL := fmt.Sprintf("http://%s/api/login", addr)
	body, _ := json.Marshal(map[string]string{"password": "secret123"})
	resp, _ := client.Post(loginURL, "application/json", bytes.NewReader(body))
	resp.Body.Close()

	// Run Chat
	chatReq, _ := json.Marshal(service.ChatInput{
		Prompt: "Hello",
	})
	chatURL := fmt.Sprintf("http://%s/api/chat", addr)
	resp, err = client.Post(chatURL, "application/json", bytes.NewReader(chatReq))
	if err != nil {
		t.Fatalf("POST chat failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Read stream response
	buf := new(bytes.Buffer)
	_, _ = io.Copy(buf, resp.Body)
	respStr := buf.String()

	if !bytes.Contains(buf.Bytes(), []byte("event: init")) {
		t.Errorf("expected init event in stream, got:\n%s", respStr)
	}
	if !bytes.Contains(buf.Bytes(), []byte("Hello,")) {
		t.Errorf("expected 'Hello,' in stream, got:\n%s", respStr)
	}
	if !bytes.Contains(buf.Bytes(), []byte(" world!")) {
		t.Errorf("expected ' world!' in stream, got:\n%s", respStr)
	}
	if !bytes.Contains(buf.Bytes(), []byte("event: done")) {
		t.Errorf("expected done event in stream, got:\n%s", respStr)
	}
}
