package service_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/llm/adapter"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// fakeProfileStore satisfies store.ProfileStore.
type fakeProfileStore struct {
	mu       sync.RWMutex
	profiles map[string]*store.Profile
}

func newFakeProfileStore() *fakeProfileStore {
	return &fakeProfileStore{profiles: make(map[string]*store.Profile)}
}

func (f *fakeProfileStore) AddProfile(_ context.Context, p *store.Profile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.profiles[p.Name] = p
	return nil
}

func (f *fakeProfileStore) GetProfile(_ context.Context, name string) (*store.Profile, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	p, ok := f.profiles[name]
	if !ok {
		return nil, fmt.Errorf("profile %q not found", name)
	}
	pCopy := *p
	return &pCopy, nil
}

func (f *fakeProfileStore) ListProfiles(_ context.Context) ([]*store.Profile, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var list []*store.Profile
	for _, p := range f.profiles {
		pCopy := *p
		list = append(list, &pCopy)
	}
	return list, nil
}

func (f *fakeProfileStore) RemoveProfile(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.profiles, name)
	return nil
}

// fakeSecretStore satisfies store.SecretStore.
type fakeSecretStore struct {
	mu      sync.RWMutex
	secrets map[string]string
}

func newFakeSecretStore() *fakeSecretStore {
	return &fakeSecretStore{secrets: make(map[string]string)}
}

func (f *fakeSecretStore) SetSecret(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.secrets[key] = value
	return nil
}

func (f *fakeSecretStore) GetSecret(_ context.Context, key string) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.secrets[key], nil
}

func (f *fakeSecretStore) DeleteSecret(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.secrets, key)
	return nil
}

// fakeAgentStore satisfies store.AgentStore.
type fakeAgentStore struct {
	mu     sync.RWMutex
	agents map[string]*store.Agent
}

func newFakeAgentStore() *fakeAgentStore {
	return &fakeAgentStore{agents: make(map[string]*store.Agent)}
}

func (f *fakeAgentStore) AddAgent(_ context.Context, a *store.Agent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents[a.Name] = a
	return nil
}

func (f *fakeAgentStore) GetAgent(_ context.Context, name string) (*store.Agent, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	a, ok := f.agents[name]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", name)
	}
	aCopy := *a
	return &aCopy, nil
}

func (f *fakeAgentStore) ListAgents(_ context.Context) ([]*store.Agent, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var list []*store.Agent
	for _, a := range f.agents {
		aCopy := *a
		list = append(list, &aCopy)
	}
	return list, nil
}

func (f *fakeAgentStore) UpdateAgent(_ context.Context, a *store.Agent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents[a.Name] = a
	return nil
}

func (f *fakeAgentStore) RemoveAgent(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.agents, name)
	return nil
}

// fakeKVStore satisfies store.KVStore.
type fakeKVStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func newFakeKVStore() *fakeKVStore {
	return &fakeKVStore{data: make(map[string]string)}
}

func (f *fakeKVStore) Set(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = value
	return nil
}

func (f *fakeKVStore) Get(_ context.Context, key string) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	v, ok := f.data[key]
	if !ok {
		return "", fmt.Errorf("key %q not found", key)
	}
	return v, nil
}

func (f *fakeKVStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
	return nil
}

// fakeConversationStore satisfies store.ConversationStore.
type fakeConversationStore struct{}

func (f *fakeConversationStore) CreateConversation(_ context.Context, _ string) (int64, error) {
	return 1, nil
}
func (f *fakeConversationStore) AppendMessage(_ context.Context, _ int64, _, _ string) (int64, error) {
	return 1, nil
}
func (f *fakeConversationStore) LoadHistory(_ context.Context, _ int64) (*store.MessageRow, []*store.MessageRow, error) {
	return nil, nil, nil
}
func (f *fakeConversationStore) ListMessages(_ context.Context, _ int64) ([]*store.MessageRow, error) {
	return nil, nil
}
func (f *fakeConversationStore) SaveSummary(_ context.Context, _ int64, _ string, _ int64) error {
	return nil
}
func (f *fakeConversationStore) ListConversations(_ context.Context) ([]*store.ConversationRow, error) {
	return nil, nil
}

