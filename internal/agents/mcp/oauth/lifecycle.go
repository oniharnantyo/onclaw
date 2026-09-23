package oauth

import (
	"context"
	"errors"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// The refresh lifecycle (add-mcp-oauth-client task 4.6, design.md D6): a
// stored token inside its refresh margin is renewed on dial; a failed refresh
// FAILS OPEN — the stored token still dials — while the server row's
// `expired` status and the provider's error detail are persisted, and
// `expired` is left only by reauthorization (domain.CanTransitionMCPStatus).
// This package stays store-agnostic: the token rows ride the CredentialStore
// seam and the status write rides the StatusSink seam, both implemented by
// the services layer over store.MCPTokens and the guarded settings
// persistence.

// DefaultTokenRefreshMargin reuses the connections OAuth flow's margin value
// exactly (domain.DefaultRecipeRefreshMargin): one refresh convention across
// every OAuth credential in the system.
const DefaultTokenRefreshMargin = domain.DefaultRecipeRefreshMargin

// ErrNoCredential marks a server row with no stored token set — the caller's
// signal that the dial needs (re)authorization, not a refresh.
var ErrNoCredential = errors.New("mcp oauth: no stored credential for this server")

// CredentialRef identifies one server row's token set (the store's
// workspace/agent/server key; an empty AgentID is workspace scope).
type CredentialRef struct {
	WorkspaceID string
	AgentID     string
	ServerID    string
}

// Credential is one server row's decrypted token set. Plaintext token
// material — memory only, never logged, re-encrypted by the CredentialStore
// on Replace.
type Credential struct {
	AccessToken  string
	RefreshToken string
	// ExpiresAt is nil when the provider declared no expiry — such tokens
	// never refresh and never expire on our side.
	ExpiresAt     *time.Time
	GrantedScopes []string
	Issuer        string
	// RefreshUnreadable marks a stored refresh envelope that failed to open:
	// the credential still dials (fail-open) but can never refresh, and the
	// lifecycle treats it as a refresh failure (the expired transition).
	RefreshUnreadable bool
}

// CredentialStore is the token-row seam the services layer implements over
// store.MCPTokens and the instance master key, keeping this package free of
// the store and crypto layers. Load returns ErrNoCredential when the server
// row has no token set. Replace persists the renewed set in one write (the
// store's Replace is the atomic create-or-replace).
type CredentialStore interface {
	Load(ctx context.Context, ref CredentialRef) (*Credential, error)
	Replace(ctx context.Context, ref CredentialRef, cred *Credential) error
}

// StatusSink is the status seam the services layer implements over the
// guarded settings persistence: the ONLY status this lifecycle writes is
// `expired`, entered per the design.md D6 transition rules
// (domain.CanTransitionMCPStatus — the implementation refuses illegal moves).
type StatusSink interface {
	MarkExpired(ctx context.Context, ref CredentialRef, detail string) error
}

// EnsureCredentialParams names the discovery result and resolved client the
// stored tokens were issued under.
type EnsureCredentialParams struct {
	Meta   *Metadata
	Client *ResolvedClient
}

// EnsureFreshCredential returns the access token the dial should use: the
// stored one when fresh (or undeterminable), a renewed one after a
// successful in-margin refresh.
//
// Failure semantics (design.md D6, the fail-open contract):
//
//   - no stored credential → ErrNoCredential (the dial's
//     needs-authorization signal, not an error);
//   - a refused or otherwise failed refresh → NO error: the expired
//     transition and the provider detail are persisted through the sink and
//     the STORED token is returned so the dial proceeds;
//   - only store-level failures (Load/Replace) surface as errors.
//
// A successful refresh replaces the token rows atomically through the store's
// Replace; an omitted response refresh token keeps the stored one (RFC 6749
// §6 — rotation vs replay), and an omitted scope echo keeps the granted set.
// The status is deliberately untouched on success: expired is cleared only by
// reauthorization.
func (c *Client) EnsureFreshCredential(ctx context.Context, p EnsureCredentialParams, ref CredentialRef, store CredentialStore, sink StatusSink) (string, error) {
	if p.Meta == nil || p.Client == nil {
		return "", errors.New("mcp oauth: discovery metadata and a resolved client are required for the refresh lifecycle")
	}
	cred, err := store.Load(ctx, ref)
	if err != nil {
		return "", err
	}
	if cred.ExpiresAt == nil || time.Until(*cred.ExpiresAt) > c.margin {
		return cred.AccessToken, nil
	}
	if cred.RefreshUnreadable {
		// A refresh envelope that cannot be opened is a refresh failure (the
		// connections flow's convention) — persist it and dial on the stored
		// token.
		_ = sink.MarkExpired(ctx, ref, "the stored refresh token could not be opened")
		return cred.AccessToken, nil
	}
	if cred.RefreshToken == "" {
		// Expiry-only provider: nothing to present; the token dials until it
		// expires and reauthorization replaces it.
		return cred.AccessToken, nil
	}

	tokens, err := c.RefreshTokens(ctx, RefreshInput{
		Meta:         p.Meta,
		Client:       p.Client,
		RefreshToken: cred.RefreshToken,
	})
	if err != nil {
		// FAIL OPEN: the provider detail rides the expired transition; the
		// dial proceeds with the stored token. The sink implementation guards
		// the transition (domain.CanTransitionMCPStatus), so repeated
		// failures hold expired idempotently and illegal moves never write.
		_ = sink.MarkExpired(ctx, ref, err.Error())
		return cred.AccessToken, nil
	}

	renewed := &Credential{
		AccessToken:   tokens.AccessToken,
		RefreshToken:  cred.RefreshToken,
		ExpiresAt:     expiresAtFromTokens(tokens),
		GrantedScopes: cred.GrantedScopes,
		Issuer:        p.Meta.AuthorizationServer.Issuer,
	}
	if tokens.RefreshToken != "" {
		renewed.RefreshToken = tokens.RefreshToken
	}
	if len(tokens.Scopes) > 0 {
		renewed.GrantedScopes = tokens.Scopes
	}
	if err := store.Replace(ctx, ref, renewed); err != nil {
		return "", err
	}
	return renewed.AccessToken, nil
}

// expiresAtFromTokens converts a token set's declared lifetime; none declared
// yields nil (the expiry-only shape).
func expiresAtFromTokens(tokens *TokenSet) *time.Time {
	if tokens.ExpiresIn <= 0 {
		return nil
	}
	expiresAt := time.Now().Add(tokens.ExpiresIn)
	return &expiresAt
}
