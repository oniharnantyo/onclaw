package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/bootstrap"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
)

func seedTestSuperadmin(t *testing.T, env *testEnv, email, name, password string) (*domain.User, string) {
	t.Helper()
	ctx := context.Background()

	cfg := bootstrap.SuperadminSeedConfig{
		Email:    email,
		Name:     name,
		Password: password,
	}
	user, err := bootstrap.New(env.store).SeedSuperadmin(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to seed superadmin %s: %v", email, err)
	}

	token, err := env.issuer.Issue(ctx, user)
	if err != nil {
		t.Fatalf("failed to issue token for superadmin %s: %v", email, err)
	}

	return user, token
}

// -----------------------------------------------------------------------------
// Master Protection Tests (Suspend, Rename, Slug)
// -----------------------------------------------------------------------------

func TestAdmin_MasterProtection(t *testing.T) {
	env := setupTestEnv(t)
	_, superToken := seedTestSuperadmin(t, env, "root@onclaw.local", "Root Admin", "supersecret123")

	t.Run("cannot suspend master workspace via admin disable endpoint (400)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/admin/workspaces/master/disable", superToken, nil)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
		var envErr server.ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &envErr)
		if envErr.Error.Code != server.CodeInvalidRequest {
			t.Errorf("expected code %q, got %q", server.CodeInvalidRequest, envErr.Error.Code)
		}
	})

	t.Run("superadmin renames master workspace via PATCH /workspaces/master (200)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/master", superToken, map[string]string{
			"name": "Master HQ",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("superadmin renames master workspace via admin route (200)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, "/api/v1/admin/workspaces/master", superToken, map[string]string{
			"name":     "Master Control",
			"timezone": "Asia/Jakarta",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		masterWs, err := env.store.Workspaces().BySlug(context.Background(), domain.MasterWorkspaceSlug)
		if err != nil {
			t.Fatalf("failed to load master workspace: %v", err)
		}
		if masterWs.Name != "Master Control" || masterWs.Timezone != "Asia/Jakarta" {
			t.Errorf("expected master renamed to %q with timezone %q, got %q / %q", "Master Control", "Asia/Jakarta", masterWs.Name, masterWs.Timezone)
		}
	})

	t.Run("master tenant Member (non-superadmin) cannot modify master via PATCH /workspaces/master (403)", func(t *testing.T) {
		member, memberToken := createTestUser(t, env, "membra@example.com", "Master Member", "password123")
		masterWs, _ := env.store.Workspaces().BySlug(context.Background(), domain.MasterWorkspaceSlug)
		masterMemberRole, _ := env.store.Roles().FindByName(context.Background(), masterWs.ID, domain.RoleMember)

		// Add normalUser to master workspace as Member (read only, no workspace.write)
		addMember(t, env, masterWs.ID, member.ID, masterMemberRole.ID)

		w := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/master", memberToken, map[string]string{
			"name": "Hostile Rename",
		})
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("cannot create workspace with reserved slug 'master' via user route (400)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces", superToken, map[string]string{
			"name": "Fake Master",
			"slug": "master",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("cannot create workspace with reserved slug 'master' via admin route (400)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/admin/workspaces", superToken, map[string]string{
			"name":        "Fake Master 2",
			"slug":        "master",
			"owner_email": "someone@example.com",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// -----------------------------------------------------------------------------
// Workspaces Management & Suspend/Restore Lifecycle Tests
// -----------------------------------------------------------------------------

func TestAdmin_WorkspacesLifecycle(t *testing.T) {
	env := setupTestEnv(t)
	_, superToken := seedTestSuperadmin(t, env, "root@onclaw.local", "Root Admin", "supersecret123")
	normalUser, normalToken := createTestUser(t, env, "alice@example.com", "Alice", "password123")

	t.Run("list all workspaces unscoped returns master and member counts", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, "/api/v1/admin/workspaces", superToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var res struct {
			Workspaces []handlers.AdminWorkspaceItem `json:"workspaces"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if len(res.Workspaces) != 1 {
			t.Fatalf("expected 1 workspace (master), got %d", len(res.Workspaces))
		}
		if res.Workspaces[0].Slug != "master" || !res.Workspaces[0].IsMaster || res.Workspaces[0].MemberCount != 1 {
			t.Errorf("unexpected master workspace item: %+v", res.Workspaces[0])
		}
	})

	t.Run("create workspace assigning existing user as Owner", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/admin/workspaces", superToken, map[string]string{
			"name":        "Engineering Team",
			"slug":        "eng-team",
			"timezone":    "America/Chicago",
			"owner_email": "alice@example.com",
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		var res struct {
			Workspace domain.Workspace `json:"workspace"`
			Role      domain.Role      `json:"role"`
			Member    domain.Member    `json:"member"`
			User      domain.User      `json:"user"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if res.Workspace.Slug != "eng-team" || res.Role.Name != domain.RoleOwner || !res.Role.IsOwner {
			t.Errorf("unexpected created workspace or role: %+v, %+v", res.Workspace, res.Role)
		}
		if res.User.ID != normalUser.ID || res.Member.UserID != normalUser.ID {
			t.Errorf("expected owner member to be Alice (%s), got %+v", normalUser.ID, res.Member)
		}
	})

	t.Run("create workspace with unknown email auto-creates passwordless user", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/admin/workspaces", superToken, map[string]string{
			"name":        "Marketing Team",
			"slug":        "mktg-team",
			"owner_email": "newmarketer@example.com",
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		createdUser, err := env.store.Users().ByEmail(context.Background(), "newmarketer@example.com")
		if err != nil {
			t.Fatalf("expected auto-created user in store: %v", err)
		}
		if createdUser.PasswordHash != nil {
			t.Errorf("expected auto-created user to have NULL password_hash")
		}
	})

	t.Run("suspend workspace -> member gets 403 on tenant routes -> restore -> member gets 200", func(t *testing.T) {
		// Alice can currently access eng-team
		w1 := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/eng-team", normalToken, nil)
		if w1.Code != http.StatusOK {
			t.Fatalf("expected 200 OK before suspension, got %d: %s", w1.Code, w1.Body.String())
		}

		// Superadmin suspends eng-team
		w2 := doRequest(env.router, http.MethodPost, "/api/v1/admin/workspaces/eng-team/disable", superToken, nil)
		if w2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on suspend, got %d: %s", w2.Code, w2.Body.String())
		}

		// Alice now gets 403 Forbidden
		w3 := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/eng-team", normalToken, nil)
		if w3.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden when workspace suspended, got %d: %s", w3.Code, w3.Body.String())
		}

		// Superadmin restores eng-team
		w4 := doRequest(env.router, http.MethodPost, "/api/v1/admin/workspaces/eng-team/enable", superToken, nil)
		if w4.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on restore, got %d: %s", w4.Code, w4.Body.String())
		}

		// Alice gets 200 OK again
		w5 := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/eng-team", normalToken, nil)
		if w5.Code != http.StatusOK {
			t.Fatalf("expected 200 OK after workspace restored, got %d: %s", w5.Code, w5.Body.String())
		}
	})
}

// -----------------------------------------------------------------------------
// Global User Management & Disable Tests
// -----------------------------------------------------------------------------

func TestAdmin_UsersLifecycle(t *testing.T) {
	env := setupTestEnv(t)
	_, superToken := seedTestSuperadmin(t, env, "root@onclaw.local", "Root Admin", "supersecret123")

	t.Run("admin lists all users", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, "/api/v1/admin/users", superToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Users []domain.User `json:"users"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if len(res.Users) != 1 {
			t.Fatalf("expected 1 user, got %d", len(res.Users))
		}
		if res.Users[0].MembershipCount != 1 {
			t.Errorf("expected superadmin user to have membership_count 1, got %d", res.Users[0].MembershipCount)
		}
	})

	t.Run("admin creates user with password, user can log in", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/admin/users", superToken, map[string]any{
			"email":    "carol@example.com",
			"name":     "Carol Developer",
			"password": "carol-password-123",
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		var res struct {
			User domain.User `json:"user"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.User.Email != "carol@example.com" || res.User.Name != "Carol Developer" {
			t.Errorf("unexpected created user: %+v", res.User)
		}

		// Carol logs in
		loginW := doRequest(env.router, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
			"email":    "carol@example.com",
			"password": "carol-password-123",
		})
		if loginW.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on Carol login, got %d: %s", loginW.Code, loginW.Body.String())
		}

		var loginRes struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(loginW.Body.Bytes(), &loginRes)
		carolToken := loginRes.Token

		// Carol accesses /auth/me
		meW := doRequest(env.router, http.MethodGet, "/api/v1/auth/me", carolToken, nil)
		if meW.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on Carol /auth/me, got %d: %s", meW.Code, meW.Body.String())
		}

		// Superadmin globally disables Carol
		disW := doRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/admin/users/%s/disable", res.User.ID), superToken, nil)
		if disW.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on disable, got %d: %s", disW.Code, disW.Body.String())
		}

		// Carol login is now rejected (401 uniform)
		loginW2 := doRequest(env.router, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
			"email":    "carol@example.com",
			"password": "carol-password-123",
		})
		if loginW2.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for disabled user, got %d: %s", loginW2.Code, loginW2.Body.String())
		}

		// Carol authenticated request with prior token is rejected (401)
		meW2 := doRequest(env.router, http.MethodGet, "/api/v1/auth/me", carolToken, nil)
		if meW2.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for disabled user token, got %d: %s", meW2.Code, meW2.Body.String())
		}

		// Superadmin enables Carol
		enW := doRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/admin/users/%s/enable", res.User.ID), superToken, nil)
		if enW.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on enable, got %d: %s", enW.Code, enW.Body.String())
		}

		// Carol can log in again
		loginW3 := doRequest(env.router, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
			"email":    "carol@example.com",
			"password": "carol-password-123",
		})
		if loginW3.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on Carol login after enable, got %d: %s", loginW3.Code, loginW3.Body.String())
		}
	})
}

