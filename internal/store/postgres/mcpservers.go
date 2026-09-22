package postgres

import (
	"context"
	"encoding/json"
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

// workspaceMCPStore implements storeport.WorkspaceMCPServers for PostgreSQL.
type workspaceMCPStore struct {
	db Executor
}

// NewWorkspaceMCPServerStore creates a new WorkspaceMCPServers store with the
// given database executor.
func NewWorkspaceMCPServerStore(db Executor) storeport.WorkspaceMCPServers {
	return &workspaceMCPStore{db: db}
}

// agentMCPStore implements storeport.AgentMCPServers for PostgreSQL.
type agentMCPStore struct {
	db Executor
}

// NewAgentMCPServerStore creates a new AgentMCPServers store with the given
// database executor.
func NewAgentMCPServerStore(db Executor) storeport.AgentMCPServers {
	return &agentMCPStore{db: db}
}

// marshalJSONB encodes env/header rows as a jsonb array of {name,value}
// objects; nil and empty slices persist as the empty array so the column's
// NOT NULL default shape is preserved.
func marshalJSONB(rows []domain.EnvRow) ([]byte, error) {
	if len(rows) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(rows)
}

// unmarshalJSONB decodes a jsonb array of {name,value} objects; a nil result
// is normalized to the empty slice.
func unmarshalJSONB(data []byte) ([]domain.EnvRow, error) {
	var rows []domain.EnvRow
	if len(data) == 0 {
		return []domain.EnvRow{}, nil
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("%w: invalid env/headers jsonb: %v", domain.ErrInvalid, err)
	}
	if rows == nil {
		rows = []domain.EnvRow{}
	}
	return rows, nil
}

// isNameTakenViolation reports whether the error is a unique violation on one
// of the per-scope MCP server name constraints (uq_workspace_mcp_servers_
// workspace_id_name / uq_agent_mcp_servers_agent_id_name), as opposed to the
// primary key.
func isNameTakenViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgerrcode.UniqueViolation &&
			(pgErr.ConstraintName == "uq_workspace_mcp_servers_workspace_id_name" ||
				pgErr.ConstraintName == "uq_agent_mcp_servers_agent_id_name")
	}
	return false
}

// agentExistsInWorkspace mirrors the fake's Create guard: the owning agent
// must exist and belong to the server's workspace.
func (mss *agentMCPStore) agentExistsInWorkspace(ctx context.Context, workspaceID, agentID string) (bool, error) {
	var one bool
	err := mss.db.QueryRow(ctx,
		`SELECT true FROM agents WHERE workspace_id = $1 AND id = $2`,
		workspaceID, agentID,
	).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, convertError(err)
	}
	return true, nil
}

// -------------------------------------------------------------------------
// WorkspaceMCPServers implementation
// -------------------------------------------------------------------------

