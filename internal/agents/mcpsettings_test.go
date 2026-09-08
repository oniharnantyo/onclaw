package agents

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// -------------------------------------------------------------------------
// Fixtures
// -------------------------------------------------------------------------

// mcpFixture wires an MCPSettingsService over the in-memory store with one
// workspace ("acme"), one provider, and one agent ("atlas"). Returns the
// service, the raw store, the workspace id, and the agent id.
func mcpFixture(t *testing.T) (*MCPSettingsService, store.Store, string, string) {
	t.Helper()
	st := fake.New()
	wsID := seedMCPWorkspace(t, st, "acme")
	agentID := seedMCPAgent(t, st, wsID, "atlas")
	svc := NewMCPSettingsService(st.WorkspaceMCPServers(), st.AgentMCPServers(), st.Agents(), []byte(testEncryptionKey))
	return svc, st, wsID, agentID
}

func seedMCPWorkspace(t *testing.T, st store.Store, slug string) string {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: slug}
	if err := st.Workspaces().Create(context.Background(), ws); err != nil {
		t.Fatalf("create workspace %s: %v", slug, err)
	}
	return ws.ID
}

func seedMCPAgent(t *testing.T, st store.Store, workspaceID, slug string) string {
	t.Helper()
	pv := &domain.ProviderConfig{WorkspaceID: workspaceID, Type: "openai", Name: "OpenAI"}
	if err := st.Providers().Create(context.Background(), pv); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	agent := &domain.Agent{WorkspaceID: workspaceID, Name: slug, Slug: slug, ProviderID: pv.ID, Model: "gpt-4o"}
	if err := st.Agents().Create(context.Background(), agent); err != nil {
		t.Fatalf("create agent %s: %v", slug, err)
	}
	return agent.ID
}

// stdioConn builds a stdio connection carrying one env secret.
func stdioConn() domain.MCPConnection {
	return domain.MCPConnection{
		Transport: domain.MCPTransportStdio,
		Command:   "npx",
		Args:      []string{"-y", "@modelcontextprotocol/server-github"},
		Env:       []domain.EnvRow{{Name: "GITHUB_TOKEN", Value: "ghp_supersecret_1234"}},
	}
}

// httpConn builds a streamable HTTP connection carrying one header secret.
func httpConn() domain.MCPConnection {
	return domain.MCPConnection{
		Transport: domain.MCPTransportStreamableHTTP,
		URL:       "https://mcp.example.com/mcp",
		Headers:   []domain.EnvRow{{Name: "Authorization", Value: "Bearer sk-live-9999"}},
	}
}

// assertMCPFieldError asserts a *ConfigValidationError naming field with a
// message containing wantSubstring.
func assertMCPFieldError(t *testing.T, err error, field, wantSubstring string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected fielded validation error on %q containing %q, got nil", field, wantSubstring)
	}
	var cfgErr *ConfigValidationError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected *ConfigValidationError, got %T: %v", err, err)
	}
	for _, fe := range cfgErr.Errors {
		if fe.Field == field && strings.Contains(fe.Message, wantSubstring) {
			return
		}
	}
	t.Fatalf("expected fielded error on %q containing %q, got %v", field, wantSubstring, cfgErr.Errors)
}

// storedWorkspaceServer fetches the at-rest row through the raw store port
// (ciphertext intact, no hinting).
func storedWorkspaceServer(t *testing.T, st store.Store, workspaceID, id string) *domain.WorkspaceMCPServer {
	t.Helper()
	row, err := st.WorkspaceMCPServers().Get(context.Background(), workspaceID, id)
	if err != nil {
		t.Fatalf("stored workspace server: %v", err)
	}
	return row
}

func storedAgentServer(t *testing.T, st store.Store, agentID, id string) *domain.AgentMCPServer {
	t.Helper()
	row, err := st.AgentMCPServers().Get(context.Background(), agentID, id)
	if err != nil {
		t.Fatalf("stored agent server: %v", err)
	}
	return row
}

func decryptAtRest(t *testing.T, workspaceID, envelope string) string {
	t.Helper()
	plain, err := secrets.Decrypt([]byte(testEncryptionKey), []byte(workspaceID), envelope)
	if err != nil {
		t.Fatalf("decrypt stored envelope: %v", err)
	}
	return string(plain)
}

