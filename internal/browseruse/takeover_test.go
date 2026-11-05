package browseruse

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// TestHandleTakeover_GuardedRefusal pins the public surface: the handler
// never reaches the browser; the parent host parks the card.
func TestHandleTakeover_GuardedRefusal(t *testing.T) {
	b, _ := newTestTools(t, "work")
	res := callHandler(t, b.handleTakeover, map[string]any{
		"reason": "sign-in-blocked", "profile": "work",
	})
	require.True(t, res.IsError)
	require.Equal(t, "request_takeover requires the buddies host handover channel.", resultText(t, res))
}

// TestHandleRequestTakeover walks the parking validations.
func TestHandleRequestTakeover(t *testing.T) {
	t.Run("unknown profile is unavailable", func(t *testing.T) {
		b, _ := newTestTools(t, "work")
		res := callHandler(t, b.handleRequestTakeover, map[string]any{
			"reason": "sign-in-blocked", "profile": "ghost",
		})
		require.False(t, res.IsError)
		var got unavailableResult
		require.NoError(t, json.Unmarshal([]byte(resultText(t, res)), &got))
		require.Equal(t, "unavailable", got.Status)
		require.Equal(t, reasonProfileMissing, got.ReasonCode)
	})

	t.Run("bad reason is a readable tool error", func(t *testing.T) {
		b, _ := newTestTools(t, "work")
		res := callHandler(t, b.handleRequestTakeover, map[string]any{"reason": "bored"})
		require.True(t, res.IsError)
		require.Contains(t, resultText(t, res), "reason must be sign-in-blocked")
	})

	t.Run("a profile with an on-disk directory parks a request", func(t *testing.T) {
		b, m := newTestTools(t)
		b.cfg.BrowserDir = t.TempDir()
		dir, err := ProfileDir(b.cfg.BrowserDir, "saved")
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(dir, 0o700))

		res := callHandler(t, b.handleRequestTakeover, map[string]any{
			"reason": "sign-in-blocked", "profile": "saved",
		})
		require.False(t, res.IsError)
		var ready takeoverReadyResult
		require.NoError(t, json.Unmarshal([]byte(resultText(t, res)), &ready))
		require.Equal(t, "ready", ready.Status)
		require.Equal(t, "saved", ready.Profile)
		require.Len(t, ready.RequestID, 32)
		parked, ok := m.findAttention(ready.RequestID)
		require.True(t, ok)
		require.Equal(t, attentionTakeover, parked.Kind)
	})

	t.Run("a url parks on the entry for the headed landing", func(t *testing.T) {
		b, m := newTestTools(t)
		b.cfg.BrowserDir = t.TempDir()
		dir, err := ProfileDir(b.cfg.BrowserDir, "saved")
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(dir, 0o700))

		res := callHandler(t, b.handleRequestTakeover, map[string]any{
			"reason": "sign-in-blocked", "profile": "saved",
			"url": "https://www.linkedin.com/login",
		})
		require.False(t, res.IsError)
		var ready takeoverReadyResult
		require.NoError(t, json.Unmarshal([]byte(resultText(t, res)), &ready))
		parked, ok := m.findAttention(ready.RequestID)
		require.True(t, ok)
		require.Equal(t, "https://www.linkedin.com/login", parked.URL)
	})

	t.Run("an invalid url refuses the request and parks nothing", func(t *testing.T) {
		b, m := newTestTools(t)
		b.cfg.BrowserDir = t.TempDir()
		dir, err := ProfileDir(b.cfg.BrowserDir, "saved")
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(dir, 0o700))

		// A private address is refused under the default (no allow-private)
		// policy, exactly like the navigate tool.
		res := callHandler(t, b.handleRequestTakeover, map[string]any{
			"reason": "sign-in-blocked", "profile": "saved",
			"url": "http://127.0.0.1:3000/admin",
		})
		require.True(t, res.IsError)
		require.Contains(t, resultText(t, res), "invalid takeover url")
		require.Empty(t, m.attention, "the refused request parked nothing")
	})
}

