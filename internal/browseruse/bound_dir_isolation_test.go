package browseruse

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The bound-profile isolation at the server layer (add-per-agent-browser-
// profiles task 4.1, spec "Bound profiles are isolated"): two calls binding
// two different directories (two agents) resolve to two distinct live
// sessions keyed by their absolute directories, and each directory keeps its
// own cookie jar — a cookie set through one is invisible through the other.
// A direct call with no binding metadata keeps resolving the named profile
// under <browserDir>/profiles/<name> (the fallback regression; the full
// unbound behavior table lives in profile_binding_test.go).

// isolatedJarLaunch is a launch stub recording the launched directories and
// handing out stub sessions whose exporter writes one login cookie into the
// launched session's own directory — the way the real exporter writes
// cookieJarPath(s.dir) — with the cookie value keyed by directory.
func isolatedJarLaunch(launched *[]string, jarValue map[string]string) func(context.Context, string, Config) (*profileSession, error) {
	return func(_ context.Context, dir string, _ Config) (*profileSession, error) {
		*launched = append(*launched, dir)
		return &profileSession{
			dir:        dir,
			pageCtx:    context.Background(),
			browserCtx: context.Background(),
			closeFn:    func() error { return nil },
			exportFn: func() error {
				jar := CookieJarFile{Cookies: []StoredCookie{{
					Name: "session", Value: jarValue[dir], Domain: "site.test", Path: "/",
				}}}
				return writeCookieJar(dir, jar)
			},
		}, nil
	}
}

// TestTwoBoundDirs_RunInIsolatedSessionsAndJars drives two bound navigate
// calls through one server: each resolves to its own live session keyed by
// its absolute directory, the model-supplied profile argument never resolves,
// and the per-directory jar files stay disjoint.
func TestTwoBoundDirs_RunInIsolatedSessionsAndJars(t *testing.T) {
	root := t.TempDir()
	scoutDir := filepath.Join(root, "workspaces", "scout", "browser-profile")
	houndDir := filepath.Join(root, "workspaces", "hound", "browser-profile")

	m := newManager()
	var launched []string
	m.launch = isolatedJarLaunch(&launched, map[string]string{
		scoutDir: "scout-login", houndDir: "hound-login",
	})
	b := &browserTools{cfg: DefaultConfig(), mgr: m}
	b.cfg.BrowserDir = t.TempDir()

	// Two agents' bound calls, each with a model-supplied profile argument
	// the binding must ignore.
	res := callHandlerMeta(t, b.handleNavigate,
		map[string]any{"url": "https://93.184.216.34/", "profile": "other"},
		map[string]any{MetaProfileDir: scoutDir, MetaProfileName: "scout"})
	require.True(t, res.IsError, "the stub browser answers every action with an error: %s", resultText(t, res))
	res = callHandlerMeta(t, b.handleNavigate,
		map[string]any{"url": "https://93.184.216.34/", "profile": "other"},
		map[string]any{MetaProfileDir: houndDir, MetaProfileName: "hound"})
	require.True(t, res.IsError, "the stub browser answers every action with an error: %s", resultText(t, res))

	// Two distinct live sessions, each keyed by its absolute directory.
	s, ok := m.sessions[scoutDir]
	require.True(t, ok, "the bound directory keys the sessions map")
	h, ok := m.sessions[houndDir]
	require.True(t, ok, "the second bound directory keys its own session")
	require.NotSame(t, s, h, "each bound directory runs its own live session")
	require.Equal(t, scoutDir, s.dir, "the session runs on the bound user-data directory")
	require.Equal(t, houndDir, h.dir)
	require.Len(t, m.sessions, 2, "the two bound directories key exactly two sessions")
	require.NotContains(t, m.sessions, "other", "the profile argument is ignored under binding")
	require.Equal(t, []string{scoutDir, houndDir}, launched,
		"each call launched into its own directory only")
	_, err := os.Stat(filepath.Join(b.cfg.BrowserDir, "profiles", "other"))
	require.True(t, os.IsNotExist(err), "no profile named after the model's argument is created")

	// Jar-level isolation: each directory keeps its own buddies-cookies.json
	// (the withSession epilogue exported through the session's own dir), so a
	// cookie set through one is invisible through the other.
	require.FileExists(t, cookieJarPath(scoutDir))
	require.FileExists(t, cookieJarPath(houndDir))
	scoutJar, err := readCookieJar(scoutDir)
	require.NoError(t, err)
	require.Len(t, scoutJar.Cookies, 1)
	require.Equal(t, "scout-login", scoutJar.Cookies[0].Value)
	houndJar, err := readCookieJar(houndDir)
	require.NoError(t, err)
	require.Len(t, houndJar.Cookies, 1)
	require.Equal(t, "hound-login", houndJar.Cookies[0].Value)
	for _, c := range scoutJar.Cookies {
		require.NotEqual(t, "hound-login", c.Value, "scout's jar carries no hound cookie")
	}
	for _, c := range houndJar.Cookies {
		require.NotEqual(t, "scout-login", c.Value, "hound's jar carries no scout cookie")
	}
}

// TestUnboundDirectCallResolvesNamedProfile is the thin fallback regression
// pin (task 4.1, spec "Unbound calls keep named-profile behavior"): a direct
// browser-mcp call with no binding metadata resolves
// <browserDir>/profiles/<name> exactly as before the change.
func TestUnboundDirectCallResolvesNamedProfile(t *testing.T) {
	m, launched := boundTestManager(t)
	b := &browserTools{cfg: DefaultConfig(), mgr: m}
	browserDir := t.TempDir()
	b.cfg.BrowserDir = browserDir

	res := callHandlerMeta(t, b.handleNavigate,
		map[string]any{"url": "https://93.184.216.34/", "profile": "work"}, nil)
	require.True(t, res.IsError, "the stub browser answers every action with an error: %s", resultText(t, res))

	s, ok := m.sessions["work"]
	require.True(t, ok, "a call without the binding names the profile again")
	require.Equal(t, filepath.Join(browserDir, "profiles", "work"), s.dir)
	require.Equal(t, []string{filepath.Join(browserDir, "profiles", "work")}, *launched)
	require.DirExists(t, filepath.Join(browserDir, "profiles", "work"))
}
