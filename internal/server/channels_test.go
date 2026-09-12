package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/server"
)

// -----------------------------------------------------------------------------
// Channel endpoints (change integrate-agent-channels, tasks 6.x): workspace
// CRUD, membership roster, feed cursor + posts, and the SSE event stream.
// -----------------------------------------------------------------------------

// channelRow mirrors the API read view of one channel.
type channelRow struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Purpose     string `json:"purpose"`
	Conventions string `json:"conventions"`
}

type channelResponse struct {
	Channel channelRow `json:"channel"`
}

type channelListResponse struct {
	Channels []channelRow `json:"channels"`
}

// channelMemberRow mirrors the roster read view (resolved identity included).
type channelMemberRow struct {
	ID             string `json:"id"`
	MemberType     string `json:"member_type"`
	AgentID        string `json:"agent_id"`
	DisplayName    string `json:"display_name"`
	Handle         string `json:"handle"`
	Specialization string `json:"specialization"`
}

type channelMessageRow struct {
	ID         string `json:"id"`
	ChannelID  string `json:"channel_id"`
	Seq        int64  `json:"seq"`
	AuthorType string `json:"author_type"`
	AuthorUser string `json:"author_user_id"`
	Body       string `json:"body"`
}

// channelsTestEnvMembers wires the standard permission-matrix fixture for the
// channel surface.
func channelsTestEnvMembers(t *testing.T, env *testEnv, slug string) (ownerToken, adminToken, memberToken, nonMemberToken string) {
	t.Helper()
	ownerUser, ownerToken := createTestUser(t, env, slug+"-owner@example.com", "Sarah Chen", "pwd")
	adminUser, adminToken := createTestUser(t, env, slug+"-admin@example.com", "Admin User", "pwd")
	memberUser, memberToken := createTestUser(t, env, slug+"-member@example.com", "Member User", "pwd")
	_, nonMemberToken = createTestUser(t, env, slug+"-outsider@example.com", "Outsider", "pwd")

	ws, ownerRole, adminRole, memberRole := createTestWorkspaceWithRoles(t, env, slug, "Channels WS "+slug)
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, adminUser.ID, adminRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)
	return ownerToken, adminToken, memberToken, nonMemberToken
}

func createChannelViaAPI(t *testing.T, env *testEnv, token, wsSlug, slug string) channelRow {
	t.Helper()
	w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/"+wsSlug+"/channels", token, map[string]any{
		"name":        "Production Ops",
		"slug":        slug,
		"purpose":     "Coordinate production incident response.",
		"conventions": "One incident per chain.",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create channel: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var res channelResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode channel: %v", err)
	}
	return res.Channel
}

