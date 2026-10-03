package handlers_test

// Reference-document mutation matrix and listing-lens tests
// (fix-role-permission-audit 4.3, design D6): upload stays member-level;
// patch/content/delete are uploader-or-workspace.write; attaching to agents
// needs agents.write and to channels channels.write; promote/demote stay
// route-gated (covered by the contract suite). The member listing lens sees
// own uploads plus documents attached to surfaces the viewer can configure.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/references"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/storage/resolver"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"

	_ "github.com/oniharnantyo/onclaw/internal/storage/local"
)

type refDocACLEnv struct {
	st      store.Store
	router  *gin.Engine
	ws      *domain.Workspace
	owner   *domain.User
	member  *domain.User
	limited *domain.User
}

// newRefDocACLEnv wires the documents routes over the fake store with three
// callers: owner (workspace.write, agents.write, channels.write, promote),
// member (channels.write via the built-in Member set, no agents.write), and
// limited (workspace.read only — none of the write tiers).
func newRefDocACLEnv(t *testing.T) refDocACLEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	dataDir := t.TempDir()
	strg, err := storage.Open("local", storage.StorageConfig{Driver: "local", DataDir: dataDir})
	if err != nil {
		t.Fatalf("open local storage: %v", err)
	}
	wsStorage := resolver.New(strg, st.WorkspaceStorage(), st.Attachments(), []byte("01234567890123456789012345678901"), dataDir)

	ws := &domain.Workspace{Name: "Matrix WS", Slug: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	owner := &domain.User{Email: "matrix-owner@example.com", Name: "Owner"}
	member := &domain.User{Email: "matrix-member@example.com", Name: "Member"}
	limited := &domain.User{Email: "matrix-limited@example.com", Name: "Limited"}
	for _, u := range []*domain.User{owner, member, limited} {
		if err := st.Users().Create(ctx, u); err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	ownerRole := &domain.Role{WorkspaceID: ws.ID, Name: domain.RoleOwner, IsOwner: true, Permissions: domain.OwnerPermissions, BuiltIn: true}
	memberRole := &domain.Role{WorkspaceID: ws.ID, Name: domain.RoleMember, Permissions: domain.MemberPermissions, BuiltIn: true}
	limitedRole := &domain.Role{WorkspaceID: ws.ID, Name: "Limited", Permissions: []string{domain.WorkspaceRead}}
	for _, role := range []*domain.Role{ownerRole, memberRole, limitedRole} {
		if err := st.Roles().Create(ctx, role); err != nil {
			t.Fatalf("seed role: %v", err)
		}
	}
	for _, m := range []*struct {
		user *domain.User
		role *domain.Role
	}{{owner, ownerRole}, {member, memberRole}, {limited, limitedRole}} {
		if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: m.user.ID, RoleID: m.role.ID}); err != nil {
			t.Fatalf("seed membership: %v", err)
		}
	}

	agent := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas"}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	channel := &domain.Channel{WorkspaceID: ws.ID, Slug: "ops", Name: "#ops"}
	if err := st.Channels().CreateChannel(ctx, channel); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	az, err := newTestAuthorizer(ctx, st)
	if err != nil {
		t.Fatalf("build test authorizer: %v", err)
	}
	docH := handlers.NewReferenceDocumentsHandlers(references.NewService(st, wsStorage), st.ReferenceDocuments(), az)

	roleByUser := map[string]*domain.Role{owner.ID: ownerRole, member.ID: memberRole, limited.ID: limitedRole}
	userByHeader := map[string]*domain.User{"owner": owner, "member": member, "limited": limited}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		user, ok := userByHeader[c.GetHeader("X-Test-User")]
		if !ok {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(handlers.UserContextKey, user)
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.MemberContextKey, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: roleByUser[user.ID].ID, Role: roleByUser[user.ID]})
		c.Set(handlers.RoleContextKey, roleByUser[user.ID])
		c.Next()
	})
	r.POST("/api/v1/workspaces/acme/documents", docH.Upload)
	r.GET("/api/v1/workspaces/acme/documents", docH.List)
	r.PATCH("/api/v1/workspaces/acme/documents/:id", docH.Patch)
	r.DELETE("/api/v1/workspaces/acme/documents/:id", docH.Delete)
	r.PUT("/api/v1/workspaces/acme/documents/:id/agents", docH.PutAgents)
	r.PUT("/api/v1/workspaces/acme/documents/:id/channels", docH.PutChannels)
	r.PUT("/api/v1/workspaces/acme/documents/:id/content", docH.PutContent)

	return refDocACLEnv{st: st, router: r, ws: ws, owner: owner, member: member, limited: limited}
}

