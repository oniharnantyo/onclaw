package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// memoryBody mirrors the wire shape of both memory endpoints.
type memoryBody struct {
	Content   string     `json:"content"`
	MaxChars  int        `json:"max_chars"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// newMemoryTestEnv builds a gin engine with the memory handlers and a seeded
// workspace, using the fake store. The caller identity is selected per request
// through the X-Test-User header (member | admin); an unknown :ws slug 404s
// like the real RequireWorkspace middleware.
func newMemoryTestEnv(t *testing.T) (*gin.Engine, store.Store, *domain.Workspace) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	ws := &domain.Workspace{Name: "Acme", Slug: "acme", Timezone: "UTC"}
	if err := st.Workspaces().Create(nil, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	users := map[string]*domain.User{
		"member": {Email: "member@example.com", Name: "Member"},
		"admin":  {Email: "admin@example.com", Name: "Admin"},
	}
	for _, u := range users {
		if err := st.Users().Create(nil, u); err != nil {
			t.Fatalf("seed user %s: %v", u.Email, err)
		}
	}

	h := handlers.NewMemoryHandlers(st.Memories())

	r := gin.New()
	group := r.Group("/api/v1/workspaces/:ws")
	group.Use(func(c *gin.Context) {
		resolved, err := st.Workspaces().BySlug(nil, c.Param("ws"))
		if err != nil {
			handlers.AbortNotFound(c, "workspace not found")
			return
		}
		c.Set(handlers.WorkspaceContextKey, resolved)
		c.Set(handlers.UserContextKey, users[c.GetHeader("X-Test-User")])
		c.Next()
	})
	group.GET("/me/memory", h.GetUserMemory)
	group.PUT("/me/memory", h.PutUserMemory)
	group.GET("/memory", h.GetWorkspaceMemory)
	group.PUT("/memory", h.PutWorkspaceMemory)
	return r, st, ws
}

func doMemoryRequest(r *gin.Engine, method, path, user string, body any) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeMemoryBody(t *testing.T, rec *httptest.ResponseRecorder) memoryBody {
	t.Helper()
	var res memoryBody
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode memory response %q: %v", rec.Body.String(), err)
	}
	return res
}

func TestMemory_UserMemoryRoundTrip(t *testing.T) {
	r, st, ws := newMemoryTestEnv(t)

	// GET before anything is stored: empty document, budget, null timestamp.
	w := doMemoryRequest(r, http.MethodGet, "/api/v1/workspaces/acme/me/memory", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	empty := decodeMemoryBody(t, w)
	if empty.Content != "" {
		t.Errorf("expected empty content, got %q", empty.Content)
	}
	if empty.MaxChars != domain.MaxMemoryContentChars {
		t.Errorf("expected max_chars %d, got %d", domain.MaxMemoryContentChars, empty.MaxChars)
	}
	if empty.UpdatedAt != nil {
		t.Errorf("expected null updated_at, got %v", empty.UpdatedAt)
	}

	// PUT persists and echoes the saved shape.
	w = doMemoryRequest(r, http.MethodPut, "/api/v1/workspaces/acme/me/memory", "member", map[string]any{"content": "Prefers Go; lives in UTC."})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	saved := decodeMemoryBody(t, w)
	if saved.Content != "Prefers Go; lives in UTC." || saved.MaxChars != domain.MaxMemoryContentChars || saved.UpdatedAt == nil {
		t.Errorf("unexpected saved shape: %+v", saved)
	}

	// GET returns the stored content.
	w = doMemoryRequest(r, http.MethodGet, "/api/v1/workspaces/acme/me/memory", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := decodeMemoryBody(t, w).Content; got != "Prefers Go; lives in UTC." {
		t.Errorf("expected stored content, got %q", got)
	}

	// The stored row really belongs to the calling user.
	mem, err := st.Memories().UserMemory(nil, ws.ID, userIDByEmail(t, st, "member@example.com"))
	if err != nil || mem == nil || mem.Content != "Prefers Go; lives in UTC." {
		t.Fatalf("expected stored user memory, got %+v err %v", mem, err)
	}

	// A different user's document is unaffected (self-scoped by caller identity).
	w = doMemoryRequest(r, http.MethodGet, "/api/v1/workspaces/acme/me/memory", "admin", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := decodeMemoryBody(t, w).Content; got != "" {
		t.Errorf("expected admin's own empty memory, got %q", got)
	}
}

func TestMemory_UserMemoryOverCapIs422(t *testing.T) {
	r, st, ws := newMemoryTestEnv(t)

	overCap := strings.Repeat("x", domain.MaxMemoryContentChars+1)
	w := doMemoryRequest(r, http.MethodPut, "/api/v1/workspaces/acme/me/memory", "member", map[string]any{"content": overCap})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "32000") {
		t.Errorf("expected 422 body to name the limit: %s", w.Body.String())
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Details []struct {
				Field   string `json:"field"`
				Message string `json:"message"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if env.Error.Code == "" {
		t.Errorf("expected error code in envelope: %s", w.Body.String())
	}
	found := false
	for _, d := range env.Error.Details {
		if d.Field == "max_chars" && d.Message == "32000" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected max_chars detail in 422 envelope: %s", w.Body.String())
	}

	// The rejection stores nothing: absence stays a normal state.
	mem, _ := st.Memories().UserMemory(nil, ws.ID, userIDByEmail(t, st, "member@example.com"))
	if mem != nil {
		t.Errorf("expected no stored memory after 422, got %+v", mem)
	}
}

