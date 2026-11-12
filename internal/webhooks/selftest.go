package webhooks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

// selfTestTimeout bounds the self-test's HTTP round-trip: the Test action is
// a user-facing button and must never hang the UI on a wedged listener.
const selfTestTimeout = 10 * time.Second

// URL builds the delivery URL for an endpoint on a listener address
// (host:port), including the path-token segment when the endpoint verifies
// one. The service layer composes it with the listener's bound address for
// the generated-URL feature.
func URL(addr string, ep store.WebhookEndpoint) string {
	var b strings.Builder
	b.WriteString("http://")
	b.WriteString(addr)
	b.WriteString(routePrefix)
	b.WriteString(ep.ID)
	if token := pathTokenSecret(ep.Verifiers); token != "" {
		b.WriteString("/")
		b.WriteString(token)
	}
	return b.String()
}

// Test exercises the whole delivery path for one endpoint
// (add-webhook-triggers tasks 6.2, design D8): it composes a synthetic JSON
// payload, signs it with the endpoint's own credentials, and POSTs it
// through the real listener, so address binding, authentication, and
// dispatch are proven in one step. The delivery lands in the endpoint's log
// attributed via = "self-test". A failed test returns the surfaced error —
// listener down, signing defect, or the pipeline's refusal status — and the
// failed attempt still appears in the log when it reached the pipeline.
func (s *Service) Test(ctx context.Context, endpointID string) error {
	ep, err := s.rec.store.Webhooks().GetEndpoint(ctx, endpointID)
	if err != nil {
		return fmt.Errorf("webhook self-test: load endpoint: %w", err)
	}

	state := s.State()
	if !state.Serving {
		if state.BindError != "" {
			return fmt.Errorf("webhook self-test: listener is not serving: %s", state.BindError)
		}
		return fmt.Errorf("webhook self-test: listener is not serving (gateway stopped or not attached)")
	}

	now := s.rec.now()
	body := []byte(`{"self_test":true,"endpoint":` + jsonEscape(ep.Name) + `,"sent_at":"` + now.UTC().Format(time.RFC3339) + `"}`)

	deliveryID := "selftest-" + strconv.FormatInt(now.UnixNano(), 10)
	headers, err := signDeliverySet(verifierConfigs(ep.Verifiers), body, now, deliveryID)
	if err != nil {
		return err
	}
	headers.Set(selfTestHeader, "1")

	url := URL(state.BoundAddr, *ep)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook self-test: build request: %w", err)
	}
	req.Header = headers

	client := &http.Client{Timeout: selfTestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook self-test: delivery failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Read a bounded refusal snippet so the surfaced error names why.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		detail := strings.TrimSpace(string(snippet))
		if detail == "" {
			return fmt.Errorf("webhook self-test: the receiver answered %s", resp.Status)
		}
		return fmt.Errorf("webhook self-test: the receiver answered %s: %s", resp.Status, detail)
	}
	return nil
}
