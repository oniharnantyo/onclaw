package handlers_test

// Scheduler handler tests (integrate-scheduler tasks 5.x): the guard
// middleware mirrors the router's RequireWorkspace/RequirePermission pair
// against the fake store so the 403 paths exercise the real permission
// algebra (domain.HasPermission); everything else follows the channels
// handler-test env pattern (workspace + user injected by middleware).

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
	"github.com/oniharnantyo/onclaw/internal/scheduler"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// stubRunNow is the handler-level stand-in for the scheduler service's
// run-now dispatch: it performs the service's scoped read (unknown or
// foreign-workspace ids are ErrNotFound before any dispatch) and then
// replays a canned run or error, recording calls.
type stubRunNow struct {
	st            store.Store
	run           *domain.SchedulerRun
	err           error
	calls         int
	lastWorkspace string
	lastScheduler string
}

func (s *stubRunNow) RunNow(ctx context.Context, workspaceID, schedulerID string) (*domain.SchedulerRun, error) {
	s.calls++
	s.lastWorkspace = workspaceID
	s.lastScheduler = schedulerID
	sched, err := s.st.Schedulers().GetScheduler(ctx, workspaceID, schedulerID)
	if err != nil {
		return nil, err
	}
	if sched == nil {
		return nil, scheduler.ErrNotFound
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.run != nil {
		return s.run, nil
	}
	// The real service always returns a run row on a nil error; keep the
	// stub within that contract.
	return &domain.SchedulerRun{
		ID:          "run-stub",
		WorkspaceID: workspaceID,
		SchedulerID: schedulerID,
		SessionID:   "sched_" + schedulerID + "_stub",
		Trigger:     domain.SchedulerTriggerManual,
		Status:      domain.SchedulerRunStatusRunning,
	}, nil
}

// schedulerWire mirrors the exact wire shape of one scheduler read view.
type schedulerWire struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	AgentID     string     `json:"agent_id"`
	CreatedBy   *string    `json:"created_by"`
	Name        string     `json:"name"`
	Prompt      string     `json:"prompt"`
	Kind        string     `json:"kind"`
	Expr        string     `json:"expr"`
	RunAt       *time.Time `json:"run_at"`
	Delivery    struct {
		Type      string `json:"type"`
		ChannelID string `json:"channel_id"`
	} `json:"delivery"`
	Enabled    bool       `json:"enabled"`
	NextRunAt  *time.Time `json:"next_run_at"`
	LastRun    *struct{}  `json:"last_run"`
	HumanLabel string     `json:"human_label"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// runWire mirrors the exact wire shape of one run read view.
type runWire struct {
	ID            string    `json:"id"`
	WorkspaceID   string    `json:"workspace_id"`
	SchedulerID   string    `json:"scheduler_id"`
	SchedulerName string    `json:"scheduler_name"`
	AgentID       string    `json:"agent_id"`
	SessionID     string    `json:"session_id"`
	Trigger       string    `json:"trigger"`
	Status        string    `json:"status"`
	StartedAt     time.Time `json:"started_at"`
	DurationMS    int64     `json:"duration_ms"`
	TokensUsed    int       `json:"tokens_used"`
}

// errorWire mirrors the standard error envelope.
type errorWire struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"details"`
	} `json:"error"`
}

// schedulerHandlerEnv is a gin engine with the scheduler handlers mounted
// under a workspace-scoped group whose middleware resolves the workspace and
// enforces the same permission checks as the router.
type schedulerHandlerEnv struct {
	r           *gin.Engine
	st          store.Store
	ws          *domain.Workspace
	wsB         *domain.Workspace
	agent       *domain.Agent
	owner       *domain.User
	member      *domain.User
	restricted  *domain.User
	runNow      *stubRunNow
	currentUser **domain.User
}

