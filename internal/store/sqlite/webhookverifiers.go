package sqlite

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

const webhookVerifierColumns = `id, endpoint_id, position, kind, preset, header_name,
algo, encoding, prefix, scheme, timestamp_header, secret, created_at, updated_at`

// SaveVerifiers replaces one endpoint's whole verifier list: every earlier
// row (secrets included) is dropped and the slice is written in order with
// store-assigned ids, positions, and stamped timestamps. Not atomic on its
// own — callers needing an atomic swap run it inside WithTx.
func (s *webhookStore) SaveVerifiers(ctx context.Context, endpointID string, verifiers []store.WebhookVerifier) error {
	// The existence check doubles as the FK guard: replacing nothing for
	// an absent endpoint surfaces the store sentinel instead of a driver
	// constraint error (the agent_hooks precedent).
	var n int
	if err := s.e.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM webhook_endpoints WHERE id = ?`, endpointID).Scan(&n); err != nil {
		return fmt.Errorf("save webhook verifiers %s: %w", endpointID, err)
	}
	if n == 0 {
		return fmt.Errorf("save webhook verifiers %s: %w", endpointID, store.ErrNotFound)
	}

	if _, err := s.e.ExecContext(ctx,
		`DELETE FROM webhook_verifiers WHERE endpoint_id = ?`, endpointID); err != nil {
		return fmt.Errorf("save webhook verifiers %s: %w", endpointID, err)
	}
	for i := range verifiers {
		v := &verifiers[i]
		if v.ID == "" {
			id, err := newID()
			if err != nil {
				return fmt.Errorf("save webhook verifiers %s: %w", endpointID, err)
			}
			v.ID = id
		}
		v.EndpointID = endpointID
		v.Position = i
		stampCreated(&v.CreatedAt, &v.UpdatedAt)

		_, err := s.e.ExecContext(ctx, `
INSERT INTO webhook_verifiers (`+webhookVerifierColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			v.ID, v.EndpointID, v.Position, v.Kind, v.Preset, v.HeaderName, v.Algo,
			v.Encoding, v.Prefix, v.Scheme, v.TimestampHeader, v.Secret,
			formatTime(v.CreatedAt), formatTime(v.UpdatedAt),
		)
		if err != nil {
			return fmt.Errorf("save webhook verifiers %s: %w", endpointID, err)
		}
	}
	return nil
}

// verifiersOf reads one endpoint's verifiers ordered by position.
func (s *webhookStore) verifiersOf(ctx context.Context, endpointID string) ([]store.WebhookVerifier, error) {
	rows, err := s.e.QueryContext(ctx, `
SELECT `+webhookVerifierColumns+` FROM webhook_verifiers
WHERE endpoint_id = ? ORDER BY position, id`, endpointID)
	if err != nil {
		return nil, fmt.Errorf("list webhook verifiers %s: %w", endpointID, err)
	}
	defer rows.Close()

	verifiers := make([]store.WebhookVerifier, 0)
	for rows.Next() {
		v, err := scanWebhookVerifier(rows)
		if err != nil {
			return nil, fmt.Errorf("list webhook verifiers %s: %w", endpointID, err)
		}
		verifiers = append(verifiers, *v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list webhook verifiers %s: %w", endpointID, err)
	}
	return verifiers, nil
}

// scanWebhookVerifier scans one webhook_verifiers row from a single- or
// multi-row result.
func scanWebhookVerifier(row scanner) (*store.WebhookVerifier, error) {
	var v store.WebhookVerifier
	var createdAt, updatedAt string

	err := row.Scan(&v.ID, &v.EndpointID, &v.Position, &v.Kind, &v.Preset, &v.HeaderName,
		&v.Algo, &v.Encoding, &v.Prefix, &v.Scheme, &v.TimestampHeader, &v.Secret,
		&createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if v.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if v.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &v, nil
}
