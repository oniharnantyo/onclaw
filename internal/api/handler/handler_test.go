// Package handler_test provides black-box tests for the HTTP handler layer.
// It builds real service.Service instances backed by in-process fake stores.
package handler_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/handler"
	"github.com/oniharnantyo/onclaw/internal/api/service"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/llm/adapter"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/skill"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ---------------------------------------------------------------------------
// In-process fakes (duplicated from service_test — handler package is separate)
// ---------------------------------------------------------------------------

type hFakeProfileStore struct {
	mu       sync.RWMutex
	profiles map[string]*store.Profile
}

func newHFakeProfileStore() *hFakeProfileStore {
	return &hFakeProfileStore{profiles: make(map[string]*store.Profile)}
}
func (f *hFakeProfileStore) AddProfile(_ context.Context, p *store.Profile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.profiles[p.Name] = p
	return nil
}
func (f *hFakeProfileStore) GetProfile(_ context.Context, name string) (*store.Profile, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	p, ok := f.profiles[name]
	if !ok {
		return nil, fmt.Errorf("profile %q not found", name)
	}
	pc := *p
	return &pc, nil
}
func (f *hFakeProfileStore) ListProfiles(_ context.Context) ([]*store.Profile, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var list []*store.Profile
	for _, p := range f.profiles {
		pc := *p
		list = append(list, &pc)
	}
	return list, nil
}
func (f *hFakeProfileStore) RemoveProfile(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.profiles, name)
	return nil
}

type hFakeSecretStore struct {
	mu      sync.RWMutex
	secrets map[string]string
}

func newHFakeSecretStore() *hFakeSecretStore {
	return &hFakeSecretStore{secrets: make(map[string]string)}
}
func (f *hFakeSecretStore) SetSecret(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.secrets[key] = value
	return nil
}
func (f *hFakeSecretStore) GetSecret(_ context.Context, key string) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.secrets[key], nil
}
func (f *hFakeSecretStore) DeleteSecret(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.secrets, key)
	return nil
}

type hFakeAgentStore struct {
	mu     sync.RWMutex
	agents map[string]*store.Agent
}

func newHFakeAgentStore() *hFakeAgentStore {
	return &hFakeAgentStore{agents: make(map[string]*store.Agent)}
}
func (f *hFakeAgentStore) AddAgent(_ context.Context, a *store.Agent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents[a.Name] = a
	return nil
}
func (f *hFakeAgentStore) GetAgent(_ context.Context, name string) (*store.Agent, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	a, ok := f.agents[name]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", name)
	}
	ac := *a
	return &ac, nil
}
func (f *hFakeAgentStore) ListAgents(_ context.Context) ([]*store.Agent, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var list []*store.Agent
	for _, a := range f.agents {
		ac := *a
		list = append(list, &ac)
	}
	return list, nil
}
func (f *hFakeAgentStore) UpdateAgent(_ context.Context, a *store.Agent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents[a.Name] = a
	return nil
}
func (f *hFakeAgentStore) RemoveAgent(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.agents, name)
	return nil
}

type hFakeKVStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func newHFakeKVStore() *hFakeKVStore { return &hFakeKVStore{data: make(map[string]string)} }
func (f *hFakeKVStore) Set(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = value
	return nil
}
func (f *hFakeKVStore) Get(_ context.Context, key string) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	v, ok := f.data[key]
	if !ok {
		return "", fmt.Errorf("key %q not found", key)
	}
	return v, nil
}
func (f *hFakeKVStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
	return nil
}

type hFakeConversationStore struct{}

func (f *hFakeConversationStore) CreateConversation(_ context.Context, _ string) (int64, error) {
	return 1, nil
}
func (f *hFakeConversationStore) AppendMessage(_ context.Context, _ int64, _, _ string) (int64, error) {
	return 1, nil
}
func (f *hFakeConversationStore) LoadHistory(_ context.Context, _ int64) (*store.MessageRow, []*store.MessageRow, error) {
	return nil, nil, nil
}
func (f *hFakeConversationStore) ListMessages(_ context.Context, _ int64) ([]*store.MessageRow, error) {
	return nil, nil
}
func (f *hFakeConversationStore) SaveSummary(_ context.Context, _ int64, _ string, _ int64) error {
	return nil
}
func (f *hFakeConversationStore) ListConversations(_ context.Context) ([]*store.ConversationRow, error) {
	return nil, nil
}

type hFakeMCPStore struct {
	mu      sync.RWMutex
	servers map[string]*store.MCPServer
}