// fakeMCPStore satisfies store.MCPServerStore.
type fakeMCPStore struct {
	mu      sync.RWMutex
	servers map[string]*store.MCPServer
}

func newFakeMCPStore() *fakeMCPStore {
	return &fakeMCPStore{servers: make(map[string]*store.MCPServer)}
}

func (f *fakeMCPStore) AddServer(_ context.Context, s *store.MCPServer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.servers[s.Name] = s
	return nil
}

func (f *fakeMCPStore) GetServer(_ context.Context, name string) (*store.MCPServer, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	s, ok := f.servers[name]
	if !ok {
		return nil, fmt.Errorf("server %q not found", name)
	}
	sCopy := *s
	return &sCopy, nil
}

func (f *fakeMCPStore) ListServers(_ context.Context) ([]*store.MCPServer, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var list []*store.MCPServer
	for _, s := range f.servers {
		sCopy := *s
		list = append(list, &sCopy)
	}
	return list, nil
}

func (f *fakeMCPStore) UpdateServer(_ context.Context, s *store.MCPServer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.servers[s.Name] = s
	return nil
}

func (f *fakeMCPStore) RemoveServer(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.servers, name)
	return nil
}

// fakeHookStore satisfies store.HookStore.
type fakeHookStore struct {
	mu    sync.RWMutex
	hooks map[string]*store.Hook
}

func newFakeHookStore() *fakeHookStore {
	return &fakeHookStore{hooks: make(map[string]*store.Hook)}
}

func (f *fakeHookStore) AddHook(_ context.Context, h *store.Hook) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hooks[h.ID] = h
	return nil
}

func (f *fakeHookStore) GetHook(_ context.Context, id string) (*store.Hook, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	h, ok := f.hooks[id]
	if !ok {
		return nil, fmt.Errorf("hook %q not found", id)
	}
	hCopy := *h
	return &hCopy, nil
}

func (f *fakeHookStore) ListHooks(_ context.Context) ([]*store.Hook, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var list []*store.Hook
	for _, h := range f.hooks {
		hCopy := *h
		list = append(list, &hCopy)
	}
	return list, nil
}

func (f *fakeHookStore) ListHooksByScopeAndEvent(_ context.Context, _, _ string) ([]*store.Hook, error) {
	return nil, nil
}

func (f *fakeHookStore) UpdateHook(_ context.Context, h *store.Hook) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hooks[h.ID] = h
	return nil
}

func (f *fakeHookStore) RemoveHook(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.hooks, id)
	return nil
}

func (f *fakeHookStore) ToggleHook(_ context.Context, id string, enabled bool) error {
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

// fakeHookExecutionStore satisfies store.HookExecutionStore.
type fakeHookExecutionStore struct {
	mu   sync.RWMutex
	exes []*store.HookExecution
}

func newFakeHookExecutionStore() *fakeHookExecutionStore {
	return &fakeHookExecutionStore{}
}

func (f *fakeHookExecutionStore) AppendExecution(_ context.Context, e *store.HookExecution) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exes = append(f.exes, e)
	return nil
}

func (f *fakeHookExecutionStore) ListExecutions(_ context.Context) ([]*store.HookExecution, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.exes, nil
}

// fakeToolRegistryStore satisfies store.ToolRegistryStore.
type fakeToolRegistryStore struct {
	mu    sync.RWMutex
	tools map[string]*store.ToolRegistry
}

func newFakeToolRegistryStore() *fakeToolRegistryStore {
	return &fakeToolRegistryStore{tools: make(map[string]*store.ToolRegistry)}
}

