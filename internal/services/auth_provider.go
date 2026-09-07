package services

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Identity represents the authenticated identity returned by an auth provider.
type Identity struct {
	Email        string         `json:"email"`
	Name         string         `json:"name"`
	AvatarURL    string         `json:"avatar_url,omitempty"`
	ProviderData map[string]any `json:"provider_data,omitempty"`
}

// Provider defines the interface for authentication providers (password, future SSO/OAuth).
type Provider interface {
	Name() string
	Authenticate(ctx context.Context, credentials map[string]string) (*Identity, error)
	Begin(ctx context.Context, params map[string]string) (startURL string, state string, err error)
	Complete(ctx context.Context, state string, params map[string]string) (*Identity, error)
	ProvisionsUsers() bool
}

// registry manages registered authentication providers.
type registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry creates a new empty provider registry.
func NewRegistry() *registry {
	return &registry{
		providers: make(map[string]Provider),
	}
}

// Register registers an authentication provider. It panics if p is nil or if a provider with the same name is already registered.
func (r *registry) Register(p Provider) {
	if p == nil {
		panic("cannot register nil auth provider")
	}
	name := p.Name()
	if name == "" {
		panic("cannot register auth provider with empty name")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[name]; exists {
		panic(fmt.Sprintf("auth provider %q already registered", name))
	}
	r.providers[name] = p
}

// Get retrieves a registered provider by name. Returns domain.ErrInvalid if the provider is not registered.
func (r *registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, exists := r.providers[name]
	if !exists {
		return nil, fmt.Errorf("%w: unknown auth provider %q", domain.ErrInvalid, name)
	}
	return p, nil
}

// List returns a sorted list of registered provider names.
func (r *registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
