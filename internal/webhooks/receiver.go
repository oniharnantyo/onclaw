package webhooks

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

// routePrefix is the receiver's only route family: POST /webhooks/{endpointID}
// with an optional final segment carrying the path-token credential for
// path-verifier endpoints (add-webhook-triggers design D1: the receiver is
// its own listener, so it owns the whole URL space).
const routePrefix = "/webhooks/"

// selfTestHeader marks a delivery as a self-test (add-webhook-triggers
// design D8) so the delivery log attributes it via = "self-test" instead of
// the authenticating verifier's label. A forged marker only relabels an
// already-authenticated delivery in the log — authentication is unaffected —
// so the header carries no trust.
const selfTestHeader = "X-Buddies-Self-Test"

// retryAfterSeconds is the Retry-After hint the paused gate answers with:
// thirty seconds, short enough that senders honoring it re-deliver promptly
// once the pause resolves.
const retryAfterSeconds = "30"

// Receiver is the webhook accept pipeline as an http.Handler
// (add-webhook-triggers tasks 4.1): resolve endpoint → enabled gate →
// gateway pause gate → size-capped raw-body read → verify credentials →
// dedupe → dead-target re-check → dispatch, recording every outcome into the
// endpoint's bounded delivery log. It is safe for concurrent use; the
// lifecycle Service owns its listener.
type Receiver struct {
	store   store.Store
	runs    Runs
	groups  GroupSender
	gw      GatewayState
	now     func() time.Time
	bodyCap int64
	// listenAddr carries WithListenAddr's listener override for the wrapping
	// Service, consumed by NewService — the receiver itself never binds.
	listenAddr string
}

// Option configures a Receiver (and the Service wrapping it) at construction.
type Option func(*Receiver)

// WithClock overrides the receiver's delivery clock, used for session titles
// and delivery timestamps. Production leaves the default (time.Now); tests
// inject a fixed clock for deterministic expectations.
func WithClock(c func() time.Time) Option {
	return func(r *Receiver) { r.now = c }
}

// WithBodyLimit overrides the delivery size cap from DefaultBodyLimit.
func WithBodyLimit(limit int64) Option {
	return func(r *Receiver) { r.bodyCap = limit }
}

// NewReceiver builds the accept pipeline over its persistence, its two
// dispatch seams, and the gateway state read. A nil gw reads as Running —
// direct-http tests without a gateway serve normally; production always
// wires one.
func NewReceiver(st store.Store, runs Runs, groups GroupSender, gw GatewayState, opts ...Option) *Receiver {
	r := &Receiver{
		store:   st,
		runs:    runs,
		groups:  groups,
		gw:      gw,
		now:     time.Now,
		bodyCap: DefaultBodyLimit,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// setGateway wires the gateway state read at Attach time. Called before the
// listener ever serves, so the read in the pause gate needs no further
// synchronization; a nil gateway reads as Running.
func (r *Receiver) setGateway(gw GatewayState) { r.gw = gw }

// ServeHTTP answers one delivery. Non-POST methods and paths outside
// /webhooks/ are plain 404/405 answers with nothing recorded — there is no
// endpoint to attribute them to.
func (r *Receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if !strings.HasPrefix(req.URL.Path, routePrefix) {
		http.NotFound(w, req)
		return
	}
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rest := strings.TrimPrefix(req.URL.Path, routePrefix)
	endpointID, pathToken, _ := strings.Cut(rest, "/")
	if endpointID == "" {
		http.NotFound(w, req)
		return
	}
	r.handle(w, req, endpointID, pathToken)
}

// verifierConfigs maps the endpoint's stored verifier rows onto the engine
// configs Authorize runs, preserving the stored order.
func verifierConfigs(rows []store.WebhookVerifier) []VerifierConfig {
	cfgs := make([]VerifierConfig, len(rows))
	for i, row := range rows {
		cfgs[i] = VerifierConfig{
			Kind:            VerifierKind(row.Kind),
			Preset:          row.Preset,
			Secret:          row.Secret,
			HeaderName:      row.HeaderName,
			TimestampHeader: row.TimestampHeader,
			Algo:            HMACAlgo(row.Algo),
			Encoding:        HMACEncoding(row.Encoding),
			Prefix:          row.Prefix,
			Scheme:          TsHMACScheme(row.Scheme),
		}
	}
	return cfgs
}

// pathTokenSecret returns the endpoint's first path-token verifier secret —
// the URL segment credential — or "" when the endpoint verifies none.
func pathTokenSecret(rows []store.WebhookVerifier) string {
	for _, row := range rows {
		if VerifierKind(row.Kind) == KindPath {
			return row.Secret
		}
	}
	return ""
}

// record appends one delivery row to the endpoint's bounded log. A recording
// failure is logged and dropped: it never changes the HTTP answer the sender
// already earned.
func (r *Receiver) record(ctx context.Context, row store.WebhookDelivery) {
	if err := r.store.Webhooks().AppendDelivery(ctx, &row); err != nil {
		logRecordFailure(ctx, row.EndpointID, row.Outcome, err)
	}
}

// writeJSON answers the delivery with a small JSON body.
func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
