package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Client identification (add-mcp-oauth-client tasks 4.1–4.3, design.md D3):
// the strategies are tried in a fixed order — BYO (pre-registered app from
// the server config) → client-id metadata document (when the effective client
// id is an HTTPS URL) → dynamic registration (RFC 7591) against the
// authorization server's registration_endpoint. DCR results are returned to
// the caller for per-server-row persistence: register once, reuse forever —
// a persisted registration re-enters this order as a BYO client id, and a
// caller may also hand a stored registration back through
// ClientIdentity.StoredRegistration to skip re-registration outright.

// ClientStrategy names the identification strategy a ResolvedClient came
// from.
type ClientStrategy string

const (
	StrategyBYO  ClientStrategy = "byo"
	StrategyCIDM ClientStrategy = "cidm"
	StrategyDCR  ClientStrategy = "dcr"
)

// ErrClientResolution is the parent sentinel every client-identification
// failure chains to; errors.As to *ClientResolutionError for the strategy
// attempted, the detail, and operator Guidance.
var ErrClientResolution = errors.New("mcp oauth client resolution failed")

// ClientResolutionError is a typed client-identification failure. Guidance
// names the remediation per strategy; details carry provider messages
// verbatim but never client secrets.
type ClientResolutionError struct {
	Strategy  ClientStrategy
	ServerURL string
	Detail    string
	Cause     error
}

func (e *ClientResolutionError) Error() string {
	return fmt.Sprintf("%s (%s for %s): %s", ErrClientResolution.Error(), e.Strategy, e.ServerURL, e.Detail)
}

func (e *ClientResolutionError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrClientResolution}
	}
	return []error{ErrClientResolution, e.Cause}
}

// Guidance returns the operator-facing remediation for the strategy that
// failed (spec: a registration refusal surfaces the provider's error detail
// and guidance to configure a pre-registered app).
func (e *ClientResolutionError) Guidance() string {
	const byo = "configure a pre-registered OAuth app on the server row (bring-your-own client id / secret)"
	switch e.Strategy {
	case StrategyBYO:
		return "verify the pre-registered app's client id and secret; " + byo
	case StrategyCIDM:
		return "verify the client-id metadata document is reachable and declares this instance's redirect URI, or configure a regular (non-URL) pre-registered client id; " + byo
	case StrategyDCR:
		return "the authorization server refused dynamic registration (it may be disabled or rate limited); " + byo
	default:
		return byo
	}
}

// Registration is the RFC 7591 §3.2.1 client registration response, and the
// stored shape a caller hands back (or persists per server row) so a
// registration happens once and is reused forever.
type Registration struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
}

// ClientIdentity is what the caller knows before resolution: the configured
// bring-your-own rows from the server config, the redirect URI this instance
// answers callbacks on, and any stored DCR registration.
type ClientIdentity struct {
	// ConfiguredClientID is the BYO client id; an HTTPS URL value routes to
	// the client-id metadata document strategy (design.md D3 order).
	ConfiguredClientID string
	// ConfiguredClientSecret is the BYO confidential app's secret, already
	// decrypted by the caller. Empty means a public client.
	ConfiguredClientSecret string
	// RedirectURI is the derived callback this instance answers on; required
	// for the metadata-document validation and dynamic registration.
	RedirectURI string
	// StoredRegistration, when non-nil, short-circuits dynamic registration
	// (register once, reuse forever).
	StoredRegistration *Registration
}

