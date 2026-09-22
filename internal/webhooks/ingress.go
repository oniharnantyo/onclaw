package webhooks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// OriginService is the run origin stamped on event-triggered runs (spec:
// service-authority attribution): the discriminator that marks a run as
// triggered by a service connection rather than a requesting user. The value
// is defined here — not imported from internal/agents — to keep this package
// an ordinary ingress client (the gateways' OriginWhatsApp precedent); the
// runner's origin normalizer recognizes it as a first-class origin, so run
// metadata and traces carry {authority: service, connection_id,
// connection_service, event} instead of a fabricated user attribution.
const OriginService = "service"

// RunAuthorityService is the authority discriminator every event run
// carries (ingress contract §6): {authority: service, connection_id,
// connection_service, event}.
const RunAuthorityService = "service"

// ingestMaxBodyBytes bounds the raw delivery body accepted for verification
// (memory safety on a public route). 25 MB is the documented provider
// delivery ceiling; nothing legitimate arrives larger.
const ingestMaxBodyBytes = 25 << 20

// RunRequest is one rendered service-event run submitted through RunnerPort:
// the bound target's coordinates, the labeled turn input, and the
// service-authority attribution (design.md D5, contract §6).
type RunRequest struct {
	// Authority is always RunAuthorityService — the discriminator run
	// metadata keys on.
	Authority string
	// WorkspaceID, ConnectionID, and ConnectionService name the exact
	// trigger: the connection (its recipe service id) whose webhook fired.
	WorkspaceID       string
	ConnectionID      string
	ConnectionService string
	// Event is the derived catalog event id ("pull_request.opened").
	Event string
	// AgentID is the bound agent the run executes as.
	AgentID string
	// SessionID is the run's transcript session: the bound thread's session
	// id for thread targets, a per-delivery session for channel targets.
	SessionID string
	// TargetKind is the bound discriminator (thread or channel); TargetID
	// the bound thread session or channel id.
	TargetKind string
	TargetID   string
	// Input is the labeled turn input (render.go).
	Input string
}

// RunnerPort is the run machinery seam (contract §5, the gateways
// RunSubmitter precedent): a narrow interface the composition root adapts
// *agents.Runner to — building the ExecRequest (acting identity, origin),
// owning the run's stream drain, and delivering channel-target runs' final
// text into the bound channel. The port deliberately returns only an error:
// the ingress acknowledges the provider after submission and never needs
// the stream.
type RunnerPort interface {
	RunEvent(ctx context.Context, req RunRequest) error
}

// Outcome is one delivery's ingress verdict, mapped onto HTTP by the
// transport layer:
//
//   - OutcomeDelivered: verified, deduplicated, accepted (queued) — ack 2xx.
//     A full backlog degrades to inline processing and still lands here: the
//     delivery id is already recorded, so a 503 would replay the provider's
//     retry into an occupied id and lose the event.
//   - OutcomeAcked: verified but no second turn — replay or unselected
//     event — ack 2xx without side effects.
//   - OutcomeUnknown: unknown workspace/connection, disabled connection,
//     bad or missing signature, or malformed delivery — one generic
//     non-enumerating "not found", zero side effects (design.md risks).
//   - OutcomeFailed: internal store/cipher failure — generic 500.
type Outcome int

const (
	OutcomeDelivered Outcome = iota
	OutcomeAcked
	OutcomeUnknown
	OutcomeFailed
)

// DeliverInput carries what the public transport resolves: the workspace id
// (the handler resolves the slug against the workspace store), the
// connection id from the route, and the raw delivery.
type DeliverInput struct {
	WorkspaceID  string
	ConnectionID string
	// Header is the delivery's headers; the ingress reads the recipe's
	// declared signature/event-type/delivery-id headers by NAME — provider
	// names are never hardcoded.
	Header http.Header
	// Body is the raw request body: signature verification and rendering
	// both run over these exact bytes.
	Body []byte
}

