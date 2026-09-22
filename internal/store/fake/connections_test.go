package fake_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// connSeedWorkspace creates a plain workspace for connection store tests.
func connSeedWorkspace(t *testing.T, ctx context.Context, s store.Store, slug string) *domain.Workspace {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "Conn WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}
	return ws
}

// connSeedAgent creates a workspace-scoped agent (inherit model pair — no
// provider needed) for attachment-reference tests.
func connSeedAgent(t *testing.T, ctx context.Context, s store.Store, ws *domain.Workspace, slug string) *domain.Agent {
	t.Helper()
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: slug, Name: "Agent " + slug}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("unexpected create agent error: %v", err)
	}
	return a
}

// connMaterialize creates the origin-marked server row a connection owns, the
// way the connections service does at connect time.
func connMaterialize(t *testing.T, ctx context.Context, s store.Store, ws *domain.Workspace, c *domain.Connection) *domain.WorkspaceMCPServer {
	t.Helper()
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
		t.Fatalf("unexpected materialize server error: %v", err)
	}
	return srv
}

func TestConnectionStore_CRUD(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	ws1 := connSeedWorkspace(t, ctx, s, "conn-1")
	ws2 := connSeedWorkspace(t, ctx, s, "conn-2")

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

	// 3. Cross-tenant and unknown lookups are indistinguishable (ErrNotFound).
	if _, err := s.Connections().Get(ctx, ws2.ID, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant get, got %v", err)
	}
	if _, err := s.Connections().Get(ctx, ws1.ID, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown id, got %v", err)
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
	if _, err := s.Connections().GetByService(ctx, ws1.ID, "gitlab"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unconnected service, got %v", err)
	}

	// 5. List is workspace-scoped and ordered.
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
	empty, err := s.Connections().List(ctx, "")
	if err != nil {
		t.Fatalf("unexpected list empty-scope error: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty list for empty scope, got %d", len(empty))
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
	if err := s.Connections().Create(ctx, &domain.Connection{
		WorkspaceID: ws1.ID,
		Service:     "   ",
		AccessLevel: domain.ConnectionAccessReadOnly,
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for blank service, got %v", err)
	}

	// 7. Unknown workspace surfaces as ErrNotFound (FK parity).
	if err := s.Connections().Create(ctx, &domain.Connection{
		WorkspaceID: "no-such-workspace",
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
}

// One connection per service per workspace (design.md D6): the duplicate
// chains to ErrConflict; other workspaces and other services are unaffected.
func TestConnectionStore_ServiceUniqueness(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	ws1 := connSeedWorkspace(t, ctx, s, "conn-uniq-1")
	ws2 := connSeedWorkspace(t, ctx, s, "conn-uniq-2")

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
	if !errors.Is(err, domain.ErrConnectionExists) {
		t.Fatalf("expected ErrConnectionExists for duplicate service, got %v", err)
	}
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConnectionExists to chain to ErrConflict, got %v", err)
	}

	// The same service in another workspace is fine.
	other := &domain.Connection{
		WorkspaceID: ws2.ID,
		Service:     "github",
		AccessLevel: domain.ConnectionAccessReadOnly,
	}
	if err := s.Connections().Create(ctx, other); err != nil {
		t.Fatalf("expected same service in different workspace to succeed, got %v", err)
	}

	// A different service in the same workspace is fine.
	if err := s.Connections().Create(ctx, &domain.Connection{
		WorkspaceID: ws1.ID,
		Service:     "gitlab",
		AccessLevel: domain.ConnectionAccessReadOnly,
	}); err != nil {
		t.Fatalf("expected different service in same workspace to succeed, got %v", err)
	}
}

// Disconnect cascade (design.md D8, tasks.md 1.5): deleting the connection
// removes the materialized server row and every agent's attachment reference
// to it; hand-made servers and other connections are untouched. The cascade
// runs inside the store's transaction seam, so a rollback undoes all of it.
func TestConnectionStore_DeleteCascade(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	ws := connSeedWorkspace(t, ctx, s, "conn-cascade")
	agentA := connSeedAgent(t, ctx, s, ws, "atlas")
	agentB := connSeedAgent(t, ctx, s, ws, "beacon")

	c := &domain.Connection{
		WorkspaceID: ws.ID,
		Service:     "github",
		AccessLevel: domain.ConnectionAccessReadOnly,
	}
	if err := s.Connections().Create(ctx, c); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	srv := connMaterialize(t, ctx, s, ws, c)

	// Two agents attach the materialized server; one carries other
	// attachments that must survive.
	other := connMaterializeNamed(t, ctx, s, ws, "GitLab (Managed)", "https://gitlab.com/api/v4/mcp")
	attach(t, ctx, s, agentA, srv.ID, other.ID)
	attach(t, ctx, s, agentB, srv.ID)

	// 1. GetByOriginConnection resolves the materialized server.
	found, err := s.WorkspaceMCPServers().GetByOriginConnection(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected origin lookup error: %v", err)
	}
	if found == nil || found.ID != srv.ID {
		t.Fatalf("expected the materialized server, got %+v", found)
	}
	if _, err := s.WorkspaceMCPServers().GetByOriginConnection(ctx, "other-ws", c.ID); err != nil {
		t.Fatalf("expected (nil, nil) for foreign workspace origin lookup, got %v", err)
	}
	if _, err := s.WorkspaceMCPServers().GetByOriginConnection(ctx, ws.ID, "no-such-connection"); err != nil {
		t.Fatalf("expected (nil, nil) for unknown connection origin lookup, got %v", err)
	}

	// 2. Delete cascades.
	if err := s.Connections().Delete(ctx, "other-ws", c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant delete, got %v", err)
	}
	if err := s.Connections().Delete(ctx, ws.ID, "no-such-id"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown delete, got %v", err)
	}
	if err := s.Connections().Delete(ctx, ws.ID, c.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}

	// 3. The connection and its materialized server are gone.
	if _, err := s.Connections().Get(ctx, ws.ID, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for the connection after delete, got %v", err)
	}
	if _, err := s.Connections().GetByService(ctx, ws.ID, "github"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for the service after delete, got %v", err)
	}
	if _, err := s.WorkspaceMCPServers().Get(ctx, ws.ID, srv.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for the materialized server after delete, got %v", err)
	}
	if found, err := s.WorkspaceMCPServers().GetByOriginConnection(ctx, ws.ID, c.ID); err != nil || found != nil {
		t.Fatalf("expected (nil, nil) origin lookup after delete, got (%+v, %v)", found, err)
	}

	// 4. No agent lists the deleted server anymore.
	for _, agent := range []*domain.Agent{agentA, agentB} {
		got, err := s.Agents().ByID(ctx, ws.ID, agent.ID)
		if err != nil {
			t.Fatalf("unexpected agent get error: %v", err)
		}
		if slices.Contains(got.EnabledMCPS, srv.ID) {
			t.Fatalf("expected agent %s to lose the deleted server reference, got %v", agent.Slug, got.EnabledMCPS)
		}
	}

	// 5. Agent A kept its other attachment; the hand-made/unrelated server
	// row survives.
	gotA, err := s.Agents().ByID(ctx, ws.ID, agentA.ID)
	if err != nil {
		t.Fatalf("unexpected agent get error: %v", err)
	}
	if !slices.Contains(gotA.EnabledMCPS, other.ID) {
		t.Fatalf("expected agent A to keep its other attachment, got %v", gotA.EnabledMCPS)
	}
	if _, err := s.WorkspaceMCPServers().Get(ctx, ws.ID, other.ID); err != nil {
		t.Fatalf("expected the unrelated server to survive, got %v", err)
	}
}

// The cascade is transactional: a failed delete (unknown id) mutates nothing —
// here exercised through WithTx rollback semantics the service would hit if it
// composed disconnect with other work.
func TestConnectionStore_DeleteCascade_Rollback(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	ws := connSeedWorkspace(t, ctx, s, "conn-rollback")
	agent := connSeedAgent(t, ctx, s, ws, "atlas")

	c := &domain.Connection{WorkspaceID: ws.ID, Service: "github", AccessLevel: domain.ConnectionAccessReadOnly}
	if err := s.Connections().Create(ctx, c); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	srv := connMaterialize(t, ctx, s, ws, c)
	attach(t, ctx, s, agent, srv.ID)

	err := s.WithTx(ctx, func(tx store.Store) error {
		if err := tx.Connections().Delete(ctx, ws.ID, c.ID); err != nil {
			return err
		}
		return domain.ErrConflict // force rollback after the cascade ran
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected the forced conflict, got %v", err)
	}
	if _, err := s.Connections().Get(ctx, ws.ID, c.ID); err != nil {
		t.Fatalf("expected the connection to survive the rollback, got %v", err)
	}
	if _, err := s.WorkspaceMCPServers().Get(ctx, ws.ID, srv.ID); err != nil {
		t.Fatalf("expected the server to survive the rollback, got %v", err)
	}
	got, err := s.Agents().ByID(ctx, ws.ID, agent.ID)
	if err != nil {
		t.Fatalf("unexpected agent get error: %v", err)
	}
	if !slices.Contains(got.EnabledMCPS, srv.ID) {
		t.Fatalf("expected the attachment to survive the rollback, got %v", got.EnabledMCPS)
	}
}

// Hand-made servers (empty origin marker) are unaffected by the connection
// machinery: they carry no origin lookup result and survive disconnects.
func TestConnectionStore_HandMadeServersUntouched(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	ws := connSeedWorkspace(t, ctx, s, "conn-handmade")

	handmade := &domain.WorkspaceMCPServer{
		WorkspaceID: ws.ID,
		Name:        "Hand Made",
		MCPConnection: domain.MCPConnection{
			Transport: domain.MCPTransportStdio,
			Command:   "npx",
		},
	}
	if err := s.WorkspaceMCPServers().Create(ctx, handmade); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	// Deleting a connection that owns nothing must not touch hand-made rows.
	c := &domain.Connection{WorkspaceID: ws.ID, Service: "slack", AccessLevel: domain.ConnectionAccessReadOnly}
	if err := s.Connections().Create(ctx, c); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if err := s.Connections().Delete(ctx, ws.ID, c.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if _, err := s.WorkspaceMCPServers().Get(ctx, ws.ID, handmade.ID); err != nil {
		t.Fatalf("expected the hand-made server to survive, got %v", err)
	}

	// A hand-made server keeps an empty origin marker through Update — the
	// marker is birth-stamped and never rewritten (design.md D11).
	handmade.URL = "https://example.com/mcp"
	handmade.Transport = domain.MCPTransportStreamableHTTP
	handmade.Command = ""
	if err := s.WorkspaceMCPServers().Update(ctx, handmade); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	got, err := s.WorkspaceMCPServers().Get(ctx, ws.ID, handmade.ID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if got.OriginConnectionID != "" {
		t.Fatalf("expected empty origin marker on hand-made server, got %q", got.OriginConnectionID)
	}
}

// The origin marker is birth-stamped: Update cannot re-point or clear a
// managed server's origin (design.md D11).
func TestConnectionStore_OriginMarkerBirthStamped(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	ws := connSeedWorkspace(t, ctx, s, "conn-birth")

	c := &domain.Connection{WorkspaceID: ws.ID, Service: "github", AccessLevel: domain.ConnectionAccessReadOnly}
	if err := s.Connections().Create(ctx, c); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	srv := connMaterialize(t, ctx, s, ws, c)

	// A naive edit attempt carrying a cleared marker must not clear the
	// stored one.
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
}

func connMaterializeNamed(t *testing.T, ctx context.Context, s store.Store, ws *domain.Workspace, name, url string) *domain.WorkspaceMCPServer {
	t.Helper()
	srv := &domain.WorkspaceMCPServer{
		WorkspaceID: ws.ID,
		Name:        name,
		MCPConnection: domain.MCPConnection{
			Transport: domain.MCPTransportStreamableHTTP,
			URL:       url,
		},
	}
	if err := s.WorkspaceMCPServers().Create(ctx, srv); err != nil {
		t.Fatalf("unexpected create server error: %v", err)
	}
	return srv
}

func attach(t *testing.T, ctx context.Context, s store.Store, agent *domain.Agent, serverIDs ...string) {
	t.Helper()
	a, err := s.Agents().ByID(ctx, agent.WorkspaceID, agent.ID)
	if err != nil {
		t.Fatalf("unexpected agent get error: %v", err)
	}
	a.EnabledMCPS = append(a.EnabledMCPS, serverIDs...)
	if err := s.Agents().Update(ctx, a); err != nil {
		t.Fatalf("unexpected agent update error: %v", err)
	}
}
