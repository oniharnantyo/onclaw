package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/auth"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/storage"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

type testEnv struct {
	store   store.Store
	storage storage.Storage
	issuer  auth.TokenIssuer
	service auth.Service
	router  *gin.Engine
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	stor := storagefake.New()
	issuer := auth.NewJWTIssuer(auth.JWTConfig{
		Secret: "test-secret-key-that-is-at-least-32-chars-long!",
		TTL:    time.Hour * 24,
	})
	authSvc := auth.NewService(st, issuer, nil)

	r := server.NewRouter(server.RouterOptions{
		Store:   st,
		Storage: stor,
		Issuer:  issuer,
		Auth:    authSvc,
	})

	return &testEnv{
		store:   st,
		storage: stor,
		issuer:  issuer,
		service: authSvc,
		router:  r,
	}
}

func createTestUser(t *testing.T, env *testEnv, email, name, password string) (*domain.User, string) {
	t.Helper()
	ctx := context.Background()

	var hash *string
	if password != "" {
		h, err := auth.NewPasswordHasher().Hash(password)
		if err != nil {
			t.Fatalf("failed to hash password: %v", err)
		}
		hash = &h
	}

	u := &domain.User{
		Email:        email,
		Name:         name,
		PasswordHash: hash,
	}
	if err := env.store.Users().Create(ctx, u); err != nil {
		t.Fatalf("failed to create user %s: %v", email, err)
	}

	token, err := env.issuer.Issue(ctx, u)
	if err != nil {
		t.Fatalf("failed to issue token for %s: %v", email, err)
	}

	return u, token
}

func createTestWorkspaceWithRoles(t *testing.T, env *testEnv, slug, name string) (*domain.Workspace, *domain.Role, *domain.Role, *domain.Role) {
	t.Helper()
	ctx := context.Background()

	ws := &domain.Workspace{
		Slug:     slug,
		Name:     name,
		Timezone: "UTC",
	}
	if err := env.store.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("failed to create workspace %s: %v", slug, err)
	}

	ownerRole := &domain.Role{
		WorkspaceID: ws.ID,
		Name:        domain.RoleOwner,
		IsOwner:     true,
		Permissions: domain.OwnerPermissions,
		BuiltIn:     true,
	}
	if err := env.store.Roles().Create(ctx, ownerRole); err != nil {
		t.Fatalf("failed to create owner role: %v", err)
	}

	adminRole := &domain.Role{
		WorkspaceID: ws.ID,
		Name:        domain.RoleAdmin,
		IsOwner:     false,
		Permissions: domain.AdminPermissions,
		BuiltIn:     true,
	}
	if err := env.store.Roles().Create(ctx, adminRole); err != nil {
		t.Fatalf("failed to create admin role: %v", err)
	}

	memberRole := &domain.Role{
		WorkspaceID: ws.ID,
		Name:        domain.RoleMember,
		IsOwner:     false,
		Permissions: domain.MemberPermissions,
		BuiltIn:     true,
	}
	if err := env.store.Roles().Create(ctx, memberRole); err != nil {
		t.Fatalf("failed to create member role: %v", err)
	}

	return ws, ownerRole, adminRole, memberRole
}

func addMember(t *testing.T, env *testEnv, wsID, userID, roleID string) *domain.Member {
	t.Helper()
	ctx := context.Background()

	m := &domain.Member{
		WorkspaceID: wsID,
		UserID:      userID,
		RoleID:      roleID,
	}
	if err := env.store.Members().Add(ctx, m); err != nil {
		t.Fatalf("failed to add member: %v", err)
	}
	return m
}

