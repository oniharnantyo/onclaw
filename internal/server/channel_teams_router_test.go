package server_test

// Channel-teams router tests (change channel-teams, tasks 6/7): the full HTTP
// surface on the fake store — static templates route resolution, template
// materialization through the real shared agent creation path (the env's stub
// model factory keeps generation deterministic), kickoff posts through the
// real chokepoint session branch, work-session reads, member role patches,
// the awaiting-state channel views, the permission matrix, and tenant
// isolation.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// teamsTestEnvMembers is channelsTestEnvMembers under a teams-specific slug.
func teamsTestEnvMembers(t *testing.T, env *testEnv, slug string) (ownerToken, memberToken, nonMemberToken string) {
	t.Helper()
	ownerUser, ownerToken := createTestUser(t, env, slug+"-owner@example.com", "Sarah Chen", "pwd")
	memberUser, memberToken := createTestUser(t, env, slug+"-member@example.com", "Member User", "pwd")
	_, nonMemberToken = createTestUser(t, env, slug+"-outsider@example.com", "Outsider", "pwd")

	ws, ownerRole, _, memberRole := createTestWorkspaceWithRoles(t, env, slug, "Teams WS "+slug)
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)
	return ownerToken, memberToken, nonMemberToken
}

// seedProviderWithDeadBaseURL registers an openai-compatible provider pointing
// at an unroutable loopback port: spawn-time generation rides the env's stub
// model factory (no network), while any background fan-out run fails fast on
// connection refused instead of hanging the test.
func seedProviderWithDeadBaseURL(t *testing.T, env *testEnv, token, wsSlug string) string {
	t.Helper()
	w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/"+wsSlug+"/providers", token, map[string]any{
		"type": "openai-compatible", "name": "Teams Mock", "base_url": "http://127.0.0.1:9/v1", "key": "sk-teams",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("seed provider: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Provider struct {
			ID string `json:"id"`
		} `json:"provider"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	return res.Provider.ID
}

func TestTeams_TemplatesRouteResolvesBeforeIDCapture(t *testing.T) {
	env := setupTestEnv(t)
	ownerToken, _, _ := teamsTestEnvMembers(t, env, "teams-route-ws")

	// The static /channels/templates route must be served by the listing
	// handler, never captured by the /channels/:id detail route.
	w := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/teams-route-ws/channels/templates", ownerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET templates: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Templates []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Conventions string `json:"conventions"`
			Slots       []struct {
				ID             string `json:"id"`
				Title          string `json:"title"`
				Specialization string `json:"specialization"`
				Facilitator    bool   `json:"facilitator"`
			} `json:"slots"`
		} `json:"templates"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode templates: %v", err)
	}
	if len(res.Templates) != 1 || res.Templates[0].ID != "software-team" || res.Templates[0].Conventions == "" {
		t.Fatalf("unexpected templates payload: %+v", res.Templates)
	}
	if len(res.Templates[0].Slots) != 6 {
		t.Errorf("expected six slots on the wire, got %+v", res.Templates[0].Slots)
	}
	facilitators := 0
	for _, s := range res.Templates[0].Slots {
		if s.Facilitator {
			facilitators++
		}
	}
	if facilitators != 1 {
		t.Errorf("expected exactly one facilitator slot, got %d", facilitators)
	}

	// The materialize sub-route resolves too: an unknown template inside it is
	// a 404 from the handler, not a gin routing miss (which would also be 404
	// but with the generic envelope; assert the error message instead).
	w = doRequest(env.router, http.MethodPost, "/api/v1/workspaces/teams-route-ws/channels/templates/ghost/materialize", ownerToken, map[string]any{
		"name": "X", "slug": "x",
	})
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "ghost") {
		t.Errorf("unknown template inside the sub-route: expected the handler's 404 naming the template, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTeams_MaterializeSoftwareTeam(t *testing.T) {
	env := setupTestEnv(t)
	ownerToken, _, _ := teamsTestEnvMembers(t, env, "teams-mat-ws")
	seedProviderWithDeadBaseURL(t, env, ownerToken, "teams-mat-ws")

	// An existing agent for the mix binding: created through the agents API
	// (the stub model factory makes generation instant).
	wProv := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/teams-mat-ws/providers", ownerToken, nil)
	var provRes struct {
		Providers []struct {
			ID string `json:"id"`
		} `json:"providers"`
	}
	_ = json.Unmarshal(wProv.Body.Bytes(), &provRes)
	providerID := provRes.Providers[0].ID

	wAgent := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/teams-mat-ws/agents", ownerToken, map[string]any{
		"name": "Beacon", "slug": "beacon", "role": "architect", "description": "d", "brief": "b",
		"provider_id": providerID, "model": "gpt-4o",
	})
	if wAgent.Code != http.StatusCreated {
		t.Fatalf("seed agent: expected 201, got %d: %s", wAgent.Code, wAgent.Body.String())
	}
	var agentRes struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	_ = json.Unmarshal(wAgent.Body.Bytes(), &agentRes)

	body := fmt.Sprintf(`{"name":"Build Dark Mode","slug":"dark-mode","purpose":"Ship dark mode.","slots":{
		"pm":{"spawn":true},"architect":{"agent_id":"%s"},"scrum-master":{"spawn":true},
		"frontend":{"spawn":true},"backend":{"spawn":true},"tester":{"spawn":true}}}`, agentRes.Agent.ID)
	w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/teams-mat-ws/channels/templates/software-team/materialize", ownerToken, json.RawMessage(body))
	if w.Code != http.StatusCreated {
		t.Fatalf("materialize: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Channel struct {
			ID          string `json:"id"`
			Slug        string `json:"slug"`
			Conventions string `json:"conventions"`
		} `json:"channel"`
		Members []struct {
			ID             string `json:"id"`
			MemberType     string `json:"member_type"`
			DisplayName    string `json:"display_name"`
			Handle         string `json:"handle"`
			Specialization string `json:"specialization"`
			Role           string `json:"role"`
		} `json:"members"`
		CreatedAgents []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"created_agents"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode materialize: %v", err)
	}
	if res.Channel.Slug != "dark-mode" || res.Channel.Conventions == "" {
		t.Errorf("expected a channel with the conventions prefill, got %+v", res.Channel)
	}
	if len(res.Members) != 7 {
		t.Fatalf("expected 7 roster rows (6 agents + human creator), got %d", len(res.Members))
	}
	if len(res.CreatedAgents) != 5 {
		t.Errorf("expected 5 spawned agents, got %d", len(res.CreatedAgents))
	}

	facilitators := 0
	for _, m := range res.Members {
		if m.Role == "facilitator" {
			facilitators++
			if m.Handle != "scrum-master" {
				t.Errorf("facilitator role on unexpected member: %+v", m)
			}
		}
		if m.MemberType == "agent" && (m.DisplayName == "" || m.Handle == "" || m.Specialization == "") {
			t.Errorf("roster row missing resolved identity/specialization: %+v", m)
		}
	}
	if facilitators != 1 {
		t.Errorf("expected exactly one facilitator on the roster, got %d", facilitators)
	}

	// The channel view carries the roster + awaiting state (null session).
	w = doRequest(env.router, http.MethodGet, "/api/v1/workspaces/teams-mat-ws/channels/"+res.Channel.ID, ownerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("channel get: expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"active_session":null`) {
		t.Errorf("expected active_session null on the view, got %s", w.Body.String())
	}

	// Spawned agents are real workspace agents with role-informed identity:
	// the pm spawn took the slot-derived slug.
	w = doRequest(env.router, http.MethodGet, "/api/v1/workspaces/teams-mat-ws/agents/pm", ownerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("spawned pm agent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Validation: missing slots list the missing ids (422).
	w = doRequest(env.router, http.MethodPost, "/api/v1/workspaces/teams-mat-ws/channels/templates/software-team/materialize", ownerToken, map[string]any{
		"name": "Half", "slug": "half", "slots": map[string]any{"pm": map[string]any{"spawn": true}},
	})
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "tester") {
		t.Errorf("missing slots: expected 422 listing missing ids, got %d: %s", w.Code, w.Body.String())
	}

	// Slug conflicts are 409.
	w = doRequest(env.router, http.MethodPost, "/api/v1/workspaces/teams-mat-ws/channels/templates/software-team/materialize", ownerToken, map[string]any{
		"name": "Dup", "slug": "dark-mode",
		"slots": map[string]any{
			"pm": map[string]any{"spawn": true}, "architect": map[string]any{"spawn": true}, "scrum-master": map[string]any{"spawn": true},
			"frontend": map[string]any{"spawn": true}, "backend": map[string]any{"spawn": true}, "tester": map[string]any{"spawn": true},
		},
	})
	if w.Code != http.StatusConflict {
		t.Errorf("slug conflict: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTeams_KickoffAndSessions(t *testing.T) {
	env := setupTestEnv(t)
	ownerToken, _, _ := teamsTestEnvMembers(t, env, "teams-kick-ws")
	seedProviderWithDeadBaseURL(t, env, ownerToken, "teams-kick-ws")

	// Materialize with all spawns (the scrum master becomes the facilitator).
	w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/teams-kick-ws/channels/templates/software-team/materialize", ownerToken, map[string]any{
		"name": "Ops Build", "slug": "ops-build",
		"slots": map[string]any{
			"pm": map[string]any{"spawn": true}, "architect": map[string]any{"spawn": true}, "scrum-master": map[string]any{"spawn": true},
			"frontend": map[string]any{"spawn": true}, "backend": map[string]any{"spawn": true}, "tester": map[string]any{"spawn": true},
		},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("materialize: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var mat struct {
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &mat)
	channelID := mat.Channel.ID
	base := "/api/v1/workspaces/teams-kick-ws/channels/" + channelID

	// Kickoff: the flagged post mints the session (channel-teams D1).
	w = doRequest(env.router, http.MethodPost, base+"/messages", ownerToken, map[string]any{
		"body": "Add dark mode to settings", "is_kickoff": true,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("kickoff: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var kick struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
		Session *struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Budget int    `json:"budget"`
		} `json:"session"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &kick); err != nil {
		t.Fatalf("decode kickoff: %v", err)
	}
	if kick.Session == nil || kick.Session.ID == "" {
		t.Fatalf("expected the minted session on the kickoff response, got %s", w.Body.String())
	}
	if kick.Session.Status != "open" {
		t.Errorf("expected an open session, got %q", kick.Session.Status)
	}
	sessionID := kick.Session.ID

	// A second kickoff is a 409 while the session is live.
	w = doRequest(env.router, http.MethodPost, base+"/messages", ownerToken, map[string]any{
		"body": "Another goal", "is_kickoff": true,
	})
	if w.Code != http.StatusConflict {
		t.Errorf("second kickoff: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// The session list shows it; the detail carries the lifecycle payload.
	w = doRequest(env.router, http.MethodGet, base+"/sessions", ownerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("sessions list: expected 200, got %d", w.Code)
	}
	var list struct {
		Sessions []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"sessions"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Sessions) != 1 || list.Sessions[0].ID != sessionID {
		t.Errorf("expected the session in the list, got %+v", list.Sessions)
	}

	w = doRequest(env.router, http.MethodGet, base+"/sessions/"+sessionID, ownerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("session detail: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"hops_used"`) || !strings.Contains(w.Body.String(), `"budget"`) {
		t.Errorf("expected the lifecycle payload on the detail, got %s", w.Body.String())
	}

	// The awaiting state rides the channel view while the session is open.
	w = doRequest(env.router, http.MethodGet, base, ownerToken, nil)
	if !strings.Contains(w.Body.String(), `"active_session":{"id":"`+sessionID+`"`) {
		t.Errorf("expected active_session on the channel view, got %s", w.Body.String())
	}

	// Member role PATCH: promote the human creator, then a second
	// facilitator (an agent) conflicts 409.
	var members struct {
		Members []struct {
			ID         string `json:"id"`
			MemberType string `json:"member_type"`
			Role       string `json:"role"`
		} `json:"members"`
	}
	w = doRequest(env.router, http.MethodGet, base+"/members", ownerToken, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &members)
	var agentMemberID, scrumID string
	for _, m := range members.Members {
		if m.MemberType == "agent" && agentMemberID == "" {
			agentMemberID = m.ID
		}
		if m.Role == "facilitator" {
			scrumID = m.ID
		}
	}
	w = doRequest(env.router, http.MethodPatch, base+"/members/"+agentMemberID, ownerToken, map[string]any{"role": "facilitator"})
	if w.Code != http.StatusConflict {
		t.Errorf("second facilitator: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	// Demoting the current facilitator works, and the agent takes over.
	w = doRequest(env.router, http.MethodPatch, base+"/members/"+scrumID, ownerToken, map[string]any{"role": "member"})
	if w.Code != http.StatusOK {
		t.Fatalf("demote: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = doRequest(env.router, http.MethodPatch, base+"/members/"+agentMemberID, ownerToken, map[string]any{"role": "facilitator"})
	if w.Code != http.StatusOK {
		t.Errorf("re-facilitate: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTeams_PermissionMatrixAndIsolation(t *testing.T) {
	env := setupTestEnv(t)
	ownerToken, memberToken, nonMemberToken := teamsTestEnvMembers(t, env, "teams-perm-ws")

	// Template materialization spawns agents, which require a workspace
	// provider; seed one so the member write below reaches the spawn path.
	seedProviderWithDeadBaseURL(t, env, ownerToken, "teams-perm-ws")

	// The builtin Member role now holds channels.read / channels.write
	// (fix-role-permission-audit D5): the teams surface is open to members —
	// templates list 200, materialize 201, and an unknown channel's sessions
	// resolve past the gate to the enumeration defense 404.
	w := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/teams-perm-ws/channels/templates", memberToken, nil)
	if w.Code != http.StatusOK {
		t.Errorf("member templates: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = doRequest(env.router, http.MethodPost, "/api/v1/workspaces/teams-perm-ws/channels/templates/software-team/materialize", memberToken, map[string]any{
		"name": "Member Team", "slug": "member-team",
		"slots": map[string]any{
			"pm": map[string]any{"spawn": true}, "architect": map[string]any{"spawn": true}, "scrum-master": map[string]any{"spawn": true},
			"frontend": map[string]any{"spawn": true}, "backend": map[string]any{"spawn": true}, "tester": map[string]any{"spawn": true},
		},
	})
	if w.Code != http.StatusCreated {
		t.Errorf("member materialize: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	w = doRequest(env.router, http.MethodGet, "/api/v1/workspaces/teams-perm-ws/channels/00000000-0000-0000-0000-000000000000/sessions", memberToken, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("member sessions: expected 404, got %d", w.Code)
	}

	// Unauthenticated requests are rejected outright.
	w = doRequest(env.router, http.MethodGet, "/api/v1/workspaces/teams-perm-ws/channels/templates", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated templates: expected 401, got %d", w.Code)
	}

	// A custom role WITHOUT channels.* is still gated at 403, proving the
	// matrix comes from the permission set, not mere membership.
	ctx := context.Background()
	noChannelsUser, noChannelsToken := createTestUser(t, env, "teams-perm-ws-nochan@example.com", "No Channels", "pwd")
	permWS, err := env.store.Workspaces().BySlug(ctx, "teams-perm-ws")
	if err != nil {
		t.Fatalf("lookup workspace: %v", err)
	}
	noChannelsRole := &domain.Role{
		WorkspaceID: permWS.ID,
		Name:        "no-channels",
		Permissions: []string{domain.WorkspaceRead},
	}
	if err := env.store.Roles().Create(ctx, noChannelsRole); err != nil {
		t.Fatalf("create no-channels role: %v", err)
	}
	addMember(t, env, permWS.ID, noChannelsUser.ID, noChannelsRole.ID)
	w = doRequest(env.router, http.MethodGet, "/api/v1/workspaces/teams-perm-ws/channels/templates", noChannelsToken, nil)
	if w.Code != http.StatusForbidden {
		t.Errorf("no-channels role templates: expected 403, got %d", w.Code)
	}

	// A workspace outsider gets the enumeration defense 404, not 403.
	w = doRequest(env.router, http.MethodGet, "/api/v1/workspaces/teams-perm-ws/channels/templates", nonMemberToken, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("non-member templates: expected 404, got %d", w.Code)
	}

	// Tenant isolation: another workspace's owner sees nothing of this one's
	// channels — 404 across reads and writes.
	otherOwner, _ := createTestUser(t, env, "teams-other-owner@example.com", "Other Owner", "pwd")
	otherWS, otherRole, _, _ := createTestWorkspaceWithRoles(t, env, "teams-other-ws", "Other Teams WS")
	addMember(t, env, otherWS.ID, otherOwner.ID, otherRole.ID)
	otherToken, err := env.issuer.Issue(context.Background(), otherOwner)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	w = doRequest(env.router, http.MethodGet, "/api/v1/workspaces/teams-perm-ws/channels/templates", otherToken, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-tenant templates: expected 404, got %d", w.Code)
	}
	w = doRequest(env.router, http.MethodPost, "/api/v1/workspaces/teams-perm-ws/channels/templates/software-team/materialize", otherToken, map[string]any{
		"name": "Intrude", "slug": "intrude",
		"slots": map[string]any{
			"pm": map[string]any{"spawn": true}, "architect": map[string]any{"spawn": true}, "scrum-master": map[string]any{"spawn": true},
			"frontend": map[string]any{"spawn": true}, "backend": map[string]any{"spawn": true}, "tester": map[string]any{"spawn": true},
		},
	})
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-tenant materialize: expected 404, got %d", w.Code)
	}
}
