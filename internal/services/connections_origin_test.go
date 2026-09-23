package services_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/services"
)

// ---------------------------------------------------------------------------
// Connection base-URL origin (add-recipe-base-url tasks 2.1–2.4): the origin
// resolves at connect (default when omitted, ParseOrigin-validated otherwise,
// rejected for non-parametrized recipes), uniqueness narrows to per (service,
// resolved origin), materialization derives the server URL from the resolved
// origin, and the origin is immutable across refresh/reauthorization — fake
// stores, real crypto, stubbed prober (the test-registered recipe never
// dials; the reauthorization fixtures reuse the OAuth flow env).
// ---------------------------------------------------------------------------

// originRecipeDefault is the test recipe's declared SaaS origin; the endpoint
// path is the constant that must survive origin resolution.
const (
	originRecipeDefault = "https://stub.example.com"
	originRecipePath    = "/api/v4/mcp"
	originFieldName     = "Instance base URL"
)

// registerOriginTestRecipe registers one mcp-kind PAT recipe declaring an
// origin parameter (the registry is global and panics on duplicates, so every
// caller passes a unique id) — the C2c-shaped fixture the built-ins do not
// carry yet.
func registerOriginTestRecipe(t *testing.T, id string) *domain.Recipe {
	t.Helper()
	domain.RegisterRecipe(domain.Recipe{
		ID:           id,
		Service:      "Stub Origin " + id,
		Icon:         "stub",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Kind:         domain.RecipeKindMCP,
		Transport:    domain.MCPTransportStreamableHTTP,
		Endpoint:     originRecipeDefault + originRecipePath,
		TokenHeader:  "Authorization",
		TokenScheme:  "Bearer",
		OriginParam: &domain.RecipeOriginParam{
			Name:    originFieldName,
			Default: originRecipeDefault,
			Help:    "The provider instance's origin (SaaS or self-managed).",
		},
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Generate a stub token"}},
		Probe:        domain.RecipeProbe{Tool: "stub.ping"},
	})
	recipe := domain.RecipeByID(id)
	if recipe == nil {
		t.Fatalf("recipe %q did not register", id)
	}
	return recipe
}

// connectOrigin runs a PAT connect carrying a base-URL origin and unwraps the
// synchronous view.
func connectOrigin(t *testing.T, env *connectionsTestEnv, recipeID, accessLevel, token, origin string) (*services.ConnectionView, error) {
	t.Helper()
	res, err := env.svc.Connect(context.Background(), env.wsID, testUserID, recipeID, accessLevel, token, origin)
	if err != nil {
		return nil, err
	}
	return res.Connection, nil
}

// Empty submission resolves the declared default: the stored origin and the
// materialized server URL are the SaaS origin with the recipe's path constant.
func TestConnect_OriginDefaultAppliedWhenOmitted(t *testing.T) {
	recipe := registerOriginTestRecipe(t, "stuborigin-default")
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectOrigin(t, env, recipe.ID, "", testToken, "")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if view.Origin != originRecipeDefault {
		t.Errorf("expected the declared default %q stored, got %q", originRecipeDefault, view.Origin)
	}
	server, err := env.store.WorkspaceMCPServers().GetByOriginConnection(ctx, env.wsID, view.ID)
	if err != nil || server == nil {
		t.Fatalf("load materialized server: %v (%v)", server, err)
	}
	if server.URL != originRecipeDefault+originRecipePath {
		t.Errorf("expected the default-origin materialization, got %q", server.URL)
	}
}

// A submitted origin replaces only the origin: the stored value is the
// ParseOrigin-normalized form and the materialized URL joins the recipe's
// path constant onto it.
func TestConnect_OriginDerivesMaterializedURL(t *testing.T) {
	recipe := registerOriginTestRecipe(t, "stuborigin-derive")
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectOrigin(t, env, recipe.ID, "", testToken, "https://stub.git.example.com:8443/")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// Normalized: trailing slash stripped, scheme and host lowercased.
	want := "https://stub.git.example.com:8443"
	if view.Origin != want {
		t.Errorf("expected the normalized origin %q stored, got %q", want, view.Origin)
	}
	server, err := env.store.WorkspaceMCPServers().GetByOriginConnection(ctx, env.wsID, view.ID)
	if err != nil || server == nil {
		t.Fatalf("load materialized server: %v (%v)", server, err)
	}
	if server.URL != want+originRecipePath {
		t.Errorf("expected the origin-joined materialization %q, got %q", want+originRecipePath, server.URL)
	}
	// A non-default origin disambiguates the materialized server's name (the
	// settings service requires distinct names per workspace).
	if expected := recipe.Service + " (stub.git.example.com:8443)"; server.Name != expected {
		t.Errorf("expected the origin-qualified server name %q, got %q", expected, server.Name)
	}
}