// Ingress is the verify → dedupe → queue → route pipeline (design.md D1).
type Ingress struct {
	webhooks    store.ConnectionWebhookStore
	connections store.Connections
	cipher      SecretCipher
	queue       *Queue
	runner      RunnerPort
	// now is the delivery-record clock (tests pin it).
	now func() time.Time
}

// IngressOption customizes an Ingress.
type IngressOption func(*Ingress)

// WithClock pins the delivery-record clock (tests).
func WithClock(now func() time.Time) IngressOption {
	return func(g *Ingress) {
		if now != nil {
			g.now = now
		}
	}
}

// NewIngress builds the pipeline. Dependencies are granular and positional
// (AGENTS.md): the webhook state port, the connections port, the secret
// cipher (the connection's decrypted secret is verification's key material),
// the bounded queue, and the runner port — every one required, resolved by
// the composition root, never nil.
func NewIngress(
	webhooks store.ConnectionWebhookStore,
	connections store.Connections,
	cipher SecretCipher,
	queue *Queue,
	runner RunnerPort,
	opts ...IngressOption,
) *Ingress {
	g := &Ingress{
		webhooks:    webhooks,
		connections: connections,
		cipher:      cipher,
		queue:       queue,
		runner:      runner,
		now:         time.Now,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Deliver runs one inbound delivery through the pipeline (tasks 2.1–2.3)
// and returns the HTTP-facing outcome. Semantics (ingress contract §1–§5):
//
//  1. Resolve the connection and its registered recipe — unknown
//     connection, non-webhook recipe, disabled connection, missing
//     signature material, failed verification, and malformed delivery ids
//     all collapse to OutcomeUnknown: one generic, non-enumerating verdict
//     with zero side effects.
//  2. Verify the signature constant-time against the decrypted secret,
//     per the recipe's declared scheme.
//  3. Derive the event id; an unselected event acks without a turn.
//  4. Record the delivery id (ack-after-persist, design.md D3): a replay
//     acks without a second turn.
//  5. Queue for asynchronous render + route; a full backlog degrades to
//     inline processing rather than dropping the already-recorded delivery
//     (the redelivery a dropped delivery would trigger replays into an
//     occupied id and lose the event — never acceptable).
func (g *Ingress) Deliver(ctx context.Context, in DeliverInput) (Outcome, error) {
	if in.WorkspaceID == "" || in.ConnectionID == "" || in.Header == nil || len(in.Body) == 0 {
		return OutcomeUnknown, nil
	}

	conn, err := g.connections.Get(ctx, in.WorkspaceID, in.ConnectionID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return OutcomeUnknown, nil
		}
		return OutcomeFailed, fmt.Errorf("webhook ingress: resolve connection: %w", err)
	}
	recipe := domain.RecipeByID(conn.Service)
	if recipe == nil || recipe.Webhooks == nil {
		// Not a webhook-capable connection: indistinguishable from unknown.
		return OutcomeUnknown, nil
	}
	state, err := g.webhooks.GetState(ctx, in.WorkspaceID, in.ConnectionID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return OutcomeUnknown, nil
		}
		return OutcomeFailed, fmt.Errorf("webhook ingress: read webhook state: %w", err)
	}
	if !state.Enabled {
		// Disabled ingestion is inert (spec: disabling stops ingestion) —
		// and still generic.
		return OutcomeUnknown, nil
	}

	secret, err := g.cipher.DecryptSecret(in.WorkspaceID, state.SecretCiphertext)
	if err != nil {
		return OutcomeFailed, fmt.Errorf("webhook ingress: decrypt secret: %w", err)
	}

	setup := recipe.Webhooks.Setup
	provided := in.Header.Get(setup.SignatureHeader)
	eventType := in.Header.Get(setup.EventTypeHeader)
	deliveryID := in.Header.Get(setup.DeliveryIDHeader)
	if provided == "" || eventType == "" || deliveryID == "" {
		// Missing provider headers: not a delivery this endpoint accepts —
		// same generic verdict, no verification oracle.
		return OutcomeUnknown, nil
	}
	if !VerifySignature(recipe.Webhooks.SignatureScheme, provided, secret, in.Body) {
		// Tampered or misconfigured: rejected with the generic verdict,
		// nothing persisted, no turn (spec: "Invalid signature rejected").
		return OutcomeUnknown, nil
	}

	eventID := DeriveEventID(eventType, in.Body)
	if !EventSelected(state.Events, eventID) {
		// Unselected events are acknowledged without an agent turn (spec:
		// "Unselected events ignored"). The delivery id is deliberately not
		// recorded — the dedupe window exists for turn-bearing deliveries.
		return OutcomeAcked, nil
	}

	accepted, err := g.webhooks.RecordDelivery(ctx, in.WorkspaceID, in.ConnectionID, deliveryID, g.now())
	if err != nil {
		return OutcomeFailed, fmt.Errorf("webhook ingress: record delivery: %w", err)
	}
	if !accepted {
		// Replay: ack the provider, no second turn (spec: "Replay
		// deduplicated").
		return OutcomeAcked, nil
	}

	job := &Delivery{
		workspaceID:  in.WorkspaceID,
		connectionID: in.ConnectionID,
		service:      conn.Service,
		eventID:      eventID,
		state:        state,
		recipe:       recipe,
		body:         in.Body,
	}
	if !g.queue.Enqueue(job) {
		// Backlog full. The delivery id is already recorded (ack-after-
		// persist), so backpressure-to-503 would replay the provider's
		// retry into an occupied id and lose the event forever. Degrade to
		// synchronous processing instead — the provider's ack waits one
		// render + submission, and the bounded queue resumes absorbing
		// bursts as soon as it drains.
		slog.WarnContext(ctx, "webhook ingress: queue full, processing delivery inline",
			"workspace_id", in.WorkspaceID, "connection_id", in.ConnectionID,
			"service", conn.Service, "event", eventID)
		g.ProcessDelivery(context.WithoutCancel(ctx), job)
	}
	return OutcomeDelivered, nil
}