// newSchedulerHandlerEnv builds two workspaces (acme in Asia/Jakarta,
// b-side in UTC), an owner + a plain Member + a user whose role lacks the
// scheduler permissions, one agent, and mounts the scheduler routes guarded
// the way the router guards them.
func newSchedulerHandlerEnv(t *testing.T) *schedulerHandlerEnv {
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
	// A role with neither scheduler.read nor scheduler.write (spec: reads
	// require the read permission).
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
	// The owner is also a member of workspace B, so addressing an acme
	// scheduler under b-side proves store-level isolation (404), not
	// membership rejection.
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

	runNow := &stubRunNow{st: st}
	h := handlers.NewSchedulerHandlers(st.Schedulers(), runNow)

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
	group.GET("/schedulers", perm(domain.SchedulerRead), h.ListSchedulers)
	group.POST("/schedulers", perm(domain.SchedulerWrite), h.CreateScheduler)
	group.GET("/scheduler-runs", perm(domain.SchedulerRead), h.ListWorkspaceSchedulerRuns)
	group.GET("/schedulers/:id", perm(domain.SchedulerRead), h.GetScheduler)
	group.PATCH("/schedulers/:id", perm(domain.SchedulerWrite), h.PatchScheduler)
	group.DELETE("/schedulers/:id", perm(domain.SchedulerWrite), h.DeleteScheduler)
	group.POST("/schedulers/:id/run", perm(domain.SchedulerWrite), h.RunSchedulerNow)
	group.GET("/schedulers/:id/runs", perm(domain.SchedulerRead), h.ListSchedulerRuns)

	return &schedulerHandlerEnv{
		r: r, st: st, ws: ws, wsB: wsB, agent: agent,
		owner: owner, member: member, restricted: restricted,
		runNow: runNow, currentUser: &currentUser,
	}
}

func (env *schedulerHandlerEnv) as(u *domain.User) { *env.currentUser = u }

// serve runs one request and returns the recorder.
func (env *schedulerHandlerEnv) serve(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
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

// createScheduler creates a recurring scheduler through the API and returns
// the decoded wire view.
func (env *schedulerHandlerEnv) createScheduler(t *testing.T, name, expr string) *schedulerWire {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"agent_id":%q,"prompt":"Summarize overnight alerts.","kind":"recurring","expr":%q,"delivery":{"type":"thread"},"enabled":true}`,
		name, env.agent.ID, expr)
	rec := env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/schedulers", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create scheduler: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	return decodeScheduler(t, rec)
}

func decodeScheduler(t *testing.T, rec *httptest.ResponseRecorder) *schedulerWire {
	t.Helper()
	var res struct {
		Scheduler *schedulerWire `json:"scheduler"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode scheduler: %v (%s)", err, rec.Body.String())
	}
	if res.Scheduler == nil {
		t.Fatalf("decode scheduler: missing scheduler object (%s)", rec.Body.String())
	}
	return res.Scheduler
}

// seedRun inserts one run row directly through the store.
func (env *schedulerHandlerEnv) seedRun(t *testing.T, schedulerID, runID string, started time.Time) {
	t.Helper()
	run := &domain.SchedulerRun{
		ID:          runID,
		WorkspaceID: env.ws.ID,
		SchedulerID: schedulerID,
		SessionID:   "sched_" + schedulerID + "_" + runID,
		Trigger:     domain.SchedulerTriggerScheduled,
		StartedAt:   started,
	}
	if err := env.st.Schedulers().StartSchedulerRun(context.Background(), run); err != nil {
		t.Fatalf("seed run %s: %v", runID, err)
	}
}

func TestSchedulerHandlers_New(t *testing.T) {
	env := newSchedulerHandlerEnv(t)
	env.as(env.owner)
	rec := env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/schedulers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on empty listing, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"schedulers":[]`) {
		t.Fatalf("expected an empty schedulers array, got %s", rec.Body.String())
	}
}

// Create/list/get roundtrip: field names, derived human label, workspace and
// creator scoping, and the store-computed next fire time on every read.
func TestSchedulerHandlers_CreateListGetRoundtrip(t *testing.T) {
	env := newSchedulerHandlerEnv(t)
	env.as(env.owner)

	s := env.createScheduler(t, "morning-digest", "0 9 * * 1-5")
	if s.ID == "" || s.WorkspaceID != env.ws.ID || s.AgentID != env.agent.ID {
		t.Fatalf("unexpected identity fields: %+v", s)
	}
	if s.CreatedBy == nil || *s.CreatedBy != env.owner.ID {
		t.Fatalf("expected created_by = owner, got %v", s.CreatedBy)
	}
	if !s.Enabled || s.Kind != "recurring" || s.Expr != "0 9 * * 1-5" {
		t.Fatalf("unexpected schedule fields: %+v", s)
	}
	if s.NextRunAt == nil {
		t.Fatal("expected a computed next_run_at on create")
	}
	if s.HumanLabel != "09:00 · Mon–Fri" {
		t.Fatalf("expected the derived human label, got %q", s.HumanLabel)
	}
	if s.Delivery.Type != "thread" || s.Delivery.ChannelID != "" {
		t.Fatalf("expected default thread delivery, got %+v", s.Delivery)
	}

	// List carries the same derived view.
	rec := env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/schedulers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}
	var list struct {
		Schedulers []schedulerWire `json:"schedulers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Schedulers) != 1 || list.Schedulers[0].ID != s.ID || list.Schedulers[0].HumanLabel != "09:00 · Mon–Fri" {
		t.Fatalf("unexpected listing: %+v", list.Schedulers)
	}

	// Get by id matches.
	rec = env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/schedulers/"+s.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := decodeScheduler(t, rec); got.ID != s.ID || got.NextRunAt == nil {
		t.Fatalf("unexpected get payload: %+v", got)
	}
}