func doRequest(r *gin.Engine, method, path, token string, body any) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if body != nil {
		jsonBytes, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(jsonBytes)
	}

	req := httptest.NewRequest(method, path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// -----------------------------------------------------------------------------
// Auth Endpoint Tests
// -----------------------------------------------------------------------------

func TestAuthLogin(t *testing.T) {
	env := setupTestEnv(t)
	createTestUser(t, env, "alice@example.com", "Alice", "correct-password-123")

	// Disabled user
	disabledAt := time.Now().UTC()
	uDisabled := &domain.User{
		Email:      "disabled@example.com",
		Name:       "Disabled",
		DisabledAt: &disabledAt,
	}
	h, _ := auth.NewPasswordHasher().Hash("some-password")
	uDisabled.PasswordHash = &h
	_ = env.store.Users().Create(context.Background(), uDisabled)

	// Passwordless user
	uPasswordless := &domain.User{
		Email: "passwordless@example.com",
		Name:  "Passwordless",
	}
	_ = env.store.Users().Create(context.Background(), uPasswordless)

	t.Run("successful login with flat credentials", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
			"email":    "alice@example.com",
			"password": "correct-password-123",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Token string      `json:"token"`
			User  domain.User `json:"user"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if res.Token == "" || res.User.Email != "alice@example.com" {
			t.Errorf("unexpected response: %+v", res)
		}
	})

	t.Run("successful login with nested credentials", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"provider": "password",
			"credentials": map[string]string{
				"email":    "alice@example.com",
				"password": "correct-password-123",
			},
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
	})

	uniform401Cases := []struct {
		name     string
		email    string
		password string
	}{
		{"wrong password", "alice@example.com", "wrong-password"},
		{"unknown email", "unknown@example.com", "any-password"},
		{"disabled user", "disabled@example.com", "some-password"},
		{"passwordless user", "passwordless@example.com", "some-password"},
	}

	for _, tc := range uniform401Cases {
		t.Run("uniform 401 on "+tc.name, func(t *testing.T) {
			w := doRequest(env.router, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
				"email":    tc.email,
				"password": tc.password,
			})
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401 Unauthorized, got %d: %s", w.Code, w.Body.String())
			}
			var envErr server.ErrorEnvelope
			if err := json.Unmarshal(w.Body.Bytes(), &envErr); err != nil {
				t.Fatalf("failed to decode error envelope: %v", err)
			}
			if envErr.Error.Code != server.CodeUnauthenticated {
				t.Errorf("expected code %q, got %q", server.CodeUnauthenticated, envErr.Error.Code)
			}
		})
	}

	t.Run("unknown provider returns 400", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"provider": "nonexistent-sso",
			"credentials": map[string]string{
				"email": "alice@example.com",
			},
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestAuthLogout(t *testing.T) {
	env := setupTestEnv(t)
	_, token := createTestUser(t, env, "alice@example.com", "Alice", "password123")

	w := doRequest(env.router, http.MethodPost, "/api/v1/auth/logout", token, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthMe(t *testing.T) {
	env := setupTestEnv(t)
	user, token := createTestUser(t, env, "alice@example.com", "Alice", "password123")

	ws1, oRole1, _, _ := createTestWorkspaceWithRoles(t, env, "ws-one", "Workspace One")
	ws2, _, _, mRole2 := createTestWorkspaceWithRoles(t, env, "ws-two", "Workspace Two")

	addMember(t, env, ws1.ID, user.ID, oRole1.ID)
	addMember(t, env, ws2.ID, user.ID, mRole2.ID)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, "/api/v1/auth/me", "", nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})

	t.Run("returns user and all workspace memberships", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, "/api/v1/auth/me", token, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var res struct {
			User        domain.User         `json:"user"`
			Memberships []domain.MemberView `json:"memberships"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if res.User.Email != "alice@example.com" {
			t.Errorf("expected user email alice@example.com, got %s", res.User.Email)
		}
		if len(res.Memberships) != 2 {
			t.Fatalf("expected 2 memberships, got %d", len(res.Memberships))
		}
	})
}

// -----------------------------------------------------------------------------
// Workspaces Endpoint Tests
// -----------------------------------------------------------------------------

func TestWorkspacesListAndCreate(t *testing.T) {
	env := setupTestEnv(t)
	user, token := createTestUser(t, env, "alice@example.com", "Alice", "password123")

	t.Run("create workspace success with built-in roles and owner assignment", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces", token, map[string]string{
			"name":     "Acme Corp",
			"slug":     "acme-corp",
			"timezone": "America/New_York",
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		var res struct {
			Workspace domain.Workspace `json:"workspace"`
			Role      domain.Role      `json:"role"`
			Member    domain.Member    `json:"member"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}

		if res.Workspace.Slug != "acme-corp" || res.Workspace.Name != "Acme Corp" {
			t.Errorf("unexpected workspace in response: %+v", res.Workspace)
		}
		if !res.Role.IsOwner || res.Role.Name != domain.RoleOwner {
			t.Errorf("expected Owner role, got %+v", res.Role)
		}
		if res.Member.UserID != user.ID {
			t.Errorf("expected creator member user_id %s, got %s", user.ID, res.Member.UserID)
		}

		// Check roles in store
		roles, err := env.store.Roles().ListForWorkspace(context.Background(), res.Workspace.ID)
		if err != nil || len(roles) != 3 {
			t.Fatalf("expected 3 built-in roles in store, got %d (err: %v)", len(roles), err)
		}
	})

	t.Run("create duplicate slug returns 409 conflict", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces", token, map[string]string{
			"name": "Acme Copy",
			"slug": "acme-corp",
		})
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("create invalid slug returns 400", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces", token, map[string]string{
			"name": "Invalid Slug",
			"slug": "INVALID_SLUG!",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("create reserved slug returns 400", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces", token, map[string]string{
			"name": "Master Workspace",
			"slug": "master",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("list workspaces returns user workspaces", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, "/api/v1/workspaces", token, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Workspaces []domain.MemberView `json:"workspaces"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}
		if len(res.Workspaces) != 1 || res.Workspaces[0].WorkspaceSlug != "acme-corp" {
			t.Errorf("unexpected list response: %+v", res)
		}
	})
}

