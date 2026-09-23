package services_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ---------------------------------------------------------------------------
// Connections service (add-workspace-connections tasks 5.1): connect
// happy path, probe-gate hygiene, conflicts, validation, hint-only reads,
// disconnect cascade, and probe-on-demand — all against the in-memory fake
// with the real MCP settings service (real crypto over fake stores) and a
// stubbed prober.
// ---------------------------------------------------------------------------

const (
	testToken         = "ghp_smoke-token-abcd1234"
	testTokenHint     = "1234"
	testToolCount     = 3
	testEncKey        = "01234567890123456789012345678901"
	testUserID        = "test-user-0000-0000-000000000001"
	testPublicBaseURL = "https://onclaw.example.com"
)

// connectionsTestEnv is one fake store + real settings service + recording
// stub prober.
type connectionsTestEnv struct {
	store store.Store
	svc   *services.ConnectionsService
	wsID  string

	probeCalls int
	probeErr   error
	// connectionsAtProbe records how many connection rows existed when the
	// prober ran — probe-before-persist is the invariant under test.
	connectionsAtProbe int
}

func newConnectionsTestEnv(t *testing.T) *connectionsTestEnv {
	t.Helper()
	ctx := context.Background()

	st := storefake.New()
	if err := st.Workspaces().Create(ctx, &domain.Workspace{
		Slug:     "conn-ws",
		Name:     "Connections WS",
		Timezone: "UTC",
	}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	ws, err := st.Workspaces().BySlug(ctx, "conn-ws")
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}

	env := &connectionsTestEnv{store: st, wsID: ws.ID}
	settings := agents.NewMCPSettingsService(st.WorkspaceMCPServers(), st.AgentMCPServers(), st.Agents(), []byte(testEncKey))
	env.svc = services.NewConnectionsService(
		st.Connections(),
		st.WorkspaceMCPServers(),
		st.Agents(),
		settings,
		st.OAuthApps(),
		[]byte(testEncKey),
		testPublicBaseURL,
		services.WithProber(func(ctx context.Context, workspaceID, serverID, name string, conn domain.MCPConnection) (int, error) {
			env.probeCalls++
			connections, err := st.Connections().List(ctx, workspaceID)
			if err != nil {
				return 0, err
			}
			env.connectionsAtProbe = len(connections)
			if env.probeErr != nil {
				return 0, env.probeErr
			}
			return testToolCount, nil
		}),
	)
	return env
}

// connectView runs a PAT connect and unwraps the synchronous view (OAuth
// connects return an authorize URL instead).
func connectView(t *testing.T, env *connectionsTestEnv, recipeID, accessLevel, token string) (*services.ConnectionView, error) {
	t.Helper()
	res, err := env.svc.Connect(context.Background(), env.wsID, testUserID, recipeID, accessLevel, token)
	if err != nil {
		return nil, err
	}
	return res.Connection, nil
}

