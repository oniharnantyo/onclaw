package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/skillcuration"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// The skill-curation review surface (add-skill-curation-from-traces
// 6.1/8.2): the candidates queue, the approve/reject human gate, the
// pattern wiki, and the audit. The env wires the REAL workspace/permission
// middlewares over the fake store so the Member-read/Owner-Admin-write
// matrix is exercised end to end; auth is stubbed to the bearer token
// naming the user id.

type curationEnv struct {
	r         *gin.Engine
	st        store.Store
	ws        *domain.Workspace
	agent     *domain.Agent
	onClawDir string
	wikiDir   string
	ownerID   string
	adminID   string
	memberID  string
	cfg       skillcuration.Config
}

func newSkillCurationEnv(t *testing.T, cfg skillcuration.Config) *curationEnv {
	t.Helper()
	return newSkillCurationEnvWithCycle(t, cfg, nil)
}

func newSkillCurationEnvWithCycle(t *testing.T, cfg skillcuration.Config, cycle handlers.SkillCurationCycle) *curationEnv {
	t.Helper()
	return newSkillCurationEnvWith(t, cfg, cycle, nil, nil)
}

// newSkillCurationEnvWith builds the review surface with the optional
// capabilities wired: the cycle seam (run/status), the deleted-source
// checker (the list's source_available marker), and the manual-retry seam.
func newSkillCurationEnvWith(t *testing.T, cfg skillcuration.Config, cycle handlers.SkillCurationCycle, checker handlers.SessionChecker, retry handlers.SkillCurationRetryer) *curationEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	ctx := context.Background()
	st := storefake.New()

	ws := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	roles := make(map[string]*domain.Role, 3)
	for name, perms := range map[string][]string{
		domain.RoleOwner:  domain.OwnerPermissions,
		domain.RoleAdmin:  domain.AdminPermissions,
		domain.RoleMember: domain.MemberPermissions,
	} {
		role := &domain.Role{WorkspaceID: ws.ID, Name: name, IsOwner: name == domain.RoleOwner, Permissions: perms, BuiltIn: true}
		if err := st.Roles().Create(ctx, role); err != nil {
			t.Fatalf("seed role %s: %v", name, err)
		}
		roles[name] = role
	}

	userIDs := make(map[string]string, 3)
	for _, who := range []string{"owner", "admin", "member"} {
		displayName := map[string]string{"owner": "Owner", "admin": "Admin", "member": "Member"}[who]
		u := &domain.User{Email: who + "@acme.test", Name: displayName}
		if err := st.Users().Create(ctx, u); err != nil {
			t.Fatalf("seed user %s: %v", who, err)
		}
		userIDs[who] = u.ID
		roleName := map[string]string{"owner": domain.RoleOwner, "admin": domain.RoleAdmin, "member": domain.RoleMember}[who]
		if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: u.ID, RoleID: roles[roleName].ID}); err != nil {
			t.Fatalf("add member %s: %v", who, err)
		}
	}

	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI", Enabled: true}
	if err := st.Providers().Create(ctx, p); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	agent := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: p.ID, Model: "gpt-4o"}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	onClawDir := t.TempDir()
	wikiDir := filepath.Join(onClawDir, "wiki")
	lister := func(_ context.Context, _ string) ([]skillcuration.Page, error) {
		return skillcuration.NewWiki(wikiDir).List()
	}
	reader := func(_ context.Context, _, _, skillName string) (string, bool, error) {
		data, err := os.ReadFile(filepath.Join(domain.AgentSkillsDir(onClawDir, ws.Slug, agent.Slug), skillName, "SKILL.md"))
		if os.IsNotExist(err) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		return string(data), true, nil
	}
	approver := skillcuration.NewApprover(
		st.SkillCandidates(), st.Agents(), st.Workspaces(), st.WorkspaceSkills(),
		reader, func() []string { return []string{"grafana.query", "files.write"} },
		func(context.Context, string) skillcuration.Config { return cfg },
		onClawDir, slog.New(slog.DiscardHandler),
	)
	h := handlers.NewSkillCurationHandlers(st.SkillCandidates(), approver, lister,
		handlers.WithSkillCurationCycle(cycle),
		handlers.WithSessionChecker(checker),
		handlers.WithSkillCurationRetry(retry))

	// The REAL middlewares over the fake store: RequireWorkspace resolves
	// the membership and role; RequirePermission enforces the catalog.
	mw := server.NewMiddlewares(st.Users(), st.Workspaces(), st.Members(), st.Roles(), nil, mustTestAuthorizer(t, st))

	r := gin.New()
	group := r.Group("/api/v1/workspaces/:ws")
	group.Use(func(c *gin.Context) {
		u, err := st.Users().ByID(c.Request.Context(), c.GetHeader("Authorization"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
			return
		}
		c.Set(handlers.UserContextKey, u)
		c.Next()
	})
	group.Use(mw.RequireWorkspace("ws"))
	group.GET("/skill-curation/candidates", mw.RequirePermission(domain.SkillsRead), h.ListCandidates)
	group.GET("/skill-curation/candidates/:id", mw.RequirePermission(domain.SkillsRead), h.GetCandidate)
	group.POST("/skill-curation/candidates/:id/approve", mw.RequirePermission(domain.SkillsWrite), h.Approve)
	group.POST("/skill-curation/candidates/:id/reject", mw.RequirePermission(domain.SkillsWrite), h.Reject)
	if retry != nil {
		group.POST("/skill-curation/candidates/:id/retry", mw.RequirePermission(domain.SkillsWrite), h.Retry)
	}
	group.GET("/skill-curation/patterns", mw.RequirePermission(domain.SkillsRead), h.ListPatterns)
	group.GET("/skill-curation/audit", mw.RequirePermission(domain.SkillsRead), h.ListAudit)
	if cycle != nil {
		group.POST("/skill-curation/cycle/run", mw.RequirePermission(domain.SkillsWrite), h.RunCycle)
		group.GET("/skill-curation/cycle/status", mw.RequirePermission(domain.SkillsRead), h.CycleStatus)
	}

	return &curationEnv{
		r: r, st: st, ws: ws, agent: agent, onClawDir: onClawDir, wikiDir: wikiDir,
		ownerID: userIDs["owner"], adminID: userIDs["admin"], memberID: userIDs["member"],
		cfg: cfg,
	}
}