// -----------------------------------------------------------------------------
// Superadmin Management & Last-Admin Guard Tests
// -----------------------------------------------------------------------------

func TestAdmin_SuperadminsAndLastAdminGuard(t *testing.T) {
	env := setupTestEnv(t)
	rootAdmin, rootToken := seedTestSuperadmin(t, env, "root@onclaw.local", "Root Admin", "supersecret123")
	userB, userBToken := createTestUser(t, env, "userb@example.com", "User B", "pwd123")

	t.Run("attempting to revoke only superadmin returns 409 last_owner_protected", func(t *testing.T) {
		w := doRequest(env.router, http.MethodDelete, fmt.Sprintf("/api/v1/admin/superadmins/%s", rootAdmin.ID), rootToken, nil)
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict, got %d: %s", w.Code, w.Body.String())
		}
		var envErr server.ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &envErr)
		if envErr.Error.Code != server.CodeLastOwnerProtected {
			t.Errorf("expected code %q, got %q", server.CodeLastOwnerProtected, envErr.Error.Code)
		}
	})

	t.Run("grant superadmin role to user B succeeds", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/admin/superadmins", rootToken, map[string]string{
			"user_id": userB.ID,
		})
		if w.Code != http.StatusCreated && w.Code != http.StatusOK {
			t.Fatalf("expected 201 Created or 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		// User B can now perform admin operations
		w2 := doRequest(env.router, http.MethodGet, "/api/v1/admin/workspaces", userBToken, nil)
		if w2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for new superadmin User B, got %d: %s", w2.Code, w2.Body.String())
		}
	})

	t.Run("granting superadmin to user B again returns 409 conflict", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/admin/superadmins", rootToken, map[string]string{
			"email": userB.Email,
		})
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict when granting superadmin to existing superadmin, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("revoking superadmin from user B when 2 superadmins exist succeeds", func(t *testing.T) {
		w := doRequest(env.router, http.MethodDelete, fmt.Sprintf("/api/v1/admin/superadmins/%s", userB.ID), rootToken, nil)
		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content on superadmin revoke, got %d: %s", w.Code, w.Body.String())
		}

		// User B can no longer access admin routes (gets 404 because not in master tenant)
		w2 := doRequest(env.router, http.MethodGet, "/api/v1/admin/workspaces", userBToken, nil)
		if w2.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found after superadmin revoked, got %d: %s", w2.Code, w2.Body.String())
		}
	})
}

