package handlers_test

// Agent heartbeat handler tests (add-agent-heartbeat tasks 5.1): the guard
// middleware mirrors the router's RequireWorkspace/RequirePermission pair
// against the fake store, so the 403 paths exercise the real permission
// algebra (domain.HasPermission) — the scheduler handler-test env pattern.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/heartbeat"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// stubHeartbeatDispatch is the handler-level stand-in for the heartbeat
// service's run-now/resume face: it performs the service's scoped heartbeat
// read (an agent without a heartbeat is heartbeat.ErrNotFound before any
// dispatch) and then replays a canned outcome or error, recording calls.
type stubHeartbeatDispatch struct {
	st        store.Store
	run       *domain.HeartbeatRun
	resumed   *domain.Heartbeat
	err       error
	runCalls  int
	resumeN   int
	lastAgent string
}

func (s *stubHeartbeatDispatch) RunNow(ctx context.Context, workspaceID, agentID string) (*domain.HeartbeatRun, error) {
	s.runCalls++
	s.lastAgent = agentID
	hb, err := s.st.Heartbeats().GetHeartbeat(ctx, workspaceID, agentID)
	if err != nil {
		return nil, err
	}
	if hb == nil {
		return nil, heartbeat.ErrNotFound
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.run != nil {
		return s.run, nil
	}
	// The real service always returns a run row on a nil error; keep the
	// stub within that contract.
	return &domain.HeartbeatRun{
		ID:          "run-stub",
		WorkspaceID: workspaceID,
		HeartbeatID: hb.ID,
		AgentID:     agentID,
		SessionID:   "hb_" + agentID,
		Trigger:     domain.HeartbeatTriggerManual,
		Status:      domain.HeartbeatRunStatusRunning,
		StartedAt:   time.Now().UTC(),
	}, nil
}

func (s *stubHeartbeatDispatch) Resume(ctx context.Context, workspaceID, agentID string) (*domain.Heartbeat, error) {
	s.resumeN++
	hb, err := s.st.Heartbeats().GetHeartbeat(ctx, workspaceID, agentID)
	if err != nil {
		return nil, err
	}
	if hb == nil {
		return nil, heartbeat.ErrNotFound
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.resumed != nil {
		return s.resumed, nil
	}
	// Mirror the real Resume: re-enable, reset the streak, recompute the
	// next tick from now.
	next := time.Now().Add(30 * time.Minute).UTC()
	hb.Enabled = true
	hb.FailureStreak = 0
	hb.NextTickAt = &next
	if err := s.st.Heartbeats().PutHeartbeat(ctx, workspaceID, agentID, hb); err != nil {
		return nil, err
	}
	return hb, nil
}

// heartbeatWire mirrors the exact wire shape of one heartbeat read view
// (web/src/lib/heartbeats.ts ApiHeartbeat).
type heartbeatWire struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	Prompt      string `json:"prompt"`
	Expr        string `json:"expr"`
	HumanLabel  string `json:"human_label"`
	Delivery    struct {
		Type      string `json:"type"`
		ChannelID string `json:"channel_id"`
	} `json:"delivery"`
	Enabled       bool       `json:"enabled"`
	NextTickAt    *time.Time `json:"next_tick_at"`
	FailureStreak int        `json:"failure_streak"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// hbRunWire mirrors the exact wire shape of one run payload (ApiHeartbeatRun).
type hbRunWire struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	HeartbeatID string    `json:"heartbeat_id"`
	AgentID     string    `json:"agent_id"`
	SessionID   string    `json:"session_id"`
	Trigger     string    `json:"trigger"`
	Status      string    `json:"status"`
	StartedAt   time.Time `json:"started_at"`
}

// errorWire mirrors the standard error envelope (the scheduler tests' shape).
type hbErrorWire struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"details"`
	} `json:"error"`
}