// doCurationRequest issues a request whose bearer token names the user id.
func doCurationRequest(r *gin.Engine, method, path, userID string, body any) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = strings.NewReader(string(raw))
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("Authorization", userID)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// seedCandidate stores one pending candidate for the env's agent.
func seedCandidate(t *testing.T, env *curationEnv, skillName string) *domain.SkillCandidate {
	t.Helper()
	candidate := &domain.SkillCandidate{
		WorkspaceID:      env.ws.ID,
		AgentID:          env.agent.ID,
		ClusterID:        "cl-http",
		SkillName:        skillName,
		Status:           domain.SkillCandidatePending,
		ProposedContent:  "# " + skillName + "\n\nThe procedure.\n\ndescription: do the " + skillName + " procedure end to end\n",
		EvidenceEventIDs: []string{"sess-deleted-a", "sess-deleted-b"},
		CitedPatternRefs: []string{"deploy-guard"},
	}
	if err := env.st.SkillCandidates().Save(context.Background(), candidate); err != nil {
		t.Fatalf("seed candidate: %v", err)
	}
	return candidate
}

// seedFailedCandidate stores one failed (extraction-dropped) candidate for
// the env's agent.
func seedFailedCandidate(t *testing.T, env *curationEnv, skillName, reason string) *domain.SkillCandidate {
	t.Helper()
	candidate := &domain.SkillCandidate{
		WorkspaceID: env.ws.ID,
		AgentID:     env.agent.ID,
		ClusterID:   "cl-http",
		SkillName:   skillName,
		Status:      domain.SkillCandidateFailed,
		Reason:      reason,
	}
	if err := env.st.SkillCandidates().Save(context.Background(), candidate); err != nil {
		t.Fatalf("seed failed candidate: %v", err)
	}
	return candidate
}

