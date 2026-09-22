//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/postgres"
)

// pgConnSeedWorkspace creates a plain workspace for connection store tests.
func pgConnSeedWorkspace(t *testing.T, ctx context.Context, s store.Store, slug string) *domain.Workspace {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "Conn WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}
	return ws
}

func TestIntegration_ConnectionStore_CRUD(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws1 := pgConnSeedWorkspace(t, ctx, s, "pg-conn-1")
	ws2 := pgConnSeedWorkspace(t, ctx, s, "pg-conn-2")

	// 1. Create assigns ID and timestamps.
	c := &domain.Connection{
		WorkspaceID: ws1.ID,
		Service:     "github",
		AccessLevel: domain.ConnectionAccessReadOnly,
	}
	if err := s.Connections().Create(ctx, c); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if c.ID == "" {
		t.Fatal("expected connection ID to be assigned")
	}
	if c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be set")
	}

	// 2. Get roundtrips the row.
	got, err := s.Connections().Get(ctx, ws1.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if got.Service != "github" || got.AccessLevel != domain.ConnectionAccessReadOnly || got.WorkspaceID != ws1.ID {
		t.Fatalf("unexpected connection: %+v", got)
	}

	// 3. Cross-tenant, unknown, and invalid lookups are ErrNotFound.
	if _, err := s.Connections().Get(ctx, ws2.ID, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant get, got %v", err)
	}
	if _, err := s.Connections().Get(ctx, ws1.ID, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown id, got %v", err)
	}
	if _, err := s.Connections().Get(ctx, ws1.ID, "invalid-uuid"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for invalid uuid, got %v", err)
	}
	if _, err := s.Connections().Get(ctx, "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty scope, got %v", err)
	}

	// 4. GetByService resolves within the workspace only.
	byService, err := s.Connections().GetByService(ctx, ws1.ID, "github")
	if err != nil {
		t.Fatalf("unexpected get-by-service error: %v", err)
	}
	if byService.ID != c.ID {
		t.Fatalf("expected the github connection, got %+v", byService)
	}
	if _, err := s.Connections().GetByService(ctx, ws2.ID, "github"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant get-by-service, got %v", err)
	}

	// 5. List is workspace-scoped and ordered by created_at.
	second := &domain.Connection{
		WorkspaceID: ws1.ID,
		Service:     "gitlab",
		AccessLevel: domain.ConnectionAccessReadWrite,
	}
	if err := s.Connections().Create(ctx, second); err != nil {
		t.Fatalf("unexpected create second error: %v", err)
	}
	list1, err := s.Connections().List(ctx, ws1.ID)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(list1) != 2 || list1[0].ID != c.ID || list1[1].ID != second.ID {
		t.Fatalf("expected [github, gitlab] in ws1, got %+v", list1)
	}
	list2, err := s.Connections().List(ctx, ws2.ID)
	if err != nil {
		t.Fatalf("unexpected list ws2 error: %v", err)
	}
	if len(list2) != 0 {
		t.Fatalf("expected 0 connections in ws2, got %d", len(list2))
	}

	// 6. Validation errors on Create.
	if err := s.Connections().Create(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil connection, got %v", err)
	}
	if err := s.Connections().Create(ctx, &domain.Connection{
		WorkspaceID: ws1.ID,
		Service:     "figma",
		AccessLevel: "sudo",
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for unknown access level, got %v", err)
	}

	// 7. Unknown workspace surfaces as ErrNotFound (FK violation).
	if err := s.Connections().Create(ctx, &domain.Connection{
		WorkspaceID: uuid.NewString(),
		Service:     "github",
		AccessLevel: domain.ConnectionAccessReadOnly,
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}

	// 8. Duplicate explicit ID conflicts.
	if err := s.Connections().Create(ctx, &domain.Connection{
		ID:          c.ID,
		WorkspaceID: ws1.ID,
		Service:     "linear",
		AccessLevel: domain.ConnectionAccessReadOnly,
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate id, got %v", err)
	}

	// 9. Delete is scoped; unknown and cross-tenant deletes are ErrNotFound.
	if err := s.Connections().Delete(ctx, ws2.ID, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant delete, got %v", err)
	}
	if err := s.Connections().Delete(ctx, ws1.ID, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown delete, got %v", err)
	}
	if err := s.Connections().Delete(ctx, ws1.ID, second.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if _, err := s.Connections().Get(ctx, ws1.ID, second.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

// One connection per service per workspace (design.md D6): the per-workspace
// service uniqueness is a store-level pre-check (fake parity) with the schema
// constraint uq_workspace_connections_workspace_id_service as the backstop.
func TestIntegration_ConnectionStore_ServiceUniqueness(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws1 := pgConnSeedWorkspace(t, ctx, s, "pg-conn-uniq-1")
	ws2 := pgConnSeedWorkspace(t, ctx, s, "pg-conn-uniq-2")

	first := &domain.Connection{
		WorkspaceID: ws1.ID,
		Service:     "github",
		AccessLevel: domain.ConnectionAccessReadOnly,
	}
	if err := s.Connections().Create(ctx, first); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	dup := &domain.Connection{
		WorkspaceID: ws1.ID,
		Service:     "github",
		AccessLevel: domain.ConnectionAccessReadWrite,
	}
	err := s.Connections().Create(ctx, dup)
	if !errors.Is(err, domain.ErrConnectionExists) || !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConnectionExists chaining ErrConflict for duplicate service, got %v", err)
	}

	// The same service in another workspace is fine.
	if err := s.Connections().Create(ctx, &domain.Connection{
		WorkspaceID: ws2.ID,
		Service:     "github",
		AccessLevel: domain.ConnectionAccessReadOnly,
	}); err != nil {
		t.Fatalf("expected same service in different workspace to succeed, got %v", err)
	}
}

// Disconnect cascade (design.md D8, tasks.md 1.5): the connection row, the
// origin-linked workspace MCP server row, and every agent's attachment
// reference to it die in ONE transaction.
func TestIntegration_ConnectionStore_DeleteCascade(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := pgConnSeedWorkspace(t, ctx, s, "pg-conn-cascade")
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI", Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("unexpected create provider error: %v", err)
	}
	agentA := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: p.ID, Model: "gpt-4o"}
	agentB := &domain.Agent{WorkspaceID: ws.ID, Slug: "beacon", Name: "Beacon", ProviderID: p.ID, Model: "gpt-4o"}
	for _, a := range []*domain.Agent{agentA, agentB} {
		if err := s.Agents().Create(ctx, a); err != nil {
			t.Fatalf("unexpected create agent error: %v", err)
		}
	}

	c := &domain.Connection{
		WorkspaceID: ws.ID,
		Service:     "github",
		AccessLevel: domain.ConnectionAccessReadOnly,
	}
	if err := s.Connections().Create(ctx, c); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	// Materialize the linked server the way the connections service does.
	srv := &domain.WorkspaceMCPServer{
		WorkspaceID: ws.ID,
		Name:        "GitHub (Managed)",
		MCPConnection: domain.MCPConnection{
			Transport: domain.MCPTransportStreamableHTTP,
			URL:       "https://api.githubcopilot.com/mcp/",
			Headers:   []domain.EnvRow{{Name: "Authorization", Value: "enc:secret-envelope"}},
		},
		OriginConnectionID: c.ID,
	}
	if err := s.WorkspaceMCPServers().Create(ctx, srv); err != nil {
		t.Fatalf("unexpected materialize error: %v", err)
	}

	// An unrelated server that must survive the cascade.
	other := &domain.WorkspaceMCPServer{
		WorkspaceID:   ws.ID,
		Name:          "Hand Made",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}
	if err := s.WorkspaceMCPServers().Create(ctx, other); err != nil {
		t.Fatalf("unexpected create other error: %v", err)
	}

	// Two agents attach the materialized server; agent A keeps another
	// attachment.
	attach := func(a *domain.Agent, serverIDs ...string) {
		t.Helper()
		got, err := s.Agents().ByID(ctx, ws.ID, a.ID)
		if err != nil {
			t.Fatalf("unexpected agent get error: %v", err)
		}
		got.EnabledMCPS = append(got.EnabledMCPS, serverIDs...)
		if err := s.Agents().Update(ctx, got); err != nil {
			t.Fatalf("unexpected agent update error: %v", err)
		}
	}
	attach(agentA, srv.ID, other.ID)
	attach(agentB, srv.ID)

	// GetByOriginConnection resolves the materialized server.
	found, err := s.WorkspaceMCPServers().GetByOriginConnection(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected origin lookup error: %v", err)
	}
	if found == nil || found.ID != srv.ID {
		t.Fatalf("expected the materialized server, got %+v", found)
	}
	if _, err := s.WorkspaceMCPServers().GetByOriginConnection(ctx, ws.ID, uuid.NewString()); err != nil {
		t.Fatalf("expected (nil, nil) for unknown connection origin lookup, got %v", err)
	}

	// Delete cascades atomically.
	if err := s.Connections().Delete(ctx, ws.ID, c.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}

	if _, err := s.Connections().Get(ctx, ws.ID, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for the connection after delete, got %v", err)
	}
	if _, err := s.WorkspaceMCPServers().Get(ctx, ws.ID, srv.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for the materialized server after delete, got %v", err)
	}
	if found, err := s.WorkspaceMCPServers().GetByOriginConnection(ctx, ws.ID, c.ID); err != nil || found != nil {
		t.Fatalf("expected (nil, nil) origin lookup after delete, got (%+v, %v)", found, err)
	}

	for _, a := range []*domain.Agent{agentA, agentB} {
		got, err := s.Agents().ByID(ctx, ws.ID, a.ID)
		if err != nil {
			t.Fatalf("unexpected agent get error: %v", err)
		}
		if slices.Contains(got.EnabledMCPS, srv.ID) {
			t.Fatalf("expected agent %s to lose the deleted server reference, got %v", a.Slug, got.EnabledMCPS)
		}
	}

	gotA, err := s.Agents().ByID(ctx, ws.ID, agentA.ID)
	if err != nil {
		t.Fatalf("unexpected agent get error: %v", err)
	}
	if !slices.Contains(gotA.EnabledMCPS, other.ID) {
		t.Fatalf("expected agent A to keep its other attachment, got %v", gotA.EnabledMCPS)
	}
	if _, err := s.WorkspaceMCPServers().Get(ctx, ws.ID, other.ID); err != nil {
		t.Fatalf("expected the hand-made server to survive, got %v", err)
	}
}

// The origin marker is birth-stamped: Update cannot re-point or clear a
// managed server's origin (design.md D11).
func TestIntegration_ConnectionStore_OriginMarkerBirthStamped(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := pgConnSeedWorkspace(t, ctx, s, "pg-conn-birth")

	c := &domain.Connection{WorkspaceID: ws.ID, Service: "github", AccessLevel: domain.ConnectionAccessReadOnly}
	if err := s.Connections().Create(ctx, c); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	srv := &domain.WorkspaceMCPServer{
		WorkspaceID: ws.ID,
		Name:        "GitHub (Managed)",
		MCPConnection: domain.MCPConnection{
			Transport: domain.MCPTransportStreamableHTTP,
			URL:       "https://api.githubcopilot.com/mcp/",
		},
		OriginConnectionID: c.ID,
	}
	if err := s.WorkspaceMCPServers().Create(ctx, srv); err != nil {
		t.Fatalf("unexpected materialize error: %v", err)
	}

	// Update carrying a cleared marker must not clear the stored one.
	srv.OriginConnectionID = ""
	srv.Name = "GitHub (Renamed)"
	if err := s.WorkspaceMCPServers().Update(ctx, srv); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	got, err := s.WorkspaceMCPServers().Get(ctx, ws.ID, srv.ID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if got.OriginConnectionID != c.ID {
		t.Fatalf("expected origin marker to survive update, got %q", got.OriginConnectionID)
	}
	if got.Name != "GitHub (Renamed)" {
		t.Fatalf("expected the editable rename to apply, got %q", got.Name)
	}

	// Hand-made servers read back with an empty marker.
	handmade := &domain.WorkspaceMCPServer{
		WorkspaceID:   ws.ID,
		Name:          "Hand Made",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}
	if err := s.WorkspaceMCPServers().Create(ctx, handmade); err != nil {
		t.Fatalf("unexpected create handmade error: %v", err)
	}
	gotHandmade, err := s.WorkspaceMCPServers().Get(ctx, ws.ID, handmade.ID)
	if err != nil {
		t.Fatalf("unexpected get handmade error: %v", err)
	}
	if gotHandmade.OriginConnectionID != "" {
		t.Fatalf("expected empty origin marker on hand-made server, got %q", gotHandmade.OriginConnectionID)
	}
}

// TestIntegration_ConnectionsSchema covers migration 000062 (tasks.md 1.3):
// the workspace_connections table exists with its unique (workspace_id,
// service) constraint, and workspace_mcp_servers carries the nullable indexed
// origin_connection_id column with its FK to workspace_connections.
// The OAuth token lifecycle (add-connection-oauth tasks.md 1.2/1.3): Create
// persists the token set in one write and defaults an empty status to
// connected; UpdateTokenLifecycle rewrites only the lifecycle columns,
// workspace-scoped, mirroring the fake's semantics.
func TestIntegration_ConnectionStore_TokenLifecycle(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := pgConnSeedWorkspace(t, ctx, s, "pg-conn-lifecycle")

	expires := time.Now().Add(3600 * time.Second).UTC().Truncate(time.Microsecond)
	c := &domain.Connection{
		WorkspaceID:       ws.ID,
		Service:           "atlassian",
		AccessLevel:       domain.ConnectionAccessReadWrite,
		RefreshCiphertext: "v1:cmVmcmVzaA:ZW52ZWxvcGU",
		ExpiresAt:         &expires,
		GrantedScopes:     []string{"read:jira-work", "offline_access"},
	}
	if err := s.Connections().Create(ctx, c); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	got, err := s.Connections().Get(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if got.Status != domain.ConnectionStatusConnected {
		t.Fatalf("expected empty status stored as connected, got %q", got.Status)
	}
	if got.RefreshCiphertext != c.RefreshCiphertext || got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) {
		t.Fatalf("expected the token set to roundtrip, got %+v", got)
	}
	if !slices.Equal(got.GrantedScopes, c.GrantedScopes) {
		t.Fatalf("expected granted scopes to roundtrip, got %v", got.GrantedScopes)
	}
	if got.GrantedScopes == nil {
		t.Fatal("expected granted scopes to read back as an array, got nil")
	}

	// Refresh failure flips the connection expired in place; identity
	// columns survive.
	c.Status = domain.ConnectionStatusExpired
	if err := s.Connections().UpdateTokenLifecycle(ctx, c); err != nil {
		t.Fatalf("unexpected lifecycle update error: %v", err)
	}
	got, err = s.Connections().Get(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if got.Status != domain.ConnectionStatusExpired {
		t.Fatalf("expected expired status, got %q", got.Status)
	}
	if got.Service != "atlassian" || got.AccessLevel != domain.ConnectionAccessReadWrite {
		t.Fatalf("expected identity columns untouched, got %+v", got)
	}
	if got.RefreshCiphertext != c.RefreshCiphertext {
		t.Fatalf("expected the refresh envelope untouched by the status flip, got %q", got.RefreshCiphertext)
	}

	// Scope guards mirror the fake: nil, invalid, cross-tenant, unknown.
	if err := s.Connections().UpdateTokenLifecycle(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil connection, got %v", err)
	}
	foreign := &domain.Connection{WorkspaceID: uuid.NewString(), ID: c.ID, Service: "atlassian", AccessLevel: domain.ConnectionAccessReadOnly, Status: domain.ConnectionStatusExpired}
	if err := s.Connections().UpdateTokenLifecycle(ctx, foreign); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant lifecycle update, got %v", err)
	}
	unknown := &domain.Connection{WorkspaceID: ws.ID, ID: uuid.NewString(), Service: "atlassian", AccessLevel: domain.ConnectionAccessReadOnly, Status: domain.ConnectionStatusExpired}
	if err := s.Connections().UpdateTokenLifecycle(ctx, unknown); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown lifecycle update, got %v", err)
	}

	// Out-of-catalog status is rejected at the boundary (the store validates
	// before the write); the DB-level status CHECK is verified by
	// TestIntegration_ConnectionOAuthSchema.
	bad := &domain.Connection{WorkspaceID: ws.ID, ID: c.ID, Service: "atlassian", AccessLevel: domain.ConnectionAccessReadOnly, Status: "revoked"}
	if err := s.Connections().UpdateTokenLifecycle(ctx, bad); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for out-of-catalog status, got %v", err)
	}
}

// TestIntegration_ConnectionOAuthSchema covers migration 000063
// (add-connection-oauth tasks.md 1.3): workspace_connections carries the
// token-lifecycle columns (refresh envelope, expiry, granted scopes, status
// with its catalog CHECK) and the provider-unique instance_oauth_apps table
// exists.
func TestIntegration_ConnectionOAuthSchema(t *testing.T) {
	_, schemaDSN, ctx := setupTestSchema(t)

	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer conn.Close(ctx)

	// The lifecycle columns on workspace_connections. expires_at is the one
	// nullable lifecycle column: PAT connections carry no expiry by design.
	wantColumns := []struct {
		name    string
		notNull bool
	}{
		{"refresh_ciphertext", true},
		{"expires_at", false},
		{"granted_scopes", true},
		{"status", true},
	}
	rows, err := conn.Query(ctx, `
		SELECT attname, attnotnull FROM pg_attribute
		WHERE attrelid = 'workspace_connections'::regclass AND attname = ANY($1)
	`, []string{"refresh_ciphertext", "expires_at", "granted_scopes", "status"})
	if err != nil {
		t.Fatalf("failed to read lifecycle columns: %v", err)
	}
	defer rows.Close()
	seen := make(map[string]bool, len(wantColumns))
	for rows.Next() {
		var name string
		var notNull bool
		if err := rows.Scan(&name, &notNull); err != nil {
			t.Fatalf("failed to scan column: %v", err)
		}
		seen[name] = true
		for _, want := range wantColumns {
			if want.name != name {
				continue
			}
			if notNull != want.notNull {
				t.Errorf("expected %q notNull=%v, got %v", name, want.notNull, notNull)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to iterate columns: %v", err)
	}
	for _, want := range wantColumns {
		if !seen[want.name] {
			t.Errorf("expected workspace_connections.%s to exist", want.name)
		}
	}

	// The status catalog CHECK.
	var check string
	err = conn.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid = 'workspace_connections'::regclass
		  AND contype = 'c' AND pg_get_constraintdef(oid) LIKE '%status%'
	`).Scan(&check)
	if err != nil {
		t.Fatalf("failed to read status check: %v", err)
	}
	for _, v := range []string{"connected", "error", "expired"} {
		if !strings.Contains(check, v) {
			t.Errorf("expected the status CHECK to allow %q, got %s", v, check)
		}
	}

	// The provider-unique instance OAuth apps table.
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "instance_oauth_apps").Scan(&exists); err != nil {
		t.Fatalf("failed to probe instance_oauth_apps: %v", err)
	}
	if !exists {
		t.Fatal("expected instance_oauth_apps to exist after migrations")
	}
	var pkColumns int
	if err := conn.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conrelid = 'instance_oauth_apps'::regclass AND contype = 'p'
	`).Scan(&pkColumns); err != nil {
		t.Fatalf("failed to read primary key: %v", err)
	}
	if pkColumns != 1 {
		t.Fatalf("expected a primary key on instance_oauth_apps (provider-unique), got %d", pkColumns)
	}
}

func TestIntegration_ConnectionsSchema(t *testing.T) {
	_, schemaDSN, ctx := setupTestSchema(t)

	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer conn.Close(ctx)

	var exists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "workspace_connections").Scan(&exists); err != nil {
		t.Fatalf("failed to probe table: %v", err)
	}
	if !exists {
		t.Fatal("expected workspace_connections to exist after migrations")
	}

	// The unique constraint is the DB-level backstop for one-connection-per-service.
	var constraint string
	if err := conn.QueryRow(ctx, `
		SELECT conname FROM pg_constraint
		WHERE conrelid = 'workspace_connections'::regclass AND contype = 'u'
	`).Scan(&constraint); err != nil {
		t.Fatalf("failed to read unique constraint: %v", err)
	}
	if constraint != "uq_workspace_connections_workspace_id_service" {
		t.Fatalf("expected uq_workspace_connections_workspace_id_service, got %q", constraint)
	}

	// The origin marker: present, nullable, FK to workspace_connections, indexed.
	var notNull bool
	if err := conn.QueryRow(ctx, `
		SELECT attnotnull FROM pg_attribute
		WHERE attrelid = 'workspace_mcp_servers'::regclass AND attname = 'origin_connection_id'
	`).Scan(&notNull); err != nil {
		t.Fatalf("failed to read origin_connection_id column: %v", err)
	}
	if notNull {
		t.Error("expected origin_connection_id to be nullable (hand-made servers are unmarked)")
	}

	var fkCount int
	if err := conn.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conrelid = 'workspace_mcp_servers'::regclass
		  AND contype = 'f'
		  AND confrelid = 'workspace_connections'::regclass
	`).Scan(&fkCount); err != nil {
		t.Fatalf("failed to probe origin FK: %v", err)
	}
	if fkCount != 1 {
		t.Errorf("expected one FK from workspace_mcp_servers to workspace_connections, got %d", fkCount)
	}

	var hasIndex bool
	if err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE tablename = 'workspace_mcp_servers'
			  AND indexname = 'idx_workspace_mcp_servers_origin_connection_id'
		)
	`).Scan(&hasIndex); err != nil {
		t.Fatalf("failed to probe origin index: %v", err)
	}
	if !hasIndex {
		t.Fatal("expected idx_workspace_mcp_servers_origin_connection_id to exist")
	}
}

