package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/channels"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
	"github.com/oniharnantyo/onclaw/internal/teams"
)

// stubChannelPoster records Post calls and replays a canned result — the
// handler-level stand-in for the chokepoint (the full pipeline runs in the
// internal/server router tests).
type stubChannelPoster struct {
	msg        domain.ChannelMessage
	err        error
	lastBody   string
	lastAuthor channels.Author
	posts      int
}

func (s *stubChannelPoster) Post(_ context.Context, workspaceID, channelID string, author channels.Author, body string) (domain.ChannelMessage, error) {
	s.posts++
	s.lastBody = body
	s.lastAuthor = author
	if s.err != nil {
		return domain.ChannelMessage{}, s.err
	}
	return s.msg, nil
}

// stubKickoffPoster records PostKickoff calls and replays a canned message +
// session (or error) — the handler-level stand-in for the chokepoint's
// session branch (channel-teams D1).
type stubKickoffPoster struct {
	msg        domain.ChannelMessage
	session    *domain.WorkSession
	err        error
	lastBody   string
	lastAuthor channels.Author
	kickoffs   int
}

func (s *stubKickoffPoster) PostKickoff(_ context.Context, workspaceID, channelID string, author channels.Author, body string) (domain.ChannelMessage, *domain.WorkSession, error) {
	s.kickoffs++
	s.lastBody = body
	s.lastAuthor = author
	if s.err != nil {
		return domain.ChannelMessage{}, nil, s.err
	}
	return s.msg, s.session, nil
}

// stubActiveSessions answers the awaiting-state reads for the channel views.
type stubActiveSessions struct {
	session *domain.WorkSession
}

func (s *stubActiveSessions) ActiveWorkSession(_ context.Context, workspaceID, channelID string) (*domain.WorkSession, error) {
	if s.session == nil {
		return nil, nil
	}
	return s.session, nil
}

// stubSessionLister is the handler-level stand-in for the work-session store
// reads.
type stubSessionLister struct {
	sessions []domain.WorkSession
}

func (s *stubSessionLister) GetWorkSession(_ context.Context, workspaceID, id string) (*domain.WorkSession, error) {
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			return &s.sessions[i], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *stubSessionLister) ListWorkSessions(_ context.Context, workspaceID, channelID string) ([]domain.WorkSession, error) {
	return s.sessions, nil
}

// stubChannelHub is a scripted subscription: the test sends events on the
// channel and closes it to end the stream; unsubscribe records the call.
type stubChannelHub struct {
	events       chan channels.Event
	unsubscribed bool
}

func (s *stubChannelHub) Subscribe(channelID string) (<-chan channels.Event, func()) {
	return s.events, func() { s.unsubscribed = true }
}

// channelMemberRow mirrors the roster read view's JSON shape (the view type
// itself is unexported in the handlers package).
type channelMemberRow struct {
	ID             string `json:"id"`
	MemberType     string `json:"member_type"`
	DisplayName    string `json:"display_name"`
	Handle         string `json:"handle"`
	Specialization string `json:"specialization"`
	Role           string `json:"role"`
}

// channelHandlerEnv is a gin engine with the channel handlers mounted under a
// workspace-scoped group whose middleware fakes the auth/workspace context.
type channelHandlerEnv struct {
	r          *gin.Engine
	st         store.Store
	poster     *stubChannelPoster
	kickoff    *stubKickoffPoster
	activeSess *stubActiveSessions
	sessions   *stubSessionLister
	hub        *stubChannelHub
	ws         *domain.Workspace
	user       *domain.User
}