// Validation failures are fielded 422s and nothing is created.
func TestSchedulerHandlers_CreateValidationFielded(t *testing.T) {
	env := newSchedulerHandlerEnv(t)
	env.as(env.owner)

	cases := []struct {
		name      string
		body      string
		wantField string
	}{
		{
			name:      "invalid cron expression",
			body:      `{"name":"bad","agent_id":"agent-atlas-1","prompt":"p","kind":"recurring","expr":"at nine"}`,
			wantField: "expr",
		},
		{
			name:      "missing name",
			body:      `{"agent_id":"agent-atlas-1","prompt":"p","kind":"recurring","expr":"0 9 * * 1-5"}`,
			wantField: "name",
		},
		{
			name:      "missing prompt",
			body:      `{"name":"no-prompt","agent_id":"agent-atlas-1","prompt":"","kind":"recurring","expr":"0 9 * * 1-5"}`,
			wantField: "prompt",
		},
		{
			name:      "unknown kind",
			body:      `{"name":"odd","agent_id":"agent-atlas-1","prompt":"p","kind":"hourly"}`,
			wantField: "kind",
		},
		{
			name:      "one-shot with past run_at",
			body:      `{"name":"late","agent_id":"agent-atlas-1","prompt":"p","kind":"once","run_at":"2020-01-01T09:00:00Z"}`,
			wantField: "run_at",
		},
		{
			name:      "one-shot with malformed run_at",
			body:      `{"name":"odd-time","agent_id":"agent-atlas-1","prompt":"p","kind":"once","run_at":"tomorrow at nine"}`,
			wantField: "run_at",
		},
		{
			name:      "channel delivery without channel_id",
			body:      `{"name":"chan","agent_id":"agent-atlas-1","prompt":"p","kind":"recurring","expr":"0 9 * * 1-5","delivery":{"type":"channel"}}`,
			wantField: "delivery.channel_id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/schedulers", tc.body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
			}
			var env2 errorWire
			if err := json.Unmarshal(rec.Body.Bytes(), &env2); err != nil {
				t.Fatalf("decode error envelope: %v", err)
			}
			if env2.Error.Code != handlers.CodeInvalidRequest {
				t.Fatalf("expected code %q, got %q", handlers.CodeInvalidRequest, env2.Error.Code)
			}
			if len(env2.Error.Details) == 0 || env2.Error.Details[0].Field != tc.wantField {
				t.Fatalf("expected field %q in details, got %+v", tc.wantField, env2.Error.Details)
			}
		})
	}

	// Nothing was created by the rejected payloads.
	rec := env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/schedulers", "")
	var list struct {
		Schedulers []schedulerWire `json:"schedulers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Schedulers) != 0 {
		t.Fatalf("expected no schedulers after rejected creates, got %d", len(list.Schedulers))
	}
}

// The (workspace, agent, name) triple is unique — the store's conflict maps
// to 409.
func TestSchedulerHandlers_DuplicateNameConflict(t *testing.T) {
	env := newSchedulerHandlerEnv(t)
	env.as(env.owner)
	env.createScheduler(t, "morning-digest", "0 9 * * 1-5")

	body := fmt.Sprintf(`{"name":"morning-digest","agent_id":%q,"prompt":"Different prompt, same name.","kind":"recurring","expr":"0 10 * * *"}`, env.agent.ID)
	rec := env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/schedulers", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on duplicate name, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Member-role users hold scheduler.read but not scheduler.write: reads pass,
// every mutation and run-now is 403.
func TestSchedulerHandlers_MemberCannotWrite(t *testing.T) {
	env := newSchedulerHandlerEnv(t)
	env.as(env.owner)
	s := env.createScheduler(t, "morning-digest", "0 9 * * 1-5")

	env.as(env.member)
	if rec := env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/schedulers", ""); rec.Code != http.StatusOK {
		t.Fatalf("member read: expected 200, got %d", rec.Code)
	}
	if rec := env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/schedulers", `{"name":"x","agent_id":"agent-atlas-1","prompt":"p","kind":"recurring","expr":"0 9 * * 1-5"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("member create: expected 403, got %d", rec.Code)
	}
	if rec := env.serve(t, http.MethodPatch, "/api/v1/workspaces/acme/schedulers/"+s.ID, `{"enabled":false}`); rec.Code != http.StatusForbidden {
		t.Fatalf("member patch: expected 403, got %d", rec.Code)
	}
	if rec := env.serve(t, http.MethodDelete, "/api/v1/workspaces/acme/schedulers/"+s.ID, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("member delete: expected 403, got %d", rec.Code)
	}
	if rec := env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/schedulers/"+s.ID+"/run", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("member run-now: expected 403, got %d", rec.Code)
	}
}

