package cli_test

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/cli"
)

func TestPidFileAndSighup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-test-pid-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	// 1. Test writePIDFile
	pidPath, err := cli.WritePIDFile(dbPath)
	if err != nil {
		t.Fatalf("failed to write pid file: %v", err)
	}

	// Verify pid file exists and contains current PID
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("failed to read pid file: %v", err)
	}
	var writtenPid int
	if _, err := fmt.Sscanf(string(data), "%d", &writtenPid); err != nil {
		t.Fatalf("failed to parse pid: %v", err)
	}
	if writtenPid != os.Getpid() {
		t.Errorf("expected pid %d, got %d", os.Getpid(), writtenPid)
	}

	// 2. Test signalRunningProcess sending SIGHUP
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP)
	defer signal.Stop(sigChan)

	// Call signalRunningProcess
	if err := cli.SignalRunningProcess(dbPath); err != nil {
		t.Fatalf("failed to signal running process: %v", err)
	}

	// Verify signal is received
	select {
	case sig := <-sigChan:
		if sig != syscall.SIGHUP {
			t.Errorf("expected SIGHUP, got %v", sig)
		}
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for SIGHUP signal")
	}

	// Clean up and verify no signal is sent when pidfile is removed
	if err := os.Remove(pidPath); err != nil {
		t.Fatalf("failed to remove pidfile: %v", err)
	}

	// Calling it now should do nothing and return no error
	if err := cli.SignalRunningProcess(dbPath); err != nil {
		t.Errorf("expected no error when pidfile is missing, got %v", err)
	}
}
