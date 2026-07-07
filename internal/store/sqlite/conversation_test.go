package sqlite_test

import (
	"context"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
)

func TestConversationStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := sqlite.NewConversationStore(db)
	ctx := context.Background()

	// Create conversation
	convID, err := store.CreateConversation(ctx, "test-agent")
	if err != nil {
		t.Fatalf("CreateConversation failed: %v", err)
	}

	// Append turn 1
	seq1, err := store.AppendTurn(
		ctx,
		convID,
		`[{"role":"user","content_blocks":[{"type":"user_input_text","user_input_text":{"text":"Hello"}}]},{"role":"assistant","content_blocks":[{"type":"assistant_gen_text","assistant_gen_text":{"text":"Hi there"}}]}]`,
		"resp-1",
		"",
		"model-1",
		10, 20, 30,
		"Hello",
		"Hi there",
	)
	if err != nil {
		t.Fatalf("AppendTurn failed: %v", err)
	}
	if seq1 != 1 {
		t.Errorf("expected seq 1, got %d", seq1)
	}

	// Append turn 2 (chaining to turn 1)
	seq2, err := store.AppendTurn(
		ctx,
		convID,
		`[]`,
		"resp-2",
		"resp-1",
		"model-1",
		10, 20, 30,
		"How are you?",
		"I'm fine",
	)
	if err != nil {
		t.Fatalf("AppendTurn 2 failed: %v", err)
	}
	if seq2 != 2 {
		t.Errorf("expected seq 2, got %d", seq2)
	}

	// Load history
	summary, tail, err := store.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory failed: %v", err)
	}
	if summary != nil {
		t.Errorf("expected nil summary initially")
	}
	if len(tail) != 2 {
		t.Errorf("expected 2 tail turns, got %d", len(tail))
	} else {
		if tail[0].SequenceNum != 1 {
			t.Errorf("unexpected tail[0] seq number: %d", tail[0].SequenceNum)
		}
		if tail[0].Question != "Hello" || tail[0].Answer != "Hi there" {
			t.Errorf("unexpected question/answer: %q/%q", tail[0].Question, tail[0].Answer)
		}
		if tail[1].SequenceNum != 2 {
			t.Errorf("unexpected tail[1] seq number: %d", tail[1].SequenceNum)
		}
		if tail[1].PreviousResponseID != "resp-1" {
			t.Errorf("expected Turn 2's PreviousResponseID to be 'resp-1', got %q", tail[1].PreviousResponseID)
		}
		if tail[1].ResponseID != "resp-2" {
			t.Errorf("expected Turn 2's ResponseID to be 'resp-2', got %q", tail[1].ResponseID)
		}
	}

	// List turns
	allTurns, err := store.ListTurns(ctx, convID)
	if err != nil {
		t.Fatalf("ListTurns failed: %v", err)
	}
	if len(allTurns) != 2 {
		t.Errorf("expected 2 turns, got %d", len(allTurns))
	}

	// Save summary (representing compaction of seq 1)
	err = store.SaveSummary(ctx, convID, `{"role":"assistant","content_blocks":[{"type":"assistant_gen_text","assistant_gen_text":{"text":"Summary of conversation"}}]}`, 1)
	if err != nil {
		t.Fatalf("SaveSummary failed: %v", err)
	}

	// Load history again
	summary, tail, err = store.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory failed: %v", err)
	}
	if summary == nil {
		t.Fatalf("expected non-nil summary after SaveSummary")
	}
	if !strings.Contains(summary.Message, "Summary of conversation") {
		t.Errorf("unexpected summary turn message: %s", summary.Message)
	}

	// The tail should only contain turns with sequence_num > 1. So only turn 2 should remain.
	if len(tail) != 1 {
		t.Errorf("expected 1 tail turn, got %d", len(tail))
	} else if tail[0].SequenceNum != 2 {
		t.Errorf("expected remaining tail turn to be seq 2, got %d", tail[0].SequenceNum)
	}

	// Test ListConversations
	list, err := store.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 conversation, got %d", len(list))
	} else {
		row := list[0]
		if row.ID != convID {
			t.Errorf("expected ID %d, got %d", convID, row.ID)
		}
		if row.AgentName != "test-agent" {
			t.Errorf("expected agent name test-agent, got %s", row.AgentName)
		}
		// The total turn rows added: turn 1 + turn 2 + summary turn = 3 rows
		if row.MessageCount != 3 {
			t.Errorf("expected 3 turns, got %d", row.MessageCount)
		}
		if row.Preview != "Hello" {
			t.Errorf("expected Preview to be 'Hello', got %q", row.Preview)
		}
	}
}
