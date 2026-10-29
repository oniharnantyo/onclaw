package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mark3labs/mcp-go/client/transport"
)

// Token and its store are mcp-go's types re-exported so the rest of buddies
// (the store's token rows, the service flow) never imports mcp-go directly —
// the package accepts narrow data, not third-party surfaces (design D3).
type (
	// TokenStore stores and retrieves OAuth tokens (mcp-go's two-method
	// interface; the sqlite-backed implementation lives in the store layer,
	// serialized per server).
	TokenStore = transport.TokenStore
	// Token is one OAuth token set — access, refresh, expiry — as mcp-go
	// exchanges, refreshes, and saves it.
	Token = transport.Token
)

// ErrNoToken is what a TokenStore.GetToken returns when nothing is stored.
var ErrNoToken = transport.ErrNoToken

// OAuth is a connection's OAuth 2.1 client settings (design D1/D3): the
// registration persists so re-authorization never re-registers and the
// gateway refreshes forever without a human. The zero value (Enabled false)
// keeps today's static-header dial.
type OAuth struct {
	Enabled bool
	// ClientID and ClientSecret are the registered client; ClientID empty
	// means the flow must register dynamically first.
	ClientID     string
	ClientSecret string
	// RedirectURI is the registered loopback callback; empty means
	// DefaultRedirectURI.
	RedirectURI string
	// Scopes are requested at authorization time — discovered from the
	// protected resource, never user-configured (design D9).
	Scopes []string
	// AuthServerMetadataURL is the authorization server's metadata URL. It
	// MUST be a URL Discover/FetchAuthServerMetadata already validated:
	// mcp-go's own fetcher treats a failed fetch as "no metadata" without an
	// error, which would nil-dereference registration (design D1 gotcha) —
	// and it probes only the origin-level well-known path, which path-scoped
	// deployments do not serve.
	AuthServerMetadataURL string
	// Tokens stores the exchanged and refreshed tokens; required when
	// Enabled (dial fails fast otherwise — a wiring bug, not a runtime state).
	Tokens TokenStore
	// PreRegistered marks a confidential client (design D3's
	// oauth_confidential): the ClientID/ClientSecret were minted by the
	// provider's developer console, not by dynamic registration, so the flow
	// must never re-register them — not even when the callback port rebinds.
	// Google, Slack, Asana, Box, and Dropbox all work this way.
	PreRegistered bool
	// ExtraAuthParams are extra query parameters appended to the
	// authorization URL (design D3). Google requires access_type=offline and
	// prompt=consent to hand back a refresh token; a parameter already in the
	// URL wins.
	ExtraAuthParams map[string]string
}

// errNoTokenStore marks the wiring bug of an OAuth-enabled connection without
// a token store: dial fails fast instead of panicking inside mcp-go's first
// request.
var errNoTokenStore = errors.New("mcp oauth enabled without a token store")

// ready reports whether the settings are dialable.
func (o OAuth) ready() error {
	if o.Enabled && o.Tokens == nil {
		return errNoTokenStore
	}
	return nil
}

// config renders the settings as mcp-go's transport config. PKCE is always on
// (public client; design D1: mcp-go's tested OAuth stack, nothing hand-rolled).
func (o OAuth) config() transport.OAuthConfig {
	return transport.OAuthConfig{
		ClientID:              o.ClientID,
		ClientSecret:          o.ClientSecret,
		RedirectURI:           o.RedirectURI,
		Scopes:                o.Scopes,
		TokenStore:            o.Tokens,
		AuthServerMetadataURL: o.AuthServerMetadataURL,
		PKCEEnabled:           true,
	}
}

// ErrAuthRequired is the sentinel the probe classifies into the
// StatusAuthRequired status (design D5): the server requires user
// authorization, which is a distinct row state — not a plain error.
var ErrAuthRequired = errors.New("mcp server requires authorization")

// AuthRequiredError carries what an unauthenticated probe discovered about
// the server's OAuth protection. Discovery is nil when the OAuth transport
// itself reported the need (no, expired-and-unrefreshable, or rejected
// tokens) — the connection already knows where to authorize.
type AuthRequiredError struct {
	Discovery *Discovery
	Cause     error
}

// Error implements error, preserving the cause's text for the status reason.
func (e *AuthRequiredError) Error() string {
	if e.Cause == nil {
		return ErrAuthRequired.Error()
	}
	return ErrAuthRequired.Error() + ": " + e.Cause.Error()
}

// Unwrap exposes the underlying dial/handshake failure for errors.Is/As.
func (e *AuthRequiredError) Unwrap() error { return e.Cause }

// Is reports membership of ErrAuthRequired so errors.Is holds for both the
// transport-reported and discovery-reported shapes (design D5).
func (e *AuthRequiredError) Is(target error) bool { return target == ErrAuthRequired }

// ErrNoOAuth reports that a server does not advertise OAuth protection: no
// 401, a 401 without a usable resource_metadata challenge, or a challenge
// whose document does not decode to a well-formed protected resource. Such a
// server stays StatusError when it fails (design D5: only a well-formed
// advertisement is auth_required).
var ErrNoOAuth = errors.New("mcp server does not advertise oauth")