// -------------------------------------------------------------------------
// Workspace registry — spec: workspace-mcp "Workspace MCP registry"
// -------------------------------------------------------------------------

// Scenario: Register a stdio server — the server exists enabled with its
// config stored and no credentials echoed back.
func TestMCPServer_CreateWorkspaceStdio(t *testing.T) {
	svc, st, wsID, _ := mcpFixture(t)

	server := &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "GitHub", MCPConnection: stdioConn(), Enabled: true}
	if err := svc.CreateWorkspaceServer(context.Background(), server); err != nil {
		t.Fatalf("create: %v", err)
	}
	if server.ID == "" {
		t.Fatal("create must assign an id")
	}

	stored := storedWorkspaceServer(t, st, wsID, server.ID)
	if !stored.Enabled {
		t.Fatal("registered server must be enabled")
	}
	if stored.Transport != domain.MCPTransportStdio || stored.Command != "npx" || len(stored.Args) != 2 {
		t.Fatalf("connection config must be stored verbatim, got %+v", stored.MCPConnection)
	}
	if !isSecretEnvelope(stored.Env[0].Value) {
		t.Fatalf("env secret must be encrypted at rest, got %q", stored.Env[0].Value)
	}
	if strings.Contains(stored.Env[0].Value, "ghp_supersecret") {
		t.Fatal("plaintext must not survive a create")
	}

	view, err := svc.WorkspaceServer(context.Background(), wsID, server.ID)
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if view.Env[0].Value != "1234" {
		t.Fatalf("view must show only the last-4 hint, got %q", view.Env[0].Value)
	}
}

// Scenario: Register a streamable HTTP server — the header value is stored
// encrypted and the response shows only a hint for it.
func TestMCPServer_CreateWorkspaceHTTPHeaderEncrypted(t *testing.T) {
	svc, st, wsID, _ := mcpFixture(t)

	server := &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "Remote", MCPConnection: httpConn(), Enabled: true}
	if err := svc.CreateWorkspaceServer(context.Background(), server); err != nil {
		t.Fatalf("create: %v", err)
	}

	stored := storedWorkspaceServer(t, st, wsID, server.ID)
	if !isSecretEnvelope(stored.Headers[0].Value) {
		t.Fatalf("header secret must be encrypted at rest, got %q", stored.Headers[0].Value)
	}
	if decryptAtRest(t, wsID, stored.Headers[0].Value) != "Bearer sk-live-9999" {
		t.Fatal("stored envelope must decrypt to the supplied header value")
	}

	views, err := svc.ListWorkspaceServers(context.Background(), wsID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(views) != 1 || views[0].Headers[0].Value != "9999" {
		t.Fatalf("list must show only the hint, got %+v", views)
	}
	if views[0].Headers[0].Name != "Authorization" {
		t.Fatal("row names are plain text and must pass through")
	}
	if views[0].URL != "https://mcp.example.com/mcp" {
		t.Fatal("urls are plain text and must pass through")
	}
}

// Scenario: Duplicate name rejected — a create or rename submitting a name
// already used in the same workspace (case-insensitive) is rejected with a
// fielded validation error naming the conflict.
func TestMCPServer_WorkspaceDuplicateNameRejected(t *testing.T) {
	svc, _, wsID, _ := mcpFixture(t)
	ctx := context.Background()

	first := &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "GitHub", MCPConnection: stdioConn(), Enabled: true}
	if err := svc.CreateWorkspaceServer(ctx, first); err != nil {
		t.Fatalf("create first: %v", err)
	}

	err := svc.CreateWorkspaceServer(ctx, &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "github", MCPConnection: stdioConn(), Enabled: true})
	assertMCPFieldError(t, err, "name", `"GitHub"`)

	// A rename onto another server's name collides the same way.
	second := &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "Other", MCPConnection: httpConn(), Enabled: true}
	if err := svc.CreateWorkspaceServer(ctx, second); err != nil {
		t.Fatalf("create second: %v", err)
	}
	second.Name = "GITHUB"
	second.MCPConnection = httpConn()
	err = svc.UpdateWorkspaceServer(ctx, second)
	assertMCPFieldError(t, err, "name", `"GitHub"`)

	// Keeping a row's own name (even re-cased) never collides.
	second.Name = "OTHER"
	if err := svc.UpdateWorkspaceServer(ctx, second); err != nil {
		t.Fatalf("rename to own name re-cased must save: %v", err)
	}
}

