package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// workspace-files fixtures. The markdown bytes double as the read-equality
// expectation; the html and svg bodies carry executable content so the
// attachment guard is tested against real payloads, not empty files.
const (
	wfMarkdown = "# Report\n\nAll systems nominal.\n"
	wfHTML     = "<!DOCTYPE html><html><body><script>alert('stored xss')</script></body></html>"
	wfSVG      = "<svg xmlns=\"http://www.w3.org/2000/svg\"><script>alert('stored xss')</script></svg>"
)

var wfPNG = []byte("\x89PNG\x0D\x0A\x1A\x0Afakepngbytes")

// newWorkspaceFilesTestEnv builds a gin engine with the workspace-files route
// wired over the fake store and a temp workspace root, mirroring the
// composition root. The middleware plays RequireWorkspace: it resolves the
// workspace by the URL slug and binds it to the context — the handler itself
// never reads the slug. Two workspaces are seeded so cross-workspace access
// can be exercised at the handler boundary.
func newWorkspaceFilesTestEnv(t *testing.T) (*gin.Engine, store.Store, *domain.Workspace, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	user := &domain.User{Email: "member@example.com", Name: "Member"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	wsA := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(ctx, wsA); err != nil {
		t.Fatalf("seed workspace acme: %v", err)
	}
	wsB := &domain.Workspace{Name: "Other", Slug: "other"}
	if err := st.Workspaces().Create(ctx, wsB); err != nil {
		t.Fatalf("seed workspace other: %v", err)
	}
	for _, agent := range []*domain.Agent{
		{WorkspaceID: wsA.ID, Slug: "atlas", Name: "Atlas"},
		{WorkspaceID: wsB.ID, Slug: "beacon", Name: "Beacon"},
	} {
		if err := st.Agents().Create(ctx, agent); err != nil {
			t.Fatalf("seed agent %s: %v", agent.Slug, err)
		}
	}

	workspaceDir := t.TempDir()
	h := handlers.NewWorkspaceFilesHandlers(st.Agents(), workspaceDir)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		resolved, err := st.Workspaces().BySlug(c.Request.Context(), c.Param("ws"))
		if err != nil {
			handlers.AbortNotFound(c, "workspace not found")
			return
		}
		c.Set(handlers.WorkspaceContextKey, resolved)
		c.Set(handlers.UserContextKey, user)
		c.Next()
	})
	r.GET("/api/v1/workspaces/:ws/agents/:agent/files", h.ServeWorkspaceFiles)

	jailA := domain.AgentWorkspaceDir(workspaceDir, wsA.Slug, "atlas")
	jailB := domain.AgentWorkspaceDir(workspaceDir, wsB.Slug, "beacon")
	return r, st, wsA, jailA, jailB
}

