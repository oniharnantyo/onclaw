package sqlite

import (
	"context"
	"testing"
)

func TestConversationStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewConversationStore(db)
	ctx := context.Background()

	// Create conversation
	convID, err := store.CreateConversation(ctx, "test-agent")
	if err != nil {
		t.Fatalf("CreateConversation failed: %v", err)
	}

	// Append user message
	seq1, err := store.AppendMessage(ctx, convID, "user", `{"role":"user","content":"Hello"}`)
	if err != nil {
		t.Fatalf("AppendMessage failed: %v", err)
	}
	if seq1 != 1 {
		t.Errorf("expected seq 1, got %d", seq1)
	}

	// Append assistant message
	seq2, err := store.AppendMessage(ctx, convID, "assistant", `{"role":"assistant","content":"Hi there"}`)
	if err != nil {
		t.Fatalf("AppendMessage failed: %v", err)
	}
	if seq2 != 2 {
		t.Errorf("expected seq 2, got %d", seq2)
	}

	// Append tool message
	seq3, err := store.AppendMessage(ctx, convID, "tool", `{"role":"tool","content":"some tool output"}`)
	if err != nil {
		t.Fatalf("AppendMessage failed: %v", err)
	}
	if seq3 != 3 {
		t.Errorf("expected seq 3, got %d", seq3)
	}

	// Load history
	summary, tail, err := store.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory failed: %v", err)
	}
	if summary != nil {
		t.Errorf("expected nil summary initially")
	}
	if len(tail) != 3 {
		t.Errorf("expected 3 tail messages, got %d", len(tail))
	} else {
		if tail[0].Seq != 1 || tail[1].Seq != 2 || tail[2].Seq != 3 {
			t.Errorf("unexpected tail order or seq numbers: %v, %v, %v", tail[0].Seq, tail[1].Seq, tail[2].Seq)
		}
	}

	// List messages
	allMsgs, err := store.ListMessages(ctx, convID)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(allMsgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(allMsgs))
	}

	// Save summary (representing compaction of seq 1 and 2)
	err = store.SaveSummary(ctx, convID, `{"role":"assistant","content":"Summary of conversation"}`, 2)
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
	if summary.Message != `{"role":"assistant","content":"Summary of conversation"}` {
		t.Errorf("unexpected summary message: %s", summary.Message)
	}

	// The tail should only contain messages with seq > 2. So only seq3 (tool message) should be returned.
	if len(tail) != 1 {
		t.Errorf("expected 1 tail message (seq 3), got %d", len(tail))
		for _, m := range tail {
			t.Logf("Tail message: seq=%d ID=%d role=%s content=%s", m.Seq, m.ID, m.Role, m.Message)
		}
	} else if tail[0].Seq != 3 {
		t.Errorf("expected tail message to be seq 3, got %d", tail[0].Seq)
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
		// The total messages added: seq1, seq2, seq3, and the summary message added in SaveSummary makes it 4!
		if row.MessageCount != 4 {
			t.Errorf("expected 4 messages, got %d", row.MessageCount)
		}
	}
}
