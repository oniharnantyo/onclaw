package browseruse

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// stubLaunch closes counting: every test manager below swaps m.launch so no
// test ever starts a real Chromium.
func stubLaunch(closes *atomic.Int32) func(context.Context, string, Config) (*profileSession, error) {
	return func(_ context.Context, dir string, _ Config) (*profileSession, error) {
		return &profileSession{dir: dir, closeFn: func() error {
			closes.Add(1)
			return nil
		}}, nil
	}
}

func TestManager_ReaperClosesIdleSessions(t *testing.T) {
	m := newManager()
	var closes atomic.Int32
	var now atomic.Int64
	m.now = func() time.Time { return time.Unix(now.Load(), 0) }
	m.launch = stubLaunch(&closes)
	browserDir := t.TempDir()
	cfg := Config{Headless: true, IdleTimeoutSeconds: 60}

	s, release, err := m.acquire(context.Background(), browserDir, "work", cfg)
	require.NoError(t, err)
	require.Equal(t, 60*time.Second, s.idleTTL, "idle TTL must be recorded on the session at launch")
	release()

	now.Store(30) // 30 s of idle: inside the budget
	m.sweep()
	require.Equal(t, int32(0), closes.Load(), "session inside its idle budget must survive")

	now.Store(61) // past the 60 s budget
	m.sweep()
	require.Equal(t, int32(1), closes.Load(), "session past its idle budget must be closed")
	m.mu.Lock()
	_, live := m.sessions["work"]
	m.mu.Unlock()
	require.False(t, live, "reaped session must leave the registry")

	// Re-acquiring re-launches a fresh session on the same profile.
	s2, release2, err := m.acquire(context.Background(), browserDir, "work", cfg)
	require.NoError(t, err)
	release2()
	require.False(t, s == s2, "reaped profile must get a new session")
}

func TestManager_SweepSkipsBusySession(t *testing.T) {
	m := newManager()
	var closes atomic.Int32
	var now atomic.Int64
	m.now = func() time.Time { return time.Unix(now.Load(), 0) }
	m.launch = stubLaunch(&closes)
	cfg := Config{IdleTimeoutSeconds: 60}

	// acquire hands out the session with its mutex held: an action is "in
	// flight" until release.
	_, release, err := m.acquire(context.Background(), t.TempDir(), "work", cfg)
	require.NoError(t, err)

	now.Store(120) // nominally long idle, but the session is busy
	m.sweep()
	require.Equal(t, int32(0), closes.Load(), "a session with an action in flight must not be reaped")

	release()
	m.sweep()
	require.Equal(t, int32(1), closes.Load(), "once idle, the session must be reaped")
}

func TestManager_SessionReuseAndSerialization(t *testing.T) {
	m := newManager()
	var launches atomic.Int32
	m.now = time.Now
	m.launch = func(_ context.Context, dir string, _ Config) (*profileSession, error) {
		launches.Add(1)
		return &profileSession{dir: dir, closeFn: func() error { return nil }}, nil
	}
	browserDir := t.TempDir()
	cfg := DefaultConfig()

	s1, release1, err := m.acquire(context.Background(), browserDir, "work", cfg)
	require.NoError(t, err)
	require.Equal(t, int32(1), launches.Load())

	// A second acquire on the same profile must block until the first
	// releases: actions on one profile are serialized.
	acquired := make(chan *profileSession, 1)
	go func() {
		s2, release2, err := m.acquire(context.Background(), browserDir, "work", cfg)
		if err != nil {
			t.Error(err)
			return
		}
		release2()
		acquired <- s2
	}()
	select {
	case s := <-acquired:
		t.Fatalf("second acquire must wait for the first to release; got %v", s)
	case <-time.After(150 * time.Millisecond):
	}
	release1()
	select {
	case s2 := <-acquired:
		require.True(t, s1 == s2, "the same profile must reuse its session")
	case <-time.After(2 * time.Second):
		t.Fatal("second acquire never completed after release")
	}
	require.Equal(t, int32(1), launches.Load(), "same profile must not relaunch")

	// A different profile is independent: no blocking, own session.
	s3, release3, err := m.acquire(context.Background(), browserDir, "personal", cfg)
	require.NoError(t, err)
	release3()
	require.False(t, s1 == s3, "different profiles must not share a session")
	require.Equal(t, int32(2), launches.Load())
}

func TestManager_Shutdown(t *testing.T) {
	m := newManager()
	var closes atomic.Int32
	m.now = time.Now
	m.launch = stubLaunch(&closes)
	cfg := DefaultConfig()

	_, release, err := m.acquire(context.Background(), t.TempDir(), "work", cfg)
	require.NoError(t, err)
	release()

	require.NoError(t, m.shutdown("work"))
	require.Equal(t, int32(1), closes.Load(), "shutdown must close the live browser")
	m.mu.Lock()
	_, live := m.sessions["work"]
	m.mu.Unlock()
	require.False(t, live)

	require.NoError(t, m.shutdown("work"), "shutting down an already-closed profile is a no-op")
	require.Equal(t, int32(1), closes.Load())
}

