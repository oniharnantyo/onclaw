package agents

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// verifyTimeout bounds every provider endpoint probe: one cheap authenticated
// GET, never a model call (design D3).
const verifyTimeout = 10 * time.Second

// maxVerifyDetail caps the endpoint error surfaced in a failed verification:
// enough to identify the cause, short enough for one UI line.
const maxVerifyDetail = 200

// maxVerifyBodyBytes bounds the error body read from a failed probe.
const maxVerifyBodyBytes = 4096

// verifyClient is the shared HTTP client for provider endpoint probes. The
// timeout is the hard guarantee; a caller's context deadline applies on top.
var verifyClient = &http.Client{Timeout: verifyTimeout}

// anthropicVersion is the API version header the Anthropic-style models
// endpoint expects.
const anthropicVersion = "2023-06-01"

// VerifyCredentials makes one cheap authenticated GET against the provider
// type's models endpoint and reports whether the credential is accepted: any
// 2xx is a pass. A blank baseURL falls back to the type's default; the
// compatible types have no default, so a blank base fails verification with
// a clear detail rather than an error. A non-2xx response yields ok=false
// with the endpoint's error truncated; transport failures and unknown
// provider types return an error. Nothing is persisted and no tokens are
// spent.
func VerifyCredentials(ctx context.Context, providerType, baseURL, apiKey string) (bool, string, error) {
	if !IsKnownProviderType(providerType) {
		return false, "", fmt.Errorf("%w: %q", ErrProviderUnsupported, providerType)
	}

	base := baseURL
	if base == "" {
		base = DefaultBaseURL(providerType)
	}
	if base == "" {
		// Only the compatible types land here: they have no default base,
		// the user must supply one.
		return false, fmt.Sprintf("no base URL set for %q; a base URL is required for this provider type", providerType), nil
	}

	var path string
	setAuth := func(*http.Request) {}
	switch providerType {
	case ProviderAnthropic, ProviderAnthropicCompatible:
		// The Anthropic base is the origin; the /v1 is ours to append.
		path = "/v1/models"
		setAuth = func(r *http.Request) {
			r.Header.Set("x-api-key", apiKey)
			r.Header.Set("anthropic-version", anthropicVersion)
		}
	case ProviderOpenAI, ProviderOpenRouter, ProviderOpenAICompatible:
		// The OpenAI-style base already carries the version suffix.
		path = "/models"
		setAuth = func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+apiKey)
		}
	case ProviderGemini:
		path = "/v1beta/models?key=" + url.QueryEscape(apiKey)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, joinBase(base, path), nil)
	if err != nil {
		return false, "", fmt.Errorf("verify %s: %w", providerType, err)
	}
	setAuth(req)

	resp, err := verifyClient.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("verify %s: %w", providerType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, "", nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxVerifyBodyBytes))
	detail := truncateDetail(resp.Status + ": " + strings.TrimSpace(string(body)))
	return false, detail, nil
}

// joinBase appends an endpoint path to an API base, tolerating a trailing
// slash on the base.
func joinBase(base, path string) string {
	return strings.TrimRight(base, "/") + path
}

// truncateDetail shortens a verification failure detail to maxVerifyDetail
// runes, slicing on runes so multi-byte characters survive.
func truncateDetail(s string) string {
	runes := []rune(s)
	if len(runes) <= maxVerifyDetail {
		return s
	}
	return string(runes[:maxVerifyDetail]) + "…"
}
