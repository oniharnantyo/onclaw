// Package whatsappcloud implements the WhatsApp Cloud API platform adapter
// for the gateway core (add-whatsapp-gateway design D1, cloud lane): a
// pure-Go client over net/http + encoding/json (no SDK dependency),
// webhook-only ingestion (the Cloud API has no polling surface), text and
// interactive quick-reply sends with permanent-delivery classification, a
// mark-read + typing call on the chat's last inbound message, media
// download, and the phone-number metadata health probe. Webhook ingress
// verifies X-Hub-Signature-256 with VerifySignature (HMAC-SHA256 over the
// raw body with the app secret). A fake Transport backs the unit tests
// (tasks.md 3.1-3.4).
package whatsappcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/oniharnantyo/onclaw/internal/gateways"
)

// DefaultAPIBase is the public Graph API endpoint (the Cloud API lives under
// graph.facebook.com, not api.whatsapp.com).
const DefaultAPIBase = "https://graph.facebook.com"

// APIVersion pins the Graph API version path (tasks.md 3.1): the messaging
// endpoints are versioned (/{version}/{phone_number_id}/messages), and
// unpinned versions would silently change wire behavior under us.
const APIVersion = "v21.0"

// Rate-limit retry policy, mirroring the telegram adapter's conventions: a
// 429 response is retried up to maxRateLimitRetries times, sleeping the
// Retry-After the API reports (capped so a hostile value cannot stall a
// sender for minutes).
const (
	maxRateLimitRetries  = 3
	maxRetryAfterSeconds = 30
)

// Transport operation names — the fake's scripting keys and the APIError.Op
// values.
const (
	OpSendMessage = "send_message"
	OpMarkRead    = "mark_read"
	OpPhoneNumber = "phone_number"
	OpMedia       = "media"
	OpDownload    = "download"
)

// Transport is the raw Cloud API seam (tasks.md 3.1): the messaging POSTs,
// the phone-number probe, and the media resolve/download pair. The HTTP
// implementation talks to graph.facebook.com; the fake backs the tests.
type Transport interface {
	// SendMessage POSTs one message payload (JSON, already addressed) to
	// /{phone_number_id}/messages and returns the raw response body. A 429
	// response is retried with the Retry-After backoff inside this call —
	// callers see only exhausted failures.
	SendMessage(ctx context.Context, payload []byte) (json.RawMessage, error)
	// MarkRead POSTs the mark-as-read + typing-indicator payload (Cloud API
	// typing is tied to marking the user's message read).
	MarkRead(ctx context.Context, payload []byte) error
	// PhoneNumber GETs /{phone_number_id} metadata (name/verified_name) —
	// the lane health probe.
	PhoneNumber(ctx context.Context) (json.RawMessage, error)
	// Media GETs a media id's metadata, which carries the download URL.
	Media(ctx context.Context, mediaID string) (json.RawMessage, error)
	// Download fetches the media bytes from the URL a Media result reported.
	Download(ctx context.Context, mediaURL string) ([]byte, error)
}

// Window-expired classification (add-whatsapp-gateway design D4): Graph
// reports a send outside the 24-hour customer-service window with error
// code 131047 (subcode family 2105006).
const (
	windowExpiredCode    = 131047
	windowExpiredSubcode = 2105006
)

// Permanent-delivery sentinels (add-whatsapp-gateway design D4). A window
// expiry can never succeed on retry — the outbox dead-letters the entry,
// never re-engages. ErrPermanentDelivery IS the core sentinel
// (gateways.ErrPermanentDelivery, the outbox's dead-letter seam): the
// adapter aliases rather than redefines it so errors.Is finds one value
// through either package while the dependency direction stays adapter→core.
var (
	// ErrPermanentDelivery wraps every non-retryable send failure: the
	// window-expired family and the other 4xx Graph codes.
	ErrPermanentDelivery = gateways.ErrPermanentDelivery
	// ErrWindowExpired wraps ErrPermanentDelivery for the 24-hour
	// customer-service window specifically (error 131047 and its subcode
	// family, design D4). No template re-engagement exists in this change.
	ErrWindowExpired = fmt.Errorf("%w: recipient is outside the 24-hour customer service window", ErrPermanentDelivery)
)

