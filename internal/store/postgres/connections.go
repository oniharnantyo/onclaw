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
	// Empty status is the pre-OAuth shape (change-1 callers set none): stored
	// as connected (add-connection-oauth design.md D6).
	status := c.Status
	if status == "" {
		status = domain.ConnectionStatusConnected
		c.Status = status
	}
	// granted_scopes is NOT NULL with a '{}' default, but pgx binds a nil
	// slice as SQL NULL, not DEFAULT — normalize here so a pre-OAuth caller
	// (no scopes) inserts an empty array.
	grantedScopes := c.GrantedScopes
	if grantedScopes == nil {
		grantedScopes = []string{}
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
			id, workspace_id, service, access_level, status,
			refresh_ciphertext, expires_at, granted_scopes, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		)
	`
	_, err = cs.db.Exec(ctx, query,
		c.ID,
		c.WorkspaceID,
		c.Service,
		c.AccessLevel,
		status,
		c.RefreshCiphertext,
		c.ExpiresAt,
		grantedScopes,
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
		SELECT id, workspace_id, service, access_level, status,
		       refresh_ciphertext, expires_at, granted_scopes, created_at, updated_at
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
		SELECT id, workspace_id, service, access_level, status,
		       refresh_ciphertext, expires_at, granted_scopes, created_at, updated_at
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
		&c.Status,
		&c.RefreshCiphertext,
		&c.ExpiresAt,
		&c.GrantedScopes,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	// GrantedScopes always leaves the store as an array, never null (the
	// served-JSON normalization the recipe registry applies too).
	if c.GrantedScopes == nil {
		c.GrantedScopes = []string{}
	}
	return &c, nil
}

// UpdateTokenLifecycle persists the OAuth token-lifecycle columns (refresh
// envelope, expiry, granted scopes, status) in one workspace-scoped UPDATE —
// the refresh write-through and the expired transition's write path. The
// RETURNING re-reads the row so the caller's struct stays faithful to what is
// stored (timestamps included).
func (cs *connectionStore) UpdateTokenLifecycle(ctx context.Context, c *domain.Connection) error {
	if c == nil {
		return domain.ErrInvalid
	}
	if err := c.Validate(); err != nil {
		return err
	}

	query := `
		UPDATE workspace_connections SET
			refresh_ciphertext = $3,
			expires_at = $4,
			granted_scopes = $5,
			status = $6,
			updated_at = $7
		WHERE workspace_id = $1 AND id = $2
		RETURNING created_at, updated_at
	`
	// pgx binds a nil slice as SQL NULL, not DEFAULT — normalize so a nil
	// GrantedScopes stores (and reads back) as an empty array.
	grantedScopes := c.GrantedScopes
	if grantedScopes == nil {
		grantedScopes = []string{}
	}
	err := cs.db.QueryRow(ctx, query,
		c.WorkspaceID,
		c.ID,
		c.RefreshCiphertext,
		c.ExpiresAt,
		grantedScopes,
		c.Status,
		time.Now().UTC(),
	).Scan(&c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (cs *connectionStore) List(ctx context.Context, workspaceID string) ([]domain.Connection, error) {
	if workspaceID == "" {
		return []domain.Connection{}, nil
	}

	query := `
		SELECT id, workspace_id, service, access_level, status,
		       refresh_ciphertext, expires_at, granted_scopes, created_at, updated_at
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