func TestMemory_WorkspaceMemoryRoundTrip(t *testing.T) {
	r, st, ws := newMemoryTestEnv(t)

	// GET before anything is stored: empty document.
	w := doMemoryRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeMemoryBody(t, w); got.Content != "" || got.MaxChars != domain.MaxMemoryContentChars || got.UpdatedAt != nil {
		t.Errorf("unexpected empty workspace memory shape: %+v", got)
	}

	// PUT saves the shared document and echoes the saved shape.
	w = doMemoryRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory", "admin", map[string]any{"content": "# Decisions\n- Postgres for everything."})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	saved := decodeMemoryBody(t, w)
	if saved.Content != "# Decisions\n- Postgres for everything." || saved.UpdatedAt == nil {
		t.Errorf("unexpected saved shape: %+v", saved)
	}

	// The store holds it as the workspace memory — targeted update, so the
	// workspace row's other fields are untouched.
	stored, err := st.Memories().WorkspaceMemory(nil, ws.ID)
	if err != nil || stored == nil {
		t.Fatalf("expected stored workspace memory, got %+v err %v", stored, err)
	}
	if stored.Content != "# Decisions\n- Postgres for everything." {
		t.Errorf("unexpected stored content %q", stored.Content)
	}
	current, err := st.Workspaces().BySlug(nil, "acme")
	if err != nil {
		t.Fatalf("reload workspace: %v", err)
	}
	if current.Name != "Acme" || current.Timezone != "UTC" || current.ID != ws.ID {
		t.Errorf("workspace fields were clobbered: %+v", current)
	}

	// A member reads the shared memory and sees the admin's content.
	w = doMemoryRequest(r, http.MethodGet, "/api/v1/workspaces/acme/memory", "member", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := decodeMemoryBody(t, w).Content; got != "# Decisions\n- Postgres for everything." {
		t.Errorf("expected member to read shared memory, got %q", got)
	}

	// PUT replaces the whole document.
	w = doMemoryRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory", "admin", map[string]any{"content": "Replaced."})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on replace, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeMemoryBody(t, w).Content; got != "Replaced." {
		t.Errorf("expected replaced content, got %q", got)
	}
}

func TestMemory_WorkspaceMemoryOverCapIs422(t *testing.T) {
	r, st, ws := newMemoryTestEnv(t)

	overCap := strings.Repeat("y", domain.MaxMemoryContentChars*2)
	w := doMemoryRequest(r, http.MethodPut, "/api/v1/workspaces/acme/memory", "admin", map[string]any{"content": overCap})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "exceeds the 32000 char cap") {
		t.Errorf("expected 422 body to name the limit: %s", w.Body.String())
	}

	// The rejection stores nothing.
	mem, _ := st.Memories().WorkspaceMemory(nil, ws.ID)
	if mem != nil {
		t.Errorf("expected no stored workspace memory after 422, got %+v", mem)
	}
}

func TestMemory_UnknownWorkspaceIs404(t *testing.T) {
	r, _, _ := newMemoryTestEnv(t)

	for _, ep := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/workspaces/unknown-ws/me/memory"},
		{http.MethodPut, "/api/v1/workspaces/unknown-ws/me/memory"},
		{http.MethodGet, "/api/v1/workspaces/unknown-ws/memory"},
		{http.MethodPut, "/api/v1/workspaces/unknown-ws/memory"},
	} {
		w := doMemoryRequest(r, ep.method, ep.path, "member", map[string]any{"content": "hi"})
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s expected 404, got %d: %s", ep.method, ep.path, w.Code, w.Body.String())
		}
	}
}

func TestMemory_MalformedBodyIs400(t *testing.T) {
	r, _, _ := newMemoryTestEnv(t)

	for _, path := range []string{"/api/v1/workspaces/acme/me/memory", "/api/v1/workspaces/acme/memory"} {
		req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader([]byte("not json")))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", "member")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for malformed JSON on %s, got %d: %s", path, w.Code, w.Body.String())
		}
	}
}

// userIDByEmail resolves a seeded user's store-assigned ID.
func userIDByEmail(t *testing.T, st store.Store, email string) string {
	t.Helper()
	u, err := st.Users().ByEmail(nil, email)
	if err != nil {
		t.Fatalf("load user %s: %v", email, err)
	}
	return u.ID
}
