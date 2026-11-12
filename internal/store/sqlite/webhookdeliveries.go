package sqlite

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

const webhookDeliveryColumns = `id, endpoint_id, attempted_at, status, outcome,
session_id, message_id, via, sender_delivery_id, note, created_at`

// AppendDelivery records one delivery attempt and enforces the per-endpoint
// retention bound (add-webhook-triggers task 1.3): rows beyond
// store.WebhookDeliveryRetention are evicted oldest-first in the same
// statement sequence, so the log never grows past the bound.
func (s *webhookStore) AppendDelivery(ctx context.Context, d *store.WebhookDelivery) error {
	// The existence check doubles as the FK guard: an absent endpoint
	// surfaces the store sentinel instead of a driver constraint error.
	var n int
	if err := s.e.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM webhook_endpoints WHERE id = ?`, d.EndpointID).Scan(&n); err != nil {
		return fmt.Errorf("append webhook delivery: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("append webhook delivery %s: %w", d.EndpointID, store.ErrNotFound)
	}

	if d.ID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("append webhook delivery: %w", err)
		}
		d.ID = id
	}
	if d.AttemptedAt.IsZero() {
		d.AttemptedAt = nowUTC()
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = nowUTC()
	}

	_, err := s.e.ExecContext(ctx, `
INSERT INTO webhook_deliveries (`+webhookDeliveryColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.EndpointID, formatTime(d.AttemptedAt), d.Status, d.Outcome,
		d.SessionID, d.MessageID, d.Via, d.SenderDeliveryID, d.Note, formatTime(d.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("append webhook delivery %s: %w", d.EndpointID, err)
	}
	return s.evictDeliveries(ctx, d.EndpointID)
}

// ListDeliveries returns one endpoint's delivery log newest-first.
func (s *webhookStore) ListDeliveries(ctx context.Context, endpointID string) ([]store.WebhookDelivery, error) {
	var n int
	if err := s.e.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM webhook_endpoints WHERE id = ?`, endpointID).Scan(&n); err != nil {
		return nil, fmt.Errorf("list webhook deliveries %s: %w", endpointID, err)
	}
	if n == 0 {
		return nil, fmt.Errorf("list webhook deliveries %s: %w", endpointID, store.ErrNotFound)
	}

	rows, err := s.e.QueryContext(ctx, `
SELECT `+webhookDeliveryColumns+` FROM webhook_deliveries
WHERE endpoint_id = ? ORDER BY attempted_at DESC, created_at DESC`, endpointID)
	if err != nil {
		return nil, fmt.Errorf("list webhook deliveries %s: %w", endpointID, err)
	}
	defer rows.Close()

	deliveries := make([]store.WebhookDelivery, 0)
	for rows.Next() {
		d, err := scanWebhookDelivery(rows)
		if err != nil {
			return nil, fmt.Errorf("list webhook deliveries %s: %w", endpointID, err)
		}
		deliveries = append(deliveries, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list webhook deliveries %s: %w", endpointID, err)
	}
	return deliveries, nil
}

// evictDeliveries drops every row of the endpoint outside the newest
// WebhookDeliveryRetention rows (newest = attempted_at, then insertion
// order).
func (s *webhookStore) evictDeliveries(ctx context.Context, endpointID string) error {
	_, err := s.e.ExecContext(ctx, `
DELETE FROM webhook_deliveries
WHERE endpoint_id = ? AND id NOT IN (
    SELECT id FROM webhook_deliveries WHERE endpoint_id = ?
    ORDER BY attempted_at DESC, created_at DESC LIMIT ?
)`, endpointID, endpointID, store.WebhookDeliveryRetention)
	if err != nil {
		return fmt.Errorf("evict webhook deliveries %s: %w", endpointID, err)
	}
	return nil
}

// scanWebhookDelivery scans one webhook_deliveries row from a single- or
// multi-row result.
func scanWebhookDelivery(row scanner) (*store.WebhookDelivery, error) {
	var d store.WebhookDelivery
	var attemptedAt, createdAt string

	err := row.Scan(&d.ID, &d.EndpointID, &attemptedAt, &d.Status, &d.Outcome,
		&d.SessionID, &d.MessageID, &d.Via, &d.SenderDeliveryID, &d.Note, &createdAt)
	if err != nil {
		return nil, err
	}
	if d.AttemptedAt, err = parseTime(attemptedAt); err != nil {
		return nil, err
	}
	if d.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &d, nil
}
