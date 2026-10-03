package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/authz"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// mustFallbackAuthorizer is the sync-on-check test authorizer over a store —
// what the router's test assembly builds; existing middleware suites pass it
// so roles created before or after construction both enforce.
func mustFallbackAuthorizer(t *testing.T, st store.Store) authz.Authorizer {
	t.Helper()
	az, err := newFallbackAuthorizer(context.Background(), st)
	if err != nil {
		t.Fatalf("build fallback authorizer: %v", err)
	}
	return az
}

// failingAuthorizer models an authorizer evaluation failure — the engine
// being down. Every check fails closed.
type failingAuthorizer struct{}

func (failingAuthorizer) Enforce(context.Context, string, string, string) (bool, error) {
	return false, context.DeadlineExceeded
}
func (failingAuthorizer) Sync(context.Context, *domain.Role) error { return nil }
func (failingAuthorizer) Reload(context.Context) error             { return nil }

// TestRequirePermission_UnsyncedPermissionDenied proves the engine is the
// single evaluation point (fix-role-permission-audit 1.4): a role whose row
// was synced to the engine denies a permission that was never part of it —
// the engine's unknown-permission outcome — and grants exactly what the row
// holds.
func TestRequirePermission_UnsyncedPermissionDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	issuer := services.NewJWTIssuer(services.JWTConfig{
		Secret: "test-jwt-secret-key-32-chars-length!",
		TTL:    time.Hour,
	})

	user := &domain.User{Email: "clerk@example.com", Name: "Clerk"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, err := issuer.Issue(ctx, user)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	ws := &domain.Workspace{Slug: "engine-ws", Name: "Engine WS", Timezone: "UTC"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	// A narrow custom role: workspace.read only. roles.write is in the
	// catalog but was NEVER synced for this role.
	clerkRole := &domain.Role{
		WorkspaceID: ws.ID,
		Name:        "Clerk",
		Permissions: []string{domain.WorkspaceRead},
	}
	if err := st.Roles().Create(ctx, clerkRole); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: clerkRole.ID}); err != nil {
		t.Fatalf("add member: %v", err)
	}

	// The RAW engine (no sync-on-check wrapper), built directly over the
	// store's role rows — what the composition root does at boot; the Sync
	// below is what the seeding seam does for a newborn workspace.
	engine, err := authz.NewCasbin(ctx, StoreRoleSource{Store: st})
	if err != nil {
		t.Fatalf("build engine: %v", err)
	}
	if err := engine.Sync(ctx, clerkRole); err != nil {
		t.Fatalf("sync role: %v", err)
	}

	mw := NewMiddlewares(st.Users(), st.Workspaces(), st.Members(), st.Roles(), issuer, engine)
	router := gin.New()
	wsGroup := router.Group("/workspaces/:ws")
	wsGroup.Use(mw.AuthRequired())
	wsGroup.Use(mw.RequireWorkspace("ws"))
	wsGroup.GET("/read", mw.RequirePermission(domain.WorkspaceRead), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	wsGroup.POST("/write", mw.RequirePermission(domain.AgentsWrite), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	t.Run("synced permission grants (200)", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/workspaces/engine-ws/read", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for the synced permission, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("permission never synced for the role is denied (403)", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/workspaces/engine-ws/write", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for the unsynced permission, got %d: %s", w.Code, w.Body.String())
		}
		var env ErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Error.Code != CodeForbidden {
			t.Errorf("expected code %q, got %q", CodeForbidden, env.Error.Code)
		}
	})
}

// TestRequirePermission_EngineErrorFailsClosed pins the fail-closed rule: an
// authorizer evaluation failure is a 500 with the standard envelope — never a
// fail-open grant.
func TestRequirePermission_EngineErrorFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	issuer := services.NewJWTIssuer(services.JWTConfig{
		Secret: "test-jwt-secret-key-32-chars-length!",
		TTL:    time.Hour,
	})

	user := &domain.User{Email: "closer@example.com", Name: "Closer"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, err := issuer.Issue(ctx, user)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	ws := &domain.Workspace{Slug: "closed-ws", Name: "Closed WS", Timezone: "UTC"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: "Admin", Permissions: domain.AdminPermissions}
	if err := st.Roles().Create(ctx, role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("add member: %v", err)
	}

	mw := NewMiddlewares(st.Users(), st.Workspaces(), st.Members(), st.Roles(), issuer, failingAuthorizer{})
	router := gin.New()
	wsGroup := router.Group("/workspaces/:ws")
	wsGroup.Use(mw.AuthRequired())
	wsGroup.Use(mw.RequireWorkspace("ws"))
	wsGroup.GET("/thing", mw.RequirePermission(domain.AgentsRead), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/closed-ws/thing", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (fail closed), got %d: %s", w.Code, w.Body.String())
	}
	var env ErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Error.Code != CodeInternal {
		t.Errorf("expected code %q, got %q", CodeInternal, env.Error.Code)
	}
}