// -----------------------------------------------------------------------------
// Superadmin No-Bypass & Non-Superadmin Guard Tests
// -----------------------------------------------------------------------------

func TestAdmin_SuperadminNoBypass_And_NonSuperadminGuards(t *testing.T) {
	env := setupTestEnv(t)
	_, superToken := seedTestSuperadmin(t, env, "root@onclaw.local", "Root Admin", "supersecret123")
	normalUser, normalToken := createTestUser(t, env, "normal@example.com", "Normal User", "pwd123")

	// Create a tenant workspace where normalUser is Owner, but superadmin is NOT a member
	tenantWs, ownerRole, _, _ := createTestWorkspaceWithRoles(t, env, "isolated-tenant", "Isolated Tenant")
	addMember(t, env, tenantWs.ID, normalUser.ID, ownerRole.ID)

	t.Run("ordinary user querying admin route gets 404 (not in master tenant)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, "/api/v1/admin/workspaces", normalToken, nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("master tenant Member (non-superadmin) gets 403 Forbidden on admin route", func(t *testing.T) {
		masterWs, _ := env.store.Workspaces().BySlug(context.Background(), domain.MasterWorkspaceSlug)
		masterMemberRole, _ := env.store.Roles().FindByName(context.Background(), masterWs.ID, domain.RoleMember)

		// Add normalUser to master workspace as Member (read only, no admin.*)
		addMember(t, env, masterWs.ID, normalUser.ID, masterMemberRole.ID)

		w := doRequest(env.router, http.MethodGet, "/api/v1/admin/workspaces", normalToken, nil)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("superadmin querying non-master tenant endpoint without membership gets 404 (no bypass)", func(t *testing.T) {
		// Superadmin tries to list members of isolated-tenant
		w := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/isolated-tenant/members", superToken, nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found (superadmin treated as normal user with no cross-tenant bypass), got %d: %s", w.Code, w.Body.String())
		}
	})
}