// heartbeatHandlerEnv is a gin engine with the heartbeat handlers mounted
// under a workspace-scoped group whose middleware resolves the workspace and
// enforces the same permission checks as the router.
type heartbeatHandlerEnv struct {
	r           *gin.Engine
	st          store.Store
	ws          *domain.Workspace
	wsB         *domain.Workspace
	agent       *domain.Agent
	owner       *domain.User
	member      *domain.User
	restricted  *domain.User
	dispatch    *stubHeartbeatDispatch
	currentUser **domain.User
}

// newHeartbeatHandlerEnv builds two workspaces (acme in Asia/Jakarta, b-side
// in UTC), an owner + a plain Member (agents.read but not agents.write) + a
// user whose role lacks agents.read entirely, one agent, and mounts the
// heartbeat routes guarded the way the router guards them.
func newHeartbeatHandlerEnv(t *testing.T) *heartbeatHandlerEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	ws := &domain.Workspace{ID: "ws-acme", Slug: "acme", Name: "Acme", Timezone: "Asia/Jakarta"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	wsB := &domain.Workspace{ID: "ws-b", Slug: "b-side", Name: "B Side", Timezone: "UTC"}
	if err := st.Workspaces().Create(ctx, wsB); err != nil {
		t.Fatalf("create workspace b: %v", err)
	}

	ownerRole := &domain.Role{WorkspaceID: ws.ID, Name: "Owner", Permissions: domain.OwnerPermissions}
	if err := st.Roles().Create(ctx, ownerRole); err != nil {
		t.Fatalf("create owner role: %v", err)
	}
	memberRole := &domain.Role{WorkspaceID: ws.ID, Name: "Member", Permissions: domain.MemberPermissions}
	if err := st.Roles().Create(ctx, memberRole); err != nil {
		t.Fatalf("create member role: %v", err)
	}
	// A role with neither agents.read nor agents.write (D13: the read rides
	// agents.read).
	restrictedRole := &domain.Role{WorkspaceID: ws.ID, Name: "Spectator", Permissions: []string{domain.MembersRead}}
	if err := st.Roles().Create(ctx, restrictedRole); err != nil {
		t.Fatalf("create restricted role: %v", err)
	}
	bRole := &domain.Role{WorkspaceID: wsB.ID, Name: "Owner", Permissions: domain.OwnerPermissions}
	if err := st.Roles().Create(ctx, bRole); err != nil {
		t.Fatalf("create b role: %v", err)
	}

	owner := &domain.User{Email: "owner@example.com", Name: "Owner"}
	member := &domain.User{Email: "member@example.com", Name: "Member"}
	restricted := &domain.User{Email: "spectator@example.com", Name: "Spectator"}
	for _, u := range []*domain.User{owner, member, restricted} {
		if err := st.Users().Create(ctx, u); err != nil {
			t.Fatalf("create user %s: %v", u.Email, err)
		}
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: owner.ID, RoleID: ownerRole.ID}); err != nil {
		t.Fatalf("add owner: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: member.ID, RoleID: memberRole.ID}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: restricted.ID, RoleID: restrictedRole.ID}); err != nil {
		t.Fatalf("add restricted: %v", err)
	}
	// The owner is also a member of workspace B, so addressing an acme agent
	// under b-side proves store-level isolation (404), not membership
	// rejection.
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: wsB.ID, UserID: owner.ID, RoleID: bRole.ID}); err != nil {
		t.Fatalf("add owner to b: %v", err)
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

	dispatch := &stubHeartbeatDispatch{st: st}
	h := handlers.NewAgentHeartbeatHandlers(st.Agents(), st.Heartbeats(), st.Workspaces(), dispatch)

	var currentUser *domain.User
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.UserContextKey, currentUser)
		resolved, err := st.Workspaces().BySlug(c.Request.Context(), c.Param("ws"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "not_found"}})
			return
		}
		m, err := st.Members().Get(c.Request.Context(), resolved.ID, currentUser.ID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "not_found"}})
			return
		}
		role, err := st.Roles().ByID(c.Request.Context(), m.RoleID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "not_found"}})
			return
		}
		m.Role = role
		c.Set(handlers.WorkspaceContextKey, resolved)
		c.Set(handlers.MemberContextKey, m)
		c.Set(handlers.RoleContextKey, role)
		c.Next()
	})
	perm := func(permission string) gin.HandlerFunc {
		// Mirrors the router's RequirePermission guard (the real middleware
		// lives in internal/server, which cannot be imported here).
		return func(c *gin.Context) {
			role, ok := handlers.CurrentRole(c)
			if !ok || role == nil || !domain.HasPermission(role.Permissions, permission) {
				handlers.AbortForbidden(c, "insufficient permissions")
				return
			}
			c.Next()
		}
	}

	group := r.Group("/api/v1/workspaces/:ws")
	group.GET("/agents/:agent/heartbeat", perm(domain.AgentsRead), h.GetAgentHeartbeat)
	group.PUT("/agents/:agent/heartbeat", perm(domain.AgentsWrite), h.PutAgentHeartbeat)
	group.POST("/agents/:agent/heartbeat/run-now", perm(domain.AgentsWrite), h.RunAgentHeartbeatNow)
	group.POST("/agents/:agent/heartbeat/resume", perm(domain.AgentsWrite), h.ResumeAgentHeartbeat)

	return &heartbeatHandlerEnv{
		r: r, st: st, ws: ws, wsB: wsB, agent: agent,
		owner: owner, member: member, restricted: restricted,
		dispatch: dispatch, currentUser: &currentUser,
	}
}