// ResolvedClient is one identified client: how to present itself at the
// token/device endpoints (id, secret, auth method) and which grants to use.
// ClientSecret is plaintext — the caller holds it only long enough to
// authorize or refresh; it is sent only to the token/device endpoints.
type ResolvedClient struct {
	Strategy ClientStrategy
	ClientID string
	// ClientSecret is empty for public clients (method none).
	ClientSecret string
	// AuthMethod is one of the AuthMethod* constants.
	AuthMethod string
	// UsePKCE pins the S256 code challenge onto the authorization request:
	// always for public clients (the code-interception guard has no backup
	// without a client secret), otherwise whenever the server advertises S256.
	UsePKCE bool
	// Registration is non-nil for dynamic registration; the caller persists
	// it per server row (tasks.md 4.3).
	Registration *Registration
	// ReusedRegistration marks a StoredRegistration handed back by the
	// caller — no registration request was made.
	ReusedRegistration bool
	// MetadataDocumentURL is the fetched document URL for the CIDM strategy
	// (the client id itself, kept for diagnostics).
	MetadataDocumentURL string
	// RedirectURIs lists the redirect URIs the client is registered for.
	RedirectURIs []string
}

// ResolveClient identifies the client for one discovered authorization
// server, in the design.md D3 order: BYO → client-id metadata document →
// dynamic registration.
func (c *Client) ResolveClient(ctx context.Context, meta *Metadata, id ClientIdentity) (*ResolvedClient, error) {
	if meta == nil {
		return nil, &ClientResolutionError{Strategy: StrategyBYO, Detail: "discovery metadata is required"}
	}
	serverURL := meta.ServerURL
	fail := func(strategy ClientStrategy, detail string, cause error) (*ResolvedClient, error) {
		return nil, &ClientResolutionError{Strategy: strategy, ServerURL: serverURL, Detail: detail, Cause: cause}
	}
	if id.ConfiguredClientID != "" {
		if isHTTPSURL(id.ConfiguredClientID) {
			// A URL-shaped client id is a metadata-document reference, not an
			// opaque id (the Hermes ordering lesson: the document is tried
			// before DCR, and a failing document is an error — never a
			// silent reinterpretation as a normal client id).
			return c.resolveByMetadataDocument(ctx, meta, id)
		}
		return resolveByConfiguredClient(meta, id)
	}
	if id.StoredRegistration != nil {
		return resolveByStoredRegistration(meta, id)
	}
	if meta.AuthorizationServer.RegistrationEndpoint != "" {
		return c.registerDynamically(ctx, meta, id)
	}
	return fail("", "no pre-registered client is configured, the client id is not a metadata-document URL, and the authorization server advertises no registration endpoint", nil)
}

// resolveByConfiguredClient implements the BYO strategy (task 4.1): the
// configured id (+ secret = confidential, else public) is used verbatim.
func resolveByConfiguredClient(meta *Metadata, id ClientIdentity) (*ResolvedClient, error) {
	as := meta.AuthorizationServer
	rc := &ResolvedClient{
		Strategy:     StrategyBYO,
		ClientID:     id.ConfiguredClientID,
		RedirectURIs: []string{id.RedirectURI},
	}
	if id.ConfiguredClientSecret != "" {
		rc.ClientSecret = id.ConfiguredClientSecret
		rc.AuthMethod = byoAuthMethod(as)
		rc.UsePKCE = as.SupportsPKCES256()
		return rc, nil
	}
	rc.AuthMethod = AuthMethodNone
	rc.UsePKCE = true
	return rc, nil
}

// byoAuthMethod picks the confidential client's token-endpoint auth method
// from what the AS advertises: Basic when advertised (or unadvertised — the
// RFC 8414 §2 default), Post when only Post is advertised.
func byoAuthMethod(as *AuthorizationServerMetadata) string {
	switch {
	case containsFold(as.TokenEndpointAuthMethodsSupported, AuthMethodBasic):
		return AuthMethodBasic
	case containsFold(as.TokenEndpointAuthMethodsSupported, AuthMethodPost):
		return AuthMethodPost
	default:
		return AuthMethodBasic
	}
}

