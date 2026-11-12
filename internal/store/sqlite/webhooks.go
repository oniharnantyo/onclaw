package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

// webhookStore implements the endpoint half of store.WebhookStore; the
// verifier and delivery halves live in webhookverifiers.go and
// webhookdeliveries.go.
type webhookStore struct{ e executor }

const webhookEndpointColumns = `id, name, agent_id, group_id, prompt, enabled, created_at, updated_at`

// SaveEndpoint upserts one endpoint row (verifiers are not written here).
func (s *webhookStore) SaveEndpoint(ctx context.Context, ep *store.WebhookEndpoint) error {
	if (ep.AgentID == "") == (ep.GroupID == "") {
		return fmt.Errorf("save webhook endpoint: exactly one of agent id or group id must be set")
	}
	if ep.ID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("save webhook endpoint: %w", err)
		}
		ep.ID = id
	}
	stampCreated(&ep.CreatedAt, &ep.UpdatedAt)

	// The unique index on the NOCASE name rejects duplicates among other
	// endpoints; keeping the endpoint's own name never conflicts. The
	// CHECK on (agent_id, group_id) double-guards the Go validation above.
	_, err := s.e.ExecContext(ctx, `
INSERT INTO webhook_endpoints (`+webhookEndpointColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
name = excluded.name,
agent_id = excluded.agent_id,
group_id = excluded.group_id,
prompt = excluded.prompt,
enabled = excluded.enabled,
updated_at = excluded.updated_at`,
		ep.ID, ep.Name, nullString(ep.AgentID), nullString(ep.GroupID), ep.Prompt,
		boolInt(ep.Enabled), formatTime(ep.CreatedAt), formatTime(ep.UpdatedAt),
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return fmt.Errorf("save webhook endpoint %s: %w", ep.ID, store.ErrDuplicate)
		}
		return fmt.Errorf("save webhook endpoint %s: %w", ep.ID, err)
	}
	return nil
}

// GetEndpoint returns one endpoint with its verifiers ordered by position.
func (s *webhookStore) GetEndpoint(ctx context.Context, id string) (*store.WebhookEndpoint, error) {
	row := s.e.QueryRowContext(ctx,
		`SELECT `+webhookEndpointColumns+` FROM webhook_endpoints WHERE id = ?`, id)
	ep, err := scanWebhookEndpoint(row)
	if err != nil {
		return nil, readErr("get webhook endpoint", err)
	}
	if ep.Verifiers, err = s.verifiersOf(ctx, ep.ID); err != nil {
		return nil, err
	}
	return ep, nil
}

// ListEndpoints returns every endpoint with its verifiers, ordered by name
// case-insensitively (the NOCASE column sorts that way).
func (s *webhookStore) ListEndpoints(ctx context.Context) ([]store.WebhookEndpoint, error) {
	rows, err := s.e.QueryContext(ctx,
		`SELECT `+webhookEndpointColumns+` FROM webhook_endpoints ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list webhook endpoints: %w", err)
	}
	defer rows.Close()

	endpoints := make([]store.WebhookEndpoint, 0)
	for rows.Next() {
		ep, err := scanWebhookEndpoint(rows)
		if err != nil {
			return nil, fmt.Errorf("list webhook endpoints: %w", err)
		}
		endpoints = append(endpoints, *ep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list webhook endpoints: %w", err)
	}
	for i := range endpoints {
		if endpoints[i].Verifiers, err = s.verifiersOf(ctx, endpoints[i].ID); err != nil {
			return nil, err
		}
	}
	return endpoints, nil
}

// DeleteEndpoint removes one endpoint; its verifiers and delivery log go
// with it (FK cascade).
func (s *webhookStore) DeleteEndpoint(ctx context.Context, id string) error {
	res, err := s.e.ExecContext(ctx, `DELETE FROM webhook_endpoints WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete webhook endpoint %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete webhook endpoint %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// scanWebhookEndpoint scans one webhook_endpoints row from a single- or
// multi-row result; the nullable target columns read back as plain strings.
func scanWebhookEndpoint(row scanner) (*store.WebhookEndpoint, error) {
	var ep store.WebhookEndpoint
	var agentID, groupID sql.NullString
	var createdAt, updatedAt string

	err := row.Scan(&ep.ID, &ep.Name, &agentID, &groupID, &ep.Prompt, &ep.Enabled,
		&createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	ep.AgentID = agentID.String
	ep.GroupID = groupID.String
	if ep.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if ep.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &ep, nil
}