func newChannelHandlerEnv(t *testing.T) *channelHandlerEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	ws := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(nil, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	user := &domain.User{Email: "sarah@example.com", Name: "Sarah Chen"}
	if err := st.Users().Create(nil, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: domain.RoleOwner, IsOwner: true, Permissions: domain.OwnerPermissions, BuiltIn: true}
	if err := st.Roles().Create(nil, role); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	if err := st.Members().Add(nil, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("seed membership: %v", err)
	}

	poster := &stubChannelPoster{}
	kickoff := &stubKickoffPoster{}
	activeSess := &stubActiveSessions{}
	sessions := &stubSessionLister{}
	hub := &stubChannelHub{events: make(chan channels.Event, 8)}
	materializer := teams.NewMaterializer(st.Channels(), st.Agents(), &stubAgentCreator{st: st})
	h := handlers.NewChannelHandlers(st.Channels(), st.Agents(), st.Users(), poster, kickoff, activeSess, sessions, materializer, hub, 0)
	_ = h

	r := gin.New()
	group := r.Group("/api/v1/workspaces/:ws")
	group.Use(func(c *gin.Context) {
		resolved, err := st.Workspaces().BySlug(nil, c.Param("ws"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.Set(handlers.WorkspaceContextKey, resolved)
		c.Set(handlers.UserContextKey, user)
		c.Next()
	})
	group.GET("/channels/templates", h.ListChannelTemplates)
	group.POST("/channels/templates/:template/materialize", h.MaterializeTemplate)
	group.GET("/channels", h.ListChannels)
	group.POST("/channels", h.CreateChannel)
	group.GET("/channels/:id", h.GetChannel)
	group.PATCH("/channels/:id", h.PatchChannel)
	group.DELETE("/channels/:id", h.DeleteChannel)
	group.GET("/channels/:id/members", h.ListChannelMembers)
	group.POST("/channels/:id/members", h.AddChannelMember)
	group.PATCH("/channels/:id/members/:mid", h.PatchChannelMember)
	group.DELETE("/channels/:id/members/:mid", h.RemoveChannelMember)
	group.GET("/channels/:id/messages", h.ListMessages)
	group.POST("/channels/:id/messages", h.PostMessage)
	group.GET("/channels/:id/sessions", h.ListChannelSessions)
	group.GET("/channels/:id/sessions/:sid", h.GetChannelSession)
	group.GET("/channels/:id/events", h.StreamEvents)

	return &channelHandlerEnv{r: r, st: st, poster: poster, kickoff: kickoff, activeSess: activeSess, sessions: sessions, hub: hub, ws: ws, user: user}
}

func (env *channelHandlerEnv) createChannel(t *testing.T, slug string) *domain.Channel {
	t.Helper()
	rec := httptest.NewRecorder()
	body := `{"name":"Production Ops","slug":"` + slug + `","purpose":"Coordinate production incident response.","conventions":"One incident per chain."}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed channel: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Channel domain.Channel `json:"channel"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode channel: %v", err)
	}
	return &res.Channel
}

func TestChannelHandlers_New(t *testing.T) {
	env := newChannelHandlerEnv(t)
	if env.r == nil {
		t.Fatal("expected the handler engine to be built")
	}
}

func TestChannelHandlers_CreateValidationAndConflict(t *testing.T) {
	env := newChannelHandlerEnv(t)

	// Happy path.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels", strings.NewReader(`{"name":"Production Ops","slug":"ops","purpose":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Channel domain.Channel `json:"channel"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Channel.Slug != "ops" || res.Channel.WorkspaceID != env.ws.ID {
		t.Errorf("unexpected channel: %+v", res.Channel)
	}
	if res.Channel.CreatedBy == nil || *res.Channel.CreatedBy != env.user.ID {
		t.Errorf("expected created_by to carry the authenticated user, got %+v", res.Channel.CreatedBy)
	}

	// Exact slug duplicate is a 409 (the store's uniqueness is case-
	// insensitive, but kebab-case validation is lowercase-only, so a
	// case-variant slug like "OPS" is rejected as invalid input before the
	// conflict check can apply).
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels", strings.NewReader(`{"name":"Dup","slug":"ops"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("slug conflict: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var errRes struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &errRes)
	if errRes.Error.Code != handlers.CodeConflict {
		t.Errorf("expected code %q, got %q", handlers.CodeConflict, errRes.Error.Code)
	}

	// A case-variant slug is invalid input (fielded 422), not a conflict.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels", strings.NewReader(`{"name":"Ops Uppercase","slug":"OPS"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("uppercase slug: expected 422, got %d: %s", rec.Code, rec.Body.String())
	}

	// Blank name and invalid slug are fielded 422s.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels", strings.NewReader(`{"name":"  ","slug":"Bad Slug!"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"field":"name"`) || !strings.Contains(rec.Body.String(), `"field":"slug"`) {
		t.Errorf("expected fielded details for name and slug, got %s", rec.Body.String())
	}
}