// Scenario: Transport config validated per transport — stdio without a
// command, streamable HTTP without a URL, unknown transports, blank names,
// and malformed secret rows are fielded validation errors; nothing persists.
func TestMCPServer_WorkspaceTransportValidation(t *testing.T) {
	svc, st, wsID, _ := mcpFixture(t)
	ctx := context.Background()

	noCommand := &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "Broken", MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio}, Enabled: true}
	err := svc.CreateWorkspaceServer(ctx, noCommand)
	assertMCPFieldError(t, err, "command", "command is required")

	noURL := &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "Broken", MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStreamableHTTP}, Enabled: true}
	err = svc.CreateWorkspaceServer(ctx, noURL)
	assertMCPFieldError(t, err, "url", "url is required")

	badTransport := &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "Broken", MCPConnection: domain.MCPConnection{Transport: "websocket", URL: "wss://x"}, Enabled: true}
	err = svc.CreateWorkspaceServer(ctx, badTransport)
	assertMCPFieldError(t, err, "transport", "websocket")

	blankName := &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "  ", MCPConnection: stdioConn(), Enabled: true}
	err = svc.CreateWorkspaceServer(ctx, blankName)
	assertMCPFieldError(t, err, "name", "name is required")

	dupRow := stdioConn()
	dupRow.Env = append(dupRow.Env, domain.EnvRow{Name: "GITHUB_TOKEN", Value: "second"})
	err = svc.CreateWorkspaceServer(ctx, &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "Broken", MCPConnection: dupRow, Enabled: true})
	assertMCPFieldError(t, err, "env[1].name", "duplicated")

	blankRow := stdioConn()
	blankRow.Env = append(blankRow.Env, domain.EnvRow{Value: "noname"})
	err = svc.CreateWorkspaceServer(ctx, &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "Broken", MCPConnection: blankRow, Enabled: true})
	assertMCPFieldError(t, err, "env[1].name", "name is required")

	rows, err := st.WorkspaceMCPServers().List(ctx, wsID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("rejected creates must not persist, got %v rows (%v)", len(rows), err)
	}
}

// Scenario: Master switch pauses a server — no agent's executions expose its
// tools while paused, regardless of agent opt-in. The service's part: the
// flip persists and runtime reads report the paused state.
func TestMCPServer_WorkspaceMasterSwitch(t *testing.T) {
	svc, _, wsID, _ := mcpFixture(t)
	ctx := context.Background()

	server := &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "GitHub", MCPConnection: stdioConn(), Enabled: true}
	if err := svc.CreateWorkspaceServer(ctx, server); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.SetWorkspaceServerEnabled(ctx, wsID, server.ID, false); err != nil {
		t.Fatalf("pause: %v", err)
	}
	view, err := svc.WorkspaceServer(ctx, wsID, server.ID)
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if view.Enabled {
		t.Fatal("server must be paused after the flip")
	}
	runtime, err := svc.WorkspaceServersForRuntime(ctx, wsID)
	if err != nil {
		t.Fatalf("runtime list: %v", err)
	}
	if len(runtime) != 1 || runtime[0].Enabled {
		t.Fatalf("runtime reads must report the paused master switch, got %+v", runtime)
	}

	if err := svc.SetWorkspaceServerEnabled(ctx, wsID, server.ID, true); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if view, _ = svc.WorkspaceServer(ctx, wsID, server.ID); !view.Enabled {
		t.Fatal("server must be enabled after resuming")
	}
}

