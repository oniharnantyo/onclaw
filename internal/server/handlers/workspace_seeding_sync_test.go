package handlers_test

// Workspace-creation seeding Sync test (fix-role-permission-audit 1.6,
// design D1/D2): the built-in role rows born inside the birth transaction
// are written through to the authorizer immediately, so a newborn
// workspace's roles enforce without a server reboot.

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
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

func TestCreateWorkspace_SyncsNewbornRoles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	creator := &domain.User{Email: "founder@example.com", Name: "Founder"}
	if err := st.Users().Create(ctx, creator); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// The authorizer boots BEFORE the workspace exists — the newborn roles
	// must reach it through the handler's seeding Sync, not a reboot.
	az, err := newTestAuthorizer(ctx, st)
	if err != nil {
		t.Fatalf("build test authorizer: %v", err)
	}

	wsH := handlers.NewWorkspaceHandlers(st, []byte("01234567890123456789012345678901"), nil, nil, nil, t.TempDir(), az)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.UserContextKey, creator)
		c.Next()
	})
	r.POST("/workspaces", wsH.CreateWorkspace)

	body, _ := json.Marshal(map[string]string{"name": "Fresh", "slug": "fresh"})
	req := httptest.NewRequest(http.MethodPost, "/workspaces", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create workspace: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// The seeded roles enforce immediately: the Owner role holds
	// workspace.write in the new workspace, the Member role holds the
	// member set (channels.write) but not workspace.write.
	ws, err := st.Workspaces().BySlug(ctx, "fresh")
	if err != nil {
		t.Fatalf("load created workspace: %v", err)
	}
	ownerRole, err := st.Roles().FindByName(ctx, ws.ID, domain.RoleOwner)
	if err != nil {
		t.Fatalf("load owner role: %v", err)
	}
	memberRole, err := st.Roles().FindByName(ctx, ws.ID, domain.RoleMember)
	if err != nil {
		t.Fatalf("load member role: %v", err)
	}

	for _, tc := range []struct {
		name       string
		roleID     string
		permission string
		want       bool
	}{
		{"owner workspace.write", ownerRole.ID, domain.WorkspaceWrite, true},
		{"owner agents.write", ownerRole.ID, domain.AgentsWrite, true},
		{"member channels.write", memberRole.ID, domain.ChannelsWrite, true},
		{"member agents.read", memberRole.ID, domain.AgentsRead, true},
		{"member not workspace.write", memberRole.ID, domain.WorkspaceWrite, false},
		{"member not integrations.write", memberRole.ID, domain.IntegrationsWrite, false},
	} {
		got, err := az.Enforce(ctx, tc.roleID, ws.ID, tc.permission)
		if err != nil {
			t.Fatalf("%s: enforce: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: Enforce = %v, want %v", tc.name, got, tc.want)
		}
	}
}
