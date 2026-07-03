package secrets

import (
	"context"
	"errors"
)

// ErrSecretNotSet is returned when a secret is not found.
var ErrSecretNotSet = errors.New("api key not set")

// SecretResolver defines an interface for resolving secrets by environment variable or key store.
type SecretResolver interface {
	Resolve(ctx context.Context, envVar, secretKey string) (string, error)
}