// TestHandleBeginTakeover walks the headed relaunch, the hold, and the
// resolution endings on a stubbed launch.
func TestHandleBeginTakeover(t *testing.T) {
	t.Run("unknown requestID is inert", func(t *testing.T) {
		b, _ := newTestTools(t, "work")
		res := callHandler(t, b.handleBeginTakeover, map[string]any{"requestID": "unknown"})
		require.True(t, res.IsError)
		require.Equal(t, inertRefusal, resultText(t, res))
	})

	t.Run("headed relaunch, hold, and resolve endings", func(t *testing.T) {
		m := newManager()
		var launchedHeadless []bool
		var launches atomic.Int32
		m.launch = func(_ context.Context, dir string, cfg Config) (*profileSession, error) {
			launches.Add(1)
			launchedHeadless = append(launchedHeadless, cfg.Headless)
			return &profileSession{dir: dir, closeFn: func() error { return nil }}, nil
		}
		b := &browserTools{cfg: DefaultConfig(), mgr: m}
		b.cfg.BrowserDir = t.TempDir()

		// The profile's live session is headless, as configured.
		_, release, err := m.acquire(context.Background(), b.cfg.BrowserDir, "work", b.cfg)
		require.NoError(t, err)
		release()
		require.Equal(t, []bool{true}, launchedHeadless)

		// Park and begin the takeover.
		m.parkAttention(&attentionRequest{ID: "tk-1", Kind: attentionTakeover, Profile: "work"})
		res := callHandler(t, b.handleBeginTakeover, map[string]any{"requestID": "tk-1"})
		require.False(t, res.IsError, "begin succeeds: %s", resultText(t, res))
		var active takeoverActiveResult
		require.NoError(t, json.Unmarshal([]byte(resultText(t, res)), &active))
		require.Equal(t, "active", active.Status)
		require.Equal(t, []bool{true, false}, launchedHeadless,
			"the takeover relaunch overrides the headless config for this launch only")

		// The relaunched session is held: every public tool refuses.
		held, releaseHeld, err := m.acquire(context.Background(), b.cfg.BrowserDir, "work", b.cfg)
		require.NoError(t, err)
		require.True(t, held.held, "the session is marked held")
		require.Equal(t, "tk-1", held.heldRequestID)
		require.Empty(t, held.elements, "refs and the snapshot cache died with the old browser")
		releaseHeld() // hand the session back before driving handlers through acquire
		for _, tc := range []struct {
			name string
			h    func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
			args map[string]any
		}{
			{"snapshot", b.handleSnapshot, map[string]any{"profile": "work"}},
			{"navigate", b.handleNavigate, map[string]any{"url": "https://93.184.216.34/", "profile": "work"}},
			{"cookies", b.handleCookies, map[string]any{"op": "list", "profile": "work"}},
			{"wait", b.handleWait, map[string]any{"profile": "work"}},
		} {
			t.Run("held "+tc.name+" refuses", func(t *testing.T) {
				res := callHandler(t, tc.h, tc.args)
				require.True(t, res.IsError)
				require.Equal(t, heldRefusal, resultText(t, res))
			})
		}

		// The next launch (the idle respawn boundary) restores the config.
		_, releaseFresh, err := m.acquire(context.Background(), b.cfg.BrowserDir, "fresh", b.cfg)
		require.NoError(t, err)
		releaseFresh()
		require.Equal(t, []bool{true, false, true}, launchedHeadless,
			"the next idle respawn launches from the config again")

		// Done: resolve ends the hold.
		res = callHandler(t, b.handleResolveAttention, map[string]any{"requestID": "tk-1", "outcome": "completed"})
		require.False(t, res.IsError)
		var resolved attentionResolvedResult
		require.NoError(t, json.Unmarshal([]byte(resultText(t, res)), &resolved))
		require.Equal(t, "resolved", resolved.Status)
		require.False(t, held.held, "the held state ended")
		_, parked := m.findAttention("tk-1")
		require.False(t, parked)
		require.NoError(t, held.checkHeld())
	})

	t.Run("relaunch failure ends the request", func(t *testing.T) {
		m := newManager()
		m.launch = func(context.Context, string, Config) (*profileSession, error) {
			return nil, errors.New("chromium missing")
		}
		b := &browserTools{cfg: DefaultConfig(), mgr: m}
		b.cfg.BrowserDir = t.TempDir()
		m.parkAttention(&attentionRequest{ID: "tk-2", Kind: attentionTakeover, Profile: "work"})
		res := callHandler(t, b.handleBeginTakeover, map[string]any{"requestID": "tk-2"})
		require.False(t, res.IsError)
		var got channelFailedResult
		require.NoError(t, json.Unmarshal([]byte(resultText(t, res)), &got))
		require.Equal(t, "failed", got.Status)
		require.Equal(t, reasonRelaunchFailed, got.ReasonCode)
		_, parked := m.findAttention("tk-2")
		require.False(t, parked, "the failed takeover cannot be begun again")
	})
}