func TestChannels_PermissionMatrix(t *testing.T) {
	env := setupTestEnv(t)
	ownerToken, adminToken, memberToken, nonMemberToken := channelsTestEnvMembers(t, env, "chan-perm-ws")
	createChannelViaAPI(t, env, ownerToken, "chan-perm-ws", "ops")

	// Owner and Admin hold channels.read / channels.write.
	w := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/chan-perm-ws/channels", ownerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("owner list: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = doRequest(env.router, http.MethodGet, "/api/v1/workspaces/chan-perm-ws/channels", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("admin list: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The builtin Member role snapshot holds neither channel permission.
	w = doRequest(env.router, http.MethodGet, "/api/v1/workspaces/chan-perm-ws/channels", memberToken, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("member list: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	w = doRequest(env.router, http.MethodPost, "/api/v1/workspaces/chan-perm-ws/channels", memberToken, map[string]any{"name": "Nope", "slug": "nope"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("member create: expected 403, got %d", w.Code)
	}
	w = doRequest(env.router, http.MethodPost, "/api/v1/workspaces/chan-perm-ws/channels/00000000-0000-0000-0000-000000000000/messages", memberToken, map[string]any{"body": "hi"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("member post: expected 403, got %d", w.Code)
	}

	// Non-members get the enumeration defense 404, not 403.
	w = doRequest(env.router, http.MethodGet, "/api/v1/workspaces/chan-perm-ws/channels", nonMemberToken, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-member list: expected 404, got %d", w.Code)
	}
}

func TestChannels_CRUDAndSlugConflicts(t *testing.T) {
	env := setupTestEnv(t)
	ownerToken, _, _, _ := channelsTestEnvMembers(t, env, "chan-crud-ws")

	ch := createChannelViaAPI(t, env, ownerToken, "chan-crud-ws", "ops")
	if ch.ID == "" || ch.Slug != "ops" || ch.WorkspaceID == "" {
		t.Fatalf("unexpected created channel: %+v", ch)
	}

	// The exact slug duplicate is a 409 conflict. The store's uniqueness is
	// case-insensitive, but kebab-case validation is lowercase-only, so a
	// case-variant slug is rejected as invalid input (422) before the
	// conflict check can apply.
	w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/chan-crud-ws/channels", ownerToken, map[string]any{"name": "Dup", "slug": "ops"})
	if w.Code != http.StatusConflict {
		t.Fatalf("slug conflict: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var errRes struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &errRes)
	if errRes.Error.Code != server.CodeConflict {
		t.Errorf("expected error code %q, got %q", server.CodeConflict, errRes.Error.Code)
	}
	w = doRequest(env.router, http.MethodPost, "/api/v1/workspaces/chan-crud-ws/channels", ownerToken, map[string]any{"name": "Ops Uppercase", "slug": "OPS"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("uppercase slug: expected 422, got %d: %s", w.Code, w.Body.String())
	}

	// Invalid payloads are fielded 422s.
	w = doRequest(env.router, http.MethodPost, "/api/v1/workspaces/chan-crud-ws/channels", ownerToken, map[string]any{"name": "  ", "slug": "Bad Slug!"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid create: expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"field":"name"`) || !strings.Contains(w.Body.String(), `"field":"slug"`) {
		t.Errorf("expected fielded details, got %s", w.Body.String())
	}

	// Reads resolve by id and by slug.
	w = doRequest(env.router, http.MethodGet, "/api/v1/workspaces/chan-crud-ws/channels/"+ch.ID, ownerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get by id: expected 200, got %d", w.Code)
	}
	w = doRequest(env.router, http.MethodGet, "/api/v1/workspaces/chan-crud-ws/channels/ops", ownerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get by slug: expected 200, got %d", w.Code)
	}

	// PATCH overlays name/purpose/conventions; the slug is not patchable.
	w = doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/chan-crud-ws/channels/"+ch.ID, ownerToken, map[string]any{
		"name":    "Incident Ops",
		"purpose": "On-call coordination.",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var patched channelResponse
	_ = json.Unmarshal(w.Body.Bytes(), &patched)
	if patched.Channel.Name != "Incident Ops" || patched.Channel.Purpose != "On-call coordination." || patched.Channel.Slug != "ops" {
		t.Errorf("unexpected patched channel: %+v", patched.Channel)
	}

	// PATCH with no fields is 400.
	w = doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/chan-crud-ws/channels/"+ch.ID, ownerToken, map[string]any{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty patch: expected 400, got %d", w.Code)
	}

	// Delete → 204, then 404.
	w = doRequest(env.router, http.MethodDelete, "/api/v1/workspaces/chan-crud-ws/channels/"+ch.ID, ownerToken, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", w.Code)
	}
	w = doRequest(env.router, http.MethodGet, "/api/v1/workspaces/chan-crud-ws/channels/"+ch.ID, ownerToken, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("deleted channel: expected 404, got %d", w.Code)
	}

	// The list is empty again.
	w = doRequest(env.router, http.MethodGet, "/api/v1/workspaces/chan-crud-ws/channels", ownerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list after delete: expected 200, got %d", w.Code)
	}
	var list channelListResponse
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Channels) != 0 {
		t.Errorf("expected empty list after delete, got %+v", list.Channels)
	}
}

func TestChannels_MembershipLifecycle(t *testing.T) {
	env := setupTestEnv(t)
	ownerToken, _, _, _ := channelsTestEnvMembers(t, env, "chan-mem-ws")

	// An agent joins the room: provider + agent via the API (the env's stub
	// model factory makes generation deterministic).
	wProv := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/chan-mem-ws/providers", ownerToken, map[string]any{
		"type": "openai", "name": "OpenAI", "key": "sk-test-1234",
	})
	if wProv.Code != http.StatusCreated {
		t.Fatalf("provider: expected 201, got %d: %s", wProv.Code, wProv.Body.String())
	}
	var provRes struct {
		Provider struct {
			ID string `json:"id"`
		} `json:"provider"`
	}
	_ = json.Unmarshal(wProv.Body.Bytes(), &provRes)

	wAgent := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/chan-mem-ws/agents", ownerToken, map[string]any{
		"name": "Beacon", "slug": "beacon", "role": "comms", "description": "d", "brief": "b",
		"provider_id": provRes.Provider.ID, "model": "gpt-4o",
	})
	if wAgent.Code != http.StatusCreated {
		t.Fatalf("agent: expected 201, got %d: %s", wAgent.Code, wAgent.Body.String())
	}
	var agentRes struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	_ = json.Unmarshal(wAgent.Body.Bytes(), &agentRes)
	agentID := agentRes.Agent.ID

	ch := createChannelViaAPI(t, env, ownerToken, "chan-mem-ws", "ops")
	memberBase := "/api/v1/workspaces/chan-mem-ws/channels/" + ch.ID + "/members"

	// Add with a specialization; the view resolves display name + handle.
	w := doRequest(env.router, http.MethodPost, memberBase, ownerToken, map[string]any{
		"member_type": "agent", "agent_id": agentID, "specialization": "Stakeholder comms",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("add member: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var addRes struct {
		Member channelMemberRow `json:"member"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &addRes); err != nil {
		t.Fatalf("decode member: %v", err)
	}
	if addRes.Member.DisplayName != "Beacon" || addRes.Member.Handle != "beacon" {
		t.Errorf("expected resolved agent identity, got %+v", addRes.Member)
	}
	memberID := addRes.Member.ID

	// Duplicate is 409; unknown agent is 404; bad member_type is a fielded 422.
	w = doRequest(env.router, http.MethodPost, memberBase, ownerToken, map[string]any{"member_type": "agent", "agent_id": agentID})
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate member: expected 409, got %d", w.Code)
	}
	w = doRequest(env.router, http.MethodPost, memberBase, ownerToken, map[string]any{"member_type": "agent", "agent_id": "00000000-0000-0000-0000-000000000000"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown agent: expected 404, got %d", w.Code)
	}
	w = doRequest(env.router, http.MethodPost, memberBase, ownerToken, map[string]any{"member_type": "robot"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad member_type: expected 422, got %d", w.Code)
	}

	// PATCH specialization, then roster shows it.
	w = doRequest(env.router, http.MethodPatch, memberBase+"/"+memberID, ownerToken, map[string]any{"specialization": "Exec updates"})
	if w.Code != http.StatusOK {
		t.Fatalf("patch member: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = doRequest(env.router, http.MethodGet, memberBase, ownerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("roster: expected 200, got %d", w.Code)
	}
	var roster struct {
		Members []channelMemberRow `json:"members"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &roster)
	if len(roster.Members) != 1 || roster.Members[0].Specialization != "Exec updates" {
		t.Errorf("unexpected roster: %+v", roster.Members)
	}

	// Remove → 204, roster empty.
	w = doRequest(env.router, http.MethodDelete, memberBase+"/"+memberID, ownerToken, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("remove member: expected 204, got %d", w.Code)
	}
	w = doRequest(env.router, http.MethodGet, memberBase, ownerToken, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &roster)
	if len(roster.Members) != 0 {
		t.Errorf("expected empty roster after removal, got %+v", roster.Members)
	}
}

func TestChannels_FeedCursorAndTenantIsolation(t *testing.T) {
	env := setupTestEnv(t)
	ownerToken, _, _, _ := channelsTestEnvMembers(t, env, "chan-feed-ws")
	ch := createChannelViaAPI(t, env, ownerToken, "chan-feed-ws", "ops")
	msgBase := "/api/v1/workspaces/chan-feed-ws/channels/" + ch.ID

	// Post two human messages; each returns the persisted row with its seq.
	w1 := doRequest(env.router, http.MethodPost, msgBase+"/messages", ownerToken, map[string]any{"body": "first message"})
	if w1.Code != http.StatusCreated {
		t.Fatalf("post 1: expected 201, got %d: %s", w1.Code, w1.Body.String())
	}
	var m1 struct {
		Message channelMessageRow `json:"message"`
	}
	if err := json.Unmarshal(w1.Body.Bytes(), &m1); err != nil {
		t.Fatalf("decode message 1: %v", err)
	}
	if m1.Message.AuthorType != "user" || m1.Message.AuthorUser == "" || m1.Message.Seq != 1 {
		t.Errorf("unexpected persisted message: %+v", m1.Message)
	}
	w2 := doRequest(env.router, http.MethodPost, msgBase+"/messages", ownerToken, map[string]any{"body": "second message"})
	if w2.Code != http.StatusCreated {
		t.Fatalf("post 2: expected 201, got %d", w2.Code)
	}

	// Blank body is a fielded 422.
	w := doRequest(env.router, http.MethodPost, msgBase+"/messages", ownerToken, map[string]any{"body": "  "})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("blank body: expected 422, got %d", w.Code)
	}

	// Full feed in ascending seq order.
	w = doRequest(env.router, http.MethodGet, msgBase+"/messages", ownerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("feed: expected 200, got %d", w.Code)
	}
	var feed struct {
		Messages []channelMessageRow `json:"messages"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &feed)
	if len(feed.Messages) != 2 || feed.Messages[0].Seq > feed.Messages[1].Seq {
		t.Fatalf("unexpected feed: %+v", feed.Messages)
	}

	// Cursor: after=1 returns only the second; limit bounds the page.
	w = doRequest(env.router, http.MethodGet, msgBase+"/messages?after=1", ownerToken, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &feed)
	if len(feed.Messages) != 1 || feed.Messages[0].Seq != 2 {
		t.Errorf("expected only seq 2 after cursor 1, got %+v", feed.Messages)
	}
	w = doRequest(env.router, http.MethodGet, msgBase+"/messages?after=0&limit=1", ownerToken, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &feed)
	if len(feed.Messages) != 1 || feed.Messages[0].Seq != 1 {
		t.Errorf("expected only seq 1 with limit 1, got %+v", feed.Messages)
	}

	// Invalid cursor params are 400.
	w = doRequest(env.router, http.MethodGet, msgBase+"/messages?after=bogus", ownerToken, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bogus after: expected 400, got %d", w.Code)
	}
	w = doRequest(env.router, http.MethodGet, msgBase+"/messages?limit=-1", ownerToken, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("negative limit: expected 400, got %d", w.Code)
	}

	// Tenant isolation: another workspace's owner cannot read, post to, or
	// stream this channel — 404, never 403.
	otherOwner, _ := createTestUser(t, env, "chan-other-owner@example.com", "Other Owner", "pwd")
	otherWS, otherRole, _, _ := createTestWorkspaceWithRoles(t, env, "chan-other-ws", "Other WS")
	addMember(t, env, otherWS.ID, otherOwner.ID, otherRole.ID)
	otherToken, err := env.issuer.Issue(context.Background(), otherOwner)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	w = doRequest(env.router, http.MethodGet, msgBase, otherToken, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-tenant get: expected 404, got %d", w.Code)
	}
	w = doRequest(env.router, http.MethodGet, msgBase+"/messages", otherToken, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-tenant feed: expected 404, got %d", w.Code)
	}
	w = doRequest(env.router, http.MethodPost, msgBase+"/messages", otherToken, map[string]any{"body": "intruding"})
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-tenant post: expected 404, got %d", w.Code)
	}
	w = doRequest(env.router, http.MethodGet, msgBase+"/events", otherToken, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-tenant stream: expected 404, got %d", w.Code)
	}
}

// TestChannels_StreamEvents subscribes the SSE endpoint over a real HTTP
// server, posts a canary message through the REST route, and asserts the
// message_posted frame (with its feed seq for client dedup) arrives.
func TestChannels_StreamEvents(t *testing.T) {
	env := setupTestEnv(t)
	ownerToken, _, _, _ := channelsTestEnvMembers(t, env, "chan-sse-ws")
	ch := createChannelViaAPI(t, env, ownerToken, "chan-sse-ws", "ops")

	ts := httptest.NewServer(env.router)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/workspaces/chan-sse-ws/channels/"+ch.ID+"/events?stream=true", nil)
	if err != nil {
		t.Fatalf("build stream request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", ct)
	}
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Error("expected X-Accel-Buffering: no")
	}
	if resp.Header.Get("Cache-Control") != "no-cache" {
		t.Error("expected Cache-Control: no-cache")
	}

	type sseFrame struct {
		Type string `json:"type"`
		Seq  int64  `json:"seq"`
	}
	frames := make(chan sseFrame, 32)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var f sseFrame
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f) == nil {
				frames <- f
			}
		}
	}()

	// Give the subscription a moment to attach, then post the canary.
	time.Sleep(100 * time.Millisecond)
	w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/chan-sse-ws/channels/"+ch.ID+"/messages", ownerToken, map[string]any{"body": "sse canary"})
	if w.Code != http.StatusCreated {
		t.Fatalf("canary post: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	deadline := time.After(10 * time.Second)
	for {
		select {
		case f := <-frames:
			if f.Type == "message_posted" {
				if f.Seq != 1 {
					t.Errorf("expected the canary's feed seq 1 on the wire, got %d", f.Seq)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for message_posted on the SSE stream")
		}
	}
}