// A role without scheduler.read is 403 on the read surface.
func TestSchedulerHandlers_ReadRequiresSchedulerRead(t *testing.T) {
	env := newSchedulerHandlerEnv(t)
	env.as(env.owner)
	s := env.createScheduler(t, "morning-digest", "0 9 * * 1-5")

	env.as(env.restricted)
	if rec := env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/schedulers", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("restricted list: expected 403, got %d", rec.Code)
	}
	if rec := env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/schedulers/"+s.ID, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("restricted get: expected 403, got %d", rec.Code)
	}
	if rec := env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/schedulers/"+s.ID+"/runs", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("restricted runs: expected 403, got %d", rec.Code)
	}
	if rec := env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/scheduler-runs", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("restricted workspace-wide runs: expected 403, got %d", rec.Code)
	}
}

// A workspace-B scheduler id is indistinguishable from unknown (404) on every
// route, even for a member of both workspaces.
func TestSchedulerHandlers_WorkspaceIsolation(t *testing.T) {
	env := newSchedulerHandlerEnv(t)
	env.as(env.owner)
	s := env.createScheduler(t, "morning-digest", "0 9 * * 1-5")

	paths := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/workspaces/b-side/schedulers/" + s.ID, ""},
		{http.MethodPatch, "/api/v1/workspaces/b-side/schedulers/" + s.ID, `{"enabled":false}`},
		{http.MethodDelete, "/api/v1/workspaces/b-side/schedulers/" + s.ID, ""},
		{http.MethodPost, "/api/v1/workspaces/b-side/schedulers/" + s.ID + "/run", ""},
		{http.MethodGet, "/api/v1/workspaces/b-side/schedulers/" + s.ID + "/runs", ""},
	}
	for _, p := range paths {
		rec := env.serve(t, p.method, p.path, p.body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s: expected 404, got %d: %s", p.method, p.path, rec.Code, rec.Body.String())
		}
	}

	// And workspace B's own listing never shows workspace A's scheduler.
	env.as(env.owner)
	rec := env.serve(t, http.MethodGet, "/api/v1/workspaces/b-side/schedulers", "")
	var list struct {
		Schedulers []schedulerWire `json:"schedulers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode b listing: %v", err)
	}
	if len(list.Schedulers) != 0 {
		t.Fatalf("expected workspace B listing to be empty, got %d rows", len(list.Schedulers))
	}
}

// Run-now maps the scheduler service's sentinels: 409 in-flight, 404 unknown,
// 200 with the run payload on success.
func TestSchedulerHandlers_RunNowConflictNotFoundSuccess(t *testing.T) {
	env := newSchedulerHandlerEnv(t)
	env.as(env.owner)
	s := env.createScheduler(t, "morning-digest", "0 9 * * 1-5")

	// In-flight conflict (wrapped domain.ErrConflict — the service's shape).
	env.runNow.err = fmt.Errorf("scheduler %s run still in flight: %w", s.ID, domain.ErrConflict)
	if rec := env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/schedulers/"+s.ID+"/run", ""); rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on in-flight run-now, got %d: %s", rec.Code, rec.Body.String())
	}

	// Unknown scheduler: the service's scoped read answers ErrNotFound
	// (scheduler.ErrNotFound wraps domain.ErrNotFound) — 404.
	if rec := env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/schedulers/does-not-exist/run", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on unknown scheduler run-now, got %d", rec.Code)
	}

	// Success returns the run row.
	env.runNow.err = nil
	env.runNow.run = &domain.SchedulerRun{
		ID:          "run-1",
		WorkspaceID: env.ws.ID,
		SchedulerID: s.ID,
		SessionID:   "sched_" + s.ID + "_1",
		Trigger:     domain.SchedulerTriggerManual,
		Status:      domain.SchedulerRunStatusRunning,
		StartedAt:   time.Now().UTC(),
	}
	rec := env.serve(t, http.MethodPost, "/api/v1/workspaces/acme/schedulers/"+s.ID+"/run", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on run-now, got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Run *runWire `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if res.Run == nil || res.Run.ID != "run-1" || res.Run.Status != "running" || res.Run.Trigger != "manual" {
		t.Fatalf("unexpected run payload: %+v", res.Run)
	}
	if env.runNow.lastWorkspace != env.ws.ID || env.runNow.lastScheduler != s.ID {
		t.Fatalf("run-now lost its scope: ws=%q sched=%q", env.runNow.lastWorkspace, env.runNow.lastScheduler)
	}
}