// TestHandleResolveAttention covers the outcome enum and the inert refusal.
func TestHandleResolveAttention(t *testing.T) {
	b, m := newTestTools(t, "work")

	t.Run("unknown requestID is inert", func(t *testing.T) {
		res := callHandler(t, b.handleResolveAttention, map[string]any{"requestID": "nope", "outcome": "completed"})
		require.True(t, res.IsError)
		require.Equal(t, inertRefusal, resultText(t, res))
	})

	t.Run("bad outcome is a readable tool error", func(t *testing.T) {
		res := callHandler(t, b.handleResolveAttention, map[string]any{"requestID": "x", "outcome": "maybe"})
		require.True(t, res.IsError)
		require.Contains(t, resultText(t, res), "outcome must be completed, cancelled, or expired")
	})

	t.Run("every outcome releases without filling", func(t *testing.T) {
		for i, outcome := range []string{"completed", "cancelled", "expired"} {
			id := "req-" + outcome
			s := m.sessions["work"]
			armedProtection(s, id, "canary-123")
			s.holdSession(id)
			m.attention[id] = &attentionRequest{ID: id, Kind: attentionSecret, Profile: "work"}

			res := callHandler(t, b.handleResolveAttention, map[string]any{"requestID": id, "outcome": outcome})
			require.False(t, res.IsError)
			require.Nil(t, s.protection, "outcome %s releases the freeze", outcome)
			require.False(t, s.held, "outcome %s releases the hold", outcome)
			_, parked := m.findAttention(id)
			require.False(t, parked, "outcome %s drops the parked request", outcome)
			_ = i
		}
	})
}

// TestResolveAttentionReleasesAcrossRespawn pins the frozen-browser edge: the
// idle janitor never reaps a frozen browser, resolution ends the freeze, and
// a respawn afterwards starts clean (no freeze survives the old session
// object).
func TestResolveAttentionReleasesAcrossRespawn(t *testing.T) {
	m := newManager()
	var closes atomic.Int32
	var minutes atomic.Int64
	base := time.Now()
	m.now = func() time.Time { return base.Add(time.Duration(minutes.Load()) * time.Minute) }
	m.launch = stubLaunch(&closes)
	browserDir := t.TempDir()

	s, release, err := m.acquire(context.Background(), browserDir, "work", DefaultConfig())
	require.NoError(t, err)
	s.beginProtection("req-1")
	release()
	m.attention["req-1"] = &attentionRequest{ID: "req-1", Kind: attentionSecret, Profile: "work"}

	// The janitor must not reap the frozen browser (no timeout on holds).
	minutes.Store(10)
	m.sweep()
	require.Equal(t, int32(0), closes.Load())
	m.mu.Lock()
	_, frozenLive := m.sessions["work"]
	m.mu.Unlock()
	require.True(t, frozenLive)

	// Resolution ends the freeze; the browser becomes sweepable again.
	require.True(t, m.resolveAttention("req-1"))
	_, parked := m.findAttention("req-1")
	require.False(t, parked)
	minutes.Store(20)
	m.sweep()
	require.Equal(t, int32(1), closes.Load())

	// A respawn starts clean: no freeze survives the old session object.
	s2, release2, err := m.acquire(context.Background(), browserDir, "work", DefaultConfig())
	require.NoError(t, err)
	defer release2()
	require.Nil(t, s2.protection)
	require.False(t, s2.held)
}