// Discovery is what an OAuth-protected MCP server advertises (RFC 9728 for
// the protected resource, RFC 8414 for its authorization server). The
// authorize flow and DisconnectMCPServer work from it.
type Discovery struct {
	// ResourceMetadataURL is the URL the 401's WWW-Authenticate named.
	ResourceMetadataURL string
	// Issuer is the protected resource's authorization_servers[0].
	Issuer string
	// AuthServerMetadataURL is the authorization-server metadata URL that
	// validated — handed to mcp-go so its fallback discovery stays dead code
	// (design D1).
	AuthServerMetadataURL string
	// Scopes is the protected resource's scopes_supported (may be empty).
	Scopes                []string
	AuthorizationEndpoint string
	TokenEndpoint         string
	RegistrationEndpoint  string
	RevocationEndpoint    string
}

// AuthServerMetadata is the subset of RFC 8414 metadata buddies reads. It is
// decoded raw — mcp-go's metadata struct omits revocation_endpoint, which
// DisconnectMCPServer revokes through (design D9).
type AuthServerMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint,omitempty"`
	RevocationEndpoint    string   `json:"revocation_endpoint,omitempty"`
	ScopesSupported       []string `json:"scopes_supported,omitempty"`
}

// oauthHTTPClient bounds every discovery and revocation step per request; a
// probe additionally runs under the caller's connect timeout.
var oauthHTTPClient = &http.Client{Timeout: DefaultConnectTimeout}

// discoverInitialize is the minimal JSON-RPC initialize the HTTP probe sends.
// Its only job is to provoke the 401 whose challenge names the protected
// resource (verified shape: Atlassian answers 401 {"error":"invalid_token"}).
const discoverInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"buddies","version":"1.0.0"}}}`

// Discover sends ONE unauthenticated request to an HTTP/SSE MCP server —
// headers are the connection's configured headers — and, on a 401 whose
// WWW-Authenticate names a resource_metadata document (RFC 9728 §5), fetches
// and validates the protected-resource and authorization-server metadata.
// ErrNoOAuth means the server does not advertise OAuth protection (no 401, a
// 401 without a usable challenge, or a malformed advertisement); any other
// error means it does but its metadata is unreachable or invalid. mcp-go's
// built-in discovery is never relied on: it probes only the origin-level
// well-known path, which path-scoped deployments do not serve, and it would
// cache wrong endpoints (design D1) — Discover is the validation pre-step.
func Discover(ctx context.Context, transport, url string, headers map[string]string) (*Discovery, error) {
	var req *http.Request
	var err error
	switch transport {
	case TransportStreamableHTTP:
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(discoverInitialize))
		if err != nil {
			return nil, fmt.Errorf("%w: build initialize probe: %w", ErrNoOAuth, err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
	case TransportSSE:
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("%w: build probe: %w", ErrNoOAuth, err)
		}
		req.Header.Set("Accept", "text/event-stream")
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedTransport, transport)
	}
	for name, value := range headerMap(headers) {
		req.Header.Set(name, value)
	}

	// Status and headers are the whole result; the body is never read (an
	// SSE endpoint would happily stream forever).
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: probe request: %w", ErrNoOAuth, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		return nil, fmt.Errorf("%w: status %d", ErrNoOAuth, resp.StatusCode)
	}
	resourceMetadataURL := bearerResourceMetadata(resp.Header.Values("WWW-Authenticate"))
	if resourceMetadataURL == "" {
		return nil, fmt.Errorf("%w: 401 without a resource_metadata challenge", ErrNoOAuth)
	}

	// The challenge alone is not enough (design D5): the advertisement counts
	// only once its document decodes with at least one authorization server.
	var resource struct {
		AuthorizationServers []string `json:"authorization_servers"`
		Resource             string   `json:"resource"`
		ScopesSupported      []string `json:"scopes_supported"`
	}
	if err := fetchJSON(ctx, resourceMetadataURL, &resource); err != nil {
		return nil, fmt.Errorf("%w: protected resource metadata: %w", ErrNoOAuth, err)
	}
	if len(resource.AuthorizationServers) == 0 {
		return nil, fmt.Errorf("%w: protected resource lists no authorization servers", ErrNoOAuth)
	}

	issuer := resource.AuthorizationServers[0]
	candidates := authorizationServerMetadataURLs(issuer)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: invalid authorization server URL %q", ErrNoOAuth, issuer)
	}
	var lastErr error
	var lastURL string
	for _, candidate := range candidates {
		metadata, err := FetchAuthServerMetadata(ctx, candidate)
		if err == nil {
			return &Discovery{
				ResourceMetadataURL:   resourceMetadataURL,
				Issuer:                issuer,
				AuthServerMetadataURL: candidate,
				Scopes:                resource.ScopesSupported,
				AuthorizationEndpoint: metadata.AuthorizationEndpoint,
				TokenEndpoint:         metadata.TokenEndpoint,
				RegistrationEndpoint:  metadata.RegistrationEndpoint,
				RevocationEndpoint:    metadata.RevocationEndpoint,
			}, nil
		}
		lastErr, lastURL = err, candidate
	}
	// The server advertises OAuth but its authorization-server metadata is
	// broken — a failure, not "no OAuth": such a server must not pass as a
	// plain error row when it is really waiting on its provider (design D5).
	return nil, fmt.Errorf("authorization server metadata for %s: %w", lastURL, lastErr)
}

// fetchJSON GETs url as JSON into out with the bounded client: a 200 with a
// decodable body, or an error.
func fetchJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// bearerResourceMetadata returns the resource_metadata parameter of the first
// Bearer challenge across the WWW-Authenticate values (RFC 9728 §5), "" when
// no challenge names one.
func bearerResourceMetadata(values []string) string {
	for _, value := range values {
		scheme, params := parseChallenge(value)
		if strings.EqualFold(scheme, "Bearer") {
			if metadataURL := params["resource_metadata"]; metadataURL != "" {
				return metadataURL
			}
		}
	}
	return ""
}

// parseChallenge parses one WWW-Authenticate value (RFC 7235): a scheme token
// followed by name=value pairs, quoted strings may carry any bytes, parameter
// names are case-insensitive and lower-cased. A token not followed by '=' is
// another challenge within the same value and starts a fresh parameter set.
func parseChallenge(value string) (scheme string, params map[string]string) {
	params = map[string]string{}
	s := strings.TrimSpace(value)
	for s != "" {
		token, rest := cutToken(s)
		if rest == "" || rest[0] != '=' {
			scheme = token
			params = map[string]string{}
			s = strings.TrimLeft(rest, " ,")
			continue
		}
		s = rest[1:]
		var param string
		if strings.HasPrefix(s, `"`) {
			param, s = scanQuotedString(s)
		} else if comma := strings.IndexByte(s, ','); comma >= 0 {
			param, s = strings.TrimSpace(s[:comma]), s[comma+1:]
		} else {
			param, s = strings.TrimSpace(s), ""
		}
		if scheme != "" {
			params[strings.ToLower(token)] = param
		}
		s = strings.TrimLeft(s, " ,")
	}
	return scheme, params
}

