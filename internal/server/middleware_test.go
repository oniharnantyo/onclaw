package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

func setupTestAuth(t *testing.T) (store.Store, services.TokenIssuer) {
	st := storefake.New()
	issuer := services.NewJWTIssuer(services.JWTConfig{
		Secret: "test-jwt-secret-key-32-chars-length!",
		TTL:    time.Hour,
	})
	return st, issuer
}

func TestAuthRequiredMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st, issuer := setupTestAuth(t)

	// Create an active user
	activeUser := &domain.User{
		Email: "active@example.com",
		Name:  "Active User",
	}
	if err := st.Users().Create(ctx, activeUser); err != nil {
		t.Fatalf("failed to create active user: %v", err)
	}

	// Create a disabled user
	disabledAt := time.Now().UTC()
	disabledUser := &domain.User{
		Email:      "disabled@example.com",
		Name:       "Disabled User",
		DisabledAt: &disabledAt,
	}
	if err := st.Users().Create(ctx, disabledUser); err != nil {
		t.Fatalf("failed to create disabled user: %v", err)
	}

	activeToken, err := issuer.Issue(ctx, activeUser)
	if err != nil {
		t.Fatalf("failed to issue active token: %v", err)
	}

	disabledToken, err := issuer.Issue(ctx, disabledUser)
	if err != nil {
		t.Fatalf("failed to issue disabled token: %v", err)
	}

	// Create a token for a deleted/non-existent user
	nonExistentUser := &domain.User{
		ID:    "non-existent-user-id",
		Email: "deleted@example.com",
	}
	nonExistentToken, err := issuer.Issue(ctx, nonExistentUser)
	if err != nil {
		t.Fatalf("failed to issue non-existent user token: %v", err)
	}

	// Expired token issuer
	expiredIssuer := services.NewJWTIssuer(services.JWTConfig{
		Secret: "test-jwt-secret-key-32-chars-length!",
		TTL:    -time.Hour,
	})
	expiredToken, err := expiredIssuer.Issue(ctx, activeUser)
	if err != nil {
		t.Fatalf("failed to issue expired token: %v", err)
	}

	mw := NewMiddlewares(st.Users(), st.Workspaces(), st.Members(), st.Roles(), issuer, mustFallbackAuthorizer(t, st))
	router := gin.New()
	router.Use(mw.AuthRequired())
	router.GET("/protected", func(c *gin.Context) {
		u := MustCurrentUser(c)
		c.JSON(http.StatusOK, gin.H{"user_id": u.ID, "email": u.Email})
	})

	tests := []struct {
		name           string
		authHeader     string
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "missing auth header",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   CodeUnauthenticated,
		},
		{
			name:           "invalid format - basic",
			authHeader:     "Basic 12345",
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   CodeUnauthenticated,
		},
		{
			name:           "invalid format - bearer without token",
			authHeader:     "Bearer ",
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   CodeUnauthenticated,
		},
		{
			name:           "invalid token signature",
			authHeader:     "Bearer invalid.token.string",
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   CodeUnauthenticated,
		},
		{
			name:           "expired token",
			authHeader:     "Bearer " + expiredToken,
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   CodeUnauthenticated,
		},
		{
			name:           "non-existent user in store",
			authHeader:     "Bearer " + nonExistentToken,
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   CodeUnauthenticated,
		},
		{
			name:           "disabled user",
			authHeader:     "Bearer " + disabledToken,
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   CodeUnauthenticated,
		},
		{
			name:           "valid active user",
			authHeader:     "Bearer " + activeToken,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			router.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Fatalf("expected status %d, got %d, body: %s", tc.expectedStatus, w.Code, w.Body.String())
			}

			if tc.expectedCode != "" {
				var env ErrorEnvelope
				if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
					t.Fatalf("failed to decode error envelope: %v", err)
				}
				if env.Error.Code != tc.expectedCode {
					t.Errorf("expected code %q, got %q", tc.expectedCode, env.Error.Code)
				}
			}
		})
	}
}

func TestRequireWorkspaceMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	st, issuer := setupTestAuth(t)

	// Users
	memberUser := &domain.User{Email: "member@example.com", Name: "Member"}
	nonMemberUser := &domain.User{Email: "nonmember@example.com", Name: "Non Member"}
	_ = st.Users().Create(ctx, memberUser)
	_ = st.Users().Create(ctx, nonMemberUser)

	memberToken, _ := issuer.Issue(ctx, memberUser)
	nonMemberToken, _ := issuer.Issue(ctx, nonMemberUser)

	// Workspaces
	activeWs := &domain.Workspace{Slug: "acme", Name: "Acme Corp", Timezone: "UTC"}
	_ = st.Workspaces().Create(ctx, activeWs)

	suspendedAt := time.Now().UTC()
	suspendedWs := &domain.Workspace{Slug: "suspended-ws", Name: "Suspended", Timezone: "UTC", DisabledAt: &suspendedAt}
	_ = st.Workspaces().Create(ctx, suspendedWs)

	// Roles
	adminRole := &domain.Role{
		WorkspaceID: activeWs.ID,
		Name:        "Admin",
		Permissions: domain.AdminPermissions,
	}
	_ = st.Roles().Create(ctx, adminRole)

	suspendedRole := &domain.Role{
		WorkspaceID: suspendedWs.ID,
		Name:        "Admin",
		Permissions: domain.AdminPermissions,
	}
	_ = st.Roles().Create(ctx, suspendedRole)

	// Memberships
	_ = st.Members().Add(ctx, &domain.Member{
		WorkspaceID: activeWs.ID,
		UserID:      memberUser.ID,
		RoleID:      adminRole.ID,
	})
	_ = st.Members().Add(ctx, &domain.Member{
		WorkspaceID: suspendedWs.ID,
		UserID:      memberUser.ID,
		RoleID:      suspendedRole.ID,
	})

	mw := NewMiddlewares(st.Users(), st.Workspaces(), st.Members(), st.Roles(), issuer, mustFallbackAuthorizer(t, st))
	router := gin.New()
	wsGroup := router.Group("/workspaces/:ws")
	wsGroup.Use(mw.AuthRequired())
	wsGroup.Use(mw.RequireWorkspace("ws"))
	wsGroup.GET("/details", func(c *gin.Context) {
		ws := MustCurrentWorkspace(c)
		m := MustCurrentMember(c)
		r := MustCurrentRole(c)
		c.JSON(http.StatusOK, gin.H{
			"ws_slug":   ws.Slug,
			"user_id":   m.UserID,
			"role_name": r.Name,
		})
	})

	tests := []struct {
		name           string
		token          string
		slug           string
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "unknown workspace returns 404",
			token:          memberToken,
			slug:           "unknown-workspace",
			expectedStatus: http.StatusNotFound,
			expectedCode:   CodeNotFound,
		},
		{
			name:           "non-member returns 404 (indistinguishable from unknown slug)",
			token:          nonMemberToken,
			slug:           "acme",
			expectedStatus: http.StatusNotFound,
			expectedCode:   CodeNotFound,
		},
		{
			name:           "suspended workspace returns 403",
			token:          memberToken,
			slug:           "suspended-ws",
			expectedStatus: http.StatusForbidden,
			expectedCode:   CodeForbidden,
		},
		{
			name:           "active member access returns 200",
			token:          memberToken,
			slug:           "acme",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/workspaces/"+tc.slug+"/details", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			router.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Fatalf("expected status %d, got %d, body: %s", tc.expectedStatus, w.Code, w.Body.String())
			}

			if tc.expectedCode != "" {
				var env ErrorEnvelope
				if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
					t.Fatalf("failed to decode error envelope: %v", err)
				}
				if env.Error.Code != tc.expectedCode {
					t.Errorf("expected code %q, got %q", tc.expectedCode, env.Error.Code)
				}
			}
		})
	}
}

func TestRequirePermissionMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	st, issuer := setupTestAuth(t)

	// Users
	ownerUser := &domain.User{Email: "owner@example.com", Name: "Owner"}
	adminUser := &domain.User{Email: "admin@example.com", Name: "Admin"}
	memberUser := &domain.User{Email: "member@example.com", Name: "Member"}
	_ = st.Users().Create(ctx, ownerUser)
	_ = st.Users().Create(ctx, adminUser)
	_ = st.Users().Create(ctx, memberUser)

	ownerToken, _ := issuer.Issue(ctx, ownerUser)
	adminToken, _ := issuer.Issue(ctx, adminUser)
	memberToken, _ := issuer.Issue(ctx, memberUser)

	ws := &domain.Workspace{Slug: "corp", Name: "Corp", Timezone: "UTC"}
	_ = st.Workspaces().Create(ctx, ws)

	ownerRole := &domain.Role{
		WorkspaceID: ws.ID,
		Name:        "Owner",
		IsOwner:     true,
		Permissions: domain.OwnerPermissions,
	}
	_ = st.Roles().Create(ctx, ownerRole)

	adminRole := &domain.Role{
		WorkspaceID: ws.ID,
		Name:        "Admin",
		Permissions: domain.AdminPermissions,
	}
	_ = st.Roles().Create(ctx, adminRole)

	memberRole := &domain.Role{
		WorkspaceID: ws.ID,
		Name:        "Member",
		Permissions: domain.MemberPermissions,
	}
	_ = st.Roles().Create(ctx, memberRole)

	_ = st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: ownerUser.ID, RoleID: ownerRole.ID})
	_ = st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: adminUser.ID, RoleID: adminRole.ID})
	_ = st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: memberUser.ID, RoleID: memberRole.ID})

	mw := NewMiddlewares(st.Users(), st.Workspaces(), st.Members(), st.Roles(), issuer, mustFallbackAuthorizer(t, st))
	router := gin.New()
	wsGroup := router.Group("/workspaces/:ws")
	wsGroup.Use(mw.AuthRequired())
	wsGroup.Use(mw.RequireWorkspace("ws"))

	// Route 1: workspace.read (Owner, Admin, Member have it)
	wsGroup.GET("/read", mw.RequirePermission(domain.WorkspaceRead), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Route 2: workspace.write (Owner, Admin have it; Member lacks it)
	wsGroup.POST("/write", mw.RequirePermission(domain.WorkspaceWrite), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Route 3: agents.write (Owner and Admin hold it; Member lacks it). The
	// roles.write case this route once pinned left the catalog with D5 — no
	// owner-only permission survives, so the route now pins the admin tier.
	wsGroup.POST("/agents-write", mw.RequirePermission(domain.AgentsWrite), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Route 4: multiple permissions required (gateways.write and workspace.write)
	wsGroup.POST("/multi-permission", mw.RequireAllPermissions(domain.GatewaysWrite, domain.WorkspaceWrite), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	tests := []struct {
		name           string
		method         string
		path           string
		token          string
		expectedStatus int
	}{
		{
			name:           "member can read",
			method:         http.MethodGet,
			path:           "/workspaces/corp/read",
			token:          memberToken,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "member cannot write (403)",
			method:         http.MethodPost,
			path:           "/workspaces/corp/write",
			token:          memberToken,
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "admin can write",
			method:         http.MethodPost,
			path:           "/workspaces/corp/write",
			token:          adminToken,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "admin can agents.write (200)",
			method:         http.MethodPost,
			path:           "/workspaces/corp/agents-write",
			token:          adminToken,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "owner can agents.write (200)",
			method:         http.MethodPost,
			path:           "/workspaces/corp/agents-write",
			token:          ownerToken,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "owner satisfies all permissions (200)",
			method:         http.MethodPost,
			path:           "/workspaces/corp/multi-permission",
			token:          ownerToken,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "member fails all permissions check (403)",
			method:         http.MethodPost,
			path:           "/workspaces/corp/multi-permission",
			token:          memberToken,
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			router.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Fatalf("expected status %d, got %d, body: %s", tc.expectedStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestRequireMasterWorkspaceMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	st, issuer := setupTestAuth(t)

	masterUser := &domain.User{Email: "master@example.com", Name: "Master User"}
	regularUser := &domain.User{Email: "regular@example.com", Name: "Regular User"}
	_ = st.Users().Create(ctx, masterUser)
	_ = st.Users().Create(ctx, regularUser)

	masterToken, _ := issuer.Issue(ctx, masterUser)
	regularToken, _ := issuer.Issue(ctx, regularUser)

	masterWs := &domain.Workspace{
		Slug:     domain.MasterWorkspaceSlug,
		Name:     "Master Workspace",
		Timezone: "UTC",
		IsMaster: true,
	}
	if err := st.Workspaces().Create(ctx, masterWs); err != nil {
		t.Fatalf("failed to create master workspace: %v", err)
	}

	masterRole := &domain.Role{
		WorkspaceID: masterWs.ID,
		Name:        "Master Admin",
		Permissions: domain.AdminPermissions,
	}
	_ = st.Roles().Create(ctx, masterRole)

	_ = st.Members().Add(ctx, &domain.Member{
		WorkspaceID: masterWs.ID,
		UserID:      masterUser.ID,
		RoleID:      masterRole.ID,
	})

	mw := NewMiddlewares(st.Users(), st.Workspaces(), st.Members(), st.Roles(), issuer, mustFallbackAuthorizer(t, st))
	router := gin.New()
	adminGroup := router.Group("/admin")
	adminGroup.Use(mw.AuthRequired())
	adminGroup.Use(mw.RequireMasterWorkspace())
	adminGroup.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	t.Run("master member succeeds (200)", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/ping", nil)
		req.Header.Set("Authorization", "Bearer "+masterToken)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("non-master member returns 404 (enumeration defense)", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/ping", nil)
		req.Header.Set("Authorization", "Bearer "+regularToken)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}
	})
}