// TestHeldSessionJarExport pins that a held refusal still exports the cookie
// jar (the withSession wrapper always saves after the action).
func TestHeldSessionJarExport(t *testing.T) {
	m := newManager()
	var closes, exports atomic.Int32
	m.now = time.Now
	m.launch = func(_ context.Context, dir string, _ Config) (*profileSession, error) {
		return &profileSession{
			dir: dir,
			closeFn: func() error {
				closes.Add(1)
				return nil
			},
			exportFn: func() error {
				exports.Add(1)
				return nil
			},
		}, nil
	}
	b := &browserTools{cfg: DefaultConfig(), mgr: m}
	b.cfg.BrowserDir = t.TempDir()

	_, release, err := m.acquire(context.Background(), b.cfg.BrowserDir, "work", b.cfg)
	require.NoError(t, err)
	m.sessions["work"].holdSession("tk")
	release()

	_, err = b.withSession(context.Background(), "work", func(_ context.Context, _ *profileSession) (any, error) {
		return nil, nil
	})
	require.ErrorContains(t, err, "A person controls this browser")
	require.Equal(t, int32(1), exports.Load(), "even the held refusal exports the jar")

	require.NoError(t, m.shutdown("work"))
	require.Equal(t, int32(1), closes.Load())
}

// TestProfileExists covers the takeover profile validation.
func TestProfileExists(t *testing.T) {
	m := newManager()
	browserDir := t.TempDir()

	require.Error(t, m.profileExists(browserDir, "ghost"))

	dir, err := ProfileDir(browserDir, "saved")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, m.profileExists(browserDir, "saved"))

	m.sessions["live"] = &profileSession{}
	require.NoError(t, m.profileExists(browserDir, "live"), "a live session counts even without a directory")

	require.Error(t, m.profileExists(browserDir, "../escape"), "invalid names never pass validation")
	require.Error(t, m.profileExists(browserDir, filepath.Join("a", "b")))
}

// TestManager_SweepSkipsAttentionHolds pins the no-timeout design: the idle
// janitor never closes a takeover-held session or a secret-frozen one, and
// the hold survives until resolution.
func TestManager_SweepSkipsAttentionHolds(t *testing.T) {
	m := newManager()
	var closes atomic.Int32
	var minutes atomic.Int64
	base := time.Now()
	m.now = func() time.Time { return base.Add(time.Duration(minutes.Load()) * time.Minute) }
	m.launch = stubLaunch(&closes)
	browserDir := t.TempDir()
	cfg := DefaultConfig()

	held, releaseHeld, err := m.acquire(context.Background(), browserDir, "held", cfg)
	require.NoError(t, err)
	held.holdSession("tk-1")
	releaseHeld()
	m.attention["tk-1"] = &attentionRequest{ID: "tk-1", Kind: attentionTakeover, Profile: "held"}

	frozen, releaseFrozen, err := m.acquire(context.Background(), browserDir, "frozen", cfg)
	require.NoError(t, err)
	frozen.beginProtection("req-1")
	releaseFrozen()
	m.attention["req-1"] = &attentionRequest{ID: "req-1", Kind: attentionSecret, Profile: "frozen"}

	_, releasePlain, err := m.acquire(context.Background(), browserDir, "plain", cfg)
	require.NoError(t, err)
	releasePlain()

	// Well past the idle TTL: only the unheld session is reaped.
	minutes.Store(10)
	m.sweep()
	require.Equal(t, int32(1), closes.Load(), "only the plain idle session is reaped")
	m.mu.Lock()
	_, heldLive := m.sessions["held"]
	_, frozenLive := m.sessions["frozen"]
	m.mu.Unlock()
	require.True(t, heldLive, "the takeover-held browser outlives the idle TTL")
	require.True(t, frozenLive, "the secret-frozen browser outlives the idle TTL")

	// After resolution, the sessions are sweepable again.
	require.True(t, m.resolveAttention("tk-1"))
	require.True(t, m.resolveAttention("req-1"))
	minutes.Store(20)
	m.sweep()
	require.Equal(t, int32(3), closes.Load(), "resolved sessions sweep normally")
}
