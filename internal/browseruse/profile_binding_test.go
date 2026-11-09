package browseruse

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// The bound-profile resolution (add-per-agent-browser-profiles, design D3):
// the parent stamps the agent's profile directory and display name through
// the call's _meta, and every handler's profile resolution funnels through
// resolveProfile. A bound call ignores the profile argument entirely and runs
// in its own directory; any call without the binding (direct CLI use, tests)
// keeps the named-profile behavior unchanged. Like the screenshot-dir
// precedent, malformed meta is tolerated: it falls back to named resolution.

// callHandlerMeta drives one handler with JSON-style arguments and host meta.
func callHandlerMeta(t *testing.T, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any, meta map[string]any) mcp.CallToolResult {
	t.Helper()
	r := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "test", Arguments: args}}
	if meta != nil {
		r.Params.Meta = mcp.NewMetaFromMap(meta)
	}
	res, err := h(context.Background(), r)
	require.NoError(t, err, "domain failures are tool errors, never JSON-RPC errors")
	require.NotNil(t, res)
	require.True(t, res.IsError || len(res.Content) == 1, "every result carries exactly its message")
	return *res
}

// TestResolveProfile_MetaBinding is the pure resolution table: a clean
// absolute profile-dir meta binds the call (key = directory, name = display
// meta, argument ignored), everything absent or malformed falls back to the
// named path, and the fallback never touches the filesystem.
func TestResolveProfile_MetaBinding(t *testing.T) {
	browserDir := t.TempDir()
	b := &browserTools{cfg: DefaultConfig(), mgr: newManager()}
	b.cfg.BrowserDir = browserDir
	b.cfg.DefaultProfile = "configured"

	boundDir := filepath.Join(browserDir, "workspaces", "scout", "browser-profile")
	bound := map[string]any{
		MetaProfileDir:    boundDir,
		MetaProfileName:   "scout",
		MetaScreenshotDir: "/tmp/other",
	}

	cases := []struct {
		name      string
		meta      map[string]any
		arg       string
		wantKey   string
		wantName  string
		wantMkdir bool // the binding creates its directory on demand
	}{
		{
			name:     "no meta, no argument: configured default",
			meta:     nil,
			arg:      "",
			wantKey:  "configured",
			wantName: "configured",
		},
		{
			name:     "no meta, explicit argument wins",
			meta:     nil,
			arg:      "work",
			wantKey:  "work",
			wantName: "work",
		},
		{
			name:      "bound meta wins over the argument entirely",
			meta:      bound,
			arg:       "other",
			wantKey:   boundDir,
			wantName:  "scout",
			wantMkdir: true,
		},
		{
			name:      "bound dir without a display name falls back to its base",
			meta:      map[string]any{MetaProfileDir: boundDir},
			arg:       "other",
			wantKey:   boundDir,
			wantName:  "browser-profile",
			wantMkdir: true,
		},
		{
			name:     "relative meta dir is garbage: named path stays in charge",
			meta:     map[string]any{MetaProfileDir: "workspaces/scout/browser-profile", MetaProfileName: "scout"},
			arg:      "work",
			wantKey:  "work",
			wantName: "work",
		},
		{
			name:     "traversal-shaped meta dir outside the root is garbage",
			meta:     map[string]any{MetaProfileDir: "../escape", MetaProfileName: "scout"},
			arg:      "work",
			wantKey:  "work",
			wantName: "work",
		},
		{
			name:     "empty meta dir is garbage",
			meta:     map[string]any{MetaProfileDir: "", MetaProfileName: "scout"},
			arg:      "work",
			wantKey:  "work",
			wantName: "work",
		},
		{
			name:     "non-string meta dir is garbage",
			meta:     map[string]any{MetaProfileDir: 42, MetaProfileName: "scout"},
			arg:      "work",
			wantKey:  "work",
			wantName: "work",
		},
		{
			name:      "traversal that cleans to an absolute meaning binds the cleaned dir",
			meta:      map[string]any{MetaProfileDir: boundDir + "/../browser-profile", MetaProfileName: "scout"},
			arg:       "other",
			wantKey:   boundDir,
			wantName:  "scout",
			wantMkdir: true,
		},
		{
			name:      "display name without the name contract is accepted as-is",
			meta:      map[string]any{MetaProfileDir: boundDir, MetaProfileName: "Agent Scout the Explorer"},
			arg:       "other",
			wantKey:   boundDir,
			wantName:  "Agent Scout the Explorer",
			wantMkdir: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req mcp.CallToolRequest
			if tc.meta != nil {
				req.Params.Meta = mcp.NewMetaFromMap(tc.meta)
			}
			prof, err := b.resolveProfile(req, tc.arg)
			require.NoError(t, err)
			require.Equal(t, tc.wantKey, prof.Key)
			require.Equal(t, tc.wantName, prof.Name)
			if tc.wantMkdir {
				require.DirExists(t, tc.wantKey, "the binding creates its directory on demand")
			}
		})
	}

	// The named fallback path never created a profiles directory: garbage meta
	// leaves the named behavior byte-for-byte.
	_, err := os.Stat(filepath.Join(browserDir, "profiles"))
	require.True(t, os.IsNotExist(err), "resolution without a valid binding touches no named profile")
}

