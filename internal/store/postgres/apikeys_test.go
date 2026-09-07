//go:build integration

package postgres_test

import (
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// TestIntegration_WorkspaceAPIKeyStore covers the workspace_api_keys migration
// round-trip: create (hash + display fields), workspace-scoped list, revoke
// semantics, and the global lookup-by-hash used by /v1 authentication.
func TestIntegration_WorkspaceAPIKeyStore(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	ws := &domain.Workspace{Slug: "keys-it-ws", Name: "Keys IT", Timezone: "UTC"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	user := &domain.User{Email: "keys-it@example.com", Name: "Keys IT"}
	if err := s.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	key := &domain.WorkspaceAPIKey{
		WorkspaceID: ws.ID,
		Name:        "ci",
		KeyHash:     "a-very-fake-hash-value",
		KeyPrefix:   "oc_ws_abcde",
		KeySuffix:   "wxyz",
		CreatedBy:   user.ID,
	}
	if err := s.APIKeys().Create(ctx, key); err != nil {
		t.Fatalf("create key: %v", err)
	}
	if key.ID == "" || key.CreatedAt.IsZero() {
		t.Fatalf("expected ID and created_at to be assigned: %+v", key)
	}

	// Lookup by hash (the /v1 authn path).
	found, err := s.APIKeys().LookupByHash(ctx, key.KeyHash)
	if err != nil {
		t.Fatalf("lookup by hash: %v", err)
	}
	if found.ID != key.ID || found.WorkspaceID != ws.ID || found.CreatedBy != user.ID {
		t.Fatalf("lookup mismatch: %+v", found)
	}
	if found.IsRevoked() {
		t.Fatal("fresh key must not be revoked")
	}

	// Workspace-scoped list.
	list, err := s.APIKeys().List(ctx, ws.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %d keys, err %v", len(list), err)
	}

	// Revoke and confirm it stops authenticating.
	if err := s.APIKeys().Revoke(ctx, ws.ID, key.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	found, err = s.APIKeys().LookupByHash(ctx, key.KeyHash)
	if err != nil {
		t.Fatalf("lookup after revoke: %v", err)
	}
	if !found.IsRevoked() {
		t.Fatal("revoked key must report IsRevoked")
	}

	// Foreign workspace revoke is not-found.
	other := &domain.Workspace{Slug: "keys-it-other", Name: "Other", Timezone: "UTC"}
	if err := s.Workspaces().Create(ctx, other); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	if err := s.APIKeys().Revoke(ctx, other.ID, key.ID); err == nil {
		t.Fatal("foreign revoke should fail")
	}

	// Unknown hash is not-found.
	if _, err := s.APIKeys().LookupByHash(ctx, "missing-hash"); err == nil {
		t.Fatal("unknown hash should fail")
	}
}