func (env *heartbeatHandlerEnv) as(u *domain.User) { *env.currentUser = u }

// serve runs one request and returns the recorder.
func (env *heartbeatHandlerEnv) serve(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	env.r.ServeHTTP(rec, req)
	return rec
}

// decodeHeartbeat unwraps the {"heartbeat": ...} responses.
func decodeHeartbeat(t *testing.T, rec *httptest.ResponseRecorder) *heartbeatWire {
	t.Helper()
	var res struct {
		Heartbeat *heartbeatWire `json:"heartbeat"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode heartbeat: %v (%s)", err, rec.Body.String())
	}
	if res.Heartbeat == nil {
		t.Fatalf("decode heartbeat: missing heartbeat object (%s)", rec.Body.String())
	}
	return res.Heartbeat
}

// hbIsNull reports whether the GET payload's heartbeat key is an explicit
// JSON null (present, never-created state).
func hbIsNull(t *testing.T, body []byte) bool {
	t.Helper()
	var res struct {
		Heartbeat *heartbeatWire `json:"heartbeat"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decode heartbeat null: %v (%s)", err, string(body))
	}
	return res.Heartbeat == nil
}

// putBody is the canonical PUT payload builder.
func hbPutBody(enabled bool, expr, prompt string) string {
	return fmt.Sprintf(`{"enabled":%v,"expr":%q,"active_start":null,"active_end":null,"delivery":{"type":"creator_dm"},"prompt":%q}`,
		enabled, expr, prompt)
}

