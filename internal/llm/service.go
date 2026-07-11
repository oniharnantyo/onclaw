package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cloudwego/eino/components/model"

	"github.com/oniharnantyo/onclaw/internal/llm/adapter"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ErrSecretNotSet is returned by ResolveSecret when no API key is found for a
// provider profile (neither in the environment nor in the secret store).
// Keyless providers (see adapter.IsKeyless) tolerate this; all others must
// treat it as a hard error.
var ErrSecretNotSet = secrets.ErrSecretNotSet

// Service composes storage, key management, and adapter registry facades.
type Service struct {
	profileStore    store.ProfileStore
	secretStore     store.SecretStore
	keyManager      secrets.KeyManager
	adapterRegistry adapter.Registry
	agentStore      store.AgentStore

	mu            sync.RWMutex
	profiles      map[string]*store.Profile
	agents        map[string]*store.Agent
	secrets       map[string]string // profileName -> decrypted api_key
	reloadPending atomic.Bool
}

// NewService constructs a new thin Service facade instance.
func NewService(ps store.ProfileStore, ss store.SecretStore, km secrets.KeyManager, ar adapter.Registry, as store.AgentStore) *Service {
	s := &Service{
		profileStore:    ps,
		secretStore:     ss,
		keyManager:      km,
		adapterRegistry: ar,
		agentStore:      as,
	}
	s.reloadPending.Store(true)
	return s
}

// KeyManager returns the active KeyManager instance.
func (s *Service) KeyManager() secrets.KeyManager {
	return s.keyManager
}

// TriggerReload flags that database updates occurred and cache should be reloaded.
func (s *Service) TriggerReload() {
	s.reloadPending.Store(true)
}

// Reload reads profiles and decrypts secrets to update the in-memory cache.
func (s *Service) Reload(ctx context.Context) error {
	profiles, err := s.profileStore.ListProfiles(ctx)
	if err != nil {
		return err
	}

	profilesMap := make(map[string]*store.Profile)
	secretsMap := make(map[string]string)

	for _, p := range profiles {
		profilesMap[p.Name] = p
		encKey := fmt.Sprintf("provider:%s", p.Name)
		encryptedBlob, err := s.secretStore.GetSecret(ctx, encKey)
		if err != nil {
			return err
		}
		if encryptedBlob != "" {
			decryptedBytes, err := s.keyManager.Decrypt(encryptedBlob)
			if err != nil {
				return err
			}
			secretsMap[p.Name] = string(decryptedBytes)
		}
	}

	agents, err := s.agentStore.ListAgents(ctx)
	if err != nil {
		return err
	}

	agentsMap := make(map[string]*store.Agent)
	for _, a := range agents {
		agentsMap[a.Name] = a
	}

	s.mu.Lock()
	s.profiles = profilesMap
	s.agents = agentsMap
	s.secrets = secretsMap
	s.mu.Unlock()

	return nil
}

// ReloadIfNeeded reloads cache if flagged as dirty.
func (s *Service) ReloadIfNeeded(ctx context.Context) error {
	if s.reloadPending.CompareAndSwap(true, false) {
		if err := s.Reload(ctx); err != nil {
			s.reloadPending.Store(true)
			return err
		}
	}
	return nil
}

// AddProfile inserts a new profile and flags for reload.
func (s *Service) AddProfile(ctx context.Context, p *store.Profile) error {
	if err := s.profileStore.AddProfile(ctx, p); err != nil {
		return err
	}
	s.TriggerReload()
	return nil
}