func TestConnectionsService_ConnectHappyPath(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// The joined view: recipe id as service, read-only default, connected
	// status with the materialized server's handle and the hint-only token.
	if view.Service != "github" || view.AccessLevel != domain.ConnectionAccessReadOnly {
		t.Errorf("expected github/read_only, got %s/%s", view.Service, view.AccessLevel)
	}
	if view.ID == "" {
		t.Error("expected a store-assigned connection id")
	}
	if view.ServerID == "" || view.ServerEnabled == nil || !*view.ServerEnabled {
		t.Errorf("expected an enabled materialized server, got %q", view.ServerID)
	}
	if view.Status != domain.MCPStatusConnected {
		t.Errorf("expected status connected, got %q", view.Status)
	}
	if view.TokenHint != testTokenHint {
		t.Errorf("expected token hint %q, got %q", testTokenHint, view.TokenHint)
	}
	if view.ToolCount != testToolCount {
		t.Errorf("expected tool count %d, got %d", testToolCount, view.ToolCount)
	}
	if len(view.AttachedAgents) != 0 {
		t.Errorf("expected no attached agents, got %v", view.AttachedAgents)
	}
	if strings.Contains(view.StatusError, testToken) || strings.Contains(view.TokenHint, testToken) {
		t.Error("view leaked token material")
	}

	// Probe-before-persist: the prober saw ZERO connection rows.
	if env.probeCalls != 1 || env.connectionsAtProbe != 0 {
		t.Errorf("expected exactly one probe over an empty connection table, got %d probes with %d rows", env.probeCalls, env.connectionsAtProbe)
	}

	// The materialized server: recipe endpoint/transport, birth-stamped origin
	// marker, encrypted Authorization row (never plaintext), persisted status.
	server, err := env.store.WorkspaceMCPServers().Get(ctx, env.wsID, view.ServerID)
	if err != nil {
		t.Fatalf("load materialized server: %v", err)
	}
	if server.OriginConnectionID != view.ID {
		t.Errorf("expected origin marker %q, got %q", view.ID, server.OriginConnectionID)
	}
	if server.Transport != domain.MCPTransportStreamableHTTP || server.URL != "https://api.githubcopilot.com/mcp/" {
		t.Errorf("unexpected materialized connection: %s %s", server.Transport, server.URL)
	}
	if len(server.Headers) != 1 || server.Headers[0].Name != "Authorization" {
		t.Fatalf("expected a single Authorization header row, got %+v", server.Headers)
	}
	if !strings.HasPrefix(server.Headers[0].Value, "v1:") {
		t.Errorf("expected an encrypted envelope, got plaintext-shaped value %q", server.Headers[0].Value)
	}
	if strings.Contains(server.Headers[0].Value, testToken) {
		t.Error("token ciphertext must not contain the plaintext token")
	}
	if server.Status != domain.MCPStatusConnected || server.ToolCount != testToolCount {
		t.Errorf("expected persisted connected status with tool count, got %s/%d", server.Status, server.ToolCount)
	}

	// The connection row itself exists and carries no hint-shaped fields.
	if _, err := env.store.Connections().Get(ctx, env.wsID, view.ID); err != nil {
		t.Fatalf("expected the connection row, got %v", err)
	}
}

func TestConnectionsService_ProbeFailureStoresNothing(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()
	env.probeErr = errors.New("Bad credentials")

	_, err := connectView(t, env, "github", domain.ConnectionAccessReadWrite, testToken)
	if !errors.Is(err, services.ErrProbeFailed) {
		t.Fatalf("expected ErrProbeFailed, got %v", err)
	}
	// The upstream message rides verbatim after the "probe failed: " prefix.
	if !strings.Contains(err.Error(), "probe failed: Bad credentials") {
		t.Errorf("expected the upstream message in the error, got %q", err.Error())
	}

	// Nothing stored: no connection row, no server row — no token ciphertext
	// anywhere (tasks.md 2.2).
	connections, err := env.store.Connections().List(ctx, env.wsID)
	if err != nil || len(connections) != 0 {
		t.Fatalf("expected no connection rows after probe failure, got %d (%v)", len(connections), err)
	}
	servers, err := env.store.WorkspaceMCPServers().List(ctx, env.wsID)
	if err != nil || len(servers) != 0 {
		t.Fatalf("expected no server rows after probe failure, got %d (%v)", len(servers), err)
	}
}

func TestConnectionsService_DuplicateServiceConflict(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	first, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("first connect: %v", err)
	}

	_, err = connectView(t, env, "github", domain.ConnectionAccessReadWrite, "another-token")
	if !errors.Is(err, domain.ErrConnectionExists) {
		t.Fatalf("expected ErrConnectionExists, got %v", err)
	}
	// The conflict names the existing connection (design.md D6).
	if !strings.Contains(err.Error(), first.ID) {
		t.Errorf("expected the conflict to name connection %s, got %q", first.ID, err.Error())
	}
	// The duplicate check precedes the probe: still exactly one call.
	if env.probeCalls != 1 {
		t.Errorf("expected no additional probe on conflict, got %d calls", env.probeCalls)
	}
	// The first connection survives.
	if _, err := env.store.Connections().GetByService(ctx, env.wsID, "github"); err != nil {
		t.Fatalf("expected the first connection to survive, got %v", err)
	}
}

