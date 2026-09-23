// Package oauthstate holds the single-use signed-state machinery shared by
// the OAuth consent flows (add-mcp-oauth-client design.md D5): the HMAC-sealed
// state format, the single-use nonce store, and the redirect-URI derivation.
// It is the extracted, flow-agnostic core of the connections OAuth service's
// state helpers (services/connections_oauth.go) — the connections flow and the
// MCP server OAuth flow consume the same primitives.
//
// Each flow owns its concrete claims shape (JSON-marshaled into the sealed
// payload, with its own nonce/expiry fields and required-field checks), its
// context string (domain separation), and its generic rejection error; this
// package owns everything else about states. States carry scope and identity
// claims only — tokens and secrets never pass through here.
package oauthstate

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidState marks a state that failed signature or shape validation.
// Flows map every Open failure to their ONE generic rejection so a callback
// never reveals which check failed.
var ErrInvalidState = errors.New("oauth state rejected")

// Sealer seals and opens single-use signed states. The wire format is
// base64url(payload) + "." + base64url(HMAC-SHA256(context || base64url(payload)))
// with the instance master key as the HMAC key: the signature makes the state
// unforgeable, the flow-embedded expiry and the one-time nonce make it
// short-lived and single-use.
type Sealer struct {
	key     []byte
	context string
	nonces  *NonceStore
}

// NewSealer builds a sealer over the given key (the instance master key).
// context domain-separates the HMAC from the other flows' states and the
// other uses of the key; ttl bounds every nonce the sealer issues (states
// carry their own absolute expiry claim, checked by the flow).
func NewSealer(key []byte, context string, ttl time.Duration) *Sealer {
	return &Sealer{key: key, context: context, nonces: NewNonceStore(ttl)}
}

// TTL returns the state TTL the sealer was built with.
func (s *Sealer) TTL() time.Duration {
	return s.nonces.ttl
}

// Nonces returns the sealer's single-use nonce store.
func (s *Sealer) Nonces() *NonceStore {
	return s.nonces
}

// Seal MACs the marshaled claims payload into the state wire format. The
// payload is trusted input (the flow just marshaled it), so sealing cannot
// fail.
func (s *Sealer) Seal(payload []byte) string {
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(s.context))
	mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Open verifies the signature and returns the raw payload bytes. Every
// failure — malformed, forged, undecodable — is ErrInvalidState; callers
// must not distinguish the causes.
func (s *Sealer) Open(raw string) ([]byte, error) {
	encoded, sig, ok := strings.Cut(raw, ".")
	if !ok || encoded == "" || sig == "" {
		return nil, ErrInvalidState
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(s.context))
	mac.Write([]byte(encoded))
	sum, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(sum, mac.Sum(nil)) {
		return nil, ErrInvalidState
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrInvalidState
	}
	return payload, nil
}

// NonceStore issues and consumes single-use nonces. Consumption deletes the
// entry, making every state single-use: replay of a consumed nonce is
// rejected before any token exchange.
type NonceStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]nonceEntry
}

// nonceEntry records an issued nonce's absolute expiry.
type nonceEntry struct{ expiresAt time.Time }

// NewNonceStore builds a nonce store; each issued nonce lives for ttl.
func NewNonceStore(ttl time.Duration) *NonceStore {
	return &NonceStore{ttl: ttl, entries: make(map[string]nonceEntry)}
}

// Issue mints a fresh single-use nonce and opportunistically evicts expired
// entries.
func (n *NonceStore) Issue() string {
	nonce := uuid.NewString()
	now := time.Now()
	n.mu.Lock()
	defer n.mu.Unlock()
	for id, entry := range n.entries {
		if now.After(entry.expiresAt) {
			delete(n.entries, id)
		}
	}
	n.entries[nonce] = nonceEntry{expiresAt: now.Add(n.ttl)}
	return nonce
}

// Consume reports — and burns — an unconsumed, unexpired nonce.
func (n *NonceStore) Consume(nonce string) bool {
	now := time.Now()
	n.mu.Lock()
	defer n.mu.Unlock()
	entry, exists := n.entries[nonce]
	if !exists || now.After(entry.expiresAt) {
		delete(n.entries, nonce)
		return false
	}
	delete(n.entries, nonce)
	return true
}

// DeriveRedirectURI derives a flow's redirect URI from the instance public
// base URL and the flow's callback path — derived at read time, never stored.
func DeriveRedirectURI(publicBaseURL, callbackPath string) string {
	return strings.TrimRight(publicBaseURL, "/") + callbackPath
}
