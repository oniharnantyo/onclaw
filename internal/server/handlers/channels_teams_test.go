package handlers_test

// Channel-teams handler tests (change channel-teams, tasks 6/7): template
// listing, materialization, kickoff-flagged posts, work-session reads, the
// member role patch, and the awaiting-state channel views. The full-pipeline
// versions of these flows run in the internal/server router tests.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/teams"
)

// stubAgentCreator is the handler-level stand-in for the real spawner: it
// records requests and persists a real agent row, because the fake store
// validates agent references on the roster write.
type stubAgentCreator struct {
	st       store.Store
	requests []teams.SpawnAgentRequest
}

func (s *stubAgentCreator) SpawnAgent(_ context.Context, req teams.SpawnAgentRequest) (domain.Agent, error) {
	s.requests = append(s.requests, req)
	provs, err := s.st.Providers().ListForWorkspace(nil, req.WorkspaceID)
	if err != nil || len(provs) == 0 {
		return domain.Agent{}, fmt.Errorf("stub creator: no provider in workspace %s", req.WorkspaceID)
	}
	spawned := domain.Agent{
		WorkspaceID: req.WorkspaceID,
		Name:        req.Name,
		Slug:        req.Slug,
		Role:        req.Role,
		Brief:       req.Brief,
		ProviderID:  provs[0].ID,
		Model:       "gpt-4",
	}
	if err := s.st.Agents().Create(nil, &spawned); err != nil {
		return domain.Agent{}, err
	}
	return spawned, nil
}

// materializeBody builds a materialize payload binding every slot to spawn,
// except the named slots bound to agentID.
func materializeBody(name, slug string, existing map[string]string) string {
	slots := map[string]string{
		"pm": `{"spawn":true}`, "architect": `{"spawn":true}`, "scrum-master": `{"spawn":true}`,
		"frontend": `{"spawn":true}`, "backend": `{"spawn":true}`, "tester": `{"spawn":true}`,
	}
	for slot := range existing {
		slots[slot] = `{"agent_id":"` + existing[slot] + `"}`
	}
	parts := make([]string, 0, len(slots))
	for slot, binding := range slots {
		parts = append(parts, `"`+slot+`":`+binding)
	}
	return `{"name":"` + name + `","slug":"` + slug + `","purpose":"Ship the thing.","slots":{` + strings.Join(parts, ",") + `}}`
}

// seedMaterializeWorkspace adds the provider and one existing agent the
// mix-binding payloads reference.
func seedMaterializeWorkspace(t *testing.T, env *channelHandlerEnv) *domain.Agent {
	t.Helper()
	prov := &domain.ProviderConfig{WorkspaceID: env.ws.ID, Type: "openai", Name: "Test Provider", KeyCiphertext: "sk"}
	if err := env.st.Providers().Create(nil, prov); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	agent := &domain.Agent{WorkspaceID: env.ws.ID, Slug: "beacon", Name: "Beacon", ProviderID: prov.ID, Model: "gpt-4"}
	if err := env.st.Agents().Create(nil, agent); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return agent
}

