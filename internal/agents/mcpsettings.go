package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// MCPSettingsService reads and writes workspace-registered and agent-private
// MCP servers over the two MCP store ports (design.md D10). It owns the
// secret-handling scheme shared with tool settings (design.md D4): env/header
// values are encrypted at rest with the workspace ID as AAD, writes accept
// plaintext (or pre-encrypted envelopes, which pass through untouched), and
// reads return hint views where every secret value is replaced by its last-4
// hint. The *ForRuntime accessors decrypt for connection attempts and must
// never surface to clients.
//
// Validation failures return a *ConfigValidationError (HTTP 422 with per-field
// details); unknown ids, servers from another workspace, and agents from
// another workspace all return domain.ErrNotFound indistinguishably.
type MCPSettingsService struct {
	wsServers    store.WorkspaceMCPServers
	agentServers store.AgentMCPServers
	agents       store.AgentStore
	encKey       []byte
}

// NewMCPSettingsService builds the service from its granular dependencies.
// The agents store exists for one purpose: proving an agent exists inside the
// named workspace before any agent-private MCP operation touches the store —
// the AgentMCPServers port is agent-scoped only, so this service is the
// workspace boundary for private servers (design.md D2).
func NewMCPSettingsService(wsServers store.WorkspaceMCPServers, agentServers store.AgentMCPServers, agents store.AgentStore, encKey []byte) *MCPSettingsService {
	return &MCPSettingsService{wsServers: wsServers, agentServers: agentServers, agents: agents, encKey: encKey}
}

// -------------------------------------------------------------------------
// Shared validation
// -------------------------------------------------------------------------

// validateMCPServerWrite collects fielded validation errors for a server
// connection: it must be valid for its transport (command for stdio, URL for
// the HTTP transports, well-formed name-keyed rows). The fielded pass mirrors
// domain.MCPConnection.Validate so every failure names its field; where it
// finds nothing, the domain validation runs as the authority before anything
// persists.
func validateMCPServerWrite(conn domain.MCPConnection) error {
	ve := &ConfigValidationError{}
	switch conn.Transport {
	case domain.MCPTransportStdio:
		if conn.Command == "" {
			ve.add("command", fmt.Sprintf("command is required for %s transport", domain.MCPTransportStdio))
		}
		addSecretRowFieldErrors(ve, "env", conn.Env)
	case domain.MCPTransportStreamableHTTP, domain.MCPTransportSSE:
		if conn.URL == "" {
			ve.add("url", fmt.Sprintf("url is required for %s transport", conn.Transport))
		}
		addSecretRowFieldErrors(ve, "headers", conn.Headers)
	default:
		ve.add("transport", fmt.Sprintf("transport %q must be one of %s, %s, or %s", conn.Transport, domain.MCPTransportStdio, domain.MCPTransportStreamableHTTP, domain.MCPTransportSSE))
	}
	if len(ve.Errors) == 0 {
		// Authority check: redundant when the fielded pass is exhaustive, but
		// nothing persists past a domain validation failure.
		if err := conn.Validate(); err != nil {
			ve.add("connection", err.Error())
		}
	}
	if len(ve.Errors) == 0 {
		return nil
	}
	return ve
}

// addSecretRowFieldErrors validates one name-keyed row list: names are
// required and unique within the list (exactly, matching the domain rule).
func addSecretRowFieldErrors(ve *ConfigValidationError, field string, rows []domain.EnvRow) {
	seen := make(map[string]bool, len(rows))
	for i, row := range rows {
		prefix := fmt.Sprintf("%s[%d].name", field, i)
		if strings.TrimSpace(row.Name) == "" {
			ve.add(prefix, fmt.Sprintf("%s row %d: name is required", field, i+1))
			continue
		}
		if seen[row.Name] {
			ve.add(prefix, fmt.Sprintf("%s row %d: name %q is duplicated", field, i+1, row.Name))
		}
		seen[row.Name] = true
	}
}

// validateMCPServerName rejects a blank server name as a fielded error.
func validateMCPServerName(name string) error {
	if strings.TrimSpace(name) == "" {
		ve := &ConfigValidationError{}
		ve.add("name", "name is required")
		return ve
	}
	return nil
}

// -------------------------------------------------------------------------
// Shared secret plumbing (design.md D4)
// -------------------------------------------------------------------------