// Runs listing: newest-first page with the exact total, limit/offset
// passthrough, fielded 422 on garbage pagination, and 404 for an unknown
// scheduler.
func TestSchedulerHandlers_RunsPagination(t *testing.T) {
	env := newSchedulerHandlerEnv(t)
	env.as(env.owner)
	s := env.createScheduler(t, "morning-digest", "0 9 * * 1-5")

	base := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	env.seedRun(t, s.ID, "run-1", base)
	env.seedRun(t, s.ID, "run-2", base.Add(time.Hour))
	env.seedRun(t, s.ID, "run-3", base.Add(2*time.Hour))

	// First page, newest first.
	rec := env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/schedulers/"+s.ID+"/runs?limit=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var page struct {
		Runs  []runWire `json:"runs"`
		Total int       `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode runs page: %v", err)
	}
	if page.Total != 3 || len(page.Runs) != 2 {
		t.Fatalf("expected total=3 with 2 rows, got total=%d rows=%d", page.Total, len(page.Runs))
	}
	if page.Runs[0].ID != "run-3" || page.Runs[1].ID != "run-2" {
		t.Fatalf("expected newest-first order, got %s then %s", page.Runs[0].ID, page.Runs[1].ID)
	}
	if page.Runs[0].AgentID != env.agent.ID || page.Runs[0].SchedulerName != s.Name {
		t.Fatalf("expected run enrichment, got agent_id=%q scheduler_name=%q", page.Runs[0].AgentID, page.Runs[0].SchedulerName)
	}

	// Offset skips into the tail.
	rec = env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/schedulers/"+s.ID+"/runs?limit=2&offset=2", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode offset page: %v", err)
	}
	if page.Total != 3 || len(page.Runs) != 1 || page.Runs[0].ID != "run-1" {
		t.Fatalf("unexpected offset page: total=%d rows=%+v", page.Total, page.Runs)
	}

	// Garbage limit is a fielded 422.
	rec = env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/schedulers/"+s.ID+"/runs?limit=zero", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on garbage limit, got %d", rec.Code)
	}

	// Unknown scheduler is 404.
	rec = env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/schedulers/does-not-exist/runs", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown scheduler runs, got %d", rec.Code)
	}
}

// The workspace-wide runs feed merges every scheduler's history
// newest-first, carries the exact total, and enriches rows per scheduler.
func TestSchedulerHandlers_WorkspaceWideRuns(t *testing.T) {
	env := newSchedulerHandlerEnv(t)
	env.as(env.owner)
	s1 := env.createScheduler(t, "morning-digest", "0 9 * * 1-5")
	s2 := env.createScheduler(t, "evening-digest", "0 18 * * 1-5")

	base := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	env.seedRun(t, s1.ID, "run-a1", base)
	env.seedRun(t, s1.ID, "run-a2", base.Add(9*time.Hour))
	env.seedRun(t, s2.ID, "run-b1", base.Add(3*time.Hour))

	rec := env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/scheduler-runs?limit=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var page struct {
		Runs  []runWire `json:"runs"`
		Total int       `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode workspace-wide page: %v", err)
	}
	if page.Total != 3 {
		t.Fatalf("expected exact total 3, got %d", page.Total)
	}
	if len(page.Runs) != 2 {
		t.Fatalf("expected the limit to cap the page at 2, got %d", len(page.Runs))
	}
	if page.Runs[0].ID != "run-a2" || page.Runs[1].ID != "run-b1" {
		t.Fatalf("expected merged newest-first order, got %s, %s", page.Runs[0].ID, page.Runs[1].ID)
	}
	if page.Runs[1].SchedulerName != s2.Name {
		t.Fatalf("expected per-scheduler enrichment, got %+v", page.Runs[1])
	}
}

