package sqlite

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type mcpServerStore struct{ e executor }

const mcpServerColumns = `id, name, transport, command, args, env, url, headers, auth,
service, enabled, status, status_error, tool_count, created_at, updated_at`

func (m *mcpServerStore) Create(ctx context.Context, server *store.MCPServer) error {
	if server.ID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("create mcp server: %w", err)
		}
		server.ID = id
	}
	stampCreated(&server.CreatedAt, &server.UpdatedAt)

	args, env, headers, auth, err := mcpJSON(server)
	if err != nil {
		return fmt.Errorf("create mcp server: %w", err)
	}

	_, err = m.e.ExecContext(ctx, `
INSERT INTO mcp_servers (`+mcpServerColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		server.ID, server.Name, server.Transport, server.Command, args, env, server.URL,
		headers, auth, server.Service, boolInt(server.Enabled), server.Status,
		server.StatusError, server.ToolCount, formatTime(server.CreatedAt),
		formatTime(server.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("create mcp server: %w", err)
	}
	return nil
}

func (m *mcpServerStore) ByID(ctx context.Context, id string) (*store.MCPServer, error) {
	row := m.e.QueryRowContext(ctx, `SELECT `+mcpServerColumns+` FROM mcp_servers WHERE id = ?`, id)
	server, err := scanMCPServer(row)
	if err != nil {
		return nil, readErr("get mcp server", err)
	}
	return server, nil
}

func (m *mcpServerStore) List(ctx context.Context) ([]store.MCPServer, error) {
	rows, err := m.e.QueryContext(ctx,
		`SELECT `+mcpServerColumns+` FROM mcp_servers ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list mcp servers: %w", err)
	}
	defer rows.Close()

	servers := make([]store.MCPServer, 0)
	for rows.Next() {
		server, err := scanMCPServer(rows)
		if err != nil {
			return nil, fmt.Errorf("list mcp servers: %w", err)
		}
		servers = append(servers, *server)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list mcp servers: %w", err)
	}
	return servers, nil
}

func (m *mcpServerStore) Update(ctx context.Context, server *store.MCPServer) error {
	server.UpdatedAt = nowUTC()
	args, env, headers, auth, err := mcpJSON(server)
	if err != nil {
		return fmt.Errorf("update mcp server: %w", err)
	}

	res, err := m.e.ExecContext(ctx, `
UPDATE mcp_servers SET name = ?, transport = ?, command = ?, args = ?, env = ?, url = ?,
headers = ?, auth = ?, service = ?, enabled = ?, status = ?, status_error = ?, tool_count = ?, updated_at = ?
WHERE id = ?`,
		server.Name, server.Transport, server.Command, args, env, server.URL, headers,
		auth, server.Service, boolInt(server.Enabled), server.Status, server.StatusError,
		server.ToolCount, formatTime(server.UpdatedAt), server.ID,
	)
	if err != nil {
		return fmt.Errorf("update mcp server: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update mcp server %s: %w", server.ID, store.ErrNotFound)
	}
	return nil
}

func (m *mcpServerStore) Delete(ctx context.Context, id string) error {
	res, err := m.e.ExecContext(ctx, `DELETE FROM mcp_servers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete mcp server: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete mcp server %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// mcpJSON encodes the list, map, and auth JSON columns of an MCP server row.
func mcpJSON(server *store.MCPServer) (args, env, headers, auth string, err error) {
	if args, err = jsonMarshal(server.Args); err != nil {
		return "", "", "", "", err
	}
	if env, err = jsonMarshal(server.Env); err != nil {
		return "", "", "", "", err
	}
	if headers, err = jsonMarshal(server.Headers); err != nil {
		return "", "", "", "", err
	}
	if auth, err = jsonMarshal(server.Auth); err != nil {
		return "", "", "", "", err
	}
	return args, env, headers, auth, nil
}

func scanMCPServer(row scanner) (*store.MCPServer, error) {
	var server store.MCPServer
	var createdAt, updatedAt, args, env, headers, auth string

	err := row.Scan(&server.ID, &server.Name, &server.Transport, &server.Command, &args,
		&env, &server.URL, &headers, &auth, &server.Service, &server.Enabled,
		&server.Status, &server.StatusError, &server.ToolCount, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if server.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if server.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	if err := jsonUnmarshal(args, &server.Args); err != nil {
		return nil, err
	}
	if err := jsonUnmarshal(env, &server.Env); err != nil {
		return nil, err
	}
	if err := jsonUnmarshal(headers, &server.Headers); err != nil {
		return nil, err
	}
	if err := jsonUnmarshal(auth, &server.Auth); err != nil {
		return nil, err
	}
	// Computed/redacted credential views (add-integrations design D4):
	// listings expose only whether a credential is set and its last four
	// characters — never the credential itself.
	if cred := server.Auth.IntegrationCredential; cred != "" {
		server.CredentialSet = true
		if len(cred) <= 4 {
			server.CredentialHint = cred
		} else {
			server.CredentialHint = cred[len(cred)-4:]
		}
	}
	return &server, nil
}
