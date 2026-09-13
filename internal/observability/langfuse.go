// Package observability wires optional Langfuse trace export into the agent
// turn path (integrate-langfuse-tracing D1/D2/D4/D5). Every exported turn is
// one Langfuse trace carrying model calls as generations and tool calls as
// spans, with attribution, secret masking, and deterministic per-run sampling.
//
// The capability exists only when configured: an absent configuration yields
// no handler and no runtime dependency — there is no always-on no-op handler
// (D1). Masking (D4) is mandatory and not bypassable: NewLangfuseTraceHandler
// always installs the centralized Mask; no construction knob disables it.
// Sampling (D5) is deterministic per run id: the trace id is pinned to a pure
// function of the turn id, so the upstream hash-based sampler and the local
// SampledIn decision agree exactly — a sampled-out turn exports nothing and
// persists no trace id.
//
// Composition contract for the wiring workers (integrate-langfuse-tracing
// tasks 2.1–2.4, 3.1, 3.3):
//
//	internal/cli (task 3.1 / 3.3):
//	    handle, err := observability.NewLangfuseTraceHandler(observability.LangfuseConfig{
//	        Host:       cfg.LangfuseHost,
//	        PublicKey:  cfg.LangfusePublicKey,
//	        SecretKey:  cfg.LangfuseSecretKey,
//	        SampleRate: cfg.LangfuseSampleRate,
//	    })
//	    if err != nil { return err }
//	    if handle != nil { // capability present; nil is the absent capability, never a guard
//	        runnerOpts = append(runnerOpts, agents.WithTraceHandler(handle.Callback(), handle.SampleRate()))
//	        // on graceful shutdown: handle.Flush()
//	    }
//
//	internal/agents runner, per turn (tasks 2.1–2.3):
//	    traceID := observability.TraceIDForRun(turnID)
//	    if observability.SampledIn(traceID, sampleRate) {
//	        // persist traceID on the run record — exactly the id the exported
//	        // trace carries, because ApplyTraceContext pins it via WithID.
//	    }
//	    ctx = observability.ApplyTraceContext(ctx, observability.TraceContext{
//	        SessionID: req.SessionID, UserID: req.UserID, WorkspaceID: req.WorkspaceID,
//	        AgentID: req.AgentID, TurnID: turnID, Origin: req.Origin, Input: req.Input,
//	    })
//
// ApplyTraceContext must run for every traced turn regardless of the sampling
// decision: the pinned trace id makes the upstream sampler drop the whole
// turn's event set atomically, while SampledIn alone decides link-out
// persistence — so a persisted trace id always targets a real trace.
package observability

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/callbacks/langfuse"
	"github.com/cloudwego/eino/callbacks"
)

// Export tuning defaults. FlushAt/FlushInterval mirror the batching defaults
// named in design.md; MaxRetry is set explicitly because the underlying
// backoff treats 0 as unbounded retries, and the spec bounds the attempt
// count.
const (
	// DefaultSampleRate exports every traced turn (D5).
	DefaultSampleRate = 1.0

	defaultFlushAt       = 15
	defaultFlushInterval = 500 * time.Millisecond
	defaultTimeout       = 30 * time.Second
	defaultMaxRetry      = 3
)

// LangfuseConfig carries the construction knobs resolved from server
// configuration (internal/config Langfuse fields). The zero Host/keys pair
// means the capability is absent.
type LangfuseConfig struct {
	Host       string
	PublicKey  string
	SecretKey  string
	SampleRate float64
}

// empty reports whether no Langfuse backend is configured at all: every
// credential and host field is blank after trimming.
func (c LangfuseConfig) empty() bool {
	return strings.TrimSpace(c.Host) == "" &&
		strings.TrimSpace(c.PublicKey) == "" &&
		strings.TrimSpace(c.SecretKey) == ""
}

