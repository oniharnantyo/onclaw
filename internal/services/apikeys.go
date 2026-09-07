// Package services — API key domain: minting of workspace API keys for /v1
// (OpenResponses) authentication, hashing, display prefix/suffix derivation,
// and workspace-scoped management (list, revoke).
package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// APIKeyPrefix is the fixed prefix of every workspace API key plaintext.
const APIKeyPrefix = "oc_ws_"

// apiKeySecretBytes is the entropy of the random secret portion. 32 bytes
// base64url-encode to 43 characters, yielding plaintexts of 49 characters
// ("oc_ws_" + 43).
const apiKeySecretBytes = 32

// APIKeyService mints and manages workspace API keys. Plaintext keys are
// returned exactly once at creation and never stored — only their SHA-256
// hash plus display prefix/suffix persist.
type APIKeyService struct {
	keys store.WorkspaceAPIKeyStore
}

// NewAPIKeyService creates a new APIKeyService with the given granular store.
func NewAPIKeyService(keys store.WorkspaceAPIKeyStore) *APIKeyService {
	return &APIKeyService{keys: keys}
}

// MintAPIKey generates a fresh plaintext workspace API key:
// "oc_ws_" + 43 url-safe random characters.
func MintAPIKey() (string, error) {
	buf := make([]byte, apiKeySecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate api key secret: %w", err)
	}
	return APIKeyPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// DeriveAPIKeyDisplay returns the display prefix (at most the first 11
// characters: "oc_ws_" + 5 secret characters) and suffix (last 4 characters)
// of a plaintext key.
func DeriveAPIKeyDisplay(plaintext string) (prefix, suffix string) {
	if len(plaintext) <= 4 {
		return "", plaintext
	}
	prefix = plaintext
	if len(prefix) > 11 {
		prefix = prefix[:11]
	}
	suffix = plaintext[len(plaintext)-4:]
	return prefix, suffix
}

// Create mints a new workspace API key. It returns the plaintext exactly once
// alongside the stored entity (hash + display fields only). The name must be
// non-empty.
func (s *APIKeyService) Create(ctx context.Context, workspaceID, name, createdByUserID string) (string, *domain.WorkspaceAPIKey, error) {
	if workspaceID == "" {
		return "", nil, fmt.Errorf("%w: workspace id is required", domain.ErrInvalid)
	}
	if createdByUserID == "" {
		return "", nil, fmt.Errorf("%w: creator user id is required", domain.ErrInvalid)
	}
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return "", nil, fmt.Errorf("%w: name is required", domain.ErrInvalid)
	}

	plaintext, err := MintAPIKey()
	if err != nil {
		return "", nil, err
	}

	prefix, suffix := DeriveAPIKeyDisplay(plaintext)
	key := &domain.WorkspaceAPIKey{
		WorkspaceID: workspaceID,
		Name:        trimmedName,
		KeyHash:     domain.HashAPIKey(plaintext),
		KeyPrefix:   prefix,
		KeySuffix:   suffix,
		CreatedBy:   createdByUserID,
	}
	if err := s.keys.Create(ctx, key); err != nil {
		return "", nil, err
	}
	return plaintext, key, nil
}

// List returns the workspace's API keys (hash + display fields, no plaintext).
func (s *APIKeyService) List(ctx context.Context, workspaceID string) ([]domain.WorkspaceAPIKey, error) {
	return s.keys.List(ctx, workspaceID)
}

// Revoke revokes a workspace API key; revoked keys stop authenticating
// immediately. Revoking an already-revoked or foreign key returns
// domain.ErrNotFound.
func (s *APIKeyService) Revoke(ctx context.Context, workspaceID, id string) error {
	return s.keys.Revoke(ctx, workspaceID, id)
}