// uploadAs seeds one document through the API as the given caller and
// returns the decoded view.
func uploadAs(t *testing.T, router *gin.Engine, who, name string, extra map[string][]string) documentResponse {
	t.Helper()
	fields := map[string][]string{"name": {name}}
	for k, vs := range extra {
		fields[k] = vs
	}
	rec := refDocUpload(t, router, who, http.MethodPost, "/api/v1/workspaces/acme/documents", fields, name+".md", []byte(refDocMD))
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload %s as %s: expected 201, got %d: %s", name, who, rec.Code, rec.Body.String())
	}
	return decodeDocument(t, rec)
}

func TestRefDocMatrix_MemberMutatesOwnUpload(t *testing.T) {
	env := newRefDocACLEnv(t)

	doc := uploadAs(t, env.router, "member", "mine", nil)

	rec := refDocJSON(t, env.router, "member", http.MethodPatch, "/api/v1/workspaces/acme/documents/"+doc.ID, map[string]any{"name": "Renamed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("member patch own: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = refDocUpload(t, env.router, "member", http.MethodPut, "/api/v1/workspaces/acme/documents/"+doc.ID+"/content", nil, "mine.md", []byte("# replaced\n"))
	if rec.Code != http.StatusOK {
		t.Fatalf("member replace own: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = refDocJSON(t, env.router, "member", http.MethodDelete, "/api/v1/workspaces/acme/documents/"+doc.ID, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("member delete own: status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestRefDocMatrix_MemberCannotMutateOthersUpload(t *testing.T) {
	env := newRefDocACLEnv(t)

	doc := uploadAs(t, env.router, "owner", "theirs", nil)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		run    func(asWho string) *httptest.ResponseRecorder
	}{
		{"patch", http.MethodPatch, "/api/v1/workspaces/acme/documents/" + doc.ID, func(asWho string) *httptest.ResponseRecorder {
			return refDocJSON(t, env.router, asWho, http.MethodPatch, "/api/v1/workspaces/acme/documents/"+doc.ID, map[string]any{"name": "Hijacked"})
		}},
		{"content", http.MethodPut, "/api/v1/workspaces/acme/documents/" + doc.ID + "/content", func(asWho string) *httptest.ResponseRecorder {
			return refDocUpload(t, env.router, asWho, http.MethodPut, "/api/v1/workspaces/acme/documents/"+doc.ID+"/content", nil, "swap.md", []byte("# swapped\n"))
		}},
		{"delete", http.MethodDelete, "/api/v1/workspaces/acme/documents/" + doc.ID, func(asWho string) *httptest.ResponseRecorder {
			return refDocJSON(t, env.router, asWho, http.MethodDelete, "/api/v1/workspaces/acme/documents/"+doc.ID, nil)
		}},
	} {
		rec := tc.run("member")
		if rec.Code != http.StatusForbidden {
			t.Errorf("member %s on another member's document: status = %d, want 403: %s", tc.name, rec.Code, rec.Body.String())
		}
	}

	// The document is untouched.
	row, err := env.st.ReferenceDocuments().Get(context.Background(), env.ws.ID, doc.ID)
	if err != nil || row.Name != "theirs" {
		t.Fatalf("forbidden mutations must not apply: %v (%+v)", err, row)
	}
}

func TestRefDocMatrix_WorkspaceWriteMutatesAnyDocument(t *testing.T) {
	env := newRefDocACLEnv(t)

	doc := uploadAs(t, env.router, "member", "members", nil)

	rec := refDocJSON(t, env.router, "owner", http.MethodPatch, "/api/v1/workspaces/acme/documents/"+doc.ID, map[string]any{"name": "Admin-renamed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("owner patch member's document: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = refDocUpload(t, env.router, "owner", http.MethodPut, "/api/v1/workspaces/acme/documents/"+doc.ID+"/content", nil, "members.md", []byte("# admin replace\n"))
	if rec.Code != http.StatusOK {
		t.Fatalf("owner replace member's document: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = refDocJSON(t, env.router, "owner", http.MethodDelete, "/api/v1/workspaces/acme/documents/"+doc.ID, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("owner delete member's document: status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestRefDocMatrix_AttachGating(t *testing.T) {
	env := newRefDocACLEnv(t)
	ctx := context.Background()

	doc := uploadAs(t, env.router, "member", "attach", nil)

	agents, err := env.st.Agents().ListForWorkspace(ctx, env.ws.ID)
	if err != nil || len(agents) == 0 {
		t.Fatalf("seeded agent missing: %v", err)
	}
	channels, err := env.st.Channels().ListChannels(ctx, env.ws.ID)
	if err != nil || len(channels) == 0 {
		t.Fatalf("seeded channel missing: %v", err)
	}

	// Agents.write: the member holds none — 403; the owner holds it — 200.
	rec := refDocJSON(t, env.router, "member", http.MethodPut, "/api/v1/workspaces/acme/documents/"+doc.ID+"/agents", map[string]any{"agentIds": []string{agents[0].ID}})
	if rec.Code != http.StatusForbidden {
		t.Errorf("member attach agents: status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	rec = refDocJSON(t, env.router, "owner", http.MethodPut, "/api/v1/workspaces/acme/documents/"+doc.ID+"/agents", map[string]any{"agentIds": []string{agents[0].ID}})
	if rec.Code != http.StatusOK {
		t.Errorf("owner attach agents: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// Channels.write: the built-in Member set holds it — 200; the limited
	// role holds none — 403.
	rec = refDocJSON(t, env.router, "member", http.MethodPut, "/api/v1/workspaces/acme/documents/"+doc.ID+"/channels", map[string]any{"channelIds": []string{channels[0].ID}})
	if rec.Code != http.StatusOK {
		t.Errorf("member attach channels: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	rec = refDocJSON(t, env.router, "limited", http.MethodPut, "/api/v1/workspaces/acme/documents/"+doc.ID+"/channels", map[string]any{"channelIds": []string{channels[0].ID}})
	if rec.Code != http.StatusForbidden {
		t.Errorf("limited attach channels: status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func TestRefDocMatrix_MemberListingLens(t *testing.T) {
	env := newRefDocACLEnv(t)
	ctx := context.Background()

	agents, err := env.st.Agents().ListForWorkspace(ctx, env.ws.ID)
	if err != nil || len(agents) == 0 {
		t.Fatalf("seeded agent missing: %v", err)
	}
	channels, err := env.st.Channels().ListChannels(ctx, env.ws.ID)
	if err != nil || len(channels) == 0 {
		t.Fatalf("seeded channel missing: %v", err)
	}

	ownDoc := uploadAs(t, env.router, "member", "own", nil)
	ownAgentDoc := uploadAs(t, env.router, "member", "own-agent", map[string][]string{"agentIds": {agents[0].ID}})
	channelDoc := uploadAs(t, env.router, "owner", "chan", map[string][]string{"channelIds": {channels[0].ID}})
	agentDoc := uploadAs(t, env.router, "owner", "agent", map[string][]string{"agentIds": {agents[0].ID}})
	promotedDoc := uploadAs(t, env.router, "owner", "promoted", nil)
	if err := env.st.ReferenceDocuments().SetScope(ctx, env.ws.ID, promotedDoc.ID, domain.RefDocScopeWorkspace); err != nil {
		t.Fatalf("promote fixture: %v", err)
	}

	listIDs := func(t *testing.T, asWho, query string) map[string]bool {
		t.Helper()
		rec := refDocJSON(t, env.router, asWho, http.MethodGet, "/api/v1/workspaces/acme/documents"+query, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s list%s: status = %d, body = %s", asWho, query, rec.Code, rec.Body.String())
		}
		var res struct {
			Documents []documentResponse `json:"documents"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		ids := map[string]bool{}
		for _, d := range res.Documents {
			ids[d.ID] = true
		}
		return ids
	}

	// The member lens: own uploads (including the agent-attached one) plus
	// documents attached to channels they can configure — not the owner's
	// agent-attached upload and not the promoted workspace-tier document.
	got := listIDs(t, "member", "")
	if len(got) != 3 || !got[ownDoc.ID] || !got[ownAgentDoc.ID] || !got[channelDoc.ID] {
		t.Fatalf("member lens = %v, want own %s + own-agent %s + channel %s", got, ownDoc.ID, ownAgentDoc.ID, channelDoc.ID)
	}
	if got[agentDoc.ID] || got[promotedDoc.ID] {
		t.Fatalf("member lens leaks agent-attached %v or promoted %v", got[agentDoc.ID], got[promotedDoc.ID])
	}

	// The agent lens applies the same predicate: the member's own
	// agent-attached upload passes; the owner's does not.
	got = listIDs(t, "member", "?agent="+agents[0].ID)
	if len(got) != 1 || !got[ownAgentDoc.ID] {
		t.Fatalf("member agent lens = %v, want only own-agent %s", got, ownAgentDoc.ID)
	}

	// The channel lens: every channel-attached document passes for the
	// member (channels.write covers configuration).
	got = listIDs(t, "member", "?channel="+channels[0].ID)
	if len(got) != 1 || !got[channelDoc.ID] {
		t.Fatalf("member channel lens = %v, want channel doc %s", got, channelDoc.ID)
	}

	// The owner (reference_documents.promote) sees the whole library.
	got = listIDs(t, "owner", "")
	if len(got) != 5 {
		t.Fatalf("owner lens = %d documents, want all 5", len(got))
	}

	// The limited role sees only its own uploads — none of the others',
	// including the channel-attached one (no channels.write to configure).
	got = listIDs(t, "limited", "")
	if len(got) != 0 {
		t.Fatalf("limited lens = %v, want empty", got)
	}
}
