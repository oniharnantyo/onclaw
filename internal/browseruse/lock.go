package browseruse

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// lockFileName is the single-owner lockfile (design D4) guarding the browser
// directory: created with O_CREATE|O_EXCL, holding pid + timestamp, guarding
// against desktop and server-mode double runs sharing one browser dir.
const lockFileName = "browser-use.lock"

// lockRetakeAttempts bounds the stale-retake retry loop: a concurrent
// starter could claim the broken lock between our remove and re-create.
const lockRetakeAttempts = 5

// LockFile is the persisted lock record.
type LockFile struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
}

// AcquireBrowserLock takes the exclusive single-owner lock on browserDir,
// creating the directory if needed. A lock left by a dead process is broken
// and retaken; a lock held by a live process is refused with a clear reason
// (it surfaces as the server's connection failure). The returned release
// removes the lock; releasing twice is a safe no-op.
func AcquireBrowserLock(browserDir string) (func() error, error) {
	if err := os.MkdirAll(browserDir, 0o700); err != nil {
		return nil, fmt.Errorf("browser: create browser dir: %w", err)
	}
	path := filepath.Join(browserDir, lockFileName)
	record := LockFile{PID: os.Getpid(), StartedAt: time.Now().UTC()}
	data, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("browser: encode lock: %w", err)
	}
	for attempt := 0; ; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, writeErr := f.Write(data)
			closeErr := f.Close()
			if writeErr != nil || closeErr != nil {
				_ = os.Remove(path) // a half-written lock must not wedge the dir
				if writeErr != nil {
					return nil, fmt.Errorf("browser: write lock: %w", writeErr)
				}
				return nil, fmt.Errorf("browser: close lock: %w", closeErr)
			}
			var released bool
			return func() error {
				if released {
					return nil
				}
				released = true
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("browser: release lock: %w", err)
				}
				return nil
			}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("browser: create lock: %w", err)
		}
		// The lock exists: a live owner wins, a dead owner's lock is broken.
		holder, readErr := readLockFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("browser: read existing lock %s: %w", path, readErr)
		}
		if pidAlive(holder.PID) {
			return nil, fmt.Errorf(
				"browser: browser directory %s is already owned by a running server (pid %d, lock %s); stop that server or choose another browser directory",
				browserDir, holder.PID, path)
		}
		if attempt >= lockRetakeAttempts {
			return nil, fmt.Errorf("browser: stale lock %s could not be retaken after %d attempts", path, lockRetakeAttempts)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("browser: break stale lock: %w", err)
		}
	}
}

// readLockFile parses an existing lock record.
func readLockFile(path string) (LockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LockFile{}, err
	}
	var record LockFile
	if err := json.Unmarshal(data, &record); err != nil {
		return LockFile{}, fmt.Errorf("decode: %w", err)
	}
	return record, nil
}