// resolveByStoredRegistration reuses a caller-held registration (the
// register-once seam, tasks.md 4.3): no registration request is made.
func resolveByStoredRegistration(meta *Metadata, id ClientIdentity) (*ResolvedClient, error) {
	reg := id.StoredRegistration
	if reg == nil || reg.ClientID == "" {
		return nil, &ClientResolutionError{Strategy: StrategyDCR, ServerURL: meta.ServerURL, Detail: "the stored registration carries no client_id"}
	}
	rc := &ResolvedClient{
		Strategy:           StrategyDCR,
		ReusedRegistration: true,
		Registration:       reg,
		ClientID:           reg.ClientID,
		ClientSecret:       reg.ClientSecret,
		RedirectURIs:       reg.RedirectURIs,
	}
	switch {
	case reg.TokenEndpointAuthMethod != "":
		rc.AuthMethod = reg.TokenEndpointAuthMethod
	case reg.ClientSecret != "":
		rc.AuthMethod = AuthMethodBasic
	default:
		rc.AuthMethod = AuthMethodNone
	}
	rc.UsePKCE = rc.AuthMethod == AuthMethodNone || meta.AuthorizationServer.SupportsPKCES256()
	return rc, nil
}

// ---------------------------------------------------------------------------
// Client ID Metadata Document strategy (task 4.2)
// ---------------------------------------------------------------------------

// clientIDMetadataDocument is the RFC 7591-shaped client metadata document
// served at an HTTPS client_id URL (draft-ietf-oauth-client-id-metadata-
// document): the client_id used in the flows IS the document URL, and the
// document carries the registration facts (redirect_uris, auth method, …).
type clientIDMetadataDocument struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

// resolveByMetadataDocument fetches and validates the document at the URL
// client id. Validation is strict — the authorization server will hold the
// client to the document, so anything we cannot honor fails here with
// guidance instead of mid-flow:
//
//   - the document's client_id, when present, must be identical to the URL;
//   - redirect_uris must be declared and must include our derived callback
//     (exact string match, RFC 6749 §3.1.2.3);
//   - only public clients are supported (token_endpoint_auth_method empty or
//     "none") — a metadata document carries no secret we could present;
//   - grant_types / response_types, when declared, must include the
//     authorization-code shapes.
func (c *Client) resolveByMetadataDocument(ctx context.Context, meta *Metadata, id ClientIdentity) (*ResolvedClient, error) {
	fail := func(detail string, cause error) (*ResolvedClient, error) {
		return nil, &ClientResolutionError{Strategy: StrategyCIDM, ServerURL: meta.ServerURL, Detail: detail, Cause: cause}
	}
	doc := &clientIDMetadataDocument{}
	result := c.discovery.fetch(ctx, id.ConfiguredClientID, doc)
	if !result.fetched {
		return fail("the client-id metadata document at "+id.ConfiguredClientID+" could not be fetched", result.err)
	}
	if result.invalid {
		return fail("the client-id metadata document at "+id.ConfiguredClientID+" is not a valid JSON document", result.err)
	}
	if doc.ClientID != "" && doc.ClientID != id.ConfiguredClientID {
		return fail(fmt.Sprintf("the document's client_id %q does not match its URL %q", doc.ClientID, id.ConfiguredClientID), nil)
	}
	if len(doc.RedirectURIs) == 0 {
		return fail("the document declares no redirect_uris; this instance's callback cannot be registered", nil)
	}
	if !slices.Contains(doc.RedirectURIs, id.RedirectURI) {
		return fail(fmt.Sprintf("the document's redirect_uris do not include this instance's derived callback %q", id.RedirectURI), nil)
	}
	switch method := doc.TokenEndpointAuthMethod; method {
	case "", AuthMethodNone:
		// public client — the supported shape
	default:
		return fail(fmt.Sprintf("the document declares token_endpoint_auth_method %q; only public clients (none) are supported for metadata-document clients", method), nil)
	}
	if len(doc.GrantTypes) > 0 && !containsFold(doc.GrantTypes, "authorization_code") {
		return fail("the document's grant_types do not include authorization_code", nil)
	}
	if len(doc.ResponseTypes) > 0 && !containsFold(doc.ResponseTypes, "code") {
		return fail("the document's response_types do not include code", nil)
	}
	return &ResolvedClient{
		Strategy:            StrategyCIDM,
		ClientID:            id.ConfiguredClientID,
		AuthMethod:          AuthMethodNone,
		UsePKCE:             true,
		MetadataDocumentURL: id.ConfiguredClientID,
		RedirectURIs:        doc.RedirectURIs,
	}, nil
}

