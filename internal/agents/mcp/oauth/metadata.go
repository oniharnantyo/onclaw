package oauth

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// The two metadata well-knowns this chain resolves (RFC 9728 §3.1 and
// RFC 8414 §3.1 name the same insertion shape for both documents).
const (
	protectedResourceWellKnown   = "oauth-protected-resource"
	authorizationServerWellKnown = "oauth-authorization-server"
)

// Metadata is one resolved discovery result: the validated protected-resource
// metadata, the authorization server selected from it, and that server's
// RFC 8414 metadata. Discover hands out shared pointers from its cache, so
// callers must treat every field as read-only.
type Metadata struct {
	// ServerURL is the MCP server URL the chain ran for, as passed to
	// Discover (the cache key is its normalizeServerURL form).
	ServerURL string
	// Resource is the RFC 9728 protected-resource metadata, validated to
	// describe ServerURL (§3.3).
	Resource *ProtectedResourceMetadata
	// ResourceMetadataURL is the URL the document was fetched from — the
	// challenge's resource_metadata URL when the server challenged, else the
	// well-known location that hit.
	ResourceMetadataURL string
	// AuthorizationServerURL is the selected authorization server: the first
	// entry of Resource.AuthorizationServers whose metadata resolved.
	AuthorizationServerURL string
	// AuthorizationServer is the selected server's RFC 8414 metadata,
	// validated: issuer identical to AuthorizationServerURL (§4.3) and the
	// authorization-code grant's endpoints present.
	AuthorizationServer *AuthorizationServerMetadata
}

// ProtectedResourceMetadata is the RFC 9728 §2 protected-resource metadata
// document. Only the fields the client consumes are kept.
type ProtectedResourceMetadata struct {
	// Resource is the resource identifier this document describes (REQUIRED
	// per RFC 9728 §2) — validated against the server URL (§3.3) before use.
	Resource string `json:"resource"`
	// AuthorizationServers lists authorization-server issuer URLs able to
	// issue tokens for this resource, in preference order.
	AuthorizationServers []string `json:"authorization_servers,omitempty"`
	// ScopesSupported lists the scopes the resource expects access tokens to
	// carry (RFC 9728 §2); the AS metadata's list stays the grant-time source.
	ScopesSupported []string `json:"scopes_supported,omitempty"`
	// BearerMethodsSupported lists how bearers may be presented ("header", …).
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
	ResourceName           string   `json:"resource_name,omitempty"`
	ResourceDocumentation  string   `json:"resource_documentation,omitempty"`
}

// matchesResource reports whether the document describes serverURL per
// RFC 9728 §3.3: simple string comparison, tolerant of exactly one trailing
// slash on either side.
func (p *ProtectedResourceMetadata) matchesResource(serverURL string) bool {
	return sameResourceIdentifier(p.Resource, serverURL)
}