// -----------------------------------------------------------------------------
// Admin Workspace Patch Tests
// -----------------------------------------------------------------------------

func TestAdmin_WorkspacePatch(t *testing.T) {
	env := setupTestEnv(t)
	_, superToken := seedTestSuperadmin(t, env, "root@onclaw.local", "Root Admin", "supersecret123")
	normalUser, normalToken := createTestUser(t, env, "alice@example.com", "Alice", "password123")

	ws, ownerRole, _, _ := createTestWorkspaceWithRoles(t, env, "acme", "Acme")
	addMember(t, env, ws.ID, normalUser.ID, ownerRole.ID)

	t.Run("superadmin renames workspace and changes timezone (200 OK)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, "/api/v1/admin/workspaces/acme", superToken, map[string]string{
			"name":     "Acme Corporation",
			"timezone": "Asia/Jakarta",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var res struct {
			Workspace domain.Workspace `json:"workspace"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if res.Workspace.Name != "Acme Corporation" || res.Workspace.Timezone != "Asia/Jakarta" {
			t.Errorf("unexpected workspace patch response: %+v", res.Workspace)
		}

		// Subsequent list reflects the changes
		listW := doRequest(env.router, http.MethodGet, "/api/v1/admin/workspaces", superToken, nil)
		if listW.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on list, got %d: %s", listW.Code, listW.Body.String())
		}
		var listRes struct {
			Workspaces []handlers.AdminWorkspaceItem `json:"workspaces"`
		}
		_ = json.Unmarshal(listW.Body.Bytes(), &listRes)
		var found bool
		for _, item := range listRes.Workspaces {
			if item.Slug == "acme" {
				found = true
				if item.Name != "Acme Corporation" || item.Timezone != "Asia/Jakarta" {
					t.Errorf("expected updated name and timezone in list, got %+v", item)
				}
			}
		}
		if !found {
			t.Errorf("expected to find acme workspace in list")
		}
	})

	t.Run("patch workspace by ID succeeds", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/admin/workspaces/%s", ws.ID), superToken, map[string]string{
			"name": "Acme Global",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK when patching by ID, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("master workspace rename succeeds for superadmin (200 OK)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, "/api/v1/admin/workspaces/master", superToken, map[string]string{
			"name": "Renamed Master",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on master workspace patch, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("empty workspace name is refused (400 Bad Request)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, "/api/v1/admin/workspaces/acme", superToken, map[string]string{
			"name": "   ",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on empty name, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid timezone is refused (400 Bad Request)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, "/api/v1/admin/workspaces/acme", superToken, map[string]string{
			"timezone": "Invalid/Timezone_Name",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on invalid timezone, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("empty payload with no fields is refused (400 Bad Request)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, "/api/v1/admin/workspaces/acme", superToken, map[string]string{})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on empty payload, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("non-existent workspace returns 404 Not Found", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, "/api/v1/admin/workspaces/non-existent-ws", superToken, map[string]string{
			"name": "Doesn't Matter",
		})
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("non-superadmin caller gets 404 (not in master tenant)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, "/api/v1/admin/workspaces/acme", normalToken, map[string]string{
			"name": "Hacked Name",
		})
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for non-admin user, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// -----------------------------------------------------------------------------
// Admin Workspace Owner Transfer Tests (All 4 Spec Scenarios)
// -----------------------------------------------------------------------------

func TestAdmin_WorkspaceOwnerTransfer(t *testing.T) {
	env := setupTestEnv(t)
	_, superToken := seedTestSuperadmin(t, env, "root@onclaw.local", "Root Admin", "supersecret123")
	userA, _ := createTestUser(t, env, "usera@example.com", "User A", "pwd123")
	userB, _ := createTestUser(t, env, "userb@example.com", "User B", "pwd123")
	userC, _ := createTestUser(t, env, "userc@example.com", "User C", "pwd123")

	t.Run("Scenario 1: transfer ownership to existing Admin -> Admin becomes sole Owner, previous Owner demoted to Admin", func(t *testing.T) {
		ws, ownerRole, adminRole, _ := createTestWorkspaceWithRoles(t, env, "ws-swap-1", "Workspace Swap 1")
		addMember(t, env, ws.ID, userA.ID, ownerRole.ID)
		addMember(t, env, ws.ID, userB.ID, adminRole.ID)

		w := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/admin/workspaces/%s/owner", ws.Slug), superToken, map[string]string{
			"user_id": userB.ID,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on owner swap, got %d: %s", w.Code, w.Body.String())
		}

		// Verify User B is now Owner
		mB, err := env.store.Members().Get(context.Background(), ws.ID, userB.ID)
		if err != nil {
			t.Fatalf("failed to get member B: %v", err)
		}
		if mB.RoleID != ownerRole.ID {
			t.Errorf("expected User B to have Owner role %s, got %s", ownerRole.ID, mB.RoleID)
		}

		// Verify User A was demoted to Admin
		mA, err := env.store.Members().Get(context.Background(), ws.ID, userA.ID)
		if err != nil {
			t.Fatalf("failed to get member A: %v", err)
		}
		if mA.RoleID != adminRole.ID {
			t.Errorf("expected User A to be demoted to Admin role %s, got %s", adminRole.ID, mA.RoleID)
		}

		// Verify total member count remains 2 and exactly one owner exists
		members, _ := env.store.Members().ListForWorkspace(context.Background(), ws.ID)
		if len(members) != 2 {
			t.Errorf("expected 2 members, got %d", len(members))
		}
		ownerCount := 0
		for _, m := range members {
			if m.RoleID == ownerRole.ID {
				ownerCount++
			}
		}
		if ownerCount != 1 {
			t.Errorf("expected exactly 1 owner, got %d", ownerCount)
		}
	})

	t.Run("Scenario 2: transfer ownership to outsider -> user added as Owner, previous Owner demoted to Admin", func(t *testing.T) {
		ws, ownerRole, adminRole, _ := createTestWorkspaceWithRoles(t, env, "ws-swap-2", "Workspace Swap 2")
		addMember(t, env, ws.ID, userA.ID, ownerRole.ID)

		w := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/admin/workspaces/%s/owner", ws.Slug), superToken, map[string]string{
			"email": userC.Email,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on owner transfer to outsider, got %d: %s", w.Code, w.Body.String())
		}

		// Verify User C is now added as Owner
		mC, err := env.store.Members().Get(context.Background(), ws.ID, userC.ID)
		if err != nil {
			t.Fatalf("failed to get member C: %v", err)
		}
		if mC.RoleID != ownerRole.ID {
			t.Errorf("expected User C to have Owner role %s, got %s", ownerRole.ID, mC.RoleID)
		}

		// Verify User A was demoted to Admin
		mA, err := env.store.Members().Get(context.Background(), ws.ID, userA.ID)
		if err != nil {
			t.Fatalf("failed to get member A: %v", err)
		}
		if mA.RoleID != adminRole.ID {
			t.Errorf("expected User A to be demoted to Admin role %s, got %s", adminRole.ID, mA.RoleID)
		}

		// Verify exactly one owner exists
		members, _ := env.store.Members().ListForWorkspace(context.Background(), ws.ID)
		if len(members) != 2 {
			t.Errorf("expected 2 members, got %d", len(members))
		}
		ownerCount := 0
		for _, m := range members {
			if m.RoleID == ownerRole.ID {
				ownerCount++
			}
		}
		if ownerCount != 1 {
			t.Errorf("expected exactly 1 owner, got %d", ownerCount)
		}
	})

	t.Run("Scenario 3: multi-owner workspace transfer -> exactly one owner remains", func(t *testing.T) {
		ws, ownerRole, adminRole, _ := createTestWorkspaceWithRoles(t, env, "ws-swap-3", "Workspace Swap 3")
		addMember(t, env, ws.ID, userA.ID, ownerRole.ID)
		addMember(t, env, ws.ID, userB.ID, ownerRole.ID) // 2 owners

		// Transfer to User A
		w := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/admin/workspaces/%s/owner", ws.ID), superToken, map[string]string{
			"user_id": userA.ID,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		// User A is Owner
		mA, _ := env.store.Members().Get(context.Background(), ws.ID, userA.ID)
		if mA.RoleID != ownerRole.ID {
			t.Errorf("expected User A to be Owner, got %s", mA.RoleID)
		}

		// User B was demoted to Admin
		mB, _ := env.store.Members().Get(context.Background(), ws.ID, userB.ID)
		if mB.RoleID != adminRole.ID {
			t.Errorf("expected User B to be demoted to Admin, got %s", mB.RoleID)
		}

		members, _ := env.store.Members().ListForWorkspace(context.Background(), ws.ID)
		ownerCount := 0
		for _, m := range members {
			if m.RoleID == ownerRole.ID {
				ownerCount++
			}
		}
		if ownerCount != 1 {
			t.Errorf("expected exactly 1 owner, got %d", ownerCount)
		}
	})

	t.Run("Scenario 4: master workspace owner transfer refused (400 Bad Request)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, "/api/v1/admin/workspaces/master/owner", superToken, map[string]string{
			"user_id": userA.ID,
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on master owner transfer, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("transfer to unknown user returns 404 Not Found", func(t *testing.T) {
		ws, _, _, _ := createTestWorkspaceWithRoles(t, env, "ws-swap-4", "Workspace Swap 4")
		w := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/admin/workspaces/%s/owner", ws.Slug), superToken, map[string]string{
			"email": "ghost-user@example.com",
		})
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for unknown user, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// -----------------------------------------------------------------------------
// Admin Workspace Members Management Tests
// -----------------------------------------------------------------------------

func TestAdmin_WorkspaceMembers(t *testing.T) {
	env := setupTestEnv(t)
	_, superToken := seedTestSuperadmin(t, env, "root@onclaw.local", "Root Admin", "supersecret123")
	userA, _ := createTestUser(t, env, "usera@example.com", "User A", "pwd123")
	userB, _ := createTestUser(t, env, "userb@example.com", "User B", "pwd123")
	userC, _ := createTestUser(t, env, "userc@example.com", "User C", "pwd123")

	ws, ownerRole, adminRole, memberRole := createTestWorkspaceWithRoles(t, env, "target-tenant", "Target Tenant")
	addMember(t, env, ws.ID, userA.ID, ownerRole.ID)

	t.Run("Scenario: Superadmin lists outside tenant members", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/admin/workspaces/%s/members", ws.Slug), superToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var res struct {
			Members []handlers.MemberItemResponse `json:"members"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(res.Members) != 1 {
			t.Fatalf("expected 1 member, got %d", len(res.Members))
		}
		if res.Members[0].Email != "usera@example.com" || res.Members[0].RoleName != "Owner" {
			t.Errorf("unexpected member details: %+v", res.Members[0])
		}
	})

	t.Run("Scenario: Superadmin adds existing user to outside tenant with Admin role", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/admin/workspaces/%s/members", ws.Slug), superToken, map[string]string{
			"user_id":   userB.ID,
			"role_name": "Admin",
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		// Verify membership was created
		m, err := env.store.Members().Get(context.Background(), ws.ID, userB.ID)
		if err != nil {
			t.Fatalf("failed to get member: %v", err)
		}
		if m.RoleID != adminRole.ID {
			t.Errorf("expected Admin role %s, got %s", adminRole.ID, m.RoleID)
		}
	})

	t.Run("Scenario: Superadmin adds existing user to outside tenant with Member role by role_id", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/admin/workspaces/%s/members", ws.ID), superToken, map[string]string{
			"email":   userC.Email,
			"role_id": memberRole.ID,
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		m, err := env.store.Members().Get(context.Background(), ws.ID, userC.ID)
		if err != nil {
			t.Fatalf("failed to get member: %v", err)
		}
		if m.RoleID != memberRole.ID {
			t.Errorf("expected Member role %s, got %s", memberRole.ID, m.RoleID)
		}
	})

	t.Run("Scenario: Owner role not assignable via member add (400 Bad Request)", func(t *testing.T) {
		userD, _ := createTestUser(t, env, "userd@example.com", "User D", "pwd123")
		w := doRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/admin/workspaces/%s/members", ws.Slug), superToken, map[string]string{
			"user_id":   userD.ID,
			"role_name": "Owner",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request when assigning Owner role via members endpoint, got %d: %s", w.Code, w.Body.String())
		}

		w2 := doRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/admin/workspaces/%s/members", ws.Slug), superToken, map[string]string{
			"user_id": userD.ID,
			"role_id": ownerRole.ID,
		})
		if w2.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request when assigning ownerRole.ID via members endpoint, got %d: %s", w2.Code, w2.Body.String())
		}
	})

	t.Run("Scenario: Already a member returns 409 Conflict", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/admin/workspaces/%s/members", ws.Slug), superToken, map[string]string{
			"user_id":   userA.ID,
			"role_name": "Admin",
		})
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict for existing member, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Scenario: Unknown user returns 404 Not Found (no auto-provisioning)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/admin/workspaces/%s/members", ws.Slug), superToken, map[string]string{
			"email":     "nonexistent-user@example.com",
			"role_name": "Admin",
		})
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for unknown user, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// -----------------------------------------------------------------------------
// Admin Users is_superadmin Flag Tests
// -----------------------------------------------------------------------------