func TestChannelHandlers_ListTemplates(t *testing.T) {
	env := newChannelHandlerEnv(t)

	// The static route resolves BEFORE :id — gin serves the listing, not the
	// channel detail handler with id="templates".
	rec := httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/templates", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("templates: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Templates []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Slots []struct {
				ID             string `json:"id"`
				Title          string `json:"title"`
				Specialization string `json:"specialization"`
				Facilitator    bool   `json:"facilitator"`
			} `json:"slots"`
		} `json:"templates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Templates) != 1 || res.Templates[0].ID != "software-team" {
		t.Fatalf("expected the software-team template, got %+v", res.Templates)
	}
	if len(res.Templates[0].Slots) != 6 {
		t.Errorf("expected six slots, got %+v", res.Templates[0].Slots)
	}
	facilitators := 0
	for _, slot := range res.Templates[0].Slots {
		if slot.Facilitator {
			facilitators++
			if slot.ID != "scrum-master" {
				t.Errorf("unexpected facilitator slot %q", slot.ID)
			}
		}
	}
	if facilitators != 1 {
		t.Errorf("expected exactly one facilitator flag on the wire, got %d", facilitators)
	}
}

func TestChannelHandlers_Materialize(t *testing.T) {
	env := newChannelHandlerEnv(t)
	agent := seedMaterializeWorkspace(t, env)

	// Happy path: spawn + existing mix. Roster = 6 agents + the human creator.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/templates/software-team/materialize",
		strings.NewReader(materializeBody("Build Thing", "build-thing", map[string]string{"architect": agent.ID})))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("materialize: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Channel domain.Channel         `json:"channel"`
		Members []domain.ChannelMember `json:"members"`
		Agents  []domain.Agent         `json:"created_agents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Channel.Slug != "build-thing" || res.Channel.Conventions == "" {
		t.Errorf("expected channel with conventions prefill, got %+v", res.Channel)
	}
	if len(res.Members) != 7 {
		t.Errorf("expected 7 roster rows (6 agents + human), got %d", len(res.Members))
	}
	if len(res.Agents) != 5 {
		t.Errorf("expected 5 spawned agents, got %d", len(res.Agents))
	}
	facilitators := 0
	for _, m := range res.Members {
		if m.Role == domain.ChannelMemberRoleFacilitator {
			facilitators++
		}
	}
	if facilitators != 1 {
		t.Errorf("expected the scrum master to land as the single facilitator, got %d", facilitators)
	}

	// Unknown template is 404.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/templates/nope/materialize",
		strings.NewReader(materializeBody("X", "x", nil)))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown template: expected 404, got %d", rec.Code)
	}

	// Missing slot bindings are a fielded 422 listing the missing ids.
	rec = httptest.NewRecorder()
	body := `{"name":"X","slug":"x","slots":{"pm":{"spawn":true}}}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/templates/software-team/materialize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing slots: expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "tester") {
		t.Errorf("expected the missing-slot detail to list tester, got %s", rec.Body.String())
	}

	// A malformed binding is a fielded 422 on the slot: one binding carrying
	// both agent_id and spawn.
	broken := strings.Replace(materializeBody("X", "x-2", nil), `"pm":{"spawn":true}`, `"pm":{"spawn":true,"agent_id":"`+agent.ID+`"}`, 1)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/templates/software-team/materialize", strings.NewReader(broken))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad binding: expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"field":"slots.pm"`) {
		t.Errorf("expected fielded slots.pm detail, got %s", rec.Body.String())
	}

	// A channel slug conflict is 409.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/templates/software-team/materialize",
		strings.NewReader(materializeBody("Dup", "build-thing", nil)))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("slug conflict: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestChannelHandlers_KickoffPost(t *testing.T) {
	env := newChannelHandlerEnv(t)
	ch := env.createChannel(t, "ops")

	env.kickoff.msg = domain.ChannelMessage{ID: "kick-1", WorkspaceID: env.ws.ID, ChannelID: ch.ID, Seq: 1, AuthorType: domain.ChannelMemberTypeUser, Body: "Add dark mode", Mentions: []domain.Mention{}}
	env.kickoff.session = &domain.WorkSession{ID: "sess-1", WorkspaceID: env.ws.ID, ChannelID: ch.ID, Goal: "Add dark mode", Status: domain.WorkSessionOpen}

	// is_kickoff routes to the chokepoint's kickoff branch and returns both
	// the message and the minted session.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/"+ch.ID+"/messages",
		strings.NewReader(`{"body":"Add dark mode","is_kickoff":true}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("kickoff: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Message domain.ChannelMessage `json:"message"`
		Session *domain.WorkSession   `json:"session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Message.ID != "kick-1" || res.Session == nil || res.Session.ID != "sess-1" {
		t.Errorf("expected message + session, got %+v / %+v", res.Message, res.Session)
	}
	if env.kickoff.lastAuthor.UserID != env.user.ID {
		t.Errorf("expected the caller as the kickoff author, got %+v", env.kickoff.lastAuthor)
	}

	// An already-active session is 409.
	env.kickoff.err = fmt.Errorf("%w: channel already has an active work session", domain.ErrWorkSessionActive)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/"+ch.ID+"/messages",
		strings.NewReader(`{"body":"again","is_kickoff":true}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("active session: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	// A kickoff on a channel without a facilitator is a fielded 422.
	env.kickoff.err = fmt.Errorf("%w: channel has no facilitator member", domain.ErrInvalid)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/"+ch.ID+"/messages",
		strings.NewReader(`{"body":"again","is_kickoff":true}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("no facilitator: expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	env.kickoff.err = nil

	// A plain post ignores the kickoff branch entirely.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/"+ch.ID+"/messages",
		strings.NewReader(`{"body":"plain note"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("plain post: expected 201, got %d", rec.Code)
	}
	var plain struct {
		Session *domain.WorkSession `json:"session"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &plain)
	if plain.Session != nil {
		t.Errorf("plain post must not carry a session, got %+v", plain.Session)
	}
	if env.kickoff.kickoffs != 3 {
		t.Errorf("expected the plain post to skip the kickoff branch, kickoff calls: %d", env.kickoff.kickoffs)
	}
}

func TestChannelHandlers_Sessions(t *testing.T) {
	env := newChannelHandlerEnv(t)
	ch := env.createChannel(t, "ops")

	// Two sessions, newest first per the store contract.
	env.sessions.sessions = []domain.WorkSession{
		{ID: "sess-new", WorkspaceID: env.ws.ID, ChannelID: ch.ID, Goal: "g2", Status: domain.WorkSessionPaused, PauseReason: "awaiting-human"},
		{ID: "sess-old", WorkspaceID: env.ws.ID, ChannelID: ch.ID, Goal: "g1", Status: domain.WorkSessionClosed, Summary: "done"},
	}

	rec := httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/"+ch.ID+"/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}
	var list struct {
		Sessions []domain.WorkSession `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Sessions) != 2 || list.Sessions[0].ID != "sess-new" {
		t.Errorf("expected newest-first sessions, got %+v", list.Sessions)
	}

	// Detail with the full lifecycle payload.
	rec = httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/"+ch.ID+"/sessions/sess-new", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("detail: expected 200, got %d", rec.Code)
	}
	var detail struct {
		Session domain.WorkSession `json:"session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Session.Status != domain.WorkSessionPaused || detail.Session.PauseReason != "awaiting-human" {
		t.Errorf("unexpected session detail: %+v", detail.Session)
	}

	// Unknown session is 404; another channel's session is 404 too.
	rec = httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/"+ch.ID+"/sessions/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown session: expected 404, got %d", rec.Code)
	}
	other := env.sessions.sessions[0]
	other.ChannelID = "chan-other"
	env.sessions.sessions = []domain.WorkSession{other, env.sessions.sessions[1]}
	rec = httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/"+ch.ID+"/sessions/sess-new", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("foreign-channel session: expected 404, got %d", rec.Code)
	}
}