func TestChannelHandlers_GetPatchDelete(t *testing.T) {
	env := newChannelHandlerEnv(t)
	ch := env.createChannel(t, "ops")

	// By slug.
	rec := httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/ops", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get by slug: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// By id.
	rec = httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/"+ch.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get by id: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Unknown id is 404.
	rec = httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown channel: expected 404, got %d", rec.Code)
	}

	// PATCH overlays name/purpose/conventions, leaves the slug.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/acme/channels/"+ch.ID, strings.NewReader(`{"name":"Incidents","purpose":"On-call."}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Channel domain.Channel `json:"channel"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Channel.Name != "Incidents" || res.Channel.Purpose != "On-call." || res.Channel.Slug != "ops" {
		t.Errorf("unexpected patched channel: %+v", res.Channel)
	}

	// PATCH with no fields is 400.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/acme/channels/"+ch.ID, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty patch: expected 400, got %d", rec.Code)
	}

	// DELETE removes; afterwards 404.
	rec = httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/acme/channels/"+ch.ID, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/"+ch.ID, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("deleted channel: expected 404, got %d", rec.Code)
	}
}

func TestChannelHandlers_Membership(t *testing.T) {
	env := newChannelHandlerEnv(t)
	ch := env.createChannel(t, "ops")

	// The fake's agent create checks the provider reference, so seed one.
	prov := &domain.ProviderConfig{WorkspaceID: env.ws.ID, Type: "openai", Name: "Test Provider", KeyCiphertext: "sk"}
	if err := env.st.Providers().Create(nil, prov); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	agent := &domain.Agent{
		WorkspaceID: env.ws.ID,
		Slug:        "beacon",
		Name:        "Beacon",
		ProviderID:  prov.ID,
		Model:       "gpt-4",
	}
	if err := env.st.Agents().Create(nil, agent); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	// Add the agent with a specialization; the view resolves name + handle.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/"+ch.ID+"/members",
		strings.NewReader(`{"member_type":"agent","agent_id":"`+agent.ID+`","specialization":"Stakeholder comms"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add member: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var addRes struct {
		Member channelMemberRow `json:"member"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &addRes); err != nil {
		t.Fatalf("decode member: %v", err)
	}
	if addRes.Member.DisplayName != "Beacon" || addRes.Member.Handle != "beacon" {
		t.Errorf("expected resolved agent identity, got %+v", addRes.Member)
	}
	if addRes.Member.Specialization != "Stakeholder comms" {
		t.Errorf("expected specialization round-trip, got %+v", addRes.Member)
	}
	memberID := addRes.Member.ID

	// Duplicate roster row is 409.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/"+ch.ID+"/members",
		strings.NewReader(`{"member_type":"agent","agent_id":"`+agent.ID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate member: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	// Unknown agent in the workspace is 404.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/"+ch.ID+"/members",
		strings.NewReader(`{"member_type":"agent","agent_id":"00000000-0000-0000-0000-000000000000"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown agent member: expected 404, got %d", rec.Code)
	}

	// Bad member_type is a fielded 422.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/"+ch.ID+"/members",
		strings.NewReader(`{"member_type":"robot","agent_id":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad member_type: expected 422, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"field":"member_type"`) {
		t.Errorf("expected member_type detail, got %s", rec.Body.String())
	}

	// The workspace member joins too; the roster resolves both identities
	// (human handle = email local-part).
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/"+ch.ID+"/members",
		strings.NewReader(`{"member_type":"user","user_id":"`+env.user.ID+`","specialization":"Incident commander"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add user member: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/"+ch.ID+"/members", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("roster: expected 200, got %d", rec.Code)
	}
	var listRes struct {
		Members []channelMemberRow `json:"members"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listRes); err != nil {
		t.Fatalf("decode roster: %v", err)
	}
	if len(listRes.Members) != 2 {
		t.Fatalf("expected 2 roster rows, got %d", len(listRes.Members))
	}
	handles := map[string]string{}
	for _, m := range listRes.Members {
		if m.DisplayName == "" || m.Handle == "" {
			t.Errorf("roster row missing resolved identity: %+v", m)
		}
		handles[m.MemberType] = m.Handle
	}
	if handles["user"] != "sarah-chen" {
		t.Errorf("expected the shared human handle rule (sarah-chen), got %q", handles["user"])
	}

	// PATCH specialization.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/acme/channels/"+ch.ID+"/members/"+memberID,
		strings.NewReader(`{"specialization":"Exec updates"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch member: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &addRes)
	if addRes.Member.Specialization != "Exec updates" {
		t.Errorf("expected patched specialization, got %+v", addRes.Member)
	}

	// Unknown member id is 404.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/acme/channels/"+ch.ID+"/members/nope",
		strings.NewReader(`{"specialization":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown member: expected 404, got %d", rec.Code)
	}

	// Remove → 204, roster drops to 1.
	rec = httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/acme/channels/"+ch.ID+"/members/"+memberID, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove member: expected 204, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/"+ch.ID+"/members", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &listRes)
	if len(listRes.Members) != 1 {
		t.Fatalf("expected 1 roster row after removal, got %d", len(listRes.Members))
	}
}

func TestChannelHandlers_Messages(t *testing.T) {
	env := newChannelHandlerEnv(t)
	ch := env.createChannel(t, "ops")

	env.poster.msg = domain.ChannelMessage{
		ID:          "msg-1",
		WorkspaceID: env.ws.ID,
		ChannelID:   ch.ID,
		Seq:         1,
		AuthorType:  domain.ChannelMemberTypeUser,
		Body:        "hello room",
		Mentions:    []domain.Mention{},
	}

	// Post persists through the chokepoint with the authenticated user as
	// author and returns the persisted message.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/"+ch.ID+"/messages", strings.NewReader(`{"body":"@beacon hello room"}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("post: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Message domain.ChannelMessage `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode message: %v", err)
	}
	if res.Message.ID != "msg-1" || res.Message.Seq != 1 {
		t.Errorf("unexpected message: %+v", res.Message)
	}
	if env.poster.lastAuthor.UserID != env.user.ID || env.poster.lastAuthor.Type != string(domain.ChannelMemberTypeUser) {
		t.Errorf("expected user author on the chokepoint call, got %+v", env.poster.lastAuthor)
	}
	if env.poster.lastBody != "@beacon hello room" {
		t.Errorf("expected the raw body on the chokepoint call, got %q", env.poster.lastBody)
	}

	// Blank body is a fielded 422.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/acme/channels/"+ch.ID+"/messages", strings.NewReader(`{"body":"   "}`))
	req.Header.Set("Content-Type", "application/json")
	env.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("blank body: expected 422, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"field":"body"`) {
		t.Errorf("expected body detail, got %s", rec.Body.String())
	}

	// Seed two more feed rows directly and exercise the cursor.
	m2 := domain.ChannelMessage{WorkspaceID: env.ws.ID, ChannelID: ch.ID, AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: env.user.ID, Body: "second", Mentions: []domain.Mention{}}
	m3 := domain.ChannelMessage{WorkspaceID: env.ws.ID, ChannelID: ch.ID, AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: env.user.ID, Body: "third", Mentions: []domain.Mention{}}
	if err := env.st.Channels().InsertChannelMessage(nil, &m2); err != nil {
		t.Fatalf("seed m2: %v", err)
	}
	if err := env.st.Channels().InsertChannelMessage(nil, &m3); err != nil {
		t.Fatalf("seed m3: %v", err)
	}

	rec = httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/"+ch.ID+"/messages?after=1&limit=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("cursor list: expected 200, got %d", rec.Code)
	}
	var listRes struct {
		Messages []domain.ChannelMessage `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listRes); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listRes.Messages) != 1 || listRes.Messages[0].Seq != m3.Seq {
		t.Errorf("expected exactly seq %d after cursor %d with limit 1, got %+v", m3.Seq, m2.Seq, listRes.Messages)
	}

	// Invalid cursor params are 400.
	rec = httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/"+ch.ID+"/messages?after=bogus", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus after: expected 400, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/"+ch.ID+"/messages?limit=0", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("zero limit: expected 400, got %d", rec.Code)
	}
}

func TestChannelHandlers_StreamEvents(t *testing.T) {
	env := newChannelHandlerEnv(t)
	ch := env.createChannel(t, "ops")

	// One scripted event, then the hub closes the subscription — the stream
	// ends and the deferred unsubscribe has run.
	env.hub.events <- channels.Event{
		Type:    channels.EventMessagePosted,
		Seq:     7,
		Payload: json.RawMessage(`{"message_id":"m1"}`),
	}
	close(env.hub.events)

	rec := httptest.NewRecorder()
	env.r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/channels/"+ch.ID+"/events?stream=true", nil))

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", ct)
	}
	if rec.Header().Get("X-Accel-Buffering") != "no" {
		t.Error("expected X-Accel-Buffering: no")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "\"type\":\"message_posted\"") || !strings.Contains(rec.Body.String(), "\"seq\":7") {
		t.Errorf("expected the event frame with its seq on the wire, got %q", rec.Body.String())
	}
	if !env.hub.unsubscribed {
		t.Error("expected the subscription to be released when the stream ends")
	}
}
