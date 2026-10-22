package store

import "context"

// ProviderModelStore manages stored model rows for providers (design D2).
type ProviderModelStore interface {
	// Create stores a new model for a provider. ID is store-assigned;
	// timestamps are stamped when zero. A duplicate (provider_id, model_id)
	// returns ErrDuplicate.
	Create(ctx context.Context, model *ProviderModel) error

	// ByID returns the model row with the given ID; ErrNotFound when absent.
	ByID(ctx context.Context, id int64) (*ProviderModel, error)

	// ByProvider returns all model rows for providerID, ordered by id ASC.
	// The result is never nil.
	ByProvider(ctx context.Context, providerID string) ([]*ProviderModel, error)

	// Update rewrites an existing model row; ErrNotFound when absent.
	// A duplicate (provider_id, model_id) returns ErrDuplicate.
	Update(ctx context.Context, model *ProviderModel) error

	// Delete removes a model row by ID; ErrNotFound when absent.
	Delete(ctx context.Context, id int64) error

	// DeleteByProvider removes all model rows for providerID. An absent provider
	// or no rows is a safe no-op returning nil.
	DeleteByProvider(ctx context.Context, providerID string) error

	// UpsertMissing inserts models that do not already exist for their provider
	// (matching on (provider_id, model_id)). Existing rows are left untouched.
	// Returns the number of newly inserted rows.
	UpsertMissing(ctx context.Context, models []*ProviderModel) (inserted int, err error)
}