// cutToken splits off the leading token (up to a space, '=', or comma) and
// returns the remainder with leading spaces trimmed.
func cutToken(s string) (token, rest string) {
	end := strings.IndexAny(s, " =,")
	if end < 0 {
		return s, ""
	}
	return s[:end], strings.TrimLeft(s[end:], " ")
}

// scanQuotedString reads an RFC 7230 quoted-string (s starts with '"'),
// honoring backslash escapes, and returns the unquoted value and the rest.
func scanQuotedString(s string) (value, rest string) {
	var sb strings.Builder
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if i+1 < len(s) {
				i++
				sb.WriteByte(s[i])
			}
		case '"':
			return sb.String(), s[i+1:]
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String(), ""
}

// authorizationServerMetadataURLs lists an issuer's candidate metadata URLs
// in probe order: RFC 8414's path-aware well-known form first, then OIDC's
// fixed discovery path (some authorization servers only ship the latter).
func authorizationServerMetadataURLs(issuer string) []string {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil
	}
	wellKnown := parsed.Scheme + "://" + parsed.Host + "/.well-known/oauth-authorization-server"
	if parsed.Path != "" && parsed.Path != "/" {
		wellKnown += parsed.Path
	}
	return []string{wellKnown, parsed.Scheme + "://" + parsed.Host + "/.well-known/openid-configuration"}
}

// FetchAuthServerMetadata fetches an authorization server's metadata document
// and validates what buddies needs: a 200 response carrying non-empty
// authorization_endpoint and token_endpoint. Validating up front is what lets
// the OAuth transports and StartAuthorization be handed AuthServerMetadataURL
// blindly (design D1's gotcha).
func FetchAuthServerMetadata(ctx context.Context, metadataURL string) (*AuthServerMetadata, error) {
	var metadata AuthServerMetadata
	if err := fetchJSON(ctx, metadataURL, &metadata); err != nil {
		return nil, fmt.Errorf("authorization server metadata %s: %w", metadataURL, err)
	}
	if metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" {
		return nil, fmt.Errorf("authorization server metadata %s: missing authorization or token endpoint", metadataURL)
	}
	return &metadata, nil
}

// RevokeToken posts an RFC 7009 revocation: the token, an optional
// token_type_hint, and the client credentials in the form. A non-2xx response
// is an error; callers treat the whole call as best-effort (a failed
// revocation must not block a disconnect — design D9).
func RevokeToken(ctx context.Context, revocationEndpoint, token, tokenTypeHint, clientID, clientSecret string) error {
	form := url.Values{}
	form.Set("token", token)
	if tokenTypeHint != "" {
		form.Set("token_type_hint", tokenTypeHint)
	}
	if clientID != "" {
		form.Set("client_id", clientID)
	}
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, revocationEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("token revocation request %s: %w", revocationEndpoint, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("token revocation request %s: %w", revocationEndpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("token revocation %s: status %d: %s", revocationEndpoint, resp.StatusCode, body)
	}
	return nil
}
