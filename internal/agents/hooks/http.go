package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents/tools"
)

// maxHookHTTPBodyBytes caps how much of a webhook response body is read for
// decision parsing; decision objects are tiny and anything past the cap is
// ignored (the connection is still drained-and-closed, not left hanging).
const maxHookHTTPBodyBytes = 1 << 20

// httpHookConfig is the decrypted http handler config shape (see the pinned
// shapes in secrets.go). Header values arrive as plaintext — the registry
// opens sealed values before handlers run.
type httpHookConfig struct {
	URL     string      `json:"url"`
	Headers []configRow `json:"headers,omitempty"`
}

// configRow is one name-keyed config row (headers for http, env for command).
type configRow struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// executeHTTP POSTs the event payload to the hook's webhook (D9 HTTP
// handler): Content-Type application/json, event and delivery id headers,
// plus the configured headers in plaintext. A 2xx body that parses as a
// decision object is honored; any other 2xx means allow; a non-2xx status or
// a transport error is a failure for the hook's on_failure policy. Outbound
// requests go through the same outbound-fetch guards as web.fetch — URL
// validation up front, and the guarded client re-validating every resolved
// address and redirect hop.
func (r *Registry) executeHTTP(ctx context.Context, cfg json.RawMessage, ev Event, _ HookRef, _ time.Duration) (Result, error) {
	var conf httpHookConfig
	if err := json.Unmarshal(cfg, &conf); err != nil {
		return Result{}, fmt.Errorf("hook http: decode config: %w", err)
	}

	u, err := url.Parse(strings.TrimSpace(conf.URL))
	if err != nil {
		return Result{}, fmt.Errorf("hook http: invalid url: %w", err)
	}
	if err := tools.ValidateOutboundURL(u, r.httpAllowPrivate); err != nil {
		return Result{}, fmt.Errorf("hook http: %s", guardErrorText(err))
	}

	payload, err := json.Marshal(ev)
	if err != nil {
		return Result{}, fmt.Errorf("hook http: encode event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
	if err != nil {
		return Result{}, fmt.Errorf("hook http: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Onclaw-Event", ev.Event)
	req.Header.Set("X-Onclaw-Delivery", ev.DeliveryID)
	for _, row := range conf.Headers {
		req.Header.Set(row.Name, row.Value)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("hook http: delivery failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxHookHTTPBodyBytes))
	status := resp.StatusCode

	if status < 200 || status > 299 {
		return Result{HTTPStatus: &status}, fmt.Errorf("hook http: endpoint returned status %d", status)
	}
	if decision, ok := ParseDecisionJSON(body); ok {
		return Result{Decision: decision.Decision, Reason: decision.Reason, HTTPStatus: &status}, nil
	}
	// Any other 2xx (empty body, log text, non-decision JSON) means allow.
	return Result{Decision: "allow", HTTPStatus: &status}, nil
}

// guardErrorText strips the web.fetch prefix from the shared guard's errors
// so hook audit lines name the hook path, not the fetch tool.
func guardErrorText(err error) string {
	msg := err.Error()
	msg = strings.TrimPrefix(msg, "web.fetch: ")
	return msg
}