// seedJail lays out an agent workspace tree: files for the read scenarios,
// two directories for the one-level listing scenarios, and two symlinks
// pointing outside the jail (a file link and a directory link) for the
// escape rejections. The outside target lives in a sibling of the jail so an
// escaped read would serve content if confinement failed.
func seedJail(t *testing.T, jail string) {
	t.Helper()

	outside := filepath.Join(filepath.Dir(filepath.Dir(jail)), "outside-secret")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("top secret\n"), 0o644); err != nil {
		t.Fatalf("write outside secret: %v", err)
	}

	for _, dir := range []string{"reports", "browser"} {
		if err := os.MkdirAll(filepath.Join(jail, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	files := map[string][]byte{
		"notes.md":         []byte(wfMarkdown),
		"page.html":        []byte(wfHTML),
		"logo.svg":         []byte(wfSVG),
		"reports/q3.md":    []byte("# Q3\n"),
		"browser/shot.png": wfPNG,
	}
	for name, content := range files {
		full := filepath.Join(jail, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(jail, "escape.md")); err != nil {
		t.Fatalf("symlink escape.md: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(jail, "out-dir")); err != nil {
		t.Fatalf("symlink out-dir: %v", err)
	}
}

func wfGet(r *gin.Engine, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestWorkspaceFiles_ReadServingGuards(t *testing.T) {
	r, _, _, jail, _ := newWorkspaceFilesTestEnv(t)
	seedJail(t, jail)

	cases := []struct {
		name       string
		path       string
		wantStatus int
		wantCT     string // exact or prefix when suffixed with "*"
		wantInline bool   // true = no Content-Disposition allowed
		wantBody   string // "" skips the byte-equality check
	}{
		{name: "authored markdown serves inline", path: "notes.md", wantStatus: http.StatusOK, wantCT: "text/*", wantInline: true, wantBody: wfMarkdown},
		{name: "png serves inline", path: "browser/shot.png", wantStatus: http.StatusOK, wantCT: "image/png", wantInline: true, wantBody: string(wfPNG)},
		{name: "html forces attachment", path: "page.html", wantStatus: http.StatusOK, wantCT: "text/html", wantInline: false, wantBody: wfHTML},
		{name: "svg forces attachment", path: "logo.svg", wantStatus: http.StatusOK, wantCT: "image/svg+xml", wantInline: false, wantBody: wfSVG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := wfGet(r, "/api/v1/workspaces/acme/agents/atlas/files?path="+tc.path)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
			ct := rec.Header().Get("Content-Type")
			if tc.wantCT != "" {
				if strings.HasSuffix(tc.wantCT, "*") {
					if !strings.HasPrefix(ct, strings.TrimSuffix(tc.wantCT, "*")) {
						t.Errorf("Content-Type = %q, want prefix %q", ct, strings.TrimSuffix(tc.wantCT, "*"))
					}
				} else if ct != tc.wantCT && !strings.HasPrefix(ct, tc.wantCT+";") {
					t.Errorf("Content-Type = %q, want %q", ct, tc.wantCT)
				}
			}
			disp := rec.Header().Get("Content-Disposition")
			if tc.wantInline && disp != "" {
				t.Errorf("Content-Disposition = %q, want no disposition for a display-safe type", disp)
			}
			if !tc.wantInline && !strings.Contains(disp, "attachment") {
				t.Errorf("Content-Disposition = %q, want attachment for an executable type", disp)
			}
			if tc.wantBody != "" && rec.Body.String() != tc.wantBody {
				t.Errorf("served bytes differ: got %d bytes, want %d", rec.Body.Len(), len(tc.wantBody))
			}
		})
	}
}

// TestWorkspaceFiles_Rejections covers every confinement and miss scenario:
// all of them collapse to the same not-found envelope — no existence oracle.
func TestWorkspaceFiles_Rejections(t *testing.T) {
	r, _, _, jail, _ := newWorkspaceFilesTestEnv(t)
	seedJail(t, jail)

	cases := []struct {
		name string
		url  string
	}{
		{name: "parent traversal", url: "?path=../../beacon/notes.md"},
		{name: "deep parent traversal", url: "?path=reports/../../outside-secret/secret.md"},
		{name: "absolute path", url: "?path=" + "/etc/passwd"},
		{name: "symlink file escape", url: "?path=escape.md"},
		{name: "symlink directory escape", url: "?path=out-dir/secret.md"},
		{name: "missing file", url: "?path=missing.md"},
		{name: "missing nested file", url: "?path=reports/missing.md"},
		{name: "read a directory", url: "?path=reports"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := wfGet(r, "/api/v1/workspaces/acme/agents/atlas/files"+tc.url)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
				t.Errorf("expected the standard not_found envelope: %s", rec.Body.String())
			}
		})
	}

	t.Run("empty path is invalid input", func(t *testing.T) {
		rec := wfGet(r, "/api/v1/workspaces/acme/agents/atlas/files?path=")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestWorkspaceFiles_ListRoot(t *testing.T) {
	r, _, _, jail, _ := newWorkspaceFilesTestEnv(t)
	seedJail(t, jail)

	rec := wfGet(r, "/api/v1/workspaces/acme/agents/atlas/files?mode=list")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff on listings too", got)
	}

	var res struct {
		Path    string `json:"path"`
		Entries []struct {
			Name     string `json:"name"`
			Kind     string `json:"kind"`
			Size     int64  `json:"size"`
			Modified string `json:"modified"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode listing: %v", err)
	}

	// Exactly one level: every seeded direct child is present with its kind,
	// and no deeper descendant appears.
	got := map[string]string{}
	for _, e := range res.Entries {
		got[e.Name] = e.Kind
		if e.Modified == "" {
			t.Errorf("entry %s carries no modified time", e.Name)
		}
	}
	want := map[string]string{
		"notes.md":  "file",
		"page.html": "file",
		"logo.svg":  "file",
		"reports":   "directory",
		"browser":   "directory",
		"escape.md": "file", // symlink entries list, but reads of them reject
		"out-dir":   "file",
	}
	if len(got) != len(want) {
		t.Errorf("entry count = %d, want %d (%v)", len(got), len(want), got)
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("entry %s kind = %q, want %q", name, got[name], kind)
		}
	}
	if _, deeper := got["q3.md"]; deeper {
		t.Error("root listing leaked a second-level descendant (q3.md)")
	}

	// The file entry carries the real size.
	for _, e := range res.Entries {
		if e.Name == "notes.md" && e.Size != int64(len(wfMarkdown)) {
			t.Errorf("notes.md size = %d, want %d", e.Size, len(wfMarkdown))
		}
	}
}

func TestWorkspaceFiles_ListNestedDirectory(t *testing.T) {
	r, _, _, jail, _ := newWorkspaceFilesTestEnv(t)
	seedJail(t, jail)

	rec := wfGet(r, "/api/v1/workspaces/acme/agents/atlas/files?path=reports&mode=list")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}

	var res struct {
		Entries []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode listing: %v", err)
	}
	if len(res.Entries) != 1 || res.Entries[0].Name != "q3.md" || res.Entries[0].Kind != "file" {
		t.Errorf("entries = %+v, want exactly [q3.md file]", res.Entries)
	}
}

func TestWorkspaceFiles_ListRegularFileNotFound(t *testing.T) {
	r, _, _, jail, _ := newWorkspaceFilesTestEnv(t)
	seedJail(t, jail)

	rec := wfGet(r, "/api/v1/workspaces/acme/agents/atlas/files?path=notes.md&mode=list")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Errorf("expected the standard not_found envelope: %s", rec.Body.String())
	}
}

// TestWorkspaceFiles_CrossWorkspaceIsNotFound proves scoping rides the
// context workspace, not the URL: beacon belongs to workspace "other", its
// jail exists on disk with the same layout, and a request addressed through
// workspace "acme" still gets the missing-agent treatment.
func TestWorkspaceFiles_CrossWorkspaceIsNotFound(t *testing.T) {
	r, _, _, jailA, jailB := newWorkspaceFilesTestEnv(t)
	seedJail(t, jailA)
	seedJail(t, jailB)

	for _, tc := range []struct {
		name string
		url  string
	}{
		{name: "read", url: "/api/v1/workspaces/acme/agents/beacon/files?path=notes.md"},
		{name: "list", url: "/api/v1/workspaces/acme/agents/beacon/files?mode=list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := wfGet(r, tc.url)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
				t.Errorf("expected the standard not_found envelope: %s", rec.Body.String())
			}
		})
	}
}

func TestWorkspaceFiles_MissingAgentNotFound(t *testing.T) {
	r, _, _, jail, _ := newWorkspaceFilesTestEnv(t)
	seedJail(t, jail)

	rec := wfGet(r, "/api/v1/workspaces/acme/agents/ghost/files?path=notes.md")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}