// boundTestManager returns a manager whose launch stub records the launched
// directories and hands out sessions whose actions fail gracefully (plain
// page and browser contexts answer chromedp with its invalid-context error,
// and the cookie paths answer "no browser session" — no real browser, no
// panic).
func boundTestManager(t *testing.T) (*manager, *[]string) {
	t.Helper()
	var launched []string
	m := newManager()
	m.launch = func(_ context.Context, dir string, _ Config) (*profileSession, error) {
		launched = append(launched, dir)
		return &profileSession{
			dir:        dir,
			pageCtx:    context.Background(),
			browserCtx: context.Background(),
			closeFn:    func() error { return nil },
		}, nil
	}
	return m, &launched
}

// requireOnlyBoundSession pins the bound handler outcome: the call ran in the
// bound directory, and the model-supplied profile argument never resolved —
// no session, no launch, no on-disk profile named after it.
func requireOnlyBoundSession(t *testing.T, m *manager, launched *[]string, browserDir, boundDir string) {
	t.Helper()
	s, ok := m.sessions[boundDir]
	require.True(t, ok, "the bound directory keys the sessions map")
	require.Equal(t, boundDir, s.dir, "the session runs on the bound user-data directory")
	require.NotContains(t, m.sessions, "other", "the profile argument is ignored")
	require.Len(t, *launched, 1, "exactly one launch, into the bound directory")
	require.Equal(t, boundDir, (*launched)[0])
	_, err := os.Stat(filepath.Join(browserDir, "profiles", "other"))
	require.True(t, os.IsNotExist(err), "no profile named after the model's argument is created")
}