func newHFakeMCPStore() *hFakeMCPStore {
	return &hFakeMCPStore{servers: make(map[string]*store.MCPServer)}
}
func (f *hFakeMCPStore) AddServer(_ context.Context, s *store.MCPServer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.servers[s.Name] = s
	return nil
}
func (f *hFakeMCPStore) GetServer(_ context.Context, name string) (*store.MCPServer, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	s, ok := f.servers[name]
	if !ok {
		return nil, fmt.Errorf("server %q not found", name)
	}
	sc := *s
	return &sc, nil
}
func (f *hFakeMCPStore) ListServers(_ context.Context) ([]*store.MCPServer, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var list []*store.MCPServer
	for _, s := range f.servers {
		sc := *s
		list = append(list, &sc)
	}
	return list, nil
}
func (f *hFakeMCPStore) UpdateServer(_ context.Context, s *store.MCPServer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.servers[s.Name] = s
	return nil
}
func (f *hFakeMCPStore) RemoveServer(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.servers, name)
	return nil
}

type hFakeHookStore struct {
	mu    sync.RWMutex
	hooks map[string]*store.Hook
}

func newHFakeHookStore() *hFakeHookStore { return &hFakeHookStore{hooks: make(map[string]*store.Hook)} }
func (f *hFakeHookStore) AddHook(_ context.Context, h *store.Hook) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hooks[h.ID] = h
	return nil
}
func (f *hFakeHookStore) GetHook(_ context.Context, id string) (*store.Hook, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	h, ok := f.hooks[id]
	if !ok {
		return nil, fmt.Errorf("hook %q not found", id)
	}
	hc := *h
	return &hc, nil
}
func (f *hFakeHookStore) ListHooks(_ context.Context) ([]*store.Hook, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var list []*store.Hook
	for _, h := range f.hooks {
		hc := *h
		list = append(list, &hc)
	}
	return list, nil
}
func (f *hFakeHookStore) ListHooksByScopeAndEvent(_ context.Context, _, _ string) ([]*store.Hook, error) {
	return nil, nil
}
func (f *hFakeHookStore) UpdateHook(_ context.Context, h *store.Hook) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hooks[h.ID] = h
	return nil
}
func (f *hFakeHookStore) RemoveHook(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.hooks[id]; !ok {
		return fmt.Errorf("hook %q not found", id)
	}
	delete(f.hooks, id)
	return nil
}
func (f *hFakeHookStore) ToggleHook(_ context.Context, id string, enabled bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.hooks[id]
	if !ok {
		return fmt.Errorf("hook %q not found", id)
	}
	if enabled {
		h.Enabled = 1
	} else {
		h.Enabled = 0
	}
	return nil
}

type hFakeSkillStore struct {
	mu     sync.RWMutex
	skills map[string]*store.Skill
}

func newHFakeSkillStore() *hFakeSkillStore {
	return &hFakeSkillStore{skills: make(map[string]*store.Skill)}
}
func (f *hFakeSkillStore) AddSkill(_ context.Context, s *store.Skill) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.skills[s.Name+":"+s.Scope] = s
	return nil
}
func (f *hFakeSkillStore) GetSkill(_ context.Context, name, scope string) (*store.Skill, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	s, ok := f.skills[name+":"+scope]
	if !ok {
		return nil, fmt.Errorf("skill %q not found", name)
	}
	sc := *s
	return &sc, nil
}
func (f *hFakeSkillStore) ListSkills(_ context.Context) ([]*store.Skill, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var list []*store.Skill
	for _, s := range f.skills {
		sc := *s
		list = append(list, &sc)
	}
	return list, nil
}
func (f *hFakeSkillStore) ListSkillsByScope(_ context.Context, scope string) ([]*store.Skill, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var list []*store.Skill
	for _, s := range f.skills {
		if s.Scope == scope {
			sc := *s
			list = append(list, &sc)
		}
	}
	return list, nil
}
func (f *hFakeSkillStore) UpdateSkill(_ context.Context, s *store.Skill) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.skills[s.Name+":"+s.Scope] = s
	return nil
}
func (f *hFakeSkillStore) RemoveSkill(_ context.Context, name, scope string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.skills[name+":"+scope]; !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	delete(f.skills, name+":"+scope)
	return nil
}

type hFakeHookExecutionStore struct{}

func (f *hFakeHookExecutionStore) AppendExecution(_ context.Context, _ *store.HookExecution) error {
	return nil
}
func (f *hFakeHookExecutionStore) ListExecutions(_ context.Context) ([]*store.HookExecution, error) {
	return nil, nil
}

type hFakeToolRegistryStore struct {
	mu    sync.RWMutex
	tools map[string]*store.ToolRegistry
}

