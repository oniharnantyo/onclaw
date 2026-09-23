package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// mcpTokenStore implements storeport.MCPTokens for PostgreSQL. The table
// (migration 000068) carries a two-column server pointer with an exactly-one
// CHECK: ws_server_id for workspace-scoped servers (agent_id NULL) and
// agent_server_id for agent-private servers (agent_id set), each unique and
// ON DELETE CASCADE — the schema enforces the one-token-set-per-server rule
// and the deletion cascades, so this adapter only maps the scope kind to the
// right columns.
type mcpTokenStore struct {
	db Executor
}

// NewMCPTokenStore creates a new MCPTokens store with the given database
// executor.
func NewMCPTokenStore(db Executor) storeport.MCPTokens {
	return &mcpTokenStore{db: db}
}

const mcpTokenColumns = `
	id, workspace_id, agent_id, ws_server_id, agent_server_id,
	access_ciphertext, refresh_ciphertext, expires_at, granted_scopes, issuer,
	created_at, updated_at
`

// mcpTokenWorkspaceScope addresses a workspace-scoped server's row (the
// agent_id IS NULL half of the scope CHECK).
const mcpTokenWorkspaceScope = `workspace_id = $1 AND agent_id IS NULL AND ws_server_id = $2`

// mcpTokenAgentScope addresses an agent-scoped server's row.
const mcpTokenAgentScope = `workspace_id = $1 AND agent_id = $2 AND agent_server_id = $3`

// scanMCPToken decodes one row; the scope pointer collapses back into the
// entity's (AgentID, ServerID) pair — ServerID is whichever server column is
// set.
func scanMCPToken(row pgx.Row) (*domain.MCPToken, error) {
	var t domain.MCPToken
	var agentID, wsServerID, agentServerID *string
	var expiresAt *time.Time
	err := row.Scan(
		&t.ID,
		&t.WorkspaceID,
		&agentID,
		&wsServerID,
		&agentServerID,
		&t.AccessTokenCiphertext,
		&t.RefreshTokenCiphertext,
		&expiresAt,
		&t.GrantedScopes,
		&t.Issuer,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	if agentID != nil {
		t.AgentID = *agentID
	}
	switch {
	case agentServerID != nil:
		t.ServerID = *agentServerID
	case wsServerID != nil:
		t.ServerID = *wsServerID
	}
	t.ExpiresAt = expiresAt
	if t.GrantedScopes == nil {
		t.GrantedScopes = []string{}
	}
	return &t, nil
}

// Replace create-or-replaces the server row's token set in one statement (the
// reauthorization/refresh write path: the whole row replaced atomically, id
// and created_at fixed at birth by the ON CONFLICT DO UPDATE). Unknown
// workspace/server/agent references fail the FKs and convert to
// domain.ErrNotFound.
func (m *mcpTokenStore) Replace(ctx context.Context, t *domain.MCPToken) error {
	if t == nil {
		return domain.ErrInvalid
	}
	if err := t.Validate(); err != nil {
		return err
	}
	if t.GrantedScopes == nil {
		t.GrantedScopes = []string{}
	}

	if t.AgentID == "" {
		query := `
			INSERT INTO mcp_oauth_tokens (
				workspace_id, ws_server_id, access_ciphertext, refresh_ciphertext,
				expires_at, granted_scopes, issuer
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (ws_server_id) DO UPDATE SET
				access_ciphertext  = EXCLUDED.access_ciphertext,
				refresh_ciphertext = EXCLUDED.refresh_ciphertext,
				expires_at         = EXCLUDED.expires_at,
				granted_scopes     = EXCLUDED.granted_scopes,
				issuer             = EXCLUDED.issuer,
				updated_at         = now()
			RETURNING ` + mcpTokenColumns + `
		`
		stored, err := scanMCPToken(m.db.QueryRow(ctx, query,
			t.WorkspaceID, t.ServerID, t.AccessTokenCiphertext, t.RefreshTokenCiphertext,
			t.ExpiresAt, t.GrantedScopes, t.Issuer,
		))
		if err != nil {
			return err
		}
		*t = *stored
		return nil
	}

	query := `
		INSERT INTO mcp_oauth_tokens (
			workspace_id, agent_id, agent_server_id, access_ciphertext, refresh_ciphertext,
			expires_at, granted_scopes, issuer
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (agent_server_id) DO UPDATE SET
			access_ciphertext  = EXCLUDED.access_ciphertext,
			refresh_ciphertext = EXCLUDED.refresh_ciphertext,
			expires_at         = EXCLUDED.expires_at,
			granted_scopes     = EXCLUDED.granted_scopes,
			issuer             = EXCLUDED.issuer,
			updated_at         = now()
		RETURNING ` + mcpTokenColumns + `
	`
	stored, err := scanMCPToken(m.db.QueryRow(ctx, query,
		t.WorkspaceID, t.AgentID, t.ServerID, t.AccessTokenCiphertext, t.RefreshTokenCiphertext,
		t.ExpiresAt, t.GrantedScopes, t.Issuer,
	))
	if err != nil {
		return err
	}
	*t = *stored
	return nil
}

func (m *mcpTokenStore) Get(ctx context.Context, workspaceID, agentID, serverID string) (*domain.MCPToken, error) {
	if workspaceID == "" || serverID == "" {
		return nil, domain.ErrNotFound
	}
	query := `SELECT ` + mcpTokenColumns + ` FROM mcp_oauth_tokens WHERE `
	if agentID == "" {
		return scanMCPToken(m.db.QueryRow(ctx, query+mcpTokenWorkspaceScope, workspaceID, serverID))
	}
	return scanMCPToken(m.db.QueryRow(ctx, query+mcpTokenAgentScope, workspaceID, agentID, serverID))
}

// GetForUpdate is Get under SELECT ... FOR UPDATE — meaningful inside a
// WithTx transaction (the row stays locked until commit); outside one the
// lock ends with the statement.
func (m *mcpTokenStore) GetForUpdate(ctx context.Context, workspaceID, agentID, serverID string) (*domain.MCPToken, error) {
	if workspaceID == "" || serverID == "" {
		return nil, domain.ErrNotFound
	}
	query := `SELECT ` + mcpTokenColumns + ` FROM mcp_oauth_tokens WHERE `
	if agentID == "" {
		return scanMCPToken(m.db.QueryRow(ctx, query+mcpTokenWorkspaceScope+` FOR UPDATE`, workspaceID, serverID))
	}
	return scanMCPToken(m.db.QueryRow(ctx, query+mcpTokenAgentScope+` FOR UPDATE`, workspaceID, agentID, serverID))
}

func (m *mcpTokenStore) Delete(ctx context.Context, workspaceID, agentID, serverID string) error {
	if workspaceID == "" || serverID == "" {
		return domain.ErrNotFound
	}
	query := `DELETE FROM mcp_oauth_tokens WHERE `
	var tag pgconn.CommandTag
	var err error
	if agentID == "" {
		tag, err = m.db.Exec(ctx, query+mcpTokenWorkspaceScope, workspaceID, serverID)
	} else {
		tag, err = m.db.Exec(ctx, query+mcpTokenAgentScope, workspaceID, agentID, serverID)
	}
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
