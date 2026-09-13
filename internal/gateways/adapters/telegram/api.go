// Package telegram implements the Telegram Bot API platform adapter for the
// gateway core (integrate-telegram-gateway design D1): a pure-Go client over
// net/http + encoding/json (no SDK dependency), long-polling and webhook
// ingestion, HTML send/edit with plain-text fallback and rate-limit backoff,
// inline-keyboard approval cards, and file download. A fake Transport backs
// the unit tests (tasks.md 2.2).
package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultAPIBase is the public Telegram Bot API endpoint.
const DefaultAPIBase = "https://api.telegram.org"

// Rate-limit retry policy (tasks 3.4): a 429 response is retried up to
// maxRateLimitRetries times, sleeping the Retry-After the API reports
// (capped so a hostile value cannot stall a poller for minutes).
const (
	maxRateLimitRetries  = 3
	maxRetryAfterSeconds = 30
)

// Transport is the raw Bot API seam (tasks 2.2): one method call plus file
// download. The HTTP implementation talks to the public API; the fake backs
// the tests.
type Transport interface {
	// Call executes one Bot API method with form parameters and returns the
	// decoded "result" envelope. A 429 response is retried with the
	// Retry-After backoff inside this call — callers see only exhausted
	// failures.
	Call(ctx context.Context, method string, params url.Values) (json.RawMessage, error)
	// Download fetches a file body by the file_path a getFile result
	// reported.
	Download(ctx context.Context, filePath string) ([]byte, error)
}

// APIError is one Bot API failure surfaced across the Transport boundary.
// Callers branch on IsParseError for the plain-text fallback (design D5);
// rate-limit exhaustion carries the last reported Retry-After.
type APIError struct {
	Method      string
	Code        int
	Description string
	RetryAfter  int // seconds; set on 429
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram %s: %d %s", e.Method, e.Code, e.Description)
}

// IsParseError reports whether the failure is Telegram rejecting formatted
// text ("can't parse entities") — the single plain-text retry trigger.
func IsParseError(err error) bool {
	apiErr, ok := err.(*APIError)
	return ok && strings.Contains(apiErr.Description, "can't parse entities")
}

// apiResponse is the Bot API response envelope.
type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
	Parameters  *struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// httpTransport is the default Transport: HTTPS form posts to the Bot API.
type httpTransport struct {
	token   string
	apiBase string
	client  *http.Client
}

// newHTTPTransport builds the default transport.
func newHTTPTransport(token, apiBase string, client *http.Client) *httpTransport {
	if apiBase == "" {
		apiBase = DefaultAPIBase
	}
	if client == nil {
		client = &http.Client{Timeout: 65 * time.Second}
	}
	return &httpTransport{token: token, apiBase: apiBase, client: client}
}

// Call implements Transport: POST the parameters, decode the envelope, and
// retry 429s with the reported Retry-After (capped).
func (t *httpTransport) Call(ctx context.Context, method string, params url.Values) (json.RawMessage, error) {
	endpoint := fmt.Sprintf("%s/bot%s/%s", t.apiBase, t.token, method)

	var lastErr error
	for attempt := 0; attempt <= maxRateLimitRetries; attempt++ {
		if attempt > 0 {
			if sleepErr := sleepContext(ctx, retryAfterDelay(lastErr)); sleepErr != nil {
				return nil, sleepErr
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(params.Encode()))
		if err != nil {
			return nil, fmt.Errorf("telegram %s: build request: %w", method, err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := t.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("telegram %s: %w", method, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("telegram %s: read body: %w", method, err)
		}

		var envelope apiResponse
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("telegram %s: decode response (http %d): %w", method, resp.StatusCode, err)
		}
		if envelope.OK {
			return envelope.Result, nil
		}

		lastErr = &APIError{
			Method:      method,
			Code:        envelope.ErrorCode,
			Description: envelope.Description,
		}
		if envelope.Parameters != nil && envelope.Parameters.RetryAfter > 0 {
			lastErr.(*APIError).RetryAfter = envelope.Parameters.RetryAfter
		}
		// Only 429 with a Retry-After is retried here; everything else is
		// terminal (parse failures fall back one level up).
		if envelope.ErrorCode != http.StatusTooManyRequests || envelope.Parameters == nil || envelope.Parameters.RetryAfter <= 0 {
			return nil, lastErr
		}
	}
	return nil, lastErr
}

// retryAfterDelay extracts the capped Retry-After sleep from a rate-limit
// APIError; non-rate-limit errors sleep a fixed second (exhausted retries).
func retryAfterDelay(err error) time.Duration {
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.RetryAfter <= 0 {
		return time.Second
	}
	if apiErr.RetryAfter > maxRetryAfterSeconds {
		return maxRetryAfterSeconds * time.Second
	}
	return time.Duration(apiErr.RetryAfter) * time.Second
}

// Download implements Transport: GET the file endpoint by file_path.
func (t *httpTransport) Download(ctx context.Context, filePath string) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/file/bot%s/%s", t.apiBase, t.token, filePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("telegram download: build request: %w", err)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram download: http %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// sleepContext waits for d or until ctx is done.
func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