// Scenario: Delete leaves references inert — the delete succeeds and agents
// keep the stale id, which contributes no tools and raises no error. The
// service never rewrites agent rows.
func TestMCPServer_WorkspaceDeleteReferencesInert(t *testing.T) {
	svc, st, wsID, agentID := mcpFixture(t)
	ctx := context.Background()

	server := &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "GitHub", MCPConnection: stdioConn(), Enabled: true}
	if err := svc.CreateWorkspaceServer(ctx, server); err != nil {
		t.Fatalf("create: %v", err)
	}

	// An agent opts in to the server.
	agent, err := st.Agents().ByID(ctx, wsID, agentID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	agent.EnabledMCPS = []string{server.ID}
	if err := st.Agents().Update(ctx, agent); err != nil {
		t.Fatalf("opt in: %v", err)
	}

	if err := svc.DeleteWorkspaceServer(ctx, wsID, server.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.WorkspaceServer(ctx, wsID, server.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted server must be gone, got %v", err)
	}
	agent, err = st.Agents().ByID(ctx, wsID, agentID)
	if err != nil {
		t.Fatalf("reload agent: %v", err)
	}
	if len(agent.EnabledMCPS) != 1 || agent.EnabledMCPS[0] != server.ID {
		t.Fatalf("stale reference must be left inert, got %v", agent.EnabledMCPS)
	}
}

// -------------------------------------------------------------------------
// Credential secrecy — spec: workspace-mcp "MCP credential secrecy"
// -------------------------------------------------------------------------

// Scenario: Secret never echoed — list and get replace every secret value
// with its hint; no plaintext or ciphertext is present.
func TestMCPServer_WorkspaceViewsNeverEchoSecrets(t *testing.T) {
	svc, _, wsID, _ := mcpFixture(t)
	ctx := context.Background()

	if err := svc.CreateWorkspaceServer(ctx, &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "GitHub", MCPConnection: stdioConn(), Enabled: true}); err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := svc.ListWorkspaceServers(ctx, wsID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, view := range list {
		for _, row := range append(append([]domain.EnvRow{}, view.Env...), view.Headers...) {
			if row.Value == "ghp_supersecret_1234" || isSecretEnvelope(row.Value) {
				t.Fatalf("view leaks a secret value: %q", row.Value)
			}
		}
		if view.Env[0].Value != "1234" {
			t.Fatalf("expected last-4 hint, got %q", view.Env[0].Value)
		}
	}

	views, _ := svc.ListWorkspaceServers(ctx, wsID)
	view, err := svc.WorkspaceServer(ctx, wsID, views[0].ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.Env[0].Value != "1234" {
		t.Fatalf("get must show only the hint, got %q", view.Env[0].Value)
	}
}

// Scenario: Empty value keeps the stored secret.
func TestMCPServer_WorkspaceEmptyValueKeepsSecret(t *testing.T) {
	svc, st, wsID, _ := mcpFixture(t)
	ctx := context.Background()

	server := &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "GitHub", MCPConnection: stdioConn(), Enabled: true}
	if err := svc.CreateWorkspaceServer(ctx, server); err != nil {
		t.Fatalf("create: %v", err)
	}
	before := storedWorkspaceServer(t, st, wsID, server.ID).Env[0].Value

	server.MCPConnection = stdioConn()
	server.MCPConnection.Env[0].Value = "" // client echoes the row without the secret
	if err := svc.UpdateWorkspaceServer(ctx, server); err != nil {
		t.Fatalf("update: %v", err)
	}

	after := storedWorkspaceServer(t, st, wsID, server.ID).Env[0].Value
	if after != before {
		t.Fatalf("empty value must keep the stored secret, got %q want %q", after, before)
	}
	if decryptAtRest(t, wsID, after) != "ghp_supersecret_1234" {
		t.Fatal("kept secret must still decrypt to the original value")
	}
}

// Scenario: New value replaces the stored secret — encrypted, stored, and a
// fresh hint is recorded.
func TestMCPServer_WorkspaceNewValueReplaces(t *testing.T) {
	svc, st, wsID, _ := mcpFixture(t)
	ctx := context.Background()

	server := &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "GitHub", MCPConnection: stdioConn(), Enabled: true}
	if err := svc.CreateWorkspaceServer(ctx, server); err != nil {
		t.Fatalf("create: %v", err)
	}
	before := storedWorkspaceServer(t, st, wsID, server.ID).Env[0].Value

	server.MCPConnection = stdioConn()
	server.MCPConnection.Env[0].Value = "ghp_rotated_7777"
	if err := svc.UpdateWorkspaceServer(ctx, server); err != nil {
		t.Fatalf("update: %v", err)
	}

	after := storedWorkspaceServer(t, st, wsID, server.ID).Env[0].Value
	if after == before {
		t.Fatal("a non-empty supply must replace the stored envelope")
	}
	if decryptAtRest(t, wsID, after) != "ghp_rotated_7777" {
		t.Fatal("replaced envelope must decrypt to the new value")
	}
	view, err := svc.WorkspaceServer(ctx, wsID, server.ID)
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if view.Env[0].Value != "7777" {
		t.Fatalf("fresh hint must reflect the new value, got %q", view.Env[0].Value)
	}
}

