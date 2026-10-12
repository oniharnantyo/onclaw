package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// workspaceWebhookStore implements storeport.WorkspaceWebhookStore for
// PostgreSQL: the workspace_webhooks aggregate rows plus the
// workspace_webhook_deliveries dedupe table and the
// workspace_webhook_delivery_log bounded history (add-workspace-webhooks
// tasks.md 1.3, design.md D1/D4/D8).
type workspaceWebhookStore struct {
	db Executor
}

// NewWorkspaceWebhookStore creates a new WorkspaceWebhookStore with the
// given database executor.
func NewWorkspaceWebhookStore(db Executor) storeport.WorkspaceWebhookStore {
	return &workspaceWebhookStore{db: db}
}

// webhookColumns is the shared SELECT list — the WHERE clause on
// workspace_id makes unknown and foreign-workspace ids indistinguishably
// domain.ErrNotFound on every read. The filter/template/sample columns are
// NOT NULL DEFAULT ” (the blank-is-unset shape, the 000064 convention).
const webhookColumns = `
	SELECT id, workspace_id, name, scheme, secret_ciphertext, secret_hint,
	       event_source, delivery_id_source, filter_expression,
	       turn_template, sample_payload,
	       target_agent_id::text, target_kind, target_id, enabled,
	       created_at, updated_at
	FROM workspace_webhooks
`

