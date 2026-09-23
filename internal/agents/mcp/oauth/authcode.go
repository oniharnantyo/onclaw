package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// The authorization-code grant with PKCE S256 (add-mcp-oauth-client task 4.4,
// design.md D5): a single-use, server-bound HMAC-sealed state (the shared
// oauthstate machinery) rides the authorize URL, and the PKCE verifier rides
// a SECOND sealed bundle the caller holds and echoes at completion — the
// state is attacker-readable (it travels through the browser), so the verifier
// must never ride in it. The callback validates state and session before any
// token exchange, and every rejection is the ONE generic sentinel.
const (
	// StateContext domain-separates the MCP authorization states' HMAC from
	// the other users of the instance master key (the connections flow's
	// states, the verifier sessions below, the secret envelopes).
	StateContext = "onclaw:mcp-oauth-state:v1"
	// SessionContext domain-separates the PKCE verifier session bundles.
	SessionContext = "onclaw:mcp-oauth-session:v1"
	// DefaultStateTTL bounds a signed state (and its session bundle). It
	// mirrors the connections flow's DefaultOAuthStateTTL.
	DefaultStateTTL = 10 * time.Minute
)

// ErrStateInvalid marks a callback state or session that failed signature,
// TTL, single-use, binding, or shape validation — deliberately ONE generic
// error so the callback response never reveals WHICH check rejected the
// attempt (the connections flow's rejection convention).
var ErrStateInvalid = errors.New("mcp oauth authorization rejected")

// StateClaims is the signed state payload: the tenant/server row the consent
// belongs to (workspace + optional owning agent + server id), the
// authorization server the flow was started against (the callback refuses a
// state minted for a different discovery), the one-time nonce, and the
// absolute expiry. Identity claims only — no tokens, no secrets.
type StateClaims struct {
	WorkspaceID string `json:"w"`
	AgentID     string `json:"g,omitempty"` // empty = workspace scope
	ServerID    string `json:"s"`
	Issuer      string `json:"i,omitempty"`
	Nonce       string `json:"n"`
	ExpiresAt   int64  `json:"e"`
}

// codeSession is the sealed verifier-session payload: the PKCE verifier bound
// to the state's nonce, so a session cannot be paired with a foreign state.
type codeSession struct {
	Nonce     string `json:"n"`
	Verifier  string `json:"v,omitempty"`
	ExpiresAt int64  `json:"e"`
}

// BeginParams is one authorization start.
type BeginParams struct {
	// Meta is the discovery result to authorize against; its issuer is
	// stamped into the state.
	Meta *Metadata
	// Client is the resolved client (ResolveClient).
	Client *ResolvedClient
	// RedirectURI is this instance's derived callback.
	RedirectURI string
	// Scopes is the consent request; empty omits the scope parameter (the
	// server then applies its defaults).
	Scopes []string
	// Claims carries the tenant/server identity (WorkspaceID, AgentID,
	// ServerID); the nonce and expiry are filled when unset.
	Claims StateClaims
}

// AuthorizationBegin is one started flow: the URL to redirect the user to,
// the sealed state (already embedded in the URL), and the sealed verifier
// session the CALLER must hold and echo at CompleteAuthorization (cookie,
// pending-flow row — the transport is the caller's choice).
type AuthorizationBegin struct {
	AuthorizeURL string
	State        string
	Session      string
	Claims       StateClaims
}

// BeginAuthorization composes the provider authorize URL: response_type=code,
// the client id, the derived redirect URI, the sealed state, the requested
// scopes, and the S256 PKCE challenge when the resolved client uses PKCE.
// The client secret never appears on this URL — it exists only for the token
// endpoint.
func (c *Client) BeginAuthorization(p BeginParams) (*AuthorizationBegin, error) {
	if p.Meta == nil || p.Client == nil {
		return nil, fmt.Errorf("%w: discovery metadata and a resolved client are required", domain.ErrInvalid)
	}
	if p.RedirectURI == "" {
		return nil, fmt.Errorf("%w: the redirect uri is required", domain.ErrInvalid)
	}
	claims := p.Claims
	if claims.WorkspaceID == "" || claims.ServerID == "" {
		return nil, fmt.Errorf("%w: the state claims must name the workspace and server", domain.ErrInvalid)
	}
	if claims.Nonce == "" {
		claims.Nonce = c.state.Nonces().Issue()
	}
	if claims.ExpiresAt == 0 {
		claims.ExpiresAt = time.Now().Add(DefaultStateTTL).Unix()
	}
	// The issuer is stamped (not caller-supplied): the flow is bound to the
	// discovery that started it, so a re-discovery between begin and
	// callback cannot silently switch authorization servers mid-consent.
	claims.Issuer = p.Meta.AuthorizationServer.Issuer

	payload, err := json.Marshal(claims)
	if err != nil {
		return nil, fmt.Errorf("marshal mcp oauth state: %w", err)
	}
	state := c.state.Seal(payload)

	session := codeSession{Nonce: claims.Nonce, ExpiresAt: claims.ExpiresAt}
	if p.Client.UsePKCE {
		verifier, _, err := newPKCE()
		if err != nil {
			return nil, err
		}
		session.Verifier = verifier
	}
	sessionPayload, err := json.Marshal(session)
	if err != nil {
		return nil, fmt.Errorf("marshal mcp oauth session: %w", err)
	}

	endpoint, err := url.Parse(p.Meta.AuthorizationServer.AuthorizationEndpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: the authorization endpoint is unparseable: %v", domain.ErrInvalid, err)
	}
	query := endpoint.Query()
	query.Set("response_type", "code")
	query.Set("client_id", p.Client.ClientID)
	query.Set("redirect_uri", p.RedirectURI)
	query.Set("state", state)
	if len(p.Scopes) > 0 {
		query.Set("scope", strings.Join(p.Scopes, " "))
	}
	if p.Client.UsePKCE {
		challenge, err := ChallengeS256(session.Verifier)
		if err != nil {
			return nil, err
		}
		query.Set("code_challenge", challenge)
		query.Set("code_challenge_method", "S256")
	}
	endpoint.RawQuery = query.Encode()

	return &AuthorizationBegin{
		AuthorizeURL: endpoint.String(),
		State:        state,
		Session:      c.sessions.Seal(sessionPayload),
		Claims:       claims,
	}, nil
}