func (mss *workspaceMCPStore) Create(ctx context.Context, srv *domain.WorkspaceMCPServer) error {
	if srv == nil {
		return domain.ErrInvalid
	}
	if err := srv.Validate(); err != nil {
		return err
	}

	if srv.ID == "" {
		srv.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if srv.CreatedAt.IsZero() {
		srv.CreatedAt = now
	}
	if srv.UpdatedAt.IsZero() {
		srv.UpdatedAt = now
	}

	// args is NOT NULL in the schema; a nil slice would bind as SQL NULL.
	if srv.Args == nil {
		srv.Args = []string{}
	}
	// origin_connection_id is a nullable uuid; the entity's empty-string
	// "no origin" marker binds as SQL NULL (an empty string is not a uuid).
	var originID *string
	if srv.OriginConnectionID != "" {
		originID = &srv.OriginConnectionID
	}
	env, err := marshalJSONB(srv.Env)
	if err != nil {
		return convertError(err)
	}
	headers, err := marshalJSONB(srv.Headers)
	if err != nil {
		return convertError(err)
	}

	// Case-insensitive per-workspace name uniqueness (the schema constraint is
	// exact-only; see migration 000024) — pre-check like the fake.
	var exists bool
	err = mss.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM workspace_mcp_servers WHERE workspace_id = $1 AND lower(name) = lower($2))`,
		srv.WorkspaceID, srv.Name,
	).Scan(&exists)
	if err != nil {
		return convertError(err)
	}
	if exists {
		return fmt.Errorf("%w: workspace mcp server %q already exists in workspace", domain.ErrMCPServerNameTaken, srv.Name)
	}

	query := `
		INSERT INTO workspace_mcp_servers (
			id, workspace_id, name, transport, command, args, env, url, headers,
			enabled, status, status_error, tool_count, origin_connection_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
		)
	`
	_, err = mss.db.Exec(ctx, query,
		srv.ID,
		srv.WorkspaceID,
		srv.Name,
		srv.Transport,
		srv.Command,
		srv.Args,
		env,
		srv.URL,
		headers,
		srv.Enabled,
		srv.Status,
		srv.StatusError,
		srv.ToolCount,
		originID,
		srv.CreatedAt,
		srv.UpdatedAt,
	)
	if err != nil {
		if isNameTakenViolation(err) {
			return fmt.Errorf("%w: workspace mcp server %q already exists in workspace", domain.ErrMCPServerNameTaken, srv.Name)
		}
		return convertError(err)
	}
	return nil
}

func (mss *workspaceMCPStore) Get(ctx context.Context, workspaceID, id string) (*domain.WorkspaceMCPServer, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT id, workspace_id, name, transport, command, args, env, url, headers,
		       enabled, status, status_error, tool_count, origin_connection_id, created_at, updated_at
		FROM workspace_mcp_servers
		WHERE workspace_id = $1 AND id = $2
	`
	return scanWorkspaceServer(mss.db.QueryRow(ctx, query, workspaceID, id))
}