// encryptConnection encrypts a connection's secret row values in place with
// the workspace ID as AAD — agent-private rows use the same workspace-AAD
// derivation as registry rows. Envelopes pass through untouched; plaintext
// never survives a write.
func (s *MCPSettingsService) encryptConnection(workspaceID string, conn *domain.MCPConnection) error {
	aad := secretAAD(workspaceID)
	if err := encryptSecretRows(s.encKey, aad, conn.Env); err != nil {
		return err
	}
	return encryptSecretRows(s.encKey, aad, conn.Headers)
}

// hintConnection replaces a connection's secret row values with their last-4
// hints in place — the read-view shape.
func (s *MCPSettingsService) hintConnection(workspaceID string, conn *domain.MCPConnection) {
	aad := secretAAD(workspaceID)
	hintSecretRows(s.encKey, aad, conn.Env)
	hintSecretRows(s.encKey, aad, conn.Headers)
}

// decryptConnection replaces a connection's envelope row values with plaintext
// in place — runtime use only.
func (s *MCPSettingsService) decryptConnection(workspaceID string, conn *domain.MCPConnection) {
	aad := secretAAD(workspaceID)
	decryptSecretRows(s.encKey, aad, conn.Env)
	decryptSecretRows(s.encKey, aad, conn.Headers)
}

// mergeConnectionSecrets applies the name-keyed secret merge to a connection
// against the stored one: rows re-supplied with an empty value keep the
// stored secret, non-empty values (plaintext or envelope) replace them.
func mergeConnectionSecrets(incoming, stored *domain.MCPConnection) {
	incoming.Env = mergeSecretRows(incoming.Env, stored.Env)
	incoming.Headers = mergeSecretRows(incoming.Headers, stored.Headers)
}

// -------------------------------------------------------------------------
// Shared status guard
// -------------------------------------------------------------------------

// validateMCPStatus accepts only the persisted probe statuses (or empty —
// not yet probed).
func validateMCPStatus(status string) error {
	switch status {
	case "", domain.MCPStatusConnected, domain.MCPStatusError:
		return nil
	default:
		return fmt.Errorf("%w: unknown mcp status %q", domain.ErrInvalid, status)
	}
}

// -------------------------------------------------------------------------
// Workspace scope (design.md D2: the shared, admin-managed registry)
// -------------------------------------------------------------------------

// CreateWorkspaceServer registers a workspace MCP server. The connection is
// validated per transport and secret row values are encrypted in place before
// persistence, so the caller's entity never carries plaintext after the call.
// Enabled is persisted as given — the API layer applies the default-enabled
// registration rule; status stays empty until the first probe.
func (s *MCPSettingsService) CreateWorkspaceServer(ctx context.Context, server *domain.WorkspaceMCPServer) error {
	if server == nil || server.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", domain.ErrInvalid)
	}
	if err := validateMCPServerName(server.Name); err != nil {
		return err
	}
	if err := validateMCPServerWrite(server.MCPConnection); err != nil {
		return err
	}
	if err := s.encryptConnection(server.WorkspaceID, &server.MCPConnection); err != nil {
		return err
	}
	if err := s.wsServers.Create(ctx, server); err != nil {
		if errors.Is(err, domain.ErrMCPServerNameTaken) {
			return s.workspaceNameTaken(ctx, server.WorkspaceID, server.Name)
		}
		return err
	}
	return nil
}

// UpdateWorkspaceServer replaces a server's editable fields (name, connection,
// enabled switch). Incoming secret rows merge on NAME against the stored row:
// an empty value keeps the stored secret, a non-empty value replaces it, and
// already-encrypted envelopes pass through untouched. Status, status_error,
// and tool_count persist until the next SetStatus (store semantics).
func (s *MCPSettingsService) UpdateWorkspaceServer(ctx context.Context, server *domain.WorkspaceMCPServer) error {
	if server == nil || server.ID == "" || server.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id and server id cannot be empty", domain.ErrInvalid)
	}
	if err := validateMCPServerName(server.Name); err != nil {
		return err
	}
	if err := validateMCPServerWrite(server.MCPConnection); err != nil {
		return err
	}
	existing, err := s.wsServers.Get(ctx, server.WorkspaceID, server.ID)
	if err != nil {
		return err // domain.ErrNotFound: unknown id or another workspace's
	}
	mergeConnectionSecrets(&server.MCPConnection, &existing.MCPConnection)
	if err := s.encryptConnection(server.WorkspaceID, &server.MCPConnection); err != nil {
		return err
	}
	if err := s.wsServers.Update(ctx, server); err != nil {
		if errors.Is(err, domain.ErrMCPServerNameTaken) {
			return s.workspaceNameTaken(ctx, server.WorkspaceID, server.Name)
		}
		return err
	}
	return nil
}