// ReadBody drains the delivery body under the public route's size cap: the
// raw bytes verification and rendering both run over. An over-cap body is a
// transport failure the caller maps to the generic verdict.
func ReadBody(r io.Reader) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r, ingestMaxBodyBytes+1))
	if err != nil || len(body) > ingestMaxBodyBytes {
		return nil, false
	}
	return body, true
}

// ProcessDelivery renders one queued delivery through the recipe's template and
// routes the turn to the bound target (tasks 2.4/2.5). The render is
// fail-closed: a missing whitelisted field drops the delivery with an
// explicit error — never an empty turn (design.md risks) — and the failure is
// persisted on the connection's webhook state (best-effort) so the manage
// surface can surface it; the next successful render clears it.
func (g *Ingress) ProcessDelivery(ctx context.Context, job *Delivery) {
	tmpl, found := templateFor(job.recipe, job.eventID)
	if !found {
		// Derived event id outside the recipe catalog's templates. The
		// selection gate makes this unreachable for recorded recipes; if a
		// registration/regression ever lands it, fail closed.
		slog.ErrorContext(ctx, "webhook ingress: no template for derived event, dropping delivery", job.logAttrs()...)
		return
	}
	rendered, err := Render(*tmpl, job.body)
	if err != nil {
		// Fail-closed render failure (task 2.4): drop the delivery, no
		// turn, and persist the failure on the connection's observability
		// surface — structured log naming workspace, connection, service,
		// and event, then the best-effort last-error write.
		slog.ErrorContext(ctx, "webhook ingress: render failed, delivery dropped (fail closed)",
			append(job.logAttrs(), "error", err)...)
		g.persistLastError(ctx, job, err)
		return
	}
	g.clearLastError(ctx, job)

	req := RunRequest{
		Authority:         RunAuthorityService,
		WorkspaceID:       job.workspaceID,
		ConnectionID:      job.connectionID,
		ConnectionService: job.service,
		Event:             job.eventID,
		AgentID:           job.state.TargetAgentID,
		SessionID:         runSessionID(job),
		TargetKind:        job.state.TargetKind,
		TargetID:          job.state.TargetID,
		Input:             TurnInput(job.service, job.connectionID, job.eventID, rendered),
	}
	if err := g.runner.RunEvent(ctx, req); err != nil {
		// Submission failure after the ack window: the delivery id is
		// recorded, so a provider redelivery replays into it — the loss is
		// honest and logged, never silently retried into a double turn.
		slog.ErrorContext(ctx, "webhook ingress: run submission failed",
			append(job.logAttrs(), "error", err)...)
	}
}