// Uniqueness is per (service, resolved origin): the same origin twice is
// rejected naming the origin and the existing connection, before any probe;
// a different origin connects side by side.
func TestConnect_OriginUniqueness(t *testing.T) {
	recipe := registerOriginTestRecipe(t, "stuborigin-uniq")
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	first, err := connectOrigin(t, env, recipe.ID, "", testToken, "https://one.stub.example.com")
	if err != nil {
		t.Fatalf("first connect: %v", err)
	}

	_, err = connectOrigin(t, env, recipe.ID, "", "another-token", "https://one.stub.example.com")
	if !errors.Is(err, domain.ErrConnectionExists) {
		t.Fatalf("expected ErrConnectionExists for the same origin, got %v", err)
	}
	for _, want := range []string{"https://one.stub.example.com", first.ID} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the conflict to name %q, got %q", want, err.Error())
		}
	}
	// The pre-check precedes the probe: still exactly one call.
	if env.probeCalls != 1 {
		t.Errorf("expected no additional probe on conflict, got %d calls", env.probeCalls)
	}

	second, err := connectOrigin(t, env, recipe.ID, "", testToken, "https://two.stub.example.com")
	if err != nil {
		t.Fatalf("second-origin connect: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("expected an independent connection for the second origin")
	}
	rows, err := env.store.Connections().List(ctx, env.wsID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("expected both origins side by side, got %d rows (%v)", len(rows), err)
	}
	if second.Origin != "https://two.stub.example.com" {
		t.Errorf("expected the second connection's own origin, got %q", second.Origin)
	}
}

// Plain recipes keep the plain per-service rule, and a submitted origin is
// IGNORED: the connection resolves to the recipe's fixed endpoint (spec:
// "Undeclared recipes ignore origin") — stored origin empty, materialized
// URL byte-identical to the declared endpoint. github declares an origin
// parameter since tasks.md 4.1, so the plain fixture is a test-registered
// param-less mcp recipe.
func TestConnect_PlainRecipeUniquenessAndOriginIgnored(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()
	domain.RegisterRecipe(domain.Recipe{
		ID:           "stuborigin-plain",
		Service:      "Stub Plain",
		Icon:         "stub",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Transport:    domain.MCPTransportStreamableHTTP,
		Endpoint:     "https://plain.stub.example.com/mcp",
		TokenHeader:  "Authorization",
		TokenScheme:  "Bearer",
		AccessLevels: []string{domain.ConnectionAccessReadOnly, domain.ConnectionAccessReadWrite},
		Steps:        []domain.RecipeStep{{Title: "Generate a stub token"}},
		Probe:        domain.RecipeProbe{Tool: "stub.ping"},
	})

	// The override changes nothing: a plain recipe has no origin parameter,
	// so the connect resolves to the fixed endpoint with an empty stored
	// origin.
	first, err := connectOrigin(t, env, "stuborigin-plain", "", testToken, "https://elsewhere.example.com")
	if err != nil {
		t.Fatalf("connect with ignored origin: %v", err)
	}
	if first.Origin != "" {
		t.Errorf("expected the undeclared origin ignored (empty stored origin), got %q", first.Origin)
	}
	server, err := env.store.WorkspaceMCPServers().GetByOriginConnection(ctx, env.wsID, first.ID)
	if err != nil || server == nil {
		t.Fatalf("load materialized server: %v (%v)", server, err)
	}
	if server.URL != "https://plain.stub.example.com/mcp" {
		t.Errorf("expected the fixed endpoint, got %q", server.URL)
	}
	if env.probeCalls != 1 {
		t.Errorf("expected exactly one probe, got %d calls", env.probeCalls)
	}

	// Same service, origin or not: the historical per-service conflict (the
	// ignored override resolves to the same fixed endpoint, origin '').
	_, err = connectOrigin(t, env, "stuborigin-plain", domain.ConnectionAccessReadWrite, "another-token", "https://yet.another.example.com")
	if !errors.Is(err, domain.ErrConnectionExists) {
		t.Fatalf("expected ErrConnectionExists for the plain duplicate, got %v", err)
	}
	rows, err := env.store.Connections().List(ctx, env.wsID)
	if err != nil || len(rows) != 1 || rows[0].ID != first.ID {
		t.Fatalf("expected only the first connection to survive, got %v (%v)", rows, err)
	}
}

