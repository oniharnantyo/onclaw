//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// mcpSeedWorkspace creates a plain workspace for MCP store tests.
func mcpSeedWorkspace(t *testing.T, ctx context.Context, s store.Store, slug string) *domain.Workspace {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "MCP WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}
	return ws
}

// mcpSeedAgent creates a workspace-scoped agent for agent-private MCP tests.
func mcpSeedAgent(t *testing.T, ctx context.Context, s store.Store, slug string) (*domain.Workspace, *domain.Agent) {
	t.Helper()
	ws := mcpSeedWorkspace(t, ctx, s, slug)
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI " + slug, Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("unexpected create provider error: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: "mcp-agent", Name: "MCP Agent", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("unexpected create agent error: %v", err)
	}
	return ws, a
}

func TestIntegration_WorkspaceMCPServerStore_CRUD(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws1 := mcpSeedWorkspace(t, ctx, s, "ws-mcp-1")
	ws2 := mcpSeedWorkspace(t, ctx, s, "ws-mcp-2")

	// 1. Create a stdio server with full connection config (multiple env rows).
	srv := &domain.WorkspaceMCPServer{
		WorkspaceID: ws1.ID,
		Name:        "GitHub",
		MCPConnection: domain.MCPConnection{
			Transport: domain.MCPTransportStdio,
			Command:   "npx",
			Args:      []string{"-y", "@modelcontextprotocol/server-github"},
			Env: []domain.EnvRow{
				{Name: "GITHUB_TOKEN", Value: "enc:ghp_secret_envelope"},
				{Name: "LOG_LEVEL", Value: "debug"},
			},
		},
		Enabled: true,
	}
	if err := s.WorkspaceMCPServers().Create(ctx, srv); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if srv.ID == "" {
		t.Fatal("expected server ID to be assigned")
	}
	if srv.CreatedAt.IsZero() || srv.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be set")
	}

	// 2. Get roundtrips the full row including jsonb env rows and args order.
	got, err := s.WorkspaceMCPServers().Get(ctx, ws1.ID, srv.ID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if got.Name != "GitHub" || got.Command != "npx" || got.Transport != domain.MCPTransportStdio {
		t.Fatalf("unexpected server: %+v", got)
	}
	if len(got.Args) != 2 || got.Args[0] != "-y" || got.Args[1] != "@modelcontextprotocol/server-github" {
		t.Fatalf("unexpected args: %+v", got.Args)
	}
	if len(got.Env) != 2 ||
		got.Env[0].Name != "GITHUB_TOKEN" || got.Env[0].Value != "enc:ghp_secret_envelope" ||
		got.Env[1].Name != "LOG_LEVEL" || got.Env[1].Value != "debug" {
		t.Fatalf("unexpected env rows: %+v", got.Env)
	}
	if got.URL != "" || len(got.Headers) != 0 {
		t.Fatalf("expected url transports fields empty, got: %+v", got)
	}

	// 3. Cross-tenant and unknown lookups are indistinguishable (ErrNotFound).
	if _, err := s.WorkspaceMCPServers().Get(ctx, ws2.ID, srv.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant get, got %v", err)
	}
	if _, err := s.WorkspaceMCPServers().Get(ctx, ws1.ID, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown id, got %v", err)
	}
	if _, err := s.WorkspaceMCPServers().Get(ctx, ws1.ID, "invalid-uuid"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for invalid uuid, got %v", err)
	}
	if _, err := s.WorkspaceMCPServers().Get(ctx, "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty scope, got %v", err)
	}

	// 4. Empty env/headers persist as empty (not null).
	plain := &domain.WorkspaceMCPServer{
		WorkspaceID:   ws1.ID,
		Name:          "Bare",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "uvx"},
	}
	if err := s.WorkspaceMCPServers().Create(ctx, plain); err != nil {
		t.Fatalf("unexpected create bare error: %v", err)
	}
	gotBare, err := s.WorkspaceMCPServers().Get(ctx, ws1.ID, plain.ID)
	if err != nil {
		t.Fatalf("unexpected get bare error: %v", err)
	}
	if gotBare.Env == nil || len(gotBare.Env) != 0 || gotBare.Headers == nil || len(gotBare.Headers) != 0 {
		t.Fatalf("expected empty non-nil env/headers, got: %+v", gotBare)
	}

	// 5. List is workspace-scoped and ordered by created_at.
	list1, err := s.WorkspaceMCPServers().List(ctx, ws1.ID)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(list1) != 2 || list1[0].ID != srv.ID || list1[1].ID != plain.ID {
		t.Fatalf("expected 2 servers in ws1 ordered [GitHub, Bare], got %+v", list1)
	}
	list2, err := s.WorkspaceMCPServers().List(ctx, ws2.ID)
	if err != nil {
		t.Fatalf("unexpected list ws2 error: %v", err)
	}
	if len(list2) != 0 {
		t.Fatalf("expected 0 servers in ws2, got %d", len(list2))
	}
	emptyList, err := s.WorkspaceMCPServers().List(ctx, "")
	if err != nil {
		t.Fatalf("unexpected list empty-scope error: %v", err)
	}
	if len(emptyList) != 0 {
		t.Fatalf("expected empty list for empty scope, got %d", len(emptyList))
	}

	// 6. SetStatus is the sole status writer.
	if err := s.WorkspaceMCPServers().SetStatus(ctx, ws1.ID, srv.ID, domain.MCPStatusConnected, "", 24); err != nil {
		t.Fatalf("unexpected set-status error: %v", err)
	}
	if err := s.WorkspaceMCPServers().SetStatus(ctx, ws2.ID, srv.ID, domain.MCPStatusError, "nope", 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant set-status, got %v", err)
	}
	if err := s.WorkspaceMCPServers().SetStatus(ctx, ws1.ID, uuid.NewString(), domain.MCPStatusError, "nope", 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown set-status, got %v", err)
	}

	// 7. Validation errors on Create.
	if err := s.WorkspaceMCPServers().Create(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil server, got %v", err)
	}
	if err := s.WorkspaceMCPServers().Create(ctx, &domain.WorkspaceMCPServer{
		WorkspaceID:   ws1.ID,
		Name:          "NoCmd",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio},
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for stdio without command, got %v", err)
	}
	if err := s.WorkspaceMCPServers().Create(ctx, &domain.WorkspaceMCPServer{
		WorkspaceID:   ws1.ID,
		Name:          "BadTransport",
		MCPConnection: domain.MCPConnection{Transport: "carrier-pigeon"},
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for unknown transport, got %v", err)
	}
	// Unknown workspace surfaces as ErrNotFound (FK violation).
	if err := s.WorkspaceMCPServers().Create(ctx, &domain.WorkspaceMCPServer{
		WorkspaceID:   uuid.NewString(),
		Name:          "Orphan",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}
	// Duplicate explicit ID conflicts.
	if err := s.WorkspaceMCPServers().Create(ctx, &domain.WorkspaceMCPServer{
		ID:            srv.ID,
		WorkspaceID:   ws1.ID,
		Name:          "Other",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate id, got %v", err)
	}

	// 8. Update replaces editable fields only: status/status_error/tool_count
	// and created_at persist through it.
	got.Enabled = false
	got.Name = "GitHub Enterprise"
	got.MCPConnection = domain.MCPConnection{
		Transport: domain.MCPTransportStreamableHTTP,
		URL:       "https://mcp.example.com/stream",
		Headers: []domain.EnvRow{
			{Name: "Authorization", Value: "enc:bearer_envelope"},
			{Name: "X-Tenant", Value: "acme"},
		},
	}
	origCreated := got.CreatedAt
	origUpdated := got.UpdatedAt
	if err := s.WorkspaceMCPServers().Update(ctx, got); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	reloaded, err := s.WorkspaceMCPServers().Get(ctx, ws1.ID, srv.ID)
	if err != nil {
		t.Fatalf("unexpected get after update: %v", err)
	}
	if reloaded.Enabled || reloaded.Name != "GitHub Enterprise" {
		t.Fatalf("unexpected editable fields after update: %+v", reloaded)
	}
	if reloaded.Transport != domain.MCPTransportStreamableHTTP || reloaded.URL != "https://mcp.example.com/stream" {
		t.Fatalf("unexpected connection after update: %+v", reloaded.MCPConnection)
	}
	if len(reloaded.Headers) != 2 || reloaded.Headers[0].Name != "Authorization" || reloaded.Headers[0].Value != "enc:bearer_envelope" || reloaded.Headers[1].Name != "X-Tenant" {
		t.Fatalf("unexpected header rows after update: %+v", reloaded.Headers)
	}
	if reloaded.Command != "" || len(reloaded.Args) != 0 || len(reloaded.Env) != 0 {
		t.Fatalf("expected other-transport fields cleared, got: %+v", reloaded.MCPConnection)
	}
	if reloaded.Status != domain.MCPStatusConnected || reloaded.StatusError != "" || reloaded.ToolCount != 24 {
		t.Fatalf("expected status fields preserved through update, got: %+v", reloaded)
	}
	if !reloaded.CreatedAt.Equal(origCreated) {
		t.Fatalf("expected created_at preserved: %v vs %v", origCreated, reloaded.CreatedAt)
	}
	if !reloaded.UpdatedAt.After(origUpdated) {
		t.Fatalf("expected updated_at to advance: %v vs %v", origUpdated, reloaded.UpdatedAt)
	}

	// Update on unknown id / cross-tenant is ErrNotFound.
	if err := s.WorkspaceMCPServers().Update(ctx, &domain.WorkspaceMCPServer{
		ID:            uuid.NewString(),
		WorkspaceID:   ws1.ID,
		Name:          "Ghost",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown update, got %v", err)
	}
	cross := *got
	cross.WorkspaceID = ws2.ID
	if err := s.WorkspaceMCPServers().Update(ctx, &cross); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant update, got %v", err)
	}

	// 9. Delete is scoped; unknown and cross-tenant deletes are ErrNotFound.
	if err := s.WorkspaceMCPServers().Delete(ctx, ws2.ID, srv.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant delete, got %v", err)
	}
	if err := s.WorkspaceMCPServers().Delete(ctx, ws1.ID, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown delete, got %v", err)
	}
	if err := s.WorkspaceMCPServers().Delete(ctx, ws1.ID, srv.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if _, err := s.WorkspaceMCPServers().Get(ctx, ws1.ID, srv.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

// The schema's UNIQUE (workspace_id, name) is exact-only (migration 000024);
// case-insensitive uniqueness is enforced at the store level with a lower(name)
// pre-check, matching the fake. Exact duplicates additionally hit the DB
// constraint and map to the same sentinel as a backstop.
func TestIntegration_WorkspaceMCPServerStore_NameUniqueness(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws1 := mcpSeedWorkspace(t, ctx, s, "ws-mcp-uniq")
	ws2 := mcpSeedWorkspace(t, ctx, s, "ws-mcp-uniq-2")

	first := &domain.WorkspaceMCPServer{
		WorkspaceID:   ws1.ID,
		Name:          "GitHub",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}
	if err := s.WorkspaceMCPServers().Create(ctx, first); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	// Exact duplicate rejected.
	if err := s.WorkspaceMCPServers().Create(ctx, &domain.WorkspaceMCPServer{
		WorkspaceID:   ws1.ID,
		Name:          "GitHub",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}); !errors.Is(err, domain.ErrMCPServerNameTaken) {
		t.Fatalf("expected ErrMCPServerNameTaken for exact duplicate, got %v", err)
	}

	// Case-insensitive duplicate rejected.
	if err := s.WorkspaceMCPServers().Create(ctx, &domain.WorkspaceMCPServer{
		WorkspaceID:   ws1.ID,
		Name:          "github",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}); !errors.Is(err, domain.ErrMCPServerNameTaken) {
		t.Fatalf("expected ErrMCPServerNameTaken for case-insensitive duplicate, got %v", err)
	}

	// Same name in another workspace is fine.
	other := &domain.WorkspaceMCPServer{
		WorkspaceID:   ws2.ID,
		Name:          "GITHUB",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}
	if err := s.WorkspaceMCPServers().Create(ctx, other); err != nil {
		t.Fatalf("expected same name in different workspace to succeed, got %v", err)
	}

	// Rename onto another server's name (case variant) rejected.
	second := &domain.WorkspaceMCPServer{
		WorkspaceID:   ws1.ID,
		Name:          "Linear",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}
	if err := s.WorkspaceMCPServers().Create(ctx, second); err != nil {
		t.Fatalf("unexpected create second error: %v", err)
	}
	second.Name = "GiThUb"
	if err := s.WorkspaceMCPServers().Update(ctx, second); !errors.Is(err, domain.ErrMCPServerNameTaken) {
		t.Fatalf("expected ErrMCPServerNameTaken for rename conflict, got %v", err)
	}

	// Keeping your own name, even case-swapped, is fine.
	first.Name = "GITHUB"
	if err := s.WorkspaceMCPServers().Update(ctx, first); err != nil {
		t.Fatalf("expected case-swap rename of own name to succeed, got %v", err)
	}
	got, err := s.WorkspaceMCPServers().Get(ctx, ws1.ID, first.ID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if got.Name != "GITHUB" {
		t.Fatalf("expected renamed server, got %q", got.Name)
	}
}

func TestIntegration_AgentMCPServerStore_CRUD(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws1, agent1 := mcpSeedAgent(t, ctx, s, "ws-amcp-1")
	_, agent2 := mcpSeedAgent(t, ctx, s, "ws-amcp-2")

	// 1. Create an agent-private streamable HTTP server with header rows.
	srv := &domain.AgentMCPServer{
		WorkspaceID: ws1.ID,
		AgentID:     agent1.ID,
		Name:        "Private Search",
		MCPConnection: domain.MCPConnection{
			Transport: domain.MCPTransportStreamableHTTP,
			URL:       "https://search.internal/mcp",
			Headers: []domain.EnvRow{
				{Name: "Authorization", Value: "enc:bearer_envelope"},
				{Name: "X-Trace", Value: "on"},
			},
		},
	}
	if err := s.AgentMCPServers().Create(ctx, srv); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if srv.ID == "" {
		t.Fatal("expected server ID to be assigned")
	}
	if srv.CreatedAt.IsZero() || srv.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be set")
	}

	got, err := s.AgentMCPServers().Get(ctx, agent1.ID, srv.ID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if got.WorkspaceID != ws1.ID || got.AgentID != agent1.ID || got.Name != "Private Search" {
		t.Fatalf("unexpected server: %+v", got)
	}
	if got.URL != "https://search.internal/mcp" {
		t.Fatalf("unexpected url: %q", got.URL)
	}
	if len(got.Headers) != 2 || got.Headers[0].Name != "Authorization" || got.Headers[0].Value != "enc:bearer_envelope" || got.Headers[1].Name != "X-Trace" || got.Headers[1].Value != "on" {
		t.Fatalf("unexpected header rows: %+v", got.Headers)
	}

	// 2. Cross-agent and unknown lookups are ErrNotFound.
	if _, err := s.AgentMCPServers().Get(ctx, agent2.ID, srv.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-agent get, got %v", err)
	}
	if _, err := s.AgentMCPServers().Get(ctx, agent1.ID, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown id, got %v", err)
	}
	if _, err := s.AgentMCPServers().Get(ctx, "", srv.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty scope, got %v", err)
	}

	// 3. List is agent-scoped.
	list1, err := s.AgentMCPServers().List(ctx, agent1.ID)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(list1) != 1 || list1[0].ID != srv.ID {
		t.Fatalf("expected 1 server for agent1, got %+v", list1)
	}
	list2, err := s.AgentMCPServers().List(ctx, agent2.ID)
	if err != nil {
		t.Fatalf("unexpected list agent2 error: %v", err)
	}
	if len(list2) != 0 {
		t.Fatalf("expected 0 servers for agent2, got %d", len(list2))
	}

	// 4. Unknown agent and cross-workspace agent are ErrNotFound (fake parity).
	if err := s.AgentMCPServers().Create(ctx, &domain.AgentMCPServer{
		WorkspaceID:   ws1.ID,
		AgentID:       uuid.NewString(),
		Name:          "Ghost",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown agent, got %v", err)
	}
	if err := s.AgentMCPServers().Create(ctx, &domain.AgentMCPServer{
		WorkspaceID:   ws1.ID,
		AgentID:       agent2.ID, // agent2 belongs to ws2!
		Name:          "Cross Tenant",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-workspace agent, got %v", err)
	}
	if err := s.AgentMCPServers().Create(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil server, got %v", err)
	}

	// 5. SetStatus is the sole status writer; cross-agent is ErrNotFound.
	if err := s.AgentMCPServers().SetStatus(ctx, agent1.ID, srv.ID, domain.MCPStatusError, "dial tcp: connection refused", 0); err != nil {
		t.Fatalf("unexpected set-status error: %v", err)
	}
	if err := s.AgentMCPServers().SetStatus(ctx, agent2.ID, srv.ID, domain.MCPStatusConnected, "", 3); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-agent set-status, got %v", err)
	}

	// 6. Update replaces editable fields; status fields and created_at persist.
	got.Enabled = false
	got.Name = "Private Search v2"
	got.MCPConnection = domain.MCPConnection{
		Transport: domain.MCPTransportSSE,
		URL:       "https://search.internal/sse",
	}
	origCreated := got.CreatedAt
	origUpdated := got.UpdatedAt
	if err := s.AgentMCPServers().Update(ctx, got); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	reloaded, err := s.AgentMCPServers().Get(ctx, agent1.ID, srv.ID)
	if err != nil {
		t.Fatalf("unexpected get after update: %v", err)
	}
	if reloaded.Enabled || reloaded.Name != "Private Search v2" {
		t.Fatalf("unexpected editable fields after update: %+v", reloaded)
	}
	if reloaded.Transport != domain.MCPTransportSSE || reloaded.URL != "https://search.internal/sse" {
		t.Fatalf("unexpected connection after update: %+v", reloaded.MCPConnection)
	}
	if reloaded.Status != domain.MCPStatusError || reloaded.StatusError != "dial tcp: connection refused" || reloaded.ToolCount != 0 {
		t.Fatalf("expected status fields preserved through update, got: %+v", reloaded)
	}
	if !reloaded.CreatedAt.Equal(origCreated) {
		t.Fatalf("expected created_at preserved: %v vs %v", origCreated, reloaded.CreatedAt)
	}
	if !reloaded.UpdatedAt.After(origUpdated) {
		t.Fatalf("expected updated_at to advance: %v vs %v", origUpdated, reloaded.UpdatedAt)
	}

	// Cross-agent and unknown-id updates are ErrNotFound.
	if err := s.AgentMCPServers().Update(ctx, &domain.AgentMCPServer{
		WorkspaceID:   ws1.ID,
		AgentID:       agent2.ID,
		ID:            srv.ID,
		Name:          "Hijack",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-agent update, got %v", err)
	}
	if err := s.AgentMCPServers().Update(ctx, &domain.AgentMCPServer{
		WorkspaceID:   ws1.ID,
		AgentID:       agent1.ID,
		ID:            uuid.NewString(),
		Name:          "Ghost",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown update, got %v", err)
	}

	// 7. Delete is agent-scoped.
	if err := s.AgentMCPServers().Delete(ctx, agent2.ID, srv.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-agent delete, got %v", err)
	}
	if err := s.AgentMCPServers().Delete(ctx, agent1.ID, srv.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if _, err := s.AgentMCPServers().Get(ctx, agent1.ID, srv.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestIntegration_AgentMCPServerStore_CascadeAndUniqueness(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws, agent := mcpSeedAgent(t, ctx, s, "ws-amcp-cascade")

	first := &domain.AgentMCPServer{
		WorkspaceID:   ws.ID,
		AgentID:       agent.ID,
		Name:          "Notes",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}
	if err := s.AgentMCPServers().Create(ctx, first); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	// Exact and case-insensitive duplicates within the agent scope rejected.
	if err := s.AgentMCPServers().Create(ctx, &domain.AgentMCPServer{
		WorkspaceID:   ws.ID,
		AgentID:       agent.ID,
		Name:          "notes",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}); !errors.Is(err, domain.ErrMCPServerNameTaken) {
		t.Fatalf("expected ErrMCPServerNameTaken for case-insensitive duplicate, got %v", err)
	}

	// Cascade: deleting the agent removes its private servers.
	if err := s.Agents().Delete(ctx, ws.ID, agent.ID); err != nil {
		t.Fatalf("unexpected delete agent error: %v", err)
	}
	if _, err := s.AgentMCPServers().Get(ctx, agent.ID, first.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after agent delete cascade, got %v", err)
	}
	remaining, err := s.AgentMCPServers().List(ctx, agent.ID)
	if err != nil {
		t.Fatalf("unexpected list after cascade: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected 0 servers after cascade, got %d", len(remaining))
	}
}
