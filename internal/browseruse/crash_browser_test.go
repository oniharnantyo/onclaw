package browseruse

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// The loopback dead-browser regression (user-reported): a cached profile
// browser that dies underneath the manager used to fail every following
// action with rod's raw CDP error ("{-32001 Session with given id not found.
// }") until the child restarted. The manager must relaunch the profile from
// its persisted state on the next call instead.

func TestBrowserTools_NavigateRelaunchesDeadBrowser(t *testing.T) {
	b := newBrowserTools(t)
	srv := newFixtureServer(t)

	res := callHandler(t, b.handleNavigate, map[string]any{"url": srv.URL})
	require.False(t, res.IsError, "first navigate must prime the profile: %s", resultText(t, res))

	// The browser dies underneath the manager, the way a crash or a person
	// closing the window would: the manager still caches the session, and
	// its handles are now stale.
	profile, err := b.profile("")
	require.NoError(t, err)
	s, release, err := b.mgr.acquire(context.Background(), b.cfg.BrowserDir, profile, b.cfg)
	require.NoError(t, err)
	require.NoError(t, s.proc.killNow())
	release()

	// The next navigate relaunches the profile from its persisted state
	// instead of failing on the stale CDP session.
	res = callHandler(t, b.handleNavigate, map[string]any{"url": srv.URL})
	require.False(t, res.IsError, "navigate after the browser died must relaunch, got: %s", resultText(t, res))
}
