package store

import "context"

// ProviderStore manages model provider configurations.
type ProviderStore interface {
	// Create stores a new provider, assigning ID and stamping timestamps
	// when empty. A duplicate provider name is an error.
	Create(ctx context.Context, provider *Provider) error
	// ByID returns the provider with the given ID; ErrNotFound when absent.
	ByID(ctx context.Context, id string) (*Provider, error)
	// List returns all providers, oldest first. The result is never nil.
	List(ctx context.Context) ([]Provider, error)
	// Update rewrites an existing provider; ErrNotFound when absent.
	Update(ctx context.Context, provider *Provider) error
	// Delete removes a provider; ErrNotFound when absent.
	Delete(ctx context.Context, id string) error
}