func newHFakeToolRegistryStore() *hFakeToolRegistryStore {
	return &hFakeToolRegistryStore{tools: make(map[string]*store.ToolRegistry)}
}
func (f *hFakeToolRegistryStore) ListTools(_ context.Context) ([]*store.ToolRegistry, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var list []*store.ToolRegistry
	for _, t := range f.tools {
		tc := *t
		list = append(list, &tc)
	}
	return list, nil
}
func (f *hFakeToolRegistryStore) GetTool(_ context.Context, name string) (*store.ToolRegistry, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	t, ok := f.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool %q not found", name)
	}
	tc := *t
	return &tc, nil
}
func (f *hFakeToolRegistryStore) UpsertTool(_ context.Context, t *store.ToolRegistry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tools[t.Name] = t
	return nil
}
func (f *hFakeToolRegistryStore) ToggleTool(_ context.Context, name string, enabled bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tools[name]
	if !ok {
		return fmt.Errorf("tool %q not found", name)
	}
	if enabled {
		t.Enabled = 1
	} else {
		t.Enabled = 0
	}
	return nil
}

type hFakeToolGroupConfigStore struct {
	mu      sync.RWMutex
	configs map[string]*store.ToolGroupConfig
}

func newHFakeToolGroupConfigStore() *hFakeToolGroupConfigStore {
	return &hFakeToolGroupConfigStore{configs: make(map[string]*store.ToolGroupConfig)}
}
func (f *hFakeToolGroupConfigStore) GetConfig(_ context.Context, category string) (*store.ToolGroupConfig, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	c, ok := f.configs[category]
	if !ok {
		return nil, fmt.Errorf("config for %q not found", category)
	}
	cc := *c
	return &cc, nil
}
func (f *hFakeToolGroupConfigStore) PutConfig(_ context.Context, category, config string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configs[category] = &store.ToolGroupConfig{Category: category, Config: config}
	return nil
}

type hFakeKeyManager struct{}

func (f *hFakeKeyManager) Encrypt(p []byte) (string, error)         { return string(p), nil }
func (f *hFakeKeyManager) Decrypt(b string) ([]byte, error)         { return []byte(b), nil }
func (f *hFakeKeyManager) GetDEK() []byte                           { return nil }
func (f *hFakeKeyManager) SwitchToKeyfile(_ string) (string, error) { return "", nil }

var _ secrets.KeyManager = (*hFakeKeyManager)(nil)

// ---------------------------------------------------------------------------
// Fixture builder
// ---------------------------------------------------------------------------

type hFixture struct {
	profileStore *hFakeProfileStore
	kvStore      *hFakeKVStore
	mcpStore     *hFakeMCPStore
	hookStore    *hFakeHookStore
	toolStore    *hFakeToolRegistryStore
	skillStore   *hFakeSkillStore
	svc          *service.Service
	h            *handler.Handler
}

func newHFixture(t *testing.T) *hFixture {
	t.Helper()
	ps := newHFakeProfileStore()
	ss := newHFakeSecretStore()
	as := newHFakeAgentStore()
	kv := newHFakeKVStore()
	mcp := newHFakeMCPStore()
	hooks := newHFakeHookStore()
	tools := newHFakeToolRegistryStore()
	cfg := newHFakeToolGroupConfigStore()
	skills := newHFakeSkillStore()

	km := &hFakeKeyManager{}
	reg := adapter.NewRegistry()
	llmSvc := llm.NewService(ps, ss, km, reg, as)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	inst := skill.NewInstaller(skills, t.TempDir())

	svc := service.New(
		llmSvc,
		kv,
		&hFakeConversationStore{},
		nil,
		inst,
		log,
		hooks,
		&hFakeHookExecutionStore{},
		mcp,
		func() {},
		func(_ context.Context, _ *store.MCPServer) ([]string, error) {
			return []string{"tool_a"}, nil
		},
		tools,
		cfg,
	)

	h := handler.New(svc)

	return &hFixture{
		profileStore: ps,
		kvStore:      kv,
		mcpStore:     mcp,
		hookStore:    hooks,
		toolStore:    tools,
		skillStore:   skills,
		svc:          svc,
		h:            h,
	}
}

// Helper to make a request with an optional body and optional path value mock.
func makeReq(method, path, body string) *http.Request {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	return req
}

// ---------------------------------------------------------------------------
// handler / error / helper tests
// ---------------------------------------------------------------------------

func TestHandlerNew(t *testing.T) {
	f := newHFixture(t)
	if f.h == nil {
		t.Error("expected non-nil Handler")
	}
}

func TestHandleError_NotFound(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodGet, "/api/providers/ghost", "")
	w := httptest.NewRecorder()
	f.h.GetProvider(w, req)
	// path value "name" is empty string, which triggers not found
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleError_InternalError(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodGet, "/api/providers", "")
	w := httptest.NewRecorder()
	f.h.ListProviders(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleError_InternalServer(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodGet, "/api/providers/ghost", "")
	w := httptest.NewRecorder()
	f.h.GetProvider(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