func TestWorkspaceGetAndPatch(t *testing.T) {
	env := setupTestEnv(t)
	ownerUser, ownerToken := createTestUser(t, env, "owner@example.com", "Owner", "pwd")
	memberUser, memberToken := createTestUser(t, env, "member@example.com", "Member", "pwd")
	nonMemberUser, nonMemberToken := createTestUser(t, env, "nonmember@example.com", "Non Member", "pwd")
	_ = nonMemberUser

	ws, ownerRole, _, memberRole := createTestWorkspaceWithRoles(t, env, "my-team", "My Team")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)

	t.Run("non-member gets 404 (indistinguishable from unknown slug)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/my-team", nonMemberToken, nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", w.Code)
		}
	})

	t.Run("unknown slug gets 404", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/unknown-slug", ownerToken, nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", w.Code)
		}
	})

	t.Run("member gets workspace details", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/my-team", memberToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Workspace domain.Workspace `json:"workspace"`
			Role      domain.Role      `json:"role"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}
		if res.Workspace.Slug != "my-team" || res.Role.Name != domain.RoleMember {
			t.Errorf("unexpected get response: %+v", res)
		}
	})

	t.Run("member without workspace.write cannot PATCH (403)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/my-team", memberToken, map[string]string{
			"name": "New Team Name",
		})
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d", w.Code)
		}
	})

	t.Run("owner can PATCH workspace name and timezone", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/my-team", ownerToken, map[string]string{
			"name":     "Renamed Team",
			"timezone": "Europe/London",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Workspace domain.Workspace `json:"workspace"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}
		if res.Workspace.Name != "Renamed Team" || res.Workspace.Timezone != "Europe/London" || res.Workspace.Slug != "my-team" {
			t.Errorf("unexpected updated workspace: %+v", res.Workspace)
		}
	})
}

// -----------------------------------------------------------------------------
// Roles Endpoint Tests
// -----------------------------------------------------------------------------

func TestRolesList(t *testing.T) {
	env := setupTestEnv(t)
	memberUser, memberToken := createTestUser(t, env, "member@example.com", "Member", "pwd")
	ws, _, _, memberRole := createTestWorkspaceWithRoles(t, env, "roles-ws", "Roles WS")
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)

	w := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/roles-ws/roles", memberToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Roles []domain.Role `json:"roles"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode roles: %v", err)
	}

	if len(res.Roles) != 3 {
		t.Fatalf("expected 3 roles, got %d", len(res.Roles))
	}
}

// -----------------------------------------------------------------------------
// Members Endpoint Tests (Guards, Demotions, Last-Owner Protection)
// -----------------------------------------------------------------------------