func scanWorkspaceServer(row pgx.Row) (*domain.WorkspaceMCPServer, error) {
	var srv domain.WorkspaceMCPServer
	var env, headers []byte
	var originID *string // NULL uuid = the entity's empty "no origin" marker
	err := row.Scan(
		&srv.ID,
		&srv.WorkspaceID,
		&srv.Name,
		&srv.Transport,
		&srv.Command,
		&srv.Args,
		&env,
		&srv.URL,
		&headers,
		&srv.Enabled,
		&srv.Status,
		&srv.StatusError,
		&srv.ToolCount,
		&originID,
		&srv.CreatedAt,
		&srv.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	if srv.Args == nil {
		srv.Args = []string{}
	}
	if originID != nil {
		srv.OriginConnectionID = *originID
	}
	if srv.Env, err = unmarshalJSONB(env); err != nil {
		return nil, err
	}
	if srv.Headers, err = unmarshalJSONB(headers); err != nil {
		return nil, err
	}
	return &srv, nil
}

func (mss *workspaceMCPStore) List(ctx context.Context, workspaceID string) ([]domain.WorkspaceMCPServer, error) {
	if workspaceID == "" {
		return []domain.WorkspaceMCPServer{}, nil
	}

	query := `
		SELECT id, workspace_id, name, transport, command, args, env, url, headers,
		       enabled, status, status_error, tool_count, origin_connection_id, created_at, updated_at
		FROM workspace_mcp_servers
		WHERE workspace_id = $1
		ORDER BY created_at ASC, id ASC
	`
	rows, err := mss.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	servers := make([]domain.WorkspaceMCPServer, 0)
	for rows.Next() {
		srv, err := scanWorkspaceServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, *srv)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return servers, nil
}

func (mss *workspaceMCPStore) Update(ctx context.Context, srv *domain.WorkspaceMCPServer) error {
	if srv == nil || srv.ID == "" || srv.WorkspaceID == "" {
		return domain.ErrInvalid
	}
	if err := srv.Validate(); err != nil {
		return err
	}

	// args is NOT NULL in the schema; a nil slice would bind as SQL NULL.
	if srv.Args == nil {
		srv.Args = []string{}
	}
	env, err := marshalJSONB(srv.Env)
	if err != nil {
		return convertError(err)
	}
	headers, err := marshalJSONB(srv.Headers)
	if err != nil {
		return convertError(err)
	}

	// Case-insensitive name uniqueness within the workspace, excluding this row.
	var exists bool
	err = mss.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM workspace_mcp_servers WHERE workspace_id = $1 AND lower(name) = lower($2) AND id <> $3)`,
		srv.WorkspaceID, srv.Name, srv.ID,
	).Scan(&exists)
	if err != nil {
		return convertError(err)
	}
	if exists {
		return fmt.Errorf("%w: workspace mcp server %q already exists in workspace", domain.ErrMCPServerNameTaken, srv.Name)
	}

	// Editable fields only: status, status_error, and tool_count persist until
	// the next SetStatus; created_at is immutable; origin_connection_id is
	// birth-stamped (design.md D11 — the connection is the managed server's
	// single authority) and is never rewritten here.
	query := `
		UPDATE workspace_mcp_servers
		SET name = $3,
		    transport = $4,
		    command = $5,
		    args = $6,
		    env = $7,
		    url = $8,
		    headers = $9,
		    enabled = $10,
		    updated_at = $11
		WHERE workspace_id = $1 AND id = $2
		RETURNING created_at, status, status_error, tool_count, origin_connection_id
	`
	var originID *string
	err = mss.db.QueryRow(ctx, query,
		srv.WorkspaceID,
		srv.ID,
		srv.Name,
		srv.Transport,
		srv.Command,
		srv.Args,
		env,
		srv.URL,
		headers,
		srv.Enabled,
		time.Now().UTC(),
	).Scan(&srv.CreatedAt, &srv.Status, &srv.StatusError, &srv.ToolCount, &originID)
	if err == nil {
		if originID != nil {
			srv.OriginConnectionID = *originID
		} else {
			srv.OriginConnectionID = ""
		}
	}
	if err != nil {
		if isNameTakenViolation(err) {
			return fmt.Errorf("%w: workspace mcp server %q already exists in workspace", domain.ErrMCPServerNameTaken, srv.Name)
		}
		return convertError(err)
	}
	return nil
}

func (mss *workspaceMCPStore) Delete(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	query := `
		DELETE FROM workspace_mcp_servers
		WHERE workspace_id = $1 AND id = $2
	`
	tag, err := mss.db.Exec(ctx, query, workspaceID, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (mss *workspaceMCPStore) SetStatus(ctx context.Context, workspaceID, id, status, statusError string, toolCount int) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	query := `
		UPDATE workspace_mcp_servers
		SET status = $3,
		    status_error = $4,
		    tool_count = $5,
		    updated_at = $6
		WHERE workspace_id = $1 AND id = $2
	`
	tag, err := mss.db.Exec(ctx, query, workspaceID, id, status, statusError, toolCount, time.Now().UTC())
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// GetByOriginConnection returns the workspace MCP server materialized by the
// given connection (add-workspace-connections design.md D1); (nil, nil) when
// the connection has no linked server.
func (mss *workspaceMCPStore) GetByOriginConnection(ctx context.Context, workspaceID, connectionID string) (*domain.WorkspaceMCPServer, error) {
	if workspaceID == "" || connectionID == "" {
		return nil, nil
	}

	query := `
		SELECT id, workspace_id, name, transport, command, args, env, url, headers,
		       enabled, status, status_error, tool_count, origin_connection_id, created_at, updated_at
		FROM workspace_mcp_servers
		WHERE workspace_id = $1 AND origin_connection_id = $2
		ORDER BY created_at ASC, id ASC
		LIMIT 1
	`
	srv, err := scanWorkspaceServer(mss.db.QueryRow(ctx, query, workspaceID, connectionID))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return srv, nil
}

// -------------------------------------------------------------------------
// AgentMCPServers implementation
// -------------------------------------------------------------------------

func (mss *agentMCPStore) Create(ctx context.Context, srv *domain.AgentMCPServer) error {
	if srv == nil {
		return domain.ErrInvalid
	}
	if err := srv.Validate(); err != nil {
		return err
	}

	// The owning agent must exist within the server's workspace (fake parity;
	// the plain agent_id FK alone would admit cross-workspace agents).
	found, err := mss.agentExistsInWorkspace(ctx, srv.WorkspaceID, srv.AgentID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
	}

	if srv.ID == "" {
		srv.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if srv.CreatedAt.IsZero() {
		srv.CreatedAt = now
	}
	if srv.UpdatedAt.IsZero() {
		srv.UpdatedAt = now
	}

	// args is NOT NULL in the schema; a nil slice would bind as SQL NULL.
	if srv.Args == nil {
		srv.Args = []string{}
	}
	env, err := marshalJSONB(srv.Env)
	if err != nil {
		return convertError(err)
	}
	headers, err := marshalJSONB(srv.Headers)
	if err != nil {
		return convertError(err)
	}

	// Case-insensitive per-agent name uniqueness (the schema constraint is
	// exact-only; see migration 000024) — pre-check like the fake.
	var exists bool
	err = mss.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM agent_mcp_servers WHERE agent_id = $1 AND lower(name) = lower($2))`,
		srv.AgentID, srv.Name,
	).Scan(&exists)
	if err != nil {
		return convertError(err)
	}
	if exists {
		return fmt.Errorf("%w: agent mcp server %q already exists for agent", domain.ErrMCPServerNameTaken, srv.Name)
	}

	query := `
		INSERT INTO agent_mcp_servers (
			id, workspace_id, agent_id, name, transport, command, args, env, url, headers,
			enabled, status, status_error, tool_count, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
		)
	`
	_, err = mss.db.Exec(ctx, query,
		srv.ID,
		srv.WorkspaceID,
		srv.AgentID,
		srv.Name,
		srv.Transport,
		srv.Command,
		srv.Args,
		env,
		srv.URL,
		headers,
		srv.Enabled,
		srv.Status,
		srv.StatusError,
		srv.ToolCount,
		srv.CreatedAt,
		srv.UpdatedAt,
	)
	if err != nil {
		if isNameTakenViolation(err) {
			return fmt.Errorf("%w: agent mcp server %q already exists for agent", domain.ErrMCPServerNameTaken, srv.Name)
		}
		return convertError(err)
	}
	return nil
}

