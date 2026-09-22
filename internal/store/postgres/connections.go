package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// connectionStore implements storeport.Connections for PostgreSQL.
type connectionStore struct {
	db Executor
}

// NewConnectionStore creates a new Connections store with the given database
// executor.
func NewConnectionStore(db Executor) storeport.Connections {
	return &connectionStore{db: db}
}

// isServiceTakenViolation reports whether the error is a unique violation on
// the per-workspace connection service constraint
// (uq_workspace_connections_workspace_id_service), as opposed to the primary key.
func isServiceTakenViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgerrcode.UniqueViolation &&
			pgErr.ConstraintName == "uq_workspace_connections_workspace_id_service"
	}
	return false
}

func (cs *connectionStore) Create(ctx context.Context, c *domain.Connection) error {
	if c == nil {
		return domain.ErrInvalid
	}
	if err := c.Validate(); err != nil {
		return err
	}

	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = now
	}

	// One connection per service per workspace (design.md D6): pre-check like
	// the fake so the conflict names the service; the schema constraint is the
	// backstop.
	var exists bool
	err := cs.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM workspace_connections WHERE workspace_id = $1 AND service = $2)`,
		c.WorkspaceID, c.Service,
	).Scan(&exists)
	if err != nil {
		return convertError(err)
	}
	if exists {
		return fmt.Errorf("%w: service %q already connected in workspace", domain.ErrConnectionExists, c.Service)
	}

	query := `
		INSERT INTO workspace_connections (
			id, workspace_id, service, access_level, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6
		)
	`
	_, err = cs.db.Exec(ctx, query,
		c.ID,
		c.WorkspaceID,
		c.Service,
		c.AccessLevel,
		c.CreatedAt,
		c.UpdatedAt,
	)
	if err != nil {
		if isServiceTakenViolation(err) {
			return fmt.Errorf("%w: service %q already connected in workspace", domain.ErrConnectionExists, c.Service)
		}
		return convertError(err)
	}
	return nil
}

func (cs *connectionStore) Get(ctx context.Context, workspaceID, id string) (*domain.Connection, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT id, workspace_id, service, access_level, created_at, updated_at
		FROM workspace_connections
		WHERE workspace_id = $1 AND id = $2
	`
	return scanConnection(cs.db.QueryRow(ctx, query, workspaceID, id))
}

func (cs *connectionStore) GetByService(ctx context.Context, workspaceID, service string) (*domain.Connection, error) {
	if workspaceID == "" || service == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT id, workspace_id, service, access_level, created_at, updated_at
		FROM workspace_connections
		WHERE workspace_id = $1 AND service = $2
	`
	return scanConnection(cs.db.QueryRow(ctx, query, workspaceID, service))
}

func scanConnection(row pgx.Row) (*domain.Connection, error) {
	var c domain.Connection
	err := row.Scan(
		&c.ID,
		&c.WorkspaceID,
		&c.Service,
		&c.AccessLevel,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &c, nil
}

func (cs *connectionStore) List(ctx context.Context, workspaceID string) ([]domain.Connection, error) {
	if workspaceID == "" {
		return []domain.Connection{}, nil
	}

	query := `
		SELECT id, workspace_id, service, access_level, created_at, updated_at
		FROM workspace_connections
		WHERE workspace_id = $1
		ORDER BY created_at ASC, id ASC
	`
	rows, err := cs.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	connections := make([]domain.Connection, 0)
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		connections = append(connections, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return connections, nil
}

// Delete cascades the disconnect in one transaction (design.md D8, tasks.md
// 1.5): the linked workspace MCP server row and every agent's attachment
// reference to it die with the connection row, so the token ciphertext is
// unrecoverable afterwards. The whole cascade rides one snapshot — the
// scheduler store's txBeginner precedent (nested Executor transactions ride
// savepoints when already inside store.WithTx).
func (cs *connectionStore) Delete(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	tx, err := cs.db.(txBeginner).Begin(ctx)
	if err != nil {
		return convertError(err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Lock the row: existence and workspace scope in one statement.
	var connID string
	err = tx.QueryRow(ctx,
		`SELECT id FROM workspace_connections WHERE workspace_id = $1 AND id = $2 FOR UPDATE`,
		workspaceID, id,
	).Scan(&connID)
	if err != nil {
		return convertError(err)
	}

	// The materialized server(s) linked to this connection.
	rows, err := tx.Query(ctx,
		`SELECT id FROM workspace_mcp_servers WHERE workspace_id = $1 AND origin_connection_id = $2`,
		workspaceID, id,
	)
	if err != nil {
		return convertError(err)
	}
	var serverIDs []string
	for rows.Next() {
		var serverID string
		if err := rows.Scan(&serverID); err != nil {
			rows.Close()
			return convertError(err)
		}
		serverIDs = append(serverIDs, serverID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return convertError(err)
	}
	rows.Close()

	// Strip the agents' attachment references first — agents.enabled_mcps is a
	// plain text[] with no FK, so only this statement removes the dangling ids.
	for _, serverID := range serverIDs {
		if _, err := tx.Exec(ctx,
			`UPDATE agents SET enabled_mcps = array_remove(enabled_mcps, $2) WHERE workspace_id = $1 AND $2 = ANY(enabled_mcps)`,
			workspaceID, serverID,
		); err != nil {
			return convertError(err)
		}
	}

	// Drop the materialized server rows (the FK's ON DELETE CASCADE is the
	// backstop; the explicit delete keeps the intent visible).
	if _, err := tx.Exec(ctx,
		`DELETE FROM workspace_mcp_servers WHERE workspace_id = $1 AND origin_connection_id = $2`,
		workspaceID, id,
	); err != nil {
		return convertError(err)
	}

	// Finally the connection itself.
	tag, err := tx.Exec(ctx,
		`DELETE FROM workspace_connections WHERE workspace_id = $1 AND id = $2`,
		workspaceID, id,
	)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}

	return tx.Commit(ctx)
}