// APIError is one Graph API failure surfaced across the Transport boundary.
// Callers branch with errors.Is on ErrWindowExpired / ErrPermanentDelivery
// (via Unwrap) or the IsPermanent helper; rate-limit exhaustion carries the
// last reported Retry-After.
type APIError struct {
	Op         string // Transport operation (one of the Op* constants)
	Status     int    // HTTP status; 0 when constructed without one
	Code       int    // Graph error code
	Subcode    int    // Graph error_subcode
	Message    string
	Type       string // Graph error type (e.g. OAuthException)
	Details    string // Graph error_data.details
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("whatsapp cloud %s: http %d code %d subcode %d: %s", e.Op, e.Status, e.Code, e.Subcode, e.Message)
}

// windowExpired reports whether the failure is the 24-hour customer-service
// window (design D4): error 131047 with any subcode, or its canonical
// subcode 2105006 under any code.
func (e *APIError) windowExpired() bool {
	return e.Code == windowExpiredCode || e.Subcode == windowExpiredSubcode
}

// Permanent reports whether the failure is permanent-ish per Graph
// semantics: the window family plus every other 4xx (auth, malformed
// payload, unknown recipient). 5xx and transport timeouts stay retryable.
func (e *APIError) Permanent() bool {
	return e.windowExpired() || (e.Status >= 400 && e.Status < 500)
}

// Unwrap chains the permanent-delivery sentinels onto the error so
// errors.Is finds them through any %w wrapping: window expirations unwrap to
// ErrWindowExpired (which itself wraps ErrPermanentDelivery), other 4xx
// unwrap to ErrPermanentDelivery, and retryable failures unwrap to nil.
func (e *APIError) Unwrap() error {
	switch {
	case e.windowExpired():
		return ErrWindowExpired
	case e.Permanent():
		return ErrPermanentDelivery
	default:
		return nil
	}
}

// IsPermanent reports whether err is (or wraps) a permanent-delivery
// classification — the dead-letter signal for the outbox seam (design D4).
func IsPermanent(err error) bool {
	return errors.Is(err, ErrPermanentDelivery)
}

// graphErrorResponse is the Graph API error envelope.
type graphErrorResponse struct {
	Error *graphError `json:"error"`
}

type graphError struct {
	Message   string `json:"message"`
	Type      string `json:"type"`
	Code      int    `json:"code"`
	Subcode   int    `json:"error_subcode"`
	FBTraceID string `json:"fbtrace_id"`
	ErrorData *struct {
		Details string `json:"details"`
	} `json:"error_data"`
}

// apiErrorFromResponse builds the APIError for one non-retryable response:
// the Graph envelope is parsed when present, and a bare HTTP status stands
// in when the body is not JSON (proxies, HTML error pages).
func apiErrorFromResponse(op string, status int, retryAfter time.Duration, body []byte) *APIError {
	e := &APIError{Op: op, Status: status, RetryAfter: retryAfter}
	var envelope graphErrorResponse
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error != nil {
		e.Code = envelope.Error.Code
		e.Subcode = envelope.Error.Subcode
		e.Message = envelope.Error.Message
		e.Type = envelope.Error.Type
		if envelope.Error.ErrorData != nil {
			e.Details = envelope.Error.ErrorData.Details
		}
	}
	if e.Message == "" {
		e.Message = fmt.Sprintf("http %d", status)
	}
	return e
}

// httpTransport is the default Transport: bearer-authenticated JSON over
// HTTPS to graph.facebook.com. The access token rides only the
// Authorization header — never a URL query, never a log line.
type httpTransport struct {
	creds      Credentials
	apiBase    string
	apiVersion string
	client     *http.Client
}

