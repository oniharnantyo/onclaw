package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
)

func TestEpisodicStore_AppendAndListUnpromoted(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := sqlite.NewEpisodicStore(db)
	ctx := context.Background()

	expiresAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)

	id, err := store.AppendEpisodic(ctx, "test-agent", "session summary", "l0 abstract", "topic1, topic2", "source_1", expiresAt)
	if err != nil {
		t.Fatalf("AppendEpisodic failed: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	episodes, err := store.ListUnpromoted(ctx, "test-agent")
	if err != nil {
		t.Fatalf("ListUnpromoted failed: %v", err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected 1 unpromoted episode, got %d", len(episodes))
	}
	if episodes[0].Summary != "session summary" {
		t.Errorf("expected summary %q, got %q", "session summary", episodes[0].Summary)
	}
	if episodes[0].L0Abstract != "l0 abstract" {
		t.Errorf("expected l0_abstract %q, got %q", "l0 abstract", episodes[0].L0Abstract)
	}
	if episodes[0].SourceID != "source_1" {
		t.Errorf("expected source_id %q, got %q", "source_1", episodes[0].SourceID)
	}
}

func TestEpisodicStore_MarkPromoted(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := sqlite.NewEpisodicStore(db)
	ctx := context.Background()

	expiresAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)

	id, err := store.AppendEpisodic(ctx, "test-agent", "summary", "l0", "topics", "src_1", expiresAt)
	if err != nil {
		t.Fatalf("AppendEpisodic failed: %v", err)
	}

	if err := store.MarkPromoted(ctx, id); err != nil {
		t.Fatalf("MarkPromoted failed: %v", err)
	}

	episodes, err := store.ListUnpromoted(ctx, "test-agent")
	if err != nil {
		t.Fatalf("ListUnpromoted failed: %v", err)
	}
	if len(episodes) != 0 {
		t.Errorf("expected 0 unpromoted after marking promoted, got %d", len(episodes))
	}

	got, err := store.GetEpisodic(ctx, id)
	if err != nil {
		t.Fatalf("GetEpisodic failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil episode after promote")
	}
	if got.PromotedAt == nil {
		t.Error("expected promoted_at to be set")
	}
}

func TestEpisodicStore_CountUnpromoted(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := sqlite.NewEpisodicStore(db)
	ctx := context.Background()

	expiresAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)

	count, err := store.CountUnpromoted(ctx, "test-agent")
	if err != nil {
		t.Fatalf("CountUnpromoted failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 unpromoted initially, got %d", count)
	}

	for i := 0; i < 3; i++ {
		src := "src_" + string(rune('a'+i))
		_, err := store.AppendEpisodic(ctx, "test-agent", "s", "l0", "t", src, expiresAt)
		if err != nil {
			t.Fatalf("AppendEpisodic %d failed: %v", i, err)
		}
	}

	count, err = store.CountUnpromoted(ctx, "test-agent")
	if err != nil {
		t.Fatalf("CountUnpromoted failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 unpromoted, got %d", count)
	}
}

func TestEpisodicStore_PruneExpired(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := sqlite.NewEpisodicStore(db)
	ctx := context.Background()

	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	future := time.Now().Add(24 * time.Hour).Format(time.RFC3339)

	_, err := store.AppendEpisodic(ctx, "test-agent", "expired", "l0", "t", "src_exp", past)
	if err != nil {
		t.Fatalf("AppendEpisodic (expired) failed: %v", err)
	}
	_, err = store.AppendEpisodic(ctx, "test-agent", "valid", "l0", "t", "src_val", future)
	if err != nil {
		t.Fatalf("AppendEpisodic (valid) failed: %v", err)
	}

	n, err := store.PruneExpired(ctx)
	if err != nil {
		t.Fatalf("PruneExpired failed: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 pruned row, got %d", n)
	}

	episodes, err := store.ListUnpromoted(ctx, "test-agent")
	if err != nil {
		t.Fatalf("ListUnpromoted failed: %v", err)
	}
	if len(episodes) != 1 {
		t.Errorf("expected 1 remaining episode, got %d", len(episodes))
	}
	if episodes[0].SourceID != "src_val" {
		t.Errorf("expected remaining episode source_id %q, got %q", "src_val", episodes[0].SourceID)
	}
}

