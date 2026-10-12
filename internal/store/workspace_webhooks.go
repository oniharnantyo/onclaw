package store

import (
	"context"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// WorkspaceWebhookStore manages the standalone workspace webhook endpoints
// (add-workspace-webhooks tasks.md 1.3, design.md D1): the endpoint
// aggregate's CRUD, the durable delivery-dedupe store, and the bounded
// delivery log. It is a focused port: the webhook management service and the
// standalone ingress pipeline take exactly this dependency. All operations
// are workspace-scoped; an endpoint belonging to another workspace is
// indistinguishable from an unknown id (domain.ErrNotFound).
type WorkspaceWebhookStore interface {
	// Create inserts a new endpoint row, assigning ID (when empty) and the
	// timestamps app-side. Structural validation runs at the boundary
	// (domain.WorkspaceWebhook.Validate); the management service owns the
	// save-time filter compilation and template-vs-sample checks. An unknown
	// workspace returns domain.ErrNotFound; a duplicate caller-supplied id
	// returns domain.ErrConflict.
	Create(ctx context.Context, wh *domain.WorkspaceWebhook) error
	// ByID reads one endpoint. Absent or foreign-workspace ids return
	// domain.ErrNotFound.
	ByID(ctx context.Context, workspaceID, id string) (*domain.WorkspaceWebhook, error)
	// List returns the workspace's endpoints, oldest first. The result is
	// never nil.
	List(ctx context.Context, workspaceID string) ([]*domain.WorkspaceWebhook, error)
	// Update writes the full endpoint state — the config-edit, rotate, and
	// enable/pause write path. The struct's identity fields (ID,
	// WorkspaceID, CreatedAt) are immutable; UpdatedAt is stamped
	// app-side and the stored state is written back into wh so it stays
	// faithful. Unknown or foreign-workspace endpoints return
	// domain.ErrNotFound.
	Update(ctx context.Context, wh *domain.WorkspaceWebhook) error
	// Delete removes the endpoint; its dedupe rows and delivery log die with
	// it (ON DELETE CASCADE). Unknown or foreign-workspace ids return
	// domain.ErrNotFound.
	Delete(ctx context.Context, workspaceID, id string) error
	// RecordDelivery records a dedupe key for the endpoint — the
	// ack-after-persist primitive (design.md D4): the ingress persists the
	// key after verification, before enqueue, so a provider retry on timeout
	// redelivers into an occupied key. accepted reports whether this is the
	// first sighting (true — proceed to filter and enqueue) or a replay
	// (false — ack without a second run). dedupeKey is the delivery id from
	// the endpoint's declared delivery-id source, or "sig:<hex>" for a
	// timestamped-signature replay rejection (design.md D4). An empty key is
	// domain.ErrInvalid; unknown or foreign-workspace endpoints return
	// domain.ErrNotFound.
	RecordDelivery(ctx context.Context, workspaceID, webhookID, dedupeKey string, now time.Time) (accepted bool, err error)
	// AppendDeliveryLog appends one processed delivery's row (design.md D8).
	// The entry's outcome must be in the domain catalog
	// (domain.WebhookDeliveryLogEntry.Validate); ID, webhook scope, and
	// CreatedAt are store-stamped when absent. Unknown or foreign-workspace
	// endpoints return domain.ErrNotFound.
	AppendDeliveryLog(ctx context.Context, workspaceID, webhookID string, entry domain.WebhookDeliveryLogEntry) error
	// ListDeliveryLog returns the endpoint's delivery history, newest first
	// (design.md D8 — "did the sender's event reach the agent, and if not,
	// why not"). limit <= 0 means no limit. Unknown or foreign-workspace
	// endpoints return domain.ErrNotFound; the result is never nil.
	ListDeliveryLog(ctx context.Context, workspaceID, webhookID string, limit int) ([]domain.WebhookDeliveryLogEntry, error)
	// PruneDeliveries deletes dedupe rows recorded strictly before the
	// window start — the hourly sweep's dedupe half
	// (domain.WorkspaceWebhookDeliveryWindow is the window).
	PruneDeliveries(ctx context.Context, olderThan time.Time) error
	// PruneDeliveryLog caps every endpoint's delivery history at the newest
	// domain.WorkspaceWebhookDeliveryLogKeep rows (design.md D8), considering
	// only rows stamped before now — the hourly sweep's log half. Rows
	// stamped at or after now are never pruned.
	PruneDeliveryLog(ctx context.Context, now time.Time) error
	// FinishDeliveryRun records a submitted delivery's run telemetry on its
	// log row (add-webhook-channel-visibility 4.2, design.md D5): the
	// submitter's drain writes the terminal status, duration, and token
	// usage keyed by the run id it returned. Only the submitted row carries
	// a run id; a missing or pruned row is a no-op, not an error — the
	// finish is strictly best-effort (a telemetry write never surfaces).
	FinishDeliveryRun(ctx context.Context, workspaceID, runID, status string, durationMS int64, tokensUsed int) error
	// ListRuns returns the workspace-wide webhook run listing
	// (add-webhook-channel-visibility 4.4): the submitted deliveries of
	// every endpoint in the workspace, projected with the endpoint name and
	// the bound agent's id and name (resolved at read time; a deleted agent
	// leaves the name blank), newest first, bounded at limit rows (limit
	// <= 0 means the domain default). Legacy rows with a blank status read
	// as completed. The result is never nil.
	ListRuns(ctx context.Context, workspaceID string, limit int) ([]domain.WorkspaceWebhookRun, error)
}