func TestMembersLifecycleAndGuards(t *testing.T) {
	env := setupTestEnv(t)
	ownerUser, ownerToken := createTestUser(t, env, "owner@example.com", "Owner", "pwd")
	adminUser, adminToken := createTestUser(t, env, "admin@example.com", "Admin", "pwd")
	memberUser, memberToken := createTestUser(t, env, "member@example.com", "Member", "pwd")

	ws, ownerRole, adminRole, memberRole := createTestWorkspaceWithRoles(t, env, "team-guards", "Team Guards")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, adminUser.ID, adminRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)

	t.Run("list members returns all joined members", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/team-guards/members", memberToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Members []handlers.MemberItemResponse `json:"members"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if len(res.Members) != 3 {
			t.Fatalf("expected 3 members, got %d", len(res.Members))
		}
		for _, m := range res.Members {
			if m.Invited {
				t.Errorf("expected member %s with password to have Invited == false", m.Email)
			}
		}
	})

	t.Run("add member with existing account by email", func(t *testing.T) {
		existingUser, _ := createTestUser(t, env, "existing@example.com", "Existing", "pwd")

		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/team-guards/members", adminToken, map[string]string{
			"email":   "existing@example.com",
			"role_id": memberRole.ID,
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		m, err := env.store.Members().Get(context.Background(), ws.ID, existingUser.ID)
		if err != nil || m.RoleID != memberRole.ID {
			t.Fatalf("expected membership created, got %v (err: %v)", m, err)
		}
	})

	t.Run("add member with new email auto-creates passwordless user", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/team-guards/members", adminToken, map[string]string{
			"email":   "brandnew@example.com",
			"role_id": memberRole.ID,
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		newUser, err := env.store.Users().ByEmail(context.Background(), "brandnew@example.com")
		if err != nil {
			t.Fatalf("expected auto-created user in store: %v", err)
		}
		if newUser.PasswordHash != nil {
			t.Errorf("auto-created user should have NULL password_hash")
		}

		wList := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/team-guards/members", adminToken, nil)
		var listRes struct {
			Members []handlers.MemberItemResponse `json:"members"`
		}
		_ = json.Unmarshal(wList.Body.Bytes(), &listRes)
		var brandnewMember *handlers.MemberItemResponse
		for _, m := range listRes.Members {
			if m.Email == "brandnew@example.com" {
				mCopy := m
				brandnewMember = &mCopy
				break
			}
		}
		if brandnewMember == nil || !brandnewMember.Invited {
			t.Errorf("expected auto-created passwordless member to have Invited: true, got %+v", brandnewMember)
		}
	})

	t.Run("add already existing member returns 409 conflict", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/team-guards/members", adminToken, map[string]string{
			"email":   "member@example.com",
			"role_id": memberRole.ID,
		})
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("admin attempting to assign Owner role returns 403 (CanAssign check)", func(t *testing.T) {
		newU, _ := createTestUser(t, env, "candidate@example.com", "Candidate", "pwd")
		_ = newU

		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/team-guards/members", adminToken, map[string]string{
			"email":   "candidate@example.com",
			"role_id": ownerRole.ID,
		})
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("admin attempting to modify Admin peer returns 403 (CanEdit check)", func(t *testing.T) {
		peerAdmin, _ := createTestUser(t, env, "admin2@example.com", "Admin 2", "pwd")
		addMember(t, env, ws.ID, peerAdmin.ID, adminRole.ID)

		w := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/team-guards/members/%s", peerAdmin.ID), adminToken, map[string]string{
			"role_id": memberRole.ID,
		})
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden on peer edit, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("admin can promote Member to Admin (strict subset)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/team-guards/members/%s", memberUser.ID), adminToken, map[string]string{
			"role_id": adminRole.ID,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("owner demoting the last owner returns 409 last_owner_protected", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/team-guards/members/%s", ownerUser.ID), ownerToken, map[string]string{
			"role_id": adminRole.ID,
		})
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict, got %d: %s", w.Code, w.Body.String())
		}
		var envErr server.ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &envErr)
		if envErr.Error.Code != server.CodeLastOwnerProtected {
			t.Errorf("expected code %q, got %q", server.CodeLastOwnerProtected, envErr.Error.Code)
		}
	})

	t.Run("owner demoting an owner when a second owner exists succeeds", func(t *testing.T) {
		secondOwner, _ := createTestUser(t, env, "owner2@example.com", "Owner 2", "pwd")
		addMember(t, env, ws.ID, secondOwner.ID, ownerRole.ID)

		w := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/team-guards/members/%s", secondOwner.ID), ownerToken, map[string]string{
			"role_id": adminRole.ID,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("admin removing another admin returns 403 (CanEdit check)", func(t *testing.T) {
		peerAdmin, _ := env.store.Users().ByEmail(context.Background(), "admin2@example.com")
		w2 := doRequest(env.router, http.MethodDelete, fmt.Sprintf("/api/v1/workspaces/team-guards/members/%s", peerAdmin.ID), adminToken, nil)
		if w2.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d: %s", w2.Code, w2.Body.String())
		}
	})

	t.Run("last owner cannot leave (delete self)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodDelete, fmt.Sprintf("/api/v1/workspaces/team-guards/members/%s", ownerUser.ID), ownerToken, nil)
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("non-owner member can leave (delete self)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodDelete, fmt.Sprintf("/api/v1/workspaces/team-guards/members/%s", adminUser.ID), adminToken, nil)
		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content, got %d: %s", w.Code, w.Body.String())
		}
		// Confirm membership is deleted
		_, err := env.store.Members().Get(context.Background(), ws.ID, adminUser.ID)
		if err == nil {
			t.Fatalf("expected membership to be removed")
		}
	})
}

