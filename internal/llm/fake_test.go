package llm_test

import (
	"context"
	"fmt"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/store"
)

type fakeProfileStore struct {
	mu       sync.RWMutex
	profiles map[string]*store.Profile
}

func newFakeProfileStore() *fakeProfileStore {
	return &fakeProfileStore{profiles: make(map[string]*store.Profile)}
}

func (f *fakeProfileStore) AddProfile(ctx context.Context, p *store.Profile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.profiles[p.Name] = p
	return nil
}

func (f *fakeProfileStore) GetProfile(ctx context.Context, name string) (*store.Profile, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	p, ok := f.profiles[name]
	if !ok {
		return nil, fmt.Errorf("profile %q not found", name)
	}
	return p, nil
}

func (f *fakeProfileStore) ListProfiles(ctx context.Context) ([]*store.Profile, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var list []*store.Profile
	for _, p := range f.profiles {
		list = append(list, p)
	}
	return list, nil
}

func (f *fakeProfileStore) RemoveProfile(ctx context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.profiles, name)
	return nil
}

type fakeSecretStore struct {
	mu      sync.RWMutex
	secrets map[string]string
}

func newFakeSecretStore() *fakeSecretStore {
	return &fakeSecretStore{secrets: make(map[string]string)}
}

func (f *fakeSecretStore) SetSecret(ctx context.Context, key string, encryptedValue string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.secrets[key] = encryptedValue
	return nil
}

func (f *fakeSecretStore) GetSecret(ctx context.Context, key string) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.secrets[key], nil
}

func (f *fakeSecretStore) DeleteSecret(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.secrets, key)
	return nil
}

type fakeAgentStore struct {
	mu     sync.RWMutex
	agents map[string]*store.Agent
}

func newFakeAgentStore() *fakeAgentStore {
	return &fakeAgentStore{agents: make(map[string]*store.Agent)}
}

func (f *fakeAgentStore) AddAgent(ctx context.Context, a *store.Agent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents[a.Name] = a
	return nil
}

func (f *fakeAgentStore) GetAgent(ctx context.Context, name string) (*store.Agent, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	a, ok := f.agents[name]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", name)
	}
	return a, nil
}

func (f *fakeAgentStore) ListAgents(ctx context.Context) ([]*store.Agent, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var list []*store.Agent
	for _, a := range f.agents {
		list = append(list, a)
	}
	return list, nil
}

func (f *fakeAgentStore) RemoveAgent(ctx context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.agents, name)
	return nil
}

func (f *fakeAgentStore) UpdateAgent(ctx context.Context, a *store.Agent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents[a.Name] = a
	return nil
}

func (f *fakeAgentStore) UpdateAgentTools(ctx context.Context, name string, tools string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.agents[name]
	if !ok {
		return fmt.Errorf("agent not found")
	}
	a.Tools = tools
	return nil
}
