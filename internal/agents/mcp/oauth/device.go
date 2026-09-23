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

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// The RFC 8628 device-code grant (add-mcp-oauth-client task 4.5): the
// headless-instance flow for servers advertising device_authorization_endpoint
// and instances without a public base URL. The begin step returns the
// paste-back payload (verification URI + user code) for the UI; the poll loop
// honors the server-advised interval, grows it by the RFC 8628 §3.5 five
// seconds on slow_down, is bounded by the device code's expires_in, and
// propagates context cancellation.
const (
	// DefaultDevicePollInterval applies when the authorization response omits
	// interval (RFC 8628 §3.2's default).
	DefaultDevicePollInterval = 5 * time.Second
	// DefaultDeviceSlowDownStep is the RFC 8628 §3.5 slow_down penalty.
	DefaultDeviceSlowDownStep = 5 * time.Second
	// DefaultDeviceAuthorizationExpiry bounds the poll loop when the
	// authorization response omits expires_in.
	DefaultDeviceAuthorizationExpiry = 10 * time.Minute
)

// Terminal device-flow outcomes (AwaitDeviceToken).
var (
	// ErrDeviceExpired marks a device code that expired (the server answered
	// expired_token, or the expires_in bound elapsed while still pending).
	ErrDeviceExpired = errors.New("mcp oauth device code expired before authorization completed")
	// ErrDeviceDenied marks the user's refusal at the verification URI.
	ErrDeviceDenied = errors.New("mcp oauth device authorization was denied")
	// ErrDeviceTimedOut marks the poll loop hitting its hard duration bound.
	ErrDeviceTimedOut = errors.New("mcp oauth device authorization window elapsed")
)

// DeviceGrant is the parsed device authorization response (RFC 8628 §3.2):
// the paste-back payload plus the poll parameters. The poll fields are
// exported so callers (and tests) can drive AwaitDeviceToken with a literal
// grant.
type DeviceGrant struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	// ExpiresIn bounds the poll loop; 0 applies DefaultDeviceAuthorizationExpiry.
	ExpiresIn time.Duration
	// Interval is the server-advised poll interval; 0 applies
	// DefaultDevicePollInterval.
	Interval time.Duration
}

// DeviceOutcome is the typed result of one token poll (RFC 8628 §3.5).
type DeviceOutcome int

const (
	DeviceSuccess DeviceOutcome = iota
	DeviceAuthorizationPending
	DeviceSlowDown
	DeviceExpiredToken
	DeviceAccessDenied
)

// DevicePollResult is one poll's typed outcome; Tokens is set only on
// DeviceSuccess.
type DevicePollResult struct {
	Outcome DeviceOutcome
	Tokens  *TokenSet
	Detail  string
}