// TestBoundHandlers_RunInTheBoundDir walks the page tools with a bound meta
// and a model-supplied profile argument: the binding wins for every one of
// them, and the actions run (and fail, on the stub) inside the bound dir.
func TestBoundHandlers_RunInTheBoundDir(t *testing.T) {
	browserDir := t.TempDir()
	boundDir := filepath.Join(t.TempDir(), "workspaces", "scout", "browser-profile")
	boundMeta := map[string]any{MetaProfileDir: boundDir, MetaProfileName: "scout"}

	t.Run("navigate", func(t *testing.T) {
		m, launched := boundTestManager(t)
		b := &browserTools{cfg: DefaultConfig(), mgr: m}
		b.cfg.BrowserDir = browserDir
		res := callHandlerMeta(t, b.handleNavigate,
			map[string]any{"url": "https://93.184.216.34/", "profile": "other"}, boundMeta)
		require.True(t, res.IsError, "the stub browser answers every action with an error: %s", resultText(t, res))
		requireOnlyBoundSession(t, m, launched, browserDir, boundDir)
	})

	t.Run("cookies", func(t *testing.T) {
		m, launched := boundTestManager(t)
		b := &browserTools{cfg: DefaultConfig(), mgr: m}
		b.cfg.BrowserDir = browserDir
		res := callHandlerMeta(t, b.handleCookies,
			map[string]any{"op": "list", "profile": "other"}, boundMeta)
		require.True(t, res.IsError, "the stub browser answers every action with an error: %s", resultText(t, res))
		requireOnlyBoundSession(t, m, launched, browserDir, boundDir)
	})

	t.Run("screenshot", func(t *testing.T) {
		m, launched := boundTestManager(t)
		b := &browserTools{cfg: DefaultConfig(), mgr: m}
		b.cfg.BrowserDir = browserDir
		res := callHandlerMeta(t, b.handleScreenshot,
			map[string]any{"profile": "other"}, boundMeta)
		require.True(t, res.IsError, "the stub browser answers every action with an error: %s", resultText(t, res))
		requireOnlyBoundSession(t, m, launched, browserDir, boundDir)
	})

	t.Run("click", func(t *testing.T) {
		m, launched := boundTestManager(t)
		b := &browserTools{cfg: DefaultConfig(), mgr: m}
		b.cfg.BrowserDir = browserDir
		res := callHandlerMeta(t, b.handleClick,
			map[string]any{"name": "Sign in", "profile": "other"}, boundMeta)
		require.True(t, res.IsError, "the stub browser answers every action with an error: %s", resultText(t, res))
		requireOnlyBoundSession(t, m, launched, browserDir, boundDir)
	})

	t.Run("unbound navigate keeps the named path", func(t *testing.T) {
		m, launched := boundTestManager(t)
		b := &browserTools{cfg: DefaultConfig(), mgr: m}
		b.cfg.BrowserDir = browserDir
		res := callHandlerMeta(t, b.handleNavigate,
			map[string]any{"url": "https://93.184.216.34/", "profile": "other"}, nil)
		require.True(t, res.IsError, "the stub browser answers every action with an error: %s", resultText(t, res))
		s, ok := m.sessions["other"]
		require.True(t, ok, "without the binding the argument names the profile again")
		require.Equal(t, filepath.Join(browserDir, "profiles", "other"), s.dir)
		require.Equal(t, []string{filepath.Join(browserDir, "profiles", "other")}, *launched)
		require.DirExists(t, filepath.Join(browserDir, "profiles", "other"))
	})
}

// TestHandleRequestTakeover_BoundDir pins takeover readiness under binding: a
// fresh agent's bound directory does not exist yet, and the request still
// parks ready — never the profile-missing refusal — because the resolution
// seam created the directory on demand. The parked entry carries the bound
// key for the relaunch; the echo carries the display name.
func TestHandleRequestTakeover_BoundDir(t *testing.T) {
	b, m := newTestTools(t)
	b.cfg.BrowserDir = t.TempDir()
	boundDir := filepath.Join(b.cfg.BrowserDir, "workspaces", "scout", "browser-profile")

	res := callHandlerMeta(t, b.handleRequestTakeover, map[string]any{
		"reason": "sign-in-blocked", "profile": "ghost",
	}, map[string]any{MetaProfileDir: boundDir, MetaProfileName: "scout"})
	require.False(t, res.IsError)
	var ready takeoverReadyResult
	require.NoError(t, json.Unmarshal([]byte(resultText(t, res)), &ready))
	require.Equal(t, "ready", ready.Status, "the bound dir is auto-created: never profile-missing")
	require.Equal(t, "scout", ready.Profile, "the echo carries the display name")
	require.Len(t, ready.RequestID, 32)

	require.DirExists(t, boundDir, "the binding created the directory on demand")
	parked, ok := m.findAttention(ready.RequestID)
	require.True(t, ok)
	require.Equal(t, attentionTakeover, parked.Kind)
	require.Equal(t, boundDir, parked.Profile, "the relaunch will open the bound directory")

	_, err := os.Stat(filepath.Join(b.cfg.BrowserDir, "profiles", "ghost"))
	require.True(t, os.IsNotExist(err), "the model's profile argument is never resolved")
}