// newHTTPTransport builds the default transport over parsed credentials.
func newHTTPTransport(creds Credentials, apiBase, apiVersion string, client *http.Client) *httpTransport {
	if apiBase == "" {
		apiBase = DefaultAPIBase
	}
	if apiVersion == "" {
		apiVersion = APIVersion
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &httpTransport{creds: creds, apiBase: apiBase, apiVersion: apiVersion, client: client}
}

// messagesEndpoint is the versioned messaging path shared by sends and the
// mark-read/typing call.
func (t *httpTransport) messagesEndpoint() string {
	return fmt.Sprintf("%s/%s/%s/messages", t.apiBase, t.apiVersion, t.creds.PhoneNumberID)
}

// SendMessage implements Transport: POST the JSON payload with the 429
// retry loop.
func (t *httpTransport) SendMessage(ctx context.Context, payload []byte) (json.RawMessage, error) {
	body, err := t.do(ctx, OpSendMessage, http.MethodPost, t.messagesEndpoint(), payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

// MarkRead implements Transport: POST the read + typing payload. The call
// returns no useful body.
func (t *httpTransport) MarkRead(ctx context.Context, payload []byte) error {
	_, err := t.do(ctx, OpMarkRead, http.MethodPost, t.messagesEndpoint(), payload)
	return err
}

// PhoneNumber implements Transport: GET the versioned phone-number
// metadata.
func (t *httpTransport) PhoneNumber(ctx context.Context) (json.RawMessage, error) {
	endpoint := fmt.Sprintf("%s/%s/%s", t.apiBase, t.apiVersion, t.creds.PhoneNumberID)
	body, err := t.do(ctx, OpPhoneNumber, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

// Media implements Transport: GET the versioned media metadata.
func (t *httpTransport) Media(ctx context.Context, mediaID string) (json.RawMessage, error) {
	endpoint := fmt.Sprintf("%s/%s/%s", t.apiBase, t.apiVersion, mediaID)
	body, err := t.do(ctx, OpMedia, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

// Download implements Transport: GET the media bytes from the resolved URL.
// Media URLs are pre-signed on Meta's CDN but still require the bearer
// token, so the credential rides the header here too.
func (t *httpTransport) Download(ctx context.Context, mediaURL string) ([]byte, error) {
	return t.do(ctx, OpDownload, http.MethodGet, mediaURL, nil)
}

// do executes one HTTP call with the 429 retry loop (mirroring the telegram
// transport's conventions): only 429s with a Retry-After are retried here;
// everything else is terminal at this layer (5xx/timeouts stay retryable
// for the outbox's reschedule, window expirations classify permanent
// upstream).
func (t *httpTransport) do(ctx context.Context, op, method, endpoint string, payload []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRateLimitRetries; attempt++ {
		if attempt > 0 {
			if err := sleepContext(ctx, retryAfterDelay(lastErr)); err != nil {
				return nil, err
			}
		}

		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
		if err != nil {
			return nil, fmt.Errorf("whatsapp cloud %s: build request: %w", op, err)
		}
		req.Header.Set("Authorization", "Bearer "+t.creds.AccessToken)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := t.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("whatsapp cloud %s: %w", op, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("whatsapp cloud %s: read body: %w", op, err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = apiErrorFromResponse(op, resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After")), body)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, apiErrorFromResponse(op, resp.StatusCode, 0, body)
		}
		return body, nil
	}
	return nil, lastErr
}

// parseRetryAfter parses the Retry-After header (seconds).
func parseRetryAfter(header string) time.Duration {
	secs, err := strconv.Atoi(header)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// retryAfterDelay extracts the capped Retry-After sleep from a rate-limit
// APIError; non-rate-limit errors sleep a fixed second (exhausted retries).
func retryAfterDelay(err error) time.Duration {
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.RetryAfter <= 0 {
		return time.Second
	}
	if apiErr.RetryAfter > maxRetryAfterSeconds*time.Second {
		return maxRetryAfterSeconds * time.Second
	}
	return apiErr.RetryAfter
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
