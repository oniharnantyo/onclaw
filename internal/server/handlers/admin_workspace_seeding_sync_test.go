package handlers_test

// Admin workspace-creation seeding Sync test (fix-role-permission-audit 1.6,
// design D1/D2): the ADMIN console's AdminCreateWorkspace births the same
// three built-in role rows inside its transaction and must write them through
// to the authorizer immediately — the engine boot-loaded once at startup
// knows nothing about workspaces created afterwards, and fail-closed would
// deny every permission in the newborn workspace until a reboot.

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
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

func TestAdminCreateWorkspace_SyncsNewbornRoles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()

	// The authorizer boots BEFORE the workspace exists — the newborn roles
	// must reach it through the handler's seeding Sync, not a reboot.
	az, err := newTestAuthorizer(ctx, st)
	if err != nil {
		t.Fatalf("build test authorizer: %v", err)
	}

	admH := handlers.NewAdminWorkspaceHandlers(st, storagefake.New(), az)
	r := gin.New()
	r.POST("/admin/workspaces", admH.AdminCreateWorkspace)

	body, _ := json.Marshal(map[string]string{
		"name":        "Admin Born",
		"slug":        "admin-born",
		"owner_email": "admin.founder@example.com",
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/workspaces", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create workspace: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// The seeded roles enforce immediately: the Owner role holds
	// workspace.write in the new workspace, the Admin role holds its
	// workspace set, and the Member role holds the member set
	// (channels.write) but not workspace.write — nor does any workspace
	// role reach into the instance-admin catalog.
	ws, err := st.Workspaces().BySlug(ctx, "admin-born")
	if err != nil {
		t.Fatalf("load created workspace: %v", err)
	}
	ownerRole, err := st.Roles().FindByName(ctx, ws.ID, domain.RoleOwner)
	if err != nil {
		t.Fatalf("load owner role: %v", err)
	}
	adminRole, err := st.Roles().FindByName(ctx, ws.ID, domain.RoleAdmin)
	if err != nil {
		t.Fatalf("load admin role: %v", err)
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
		{"admin workspace.write", adminRole.ID, domain.WorkspaceWrite, true},
		{"admin not admin.workspaces.write", adminRole.ID, domain.AdminWorkspacesWrite, false},
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