// jarSeededLaunch is a launch stub that exports a login cookie into the
// launched session's own directory at every jar save, the way the real
// exporter writes cookieJarPath(s.dir).
func jarSeededLaunch(launched *[]string, headless *[]bool, closes *atomic.Int32) func(context.Context, string, Config) (*profileSession, error) {
	return func(_ context.Context, dir string, cfg Config) (*profileSession, error) {
		*launched = append(*launched, dir)
		*headless = append(*headless, cfg.Headless)
		return &profileSession{
			dir:        dir,
			pageCtx:    context.Background(),
			browserCtx: context.Background(),
			closeFn: func() error {
				closes.Add(1)
				return nil
			},
			exportFn: func() error {
				jar := CookieJarFile{Cookies: []StoredCookie{{
					Name: "session", Value: "login-state", Domain: "site.test", Path: "/",
				}}}
				return writeCookieJar(dir, jar)
			},
		}, nil
	}
}

// TestTakeoverBoundDir_HeadedRelaunchAndJarReuse walks the full takeover arc
// on a bound directory with stubbed launch seams: the headed relaunch opens
// the bound directory, the jar lands there at teardown, and the next bound
// action relaunches on the same directory with the login state on disk.
func TestTakeoverBoundDir_HeadedRelaunchAndJarReuse(t *testing.T) {
	m := newManager()
	var launched []string
	var headless []bool
	var closes atomic.Int32
	m.launch = jarSeededLaunch(&launched, &headless, &closes)
	b := &browserTools{cfg: DefaultConfig(), mgr: m}
	b.cfg.BrowserDir = t.TempDir()
	boundDir := filepath.Join(b.cfg.BrowserDir, "workspaces", "scout", "browser-profile")

	// Park a takeover on the bound key, the way the bound __request_takeover
	// seam parks it (the directory need not exist for begin to run).
	m.parkAttention(&attentionRequest{ID: "tk-bound", Kind: attentionTakeover, Profile: boundDir})
	res := callHandler(t, b.handleBeginTakeover, map[string]any{"requestID": "tk-bound"})
	require.False(t, res.IsError, "begin: %s", resultText(t, res))
	var active takeoverActiveResult
	require.NoError(t, json.Unmarshal([]byte(resultText(t, res)), &active))
	require.Equal(t, "active", active.Status)
	require.Equal(t, []string{boundDir}, launched, "the headed relaunch opens the bound directory")
	require.Equal(t, []bool{false}, headless, "the takeover relaunch is headed")

	held, release, err := m.acquire(context.Background(), b.cfg.BrowserDir, boundDir, b.cfg)
	require.NoError(t, err)
	require.True(t, held.held, "the relaunched session is held for the person")
	require.Equal(t, boundDir, held.dir)
	release() // resolveAttention takes the session lock: hand it back first
	require.True(t, m.resolveAttention("tk-bound"))
	require.False(t, held.held)

	// Teardown exports the jar into the bound directory.
	require.NoError(t, m.shutdown(boundDir))
	require.Equal(t, int32(1), closes.Load())
	jar, err := readCookieJar(boundDir)
	require.NoError(t, err)
	require.Len(t, jar.Cookies, 1, "the login state landed in the bound directory")
	require.Equal(t, "login-state", jar.Cookies[0].Value)

	// The next bound action relaunches on the same directory — the jar file is
	// there for the launch to re-inject — and the model's argument stays inert.
	res = callHandlerMeta(t, b.handleCookies,
		map[string]any{"op": "list", "profile": "other"},
		map[string]any{MetaProfileDir: boundDir, MetaProfileName: "scout"})
	require.True(t, res.IsError, "the stub browser answers the action with an error: %s", resultText(t, res))
	require.Equal(t, []string{boundDir, boundDir}, launched, "the bound profile relaunched on the same directory")
	s, ok := m.sessions[boundDir]
	require.True(t, ok)
	require.Equal(t, boundDir, s.dir)
	require.NotContains(t, m.sessions, "other")
	_, err = os.Stat(filepath.Join(b.cfg.BrowserDir, "profiles", "other"))
	require.True(t, os.IsNotExist(err))
	require.FileExists(t, cookieJarPath(boundDir), "the login state persists in the bound directory")
}