func (f *fakeToolRegistryStore) ListTools(_ context.Context) ([]*store.ToolRegistry, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var list []*store.ToolRegistry
	for _, t := range f.tools {
		tCopy := *t
		list = append(list, &tCopy)
	}
	return list, nil
}

func (f *fakeToolRegistryStore) GetTool(_ context.Context, name string) (*store.ToolRegistry, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	t, ok := f.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool %q not found", name)
	}
	tCopy := *t
	return &tCopy, nil
}

func (f *fakeToolRegistryStore) UpsertTool(_ context.Context, t *store.ToolRegistry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tools[t.Name] = t
	return nil
}

func (f *fakeToolRegistryStore) ToggleTool(_ context.Context, name string, enabled bool) error {
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

// fakeToolGroupConfigStore satisfies store.ToolGroupConfigStore.
type fakeToolGroupConfigStore struct {
	mu      sync.RWMutex
	configs map[string]*store.ToolGroupConfig
}

func newFakeToolGroupConfigStore() *fakeToolGroupConfigStore {
	return &fakeToolGroupConfigStore{configs: make(map[string]*store.ToolGroupConfig)}
}

func (f *fakeToolGroupConfigStore) GetConfig(_ context.Context, category string) (*store.ToolGroupConfig, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	c, ok := f.configs[category]
	if !ok {
		return nil, fmt.Errorf("config for category %q not found", category)
	}
	cCopy := *c
	return &cCopy, nil
}

func (f *fakeToolGroupConfigStore) PutConfig(_ context.Context, category, config string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configs[category] = &store.ToolGroupConfig{Category: category, Config: config}
	return nil
}

// fakeKeyManager is a no-op KeyManager where encrypt == identity.
type fakeKeyManager struct{}

func (f *fakeKeyManager) Encrypt(plaintext []byte) (string, error) { return string(plaintext), nil }
func (f *fakeKeyManager) Decrypt(blob string) ([]byte, error)      { return []byte(blob), nil }
func (f *fakeKeyManager) GetDEK() []byte                           { return nil }
func (f *fakeKeyManager) SwitchToKeyfile(_ string) (string, error) { return "", nil }

// Ensure fakeKeyManager implements secrets.KeyManager.
var _ secrets.KeyManager = (*fakeKeyManager)(nil)

type fixture struct {
	profileStore *fakeProfileStore
	secretStore  *fakeSecretStore
	agentStore   *fakeAgentStore
	kvStore      *fakeKVStore
	mcpStore     *fakeMCPStore
	hookStore    *fakeHookStore
	execStore    *fakeHookExecutionStore
	toolStore    *fakeToolRegistryStore
	cfgStore     *fakeToolGroupConfigStore
	llmSvc       *llm.Service
	svc          *service.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	ps := newFakeProfileStore()
	ss := newFakeSecretStore()
	as := newFakeAgentStore()
	kv := newFakeKVStore()
	mcp := newFakeMCPStore()
	hooks := newFakeHookStore()
	execs := newFakeHookExecutionStore()
	tools := newFakeToolRegistryStore()
	cfg := newFakeToolGroupConfigStore()

	km := &fakeKeyManager{}
	reg := adapter.NewRegistry()

	llmSvc := llm.NewService(ps, ss, km, reg, as)

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	svc := service.New(
		llmSvc,
		kv,
		&fakeConversationStore{},
		nil,
		nil,
		log,
		hooks,
		execs,
		mcp,
		func() {},
		func(_ context.Context, _ *store.MCPServer) ([]string, error) {
			return []string{"tool_a", "tool_b"}, nil
		},
		tools,
		cfg,
	)

	return &fixture{
		profileStore: ps,
		secretStore:  ss,
		agentStore:   as,
		kvStore:      kv,
		mcpStore:     mcp,
		hookStore:    hooks,
		execStore:    execs,
		toolStore:    tools,
		cfgStore:     cfg,
		llmSvc:       llmSvc,
		svc:          svc,
	}
}