// DeleteWorkspaceServer removes a registry entry. Agent opt-in references
// (Agent.EnabledMCPS) become inert by design (design.md D1) — agent rows are
// not rewritten.
func (s *MCPSettingsService) DeleteWorkspaceServer(ctx context.Context, workspaceID, id string) error {
	return s.wsServers.Delete(ctx, workspaceID, id)
}

// SetWorkspaceServerEnabled flips the master switch. The stored connection is
// re-persisted untouched (envelopes pass through), so this never disturbs
// secrets or probe status.
func (s *MCPSettingsService) SetWorkspaceServerEnabled(ctx context.Context, workspaceID, id string, enabled bool) error {
	row, err := s.wsServers.Get(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	row.Enabled = enabled
	return s.wsServers.Update(ctx, row)
}

// SetWorkspaceServerStatus persists a probe or runtime connection outcome:
// status, failure message, and exposed-tool count.
func (s *MCPSettingsService) SetWorkspaceServerStatus(ctx context.Context, workspaceID, id, status, statusError string, toolCount int) error {
	if err := validateMCPStatus(status); err != nil {
		return err
	}
	return s.wsServers.SetStatus(ctx, workspaceID, id, status, statusError, toolCount)
}

// ListWorkspaceServers returns the workspace's registered servers as hint
// views: every env/header value is replaced by its last-4 hint — no plaintext
// or ciphertext is present.
func (s *MCPSettingsService) ListWorkspaceServers(ctx context.Context, workspaceID string) ([]domain.WorkspaceMCPServer, error) {
	rows, err := s.wsServers.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		s.hintConnection(workspaceID, &rows[i].MCPConnection)
	}
	return rows, nil
}

// WorkspaceServer returns one registered server as a hint view. A server from
// another workspace is domain.ErrNotFound, indistinguishable from an unknown
// id.
func (s *MCPSettingsService) WorkspaceServer(ctx context.Context, workspaceID, id string) (*domain.WorkspaceMCPServer, error) {
	row, err := s.wsServers.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	s.hintConnection(workspaceID, &row.MCPConnection)
	return row, nil
}

// WorkspaceServersForRuntime returns the workspace's servers with secret row
// values decrypted for connection attempts (manager, probe). Runtime-only:
// never surface these rows to clients.
func (s *MCPSettingsService) WorkspaceServersForRuntime(ctx context.Context, workspaceID string) ([]domain.WorkspaceMCPServer, error) {
	rows, err := s.wsServers.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		s.decryptConnection(workspaceID, &rows[i].MCPConnection)
	}
	return rows, nil
}

// WorkspaceServerForRuntime returns one server with secret row values
// decrypted for a connection attempt. Runtime-only.
func (s *MCPSettingsService) WorkspaceServerForRuntime(ctx context.Context, workspaceID, id string) (*domain.WorkspaceMCPServer, error) {
	row, err := s.wsServers.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	s.decryptConnection(workspaceID, &row.MCPConnection)
	return row, nil
}

// workspaceNameTaken builds the fielded duplicate-name error, naming the
// conflicting server when a scan finds it. The store's uniqueness constraint
// stays authoritative; this only enriches the message.
func (s *MCPSettingsService) workspaceNameTaken(ctx context.Context, workspaceID, name string) error {
	ve := &ConfigValidationError{}
	message := "mcp server name is already used in this workspace"
	if rows, err := s.wsServers.List(ctx, workspaceID); err == nil {
		for _, row := range rows {
			if strings.EqualFold(row.Name, name) {
				message = fmt.Sprintf("mcp server name %q is already used by another server in this workspace", row.Name)
				break
			}
		}
	}
	ve.add("name", message)
	return ve
}

// -------------------------------------------------------------------------
// Agent scope (design.md D2: agent-private servers)
// -------------------------------------------------------------------------

// resolveAgentScope verifies the agent exists inside the workspace before any
// agent-scope MCP operation touches the MCP store. Unknown agents and agents
// of another workspace are domain.ErrNotFound, indistinguishable from an
// unknown server id.
func (s *MCPSettingsService) resolveAgentScope(ctx context.Context, workspaceID, agentID string) error {
	if workspaceID == "" || agentID == "" {
		return fmt.Errorf("%w: workspace id and agent id cannot be empty", domain.ErrInvalid)
	}
	if _, err := s.agents.ByID(ctx, workspaceID, agentID); err != nil {
		return err
	}
	return nil
}