// -----------------------------------------------------------------------------
// Users & Avatar Tests
// -----------------------------------------------------------------------------

func TestUserPatchMeAndAvatar(t *testing.T) {
	env := setupTestEnv(t)
	user, token := createTestUser(t, env, "bob@example.com", "Bob", "pwd")
	_ = user

	t.Run("PATCH /users/me updates name", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, "/api/v1/users/me", token, map[string]string{
			"name": "Robert",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			User domain.User `json:"user"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.User.Name != "Robert" {
			t.Errorf("expected name Robert, got %s", res.User.Name)
		}
	})

	t.Run("PATCH /users/me empty name returns 400", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, "/api/v1/users/me", token, map[string]string{
			"name": "   ",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("POST /users/me/avatar invalid file type (text file masquerading as png) returns 400", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "fake.png")
		_, _ = part.Write([]byte("this is plain text pretending to be png"))
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/avatar", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("POST /users/me/avatar valid PNG upload succeeds and sets avatar_url", func(t *testing.T) {
		// Minimal valid 1x1 PNG bytes
		pngBytes := []byte{
			0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
			0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
			0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
			0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
			0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
			0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
		}

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "avatar.png")
		_, _ = part.Write(pngBytes)
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/avatar", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var res struct {
			AvatarURL string      `json:"avatar_url"`
			User      domain.User `json:"user"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode avatar response: %v", err)
		}

		if res.AvatarURL == "" || res.User.AvatarKey == nil {
			t.Fatalf("expected avatar url and key in response: %+v", res)
		}

		// Verify capability serving of this avatar
		key := *res.User.AvatarKey
		serveReq := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+key, nil)
		serveW := httptest.NewRecorder()
		env.router.ServeHTTP(serveW, serveReq)

		if serveW.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on file serve, got %d", serveW.Code)
		}
		if serveW.Header().Get("Cache-Control") == "" {
			t.Errorf("expected Cache-Control header on served file")
		}
		if serveW.Header().Get("Content-Type") != "image/png" {
			t.Errorf("expected Content-Type image/png, got %s", serveW.Header().Get("Content-Type"))
		}
	})

	t.Run("POST /users/me/avatar oversized (>2MB) returns 413", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "huge.png")
		// Write 2MB + 10 bytes
		hugeData := make([]byte, 2*1024*1024+10)
		_, _ = part.Write(hugeData)
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/avatar", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)

		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413 Payload Too Large, got %d: %s", w.Code, w.Body.String())
		}
		var errEnv server.ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &errEnv)
		if errEnv.Error.Code != server.CodePayloadTooLarge {
			t.Errorf("expected error code %q, got %q", server.CodePayloadTooLarge, errEnv.Error.Code)
		}
	})

	t.Run("PATCH /users/me clear avatar sets avatar_key and avatar_url to nil", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, "/api/v1/users/me", token, map[string]any{
			"avatar": nil,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			User domain.User `json:"user"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.User.AvatarKey != nil || res.User.AvatarURL != nil {
			t.Errorf("expected avatar_key and avatar_url to be nil, got key=%v, url=%v", res.User.AvatarKey, res.User.AvatarURL)
		}
	})
}

// -----------------------------------------------------------------------------
// Capability File Serving Tests
// -----------------------------------------------------------------------------

func TestCapabilityFileServing(t *testing.T) {
	env := setupTestEnv(t)

	ctx := context.Background()
	testData := []byte("hello-capability-file-content")
	_ = env.storage.Put(ctx, "abc123key", bytes.NewReader(testData), int64(len(testData)), "text/plain")

	t.Run("serves public file under /api/v1/files/:key without auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/files/abc123key", nil)
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		if w.Body.String() != "hello-capability-file-content" {
			t.Errorf("unexpected body: %s", w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Errorf("unexpected Cache-Control: %s", w.Header().Get("Cache-Control"))
		}
	})

	t.Run("serves public file under /files/:key without auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/files/abc123key", nil)
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("non-existent file returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/files/non-existent-key", nil)
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", w.Code)
		}
	})
}