func TestAdmin_UsersSuperadminFlag(t *testing.T) {
	env := setupTestEnv(t)
	rootAdmin, rootToken := seedTestSuperadmin(t, env, "root@onclaw.local", "Root Admin", "supersecret123")
	userAlice, _ := createTestUser(t, env, "alice@example.com", "Alice", "pwd123")

	t.Run("listing flags superadmins correctly (true for superadmin, false for normal user)", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, "/api/v1/admin/users", rootToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var res struct {
			Users []domain.User `json:"users"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		for _, u := range res.Users {
			if u.ID == rootAdmin.ID {
				if !u.IsSuperadmin {
					t.Errorf("expected root admin %s to have is_superadmin=true, got false", u.Email)
				}
			} else if u.ID == userAlice.ID {
				if u.IsSuperadmin {
					t.Errorf("expected normal user %s to have is_superadmin=false, got true", u.Email)
				}
			}
		}
	})

	t.Run("promoting normal user to superadmin updates is_superadmin to true", func(t *testing.T) {
		// Grant superadmin to Alice
		wGrant := doRequest(env.router, http.MethodPost, "/api/v1/admin/superadmins", rootToken, map[string]string{
			"user_id": userAlice.ID,
		})
		if wGrant.Code != http.StatusCreated && wGrant.Code != http.StatusOK {
			t.Fatalf("expected 201/200 on grant superadmin, got %d: %s", wGrant.Code, wGrant.Body.String())
		}

		// List users again
		wList := doRequest(env.router, http.MethodGet, "/api/v1/admin/users", rootToken, nil)
		if wList.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on list, got %d: %s", wList.Code, wList.Body.String())
		}

		var res struct {
			Users []domain.User `json:"users"`
		}
		_ = json.Unmarshal(wList.Body.Bytes(), &res)
		for _, u := range res.Users {
			if u.ID == userAlice.ID {
				if !u.IsSuperadmin {
					t.Errorf("expected Alice to have is_superadmin=true after promotion, got false")
				}
			}
		}
	})

	t.Run("revoking superadmin role updates is_superadmin to false", func(t *testing.T) {
		// Revoke superadmin from Alice
		wRevoke := doRequest(env.router, http.MethodDelete, fmt.Sprintf("/api/v1/admin/superadmins/%s", userAlice.ID), rootToken, nil)
		if wRevoke.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content on revoke, got %d: %s", wRevoke.Code, wRevoke.Body.String())
		}

		// List users again
		wList := doRequest(env.router, http.MethodGet, "/api/v1/admin/users", rootToken, nil)
		if wList.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on list, got %d: %s", wList.Code, wList.Body.String())
		}

		var res struct {
			Users []domain.User `json:"users"`
		}
		_ = json.Unmarshal(wList.Body.Bytes(), &res)
		for _, u := range res.Users {
			if u.ID == userAlice.ID {
				if u.IsSuperadmin {
					t.Errorf("expected Alice to have is_superadmin=false after revocation, got true")
				}
			}
		}
	})
}