// GET on an agent without a heartbeat reads the never-created state: explicit
// null heartbeat and the embedded default checklist template (D3).
func TestHeartbeatHandlers_GetWithoutHeartbeat(t *testing.T) {
	env := newHeartbeatHandlerEnv(t)
	env.as(env.owner)
	rec := env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/agents/atlas/heartbeat", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !hbIsNull(t, rec.Body.Bytes()) {
		t.Fatalf("expected heartbeat null, got %s", rec.Body.String())
	}
	var res struct {
		DefaultPrompt string `json:"default_prompt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode default_prompt: %v", err)
	}
	if strings.TrimSpace(res.DefaultPrompt) == "" {
		t.Fatal("expected a non-empty default_prompt template")
	}
}

// PUT creates the heartbeat: an empty prompt seeds the embedded template
// (D3), the creator is recorded, and the enabled save derives next_tick_at
// from now in the workspace timezone (D4).
func TestHeartbeatHandlers_PutCreatesSeedingTemplate(t *testing.T) {
	env := newHeartbeatHandlerEnv(t)
	env.as(env.owner)

	rec := env.serve(t, http.MethodPut, "/api/v1/workspaces/acme/agents/atlas/heartbeat", hbPutBody(true, "*/30 * * * *", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on create, got %d: %s", rec.Code, rec.Body.String())
	}
	hb := decodeHeartbeat(t, rec)
	if hb.ID == "" || hb.WorkspaceID != env.ws.ID || hb.AgentID != env.agent.ID {
		t.Fatalf("unexpected identity fields: %+v", hb)
	}
	if hb.Prompt == "" {
		t.Fatal("expected the first save to seed the default checklist template")
	}
	if !hb.Enabled || hb.NextTickAt == nil {
		t.Fatalf("expected an enabled heartbeat with a derived next tick, got %+v", hb)
	}
	if hb.HumanLabel == "" {
		t.Fatal("expected the derived human_label on the write response")
	}
	if hb.Expr != "*/30 * * * *" {
		t.Fatalf("expected the canonical expression back, got %q", hb.Expr)
	}

	// The stored row matches, and a GET echoes the template as default_prompt
	// equal to the seeded prompt.
	rec = env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/agents/atlas/heartbeat", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on get, got %d", rec.Code)
	}
	var res struct {
		Heartbeat     *heartbeatWire `json:"heartbeat"`
		DefaultPrompt string         `json:"default_prompt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if res.Heartbeat == nil || res.Heartbeat.Prompt != hb.Prompt || res.Heartbeat.NextTickAt == nil {
		t.Fatalf("unexpected stored heartbeat: %+v", res.Heartbeat)
	}
	if strings.TrimSpace(res.DefaultPrompt) != strings.TrimSpace(res.Heartbeat.Prompt) {
		// ValidateHeartbeat trims the stored prompt; the template body matches
		// modulo that trim.
		t.Fatalf("expected the seeded prompt to be the embedded template, got default=%q stored=%q", res.DefaultPrompt, res.Heartbeat.Prompt)
	}
}