func (mss *agentMCPStore) Get(ctx context.Context, agentID, id string) (*domain.AgentMCPServer, error) {
	if agentID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT id, workspace_id, agent_id, name, transport, command, args, env, url, headers,
		       enabled, status, status_error, tool_count, created_at, updated_at
		FROM agent_mcp_servers
		WHERE agent_id = $1 AND id = $2
	`
	return scanAgentServer(mss.db.QueryRow(ctx, query, agentID, id))
}

func scanAgentServer(row pgx.Row) (*domain.AgentMCPServer, error) {
	var srv domain.AgentMCPServer
	var env, headers []byte
	err := row.Scan(
		&srv.ID,
		&srv.WorkspaceID,
		&srv.AgentID,
		&srv.Name,
		&srv.Transport,
		&srv.Command,
		&srv.Args,
		&env,
		&srv.URL,
		&headers,
		&srv.Enabled,
		&srv.Status,
		&srv.StatusError,
		&srv.ToolCount,
		&srv.CreatedAt,
		&srv.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	if srv.Args == nil {
		srv.Args = []string{}
	}
	if srv.Env, err = unmarshalJSONB(env); err != nil {
		return nil, err
	}
	if srv.Headers, err = unmarshalJSONB(headers); err != nil {
		return nil, err
	}
	return &srv, nil
}

func (mss *agentMCPStore) List(ctx context.Context, agentID string) ([]domain.AgentMCPServer, error) {
	if agentID == "" {
		return []domain.AgentMCPServer{}, nil
	}

	query := `
		SELECT id, workspace_id, agent_id, name, transport, command, args, env, url, headers,
		       enabled, status, status_error, tool_count, created_at, updated_at
		FROM agent_mcp_servers
		WHERE agent_id = $1
		ORDER BY created_at ASC, id ASC
	`
	rows, err := mss.db.Query(ctx, query, agentID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	servers := make([]domain.AgentMCPServer, 0)
	for rows.Next() {
		srv, err := scanAgentServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, *srv)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return servers, nil
}

func (mss *agentMCPStore) Update(ctx context.Context, srv *domain.AgentMCPServer) error {
	if srv == nil || srv.ID == "" || srv.WorkspaceID == "" || srv.AgentID == "" {
		return domain.ErrInvalid
	}
	if err := srv.Validate(); err != nil {
		return err
	}

	// args is NOT NULL in the schema; a nil slice would bind as SQL NULL.
	if srv.Args == nil {
		srv.Args = []string{}
	}
	env, err := marshalJSONB(srv.Env)
	if err != nil {
		return convertError(err)
	}
	headers, err := marshalJSONB(srv.Headers)
	if err != nil {
		return convertError(err)
	}

	// Case-insensitive name uniqueness within the agent scope, excluding this row.
	var exists bool
	err = mss.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM agent_mcp_servers WHERE agent_id = $1 AND lower(name) = lower($2) AND id <> $3)`,
		srv.AgentID, srv.Name, srv.ID,
	).Scan(&exists)
	if err != nil {
		return convertError(err)
	}
	if exists {
		return fmt.Errorf("%w: agent mcp server %q already exists for agent", domain.ErrMCPServerNameTaken, srv.Name)
	}

	// Editable fields only: status, status_error, and tool_count persist until
	// the next SetStatus; created_at is immutable.
	query := `
		UPDATE agent_mcp_servers
		SET name = $4,
		    transport = $5,
		    command = $6,
		    args = $7,
		    env = $8,
		    url = $9,
		    headers = $10,
		    enabled = $11,
		    updated_at = $12
		WHERE agent_id = $2 AND workspace_id = $3 AND id = $1
		RETURNING created_at, status, status_error, tool_count
	`
	err = mss.db.QueryRow(ctx, query,
		srv.ID,
		srv.AgentID,
		srv.WorkspaceID,
		srv.Name,
		srv.Transport,
		srv.Command,
		srv.Args,
		env,
		srv.URL,
		headers,
		srv.Enabled,
		time.Now().UTC(),
	).Scan(&srv.CreatedAt, &srv.Status, &srv.StatusError, &srv.ToolCount)
	if err != nil {
		if isNameTakenViolation(err) {
			return fmt.Errorf("%w: agent mcp server %q already exists for agent", domain.ErrMCPServerNameTaken, srv.Name)
		}
		return convertError(err)
	}
	return nil
}

func (mss *agentMCPStore) Delete(ctx context.Context, agentID, id string) error {
	if agentID == "" || id == "" {
		return domain.ErrNotFound
	}

	query := `
		DELETE FROM agent_mcp_servers
		WHERE agent_id = $1 AND id = $2
	`
	tag, err := mss.db.Exec(ctx, query, agentID, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (mss *agentMCPStore) SetStatus(ctx context.Context, agentID, id, status, statusError string, toolCount int) error {
	if agentID == "" || id == "" {
		return domain.ErrNotFound
	}

	query := `
		UPDATE agent_mcp_servers
		SET status = $3,
		    status_error = $4,
		    tool_count = $5,
		    updated_at = $6
		WHERE agent_id = $1 AND id = $2
	`
	tag, err := mss.db.Exec(ctx, query, agentID, id, status, statusError, toolCount, time.Now().UTC())
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
