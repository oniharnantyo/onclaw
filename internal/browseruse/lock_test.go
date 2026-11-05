package browseruse

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAcquireBrowserLock(t *testing.T) {
	t.Run("creates the lock with pid and timestamp", func(t *testing.T) {
		dir := t.TempDir()
		release, err := AcquireBrowserLock(dir)
		require.NoError(t, err)
		defer release()

		data, err := os.ReadFile(filepath.Join(dir, lockFileName))
		require.NoError(t, err)
		var record LockFile
		require.NoError(t, json.Unmarshal(data, &record))
		require.Equal(t, os.Getpid(), record.PID)
		require.False(t, record.StartedAt.IsZero())

		info, err := os.Stat(filepath.Join(dir, lockFileName))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0), info.Mode().Perm()&0o077, "the lock is private to the user")
	})

	t.Run("double run is refused with the owner pid", func(t *testing.T) {
		dir := t.TempDir()
		release, err := AcquireBrowserLock(dir)
		require.NoError(t, err)
		defer release()

		_, err = AcquireBrowserLock(dir)
		require.ErrorContains(t, err, "already owned by a running server")
		require.Contains(t, err.Error(), lockFileName, "the failure must name the lock")
	})

	t.Run("stale lock is broken and retaken", func(t *testing.T) {
		dir := t.TempDir()
		dead := LockFile{PID: 4_000_000_000, StartedAt: time.Now().UTC()}
		require.NoError(t, os.MkdirAll(dir, 0o700))
		data, err := json.Marshal(dead)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, lockFileName), data, 0o600))

		release, err := AcquireBrowserLock(dir)
		require.NoError(t, err, "a lock naming a dead process must be broken and retaken")
		defer release()

		// The retaken lock must carry our own pid.
		data, err = os.ReadFile(filepath.Join(dir, lockFileName))
		require.NoError(t, err)
		var record LockFile
		require.NoError(t, json.Unmarshal(data, &record))
		require.Equal(t, os.Getpid(), record.PID)
	})

	t.Run("corrupt lock is treated as stale", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, lockFileName), []byte("garbage"), 0o600))

		_, err := AcquireBrowserLock(dir)
		require.Error(t, err, "an unreadable record cannot prove liveness; refusing is the safe answer")
		require.ErrorContains(t, err, "read existing lock")
	})

	t.Run("release removes the lock and is idempotent", func(t *testing.T) {
		dir := t.TempDir()
		release, err := AcquireBrowserLock(dir)
		require.NoError(t, err)

		require.NoError(t, release())
		_, err = os.Stat(filepath.Join(dir, lockFileName))
		require.True(t, os.IsNotExist(err), "release must remove the lock file")

		require.NoError(t, release(), "releasing twice is a no-op")

		// The freed directory accepts a new owner immediately.
		release2, err := AcquireBrowserLock(dir)
		require.NoError(t, err)
		defer release2()
	})
}

func TestPidAlive(t *testing.T) {
	require.True(t, pidAlive(os.Getpid()), "the test process is alive")
	require.False(t, pidAlive(-1))
	require.False(t, pidAlive(0))
}