// AuthorizationServerMetadata is the RFC 8414 §2 authorization-server metadata
// document. registration_endpoint and token_endpoint_auth_methods_supported
// drive the client-strategy choice (BYO / client-id metadata document / DCR);
// code_challenge_methods_supported and device_authorization_endpoint gate
// PKCE and the device grant.
type AuthorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint,omitempty"`
	DeviceAuthorizationEndpoint       string   `json:"device_authorization_endpoint,omitempty"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	GrantTypesSupported               []string `json:"grant_types_supported,omitempty"`
	ResponseTypesSupported            []string `json:"response_types_supported,omitempty"`
	// AuthorizationResponseISSParameterSupported advertises that token
	// responses carry the RFC 9207 iss parameter (add-mcp-oauth-client 4.4
	// pin: when true, an absent iss in a token response is a validation
	// failure; when false, iss is optional but validated against the issuer
	// whenever present). Additive field — parsing defaults to false, so
	// discovery behavior is unchanged.
	AuthorizationResponseISSParameterSupported bool `json:"authorization_response_iss_parameter_supported,omitempty"`
}

// validate enforces what the grants cannot proceed without: an issuer
// identical to the URL discovery derived the document from (RFC 8414 §4.3 —
// simple string comparison, modulo one trailing slash) and absolute http(s)
// authorization + token endpoints (REQUIRED for the authorization-code
// grant). Optional endpoints are validated only when present.
func (a *AuthorizationServerMetadata) validate(issuerURL string) error {
	if a.Issuer == "" {
		return errors.New("issuer is required")
	}
	if !sameResourceIdentifier(a.Issuer, issuerURL) {
		return fmt.Errorf("issuer %q does not match the authorization server url %q (RFC 8414 issuer check)", a.Issuer, issuerURL)
	}
	if err := requireEndpoint("authorization_endpoint", a.AuthorizationEndpoint); err != nil {
		return err
	}
	if err := requireEndpoint("token_endpoint", a.TokenEndpoint); err != nil {
		return err
	}
	for name, endpoint := range map[string]string{
		"registration_endpoint":         a.RegistrationEndpoint,
		"device_authorization_endpoint": a.DeviceAuthorizationEndpoint,
	} {
		if endpoint == "" {
			continue
		}
		if err := validateAbsoluteHTTPURL(endpoint); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// SupportsPKCES256 reports whether the server advertises the S256
// code-challenge method (RFC 7636 §4.2); PKCE is used whenever it does.
func (a *AuthorizationServerMetadata) SupportsPKCES256() bool {
	return containsFold(a.CodeChallengeMethodsSupported, "S256")
}

// SupportsDeviceFlow reports whether the server advertises a device
// authorization endpoint — the headless-instance grant.
func (a *AuthorizationServerMetadata) SupportsDeviceFlow() bool {
	return a.DeviceAuthorizationEndpoint != ""
}

// wellKnownURLs derives the candidate metadata URLs for serverURL: the
// path-inserted form first — /.well-known/<wellKnown> inserted between the
// host and the path (RFC 9728 §3.1 and RFC 8414 §3.1) — then the root
// fallback (https://host/.well-known/<wellKnown>), the two shapes MCP's
// authorization spec directs clients to try. Query and fragment components
// are dropped; a server URL without a path yields the root form only.
func wellKnownURLs(serverURL, wellKnown string) ([]string, error) {
	u, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", serverURL, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%q is not an absolute http(s) url", serverURL)
	}
	path := strings.TrimRight(u.Path, "/")
	inserted := *u
	inserted.Path = "/.well-known/" + wellKnown + path
	inserted.RawQuery = ""
	inserted.Fragment = ""
	candidates := []string{inserted.String()}
	if path != "" {
		root := *u
		root.Path = "/.well-known/" + wellKnown
		root.RawQuery = ""
		root.Fragment = ""
		candidates = append(candidates, root.String())
	}
	return candidates, nil
}

// sameResourceIdentifier is the RFC 9728 §3.3 / RFC 8414 §4.3 identifier
// check: simple string comparison, modulo one trailing slash.
func sameResourceIdentifier(a, b string) bool {
	return stripTrailingSlash(a) == stripTrailingSlash(b)
}

func stripTrailingSlash(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), "/")
}

// validateAbsoluteHTTPURL requires an absolute http(s) URL with a host — the
// only endpoint shape this client dials.
func validateAbsoluteHTTPURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("parse %q: %w", raw, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%q is not an absolute http(s) url", raw)
	}
	return nil
}

// requireEndpoint checks one REQUIRED metadata endpoint (RFC 8414 §2).
func requireEndpoint(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required for the authorization-code grant", name)
	}
	if err := validateAbsoluteHTTPURL(value); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// containsFold reports whether values contains want, case-insensitively
// (metadata array members are case-insensitive tokens).
func containsFold(values []string, want string) bool {
	for _, v := range values {
		if strings.EqualFold(strings.TrimSpace(v), want) {
			return true
		}
	}
	return false
}
