package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/oniharnantyo/onclaw/internal/cli"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
)

func setupTestDBWithStagedWrite(t *testing.T) (string, int64, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	db.Close()
	_ = os.Chmod(dbPath, 0600)

	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	stagedStore := sqlite.NewStagedWriteStore(db)
	writeID, err := stagedStore.StageWrite(context.Background(), "master", "add", "", "learned new fact")
	if err != nil {
		t.Fatalf("stage write: %v", err)
	}
	db.Close()

	t.Setenv("ONCLAW_DB_PATH", dbPath)

	return dbPath, writeID, func() {
		os.RemoveAll(tmpDir)
	}
}

func TestMemoryPendingCommand(t *testing.T) {
	_, _, cleanup := setupTestDBWithStagedWrite(t)
	defer cleanup()

	app := cli.New()
	ctx := context.Background()

	buf := &bytes.Buffer{}
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := app.Run(ctx, []string{"onclaw", "memory", "pending"})
	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("memory pending failed: %v", err)
	}

	_, _ = io.Copy(buf, r)
	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("learned new fact")) {
		t.Errorf("expected pending write output to contain 'learned new fact', got:\n%s", out)
	}
}

func TestMemoryApproveCommand(t *testing.T) {
	_, writeID, cleanup := setupTestDBWithStagedWrite(t)
	defer cleanup()

	app := cli.New()
	ctx := context.Background()

	tmpWorkspace := t.TempDir()

	idStr := fmt.Sprintf("%d", writeID)
	err := app.Run(ctx, []string{"onclaw", "memory", "approve", "--workspace", tmpWorkspace, idStr})
	if err != nil {
		t.Fatalf("memory approve failed: %v", err)
	}

	// Verify MEMORY.md was created in workspace
	memPath := filepath.Join(tmpWorkspace, "MEMORY.md")
	content, err := os.ReadFile(memPath)
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	if !bytes.Contains(content, []byte("learned new fact")) {
		t.Errorf("expected MEMORY.md to contain 'learned new fact', got %q", string(content))
	}
}

func TestMemoryRejectCommand(t *testing.T) {
	_, writeID, cleanup := setupTestDBWithStagedWrite(t)
	defer cleanup()

	app := cli.New()
	ctx := context.Background()

	idStr := fmt.Sprintf("%d", writeID)
	err := app.Run(ctx, []string{"onclaw", "memory", "reject", idStr})
	if err != nil {
		t.Fatalf("memory reject failed: %v", err)
	}
}
