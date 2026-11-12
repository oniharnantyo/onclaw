package store

import "context"

// WebhookDeliveryRetention is the per-endpoint bound on the delivery log:
// appending past the bound evicts the endpoint's oldest rows first
// (add-webhook-triggers task 1.3).
const WebhookDeliveryRetention = 50

// WebhookStore persists the webhook trigger aggregates (add-webhook-triggers
// design D7): endpoints, their ordered verifier children, and the bounded
// per-endpoint delivery log. The listener address is not part of this port —
// it rides GatewaySettingsStore with the gateway's other trigger
// configuration. Absent rows read as ErrNotFound; a violated endpoint-name
// uniqueness reads as ErrDuplicate.
type WebhookStore interface {
	// SaveEndpoint upserts one endpoint: an absent ID inserts (the store
	// assigns the ID and stamps the timestamps, UTC), a present ID rewrites
	// the definition columns in place keeping the original CreatedAt. The
	// name is unique among endpoints case-insensitively: colliding with
	// another endpoint returns ErrDuplicate; keeping the endpoint's own
	// name is fine. Exactly one of AgentID or GroupID must be set; anything
	// else is rejected. Verifiers are not written here — SaveVerifiers
	// replaces them.
	SaveEndpoint(ctx context.Context, e *WebhookEndpoint) error
	// GetEndpoint returns the endpoint with its verifiers ordered by
	// position; ErrNotFound when absent.
	GetEndpoint(ctx context.Context, id string) (*WebhookEndpoint, error)
	// ListEndpoints returns every endpoint with its verifiers, ordered by
	// name case-insensitively. The result is never nil.
	ListEndpoints(ctx context.Context) ([]WebhookEndpoint, error)
	// DeleteEndpoint removes the endpoint; its verifiers and delivery log
	// go with it. ErrNotFound when absent.
	DeleteEndpoint(ctx context.Context, id string) error
	// SaveVerifiers replaces the endpoint's whole verifier list: rows are
	// written in slice order (Position and EndpointID assigned by the
	// store), and every earlier row — secrets included — is dropped; an
	// empty list clears. The ≥1-verifier save rule lives above this port.
	// ErrNotFound when the endpoint is absent. The replace is not atomic
	// on its own: wrap it in WithTx together with the endpoint save when
	// the swap must commit as one.
	SaveVerifiers(ctx context.Context, endpointID string, verifiers []WebhookVerifier) error
	// AppendDelivery records one delivery attempt and enforces the
	// per-endpoint retention bound (WebhookDeliveryRetention): appending
	// past the bound evicts the endpoint's oldest rows. Zero AttemptedAt
	// and CreatedAt are stamped (UTC); ErrNotFound when the endpoint is
	// absent.
	AppendDelivery(ctx context.Context, d *WebhookDelivery) error
	// ListDeliveries returns one endpoint's delivery log newest-first. The
	// result is never nil; ErrNotFound when the endpoint is absent.
	ListDeliveries(ctx context.Context, endpointID string) ([]WebhookDelivery, error)
}