// Non-origin values are rejected with a validation error naming the
// base-URL field, and nothing is stored or probed.
func TestConnect_InvalidOriginRejected(t *testing.T) {
	recipe := registerOriginTestRecipe(t, "stuborigin-invalid")
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	for name, origin := range map[string]string{
		"carries a path":     "https://stub.example.com/api/v4",
		"no scheme":          "stub.example.com",
		"unsupported scheme": "ftp://stub.example.com",
		"userinfo":           "https://user@stub.example.com",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := connectOrigin(t, env, recipe.ID, "", testToken, origin)
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
			if !strings.Contains(err.Error(), originFieldName) {
				t.Errorf("expected the error to name the base-URL field %q, got %q", originFieldName, err.Error())
			}
		})
	}
	if env.probeCalls != 0 {
		t.Errorf("validation failures must not probe, got %d calls", env.probeCalls)
	}
	rows, err := env.store.Connections().List(ctx, env.wsID)
	if err != nil || len(rows) != 0 {
		t.Errorf("validation failures must not store, got %d rows (%v)", len(rows), err)
	}
}

// The github recipe's endpoint/path constants resolve against a ghe.com-style
// data-residency origin now that the recipe declares the parameter natively
// (add-recipe-base-url tasks.md 4.2); a non-parametrized recipe resolves its
// declared endpoint byte-identically regardless of a submitted origin.
func TestConnect_ResolveRecipeBaseWithGithubShape(t *testing.T) {
	github := domain.RecipeByID("github")
	if github == nil {
		t.Fatal("the github recipe must be registered")
	}
	if github.OriginParam == nil {
		t.Fatal("expected github to declare an origin parameter (tasks.md 4.1)")
	}

	base, err := domain.ResolveRecipeBase(github, "https://copilot-api.acme.ghe.com")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if base != "https://copilot-api.acme.ghe.com/mcp/" {
		t.Errorf("expected the ghe.com origin joined with the github endpoint path, got %q", base)
	}

	// The empty submission falls back to the declared default, and the
	// non-parametrized figma recipe resolves byte-identically to its base.
	fallback, err := domain.ResolveRecipeBase(github, "")
	if err != nil || fallback != "https://api.githubcopilot.com/mcp/" {
		t.Errorf("expected the default-origin fallback, got %q (%v)", fallback, err)
	}
	figma := domain.RecipeByID("figma")
	plain, err := domain.ResolveRecipeBase(figma, "https://copilot-api.acme.ghe.com")
	if err != nil || plain != figma.BaseURL {
		t.Errorf("expected the non-parametrized base url byte-identical, got %q (%v)", plain, err)
	}
}

// Origin immutability (tasks.md 2.3): neither the refresh write-through nor
// the reauthorization round trip alters the stored origin, and the
// reauthorization probe dials the recipe's declared endpoint — for a
// non-parametrized recipe ResolveRecipeBase ignores the stored value
// byte-identically, and the lifecycle write never rewrites the column.
func TestConnect_OriginImmutableAcrossRefreshAndReauthorize(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")
	ctx := context.Background()

	storedOrigin := "https://atlassian.self.example.com"
	conn := seedOAuthConnection(t, env, "atlassian", func(c *domain.Connection) {
		c.Origin = storedOrigin
	})

	// Refresh-on-resolution: the write-through renews the token set and must
	// leave the origin untouched.
	value, err := env.svc.ResolveCredential(ctx, env.wsID, "atlassian")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if value != "Bearer at-new-1" {
		t.Errorf("expected the refreshed token value, got %q", value)
	}
	afterRefresh, err := env.store.Connections().Get(ctx, env.wsID, conn.ID)
	if err != nil {
		t.Fatalf("reload after refresh: %v", err)
	}
	if afterRefresh.Origin != storedOrigin {
		t.Errorf("expected the origin to survive the refresh write, got %q", afterRefresh.Origin)
	}

	// Reauthorization: replaces the token set in place; origin unchanged and
	// the probe candidate resolves from the recipe's declared endpoint.
	authorizeURL, err := env.svc.BeginReauthorize(ctx, env.wsID, env.userID, conn.ID)
	if err != nil {
		t.Fatalf("begin reauthorize: %v", err)
	}
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	env.transport.setResponse(200, `{"access_token":"at-renewed-2","refresh_token":"rt-renewed-2","expires_in":7200,"scope":"read:jira-work offline_access"}`)
	res := env.svc.HandleCallback(ctx, "code-immut", parsed.Query().Get("state"))
	if res.Status != services.OAuthCallbackConnected {
		t.Fatalf("reauthorize callback: %q (%s)", res.Status, res.Detail)
	}

	after, err := env.store.Connections().Get(ctx, env.wsID, conn.ID)
	if err != nil {
		t.Fatalf("reload after reauthorization: %v", err)
	}
	if after.Origin != storedOrigin {
		t.Errorf("expected the origin to survive reauthorization, got %q", after.Origin)
	}
	if len(env.probeCandidates) == 0 {
		t.Fatal("expected the reauthorization probe to run")
	}
	candidate := env.probeCandidates[len(env.probeCandidates)-1]
	atlassian := domain.RecipeByID("atlassian")
	if candidate.URL != atlassian.Endpoint {
		t.Errorf("expected the probe on the declared endpoint %q, got %q", atlassian.Endpoint, candidate.URL)
	}
}