// CompleteParams is one callback completion.
type CompleteParams struct {
	Meta        *Metadata
	Client      *ResolvedClient
	RedirectURI string
	Code        string
	State       string
	Session     string
}

// AuthorizationResult is a completed authorization: the exchanged tokens and
// the validated state claims (the caller persists the tokens against the
// named server row).
type AuthorizationResult struct {
	Tokens *TokenSet
	Claims StateClaims
}

// CompleteAuthorization finishes the flow: validate the session and state
// (every rejection precedes any token exchange), consume the single-use
// nonce, exchange the code with the PKCE verifier, and validate the RFC 9207
// issuer on the token response. Every state/session rejection is
// ErrStateInvalid with no detail — the callback must not leak which check
// failed.
func (c *Client) CompleteAuthorization(ctx context.Context, p CompleteParams) (*AuthorizationResult, error) {
	session, err := c.openSession(p.Session)
	if err != nil {
		return nil, ErrStateInvalid
	}
	claims, err := c.openState(p.State)
	if err != nil {
		return nil, ErrStateInvalid
	}
	// The session must be the one minted with this state (the verifier is
	// bound to the nonce).
	if session.Nonce != claims.Nonce {
		return nil, ErrStateInvalid
	}
	if p.Meta == nil || p.Client == nil || p.RedirectURI == "" {
		return nil, fmt.Errorf("%w: discovery metadata, the resolved client, and the redirect uri are required", domain.ErrInvalid)
	}
	// The authorization server must be the one the flow was started against.
	if !sameResourceIdentifier(claims.Issuer, p.Meta.AuthorizationServer.Issuer) {
		return nil, ErrStateInvalid
	}
	if strings.TrimSpace(p.Code) == "" {
		return nil, fmt.Errorf("%w: the callback carried no authorization code", ErrTokenExchange)
	}

	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {p.Code},
		"redirect_uri": {p.RedirectURI},
	}
	if session.Verifier != "" {
		form.Set("code_verifier", session.Verifier)
	}
	tokens, err := c.postToken(ctx, p.Meta.AuthorizationServer.TokenEndpoint, form, p.Client, p.Meta)
	if err != nil {
		return nil, err
	}
	return &AuthorizationResult{Tokens: tokens, Claims: claims}, nil
}

// openState verifies the state's signature, shape, and TTL and consumes its
// nonce — a replayed state is rejected before any token exchange.
func (c *Client) openState(raw string) (StateClaims, error) {
	payload, err := c.state.Open(raw)
	if err != nil {
		return StateClaims{}, ErrStateInvalid
	}
	var claims StateClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return StateClaims{}, ErrStateInvalid
	}
	if claims.WorkspaceID == "" || claims.ServerID == "" || claims.Nonce == "" {
		return StateClaims{}, ErrStateInvalid
	}
	if time.Now().Unix() > claims.ExpiresAt {
		return StateClaims{}, ErrStateInvalid
	}
	if !c.state.Nonces().Consume(claims.Nonce) {
		return StateClaims{}, ErrStateInvalid
	}
	return claims, nil
}

// PeekState verifies the state's signature, shape, and TTL WITHOUT consuming
// its nonce (tasks.md 6.1): the callback's lookup step — the claims name the
// server row the flow must re-discover and re-resolve the client against
// before CompleteAuthorization validates and consumes the state for real.
// Every rejection is ErrStateInvalid, exactly like openState, so a peeked-but
// unconsumable state never reveals which check failed.
func (c *Client) PeekState(raw string) (StateClaims, error) {
	payload, err := c.state.Open(raw)
	if err != nil {
		return StateClaims{}, ErrStateInvalid
	}
	var claims StateClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return StateClaims{}, ErrStateInvalid
	}
	if claims.WorkspaceID == "" || claims.ServerID == "" || claims.Nonce == "" {
		return StateClaims{}, ErrStateInvalid
	}
	if time.Now().Unix() > claims.ExpiresAt {
		return StateClaims{}, ErrStateInvalid
	}
	return claims, nil
}

// openSession verifies the verifier session's signature, shape, and TTL
// WITHOUT consuming anything — sessions are single-use by binding to the
// state's single-use nonce, not by their own store.
func (c *Client) openSession(raw string) (codeSession, error) {
	payload, err := c.sessions.Open(raw)
	if err != nil {
		return codeSession{}, ErrStateInvalid
	}
	var session codeSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return codeSession{}, ErrStateInvalid
	}
	if session.Nonce == "" {
		return codeSession{}, ErrStateInvalid
	}
	if time.Now().Unix() > session.ExpiresAt {
		return codeSession{}, ErrStateInvalid
	}
	return session, nil
}