// CreateAgentServer attaches a private MCP server to an agent. The agent must
// exist in the server's workspace; the connection follows the same
// validation, encryption (workspace-AAD), and secrecy rules as registry
// servers; names are unique per agent.
func (s *MCPSettingsService) CreateAgentServer(ctx context.Context, server *domain.AgentMCPServer) error {
	if server == nil {
		return fmt.Errorf("%w: server cannot be nil", domain.ErrInvalid)
	}
	if err := s.resolveAgentScope(ctx, server.WorkspaceID, server.AgentID); err != nil {
		return err
	}
	if err := validateMCPServerName(server.Name); err != nil {
		return err
	}
	if err := validateMCPServerWrite(server.MCPConnection); err != nil {
		return err
	}
	if err := s.encryptConnection(server.WorkspaceID, &server.MCPConnection); err != nil {
		return err
	}
	if err := s.agentServers.Create(ctx, server); err != nil {
		if errors.Is(err, domain.ErrMCPServerNameTaken) {
			return s.agentNameTaken(ctx, server.WorkspaceID, server.AgentID, server.Name)
		}
		return err
	}
	return nil
}

// UpdateAgentServer replaces a private server's editable fields with the same
// name-keyed secret merge as the workspace scope. The agent scope is verified
// before the store is touched.
func (s *MCPSettingsService) UpdateAgentServer(ctx context.Context, server *domain.AgentMCPServer) error {
	if server == nil || server.ID == "" {
		return fmt.Errorf("%w: server id cannot be empty", domain.ErrInvalid)
	}
	if err := s.resolveAgentScope(ctx, server.WorkspaceID, server.AgentID); err != nil {
		return err
	}
	if err := validateMCPServerName(server.Name); err != nil {
		return err
	}
	if err := validateMCPServerWrite(server.MCPConnection); err != nil {
		return err
	}
	existing, err := s.agentServers.Get(ctx, server.AgentID, server.ID)
	if err != nil {
		return err // domain.ErrNotFound: unknown id or another agent's
	}
	mergeConnectionSecrets(&server.MCPConnection, &existing.MCPConnection)
	if err := s.encryptConnection(server.WorkspaceID, &server.MCPConnection); err != nil {
		return err
	}
	if err := s.agentServers.Update(ctx, server); err != nil {
		if errors.Is(err, domain.ErrMCPServerNameTaken) {
			return s.agentNameTaken(ctx, server.WorkspaceID, server.AgentID, server.Name)
		}
		return err
	}
	return nil
}

// DeleteAgentServer removes a private server. Only the owning agent's scope
// can address it.
func (s *MCPSettingsService) DeleteAgentServer(ctx context.Context, workspaceID, agentID, id string) error {
	if err := s.resolveAgentScope(ctx, workspaceID, agentID); err != nil {
		return err
	}
	return s.agentServers.Delete(ctx, agentID, id)
}

// SetAgentServerEnabled flips a private server's master switch without
// disturbing secrets or probe status.
func (s *MCPSettingsService) SetAgentServerEnabled(ctx context.Context, workspaceID, agentID, id string, enabled bool) error {
	if err := s.resolveAgentScope(ctx, workspaceID, agentID); err != nil {
		return err
	}
	row, err := s.agentServers.Get(ctx, agentID, id)
	if err != nil {
		return err
	}
	row.Enabled = enabled
	return s.agentServers.Update(ctx, row)
}

// SetAgentServerStatus persists a probe or runtime connection outcome for a
// private server.
func (s *MCPSettingsService) SetAgentServerStatus(ctx context.Context, workspaceID, agentID, id, status, statusError string, toolCount int) error {
	if err := validateMCPStatus(status); err != nil {
		return err
	}
	if err := s.resolveAgentScope(ctx, workspaceID, agentID); err != nil {
		return err
	}
	return s.agentServers.SetStatus(ctx, agentID, id, status, statusError, toolCount)
}

// ListAgentServers returns the agent's private servers as hint views. Servers
// belonging to other agents (even in the same workspace) are invisible here.
func (s *MCPSettingsService) ListAgentServers(ctx context.Context, workspaceID, agentID string) ([]domain.AgentMCPServer, error) {
	if err := s.resolveAgentScope(ctx, workspaceID, agentID); err != nil {
		return nil, err
	}
	rows, err := s.agentServers.List(ctx, agentID)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		s.hintConnection(workspaceID, &rows[i].MCPConnection)
	}
	return rows, nil
}

