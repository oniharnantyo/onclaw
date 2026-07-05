package sqlite_test

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
)

func TestStagedWriteStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := sqlite.NewStagedWriteStore(db)
	ctx := context.Background()

	t.Run("StageWrite and ListStaged", func(t *testing.T) {
		// Stage a write
		id, err := store.StageWrite(ctx, "agent-1", "add", "", "Test memory content")
		if err != nil {
			t.Fatalf("failed to stage write: %v", err)
		}
		if id == 0 {
			t.Fatal("expected non-zero ID")
		}

		// List staged writes
		writes, err := store.ListStaged(ctx, "agent-1")
		if err != nil {
			t.Fatalf("failed to list staged writes: %v", err)
		}
		if len(writes) != 1 {
			t.Fatalf("expected 1 staged write, got %d", len(writes))
		}
		if writes[0].Operation != "add" {
			t.Errorf("expected operation 'add', got %q", writes[0].Operation)
		}
		if writes[0].Content != "Test memory content" {
			t.Errorf("expected content 'Test memory content', got %q", writes[0].Content)
		}
		if writes[0].Status != "pending" {
			t.Errorf("expected status 'pending', got %q", writes[0].Status)
		}
	})

	t.Run("ApproveWrite", func(t *testing.T) {
		// Stage a write
		id, err := store.StageWrite(ctx, "agent-2", "replace", "old text", "new text")
		if err != nil {
			t.Fatalf("failed to stage write: %v", err)
		}

		// Approve it
		err = store.ApproveWrite(ctx, id)
		if err != nil {
			t.Fatalf("failed to approve write: %v", err)
		}

		// List should now be empty (only pending writes are returned)
		writes, err := store.ListStaged(ctx, "agent-2")
		if err != nil {
			t.Fatalf("failed to list staged writes: %v", err)
		}
		if len(writes) != 0 {
			t.Errorf("expected 0 pending staged writes after approval, got %d", len(writes))
		}
	})

	t.Run("RejectWrite", func(t *testing.T) {
		// Stage a write
		id, err := store.StageWrite(ctx, "agent-3", "remove", "bad content", "")
		if err != nil {
			t.Fatalf("failed to stage write: %v", err)
		}

		// Reject it
		err = store.RejectWrite(ctx, id)
		if err != nil {
			t.Fatalf("failed to reject write: %v", err)
		}

		// List should now be empty
		writes, err := store.ListStaged(ctx, "agent-3")
		if err != nil {
			t.Fatalf("failed to list staged writes: %v", err)
		}
		if len(writes) != 0 {
			t.Errorf("expected 0 pending staged writes after rejection, got %d", len(writes))
		}
	})

	t.Run("ListStaged only returns pending writes", func(t *testing.T) {
		// Stage multiple writes
		id1, _ := store.StageWrite(ctx, "agent-4", "add", "", "content1")
		id2, _ := store.StageWrite(ctx, "agent-4", "add", "", "content2")
		id3, _ := store.StageWrite(ctx, "agent-4", "add", "", "content3")

		// Approve one, reject one
		store.ApproveWrite(ctx, id1)
		store.RejectWrite(ctx, id2)

		// Only id3 should be pending
		writes, err := store.ListStaged(ctx, "agent-4")
		if err != nil {
			t.Fatalf("failed to list staged writes: %v", err)
		}
		if len(writes) != 1 {
			t.Errorf("expected 1 pending staged write, got %d", len(writes))
		}
		if writes[0].ID != id3 {
			t.Errorf("expected ID %d, got %d", id3, writes[0].ID)
		}
	})

	t.Run("ListStaged filters by agent", func(t *testing.T) {
		// Stage writes for different agents
		_, _ = store.StageWrite(ctx, "agent-a", "add", "", "content-a")
		_, _ = store.StageWrite(ctx, "agent-b", "add", "", "content-b")

		// List for agent-a only
		writesA, err := store.ListStaged(ctx, "agent-a")
		if err != nil {
			t.Fatalf("failed to list staged writes for agent-a: %v", err)
		}
		if len(writesA) != 1 {
			t.Errorf("expected 1 staged write for agent-a, got %d", len(writesA))
		}
		if writesA[0].Agent != "agent-a" {
			t.Errorf("expected agent 'agent-a', got %q", writesA[0].Agent)
		}

		// List for agent-b only
		writesB, err := store.ListStaged(ctx, "agent-b")
		if err != nil {
			t.Fatalf("failed to list staged writes for agent-b: %v", err)
		}
		if len(writesB) != 1 {
			t.Errorf("expected 1 staged write for agent-b, got %d", len(writesB))
		}
	})
}

func TestStagedWriteStore_Integration(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := sqlite.NewStagedWriteStore(db)
	ctx := context.Background()

	t.Run("write_approval workflow", func(t *testing.T) {
		// Simulate agent trying to write with approval enabled
		writeID, err := store.StageWrite(ctx, "agent-workflow", "add", "", "Important fact about the project")
		if err != nil {
			t.Fatalf("failed to stage write: %v", err)
		}

		// Human reviews the staged write
		writes, err := store.ListStaged(ctx, "agent-workflow")
		if err != nil {
			t.Fatalf("failed to list for review: %v", err)
		}
		if len(writes) != 1 {
			t.Fatal("expected 1 write to review")
		}

		// Human approves
		err = store.ApproveWrite(ctx, writeID)
		if err != nil {
			t.Fatalf("failed to approve: %v", err)
		}

		// No more pending writes
		writes, err = store.ListStaged(ctx, "agent-workflow")
		if err != nil {
			t.Fatalf("failed to list after approval: %v", err)
		}
		if len(writes) != 0 {
			t.Error("expected no pending writes after approval")
		}
	})
}