// TestHandleRequestSecret_BoundDir pins the secret prepare under binding: the
// freeze arms and the plan parks on the bound key (the parent delivers into
// that session), and the model's profile argument never resolves.
func TestHandleRequestSecret_BoundDir(t *testing.T) {
	boundDir := filepath.Join(t.TempDir(), "workspaces", "scout", "browser-profile")
	m := newManager()
	m.launch = stubLaunchPanicking(t)
	s := &profileSession{
		dir:         boundDir,
		originFn:    func(context.Context) (string, string) { return "https://site.test/sign-in", "Sign in" },
		isolationFn: func(context.Context, context.Context, string) error { return nil },
		snapshotFn: func(context.Context) ([]SnapshotElement, error) {
			return []SnapshotElement{{Ref: "e1", Role: "textbox", Name: "Password"}}, nil
		},
		probeFn: func(context.Context, context.Context, string) (secretProbe, error) {
			return secretProbe{Framed: false, FP: formFingerprint{LocalName: "input", Type: "password"}}, nil
		},
	}
	m.sessions[boundDir] = s
	b := &browserTools{cfg: DefaultConfig(), mgr: m}
	b.cfg.BrowserDir = t.TempDir()

	res := callHandlerMeta(t, b.handleRequestSecret, map[string]any{
		"targets": []any{"Password"}, "method": "password", "submission": "enter",
		"profile": "other",
	}, map[string]any{MetaProfileDir: boundDir, MetaProfileName: "scout"})
	require.False(t, res.IsError, "prepare: %s", resultText(t, res))
	var ready secretReadyResult
	require.NoError(t, json.Unmarshal([]byte(resultText(t, res)), &ready))
	require.Equal(t, "ready", ready.Status)

	require.NotNil(t, s.protection, "the freeze arms on the bound session")
	parked, ok := m.findAttention(ready.RequestID)
	require.True(t, ok, "the fill plan is parked for the parent")
	require.Equal(t, boundDir, parked.Profile, "the delivery targets the bound session")
	require.NotContains(t, m.sessions, "other", "the profile argument is ignored")
}

// TestProfileDir_AbsolutePassthrough pins the bound-key mapping: an absolute
// name is a bound profile directory (cleaned, traversal-checked), named keys
// keep their contract.
func TestProfileDir_AbsolutePassthrough(t *testing.T) {
	browserDir := t.TempDir()

	dir := filepath.Join(browserDir, "workspaces", "scout", "browser-profile")
	got, err := ProfileDir(browserDir, dir)
	require.NoError(t, err)
	require.Equal(t, dir, got, "a bound key passes through as the literal directory")

	got, err = ProfileDir(browserDir, dir+"/../browser-profile")
	require.NoError(t, err)
	require.Equal(t, dir, got, "traversal cleans to its absolute meaning, like the screenshot meta")

	_, err = ProfileDir(browserDir, "../escape")
	require.Error(t, err, "a relative traversal is never a directory")

	got, err = ProfileDir(browserDir, "work")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(browserDir, "profiles", "work"), got, "named keys keep the profile layout")

	_, err = ProfileDir(browserDir, filepath.Join("a", "b"))
	require.Error(t, err, "named keys still carry the name contract")
}