// PUT update keeps the prompt verbatim: an explicit empty checklist stays
// empty (skip-until-edited, D3) while earned state (failure streak, last
// tick) and identity ride through.
func TestHeartbeatHandlers_PutUpdateKeepsExplicitEmptyPrompt(t *testing.T) {
	env := newHeartbeatHandlerEnv(t)
	env.as(env.owner)

	rec := env.serve(t, http.MethodPut, "/api/v1/workspaces/acme/agents/atlas/heartbeat", hbPutBody(true, "*/30 * * * *", "check the deploy queue"))
	if rec.Code != http.StatusOK {
		t.Fatalf("seed: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Editing with an explicit empty prompt keeps it empty — and a paused
	// save clears the derived next tick (D4).
	rec = env.serve(t, http.MethodPut, "/api/v1/workspaces/acme/agents/atlas/heartbeat", hbPutBody(false, "0 9 * * *", "  "))
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	hb := decodeHeartbeat(t, rec)
	if hb.Prompt != "" {
		t.Fatalf("expected the explicit clear to stay cleared, got %q", hb.Prompt)
	}
	if hb.Enabled || hb.NextTickAt != nil {
		t.Fatalf("expected a paused heartbeat with no next tick, got enabled=%v next=%v", hb.Enabled, hb.NextTickAt)
	}
	if hb.Expr != "0 9 * * *" {
		t.Fatalf("expected the edited expression, got %q", hb.Expr)
	}

	// The seeded-template rule is create-only: the prompt stays cleared.
	rec = env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/agents/atlas/heartbeat", "")
	var res struct {
		Heartbeat *heartbeatWire `json:"heartbeat"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if res.Heartbeat == nil || res.Heartbeat.Prompt != "" {
		t.Fatalf("expected the empty checklist to persist, got %+v", res.Heartbeat)
	}
}

// Validation failures are fielded 422s (the scheduler convention) and nothing
// is created.
func TestHeartbeatHandlers_PutValidationFielded(t *testing.T) {
	env := newHeartbeatHandlerEnv(t)
	env.as(env.owner)

	cases := []struct {
		name      string
		body      string
		wantField string
	}{
		{
			name:      "invalid cron expression",
			body:      hbPutBody(true, "at nine", "check"),
			wantField: "expr",
		},
		{
			name:      "missing cron expression",
			body:      hbPutBody(true, "", "check"),
			wantField: "expr",
		},
		{
			name:      "sub-five-minute cadence",
			body:      hbPutBody(true, "* * * * *", "check"),
			wantField: "expr",
		},
		{
			name:      "equal active hours",
			body:      `{"enabled":true,"expr":"*/30 * * * *","active_start":"09:00","active_end":"09:00","delivery":{"type":"creator_dm"},"prompt":"check"}`,
			wantField: "active_end",
		},
		{
			name:      "half an active-hours window",
			body:      `{"enabled":true,"expr":"*/30 * * * *","active_start":"09:00","delivery":{"type":"creator_dm"},"prompt":"check"}`,
			wantField: "active_start",
		},
		{
			name:      "channel delivery without channel_id",
			body:      `{"enabled":true,"expr":"*/30 * * * *","active_start":null,"active_end":null,"delivery":{"type":"channel"},"prompt":"check"}`,
			wantField: "delivery.channel_id",
		},
		{
			name:      "unknown delivery type",
			body:      `{"enabled":true,"expr":"*/30 * * * *","active_start":null,"active_end":null,"delivery":{"type":"pagerduty"},"prompt":"check"}`,
			wantField: "delivery.type",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.serve(t, http.MethodPut, "/api/v1/workspaces/acme/agents/atlas/heartbeat", tc.body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
			}
			var errEnv hbErrorWire
			if err := json.Unmarshal(rec.Body.Bytes(), &errEnv); err != nil {
				t.Fatalf("decode error envelope: %v", err)
			}
			if errEnv.Error.Code != handlers.CodeInvalidRequest {
				t.Fatalf("expected code %q, got %q", handlers.CodeInvalidRequest, errEnv.Error.Code)
			}
			if len(errEnv.Error.Details) == 0 || errEnv.Error.Details[0].Field != tc.wantField {
				t.Fatalf("expected field %q in details, got %+v", tc.wantField, errEnv.Error.Details)
			}
		})
	}

	// Nothing was created by the rejected payloads.
	rec := env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/agents/atlas/heartbeat", "")
	if !hbIsNull(t, rec.Body.Bytes()) {
		t.Fatalf("expected no heartbeat after rejected saves, got %s", rec.Body.String())
	}
}

// Enabled saves land on the next future occurrence (non-null); disabled
// saves clear it (D4).
func TestHeartbeatHandlers_PutRecomputesNextTick(t *testing.T) {
	env := newHeartbeatHandlerEnv(t)
	env.as(env.owner)

	rec := env.serve(t, http.MethodPut, "/api/v1/workspaces/acme/agents/atlas/heartbeat", hbPutBody(true, "*/30 * * * *", "check"))
	if rec.Code != http.StatusOK {
		t.Fatalf("enable: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if hb := decodeHeartbeat(t, rec); hb.NextTickAt == nil {
		t.Fatal("expected a non-null next_tick_at on an enabled save")
	}

	rec = env.serve(t, http.MethodPut, "/api/v1/workspaces/acme/agents/atlas/heartbeat", hbPutBody(false, "*/30 * * * *", "check"))
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if hb := decodeHeartbeat(t, rec); hb.Enabled || hb.NextTickAt != nil {
		t.Fatalf("expected a disabled heartbeat with a null next tick, got enabled=%v next=%v", hb.Enabled, hb.NextTickAt)
	}

	// Re-enabling recomputes the next tick again.
	rec = env.serve(t, http.MethodPut, "/api/v1/workspaces/acme/agents/atlas/heartbeat", hbPutBody(true, "0 9 * * *", "check"))
	if rec.Code != http.StatusOK {
		t.Fatalf("re-enable: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if hb := decodeHeartbeat(t, rec); !hb.Enabled || hb.NextTickAt == nil {
		t.Fatalf("expected the re-enabled save to recompute the next tick, got %+v", hb)
	}
}

// The PUT rides agents.write (D13): a Member (agents.read only) is 403 and
// the heartbeat is unchanged; a role without agents.read is 403 on the read.
func TestHeartbeatHandlers_PermissionGuards(t *testing.T) {
	env := newHeartbeatHandlerEnv(t)
	env.as(env.owner)
	rec := env.serve(t, http.MethodPut, "/api/v1/workspaces/acme/agents/atlas/heartbeat", hbPutBody(true, "*/30 * * * *", "check"))
	if rec.Code != http.StatusOK {
		t.Fatalf("seed: expected 200, got %d", rec.Code)
	}

	env.as(env.member)
	if rec := env.serve(t, http.MethodPut, "/api/v1/workspaces/acme/agents/atlas/heartbeat", hbPutBody(false, "0 9 * * *", "nope")); rec.Code != http.StatusForbidden {
		t.Fatalf("member put: expected 403, got %d", rec.Code)
	}
	if rec := env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/agents/atlas/heartbeat/run-now", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("member run-now: expected 403, got %d", rec.Code)
	}
	if rec := env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/agents/atlas/heartbeat/resume", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("member resume: expected 403, got %d", rec.Code)
	}
	// The member's read passes (agents.read) and shows the unchanged row.
	rec = env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/agents/atlas/heartbeat", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("member get: expected 200, got %d", rec.Code)
	}
	if hb := decodeHeartbeat(t, rec); !hb.Enabled || hb.Prompt != "check" {
		t.Fatalf("expected the unchanged heartbeat, got %+v", hb)
	}

	env.as(env.restricted)
	if rec := env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/agents/atlas/heartbeat", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("restricted get: expected 403, got %d", rec.Code)
	}
}

// Run-now maps the service's sentinels: 200 with the run payload on success,
// 409 on an in-flight tick, 404 without a heartbeat.
func TestHeartbeatHandlers_RunNow(t *testing.T) {
	env := newHeartbeatHandlerEnv(t)
	env.as(env.owner)

	// Unknown agent → 404 before any dispatch.
	if rec := env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/agents/ghost/heartbeat/run-now", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown agent: expected 404, got %d", rec.Code)
	}

	// Known agent without a heartbeat → the service's ErrNotFound → 404.
	if rec := env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/agents/atlas/heartbeat/run-now", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("absent heartbeat: expected 404, got %d: %s", rec.Code, rec.Body.String())
	}

	// Create the heartbeat, then run-now succeeds with the run payload.
	rec := env.serve(t, http.MethodPut, "/api/v1/workspaces/acme/agents/atlas/heartbeat", hbPutBody(true, "*/30 * * * *", "check"))
	if rec.Code != http.StatusOK {
		t.Fatalf("seed: expected 200, got %d", rec.Code)
	}
	hbID := decodeHeartbeat(t, rec).ID

	rec = env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/agents/atlas/heartbeat/run-now", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("run-now: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Run *hbRunWire `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode run: %v (%s)", err, rec.Body.String())
	}
	if res.Run == nil || res.Run.ID == "" || res.Run.Trigger != "manual" {
		t.Fatalf("unexpected run payload: %+v", res.Run)
	}
	if res.Run.HeartbeatID != hbID || res.Run.SessionID != "hb_"+env.agent.ID {
		t.Fatalf("run payload lost its heartbeat linkage: %+v", res.Run)
	}
	if env.dispatch.lastAgent != env.agent.ID {
		t.Fatalf("run-now lost its scope: agent=%q", env.dispatch.lastAgent)
	}

	// An in-flight tick (wrapped domain.ErrConflict — the service's shape) is
	// a 409.
	env.dispatch.err = fmt.Errorf("heartbeat for agent %s tick still in flight: %w", env.agent.ID, domain.ErrConflict)
	if rec := env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/agents/atlas/heartbeat/run-now", ""); rec.Code != http.StatusConflict {
		t.Fatalf("conflict: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	env.dispatch.err = nil
}

// Resume goes through the service: a paused heartbeat comes back enabled with
// a recomputed next tick and a reset streak (D12); absent is 404.
func TestHeartbeatHandlers_Resume(t *testing.T) {
	env := newHeartbeatHandlerEnv(t)
	env.as(env.owner)

	// Unknown agent and absent heartbeat are 404.
	if rec := env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/agents/ghost/heartbeat/resume", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown agent resume: expected 404, got %d", rec.Code)
	}
	if rec := env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/agents/atlas/heartbeat/resume", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("absent heartbeat resume: expected 404, got %d", rec.Code)
	}

	// Save a paused heartbeat carrying a five-strike streak, then resume.
	rec := env.serve(t, http.MethodPut, "/api/v1/workspaces/acme/agents/atlas/heartbeat", hbPutBody(false, "*/30 * * * *", "check"))
	if rec.Code != http.StatusOK {
		t.Fatalf("seed: expected 200, got %d", rec.Code)
	}
	stored, err := env.st.Heartbeats().GetHeartbeat(context.Background(), env.ws.ID, env.agent.ID)
	if err != nil || stored == nil {
		t.Fatalf("load stored heartbeat: %v", err)
	}
	stored.FailureStreak = 5
	if err := env.st.Heartbeats().PutHeartbeat(context.Background(), env.ws.ID, env.agent.ID, stored); err != nil {
		t.Fatalf("seed streak: %v", err)
	}

	rec = env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/agents/atlas/heartbeat/resume", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("resume: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	hb := decodeHeartbeat(t, rec)
	if !hb.Enabled || hb.NextTickAt == nil {
		t.Fatalf("expected a resumed heartbeat with a recomputed next tick, got %+v", hb)
	}
	if hb.FailureStreak != 0 {
		t.Fatalf("expected the failure streak reset, got %d", hb.FailureStreak)
	}
}

// A workspace-B agent id is indistinguishable from unknown (404) on every
// route, even for a member of both workspaces.
func TestHeartbeatHandlers_WorkspaceIsolation(t *testing.T) {
	env := newHeartbeatHandlerEnv(t)
	env.as(env.owner)
	rec := env.serve(t, http.MethodPut, "/api/v1/workspaces/acme/agents/atlas/heartbeat", hbPutBody(true, "*/30 * * * *", "check"))
	if rec.Code != http.StatusOK {
		t.Fatalf("seed: expected 200, got %d", rec.Code)
	}

	paths := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/workspaces/b-side/agents/atlas/heartbeat", ""},
		{http.MethodPut, "/api/v1/workspaces/b-side/agents/atlas/heartbeat", hbPutBody(true, "0 9 * * *", "check")},
		{http.MethodPost, "/api/v1/workspaces/b-side/agents/atlas/heartbeat/run-now", ""},
		{http.MethodPost, "/api/v1/workspaces/b-side/agents/atlas/heartbeat/resume", ""},
	}
	for _, p := range paths {
		rec := env.serve(t, p.method, p.path, p.body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s: expected 404, got %d: %s", p.method, p.path, rec.Code, rec.Body.String())
		}
	}
}