// scanWebhook scans one endpoint row, decoding the event_source jsonb into
// its declared-source struct.
func scanWebhook(scanner interface {
	Scan(dest ...any) error
}) (*domain.WorkspaceWebhook, error) {
	var w domain.WorkspaceWebhook
	var eventSource []byte
	if err := scanner.Scan(
		&w.ID, &w.WorkspaceID, &w.Name, &w.Scheme, &w.SecretCiphertext, &w.SecretHint,
		&eventSource, &w.DeliveryIDSource, &w.FilterExpression,
		&w.TurnTemplate, &w.SamplePayload,
		&w.TargetAgentID, &w.TargetKind, &w.TargetID, &w.Enabled,
		&w.CreatedAt, &w.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(eventSource) > 0 {
		if err := json.Unmarshal(eventSource, &w.EventSource); err != nil {
			return nil, fmt.Errorf("%w: stored webhook event source is not valid JSON: %v", domain.ErrInvalid, err)
		}
	}
	return &w, nil
}

// Create inserts a new endpoint row (tasks.md 1.3): validation at the
// boundary, id assignment when empty, and app-stamped timestamps. An
// unknown workspace fails the FK (domain.ErrNotFound via convertError); a
// duplicate caller-supplied id is the primary key's unique violation
// (domain.ErrConflict).
func (ws *workspaceWebhookStore) Create(ctx context.Context, wh *domain.WorkspaceWebhook) error {
	if wh == nil {
		return domain.ErrInvalid
	}
	if err := wh.Validate(); err != nil {
		return err
	}
	if wh.ID == "" {
		wh.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if wh.CreatedAt.IsZero() {
		wh.CreatedAt = now
	}
	if wh.UpdatedAt.IsZero() {
		wh.UpdatedAt = now
	}

	eventSource, err := json.Marshal(wh.EventSource)
	if err != nil {
		return fmt.Errorf("%w: webhook event source cannot marshal: %v", domain.ErrInvalid, err)
	}
	tag, err := ws.db.Exec(ctx, `
		INSERT INTO workspace_webhooks (
			id, workspace_id, name, scheme, secret_ciphertext, secret_hint,
			event_source, delivery_id_source, filter_expression, turn_template,
			sample_payload, target_agent_id, target_kind, target_id, enabled,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			$11, $12, $13, $14, $15,
			$16, $17
		)
	`, wh.ID, wh.WorkspaceID, wh.Name, wh.Scheme, wh.SecretCiphertext, wh.SecretHint,
		eventSource, wh.DeliveryIDSource, wh.FilterExpression, wh.TurnTemplate,
		wh.SamplePayload, wh.TargetAgentID, wh.TargetKind, wh.TargetID, wh.Enabled,
		wh.CreatedAt, wh.UpdatedAt)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return nil
}

// ByID reads one endpoint; absent and foreign-workspace ids are
// indistinguishably domain.ErrNotFound.
func (ws *workspaceWebhookStore) ByID(ctx context.Context, workspaceID, id string) (*domain.WorkspaceWebhook, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}
	w, err := scanWebhook(ws.db.QueryRow(ctx, webhookColumns+`WHERE workspace_id = $1 AND id = $2`, workspaceID, id))
	if err != nil {
		return nil, convertError(err)
	}
	return w, nil
}

// List returns the workspace's endpoints, oldest first. The result is never
// nil.
func (ws *workspaceWebhookStore) List(ctx context.Context, workspaceID string) ([]*domain.WorkspaceWebhook, error) {
	if workspaceID == "" {
		return []*domain.WorkspaceWebhook{}, nil
	}
	rows, err := ws.db.Query(ctx, webhookColumns+`WHERE workspace_id = $1 ORDER BY created_at ASC, id ASC`, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	webhooks := []*domain.WorkspaceWebhook{}
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, convertError(err)
		}
		webhooks = append(webhooks, w)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return webhooks, nil
}

// Update writes the full endpoint state — the config-edit, rotate, and
// enable/pause write path. CreatedAt stays as stored; RowsAffected zero
// means the endpoint is unknown or foreign: domain.ErrNotFound.
func (ws *workspaceWebhookStore) Update(ctx context.Context, wh *domain.WorkspaceWebhook) error {
	if wh == nil {
		return domain.ErrInvalid
	}
	if err := wh.Validate(); err != nil {
		return err
	}

	eventSource, err := json.Marshal(wh.EventSource)
	if err != nil {
		return fmt.Errorf("%w: webhook event source cannot marshal: %v", domain.ErrInvalid, err)
	}
	now := time.Now().UTC()
	tag, err := ws.db.Exec(ctx, `
		UPDATE workspace_webhooks SET
			name = $3,
			scheme = $4,
			secret_ciphertext = $5,
			secret_hint = $6,
			event_source = $7,
			delivery_id_source = $8,
			filter_expression = $9,
			turn_template = $10,
			sample_payload = $11,
			target_agent_id = $12,
			target_kind = $13,
			target_id = $14,
			enabled = $15,
			updated_at = $16
		WHERE workspace_id = $1 AND id = $2
	`, wh.WorkspaceID, wh.ID,
		wh.Name, wh.Scheme, wh.SecretCiphertext, wh.SecretHint,
		eventSource, wh.DeliveryIDSource, wh.FilterExpression, wh.TurnTemplate,
		wh.SamplePayload, wh.TargetAgentID, wh.TargetKind, wh.TargetID, wh.Enabled,
		now)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	wh.UpdatedAt = now
	return nil
}

// Delete removes the endpoint; its dedupe rows and delivery log die with it
// (ON DELETE CASCADE). RowsAffected zero means unknown or foreign.
func (ws *workspaceWebhookStore) Delete(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}
	tag, err := ws.db.Exec(ctx,
		`DELETE FROM workspace_webhooks WHERE workspace_id = $1 AND id = $2`,
		workspaceID, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// RecordDelivery is the ack-after-persist primitive (design.md D4): the
// existence check keeps the workspace scope honest (the dedupe row carries
// its own workspace_id column), then the INSERT's ON CONFLICT DO NOTHING
// turns a replay — or a concurrent duplicate racing the primary key — into
// accepted=false, never an error.
func (ws *workspaceWebhookStore) RecordDelivery(ctx context.Context, workspaceID, webhookID, dedupeKey string, now time.Time) (bool, error) {
	if workspaceID == "" || webhookID == "" {
		return false, domain.ErrNotFound
	}
	if dedupeKey == "" {
		return false, domain.ErrInvalid
	}

	var exists bool
	err := ws.db.QueryRow(ctx,
		`SELECT true FROM workspace_webhooks WHERE workspace_id = $1 AND id = $2`,
		workspaceID, webhookID,
	).Scan(&exists)
	if err != nil {
		return false, convertError(err)
	}

	tag, err := ws.db.Exec(ctx, `
		INSERT INTO workspace_webhook_deliveries (webhook_id, workspace_id, dedupe_key, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING
	`, webhookID, workspaceID, dedupeKey, now)
	if err != nil {
		return false, convertError(err)
	}
	return tag.RowsAffected() == 1, nil
}

// AppendDeliveryLog appends one processed delivery's row (design.md D8):
// outcome validated through the domain, id and timestamp store-stamped when
// absent.
func (ws *workspaceWebhookStore) AppendDeliveryLog(ctx context.Context, workspaceID, webhookID string, entry domain.WebhookDeliveryLogEntry) error {
	if workspaceID == "" || webhookID == "" {
		return domain.ErrNotFound
	}
	if err := entry.Validate(); err != nil {
		return err
	}

	var exists bool
	err := ws.db.QueryRow(ctx,
		`SELECT true FROM workspace_webhooks WHERE workspace_id = $1 AND id = $2`,
		workspaceID, webhookID,
	).Scan(&exists)
	if err != nil {
		return convertError(err)
	}

	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	_, err = ws.db.Exec(ctx, `
		INSERT INTO workspace_webhook_delivery_log (id, workspace_id, webhook_id, event, outcome, run_id, detail, status, created_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, $9)
	`, entry.ID, workspaceID, webhookID, entry.Event, entry.Outcome, entry.RunID, entry.Detail, entry.Status, entry.CreatedAt)
	if err != nil {
		return convertError(err)
	}
	return nil
}

// ListDeliveryLog returns the endpoint's delivery history, newest first —
// the (webhook_id, created_at DESC) index's order; limit <= 0 means no
// limit. Unknown or foreign-workspace endpoints are domain.ErrNotFound (the
// explicit existence check — an empty history is a normal state, a foreign
// one is not); the result is never nil.
func (ws *workspaceWebhookStore) ListDeliveryLog(ctx context.Context, workspaceID, webhookID string, limit int) ([]domain.WebhookDeliveryLogEntry, error) {
	if workspaceID == "" || webhookID == "" {
		return nil, domain.ErrNotFound
	}

	var exists bool
	err := ws.db.QueryRow(ctx,
		`SELECT true FROM workspace_webhooks WHERE workspace_id = $1 AND id = $2`,
		workspaceID, webhookID,
	).Scan(&exists)
	if err != nil {
		return nil, convertError(err)
	}

	query := `
		SELECT id, webhook_id, COALESCE(event, ''), outcome,
		       COALESCE(run_id, ''), COALESCE(detail, ''), status,
		       duration_ms, tokens_used, created_at
		FROM workspace_webhook_delivery_log
		WHERE workspace_id = $1 AND webhook_id = $2
		ORDER BY created_at DESC, id DESC
	`
	args := []any{workspaceID, webhookID}
	if limit > 0 {
		query += ` LIMIT $3`
		args = append(args, limit)
	}

	rows, err := ws.db.Query(ctx, query, args...)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	entries := []domain.WebhookDeliveryLogEntry{}
	for rows.Next() {
		var e domain.WebhookDeliveryLogEntry
		if err := rows.Scan(&e.ID, &e.WebhookID, &e.Event, &e.Outcome, &e.RunID, &e.Detail, &e.Status, &e.DurationMS, &e.TokensUsed, &e.CreatedAt); err != nil {
			return nil, convertError(err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return entries, nil
}

// FinishDeliveryRun records a submitted delivery's run telemetry on its log
// row (add-webhook-channel-visibility 4.2, design.md D5), keyed by the run
// id — only submitted rows carry one. A missing or pruned row affects zero
// rows and is a no-op: the finish is strictly best-effort.
func (ws *workspaceWebhookStore) FinishDeliveryRun(ctx context.Context, workspaceID, runID, status string, durationMS int64, tokensUsed int) error {
	if workspaceID == "" || runID == "" {
		return domain.ErrInvalid
	}
	_, err := ws.db.Exec(ctx, `
		UPDATE workspace_webhook_delivery_log SET
			status = $3,
			duration_ms = $4,
			tokens_used = $5
		WHERE workspace_id = $1 AND run_id = $2
	`, workspaceID, runID, status, durationMS, tokensUsed)
	if err != nil {
		return convertError(err)
	}
	return nil
}

// ListRuns returns the workspace-wide webhook run listing
// (add-webhook-channel-visibility 4.4): submitted deliveries of every
// endpoint in the workspace, joined to the endpoint and its bound agent,
// newest first. Legacy blank statuses read as completed; a deleted agent
// leaves the name blank (LEFT JOIN).
func (ws *workspaceWebhookStore) ListRuns(ctx context.Context, workspaceID string, limit int) ([]domain.WorkspaceWebhookRun, error) {
	if workspaceID == "" {
		return []domain.WorkspaceWebhookRun{}, nil
	}
	if limit <= 0 {
		limit = domain.WorkspaceWebhookRunsLimit
	}

	rows, err := ws.db.Query(ctx, `
		SELECT l.id, w.id, w.name, COALESCE(l.event, ''), COALESCE(l.run_id, ''),
		       w.target_agent_id::text,
		       COALESCE(a.name, ''),
		       COALESCE(NULLIF(l.status, ''), $2),
		       l.duration_ms, l.tokens_used, l.created_at
		FROM workspace_webhook_delivery_log l
		JOIN workspace_webhooks w ON w.id = l.webhook_id AND w.workspace_id = $1
		LEFT JOIN agents a ON a.id = w.target_agent_id AND a.workspace_id = w.workspace_id
		WHERE l.workspace_id = $1
		  AND l.outcome = $3
		  AND l.run_id <> ''
		ORDER BY l.created_at DESC, l.id DESC
		LIMIT $4
	`, workspaceID, domain.WorkspaceWebhookRunStatusCompleted, domain.WorkspaceWebhookDeliveryOutcomeSubmitted, limit)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	runs := []domain.WorkspaceWebhookRun{}
	for rows.Next() {
		var r domain.WorkspaceWebhookRun
		if err := rows.Scan(&r.ID, &r.WebhookID, &r.WebhookName, &r.Event, &r.RunID, &r.AgentID, &r.AgentName, &r.Status, &r.DurationMS, &r.TokensUsed, &r.StartedAt); err != nil {
			return nil, convertError(err)
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return runs, nil
}

// PruneDeliveries deletes the dedupe rows recorded strictly before the
// window start — the hourly sweep's dedupe half. The created_at index keeps
// it a single range scan.
func (ws *workspaceWebhookStore) PruneDeliveries(ctx context.Context, olderThan time.Time) error {
	_, err := ws.db.Exec(ctx,
		`DELETE FROM workspace_webhook_deliveries WHERE created_at < $1`,
		olderThan)
	if err != nil {
		return convertError(err)
	}
	return nil
}

// PruneDeliveryLog caps every endpoint's history at the newest
// domain.WorkspaceWebhookDeliveryLogKeep rows (design.md D8), considering
// only rows stamped before now — the hourly sweep's log half. The window
// function ranks each webhook's rows newest first (id as the tiebreak, the
// same order the fake keeps) and deletes everything past the cap.
func (ws *workspaceWebhookStore) PruneDeliveryLog(ctx context.Context, now time.Time) error {
	_, err := ws.db.Exec(ctx, `
		DELETE FROM workspace_webhook_delivery_log
		WHERE id IN (
			SELECT id FROM (
				SELECT id, row_number() OVER (
					PARTITION BY webhook_id
					ORDER BY created_at DESC, id DESC
				) AS rn
				FROM workspace_webhook_delivery_log
				WHERE created_at < $1
			) ranked
			WHERE rn > $2
		)
	`, now, domain.WorkspaceWebhookDeliveryLogKeep)
	if err != nil {
		return convertError(err)
	}
	return nil
}