// TraceHandler is the optional Langfuse export capability: the eino callback
// handler the runner attaches, the flush used at graceful shutdown, and the
// backend host the runs surface composes deep links from. Constructed only
// when configured (D1).
type TraceHandler struct {
	host    string
	handler callbacks.Handler
	flush   func()
	rate    float64
}

// NewLangfuseTraceHandler builds the Langfuse callback capability from
// configuration. An empty configuration returns (nil, nil) — the capability
// does not exist and nothing is wired (D1). A partially set configuration is
// an error (the composition root fails fast rather than silently disabling or
// half-wiring). The returned handler always carries the mandatory Mask
// function, batching defaults, bounded retries, and the configured
// deterministic sample rate.
func NewLangfuseTraceHandler(cfg LangfuseConfig) (*TraceHandler, error) {
	if cfg.empty() {
		return nil, nil
	}

	host := strings.TrimSpace(cfg.Host)
	publicKey := strings.TrimSpace(cfg.PublicKey)
	secretKey := strings.TrimSpace(cfg.SecretKey)
	if host == "" {
		return nil, errors.New("langfuse: host is required when any ONCLAW_LANGFUSE_* key is set (ONCLAW_LANGFUSE_HOST)")
	}
	if publicKey == "" {
		return nil, errors.New("langfuse: public key is required when any ONCLAW_LANGFUSE_* value is set (ONCLAW_LANGFUSE_PUBLIC_KEY)")
	}
	if secretKey == "" {
		return nil, errors.New("langfuse: secret key is required when any ONCLAW_LANGFUSE_* value is set (ONCLAW_LANGFUSE_SECRET_KEY)")
	}
	parsed, err := url.Parse(host)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("langfuse: host %q must be an http(s) URL (ONCLAW_LANGFUSE_HOST)", host)
	}

	sampleRate := cfg.SampleRate
	if sampleRate == 0 {
		sampleRate = DefaultSampleRate
	}
	if sampleRate < 0 || sampleRate > 1 {
		return nil, fmt.Errorf("langfuse: sample rate %g must be in (0, 1] (ONCLAW_LANGFUSE_SAMPLE_RATE)", sampleRate)
	}

	handler, flush := langfuse.NewLangfuseHandler(&langfuse.Config{
		Host:      host,
		PublicKey: publicKey,
		SecretKey: secretKey,

		// Mandatory, centralized secret masking (D4): not configurable, so a
		// misconfiguration can never export raw credentials.
		MaskFunc: Mask,

		SampleRate:    sampleRate,
		FlushAt:       defaultFlushAt,
		FlushInterval: defaultFlushInterval,
		Timeout:       defaultTimeout,
		MaxRetry:      defaultMaxRetry,
	})

	return &TraceHandler{
		host:    strings.TrimRight(host, "/"),
		handler: handler,
		flush:   flush,
		rate:    sampleRate,
	}, nil
}

// Callback returns the eino callback handler for the runner's callback chain
// (agents.WithTraceHandler consumes exactly this).
func (h *TraceHandler) Callback() callbacks.Handler {
	return h.handler
}

// Flush blocks until queued export events are sent. Called on graceful server
// shutdown (task 3.3) so traced turns finished just before shutdown are not
// dropped.
func (h *TraceHandler) Flush() {
	h.flush()
}

// Host returns the normalized backend host (no trailing slash) for
// langfuse_url composition (D6).
func (h *TraceHandler) Host() string {
	return h.host
}

// SampleRate returns the configured deterministic sample rate the handler was
// built with (D5). The runner applies the same rate to its local SampledIn
// persistence gate so link-out persistence and the upstream export decision
// never disagree.
func (h *TraceHandler) SampleRate() float64 {
	return h.rate
}

// TraceURL composes the runs-view deep link from the configured host and a
// persisted trace id (D6). The server composes it; the client never learns
// the host from its own configuration.
func TraceURL(host, traceID string) string {
	if host == "" || traceID == "" {
		return ""
	}
	return strings.TrimRight(host, "/") + "/trace/" + traceID
}
