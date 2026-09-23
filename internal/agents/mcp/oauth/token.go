package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultTokenHTTPTimeout bounds each token-endpoint / registration /
// device-endpoint round trip. It mirrors the connections OAuth service's
// DefaultTokenHTTPTimeout (15s) — one bound for every grant request.
const DefaultTokenHTTPTimeout = 15 * time.Second

// maxTokenResponseBytes bounds a token / registration response read (a few KiB
// in practice; the cap keeps a hostile endpoint from streaming forever).
const maxTokenResponseBytes = 1 << 20

// Token endpoint auth methods (RFC 6749 §2.3.1 and the RFC 7591 §2 metadata
// vocabulary). The method is chosen per client type from what the
// authorization server advertises (design.md D3).
const (
	AuthMethodNone  = "none"
	AuthMethodBasic = "client_secret_basic"
	AuthMethodPost  = "client_secret_post"
)

// ErrTokenExchange marks a token-endpoint refusal: the provider answered with
// an error, a non-2xx, or an unusable body. Wrap sites carry the provider's
// error detail verbatim — and nothing else: secrets never ride these errors.
var ErrTokenExchange = errors.New("mcp oauth token request failed")

// TokenSet is a parsed token-endpoint response (RFC 6749 §5.1) with the RFC
// 9207 iss echo validated. Plaintext token material — memory only, never
// logged, encrypted before persistence by the CredentialStore seam.
type TokenSet struct {
	AccessToken  string
	RefreshToken string
	// ExpiresIn is 0 when the provider declared no expiry (the token then
	// never refreshes and never expires on our side).
	ExpiresIn time.Duration
	// Scopes is the normalized granted-scope echo; empty means the provider
	// echoed nothing (the stored granted set then stays).
	Scopes []string
	// Issuer is the validated iss echo (RFC 9207); empty when the response
	// carried none and the AS does not require it.
	Issuer string
}

// expiresAtFromSeconds converts a token response's expires_in seconds into an
// absolute expiry; 0 (no declared expiry) yields nil.
func expiresAtFromSeconds(seconds int) *time.Time {
	if seconds <= 0 {
		return nil
	}
	expiresAt := time.Now().Add(time.Duration(seconds) * time.Second)
	return &expiresAt
}

