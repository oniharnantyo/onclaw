package store

import (
	"context"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// ConnectionWebhookStore manages the per-connection webhook configuration
// state and the delivery-dedupe window (add-connection-webhooks tasks.md 1.4).
// It is a focused port, not part of Connections: the webhook management
// service and the ingress pipeline take exactly this dependency, and the
// dedupe table has a lifecycle of its own (design.md D3). The state itself
// lives in the webhook columns of the workspace_connections row; this port is
// its only read/write path. All operations are workspace-scoped; a connection
// belonging to another workspace is indistinguishable from an unknown id
// (domain.ErrNotFound).
type ConnectionWebhookStore interface {
	// GetState reads the webhook state of one workspace connection. Every
	// connection has webhook state: a connection that never enabled webhooks
	// reads back as the inert default (enabled=false, empty secret, unset
	// target, empty selection). Unknown or cross-workspace connections return
	// domain.ErrNotFound.
	GetState(ctx context.Context, workspaceID, connectionID string) (*domain.ConnectionWebhook, error)
	// UpdateState writes the full webhook state of an existing connection —
	// enabled, secret envelope + hint, target binding, selected events, and
	// the last-error residue (the ingress's render-drop surface, cleared by
	// the next successful render) — and bumps the connection's updated_at. It
	// is the enablement, rotation, and event-update write path; no other
	// webhook column is written anywhere
	// else. Structural validation runs at the boundary (domain.ConnectionWebhook.Validate);
	// recipe-catalog membership of Events is the service's
	// ValidateForRecipe check. Unknown or cross-workspace connections return
	// domain.ErrNotFound.
	UpdateState(ctx context.Context, state *domain.ConnectionWebhook) error
	// RecordDelivery records a delivery id for the connection — the
	// ack-after-persist primitive (design.md D3): the ingress persists the id
	// after verification and dedupe, before run completion, so a provider
	// retry on timeout redelivers into an occupied id. accepted reports
	// whether this is the first sighting (true — proceed to render and run)
	// or a replay (false — ack the provider without a second agent turn).
	// An empty delivery id is domain.ErrInvalid; unknown or cross-workspace
	// connections return domain.ErrNotFound.
	RecordDelivery(ctx context.Context, workspaceID, connectionID, deliveryID string, now time.Time) (accepted bool, err error)
	// PruneDeliveries deletes the delivery rows recorded strictly before the
	// window start and returns how many were removed — the pruned-window
	// cleanup hook the webhook service runs on its retention cadence
	// (domain.ConnectionWebhookDeliveryWindow is the window). A negative
	// count never returns; an empty table prunes nothing.
	PruneDeliveries(ctx context.Context, before time.Time) (int64, error)
}