// Spec: values supplied as already-encrypted envelopes pass through untouched
// (workspace and agent scopes).
func TestMCPServer_SecretEnvelopePassesThrough(t *testing.T) {
	svc, st, wsID, agentID := mcpFixture(t)
	ctx := context.Background()

	envelope, err := secrets.Encrypt([]byte(testEncryptionKey), []byte(wsID), []byte("pre-encrypted-value"))
	if err != nil {
		t.Fatalf("pre-encrypt: %v", err)
	}

	wsServer := &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "Envelope", MCPConnection: stdioConn(), Enabled: true}
	wsServer.MCPConnection.Env[0].Value = envelope
	if err := svc.CreateWorkspaceServer(ctx, wsServer); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := storedWorkspaceServer(t, st, wsID, wsServer.ID).Env[0].Value; got != envelope {
		t.Fatalf("supplied envelope must pass through untouched, got %q", got)
	}

	agServer := &domain.AgentMCPServer{WorkspaceID: wsID, AgentID: agentID, Name: "Envelope", MCPConnection: httpConn(), Enabled: true}
	agServer.MCPConnection.Headers[0].Value = envelope
	if err := svc.CreateAgentServer(ctx, agServer); err != nil {
		t.Fatalf("agent create: %v", err)
	}
	if got := storedAgentServer(t, st, agentID, agServer.ID).Headers[0].Value; got != envelope {
		t.Fatalf("agent-scope envelope must pass through untouched, got %q", got)
	}
}

// -------------------------------------------------------------------------
// Tenancy — spec: workspace-mcp "MCP API and permission gating"
// -------------------------------------------------------------------------

// Scenario: Cross-tenant server is not found — a workspace-B caller addressing
// a workspace-A server id gets ErrNotFound indistinguishably from an unknown
// id, on every operation.
func TestMCPServer_WorkspaceCrossTenantNotFound(t *testing.T) {
	svc, st, wsID, _ := mcpFixture(t)
	otherID := seedMCPWorkspace(t, st, "other")
	ctx := context.Background()

	server := &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "GitHub", MCPConnection: stdioConn(), Enabled: true}
	if err := svc.CreateWorkspaceServer(ctx, server); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.WorkspaceServer(ctx, otherID, server.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant get must be not-found, got %v", err)
	}
	rows, err := svc.ListWorkspaceServers(ctx, otherID)
	if err != nil {
		t.Fatalf("list other workspace: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("cross-tenant list must be empty, got %+v", rows)
	}
	err = svc.UpdateWorkspaceServer(ctx, &domain.WorkspaceMCPServer{ID: server.ID, WorkspaceID: otherID, Name: "Stolen", MCPConnection: stdioConn(), Enabled: true})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant update must be not-found, got %v", err)
	}
	if err := svc.DeleteWorkspaceServer(ctx, otherID, server.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant delete must be not-found, got %v", err)
	}
	if err := svc.SetWorkspaceServerEnabled(ctx, otherID, server.ID, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant master switch must be not-found, got %v", err)
	}
	if err := svc.SetWorkspaceServerStatus(ctx, otherID, server.ID, domain.MCPStatusError, "boom", 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant status must be not-found, got %v", err)
	}
	if stored := storedWorkspaceServer(t, st, wsID, server.ID); stored.Name != "GitHub" {
		t.Fatalf("cross-tenant writes must not touch the row, got %q", stored.Name)
	}
}

// -------------------------------------------------------------------------
// Probe status — spec: workspace-mcp "Connection probe and status"
// -------------------------------------------------------------------------