// splitScope normalizes a provider's scope echo (space- or comma-separated,
// RFC 6749 §3.3) into the stored array shape; the result is never nil.
func splitScope(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// validateTokenIssuer enforces the RFC 9207 iss contract (add-mcp-oauth-client
// 4.4 pin): an iss on the response MUST match the authorization server's
// registered issuer; an ABSENT iss is tolerated only when the authorization
// server does not advertise `authorization_response_iss_parameter_supported`.
// An advertising server that omits iss is a rejection — the response may not
// belong to the server discovery selected.
func validateTokenIssuer(meta *Metadata, iss string) error {
	if iss == "" {
		if meta.AuthorizationServer.AuthorizationResponseISSParameterSupported {
			return fmt.Errorf("%w: the authorization server advertises the RFC 9207 iss parameter but the token response omitted it", ErrTokenExchange)
		}
		return nil
	}
	if !sameResourceIdentifier(iss, meta.AuthorizationServer.Issuer) {
		return fmt.Errorf("%w: the token response issuer %q does not match the authorization server issuer %q (RFC 9207)", ErrTokenExchange, iss, meta.AuthorizationServer.Issuer)
	}
	return nil
}

// applyClientAuth writes the resolved client's credentials into one grant
// request per its token-endpoint auth method (RFC 6749 §2.3.1): `none` puts
// only the client_id in the body; `client_secret_post` puts both in the body;
// `client_secret_basic` puts the credentials in the Authorization header
// (form-urlencoded first, per the RFC and the x/oauth2 precedent) and nothing
// in the body. Call it BEFORE encoding the form body — it mutates the form.
// The secret never lands anywhere else on the request.
func applyClientAuth(req *http.Request, form url.Values, client *ResolvedClient) {
	switch client.AuthMethod {
	case AuthMethodBasic:
		req.SetBasicAuth(url.QueryEscape(client.ClientID), url.QueryEscape(client.ClientSecret))
	case AuthMethodPost:
		form.Set("client_id", client.ClientID)
		form.Set("client_secret", client.ClientSecret)
	default: // AuthMethodNone and any unknown value: public client, id only.
		form.Set("client_id", client.ClientID)
	}
}

// postToken POSTs one form-encoded grant request, parses the JSON token
// response, and validates the RFC 9207 issuer. Provider errors surface with
// the provider's message verbatim; secrets never do (they exist only inside
// the request this function issues).
func (c *Client) postToken(ctx context.Context, tokenEndpoint string, form url.Values, client *ResolvedClient, meta *Metadata) (*TokenSet, error) {
	// Client authentication mutates the form (none/post) and must precede the
	// body encoding; the Basic header needs the request, so it is applied
	// right after.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTokenExchange, err)
	}
	applyClientAuth(req, form, client)
	formBody := form.Encode()
	req.Body = io.NopCloser(strings.NewReader(formBody))
	req.ContentLength = int64(len(formBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: the token endpoint could not be reached: %v", ErrTokenExchange, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: reading the token response failed: %v", ErrTokenExchange, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: the provider answered %s: %s", ErrTokenExchange, resp.Status, tokenEndpointError(body))
	}

	var parsed struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int    `json:"expires_in"`
		Scope            string `json:"scope"`
		Iss              string `json:"iss"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("%w: the token response was not valid JSON", ErrTokenExchange)
	}
	if parsed.AccessToken == "" {
		detail := tokenEndpointError(body)
		if detail == "" {
			detail = "the token response carried no access token"
		}
		return nil, fmt.Errorf("%w: %s", ErrTokenExchange, detail)
	}
	if err := validateTokenIssuer(meta, parsed.Iss); err != nil {
		return nil, err
	}
	return &TokenSet{
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		ExpiresIn:    time.Duration(parsed.ExpiresIn) * time.Second,
		Scopes:       splitScope(parsed.Scope),
		Issuer:       parsed.Iss,
	}, nil
}

// RefreshTokens exchanges a stored refresh token for a fresh token set (RFC
// 6749 §6) at the discovery-resolved token endpoint. An omitted refresh token
// in the response keeps the stored one — rotation vs replay is the caller's
// merge (lifecycle.go).
func (c *Client) RefreshTokens(ctx context.Context, in RefreshInput) (*TokenSet, error) {
	if in.Meta == nil || in.Client == nil {
		return nil, fmt.Errorf("%w: discovery metadata and a resolved client are required", ErrTokenExchange)
	}
	if strings.TrimSpace(in.RefreshToken) == "" {
		return nil, fmt.Errorf("%w: no refresh token to present", ErrTokenExchange)
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {in.RefreshToken},
	}
	return c.postToken(ctx, in.Meta.AuthorizationServer.TokenEndpoint, form, in.Client, in.Meta)
}

// RefreshInput is one refresh-grant request: the discovery result the stored
// tokens were issued under, the resolved client, and the plaintext refresh
// token (decrypted by the caller's CredentialStore seam).
type RefreshInput struct {
	Meta         *Metadata
	Client       *ResolvedClient
	RefreshToken string
}

// tokenEndpointError extracts the provider's OAuth error message from a
// failure body — the human detail surfaces, nothing else does (the same
// convention as the connections OAuth service).
func tokenEndpointError(body []byte) string {
	var parsed struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && (parsed.ErrorDescription != "" || parsed.Error != "") {
		if parsed.ErrorDescription != "" {
			return parsed.ErrorDescription
		}
		return parsed.Error
	}
	return strings.TrimSpace(string(body))
}
