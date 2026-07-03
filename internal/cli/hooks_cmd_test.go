package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/cli"
)

func TestHooksCommandLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-test-hooks-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("ONCLAW_DB_PATH", dbPath)

	app := cli.New()
	ctx := context.Background()

	// Initialize DB
	if err := app.Run(ctx, []string{"onclaw", "provider", "list"}); err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	// 1. Add hook
	addOut, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{
			"onclaw", "hooks", "add",
			"--name", "my-test-hook",
			"--handler", "command",
			"--event", "pre_tool_use",
			"--command", "exit 0",
			"--priority", "10",
		})
	})
	if err != nil {
		t.Fatalf("failed to add hook: %v", err)
	}

	// Extract hook ID from stdout
	re := regexp.MustCompile(`ID: (hook-\d+)`)
	matches := re.FindStringSubmatch(addOut)
	if len(matches) < 2 {
		t.Fatalf("failed to parse hook ID from output: %s", addOut)
	}
	hookID := matches[1]

	// 2. List hooks
	listOut, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "hooks", "list"})
	})
	if err != nil {
		t.Fatalf("failed to list hooks: %v", err)
	}
	if !strings.Contains(listOut, "my-test-hook") || !strings.Contains(listOut, hookID) {
		t.Errorf("list output does not contain expected hook, got:\n%s", listOut)
	}

	// 3. Show hook
	showOut, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "hooks", "show", hookID})
	})
	if err != nil {
		t.Fatalf("failed to show hook: %v", err)
	}
	if !strings.Contains(showOut, "my-test-hook") || !strings.Contains(showOut, "exit 0") {
		t.Errorf("show output does not contain expected details, got:\n%s", showOut)
	}

	// 4. Toggle hook disable
	toggleOut, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "hooks", "toggle", hookID, "--disable"})
	})
	if err != nil {
		t.Fatalf("failed to toggle hook: %v", err)
	}
	if !strings.Contains(toggleOut, "disabled successfully") {
		t.Errorf("unexpected toggle output: %s", toggleOut)
	}

	// Verify disabled in list
	listOut2, _ := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "hooks", "list"})
	})
	if !strings.Contains(listOut2, "no") {
		t.Errorf("expected hook to show disabled (no), got:\n%s", listOut2)
	}

	// 5. Test dry run
	testOut, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{
			"onclaw", "hooks", "test",
			"--handler", "command",
			"--config", `{"command":"exit 0"}`,
			"--event", "pre_tool_use",
		})
	})
	if err != nil {
		t.Fatalf("failed to test hook: %v", err)
	}
	if !strings.Contains(testOut, "allow") {
		t.Errorf("expected test output to be allow, got:\n%s", testOut)
	}

	// 6. Remove hook
	removeOut, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "hooks", "remove", hookID})
	})
	if err != nil {
		t.Fatalf("failed to remove hook: %v", err)
	}
	if !strings.Contains(removeOut, "removed successfully") {
		t.Errorf("unexpected remove output: %s", removeOut)
	}

	// Verify empty list
	listOut3, _ := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "hooks", "list"})
	})
	if !strings.Contains(listOut3, "No hooks configured") {
		t.Errorf("expected no hooks configured, got:\n%s", listOut3)
	}
}