func TestManager_ShutdownAll(t *testing.T) {
	m := newManager()
	var closes atomic.Int32
	m.now = time.Now
	m.launch = stubLaunch(&closes)
	cfg := DefaultConfig()

	for _, name := range []string{"work", "personal", "default"} {
		_, release, err := m.acquire(context.Background(), t.TempDir(), name, cfg)
		require.NoError(t, err)
		release()
	}

	require.NoError(t, m.shutdownAll())
	require.Equal(t, int32(3), closes.Load(), "shutdownAll must close every live browser")
	m.mu.Lock()
	live := len(m.sessions)
	m.mu.Unlock()
	require.Equal(t, 0, live, "shutdownAll must empty the registry")

	require.NoError(t, m.shutdownAll(), "shutdownAll on an empty registry is a no-op")
	require.Equal(t, int32(3), closes.Load())
}

func TestShutdownBrowserProfile_UnknownName(t *testing.T) {
	require.NoError(t, ShutdownBrowserProfile("no-such-profile"), "an unknown profile must not error")
}

func TestSeedProfilePreferences(t *testing.T) {
	t.Run("absent preferences are seeded before first launch", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, seedProfilePreferences(dir))

		b, err := os.ReadFile(filepath.Join(dir, "Default", "Preferences"))
		require.NoError(t, err)
		require.Equal(t, profilePreferences, string(b))

		info, err := os.Stat(filepath.Join(dir, "Default"))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0), info.Mode().Perm()&0o077, "Default dir must be private to the user")
		info, err = os.Stat(filepath.Join(dir, "Default", "Preferences"))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0), info.Mode().Perm()&0o077, "Preferences must be private to the user")
	})

	t.Run("existing preferences are never touched", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "Default"), 0o700))
		prefsPath := filepath.Join(dir, "Default", "Preferences")
		require.NoError(t, os.WriteFile(prefsPath, []byte(`{"profile":{"exit_type":"Normal"}}`), 0o600))

		require.NoError(t, seedProfilePreferences(dir))

		b, err := os.ReadFile(prefsPath)
		require.NoError(t, err)
		require.Equal(t, `{"profile":{"exit_type":"Normal"}}`, string(b), "Chrome owns the file once it exists")
	})
}

func TestRemoveStaleSingletonLocks(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie", "Preferences", "Cookies"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("stale"), 0o600))
	}
	removeStaleSingletonLocks(dir)
	for _, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		_, err := os.Stat(filepath.Join(dir, name))
		require.True(t, os.IsNotExist(err), "%s must be removed", name)
	}
	for _, name := range []string{"Preferences", "Cookies"} {
		_, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err, "%s is profile state and must survive", name)
	}
}

func TestManager_AcquirePreparesProfileDir(t *testing.T) {
	m := newManager()
	var launchedDir string
	m.now = time.Now
	m.launch = func(_ context.Context, dir string, _ Config) (*profileSession, error) {
		launchedDir = dir
		return &profileSession{dir: dir, closeFn: func() error { return nil }}, nil
	}
	browserDir := t.TempDir()
	cfg := DefaultConfig()

	wantDir, err := ProfileDir(browserDir, "work")
	require.NoError(t, err)
	// A Chromium lock left by a crashed browser must not block the relaunch.
	require.NoError(t, os.MkdirAll(wantDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(wantDir, "SingletonLock"), []byte("x"), 0o600))

	_, release, err := m.acquire(context.Background(), browserDir, "work", cfg)
	require.NoError(t, err)
	defer release()
	require.Equal(t, wantDir, launchedDir, "launch must target the profile dir")

	info, err := os.Stat(wantDir)
	require.NoError(t, err, "acquire must create the profile dir")
	require.Equal(t, os.FileMode(0), info.Mode().Perm()&0o077, "profile dir must be private to the user")

	_, err = os.Stat(filepath.Join(wantDir, "SingletonLock"))
	require.True(t, os.IsNotExist(err), "stale SingletonLock must be removed before launch")
}

// TestBrowserTools_WithSessionExportsJarAfterAction pins the jar durability
// contract: the export runs after every completed action — including failed
// ones — because a jar only saved at teardown would lose cookies written
// between actions, breaking respawn-at-idle.
func TestBrowserTools_WithSessionExportsJarAfterAction(t *testing.T) {
	m := newManager()
	var closes atomic.Int32
	var exports atomic.Int32
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

	_, err := b.withSession(context.Background(), "work", func(_ context.Context, _ *profileSession) (any, error) {
		return nil, nil
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), exports.Load(), "the jar must export after a successful action")

	_, err = b.withSession(context.Background(), "work", func(_ context.Context, _ *profileSession) (any, error) {
		return nil, errors.New("action failed")
	})
	require.Error(t, err)
	require.Equal(t, int32(2), exports.Load(), "the jar must export even after a failed action")

	require.NoError(t, m.shutdown("work"))
	require.Equal(t, int32(1), closes.Load())
	require.Equal(t, int32(3), exports.Load(), "teardown must export the jar a final time")
}