// Lifecycle: pause clears the next fire time on every read, resume lands on
// the next future occurrence, an expression edit derives a new label, and a
// no-op patch is rejected.
func TestSchedulerHandlers_PatchPauseResumeAndReschedule(t *testing.T) {
	env := newSchedulerHandlerEnv(t)
	env.as(env.owner)
	s := env.createScheduler(t, "morning-digest", "0 9 * * 1-5")

	// Pause: next_run_at reads null while everything else is retained.
	rec := env.serve(t, http.MethodPatch, "/api/v1/workspaces/acme/schedulers/"+s.ID, `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("pause: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	paused := decodeScheduler(t, rec)
	if paused.Enabled || paused.NextRunAt != nil {
		t.Fatalf("expected a paused scheduler with no next run, got enabled=%v next=%v", paused.Enabled, paused.NextRunAt)
	}
	if paused.Expr != "0 9 * * 1-5" {
		t.Fatalf("pause must retain the expression, got %q", paused.Expr)
	}

	// The stored read shows the same.
	rec = env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/schedulers/"+s.ID, "")
	if got := decodeScheduler(t, rec); got.NextRunAt != nil {
		t.Fatalf("expected next_run_at absent after pause, got %v", got.NextRunAt)
	}

	// Resume: firing resumes at the next future occurrence.
	rec = env.serve(t, http.MethodPatch, "/api/v1/workspaces/acme/schedulers/"+s.ID, `{"enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("resume: expected 200, got %d", rec.Code)
	}
	if got := decodeScheduler(t, rec); !got.Enabled || got.NextRunAt == nil {
		t.Fatalf("expected a resumed scheduler with a next run, got enabled=%v next=%v", got.Enabled, got.NextRunAt)
	}

	// Edit reschedules from now under the new schedule (D9).
	rec = env.serve(t, http.MethodPatch, "/api/v1/workspaces/acme/schedulers/"+s.ID, `{"expr":"0 10 * * *"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expr edit: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	edited := decodeScheduler(t, rec)
	if edited.Expr != "0 10 * * *" || edited.HumanLabel != "10:00 · Daily" {
		t.Fatalf("expected the new expression and derived label, got %q / %q", edited.Expr, edited.HumanLabel)
	}
	if edited.NextRunAt == nil {
		t.Fatal("expected a recomputed next run after the schedule edit")
	}

	// A patch with no recognized fields is a house 400.
	rec = env.serve(t, http.MethodPatch, "/api/v1/workspaces/acme/schedulers/"+s.ID, `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on an empty patch, got %d", rec.Code)
	}
}

// Delete removes the scheduler; it then reads as unknown.
func TestSchedulerHandlers_Delete(t *testing.T) {
	env := newSchedulerHandlerEnv(t)
	env.as(env.owner)
	s := env.createScheduler(t, "morning-digest", "0 9 * * 1-5")

	rec := env.serve(t, http.MethodDelete, "/api/v1/workspaces/acme/schedulers/"+s.ID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on delete, got %d", rec.Code)
	}
	rec = env.serve(t, http.MethodGet, "/api/v1/workspaces/acme/schedulers/"+s.ID, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec.Code)
	}
}
