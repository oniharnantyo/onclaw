package store

import "context"

// ProfileStore defines LLM provider profile operations.
type ProfileStore interface {
	AddProfile(ctx context.Context, p *Profile) error
	GetProfile(ctx context.Context, name string) (*Profile, error)
	ListProfiles(ctx context.Context) ([]*Profile, error)
	RemoveProfile(ctx context.Context, name string) error
}

// SecretStore defines opaque encrypted config key-value operations.
type SecretStore interface {
	SetSecret(ctx context.Context, key string, encryptedValue string) error
	GetSecret(ctx context.Context, key string) (string, error)
	DeleteSecret(ctx context.Context, key string) error
}

// KVStore defines application preference operations.
type KVStore interface {
	Set(ctx context.Context, key string, value string) error
	Get(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, key string) error
}

// AgentStore defines agent configuration operations.
type AgentStore interface {
	AddAgent(ctx context.Context, a *Agent) error
	GetAgent(ctx context.Context, name string) (*Agent, error)
	ListAgents(ctx context.Context) ([]*Agent, error)
	UpdateAgent(ctx context.Context, a *Agent) error
	RemoveAgent(ctx context.Context, name string) error
}