// Status persists until the next probe; updates keep it; unknown statuses are
// rejected. (The probe execution itself is wired by the HTTP layer and the
// manager; the service owns persistence.)
func TestMCPServer_WorkspaceStatusPersists(t *testing.T) {
	svc, _, wsID, _ := mcpFixture(t)
	ctx := context.Background()

	server := &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "GitHub", MCPConnection: stdioConn(), Enabled: true}
	if err := svc.CreateWorkspaceServer(ctx, server); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Scenario: Successful probe — a reachable server exposing 24 tools.
	if err := svc.SetWorkspaceServerStatus(ctx, wsID, server.ID, domain.MCPStatusConnected, "", 24); err != nil {
		t.Fatalf("set connected: %v", err)
	}
	view, _ := svc.WorkspaceServer(ctx, wsID, server.ID)
	if view.Status != domain.MCPStatusConnected || view.ToolCount != 24 {
		t.Fatalf("connected status must persist, got %q/%d", view.Status, view.ToolCount)
	}

	// An editable update must not disturb the persisted status.
	server.MCPConnection = stdioConn()
	server.Command = "node"
	if err := svc.UpdateWorkspaceServer(ctx, server); err != nil {
		t.Fatalf("update: %v", err)
	}
	view, _ = svc.WorkspaceServer(ctx, wsID, server.ID)
	if view.Status != domain.MCPStatusConnected || view.ToolCount != 24 {
		t.Fatalf("status must survive updates, got %q/%d", view.Status, view.ToolCount)
	}

	// Scenario: Unreachable server — the row records status error with the
	// failure message.
	if err := svc.SetWorkspaceServerStatus(ctx, wsID, server.ID, domain.MCPStatusError, "dial tcp: connection refused", 0); err != nil {
		t.Fatalf("set error: %v", err)
	}
	view, _ = svc.WorkspaceServer(ctx, wsID, server.ID)
	if view.Status != domain.MCPStatusError || view.StatusError != "dial tcp: connection refused" {
		t.Fatalf("error status must persist with its message, got %+v", view)
	}

	// Scenario: Re-probe on demand behaves identically (same persistence path).
	if err := svc.SetWorkspaceServerStatus(ctx, wsID, server.ID, domain.MCPStatusConnected, "", 24); err != nil {
		t.Fatalf("re-probe: %v", err)
	}
	if err := svc.SetWorkspaceServerStatus(ctx, wsID, server.ID, "flapping", "", 1); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unknown status must be rejected, got %v", err)
	}
}

// -------------------------------------------------------------------------
// Agent-private servers — spec: workspace-mcp "Agent-private MCP servers"
// -------------------------------------------------------------------------

// Scenario: Private server usable only by its agent.
func TestMCPServer_AgentPrivateIsolation(t *testing.T) {
	svc, st, wsID, agentA := mcpFixture(t)
	agentB := seedMCPAgent(t, st, wsID, "beacon")
	ctx := context.Background()

	server := &domain.AgentMCPServer{WorkspaceID: wsID, AgentID: agentA, Name: "Scraper", MCPConnection: httpConn(), Enabled: true}
	if err := svc.CreateAgentServer(ctx, server); err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := svc.ListAgentServers(ctx, wsID, agentA)
	if err != nil || len(list) != 1 {
		t.Fatalf("agent A must see its private server, got %+v (%v)", list, err)
	}
	if bList, _ := svc.ListAgentServers(ctx, wsID, agentB); len(bList) != 0 {
		t.Fatalf("agent B must see none of agent A's private servers, got %+v", bList)
	}
	if _, err := svc.AgentServer(ctx, wsID, agentB, server.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("agent B addressing agent A's server must be not-found, got %v", err)
	}

	// The workspace registry never shows private servers.
	if wsList, _ := svc.ListWorkspaceServers(ctx, wsID); len(wsList) != 0 {
		t.Fatalf("private servers must not appear in the workspace registry, got %+v", wsList)
	}
}

// Scenario: Private servers die with the agent — no orphan rows remain.
func TestMCPServer_AgentDieWithAgent(t *testing.T) {
	svc, st, wsID, agentID := mcpFixture(t)
	ctx := context.Background()

	server := &domain.AgentMCPServer{WorkspaceID: wsID, AgentID: agentID, Name: "Scraper", MCPConnection: httpConn(), Enabled: true}
	if err := svc.CreateAgentServer(ctx, server); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := st.Agents().Delete(ctx, wsID, agentID); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	rows, err := st.AgentMCPServers().List(ctx, agentID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("private servers must die with the agent, got %+v (%v)", rows, err)
	}
}