func TestConnectionsService_ConnectValidation(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	cases := []struct {
		name        string
		recipeID    string
		accessLevel string
		token       string
		wantErr     error
	}{
		{"unknown recipe", "not-a-recipe", "", testToken, domain.ErrUnknownRecipe},
		{"coming soon accepts no connections", "atlassian", "", testToken, domain.ErrInvalid},
		{"access level outside the recipe's offer", "github", "write_only", testToken, domain.ErrInvalid},
		{"access level outside the catalog", "github", "root", testToken, domain.ErrInvalid},
		{"empty token", "github", "", "   ", domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := connectView(t, env, tc.recipeID, tc.accessLevel, tc.token)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
	if env.probeCalls != 0 {
		t.Errorf("validation failures must not probe, got %d calls", env.probeCalls)
	}
	connections, _ := env.store.Connections().List(ctx, env.wsID)
	if len(connections) != 0 {
		t.Errorf("validation failures must not store, got %d rows", len(connections))
	}
}

func TestConnectionsService_ReadWriteLevelRoundTrip(t *testing.T) {
	env := newConnectionsTestEnv(t)

	view, err := connectView(t, env, "gitlab", domain.ConnectionAccessReadWrite, "glpat-xyz9876")
	if err != nil {
		t.Fatalf("connect gitlab read_write: %v", err)
	}
	if view.AccessLevel != domain.ConnectionAccessReadWrite {
		t.Errorf("expected read_write recorded, got %q", view.AccessLevel)
	}
	if view.TokenHint != "9876" {
		t.Errorf("expected hint 9876, got %q", view.TokenHint)
	}
	if view.ServerID == "" || view.Status != domain.MCPStatusConnected {
		t.Errorf("expected a connected materialized server, got %q/%q", view.ServerID, view.Status)
	}
}

func TestConnectionsService_HintOnlyReads(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	created, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	got, err := env.svc.Get(ctx, env.wsID, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.TokenHint != testTokenHint || strings.Contains(got.TokenHint, testToken) {
		t.Errorf("get: expected hint-only token display, got %q", got.TokenHint)
	}

	list, err := env.svc.List(ctx, env.wsID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: expected one connection, got %d (%v)", len(list), err)
	}
	if list[0].TokenHint != testTokenHint || strings.Contains(list[0].TokenHint, testToken) {
		t.Errorf("list: expected hint-only token display, got %q", list[0].TokenHint)
	}
}

func TestConnectionsService_DisconnectCascade(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// An agent attached to the materialized server loses the reference on
	// disconnect.
	agent := &domain.Agent{
		WorkspaceID: env.wsID,
		Slug:        "atlas",
		Name:        "Atlas",
		EnabledMCPS: []string{view.ServerID},
	}
	if err := env.store.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// The attached agents join reads through the connection view.
	got, err := env.svc.Get(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.AttachedAgents) != 1 || got.AttachedAgents[0] != "Atlas" {
		t.Errorf("expected [Atlas] attached, got %v", got.AttachedAgents)
	}

	serverID, err := env.svc.Disconnect(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if serverID != view.ServerID {
		t.Errorf("expected the materialized server id back, got %q", serverID)
	}

	// The connection, its server, and the agent reference are all gone.
	if _, err := env.store.Connections().Get(ctx, env.wsID, view.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected the connection gone, got %v", err)
	}
	if _, err := env.store.WorkspaceMCPServers().Get(ctx, env.wsID, view.ServerID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected the materialized server gone, got %v", err)
	}
	after, err := env.store.Agents().ByID(ctx, env.wsID, agent.ID)
	if err != nil {
		t.Fatalf("reload agent: %v", err)
	}
	for _, id := range after.EnabledMCPS {
		if id == view.ServerID {
			t.Error("expected the agent's attachment reference stripped")
		}
	}

	// Disconnecting again is 404.
	if _, err := env.svc.Disconnect(ctx, env.wsID, view.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound on double disconnect, got %v", err)
	}
}

func TestConnectionsService_ProbeOnDemand(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// A failing re-probe persists the error status and returns the refreshed
	// view — a status, not a request error.
	env.probeErr = errors.New("connection refused")
	refreshed, err := env.svc.Probe(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("probe after failure: %v", err)
	}
	if refreshed.Status != domain.MCPStatusError || refreshed.StatusError != "connection refused" {
		t.Errorf("expected persisted error status, got %q/%q", refreshed.Status, refreshed.StatusError)
	}

	// A passing re-probe restores connected and the tool count.
	env.probeErr = nil
	refreshed, err = env.svc.Probe(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("probe after recovery: %v", err)
	}
	if refreshed.Status != domain.MCPStatusConnected || refreshed.StatusError != "" {
		t.Errorf("expected connected status, got %q/%q", refreshed.Status, refreshed.StatusError)
	}

	// Unknown ids are ErrNotFound.
	if _, err := env.svc.Probe(ctx, env.wsID, "no-such-connection"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestConnectionsService_MaterializationFailureCompensates(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	// A hand-made server already holds the recipe's display name, so the
	// settings service rejects the materialization after the connection row
	// was created — the service must compensate the row away.
	handMade := &domain.WorkspaceMCPServer{
		WorkspaceID: env.wsID,
		Name:        "GitHub",
		MCPConnection: domain.MCPConnection{
			Transport: domain.MCPTransportStdio,
			Command:   "/bin/true",
		},
		Enabled: true,
	}
	settings := agents.NewMCPSettingsService(env.store.WorkspaceMCPServers(), env.store.AgentMCPServers(), env.store.Agents(), []byte(testEncKey))
	if err := settings.CreateWorkspaceServer(ctx, handMade); err != nil {
		t.Fatalf("seed hand-made server: %v", err)
	}

	_, err := connectView(t, env, "github", "", testToken)
	if err == nil {
		t.Fatal("expected the name-taken materialization failure")
	}
	if errors.Is(err, services.ErrProbeFailed) {
		t.Errorf("expected the server-creation failure, got %v", err)
	}
	// The connection row did not survive its failed materialization.
	if _, err := env.store.Connections().GetByService(ctx, env.wsID, "github"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected the connection row compensated away, got %v", err)
	}
}

func TestConnectionsService_ConnectionDisplayName(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if got := env.svc.ConnectionDisplayName(ctx, env.wsID, view.ID); got != "GitHub" {
		t.Errorf("expected the recipe's display name, got %q", got)
	}
	// Unknown ids degrade to the id itself — the pointer error still names
	// something useful.
	if got := env.svc.ConnectionDisplayName(ctx, env.wsID, "missing"); got != "missing" {
		t.Errorf("expected id fallback, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// add-integration-authority tasks 2.5/2.1: the declaration-derived tier
// counts on every connection view, and the origin-link lookup seam the
// runner's MCP pass satisfies through this service.
// ---------------------------------------------------------------------------

func TestConnectionsService_TierCountsProjection(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Declaration-derived: the projection is exactly the recipe's declared
	// split (contract §4 pins github at 37 read / 17 write — both tiers must
	// stay declared for the split to be meaningful).
	want := domain.ToolTierCounts(domain.RecipeByID("github"))
	if want.Read == 0 || want.Write == 0 {
		t.Fatalf("the github tier lists must declare both tiers, got %+v", want)
	}
	if view.TierCounts != want {
		t.Errorf("connect view tier_counts = %+v, want %+v", view.TierCounts, want)
	}

	// Get and List carry the same projection.
	got, err := env.svc.Get(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.TierCounts != want {
		t.Errorf("get view tier_counts = %+v, want %+v", got.TierCounts, want)
	}
	list, err := env.svc.List(ctx, env.wsID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].TierCounts != want {
		t.Errorf("list views = %+v, want the github projection %+v", list, want)
	}

	// An unregistered recipe counts zero, not garbage.
	if zero := domain.ToolTierCounts(domain.RecipeByID("no-such-recipe")); zero != (domain.RecipeTierCounts{}) {
		t.Errorf("unregistered recipe counts = %+v, want zero", zero)
	}
}

func TestConnectionsService_ConnectionServiceOfLookup(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	service, err := env.svc.ConnectionServiceOf(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if service != "github" {
		t.Errorf("connection service = %q, want the recipe id", service)
	}

	// Unknown and cross-workspace ids are ErrNotFound — the runner's gate
	// degrades those to the fail-safe write tier.
	if _, err := env.svc.ConnectionServiceOf(ctx, env.wsID, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown connection: expected ErrNotFound, got %v", err)
	}
}
