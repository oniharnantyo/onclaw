package fake

import (
	"context"
	"fmt"
	"sort"

	"github.com/oniharnantyo/buddies/internal/store"
)

type providerStore struct{ s *Store }

func (p providerStore) Create(_ context.Context, provider *store.Provider) error {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	if provider.ID == "" {
		provider.ID = nextID()
	}
	if _, exists := p.s.providers[provider.ID]; exists {
		return fmt.Errorf("provider %s already exists", provider.ID)
	}
	stamp(&provider.CreatedAt, &provider.UpdatedAt)
	p.s.providers[provider.ID] = *provider
	return nil
}

func (p providerStore) ByID(_ context.Context, id string) (*store.Provider, error) {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	provider, ok := p.s.providers[id]
	if !ok {
		return nil, fmt.Errorf("provider %s: %w", id, store.ErrNotFound)
	}
	out := provider
	return &out, nil
}

func (p providerStore) List(_ context.Context) ([]store.Provider, error) {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	out := make([]store.Provider, 0, len(p.s.providers))
	for _, provider := range p.s.providers {
		out = append(out, provider)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (p providerStore) Update(_ context.Context, provider *store.Provider) error {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	stored, ok := p.s.providers[provider.ID]
	if !ok {
		return fmt.Errorf("provider %s: %w", provider.ID, store.ErrNotFound)
	}
	provider.CreatedAt = stored.CreatedAt
	provider.UpdatedAt = nowUTC()
	p.s.providers[provider.ID] = *provider
	return nil
}

func (p providerStore) Delete(_ context.Context, id string) error {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	if _, ok := p.s.providers[id]; !ok {
		return fmt.Errorf("provider %s: %w", id, store.ErrNotFound)
	}
	delete(p.s.providers, id)
	// Mirrors the sqlite FK cascade: deleting the provider removes its models.
	p.s.deleteProviderModelsLocked(id)
	return nil
}
