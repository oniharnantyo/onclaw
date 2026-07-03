package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/cli"
)

func TestMCPCommandLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-test-mcp-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("ONCLAW_DB_PATH", dbPath)

	app := cli.New()
	ctx := context.Background()

	// 1. Add stdio server
	if err := app.Run(ctx, []string{"onclaw", "mcp", "add", "fs-stdio", "--env", "API_KEY=secret123", "--env", "DEBUG=true", "--", "node", "index.js"}); err != nil {
		t.Fatalf("failed to add stdio server: %v", err)
	}

	// 2. Add http server
	if err := app.Run(ctx, []string{"onclaw", "mcp", "add", "fs-http", "--url", "http://localhost:3000"}); err != nil {
		t.Fatalf("failed to add http server: %v", err)
	}

	// 3. Add sse server
	if err := app.Run(ctx, []string{"onclaw", "mcp", "add", "fs-sse", "--sse-url", "http://localhost:3000/sse", "--disable"}); err != nil {
		t.Fatalf("failed to add sse server: %v", err)
	}

	// 4. List servers and verify redaction
	listOut, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "mcp", "list"})
	})
	if err != nil {
		t.Fatalf("failed to list servers: %v", err)
	}

	if !strings.Contains(listOut, "fs-stdio") || !strings.Contains(listOut, "fs-http") || !strings.Contains(listOut, "fs-sse") {
		t.Errorf("expected list to contain all servers, got:\n%s", listOut)
	}

	if !strings.Contains(listOut, "API_KEY=***") || !strings.Contains(listOut, "DEBUG=***") {
		t.Errorf("expected env values to be redacted with ***, got:\n%s", listOut)
	}

	if !strings.Contains(listOut, "disabled") {
		t.Errorf("expected fs-sse to be disabled, got:\n%s", listOut)
	}

	// 5. Test connection failure for invalid command
	testErr := app.Run(ctx, []string{"onclaw", "mcp", "test", "fs-stdio"})
	if testErr == nil {
		t.Error("expected test to fail due to invalid command running, got nil")
	}

	// 6. Remove server
	if err := app.Run(ctx, []string{"onclaw", "mcp", "remove", "fs-http"}); err != nil {
		t.Fatalf("failed to remove server: %v", err)
	}

	// 7. Verify removed
	listOutAfter, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "mcp", "list"})
	})
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}

	if strings.Contains(listOutAfter, "fs-http") {
		t.Errorf("expected fs-http to be removed, but it was found in:\n%s", listOutAfter)
	}
}
