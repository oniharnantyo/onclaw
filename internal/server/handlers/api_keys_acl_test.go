package handlers_test

// API-key creator symmetry tests (fix-role-permission-audit 4.2, design D7):
// the creating member lists and revokes their own keys without
// workspace.write; other members' keys need workspace.write; unknown keys
// ride not-found. Exchange (member-level minting) is untouched.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

type apiKeyEnv struct {
	st     store.Store
	router *gin.Engine
	ws     *domain.Workspace
	owner  *domain.User
	member *domain.User
}

func newAPIKeyEnv(t *testing.T) apiKeyEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	ws := &domain.Workspace{Name: "Keys WS", Slug: "keys"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	owner := &domain.User{Email: "keys-owner@example.com", Name: "Owner"}
	member := &domain.User{Email: "keys-member@example.com", Name: "Member"}
	for _, u := range []*domain.User{owner, member} {
		if err := st.Users().Create(ctx, u); err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	ownerRole := &domain.Role{WorkspaceID: ws.ID, Name: domain.RoleOwner, IsOwner: true, Permissions: domain.OwnerPermissions, BuiltIn: true}
	memberRole := &domain.Role{WorkspaceID: ws.ID, Name: domain.RoleMember, Permissions: domain.MemberPermissions, BuiltIn: true}
	for _, role := range []*domain.Role{ownerRole, memberRole} {
		if err := st.Roles().Create(ctx, role); err != nil {
			t.Fatalf("seed role: %v", err)
		}
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: owner.ID, RoleID: ownerRole.ID}); err != nil {
		t.Fatalf("seed owner membership: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: member.ID, RoleID: memberRole.ID}); err != nil {
		t.Fatalf("seed member membership: %v", err)
	}

	az, err := newTestAuthorizer(ctx, st)
	if err != nil {
		t.Fatalf("build test authorizer: %v", err)
	}

	roleByUser := map[string]*domain.Role{owner.ID: ownerRole, member.ID: memberRole}
	keyH := handlers.NewAPIKeysHandlers(services.NewAPIKeyService(st.APIKeys()), az)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		user := member
		if c.GetHeader("X-Test-User") == "owner" {
			user = owner
		}
		c.Set(handlers.UserContextKey, user)
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.MemberContextKey, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: roleByUser[user.ID].ID, Role: roleByUser[user.ID]})
		c.Set(handlers.RoleContextKey, roleByUser[user.ID])
		c.Next()
	})
	r.GET("/api-keys", keyH.ListAPIKeys)
	r.POST("/api-keys", keyH.CreateAPIKey)
	r.DELETE("/api-keys/:id", keyH.RevokeAPIKey)

	return apiKeyEnv{st: st, router: r, ws: ws, owner: owner, member: member}
}

// mintKey creates one key through the handler itself (the creator-scoped
// surface under test) and returns its display entity.
func mintKey(t *testing.T, env apiKeyEnv, asWho, name string) domain.WorkspaceAPIKey {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	req := httptest.NewRequest(http.MethodPost, "/api-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", asWho)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("mint key as %s: status = %d, body = %s", asWho, rec.Code, rec.Body.String())
	}
	var res struct {
		APIKey domain.WorkspaceAPIKey `json:"api_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode mint response: %v", err)
	}
	return res.APIKey
}

func apiKeyList(t *testing.T, env apiKeyEnv, asWho string) []domain.WorkspaceAPIKey {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api-keys", nil)
	req.Header.Set("X-Test-User", asWho)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list keys as %s: status = %d, body = %s", asWho, rec.Code, rec.Body.String())
	}
	var res struct {
		APIKeys []domain.WorkspaceAPIKey `json:"api_keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	return res.APIKeys
}

func revokeKey(env apiKeyEnv, asWho, keyID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/api-keys/"+keyID, nil)
	req.Header.Set("X-Test-User", asWho)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}

func TestAPIKeys_MemberListsOwnKeysOnly(t *testing.T) {
	env := newAPIKeyEnv(t)

	mintKey(t, env, "owner", "admin key")
	memberKey := mintKey(t, env, "member", "chat key")

	// The member's listing lens: only the key they created, never another
	// member's (workspace.write holders see all).
	got := apiKeyList(t, env, "member")
	if len(got) != 1 || got[0].ID != memberKey.ID {
		t.Fatalf("member list = %+v, want only their own key %s", got, memberKey.ID)
	}

	// The owner (workspace.write) sees every workspace key.
	got = apiKeyList(t, env, "owner")
	if len(got) != 2 {
		t.Fatalf("owner list = %d keys, want both", len(got))
	}
}

func TestAPIKeys_MemberRevokesOwnKey(t *testing.T) {
	env := newAPIKeyEnv(t)
	memberKey := mintKey(t, env, "member", "chat key")

	rec := revokeKey(env, "member", memberKey.ID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("member revoke own key: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	keys := apiKeyList(t, env, "member")
	if len(keys) != 1 || keys[0].RevokedAt == nil {
		t.Fatalf("revoked key = %+v, want revoked_at set", keys)
	}
}

func TestAPIKeys_MemberCannotRevokeOthersKey(t *testing.T) {
	env := newAPIKeyEnv(t)
	ownerKey := mintKey(t, env, "owner", "admin key")

	rec := revokeKey(env, "member", ownerKey.ID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member revoke other's key: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if keys := apiKeyList(t, env, "owner"); len(keys) != 1 || keys[0].RevokedAt != nil {
		t.Fatalf("owner key must survive: %+v", keys)
	}
}

func TestAPIKeys_AdminRevokesMembersKey(t *testing.T) {
	env := newAPIKeyEnv(t)
	memberKey := mintKey(t, env, "member", "chat key")

	rec := revokeKey(env, "owner", memberKey.ID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("owner revoke member's key: status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAPIKeys_RevokeUnknownKeyNotFound(t *testing.T) {
	env := newAPIKeyEnv(t)

	rec := revokeKey(env, "owner", "no-such-key")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("revoke unknown key: status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