// persistLastError records a fail-closed render drop on the connection's
// webhook state (task 2.4): the event id, the render error, and the drop
// timestamp — the "why is nothing happening" surface the manage view serves.
// Strictly best-effort: a store error here logs and returns, never panics or
// fails the (already dropped) delivery. A management write that raced the
// delivery — a rotation or disable between verification and processing —
// makes the job's state snapshot stale, and the write is skipped rather than
// clobbering operator changes; the next drop re-arms the surface against the
// fresh state.
func (g *Ingress) persistLastError(ctx context.Context, job *Delivery, renderErr error) {
	g.updateLastError(ctx, job, func(fresh *domain.ConnectionWebhook) {
		fresh.LastError = formatLastError(job.eventID, renderErr, g.now())
	})
}

// clearLastError wipes the last-error residue after a successful render —
// the connection renders cleanly again. A connection without residue skips
// the store write entirely.
func (g *Ingress) clearLastError(ctx context.Context, job *Delivery) {
	g.updateLastError(ctx, job, func(state *domain.ConnectionWebhook) {
		state.LastError = ""
	})
}

// updateLastError applies fn to the connection's fresh webhook state and
// persists it through the full-state write path when the residue changed.
// Best-effort end to end: read failures, stale-snapshot races, no-op writes,
// and write failures all return quietly (logged where diagnostic) — the
// delivery pipeline's correctness never depends on the residue.
func (g *Ingress) updateLastError(ctx context.Context, job *Delivery, apply func(*domain.ConnectionWebhook)) {
	fresh, err := g.webhooks.GetState(ctx, job.workspaceID, job.connectionID)
	if err != nil {
		slog.WarnContext(ctx, "webhook ingress: last-error persistence skipped (state read failed)",
			append(job.logAttrs(), "error", err)...)
		return
	}
	if !fresh.Enabled || fresh.SecretCiphertext != job.state.SecretCiphertext {
		// The connection was disabled or its secret rotated since the
		// delivery was verified: the snapshot is stale — skip, never clobber.
		return
	}
	before := fresh.LastError
	apply(fresh)
	if fresh.LastError == before {
		return
	}
	if err := g.webhooks.UpdateState(ctx, fresh); err != nil {
		slog.WarnContext(ctx, "webhook ingress: last-error persistence failed (best-effort)",
			append(job.logAttrs(), "error", err)...)
	}
}

// runSessionID resolves the run's transcript session: the bound thread's
// session id for thread targets (the run happens in the bound conversation),
// a per-delivery session for channel targets — the scheduler's per-fire
// session shape (each delivery owns its transcript; the bound channel sees
// the final text), with nanosecond timestamps so the runner's one-run-per-
// session guard never collides across deliveries.
func runSessionID(job *Delivery) string {
	if job.state.TargetKind == domain.ConnectionWebhookTargetThread {
		return job.state.TargetID
	}
	return fmt.Sprintf("whconn_%s_%d", job.connectionID, time.Now().UnixNano())
}

// templateFor finds the recipe's template for one derived event id.
func templateFor(recipe *domain.Recipe, eventID string) (*domain.RecipeWebhookTemplate, bool) {
	if recipe == nil || recipe.Webhooks == nil {
		return nil, false
	}
	for i := range recipe.Webhooks.Templates {
		if recipe.Webhooks.Templates[i].Event == eventID {
			return &recipe.Webhooks.Templates[i], true
		}
	}
	return nil, false
}

// logAttrs names the delivery in structured logs.
func (d *Delivery) logAttrs() []any {
	return []any{
		"workspace_id", d.workspaceID,
		"connection_id", d.connectionID,
		"service", d.service,
		"event", d.eventID,
	}
}
