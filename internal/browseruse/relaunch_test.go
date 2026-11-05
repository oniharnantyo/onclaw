package browseruse

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto"
	"github.com/stretchr/testify/require"
)

// The dead-session relaunch: a cached browser that stopped answering CDP
// calls (a crash, a person closing the window, a headed takeover swap) must
// be discarded and the profile relaunched at acquire, never handed out with
// stale handles. The stubs drive the probe seam (aliveFn); the loopback
// fixture in crash_browser_test.go proves the real Chromium path.

func TestAcquire_RelaunchesDeadSession(t *testing.T) {
	m := newManager()
	var launches, closes atomic.Int32
	alive := &atomic.Bool{}
	alive.Store(true)
	m.now = time.Now
	m.launch = func(_ context.Context, dir string, _ Config) (*profileSession, error) {
		launches.Add(1)
		return &profileSession{dir: dir, aliveFn: alive.Load, closeFn: func() error {
			closes.Add(1)
			return nil
		}}, nil
	}
	cfg := Config{Headless: true}
	browserDir := t.TempDir()

	s1, release, err := m.acquire(context.Background(), browserDir, "work", cfg)
	require.NoError(t, err)
	release()
	require.Equal(t, int32(1), launches.Load())

	alive.Store(false) // the browser died underneath the manager
	s2, release2, err := m.acquire(context.Background(), browserDir, "work", cfg)
	require.NoError(t, err)
	release2()
	require.False(t, s1 == s2, "a dead session must not be handed out again")
	require.Equal(t, int32(2), launches.Load(), "the dead profile must relaunch")
	require.Equal(t, int32(1), closes.Load(), "the dead session must be discarded exactly once")

	m.mu.Lock()
	_, live := m.sessions["work"]
	m.mu.Unlock()
	require.True(t, live, "the relaunched session must be the cached one")
}

func TestAcquire_KeepsAliveSession(t *testing.T) {
	m := newManager()
	var launches, closes atomic.Int32
	m.now = time.Now
	m.launch = func(_ context.Context, dir string, _ Config) (*profileSession, error) {
		launches.Add(1)
		return &profileSession{dir: dir, aliveFn: func() bool { return true }, closeFn: func() error {
			closes.Add(1)
			return nil
		}}, nil
	}
	cfg := Config{Headless: true}
	browserDir := t.TempDir()

	s1, release, err := m.acquire(context.Background(), browserDir, "work", cfg)
	require.NoError(t, err)
	release()

	s2, release2, err := m.acquire(context.Background(), browserDir, "work", cfg)
	require.NoError(t, err)
	release2()
	require.True(t, s1 == s2, "a live session must be reused")
	require.Equal(t, int32(1), launches.Load())
	require.Equal(t, int32(0), closes.Load())
}

func TestAcquire_UnprobedSessionAssumedAlive(t *testing.T) {
	// Stubs without the probe seam (every pre-existing test session) must
	// keep today's reuse behavior — the probe is the launcher's wiring, not
	// the manager's assumption.
	m := newManager()
	var launches atomic.Int32
	m.now = time.Now
	m.launch = func(_ context.Context, dir string, _ Config) (*profileSession, error) {
		launches.Add(1)
		return &profileSession{dir: dir, closeFn: func() error { return nil }}, nil
	}
	cfg := Config{Headless: true}
	browserDir := t.TempDir()

	s1, release, err := m.acquire(context.Background(), browserDir, "work", cfg)
	require.NoError(t, err)
	release()

	s2, release2, err := m.acquire(context.Background(), browserDir, "work", cfg)
	require.NoError(t, err)
	release2()
	require.True(t, s1 == s2, "a session without a probe must be reused as today")
	require.Equal(t, int32(1), launches.Load())
}

func TestAcquire_AttentionHoldNeverRelaunched(t *testing.T) {
	// A session parked in a handover arc (held takeover, in-flight secret
	// protection) decides its own resolution: even a dead browser underneath
	// it is not the manager's to discard.
	for name, hold := range map[string]func(*profileSession){
		"held takeover": func(s *profileSession) { s.held = true },
		"secret protection": func(s *profileSession) {
			s.protection = &protectionState{}
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := newManager()
			var launches atomic.Int32
			m.now = time.Now
			m.launch = func(_ context.Context, dir string, _ Config) (*profileSession, error) {
				launches.Add(1)
				return &profileSession{dir: dir, aliveFn: func() bool { return false }, closeFn: func() error { return nil }}, nil
			}
			cfg := Config{Headless: true}
			browserDir := t.TempDir()

			s1, release, err := m.acquire(context.Background(), browserDir, "work", cfg)
			require.NoError(t, err)
			release()
			s1.mu.Lock()
			hold(s1)
			s1.mu.Unlock()

			s2, release2, err := m.acquire(context.Background(), browserDir, "work", cfg)
			require.NoError(t, err)
			release2()
			require.True(t, s1 == s2, "a held session must be handed out as-is; its arc decides resolution")
			require.Equal(t, int32(1), launches.Load(), "a held session must never relaunch")
		})
	}
}

func TestReadableCdpError(t *testing.T) {
	t.Run("nil passes through untouched", func(t *testing.T) {
		require.Nil(t, readableCdpError(nil))
	})

	t.Run("non-CDP errors pass through untouched", func(t *testing.T) {
		err := context.DeadlineExceeded
		require.Equal(t, err, readableCdpError(err))
	})

	t.Run("session-not-found carries the relaunch hint", func(t *testing.T) {
		// -32001 is the code Chrome answers Target-scoped calls with when the
		// session id is gone; cdproto has no named constant for it.
		wrapped := navigateErr(&cdproto.Error{Code: -32001, Message: "Session with given id not found."})
		out := readableCdpError(wrapped)
		text := out.Error()
		require.Contains(t, text, "Session with given id not found")
		require.Contains(t, text, "relaunches on the next call",
			"the model must be told the retry path, not the raw struct")
		require.NotContains(t, text, "{-32001", "the raw struct print must never reach the model")
	})

	t.Run("other CDP errors keep their message, struct dropped", func(t *testing.T) {
		out := readableCdpError(&cdproto.Error{Code: -32000, Message: "Cannot find context with specified id"})
		require.Contains(t, out.Error(), "Cannot find context with specified id")
		require.NotContains(t, out.Error(), "{-32000")
	})
}

// navigateErr mimics the action wrappers ("browser: navigate: %w") the
// handlers put around CDP errors.
func navigateErr(err error) error {
	return fmt.Errorf("browser: navigate: %w", err)
}