// BeginDeviceAuthorization starts the device flow at the discovered
// device_authorization_endpoint. The client id (and, for confidential
// clients, the secret — RFC 8628 §3.1 keeps token-endpoint authentication
// rules) rides the request body/header, never a URL. Scopes, when given,
// form the scope parameter.
func (c *Client) BeginDeviceAuthorization(ctx context.Context, meta *Metadata, client *ResolvedClient, scopes []string) (*DeviceGrant, error) {
	if meta == nil || client == nil {
		return nil, fmt.Errorf("%w: discovery metadata and a resolved client are required", domain.ErrInvalid)
	}
	if !meta.AuthorizationServer.SupportsDeviceFlow() {
		return nil, &ClientResolutionError{Strategy: client.Strategy, ServerURL: meta.ServerURL, Detail: "the authorization server advertises no device authorization endpoint"}
	}
	form := url.Values{}
	if len(scopes) > 0 {
		form.Set("scope", strings.Join(scopes, " "))
	}
	// Client authentication mutates the form (none/post put the id there) and
	// must precede the body encoding; the Basic header needs the request.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meta.AuthorizationServer.DeviceAuthorizationEndpoint, http.NoBody)
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
		return nil, fmt.Errorf("%w: the device authorization endpoint could not be reached: %v", ErrTokenExchange, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: reading the device authorization response failed: %v", ErrTokenExchange, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: the provider answered %s: %s", ErrTokenExchange, resp.Status, tokenEndpointError(body))
	}
	// interval/expires_in parse as JSON numbers into seconds; providers send
	// integers per the RFC, and a fractional value only ever arrives from a
	// fixture — truncation is harmless.
	var parsed struct {
		DeviceCode              string  `json:"device_code"`
		UserCode                string  `json:"user_code"`
		VerificationURI         string  `json:"verification_uri"`
		VerificationURIComplete string  `json:"verification_uri_complete"`
		ExpiresIn               float64 `json:"expires_in"`
		Interval                float64 `json:"interval"`
		Error                   string  `json:"error"`
		ErrorDescription        string  `json:"error_description"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("%w: the device authorization response was not valid JSON", ErrTokenExchange)
	}
	if parsed.DeviceCode == "" || parsed.UserCode == "" || parsed.VerificationURI == "" {
		detail := tokenEndpointError(body)
		if detail == "" {
			detail = "the device authorization response is missing required fields"
		}
		return nil, fmt.Errorf("%w: %s", ErrTokenExchange, detail)
	}
	return &DeviceGrant{
		DeviceCode:              parsed.DeviceCode,
		UserCode:                parsed.UserCode,
		VerificationURI:         parsed.VerificationURI,
		VerificationURIComplete: parsed.VerificationURIComplete,
		ExpiresIn:               secondsOrDefault(parsed.ExpiresIn, DefaultDeviceAuthorizationExpiry),
		Interval:                secondsOrDefault(parsed.Interval, DefaultDevicePollInterval),
	}, nil
}

// PollDeviceToken polls the token endpoint once for the device code. The five
// RFC 8628 §3.5 outcomes come back typed; transport failures and unexpected
// provider errors come back as errors.
func (c *Client) PollDeviceToken(ctx context.Context, meta *Metadata, client *ResolvedClient, grant *DeviceGrant) (*DevicePollResult, error) {
	if meta == nil || client == nil || grant == nil {
		return nil, fmt.Errorf("%w: metadata, client, and grant are required", domain.ErrInvalid)
	}
	form := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {grant.DeviceCode},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meta.AuthorizationServer.TokenEndpoint, http.NoBody)
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
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var ok struct {
			AccessToken      string `json:"access_token"`
			RefreshToken     string `json:"refresh_token"`
			ExpiresIn        int    `json:"expires_in"`
			Scope            string `json:"scope"`
			Iss              string `json:"iss"`
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if err := json.Unmarshal(body, &ok); err != nil {
			return nil, fmt.Errorf("%w: the token response was not valid JSON", ErrTokenExchange)
		}
		if ok.AccessToken == "" {
			// A 2xx without a token is not a shape this client accepts.
			return nil, fmt.Errorf("%w: the token response carried no access token", ErrTokenExchange)
		}
		if err := validateTokenIssuer(meta, ok.Iss); err != nil {
			return nil, err
		}
		return &DevicePollResult{Outcome: DeviceSuccess, Tokens: &TokenSet{
			AccessToken:  ok.AccessToken,
			RefreshToken: ok.RefreshToken,
			ExpiresIn:    time.Duration(ok.ExpiresIn) * time.Second,
			Scopes:       splitScope(ok.Scope),
			Issuer:       ok.Iss,
		}}, nil
	}

	var failure struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &failure)
	detail := failure.ErrorDescription
	if detail == "" {
		detail = failure.Error
	}
	switch failure.Error {
	case "authorization_pending":
		return &DevicePollResult{Outcome: DeviceAuthorizationPending}, nil
	case "slow_down":
		return &DevicePollResult{Outcome: DeviceSlowDown}, nil
	case "expired_token":
		return &DevicePollResult{Outcome: DeviceExpiredToken, Detail: detail}, nil
	case "access_denied":
		return &DevicePollResult{Outcome: DeviceAccessDenied, Detail: detail}, nil
	default:
		return nil, fmt.Errorf("%w: the provider answered %s: %s", ErrTokenExchange, resp.Status, tokenEndpointError(body))
	}
}

// AwaitDeviceToken runs the poll loop until a terminal outcome: success (the
// token set), the provider's expired_token / access_denied, the hard
// expires_in bound, or context cancellation (which propagates as ctx.Err()).
// Each slow_down grows the interval by the configured step
// (DefaultDeviceSlowDownStep — RFC 8628 §3.5's five seconds).
func (c *Client) AwaitDeviceToken(ctx context.Context, meta *Metadata, client *ResolvedClient, grant *DeviceGrant) (*TokenSet, error) {
	if grant == nil {
		return nil, fmt.Errorf("%w: a device grant is required", domain.ErrInvalid)
	}
	interval := grant.Interval
	if interval <= 0 {
		interval = DefaultDevicePollInterval
	}
	if c.deviceIntervalFloor > interval {
		interval = c.deviceIntervalFloor
	}
	bound := grant.ExpiresIn
	if bound <= 0 {
		bound = DefaultDeviceAuthorizationExpiry
	}
	deadline := time.Now().Add(bound)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		result, err := c.PollDeviceToken(ctx, meta, client, grant)
		if err != nil {
			return nil, err
		}
		switch result.Outcome {
		case DeviceSuccess:
			return result.Tokens, nil
		case DeviceExpiredToken:
			return nil, ErrDeviceExpired
		case DeviceAccessDenied:
			return nil, ErrDeviceDenied
		case DeviceSlowDown:
			interval += c.deviceSlowDownStep
		}
		// Hard bound: the next poll must start inside the device code's
		// lifetime (expires_in) — the loop never runs past it.
		if time.Now().Add(interval).After(deadline) {
			return nil, ErrDeviceTimedOut
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// secondsOrDefault converts a parsed seconds field; 0 selects the default.
func secondsOrDefault(seconds float64, fallback time.Duration) time.Duration {
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds * float64(time.Second))
}