// TestIntegration_ConnectionsPermissionBackfill covers migration 000062's
// integrations.write backfill (design.md D10, tasks.md 1.3): built-in
// Superadmin/Owner/Admin roles gain the permission idempotently (exactly one
// entry, no duplicates across re-runs), custom roles never do. Follows the
// 000027 hooks backfill test's shape: seed pre-migration role snapshots, then
// re-run the REAL migration (down to 61, up) against the seeded rows — which
// also exercises 000062's down DDL against a populated schema.
func TestIntegration_ConnectionsPermissionBackfill(t *testing.T) {
	s, schemaDSN, ctx := setupTestSchema(t)

	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer conn.Close(ctx)

	// Seed the built-in administrator roles with a pre-000062 permission
	// snapshot (no integrations.write), as roles existing before the upgrade
	// would look.
	ws := &domain.Workspace{Slug: "conn-backfill", Name: "Conn Backfill"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected Create workspace error: %v", err)
	}
	for _, name := range []string{domain.RoleSuperadmin, domain.RoleOwner, domain.RoleAdmin} {
		r := &domain.Role{
			WorkspaceID: ws.ID,
			Name:        name,
			Permissions: []string{domain.WorkspaceRead},
			BuiltIn:     true,
		}
		if err := s.Roles().Create(ctx, r); err != nil {
			t.Fatalf("unexpected Create role %s error: %v", name, err)
		}
	}
	custom := &domain.Role{
		WorkspaceID: ws.ID,
		Name:        "Tooling",
		Permissions: []string{domain.ToolsWrite},
		BuiltIn:     false,
	}
	if err := s.Roles().Create(ctx, custom); err != nil {
		t.Fatalf("unexpected Create custom role error: %v", err)
	}

	assertBackfilled := func(stage string) {
		t.Helper()
		for _, name := range []string{domain.RoleSuperadmin, domain.RoleOwner, domain.RoleAdmin} {
			r, err := s.Roles().FindByName(ctx, ws.ID, name)
			if err != nil {
				t.Fatalf("[%s] unexpected FindByName %s error: %v", stage, name, err)
			}
			count := 0
			for _, p := range r.Permissions {
				if p == domain.IntegrationsWrite {
					count++
				}
			}
			if count != 1 {
				t.Errorf("[%s] %s: expected exactly one integrations.write, got %d in %v", stage, name, count, r.Permissions)
			}
		}
		r, err := s.Roles().FindByName(ctx, ws.ID, custom.Name)
		if err != nil {
			t.Fatalf("[%s] unexpected FindByName custom error: %v", stage, err)
		}
		if slices.Contains(r.Permissions, domain.IntegrationsWrite) {
			t.Errorf("[%s] expected custom role NOT to hold integrations.write, got %v", stage, r.Permissions)
		}
	}

	// The backfill already ran during setup on an empty roles table; re-run
	// the real migration (down to the pre-backfill world at version 61, then
	// up) against the seeded rows. The 000062 down migration strips the
	// permission from built-in roles and drops the connections DDL; up
	// recreates and backfills.
	mig := postgres.NewMigrator(schemaDSN)

	rerun := func(stage string) {
		t.Helper()
		before, dirty, err := mig.Status()
		if err != nil || dirty {
			t.Fatalf("[%s] failed to read version before down: v=%d dirty=%v err=%v", stage, before, dirty, err)
		}
		if before < 62 {
			t.Fatalf("[%s] expected version >= 62 before down, got %d", stage, before)
		}
		if err := mig.MigrateToVersion(61); err != nil {
			t.Fatalf("[%s] failed to migrate down to 000061: %v", stage, err)
		}
		if v, dirty, err := mig.Status(); err != nil || v != 61 || dirty {
			t.Fatalf("[%s] expected clean version 61 after down, got v=%d dirty=%v err=%v", stage, v, dirty, err)
		}
		if err := mig.Up(); err != nil {
			t.Fatalf("[%s] failed to re-run migrations above 000061: %v", stage, err)
		}
		if v, dirty, err := mig.Status(); err != nil || v != before || dirty {
			t.Fatalf("[%s] expected clean version %d after up, got v=%d dirty=%v err=%v", stage, before, v, dirty, err)
		}
		assertBackfilled(stage)
	}

	// Idempotent: strip and re-run once more; no duplicates may appear.
	strip := func() {
		t.Helper()
		if _, err := conn.Exec(ctx,
			`UPDATE roles SET permissions = array_remove(permissions, 'integrations.write') WHERE built_in = true`,
		); err != nil {
			t.Fatalf("failed to strip integrations.write: %v", err)
		}
	}
	strip()
	rerun("first run")

	strip()
	rerun("second run")
}