func TestSkillCuration_PermissionMatrix(t *testing.T) {
	env := newSkillCurationEnv(t, defaultCurationCfg())
	candidate := seedCandidate(t, env, "deploy-rollback")

	t.Run("member reads the queue, the detail, patterns, and audit", func(t *testing.T) {
		for _, tc := range []struct{ method, path string }{
			{http.MethodGet, "/api/v1/workspaces/acme/skill-curation/candidates?status=pending"},
			{http.MethodGet, "/api/v1/workspaces/acme/skill-curation/candidates/" + candidate.ID},
			{http.MethodGet, "/api/v1/workspaces/acme/skill-curation/patterns"},
			{http.MethodGet, "/api/v1/workspaces/acme/skill-curation/audit"},
		} {
			rec := doCurationRequest(env.r, tc.method, tc.path, env.memberID, nil)
			if rec.Code != http.StatusOK {
				t.Errorf("%s %s = %d, want 200: %s", tc.method, tc.path, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("member cannot approve or reject (403)", func(t *testing.T) {
		for _, tc := range []struct {
			method, path string
			body         any
		}{
			{http.MethodPost, "/api/v1/workspaces/acme/skill-curation/candidates/" + candidate.ID + "/approve", map[string]any{}},
			{http.MethodPost, "/api/v1/workspaces/acme/skill-curation/candidates/" + candidate.ID + "/reject", map[string]string{"reason": "not allowed"}},
		} {
			rec := doCurationRequest(env.r, tc.method, tc.path, env.memberID, tc.body)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s %s = %d, want 403", tc.method, tc.path, rec.Code)
			}
		}
		// The member's blocked attempts left the candidate untouched.
		row, _ := env.st.SkillCandidates().Get(context.Background(), env.ws.ID, candidate.ID)
		if row.Status != domain.SkillCandidatePending {
			t.Errorf("blocked attempts mutated the candidate: %q", row.Status)
		}
	})

	t.Run("admin can approve (skills.write)", func(t *testing.T) {
		rec := doCurationRequest(env.r, http.MethodPost,
			"/api/v1/workspaces/acme/skill-curation/candidates/"+candidate.ID+"/approve", env.adminID, map[string]any{})
		if rec.Code != http.StatusOK {
			t.Fatalf("admin approve = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown candidate is 404 for a reader", func(t *testing.T) {
		rec := doCurationRequest(env.r, http.MethodGet,
			"/api/v1/workspaces/acme/skill-curation/candidates/no-such-id", env.memberID, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("unknown detail = %d, want 404", rec.Code)
		}
	})
}

func defaultCurationCfg() skillcuration.Config {
	cfg := skillcuration.DefaultConfig()
	cfg.CatalogBudgetPerAgent = 5
	return cfg
}

func TestSkillCuration_ApproveLifecycle(t *testing.T) {
	env := newSkillCurationEnv(t, defaultCurationCfg())

	// The wiki carries one pattern page; rejection must leave it intact.
	wiki := skillcuration.NewWiki(env.wikiDir)
	if err := wiki.Create(skillcuration.Page{
		Slug: "deploy-guard", Title: "Deploy guard", Status: skillcuration.PageActive,
		EvidenceRuns: []string{"sess-deleted-a"}, Body: "Check health first.",
	}); err != nil {
		t.Fatalf("seed wiki page: %v", err)
	}

	approved := seedCandidate(t, env, "deploy-rollback")
	rejected := seedCandidate(t, env, "log-rotation")

	t.Run("owner lists pending candidates", func(t *testing.T) {
		rec := doCurationRequest(env.r, http.MethodGet,
			"/api/v1/workspaces/acme/skill-curation/candidates?status=pending", env.ownerID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Candidates []domain.SkillCandidate `json:"candidates"`
			Count      int                     `json:"count"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if body.Count != 2 || len(body.Candidates) != 2 {
			t.Fatalf("pending list = %d rows (count %d), want 2", len(body.Candidates), body.Count)
		}
	})

	t.Run("detail renders evidence ids without hydration", func(t *testing.T) {
		rec := doCurationRequest(env.r, http.MethodGet,
			"/api/v1/workspaces/acme/skill-curation/candidates/"+approved.ID, env.ownerID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("detail = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Candidate domain.SkillCandidate `json:"candidate"`
			Evidence  struct {
				EventIDs      []string `json:"event_ids"`
				CitedPatterns []string `json:"cited_patterns"`
			} `json:"evidence"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode detail: %v", err)
		}
		if len(body.Evidence.EventIDs) != 2 || body.Evidence.EventIDs[0] != "sess-deleted-a" {
			t.Errorf("evidence event ids drifted: %v", body.Evidence.EventIDs)
		}
		if len(body.Evidence.CitedPatterns) != 1 || body.Evidence.CitedPatterns[0] != "deploy-guard" {
			t.Errorf("cited patterns drifted: %v", body.Evidence.CitedPatterns)
		}
	})

	t.Run("reject without a reason is 400 and stores nothing", func(t *testing.T) {
		for _, body := range []any{
			map[string]string{},
			map[string]string{"reason": "   "},
		} {
			rec := doCurationRequest(env.r, http.MethodPost,
				"/api/v1/workspaces/acme/skill-curation/candidates/"+rejected.ID+"/reject", env.ownerID, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("blank-reason reject = %d, want 400: %s", rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("reject with a reason records the audit and leaves the wiki intact", func(t *testing.T) {
		rec := doCurationRequest(env.r, http.MethodPost,
			"/api/v1/workspaces/acme/skill-curation/candidates/"+rejected.ID+"/reject", env.ownerID,
			map[string]string{"reason": "covered by the runbook"})
		if rec.Code != http.StatusOK {
			t.Fatalf("reject = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Candidate domain.SkillCandidate `json:"candidate"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode reject: %v", err)
		}
		if body.Candidate.Status != domain.SkillCandidateRejected || body.Candidate.Reason != "covered by the runbook" {
			t.Fatalf("rejected row drifted: %+v", body.Candidate)
		}
		// Wiki untouched.
		pages, err := wiki.List()
		if err != nil || len(pages) != 1 || pages[0].Slug != "deploy-guard" || pages[0].Status != skillcuration.PageActive {
			t.Fatalf("wiki changed across rejection: %+v (%v)", pages, err)
		}
	})

	t.Run("approve materializes the skill and enters probation", func(t *testing.T) {
		// The evidence ids name sessions that exist nowhere — approval must
		// not look them up (deleted-source rule).
		rec := doCurationRequest(env.r, http.MethodPost,
			"/api/v1/workspaces/acme/skill-curation/candidates/"+approved.ID+"/approve", env.ownerID, map[string]any{})
		if rec.Code != http.StatusOK {
			t.Fatalf("approve = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Candidate domain.SkillCandidate `json:"candidate"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode approve: %v", err)
		}
		if body.Candidate.Status != domain.SkillCandidateProvisional {
			t.Fatalf("approved row status = %q, want provisional", body.Candidate.Status)
		}

		skillDir := filepath.Join(domain.AgentSkillsDir(env.onClawDir, env.ws.Slug, env.agent.Slug), "deploy-rollback")
		for _, f := range []string{"SKILL.md", "PURPOSE.md"} {
			if _, err := os.Stat(filepath.Join(skillDir, f)); err != nil {
				t.Errorf("materialized %s missing: %v", f, err)
			}
		}
		purpose, err := os.ReadFile(filepath.Join(skillDir, "PURPOSE.md"))
		if err != nil || !strings.Contains(string(purpose), env.ownerID) || !strings.Contains(string(purpose), "deploy-guard") {
			t.Errorf("PURPOSE.md must cite patterns and reviewer: %v %q", err, purpose)
		}
	})

	t.Run("audit lists both verdicts with reviewer identity", func(t *testing.T) {
		rec := doCurationRequest(env.r, http.MethodGet,
			"/api/v1/workspaces/acme/skill-curation/audit", env.ownerID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("audit = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Entries []domain.SkillImpactEntry `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode audit: %v", err)
		}
		if len(body.Entries) != 2 {
			t.Fatalf("audit entries = %d, want 2", len(body.Entries))
		}
		sawApproved, sawRejected := false, false
		for _, e := range body.Entries {
			switch e.Verdict {
			case domain.SkillImpactApproved:
				sawApproved = e.Reviewer == env.ownerID && e.Diff != ""
			case domain.SkillImpactRejected:
				sawRejected = e.Reason == "covered by the runbook" && e.Reviewer == env.ownerID
			}
		}
		if !sawApproved || !sawRejected {
			t.Errorf("audit entries incomplete: approved=%v rejected=%v", sawApproved, sawRejected)
		}
	})

	t.Run("audit since filter accepts RFC 3339 and rejects garbage", func(t *testing.T) {
		rec := doCurationRequest(env.r, http.MethodGet,
			"/api/v1/workspaces/acme/skill-curation/audit?since="+time.Now().UTC().Add(time.Hour).Format(time.RFC3339), env.ownerID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("future since = %d, want 200 (empty)", rec.Code)
		}
		var body struct {
			Entries []domain.SkillImpactEntry `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Entries) != 0 {
			t.Errorf("future since must filter everything out, got %d entries (%v)", len(body.Entries), err)
		}
		bad := doCurationRequest(env.r, http.MethodGet,
			"/api/v1/workspaces/acme/skill-curation/audit?since=yesterday", env.ownerID, nil)
		if bad.Code != http.StatusBadRequest {
			t.Errorf("garbage since = %d, want 400", bad.Code)
		}
	})
}

func TestSkillCuration_ApproveConflicts(t *testing.T) {
	t.Run("budget refusal is 409 naming the remedy", func(t *testing.T) {
		cfg := defaultCurationCfg()
		cfg.CatalogBudgetPerAgent = 1
		env := newSkillCurationEnv(t, cfg)

		first := seedCandidate(t, env, "first-skill")
		rec := doCurationRequest(env.r, http.MethodPost,
			"/api/v1/workspaces/acme/skill-curation/candidates/"+first.ID+"/approve", env.ownerID, map[string]any{})
		if rec.Code != http.StatusOK {
			t.Fatalf("first approve = %d: %s", rec.Code, rec.Body.String())
		}

		second := seedCandidate(t, env, "second-skill")
		rec = doCurationRequest(env.r, http.MethodPost,
			"/api/v1/workspaces/acme/skill-curation/candidates/"+second.ID+"/approve", env.ownerID, map[string]any{})
		if rec.Code != http.StatusConflict {
			t.Fatalf("budget refusal = %d, want 409: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "disable or merge existing curated skills first") {
			t.Errorf("refusal must name the remedy: %s", rec.Body.String())
		}
		// The refused candidate stays pending.
		row, _ := env.st.SkillCandidates().Get(context.Background(), env.ws.ID, second.ID)
		if row.Status != domain.SkillCandidatePending {
			t.Errorf("refused candidate status = %q, want pending", row.Status)
		}
	})

	t.Run("race collision is 409 and the candidate stays pending", func(t *testing.T) {
		env := newSkillCurationEnv(t, defaultCurationCfg())
		candidate := seedCandidate(t, env, "deploy-rollback")

		// A same-named agent skill appeared between proposal and approval.
		skillDir := filepath.Join(domain.AgentSkillsDir(env.onClawDir, env.ws.Slug, env.agent.Slug), "deploy-rollback")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("seed colliding dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# hand-installed\n"), 0o644); err != nil {
			t.Fatalf("seed colliding skill: %v", err)
		}

		rec := doCurationRequest(env.r, http.MethodPost,
			"/api/v1/workspaces/acme/skill-curation/candidates/"+candidate.ID+"/approve", env.ownerID, map[string]any{})
		if rec.Code != http.StatusConflict {
			t.Fatalf("race collision = %d, want 409: %s", rec.Code, rec.Body.String())
		}
		row, _ := env.st.SkillCandidates().Get(context.Background(), env.ws.ID, candidate.ID)
		if row.Status != domain.SkillCandidatePending {
			t.Errorf("collision-blocked candidate = %q, want pending", row.Status)
		}
	})

	t.Run("double approve is 409", func(t *testing.T) {
		env := newSkillCurationEnv(t, defaultCurationCfg())
		candidate := seedCandidate(t, env, "deploy-rollback")
		path := "/api/v1/workspaces/acme/skill-curation/candidates/" + candidate.ID + "/approve"
		if rec := doCurationRequest(env.r, http.MethodPost, path, env.ownerID, map[string]any{}); rec.Code != http.StatusOK {
			t.Fatalf("first approve = %d: %s", rec.Code, rec.Body.String())
		}
		if rec := doCurationRequest(env.r, http.MethodPost, path, env.ownerID, map[string]any{}); rec.Code != http.StatusConflict {
			t.Errorf("double approve = %d, want 409", rec.Code)
		}
	})

	t.Run("patterns list renders the wiki with status and successor", func(t *testing.T) {
		env := newSkillCurationEnv(t, defaultCurationCfg())
		wiki := skillcuration.NewWiki(env.wikiDir)
		if err := wiki.Create(skillcuration.Page{
			Slug: "deploy-guard", Title: "Deploy guard", Status: skillcuration.PageActive,
			EvidenceRuns: []string{"sess-a"}, Body: "Check health first.",
		}); err != nil {
			t.Fatalf("seed page: %v", err)
		}
		if err := wiki.Supersede("deploy-guard", skillcuration.Page{
			Slug: "deploy-guard-v2", Title: "Deploy guard v2",
			EvidenceRuns: []string{"sess-b"}, Body: "Check health and quotas.",
		}); err != nil {
			t.Fatalf("supersede: %v", err)
		}

		rec := doCurationRequest(env.r, http.MethodGet,
			"/api/v1/workspaces/acme/skill-curation/patterns", env.memberID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("patterns = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Patterns []struct {
				Slug         string   `json:"slug"`
				Title        string   `json:"title"`
				Status       string   `json:"status"`
				SupersededBy string   `json:"superseded_by"`
				EvidenceRuns []string `json:"evidence_runs"`
			} `json:"patterns"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode patterns: %v", err)
		}
		if len(body.Patterns) != 2 {
			t.Fatalf("patterns = %d, want 2 (active and superseded)", len(body.Patterns))
		}
		bySlug := map[string]struct {
			Status       string
			SupersededBy string
		}{}
		for _, p := range body.Patterns {
			bySlug[p.Slug] = struct {
				Status       string
				SupersededBy string
			}{p.Status, p.SupersededBy}
		}
		old, ok := bySlug["deploy-guard"]
		if !ok || old.Status != "superseded" || old.SupersededBy != "deploy-guard-v2" {
			t.Errorf("superseded pointer missing: %+v", bySlug)
		}
		fresh, ok := bySlug["deploy-guard-v2"]
		if !ok || fresh.Status != "active" || fresh.SupersededBy != "" {
			t.Errorf("successor page wrong: %+v", bySlug)
		}
	})
}

// ---------------------------------------------------------------------------
// The cycle endpoints (add-skill-curation-from-traces 7.1/8.2): run is the
// Owner/Admin human-trigger tier (skills.write), status is member-readable;
// a double trigger maps the wrapped domain.ErrConflict to 409. The seam is a
// stub — the real Cycle's claim semantics are the skillcuration package's
// tests.
// ---------------------------------------------------------------------------

type cycleStub struct {
	mu       sync.Mutex
	runErr   error
	status   skillcuration.CycleStatus
	runCalls int
	lastWSID string
}

func (s *cycleStub) RunNow(_ context.Context, workspaceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runCalls++
	s.lastWSID = workspaceID
	return s.runErr
}

func (s *cycleStub) Status(_ context.Context, workspaceID string) skillcuration.CycleStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.WorkspaceID = workspaceID
	return s.status
}

func TestSkillCurationCycleEndpoints(t *testing.T) {
	newEnv := func(runErr error, status skillcuration.CycleStatus) (*curationEnv, *cycleStub) {
		stub := &cycleStub{runErr: runErr, status: status}
		return newSkillCurationEnvWithCycle(t, defaultCurationCfg(), stub), stub
	}

	t.Run("admin triggers a cycle and reads the status back", func(t *testing.T) {
		env, stub := newEnv(nil, skillcuration.CycleStatus{
			State: skillcuration.CycleStateRunning, Trigger: skillcuration.CycleTriggerManual,
		})
		rec := doCurationRequest(env.r, http.MethodPost,
			"/api/v1/workspaces/acme/skill-curation/cycle/run", env.adminID, map[string]any{})
		if rec.Code != http.StatusOK {
			t.Fatalf("run = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Status skillcuration.CycleStatus `json:"status"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode run: %v", err)
		}
		if body.Status.State != skillcuration.CycleStateRunning {
			t.Errorf("status state = %q, want running", body.Status.State)
		}
		if stub.runCalls != 1 || stub.lastWSID != env.ws.ID {
			t.Errorf("seam calls drifted: calls=%d ws=%q", stub.runCalls, stub.lastWSID)
		}
	})

	t.Run("member reads the status but cannot trigger (403)", func(t *testing.T) {
		env, stub := newEnv(nil, skillcuration.CycleStatus{State: skillcuration.CycleStateIdle})
		rec := doCurationRequest(env.r, http.MethodGet,
			"/api/v1/workspaces/acme/skill-curation/cycle/status", env.memberID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("member status = %d: %s", rec.Code, rec.Body.String())
		}
		rec = doCurationRequest(env.r, http.MethodPost,
			"/api/v1/workspaces/acme/skill-curation/cycle/run", env.memberID, map[string]any{})
		if rec.Code != http.StatusForbidden {
			t.Errorf("member run = %d, want 403", rec.Code)
		}
		if stub.runCalls != 0 {
			t.Errorf("the blocked trigger reached the seam %d times", stub.runCalls)
		}
	})

	t.Run("double trigger maps to 409", func(t *testing.T) {
		env, _ := newEnv(
			fmt.Errorf("skillcuration cycle still running for workspace %s: %w", "ws", domain.ErrConflict),
			skillcuration.CycleStatus{State: skillcuration.CycleStateRunning},
		)
		rec := doCurationRequest(env.r, http.MethodPost,
			"/api/v1/workspaces/acme/skill-curation/cycle/run", env.ownerID, map[string]any{})
		if rec.Code != http.StatusConflict {
			t.Fatalf("double trigger = %d, want 409: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("status renders the finished record with counters", func(t *testing.T) {
		env, _ := newEnv(nil, skillcuration.CycleStatus{
			State:     skillcuration.CycleStateSucceeded,
			Trigger:   skillcuration.CycleTriggerScheduled,
			StartedAt: time.Now().UTC().Add(-time.Minute),
			Stages: []skillcuration.CycleStageResult{
				{Stage: "probation_sweep", OK: true},
				{Stage: "wiki_maintenance", OK: false, Error: "model unavailable"},
			},
			Counters: skillcuration.CycleCounters{ProposalsDrafted: 2, CandidatesPending: 3},
		})
		rec := doCurationRequest(env.r, http.MethodGet,
			"/api/v1/workspaces/acme/skill-curation/cycle/status", env.memberID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Status skillcuration.CycleStatus `json:"status"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		if body.Status.State != skillcuration.CycleStateSucceeded || body.Status.Counters.ProposalsDrafted != 2 {
			t.Errorf("status record drifted: %+v", body.Status)
		}
		if len(body.Status.Stages) != 2 || body.Status.Stages[1].OK {
			t.Errorf("stage records drifted: %+v", body.Status.Stages)
		}
	})
}

// ---------------------------------------------------------------------------
// The retry endpoint (extraction-failed cards) and the deleted-source
// marker. The retry seam is a stub — the real draft path is the
// skillcuration package's tests; here the pass-through and the permission
// matrix are what's under test.
// ---------------------------------------------------------------------------

type retryStub struct {
	mu       sync.Mutex
	row      *domain.SkillCandidate
	err      error
	calls    int
	lastWS   string
	lastCand string
}

func (s *retryStub) RetryCandidate(_ context.Context, workspaceID, candidateID string) (*domain.SkillCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastWS = workspaceID
	s.lastCand = candidateID
	if s.err != nil {
		return nil, s.err
	}
	return s.row, nil
}

func TestSkillCuration_RetryEndpoint(t *testing.T) {
	t.Run("owner retries a failed candidate through the seam", func(t *testing.T) {
		stub := &retryStub{}
		env := newSkillCurationEnvWith(t, defaultCurationCfg(), nil, nil, stub)
		failed := seedFailedCandidate(t, env, "deploy-guard", "response is not a valid proposal object")
		stub.row = failed
		rec := doCurationRequest(env.r, http.MethodPost,
			"/api/v1/workspaces/acme/skill-curation/candidates/"+failed.ID+"/retry", env.ownerID, map[string]any{})
		if rec.Code != http.StatusOK {
			t.Fatalf("retry = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Candidate domain.SkillCandidate `json:"candidate"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode retry: %v", err)
		}
		if body.Candidate.ID != failed.ID {
			t.Errorf("response candidate = %s, want %s", body.Candidate.ID, failed.ID)
		}
		if stub.calls != 1 || stub.lastWS != env.ws.ID || stub.lastCand != failed.ID {
			t.Errorf("seam calls drifted: %+v", stub)
		}
	})

	t.Run("member cannot retry (403) and the seam is never reached", func(t *testing.T) {
		stub := &retryStub{}
		env := newSkillCurationEnvWith(t, defaultCurationCfg(), nil, nil, stub)
		failed := seedFailedCandidate(t, env, "deploy-guard", "boom")
		rec := doCurationRequest(env.r, http.MethodPost,
			"/api/v1/workspaces/acme/skill-curation/candidates/"+failed.ID+"/retry", env.memberID, map[string]any{})
		if rec.Code != http.StatusForbidden {
			t.Errorf("member retry = %d, want 403", rec.Code)
		}
		if stub.calls != 0 {
			t.Errorf("the blocked retry reached the seam %d times", stub.calls)
		}
	})

	t.Run("unknown candidate is 404 and a non-failed status is 409", func(t *testing.T) {
		env := newSkillCurationEnvWith(t, defaultCurationCfg(), nil, nil, &retryStub{err: domain.ErrNotFound})
		rec := doCurationRequest(env.r, http.MethodPost,
			"/api/v1/workspaces/acme/skill-curation/candidates/no-such-id/retry", env.ownerID, map[string]any{})
		if rec.Code != http.StatusNotFound {
			t.Errorf("unknown retry = %d, want 404", rec.Code)
		}

		conflict := fmt.Errorf("candidate x is pending, only a failed candidate can be retried: %w", domain.ErrConflict)
		env = newSkillCurationEnvWith(t, defaultCurationCfg(), nil, nil, &retryStub{err: conflict})
		rec = doCurationRequest(env.r, http.MethodPost,
			"/api/v1/workspaces/acme/skill-curation/candidates/any/retry", env.ownerID, map[string]any{})
		if rec.Code != http.StatusConflict {
			t.Errorf("non-failed retry = %d, want 409: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestSkillCuration_ListSourceAvailability(t *testing.T) {
	// The deleted-source marker (human approval gate scenario): the list
	// annotates each row with source_available — false when the checker says
	// the first evidence session is gone, true on any checker error, absent
	// evidence, or an unwired checker. Approval never consults it.
	decode := func(t *testing.T, rec *httptest.ResponseRecorder) []struct {
		ID              string `json:"id"`
		Status          string `json:"status"`
		SourceAvailable bool   `json:"source_available"`
	} {
		t.Helper()
		var body struct {
			Candidates []struct {
				ID              string `json:"id"`
				Status          string `json:"status"`
				SourceAvailable bool   `json:"source_available"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return body.Candidates
	}

	t.Run("checker reports missing source", func(t *testing.T) {
		env := newSkillCurationEnvWith(t, defaultCurationCfg(), nil,
			func(_ context.Context, _, sessionID string) (bool, error) {
				return sessionID != "sess-deleted-a", nil
			}, nil)
		row := seedCandidate(t, env, "deploy-rollback")
		rec := doCurationRequest(env.r, http.MethodGet,
			"/api/v1/workspaces/acme/skill-curation/candidates", env.ownerID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list = %d: %s", rec.Code, rec.Body.String())
		}
		rows := decode(t, rec)
		if len(rows) != 1 {
			t.Fatalf("list rows = %d, want 1", len(rows))
		}
		if rows[0].ID != row.ID || rows[0].SourceAvailable {
			t.Errorf("source_available = %v, want false for the deleted first evidence session", rows[0].SourceAvailable)
		}
	})

	t.Run("checker error defaults to available", func(t *testing.T) {
		env := newSkillCurationEnvWith(t, defaultCurationCfg(), nil,
			func(context.Context, string, string) (bool, error) {
				return false, fmt.Errorf("session store down")
			}, nil)
		seedCandidate(t, env, "deploy-rollback")
		rec := doCurationRequest(env.r, http.MethodGet,
			"/api/v1/workspaces/acme/skill-curation/candidates", env.ownerID, nil)
		rows := decode(t, rec)
		if len(rows) != 1 || !rows[0].SourceAvailable {
			t.Errorf("checker failure must default to available, got %+v", rows)
		}
	})

	t.Run("unwired checker and live source report available", func(t *testing.T) {
		env := newSkillCurationEnvWith(t, defaultCurationCfg(), nil,
			func(context.Context, string, string) (bool, error) { return true, nil }, nil)
		seedCandidate(t, env, "deploy-rollback")
		rec := doCurationRequest(env.r, http.MethodGet,
			"/api/v1/workspaces/acme/skill-curation/candidates", env.ownerID, nil)
		if rows := decode(t, rec); len(rows) != 1 || !rows[0].SourceAvailable {
			t.Errorf("live source must report available, got %+v", rows)
		}

		unwired := newSkillCurationEnv(t, defaultCurationCfg())
		seedCandidate(t, unwired, "deploy-rollback")
		rec = doCurationRequest(unwired.r, http.MethodGet,
			"/api/v1/workspaces/acme/skill-curation/candidates", unwired.ownerID, nil)
		if rows := decode(t, rec); len(rows) != 1 || !rows[0].SourceAvailable {
			t.Errorf("unwired checker must default to available, got %+v", rows)
		}
	})

	t.Run("the pending badge view excludes failed rows", func(t *testing.T) {
		env := newSkillCurationEnvWith(t, defaultCurationCfg(), nil,
			func(context.Context, string, string) (bool, error) { return true, nil }, nil)
		seedCandidate(t, env, "deploy-rollback")
		seedFailedCandidate(t, env, "ghost-draft", "response is not a valid proposal object")

		rec := doCurationRequest(env.r, http.MethodGet,
			"/api/v1/workspaces/acme/skill-curation/candidates?status=pending", env.ownerID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("pending list = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Count      int `json:"count"`
			Candidates []struct {
				Status string `json:"status"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode pending list: %v", err)
		}
		if body.Count != 1 || len(body.Candidates) != 1 || body.Candidates[0].Status != "pending" {
			t.Errorf("failed rows must not inflate the pending badge: count=%d rows=%+v", body.Count, body.Candidates)
		}

		// The full review view still lists the failed row with its marker.
		rec = doCurationRequest(env.r, http.MethodGet,
			"/api/v1/workspaces/acme/skill-curation/candidates", env.ownerID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("full list = %d: %s", rec.Code, rec.Body.String())
		}
		sawFailed := false
		for _, row := range decode(t, rec) {
			if row.Status == string(domain.SkillCandidateFailed) {
				sawFailed = row.SourceAvailable
			}
		}
		if !sawFailed {
			t.Error("the failed row must list with source_available")
		}
	})
}