func TestEpisodicStore_GetEpisodic_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := sqlite.NewEpisodicStore(db)
	ctx := context.Background()

	got, err := store.GetEpisodic(ctx, 999)
	if err != nil {
		t.Fatalf("GetEpisodic failed for non-existent: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for non-existent episode, got %+v", got)
	}
}

func TestEpisodicStore_SourceIDDedup(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := sqlite.NewEpisodicStore(db)
	ctx := context.Background()

	expiresAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)

	id1, err := store.AppendEpisodic(ctx, "test-agent", "original summary", "l0", "t", "dedup_src", expiresAt)
	if err != nil {
		t.Fatalf("first AppendEpisodic failed: %v", err)
	}
	if id1 <= 0 {
		t.Fatalf("expected positive id, got %d", id1)
	}

	id2, err := store.AppendEpisodic(ctx, "test-agent", "duplicate summary", "l0", "t", "dedup_src", expiresAt)
	if err != nil {
		t.Fatalf("second AppendEpisodic failed: %v", err)
	}
	if id2 != id1 {
		t.Errorf("expected duplicate to return existing id %d, got %d", id1, id2)
	}

	episodes, err := store.ListUnpromoted(ctx, "test-agent")
	if err != nil {
		t.Fatalf("ListUnpromoted failed: %v", err)
	}
	if len(episodes) != 1 {
		t.Errorf("expected only 1 episode after dedup, got %d", len(episodes))
	}
	if episodes[0].Summary != "original summary" {
		t.Errorf("expected summary %q (first insert), got %q", "original summary", episodes[0].Summary)
	}
}

func TestEpisodicStore_SourceIDDedup_DifferentAgents(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := sqlite.NewEpisodicStore(db)
	ctx := context.Background()

	expiresAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)

	id1, err := store.AppendEpisodic(ctx, "agent-a", "summary a", "l0", "t", "same_src", expiresAt)
	if err != nil {
		t.Fatalf("AppendEpisodic agent-a failed: %v", err)
	}

	id2, err := store.AppendEpisodic(ctx, "agent-b", "summary b", "l0", "t", "same_src", expiresAt)
	if err != nil {
		t.Fatalf("AppendEpisodic agent-b failed: %v", err)
	}
	if id2 == id1 {
		t.Errorf("expected different IDs for different agents with same source_id")
	}

	episodes, err := store.ListUnpromoted(ctx, "agent-a")
	if err != nil {
		t.Fatalf("ListUnpromoted agent-a failed: %v", err)
	}
	if len(episodes) != 1 {
		t.Errorf("expected 1 episode for agent-a, got %d", len(episodes))
	}

	episodes, err = store.ListUnpromoted(ctx, "agent-b")
	if err != nil {
		t.Fatalf("ListUnpromoted agent-b failed: %v", err)
	}
	if len(episodes) != 1 {
		t.Errorf("expected 1 episode for agent-b, got %d", len(episodes))
	}
}

func TestEpisodicStore_EmptySourceID_NoDedup(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := sqlite.NewEpisodicStore(db)
	ctx := context.Background()

	expiresAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)

	id1, err := store.AppendEpisodic(ctx, "test-agent", "first", "l0", "t", "", expiresAt)
	if err != nil {
		t.Fatalf("first AppendEpisodic failed: %v", err)
	}

	id2, err := store.AppendEpisodic(ctx, "test-agent", "second", "l0", "t", "", expiresAt)
	if err != nil {
		t.Fatalf("second AppendEpisodic failed: %v", err)
	}
	if id2 == id1 {
		t.Errorf("expected different IDs for empty source_id entries (no dedup)")
	}

	episodes, err := store.ListUnpromoted(ctx, "test-agent")
	if err != nil {
		t.Fatalf("ListUnpromoted failed: %v", err)
	}
	if len(episodes) != 2 {
		t.Errorf("expected 2 episodes with empty source_id, got %d", len(episodes))
	}
}
