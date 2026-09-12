package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// fakeSessionListerRunner extends the shared run fake with the D3 seam the
// session listing asserts on: enumerating the live runs' session ids for a
// workspace+agent pair.
type fakeSessionListerRunner struct {
	fakeAgentRunRunner

	mu     sync.Mutex
	active map[string]bool
}

func (f *fakeSessionListerRunner) setActive(ids ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.active = make(map[string]bool, len(ids))
	for _, id := range ids {
		f.active[id] = true
	}
}

func (f *fakeSessionListerRunner) ActiveRunSessionIDs(workspaceID, agentID string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(f.active))
	for id := range f.active {
		ids = append(ids, id)
	}
	return ids
}

// newAgentSessionsTestEnv builds a one-agent workspace with owner and member
// users, seeds one indexed session per user, and mounts the two session-index
// routes the way the router does — handler directly, workspace and current
// user injected by middleware. Tests flip currentUser to act as either user.
func newAgentSessionsTestEnv(t *testing.T) (*gin.Engine, store.Store, *domain.Workspace, *domain.User, *domain.User, *fakeSessionListerRunner, **domain.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	ws := &domain.Workspace{ID: "ws-sessions", Slug: "sess-ws", Name: "Sessions Workspace"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	ownerRole := &domain.Role{WorkspaceID: ws.ID, Name: "Owner", Permissions: domain.OwnerPermissions}
	if err := st.Roles().Create(ctx, ownerRole); err != nil {
		t.Fatalf("create owner role: %v", err)
	}
	memberRole := &domain.Role{WorkspaceID: ws.ID, Name: "Member", Permissions: domain.MemberPermissions}
	if err := st.Roles().Create(ctx, memberRole); err != nil {
		t.Fatalf("create member role: %v", err)
	}
	owner := &domain.User{Email: "owner@example.com", Name: "Owner"}
	if err := st.Users().Create(ctx, owner); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member := &domain.User{Email: "member@example.com", Name: "Member"}
	if err := st.Users().Create(ctx, member); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: owner.ID, RoleID: ownerRole.ID}); err != nil {
		t.Fatalf("add owner member: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: member.ID, RoleID: memberRole.ID}); err != nil {
		t.Fatalf("add member member: %v", err)
	}
	prov := &domain.ProviderConfig{ID: "prov-fake", WorkspaceID: ws.ID, Type: "fake", Name: "Fake"}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	agent := &domain.Agent{
		ID:          "agent-atlas-1",
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  prov.ID,
		Model:       "fake-model",
	}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// One indexed session per user on the same agent: the listing boundary
	// under test.
	if err := st.AgentSessions().UpsertAgentSession(ctx, ws.ID, agent.ID, owner.ID, domain.AgentSessionUpsert{SessionID: "sess-owner", Title: "Owner topic"}); err != nil {
		t.Fatalf("seed owner session: %v", err)
	}
	if err := st.AgentSessions().UpsertAgentSession(ctx, ws.ID, agent.ID, member.ID, domain.AgentSessionUpsert{SessionID: "sess-member", Title: "Member topic"}); err != nil {
		t.Fatalf("seed member session: %v", err)
	}

	runner := &fakeSessionListerRunner{active: map[string]bool{}}
	h := handlers.NewAgentHandlers(st.Agents(), st.Providers(), st.SessionEvents(), st.AgentSessions(), []byte("01234567890123456789012345678901"), nil, nil, nil, "ws-dir", runner, runner)

	var currentUser *domain.User
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.UserContextKey, currentUser)
		c.Next()
	})
	r.GET("/workspaces/:ws/agents/:agent/sessions", h.ListAgentSessions)
	r.DELETE("/workspaces/:ws/agents/:agent/sessions/:session", h.DeleteAgentSession)
	return r, st, ws, owner, member, runner, &currentUser
}

// sessionRow mirrors the exact wire shape of one listing row.
type sessionRow struct {
	ID           string `json:"id"`
	SessionID    string `json:"session_id"`
	Title        string `json:"title"`
	CreatedAt    string `json:"created_at"`
	LastActiveAt string `json:"last_active_at"`
	Running      bool   `json:"running"`
}

func listSessions(t *testing.T, r *gin.Engine, agentSlug string) (*httptest.ResponseRecorder, []sessionRow) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/sess-ws/agents/"+agentSlug+"/sessions", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		return w, nil
	}
	var res struct {
		Sessions []sessionRow `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal listing: %v (%s)", err, w.Body.String())
	}
	return w, res.Sessions
}

// TestAgentSessions_ListIsPrivatePerUser: each caller sees only their own
// rows, ordered newest-activity-first, with exactly the documented wire keys.
func TestAgentSessions_ListIsPrivatePerUser(t *testing.T) {
	r, _, _, owner, _, _, currentUser := newAgentSessionsTestEnv(t)
	*currentUser = owner

	w, sessions := listSessions(t, r, "atlas")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(sessions) != 1 {
		t.Fatalf("owner must see only their own session, got %v", sessions)
	}
	row := sessions[0]
	if row.SessionID != "sess-owner" || row.Title != "Owner topic" || row.Running {
		t.Fatalf("unexpected owner row: %+v", row)
	}
	if row.ID == "" || row.CreatedAt == "" || row.LastActiveAt == "" {
		t.Fatalf("row must carry id and timestamps: %+v", row)
	}

	// Exactly the documented keys — nothing more (no workspace/agent/user
	// attribution, no deleted_at), nothing less.
	var raw struct {
		Sessions []map[string]json.RawMessage `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	wantKeys := map[string]bool{"id": true, "session_id": true, "title": true, "created_at": true, "last_active_at": true, "running": true}
	for key := range raw.Sessions[0] {
		if !wantKeys[key] {
			t.Fatalf("unexpected key %q in listing row", key)
		}
	}
	if len(raw.Sessions[0]) != len(wantKeys) {
		t.Fatalf("row key count = %d, want %d (%v)", len(raw.Sessions[0]), len(wantKeys), raw.Sessions[0])
	}
}

