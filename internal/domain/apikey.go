package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// WorkspaceAPIKey represents a workspace-scoped credential used to authenticate
// /v1 (OpenResponses) requests. The key resolves to exactly one workspace (the
// tenant scope) and its creating user.
//
// KeyHash is the hex-encoded SHA-256 digest of the plaintext key and is the
// only credential material stored at rest; it MUST NEVER cross the HTTP
// boundary (json:"-"). KeyPrefix and KeySuffix carry display-only fragments.
type WorkspaceAPIKey struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	Name        string     `json:"name"`
	KeyHash     string     `json:"-"`
	KeyPrefix   string     `json:"key_prefix"`
	KeySuffix   string     `json:"key_suffix"`
	CreatedBy   string     `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

// IsRevoked reports whether the key has been revoked; revoked keys stop
// authenticating immediately.
func (k *WorkspaceAPIKey) IsRevoked() bool {
	return k != nil && k.RevokedAt != nil
}

// HashAPIKey returns the hex-encoded SHA-256 hash of the given plaintext key.
// The digest is what stores persist; plaintext keys are never stored.
func HashAPIKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
