package postgres

import (
	"context"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// connectionWebhookStore implements storeport.ConnectionWebhookStore for
// PostgreSQL: the webhook columns of workspace_connections plus the
// connection_webhook_deliveries dedupe table (add-connection-webhooks
// tasks.md 1.3/1.4).
type connectionWebhookStore struct {
	db Executor
}

// NewConnectionWebhookStore creates a new ConnectionWebhookStore with the
// given database executor.
func NewConnectionWebhookStore(db Executor) storeport.ConnectionWebhookStore {
	return &connectionWebhookStore{db: db}
}

// GetState reads the connection's webhook columns in one statement — the
// WHERE clause on the connection row makes unknown, cross-workspace, and
// non-existent connections indistinguishably domain.ErrNotFound. A connection
// that never enabled webhooks reads back as the inert default the columns'
// defaults produce.
func (cs *connectionWebhookStore) GetState(ctx context.Context, workspaceID, connectionID string) (*domain.ConnectionWebhook, error) {
	if workspaceID == "" || connectionID == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT webhook_enabled, webhook_secret_ciphertext, webhook_secret_hint,
		       COALESCE(webhook_target_agent_id::text, ''), webhook_target_kind,
		       webhook_target_id, webhook_events, COALESCE(webhook_last_error, '')
		FROM workspace_connections
		WHERE workspace_id = $1 AND id = $2
	`
	var w domain.ConnectionWebhook
	err := cs.db.QueryRow(ctx, query, workspaceID, connectionID).Scan(
		&w.Enabled,
		&w.SecretCiphertext,
		&w.SecretHint,
		&w.TargetAgentID,
		&w.TargetKind,
		&w.TargetID,
		&w.Events,
		&w.LastError,
	)
	if err != nil {
		return nil, convertError(err)
	}
	w.WorkspaceID = workspaceID
	w.ConnectionID = connectionID
	// Events always leaves the store as an array, never null (the
	// served-JSON normalization the connection stores apply too).
	if w.Events == nil {
		w.Events = []string{}
	}
	return &w, nil
}

// UpdateState writes the full webhook state in one workspace-scoped UPDATE —
// the enablement, rotation, and event-update write path. RowsAffected zero
// means the connection is unknown or foreign: domain.ErrNotFound.
func (cs *connectionWebhookStore) UpdateState(ctx context.Context, state *domain.ConnectionWebhook) error {
	if state == nil {
		return domain.ErrInvalid
	}
	if err := state.Validate(); err != nil {
		return err
	}

	// pgx binds a nil slice as SQL NULL, not DEFAULT — normalize so a
	// caller without an Events selection stores an empty array.
	events := state.Events
	if events == nil {
		events = []string{}
	}
	// The last-error residue stores as NULL when clear ("" → NULLIF → NULL),
	// the column's clean shape.
	lastError := state.LastError

	query := `
		UPDATE workspace_connections SET
			webhook_enabled = $3,
			webhook_secret_ciphertext = $4,
			webhook_secret_hint = $5,
			webhook_target_agent_id = NULLIF($6, '')::uuid,
			webhook_target_kind = $7,
			webhook_target_id = $8,
			webhook_events = $9,
			webhook_last_error = NULLIF($10, ''),
			updated_at = $11
		WHERE workspace_id = $1 AND id = $2
	`
	tag, err := cs.db.Exec(ctx, query,
		state.WorkspaceID,
		state.ConnectionID,
		state.Enabled,
		state.SecretCiphertext,
		state.SecretHint,
		state.TargetAgentID,
		state.TargetKind,
		state.TargetID,
		events,
		lastError,
		time.Now().UTC(),
	)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// RecordDelivery is the ack-after-persist primitive (design.md D3): the
// existence check keeps the workspace scope honest (the dedupe row carries
// its own workspace_id column), then the INSERT's ON CONFLICT DO NOTHING
// turns a replay — or a concurrent duplicate racing the primary key — into
// accepted=false, never an error.
func (cs *connectionWebhookStore) RecordDelivery(ctx context.Context, workspaceID, connectionID, deliveryID string, now time.Time) (bool, error) {
	if workspaceID == "" || connectionID == "" {
		return false, domain.ErrNotFound
	}
	if deliveryID == "" {
		return false, domain.ErrInvalid
	}

	var exists bool
	err := cs.db.QueryRow(ctx,
		`SELECT true FROM workspace_connections WHERE workspace_id = $1 AND id = $2`,
		workspaceID, connectionID,
	).Scan(&exists)
	if err != nil {
		return false, convertError(err)
	}

	tag, err := cs.db.Exec(ctx, `
		INSERT INTO connection_webhook_deliveries (connection_id, workspace_id, delivery_id, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING
	`, connectionID, workspaceID, deliveryID, now)
	if err != nil {
		return false, convertError(err)
	}
	return tag.RowsAffected() == 1, nil
}

// PruneDeliveries deletes the dedupe rows recorded strictly before the
// window start — the pruned-window cleanup hook — and reports the removal
// count. The created_at index keeps it a single range scan.
func (cs *connectionWebhookStore) PruneDeliveries(ctx context.Context, before time.Time) (int64, error) {
	tag, err := cs.db.Exec(ctx,
		`DELETE FROM connection_webhook_deliveries WHERE created_at < $1`,
		before,
	)
	if err != nil {
		return 0, convertError(err)
	}
	return tag.RowsAffected(), nil
}