// TestAgentSessions_EmptyListForKnownAgentIsArray covers the [] -not-null
// contract on a real agent whose caller owns no sessions.
func TestAgentSessions_EmptyListForKnownAgentIsArray(t *testing.T) {
	r, st, ws, _, _, _, currentUser := newAgentSessionsTestEnv(t)

	// A third workspace member who owns no sessions yet.
	stranger := &domain.User{Email: "stranger@example.com", Name: "Stranger"}
	ctx := context.Background()
	if err := st.Users().Create(ctx, stranger); err != nil {
		t.Fatalf("create stranger: %v", err)
	}
	role, err := st.Roles().FindByName(ctx, ws.ID, "Member")
	if err != nil {
		t.Fatalf("find member role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: stranger.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("add stranger member: %v", err)
	}
	*currentUser = stranger

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/sess-ws/agents/atlas/sessions", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var raw struct {
		Sessions *json.RawMessage `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw.Sessions == nil || string(*raw.Sessions) != "[]" {
		t.Fatalf("empty listing must render [], got %s", w.Body.String())
	}
}

// TestAgentSessions_RunningFlagFromLiveRuns: a row whose session id is in the
// runner's live enumeration carries running=true; the caller's other rows and
// foreign live sessions stay false.
func TestAgentSessions_RunningFlagFromLiveRuns(t *testing.T) {
	r, _, _, owner, _, runner, currentUser := newAgentSessionsTestEnv(t)
	runner.setActive("sess-owner", "sess-member") // member's run is live but foreign
	*currentUser = owner

	_, sessions := listSessions(t, r, "atlas")
	if len(sessions) != 1 {
		t.Fatalf("owner must see only their own session, got %v", sessions)
	}
	if !sessions[0].Running {
		t.Fatalf("live session must report running=true: %+v", sessions[0])
	}

	// Once the run finishes the flag drops.
	runner.setActive()
	_, sessions = listSessions(t, r, "atlas")
	if sessions[0].Running {
		t.Fatalf("finished run must report running=false: %+v", sessions[0])
	}
}

// TestAgentSessions_DeleteHidesRow: the owner's delete is 204 and the row
// disappears from subsequent listings.
func TestAgentSessions_DeleteHidesRow(t *testing.T) {
	r, _, _, owner, _, _, currentUser := newAgentSessionsTestEnv(t)
	*currentUser = owner

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/workspaces/sess-ws/agents/atlas/sessions/sess-owner", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on delete, got %d: %s", w.Code, w.Body.String())
	}

	_, sessions := listSessions(t, r, "atlas")
	for _, s := range sessions {
		if s.SessionID == "sess-owner" {
			t.Fatalf("deleted session must vanish from the listing, got %v", sessions)
		}
	}
}

// TestAgentSessions_DeleteForeignOrAbsentIs404: a foreign-owned row and an
// unknown session id are indistinguishable — both 404 (no existence leak).
func TestAgentSessions_DeleteForeignOrAbsentIs404(t *testing.T) {
	r, _, _, _, member, _, currentUser := newAgentSessionsTestEnv(t)
	*currentUser = member

	// The member listing shows only their own session: the owner's row is
	// never exposed, so the member cannot address it at all.
	_, sessions := listSessions(t, r, "atlas")
	if len(sessions) != 1 || sessions[0].SessionID != "sess-member" {
		t.Fatalf("member must see only their own session, got %v", sessions)
	}

	// Deleting the owner's session by its known id is still 404.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/workspaces/sess-ws/agents/atlas/sessions/sess-owner", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("foreign delete must be 404, got %d: %s", w.Code, w.Body.String())
	}

	// An absent session id is the same 404.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/workspaces/sess-ws/agents/atlas/sessions/sess-ghost", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("absent delete must be 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAgentSessions_UnknownAgentIs404: agent resolution mirrors the other
// agent routes (slug-or-id, 404 for unknown).
func TestAgentSessions_UnknownAgentIs404(t *testing.T) {
	r, _, _, owner, _, _, currentUser := newAgentSessionsTestEnv(t)
	*currentUser = owner

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/sess-ws/agents/ghost/sessions", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown agent must be 404, got %d: %s", w.Code, w.Body.String())
	}
}