// Scenario: Private server config matches workspace rules — encrypted,
// hinted on read, and kept on empty update exactly like a workspace server's.
func TestMCPServer_AgentSecretRulesMatchWorkspace(t *testing.T) {
	svc, st, wsID, agentID := mcpFixture(t)
	ctx := context.Background()

	server := &domain.AgentMCPServer{WorkspaceID: wsID, AgentID: agentID, Name: "Scraper", MCPConnection: httpConn(), Enabled: true}
	if err := svc.CreateAgentServer(ctx, server); err != nil {
		t.Fatalf("create: %v", err)
	}

	stored := storedAgentServer(t, st, agentID, server.ID)
	if !isSecretEnvelope(stored.Headers[0].Value) {
		t.Fatalf("private header secret must be encrypted at rest, got %q", stored.Headers[0].Value)
	}
	// design.md D4: agent-private rows encrypt with the workspace ID as AAD.
	if decryptAtRest(t, wsID, stored.Headers[0].Value) != "Bearer sk-live-9999" {
		t.Fatal("private rows must decrypt under the workspace AAD")
	}

	view, err := svc.AgentServer(ctx, wsID, agentID, server.ID)
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if view.Headers[0].Value != "9999" {
		t.Fatalf("private rows must hint on read, got %q", view.Headers[0].Value)
	}

	// Empty value keeps the stored secret, same as the workspace scope.
	before := stored.Headers[0].Value
	server.MCPConnection = httpConn()
	server.MCPConnection.Headers[0].Value = ""
	if err := svc.UpdateAgentServer(ctx, server); err != nil {
		t.Fatalf("empty-value update: %v", err)
	}
	if after := storedAgentServer(t, st, agentID, server.ID).Headers[0].Value; after != before {
		t.Fatalf("empty value must keep the stored secret, got %q want %q", after, before)
	}
}

// Ownership: an agent-private write against an agent of another workspace is
// rejected before the MCP store is touched.
func TestMCPServer_AgentCrossWorkspaceRejected(t *testing.T) {
	svc, st, wsID, _ := mcpFixture(t)
	otherWS := seedMCPWorkspace(t, st, "other")
	foreignAgent := seedMCPAgent(t, st, otherWS, "drift")
	ctx := context.Background()

	server := &domain.AgentMCPServer{WorkspaceID: wsID, AgentID: foreignAgent, Name: "Scraper", MCPConnection: httpConn(), Enabled: true}
	err := svc.CreateAgentServer(ctx, server)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-workspace create must be not-found, got %v", err)
	}
	rows, _ := st.AgentMCPServers().List(ctx, foreignAgent)
	if len(rows) != 0 {
		t.Fatalf("rejected write must not touch the MCP store, got %+v", rows)
	}

	// Seed a legit private server, then attack it across the workspace boundary.
	ownAgent := seedMCPAgent(t, st, wsID, "atlas2")
	own := &domain.AgentMCPServer{WorkspaceID: wsID, AgentID: ownAgent, Name: "Scraper", MCPConnection: httpConn(), Enabled: true}
	if err := svc.CreateAgentServer(ctx, own); err != nil {
		t.Fatalf("seed own: %v", err)
	}

	mutated := &domain.AgentMCPServer{ID: own.ID, WorkspaceID: otherWS, AgentID: foreignAgent, Name: "Stolen", MCPConnection: httpConn(), Enabled: true}
	if err := svc.UpdateAgentServer(ctx, mutated); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-workspace update must be not-found, got %v", err)
	}
	if err := svc.DeleteAgentServer(ctx, otherWS, foreignAgent, own.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-workspace delete must be not-found, got %v", err)
	}
	if err := svc.SetAgentServerEnabled(ctx, otherWS, foreignAgent, own.ID, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-workspace master switch must be not-found, got %v", err)
	}
	if err := svc.SetAgentServerStatus(ctx, otherWS, foreignAgent, own.ID, domain.MCPStatusError, "boom", 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-workspace status must be not-found, got %v", err)
	}
	if stored := storedAgentServer(t, st, own.AgentID, own.ID); stored.Name != "Scraper" {
		t.Fatalf("cross-workspace writes must not touch the row, got %q", stored.Name)
	}

	// An unknown agent id is the same not-found.
	ghost := &domain.AgentMCPServer{WorkspaceID: wsID, AgentID: "00000000-0000-0000-0000-000000000000", Name: "Ghost", MCPConnection: httpConn(), Enabled: true}
	if err := svc.CreateAgentServer(ctx, ghost); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown agent create must be not-found, got %v", err)
	}
}