// ---------------------------------------------------------------------------
// Dynamic client registration (task 4.3, RFC 7591)
// ---------------------------------------------------------------------------

// registerDynamically POSTs the client metadata to the authorization server's
// registration_endpoint and returns the parsed registration for the caller to
// persist per server row (register once, reuse forever). The auth method is
// chosen from what the AS advertises: `none` (public + PKCE) is preferred
// when offered, then Basic, then Post, defaulting to Basic (the RFC 8414 §2
// default) when nothing is advertised.
func (c *Client) registerDynamically(ctx context.Context, meta *Metadata, id ClientIdentity) (*ResolvedClient, error) {
	fail := func(detail string, cause error) (*ResolvedClient, error) {
		return nil, &ClientResolutionError{Strategy: StrategyDCR, ServerURL: meta.ServerURL, Detail: detail, Cause: cause}
	}
	as := meta.AuthorizationServer
	authMethod := dcrAuthMethod(as)
	payload := map[string]any{
		"client_name":                "OnClaw (MCP)",
		"redirect_uris":              []string{id.RedirectURI},
		"token_endpoint_auth_method": authMethod,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	}
	// The resource's declared scopes are what the registration asks for; a
	// server that defines its own defaults (empty PRM scope list) is left
	// alone (design.md: presets carry empty scope sets where the server
	// defines defaults).
	if scopes := meta.Resource.ScopesSupported; len(scopes) > 0 {
		payload["scope"] = strings.Join(scopes, " ")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fail("the registration request could not be encoded", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, as.RegistrationEndpoint, bytes.NewReader(body))
	if err != nil {
		return fail("the registration endpoint URL is unusable", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fail("the registration endpoint could not be reached", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes))
	if err != nil {
		return fail("reading the registration response failed", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := fmt.Sprintf("the authorization server answered %s: %s", resp.Status, tokenEndpointError(respBody))
		return fail(detail, nil)
	}
	reg := &Registration{}
	if err := json.Unmarshal(respBody, reg); err != nil {
		return fail("the registration response was not a valid JSON client information document", err)
	}
	if reg.ClientID == "" {
		return fail("the registration response carried no client_id", nil)
	}
	if reg.TokenEndpointAuthMethod != "" {
		authMethod = reg.TokenEndpointAuthMethod
	}
	return &ResolvedClient{
		Strategy:     StrategyDCR,
		ClientID:     reg.ClientID,
		ClientSecret: reg.ClientSecret,
		AuthMethod:   authMethod,
		UsePKCE:      authMethod == AuthMethodNone || as.SupportsPKCES256(),
		Registration: reg,
		RedirectURIs: []string{id.RedirectURI},
	}, nil
}

// dcrAuthMethod prefers the public `none` (public client + PKCE) when the AS
// advertises it, then Basic, then Post; unadvertised defaults to Basic.
func dcrAuthMethod(as *AuthorizationServerMetadata) string {
	switch {
	case containsFold(as.TokenEndpointAuthMethodsSupported, AuthMethodNone):
		return AuthMethodNone
	case containsFold(as.TokenEndpointAuthMethodsSupported, AuthMethodBasic):
		return AuthMethodBasic
	case containsFold(as.TokenEndpointAuthMethodsSupported, AuthMethodPost):
		return AuthMethodPost
	default:
		return AuthMethodBasic
	}
}

// isHTTPSURL reports whether raw is an absolute https URL with a host — the
// only shape that routes a configured client id to the metadata-document
// strategy (draft-ietf-oauth-client-id-metadata-document §2: HTTPS only).
func isHTTPSURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return u.Scheme == "https" && u.Host != ""
}