// AgentServer returns one private server as a hint view. A server of another
// agent is domain.ErrNotFound, indistinguishable from an unknown id.
func (s *MCPSettingsService) AgentServer(ctx context.Context, workspaceID, agentID, id string) (*domain.AgentMCPServer, error) {
	if err := s.resolveAgentScope(ctx, workspaceID, agentID); err != nil {
		return nil, err
	}
	row, err := s.agentServers.Get(ctx, agentID, id)
	if err != nil {
		return nil, err
	}
	s.hintConnection(workspaceID, &row.MCPConnection)
	return row, nil
}

// AgentServersForRuntime returns the agent's private servers with secret row
// values decrypted for connection attempts. Runtime-only.
func (s *MCPSettingsService) AgentServersForRuntime(ctx context.Context, workspaceID, agentID string) ([]domain.AgentMCPServer, error) {
	if err := s.resolveAgentScope(ctx, workspaceID, agentID); err != nil {
		return nil, err
	}
	rows, err := s.agentServers.List(ctx, agentID)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		s.decryptConnection(workspaceID, &rows[i].MCPConnection)
	}
	return rows, nil
}

// AgentServerForRuntime returns one private server with secret row values
// decrypted for a connection attempt. Runtime-only.
func (s *MCPSettingsService) AgentServerForRuntime(ctx context.Context, workspaceID, agentID, id string) (*domain.AgentMCPServer, error) {
	if err := s.resolveAgentScope(ctx, workspaceID, agentID); err != nil {
		return nil, err
	}
	row, err := s.agentServers.Get(ctx, agentID, id)
	if err != nil {
		return nil, err
	}
	s.decryptConnection(workspaceID, &row.MCPConnection)
	return row, nil
}

// AgentServersForRuntimeByID resolves an agent's private servers with secret
// row values decrypted for connection attempts, addressed by agent id alone —
// the runtime policy port (mcp.MCPPolicy.AgentServers) is agent-scoped because
// the runner already holds the agent. The workspace scope comes off the rows
// themselves: every private row carries its agent's workspace id, so each row
// decrypts under the same workspace-AAD derivation it was written with. Rows
// are returned enabled-gated by NO ONE here — the policy owns the master-switch
// filter. An agent with no private servers (including a deleted one, whose rows
// cascaded away) yields an empty list. Runtime-only: never surface these rows
// to clients.
func (s *MCPSettingsService) AgentServersForRuntimeByID(ctx context.Context, agentID string) ([]domain.AgentMCPServer, error) {
	if agentID == "" {
		return []domain.AgentMCPServer{}, nil
	}
	rows, err := s.agentServers.List(ctx, agentID)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		s.decryptConnection(rows[i].WorkspaceID, &rows[i].MCPConnection)
	}
	return rows, nil
}

// SetAgentServerStatusByID persists a probe or runtime connection outcome for
// a private server, addressed by agent id alone (the runtime StatusWriter seam
// is agent-scoped, mirroring the policy port). The guarded persistence keeps
// the service's status vocabulary check; the agent-scoped Get is the tenancy
// guard — an unknown id or another agent's server is domain.ErrNotFound.
func (s *MCPSettingsService) SetAgentServerStatusByID(ctx context.Context, agentID, id, status, statusError string, toolCount int) error {
	if err := validateMCPStatus(status); err != nil {
		return err
	}
	if _, err := s.agentServers.Get(ctx, agentID, id); err != nil {
		return err
	}
	return s.agentServers.SetStatus(ctx, agentID, id, status, statusError, toolCount)
}

// agentNameTaken builds the fielded duplicate-name error for an agent's
// private servers, naming the conflicting server when a scan finds it.
func (s *MCPSettingsService) agentNameTaken(ctx context.Context, workspaceID, agentID, name string) error {
	ve := &ConfigValidationError{}
	message := "mcp server name is already used by another private server on this agent"
	if rows, err := s.agentServers.List(ctx, agentID); err == nil {
		for _, row := range rows {
			if strings.EqualFold(row.Name, name) {
				message = fmt.Sprintf("mcp server name %q is already used by another private server on this agent", row.Name)
				break
			}
		}
	}
	ve.add("name", message)
	return ve
}