// Names are unique per agent (case-insensitive); different agents may reuse
// a name.
func TestMCPServer_AgentNameUniquePerAgent(t *testing.T) {
	svc, st, wsID, agentA := mcpFixture(t)
	agentB := seedMCPAgent(t, st, wsID, "beacon")
	ctx := context.Background()

	first := &domain.AgentMCPServer{WorkspaceID: wsID, AgentID: agentA, Name: "Scraper", MCPConnection: httpConn(), Enabled: true}
	if err := svc.CreateAgentServer(ctx, first); err != nil {
		t.Fatalf("create first: %v", err)
	}

	err := svc.CreateAgentServer(ctx, &domain.AgentMCPServer{WorkspaceID: wsID, AgentID: agentA, Name: "SCRAPER", MCPConnection: httpConn(), Enabled: true})
	assertMCPFieldError(t, err, "name", `"Scraper"`)

	// A rename onto the agent's own other server collides; keeping its own
	// name does not.
	second := &domain.AgentMCPServer{WorkspaceID: wsID, AgentID: agentA, Name: "Crawler", MCPConnection: httpConn(), Enabled: true}
	if err := svc.CreateAgentServer(ctx, second); err != nil {
		t.Fatalf("create second: %v", err)
	}
	second.Name = "scraper"
	if err := svc.UpdateAgentServer(ctx, second); err == nil {
		t.Fatal("rename onto a sibling's name must collide")
	} else {
		assertMCPFieldError(t, err, "name", "Scraper")
	}
	second.Name = "CRAWLER"
	if err := svc.UpdateAgentServer(ctx, second); err != nil {
		t.Fatalf("re-case of own name must save: %v", err)
	}

	// Agent B reusing the name is fine — uniqueness is per agent.
	if err := svc.CreateAgentServer(ctx, &domain.AgentMCPServer{WorkspaceID: wsID, AgentID: agentB, Name: "scraper", MCPConnection: httpConn(), Enabled: true}); err != nil {
		t.Fatalf("same name on another agent must save: %v", err)
	}
}

// Runtime accessors decrypt for connection attempts; API views never do.
func TestMCPServer_RuntimeViewsDecryptSecrets(t *testing.T) {
	svc, _, wsID, agentID := mcpFixture(t)
	ctx := context.Background()

	if err := svc.CreateWorkspaceServer(ctx, &domain.WorkspaceMCPServer{WorkspaceID: wsID, Name: "GitHub", MCPConnection: stdioConn(), Enabled: true}); err != nil {
		t.Fatalf("create ws: %v", err)
	}
	if err := svc.CreateAgentServer(ctx, &domain.AgentMCPServer{WorkspaceID: wsID, AgentID: agentID, Name: "Scraper", MCPConnection: httpConn(), Enabled: true}); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	wsRuntime, err := svc.WorkspaceServersForRuntime(ctx, wsID)
	if err != nil {
		t.Fatalf("ws runtime: %v", err)
	}
	if wsRuntime[0].Env[0].Value != "ghp_supersecret_1234" {
		t.Fatalf("runtime workspace rows must decrypt, got %q", wsRuntime[0].Env[0].Value)
	}
	one, err := svc.WorkspaceServerForRuntime(ctx, wsID, wsRuntime[0].ID)
	if err != nil || one.Env[0].Value != "ghp_supersecret_1234" {
		t.Fatalf("runtime get must decrypt, got %v/%q", err, one.Env[0].Value)
	}

	agRuntime, err := svc.AgentServersForRuntime(ctx, wsID, agentID)
	if err != nil {
		t.Fatalf("agent runtime: %v", err)
	}
	if agRuntime[0].Headers[0].Value != "Bearer sk-live-9999" {
		t.Fatalf("runtime agent rows must decrypt, got %q", agRuntime[0].Headers[0].Value)
	}
	oneAg, err := svc.AgentServerForRuntime(ctx, wsID, agentID, agRuntime[0].ID)
	if err != nil || oneAg.Headers[0].Value != "Bearer sk-live-9999" {
		t.Fatalf("runtime agent get must decrypt, got %v/%q", err, oneAg.Headers[0].Value)
	}
}