func TestChannelHandlers_ActiveSessionInViews(t *testing.T) {
	env := newChannelHandlerEnv(t)
	ch := env.createChannel(t, "ops")

	// Without a live session the view carries null.
	rec := httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/"+ch.ID, nil))
	if !strings.Contains(rec.Body.String(), `"active_session":null`) {
		t.Errorf("expected active_session null, got %s", rec.Body.String())
	}

	// With one, the awaiting state rides the view (single + list).
	env.activeSess.session = &domain.WorkSession{ID: "sess-live", WorkspaceID: env.ws.ID, ChannelID: ch.ID, Status: domain.WorkSessionOpen}
	rec = httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/"+ch.ID, nil))
	if !strings.Contains(rec.Body.String(), `"active_session":{"id":"sess-live"`) {
		t.Errorf("expected active_session on the single view, got %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels", nil))
	if !strings.Contains(rec.Body.String(), `"active_session":{"id":"sess-live"`) {
		t.Errorf("expected active_session on the list view, got %s", rec.Body.String())
	}
}

func TestChannelHandlers_MemberRolePatch(t *testing.T) {
	env := newChannelHandlerEnv(t)
	ch := env.createChannel(t, "ops")

	prov := &domain.ProviderConfig{WorkspaceID: env.ws.ID, Type: "openai", Name: "P", KeyCiphertext: "sk"}
	if err := env.st.Providers().Create(nil, prov); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	agent := &domain.Agent{WorkspaceID: env.ws.ID, Slug: "beacon", Name: "Beacon", ProviderID: prov.ID, Model: "gpt-4"}
	if err := env.st.Agents().Create(nil, agent); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	other := &domain.Agent{WorkspaceID: env.ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: prov.ID, Model: "gpt-4"}
	if err := env.st.Agents().Create(nil, other); err != nil {
		t.Fatalf("seed second agent: %v", err)
	}

	addMember := func(t *testing.T, agentID string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/"+ch.ID+"/members",
			strings.NewReader(`{"member_type":"agent","agent_id":"`+agentID+`"}`))
		req.Header.Set("Content-Type", "application/json")
		env.r.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("add member: expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var res struct {
			Member channelMemberRow `json:"member"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		return res.Member.ID
	}

	first := addMember(t, agent.ID)
	second := addMember(t, other.ID)

	// Promote the first to facilitator.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/acme/channels/"+ch.ID+"/members/"+first,
		strings.NewReader(`{"role":"facilitator"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("promote: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Member channelMemberRow `json:"member"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Member.Role != "facilitator" {
		t.Errorf("expected facilitator role on the view, got %q", res.Member.Role)
	}

	// A second facilitator is a 409 (design D2: at most one per channel).
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/acme/channels/"+ch.ID+"/members/"+second,
		strings.NewReader(`{"role":"facilitator"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second facilitator: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	// An unknown role value is a fielded 422.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/acme/channels/"+ch.ID+"/members/"+second,
		strings.NewReader(`{"role":"chair"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad role: expected 422, got %d", rec.Code)
	}

	// Demote back to member.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/acme/channels/"+ch.ID+"/members/"+first,
		strings.NewReader(`{"role":"member"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("demote: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