// GetProfile retrieves a cached profile.
func (s *Service) GetProfile(ctx context.Context, name string) (*store.Profile, error) {
	if err := s.ReloadIfNeeded(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.profiles[name]
	if !ok {
		return nil, fmt.Errorf("profile %q not found", name)
	}
	pCopy := *p
	return &pCopy, nil
}

// ListProfiles retrieves all cached profiles sorted by name.
func (s *Service) ListProfiles(ctx context.Context) ([]*store.Profile, error) {
	if err := s.ReloadIfNeeded(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*store.Profile, 0, len(s.profiles))
	for _, p := range s.profiles {
		pCopy := *p
		list = append(list, &pCopy)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list, nil
}

// RemoveProfile deletes a profile and its secret, then flags for reload.
func (s *Service) RemoveProfile(ctx context.Context, name string) error {
	if err := s.profileStore.RemoveProfile(ctx, name); err != nil {
		return err
	}
	encKey := fmt.Sprintf("provider:%s", name)
	_ = s.secretStore.DeleteSecret(ctx, encKey)
	s.TriggerReload()
	return nil
}

// SetSecret encrypts and stores the api key for a profile.
func (s *Service) SetSecret(ctx context.Context, name string, apiKey string) error {
	_, err := s.GetProfile(ctx, name)
	if err != nil {
		return err
	}

	encrypted, err := s.keyManager.Encrypt([]byte(apiKey))
	if err != nil {
		return err
	}

	encKey := fmt.Sprintf("provider:%s", name)
	if err := s.secretStore.SetSecret(ctx, encKey, encrypted); err != nil {
		return err
	}
	s.TriggerReload()
	return nil
}

// GetSecret retrieves a cached decrypted secret.
func (s *Service) GetSecret(ctx context.Context, name string) (string, error) {
	if err := s.ReloadIfNeeded(ctx); err != nil {
		return "", err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.secrets[name]
	if !ok {
		return "", nil
	}
	return val, nil
}

// Resolve resolves a secret by environment variable or key store.
func (s *Service) Resolve(ctx context.Context, envVar, secretKey string) (string, error) {
	if val := os.Getenv(envVar); val != "" {
		return val, nil
	}

	if err := s.ReloadIfNeeded(ctx); err != nil {
		return "", err
	}

	s.mu.RLock()
	val, ok := s.secrets[secretKey]
	s.mu.RUnlock()
	if ok && val != "" {
		return val, nil
	}

	// Try direct fetch from the SecretStore if not in cache (e.g. web.* secrets)
	encryptedBlob, err := s.secretStore.GetSecret(ctx, secretKey)
	if err != nil {
		return "", err
	}
	if encryptedBlob != "" {
		decryptedBytes, err := s.keyManager.Decrypt(encryptedBlob)
		if err != nil {
			return "", err
		}
		return string(decryptedBytes), nil
	}

	return "", fmt.Errorf("secret not found: %w", ErrSecretNotSet)
}

// ResolveSecret resolves provider secrets by Env > DB > error.
func (s *Service) ResolveSecret(ctx context.Context, name string) (string, error) {
	envKey := fmt.Sprintf("ONCLAW_PROVIDER_%s_API_KEY", strings.ReplaceAll(strings.ReplaceAll(strings.ToUpper(name), "-", "_"), ".", "_"))
	val, err := s.Resolve(ctx, envKey, name)
	if err != nil {
		if errors.Is(err, secrets.ErrSecretNotSet) {
			return "", fmt.Errorf("API key for provider %s is not set. Run 'onclaw provider login %s' or set %s: %w", name, name, envKey, ErrSecretNotSet)
		}
		return "", err
	}
	return val, nil
}

// resolveAPIKey resolves the API key for a profile. Keyless providers (e.g.
// local Ollama) tolerate a missing key; for all others a missing key is fatal
// and the resolution error is returned unchanged.
func (s *Service) resolveAPIKey(ctx context.Context, name, providerType string) (string, error) {
	apiKey, err := s.ResolveSecret(ctx, name)
	if err != nil {
		if !(errors.Is(err, ErrSecretNotSet) && adapter.IsKeyless(providerType)) {
			return "", err
		}
	}
	return apiKey, nil
}

// BuildWithProfile builds a model.AgenticModel using the given profile and model (handling secret resolution).
func (s *Service) BuildWithProfile(ctx context.Context, p *store.Profile, modelName string) (model.AgenticModel, error) {
	if p.Enabled == 0 {
		return nil, fmt.Errorf("provider %s is disabled", p.Name)
	}

	apiKey, err := s.resolveAPIKey(ctx, p.Name, p.ProviderType)
	if err != nil {
		return nil, err
	}

	adapter, err := s.adapterRegistry.Get(p.ProviderType)
	if err != nil {
		return nil, err
	}

	return adapter.Build(ctx, p, modelName, apiKey)
}

// AddAgent inserts a new agent and flags for reload.
func (s *Service) AddAgent(ctx context.Context, a *store.Agent) error {
	if err := s.agentStore.AddAgent(ctx, a); err != nil {
		return err
	}
	s.TriggerReload()
	return nil
}

// UpdateAgent updates an existing agent and flags for reload.
func (s *Service) UpdateAgent(ctx context.Context, a *store.Agent) error {
	if err := s.agentStore.UpdateAgent(ctx, a); err != nil {
		return err
	}
	s.TriggerReload()
	return nil
}

// UpdateAgentTools updates tools configuration for an agent and flags for reload.
func (s *Service) UpdateAgentTools(ctx context.Context, name string, tools string) error {
	if err := s.agentStore.UpdateAgentTools(ctx, name, tools); err != nil {
		return err
	}
	s.TriggerReload()
	return nil
}

// GetAgent retrieves a cached agent.
func (s *Service) GetAgent(ctx context.Context, name string) (*store.Agent, error) {
	if err := s.ReloadIfNeeded(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.agents[name]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", name)
	}
	aCopy := *a
	return &aCopy, nil
}

// ListAgents retrieves all cached agents sorted by name.
func (s *Service) ListAgents(ctx context.Context) ([]*store.Agent, error) {
	if err := s.ReloadIfNeeded(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*store.Agent, 0, len(s.agents))
	for _, a := range s.agents {
		aCopy := *a
		list = append(list, &aCopy)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list, nil
}

// RemoveAgent deletes an agent and flags for reload.
func (s *Service) RemoveAgent(ctx context.Context, name string) error {
	if err := s.agentStore.RemoveAgent(ctx, name); err != nil {
		return err
	}
	s.TriggerReload()
	return nil
}

// ResolveAgentProfile resolves the effective provider profile for an agent.
func (s *Service) ResolveAgentProfile(ctx context.Context, agentName string) (*store.Profile, error) {
	agent, err := s.GetAgent(ctx, agentName)
	if err != nil {
		return nil, err
	}

	p, err := s.GetProfile(ctx, agent.Provider)
	if err != nil {
		return nil, fmt.Errorf("agent %q referenced provider %q not found: %w", agentName, agent.Provider, err)
	}

	if p.Enabled == 0 {
		return nil, fmt.Errorf("agent %q referenced provider %q is disabled", agentName, agent.Provider)
	}

	// Copy the profile to avoid modifying the cached one
	effProfile := *p

	// Parse existing Settings JSON or initialize empty
	var settings map[string]interface{}
	if effProfile.Settings != "" {
		if err := json.Unmarshal([]byte(effProfile.Settings), &settings); err != nil {
			// If invalid JSON, initialize empty map to override
			settings = make(map[string]interface{})
		}
	} else {
		settings = make(map[string]interface{})
	}

	if agent.ReasoningEffort != "" {
		settings["reasoning_effort"] = agent.ReasoningEffort
	}
	if agent.ReasoningBudgetTokens > 0 {
		settings["reasoning_budget_tokens"] = agent.ReasoningBudgetTokens
	}

	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal settings for agent profile: %w", err)
	}
	effProfile.Settings = string(settingsJSON)

	return &effProfile, nil
}

// BuildAgent loads the agent, resolves its effective profile, gets provider key, and constructs the ToolCallingChatModel.
func (s *Service) BuildAgent(ctx context.Context, agentName string) (model.AgenticModel, error) {
	agent, err := s.GetAgent(ctx, agentName)
	if err != nil {
		return nil, err
	}

	effProfile, err := s.ResolveAgentProfile(ctx, agentName)
	if err != nil {
		return nil, err
	}

	apiKey, err := s.resolveAPIKey(ctx, effProfile.Name, effProfile.ProviderType)
	if err != nil {
		return nil, err
	}

	adapter, err := s.adapterRegistry.Get(effProfile.ProviderType)
	if err != nil {
		return nil, err
	}

	return adapter.Build(ctx, effProfile, agent.Model, apiKey)
}
