package conversation_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/cloudwego/eino/schema/gemini"
	"github.com/cloudwego/eino/schema/openai"
	"github.com/oniharnantyo/onclaw/internal/conversation"
	"github.com/oniharnantyo/onclaw/internal/store"
)

type mockConversationStore struct {
	conversations    map[int64]*store.Conversation
	turns            map[int64][]*store.TurnRow
	nextConvID       int64
	nextSeq          map[int64]int64
	summaryMessageID int64
	loadErr          error
	appendErr        error
}

func newMockStore() *mockConversationStore {
	return &mockConversationStore{
		conversations: make(map[int64]*store.Conversation),
		turns:         make(map[int64][]*store.TurnRow),
		nextSeq:       make(map[int64]int64),
		nextConvID:    1,
	}
}

func (m *mockConversationStore) CreateConversation(ctx context.Context, agentName string) (int64, error) {
	id := m.nextConvID
	m.nextConvID++
	m.conversations[id] = &store.Conversation{ID: id, AgentName: agentName}
	m.nextSeq[id] = 1
	return id, nil
}

func (m *mockConversationStore) AppendTurn(
	ctx context.Context,
	convID int64,
	msgArrayJSON string,
	responseID string,
	previousResponseID string,
	model string,
	prompt int64,
	completion int64,
	total int64,
	question string,
	answer string,
) (int64, error) {
	if m.appendErr != nil {
		return 0, m.appendErr
	}
	seq := m.nextSeq[convID]
	if seq == 0 {
		seq = 1
	}
	m.nextSeq[convID] = seq + 1

	row := &store.TurnRow{
		ID:                 int64(len(m.turns[convID]) + 1),
		ConversationID:     convID,
		SequenceNum:        seq,
		ResponseID:         responseID,
		PreviousResponseID: previousResponseID,
		Message:            msgArrayJSON,
		Model:              model,
		PromptTokens:       prompt,
		CompletionTokens:   completion,
		TotalTokens:        total,
		Question:           question,
		Answer:             answer,
	}
	m.turns[convID] = append(m.turns[convID], row)
	return seq, nil
}

func (m *mockConversationStore) LoadHistory(ctx context.Context, convID int64) (*store.TurnRow, []*store.TurnRow, error) {
	if m.loadErr != nil {
		return nil, nil, m.loadErr
	}
	var summary *store.TurnRow
	if m.summaryMessageID != 0 {
		for _, turn := range m.turns[convID] {
			if turn.ID == m.summaryMessageID {
				summary = turn
				break
			}
		}
	}
	var tail []*store.TurnRow
	for _, turn := range m.turns[convID] {
		if summary != nil && turn.ID <= summary.ID {
			continue
		}
		tail = append(tail, turn)
	}
	return summary, tail, nil
}

func (m *mockConversationStore) ListTurns(ctx context.Context, conversationID int64) ([]*store.TurnRow, error) {
	return m.turns[conversationID], nil
}

func (m *mockConversationStore) SaveSummary(ctx context.Context, conversationID int64, summaryMessageJSON string, coveredUntilSeq int64) error {
	return nil
}

func (m *mockConversationStore) GetCompactionMeta(ctx context.Context, conversationID int64) (int, string, error) {
	return 0, "", nil
}

func (m *mockConversationStore) Transcript(ctx context.Context, conversationID int64, upToSeq int64) (string, error) {
	return "", nil
}

func (m *mockConversationStore) ListConversations(ctx context.Context) ([]*store.ConversationRow, error) {
	return nil, nil
}

func TestLoadHistory_Empty(t *testing.T) {
	st := newMockStore()
	convID, _ := st.CreateConversation(context.Background(), "master")
	sm := conversation.NewSessionManager(st, convID, "gpt-4o")

	msgs, prevID, err := sm.LoadHistory(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected empty messages, got %d", len(msgs))
	}
	if prevID != "" {
		t.Errorf("expected empty prevID, got %q", prevID)
	}
}

func TestLoadHistory_TailAndSummary(t *testing.T) {
	st := newMockStore()
	convID, _ := st.CreateConversation(context.Background(), "master")

	// Append summary turn
	sumMsg := []*schema.AgenticMessage{
		{
			Role: schema.AgenticRoleTypeUser,
			ContentBlocks: []*schema.ContentBlock{
				{
					Type:          schema.ContentBlockTypeUserInputText,
					UserInputText: &schema.UserInputText{Text: "Summary of past conversation"},
				},
			},
		},
	}
	sumJSON, _ := json.Marshal(sumMsg)
	st.AppendTurn(context.Background(), convID, string(sumJSON), "resp-summary", "", "gpt-4o", 10, 5, 15, "Q0", "A0")
	st.summaryMessageID = 1

	// Append tail turn with reasoning block
	tailMsg := []*schema.AgenticMessage{
		{
			Role: schema.AgenticRoleTypeUser,
			ContentBlocks: []*schema.ContentBlock{
				{
					Type:          schema.ContentBlockTypeUserInputText,
					UserInputText: &schema.UserInputText{Text: "Hello"},
				},
			},
		},
		{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				{
					Type: schema.ContentBlockTypeReasoning,
				},
				{
					Type:             schema.ContentBlockTypeAssistantGenText,
					AssistantGenText: &schema.AssistantGenText{Text: "Hi there!"},
				},
			},
		},
	}
	tailJSON, _ := json.Marshal(tailMsg)
	st.AppendTurn(context.Background(), convID, string(tailJSON), "resp-tail", "resp-summary", "gpt-4o", 20, 10, 30, "Hello", "Hi there!")

	sm := conversation.NewSessionManager(st, convID, "gpt-4o")
	msgs, prevID, err := sm.LoadHistory(context.Background())
	if err != nil {
		t.Fatalf("LoadHistory failed: %v", err)
	}

	if prevID != "resp-tail" {
		t.Errorf("expected prevID resp-tail, got %q", prevID)
	}

	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// Verify reasoning was stripped on load
	asstMsg := msgs[2]
	if len(asstMsg.ContentBlocks) != 1 || asstMsg.ContentBlocks[0].Type == schema.ContentBlockTypeReasoning {
		t.Errorf("expected reasoning block stripped from loaded message")
	}

	// Verify persistence extra marks
	for _, m := range msgs {
		if !conversation.IsPersisted(m) {
			t.Errorf("expected IsPersisted(m) to be true")
		}
	}
}

func TestLoadHistory_ClientPrevIDOverride(t *testing.T) {
	st := newMockStore()
	convID, _ := st.CreateConversation(context.Background(), "master")

	sm := conversation.NewSessionManager(st, convID, "gpt-4o")
	ctx := conversation.WithPreviousResponseID(context.Background(), "client-override-id")

	_, prevID, err := sm.LoadHistory(ctx)
	if err != nil {
		t.Fatalf("LoadHistory failed: %v", err)
	}
	if prevID != "client-override-id" {
		t.Errorf("expected client-override-id, got %q", prevID)
	}
}

func TestCommitTurn_BasicAndRedactionAndReasoning(t *testing.T) {
	st := newMockStore()
	convID, _ := st.CreateConversation(context.Background(), "master")
	sm := conversation.NewSessionManager(st, convID, "gpt-4o")

	turnMsgs := []*schema.AgenticMessage{
		{
			Role: schema.AgenticRoleTypeUser,
			ContentBlocks: []*schema.ContentBlock{
				{
					Type:          schema.ContentBlockTypeUserInputText,
					UserInputText: &schema.UserInputText{Text: "My key is sk-1234567890abcdef1234567890abcdef"},
				},
			},
		},
		{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				{
					Type: schema.ContentBlockTypeReasoning,
					Reasoning: &schema.Reasoning{
						Text: "thinking about the key",
					},
				},
				{
					Type:             schema.ContentBlockTypeAssistantGenText,
					AssistantGenText: &schema.AssistantGenText{Text: "Got it"},
				},
			},
			ResponseMeta: &schema.AgenticResponseMeta{
				TokenUsage: &schema.TokenUsage{
					PromptTokens:     15,
					CompletionTokens: 5,
					TotalTokens:      20,
				},
				OpenAIExtension: &openai.ResponseMetaExtension{
					ID: "resp-openai-1",
				},
			},
		},
	}

	meta, err := sm.CommitTurn(context.Background(), turnMsgs)
	if err != nil {
		t.Fatalf("CommitTurn failed: %v", err)
	}

	if meta == nil {
		t.Fatal("expected non-nil TurnMeta")
	}
	if meta.ResponseID != "" {
		t.Errorf("expected ResponseID empty (OpenAI extension dropped), got %q", meta.ResponseID)
	}
	if meta.Tokens != 20 || meta.PromptTokens != 15 || meta.CompletionTokens != 5 {
		t.Errorf("unexpected token counts in meta: %+v", meta)
	}

	// Verify stored turn in mock store
	if len(st.turns[convID]) != 1 {
		t.Fatalf("expected 1 turn in store, got %d", len(st.turns[convID]))
	}
	turnRow := st.turns[convID][0]
	if turnRow.Question != "My key is sk-1234567890abcdef1234567890abcdef" {
		t.Errorf("unexpected question: %q", turnRow.Question)
	}
	if turnRow.Answer != "Got it" {
		t.Errorf("unexpected answer: %q", turnRow.Answer)
	}

	// Verify persisted JSON has secret redacted but reasoning PRESERVED (persisted as-is)
	if string(turnRow.Message) == "" {
		t.Fatal("expected non-empty message JSON")
	}
	if containsString(turnRow.Message, "sk-1234567890") {
		t.Errorf("persisted JSON still contains unredacted secret: %s", turnRow.Message)
	}
	if !containsString(turnRow.Message, "reasoning") || !containsString(turnRow.Message, "thinking about the key") {
		t.Errorf("expected reasoning block preserved in persisted JSON: %s", turnRow.Message)
	}
}

func TestCommitTurn_ResponseIDFallbackChain(t *testing.T) {
	st := newMockStore()
	convID, _ := st.CreateConversation(context.Background(), "master")

	// Case 1: Gemini extension dropped -> empty
	sm1 := conversation.NewSessionManager(st, convID, "gemini-flash")
	msgs1 := []*schema.AgenticMessage{
		{
			Role: schema.AgenticRoleTypeAssistant,
			ResponseMeta: &schema.AgenticResponseMeta{
				GeminiExtension: &gemini.ResponseMetaExtension{ID: "resp-gemini-1"},
			},
		},
	}
	meta1, _ := sm1.CommitTurn(context.Background(), msgs1)
	if meta1.ResponseID != "" {
		t.Errorf("expected empty responseID, got %q", meta1.ResponseID)
	}

	// Case 2: Eino UUID fallback
	sm2 := conversation.NewSessionManager(st, convID, "custom-model")
	validUUID := "123e4567-e89b-12d3-a456-426614174000"
	msgs2 := []*schema.AgenticMessage{
		{
			Role: schema.AgenticRoleTypeAssistant,
			Extra: map[string]interface{}{
				"_eino_msg_id": validUUID,
			},
		},
	}
	meta2, _ := sm2.CommitTurn(context.Background(), msgs2)
	if meta2.ResponseID != validUUID {
		t.Errorf("expected valid UUID %q, got %q", validUUID, meta2.ResponseID)
	}

	// Case 3: Invalid Eino UUID fallback -> empty
	sm3 := conversation.NewSessionManager(st, convID, "custom-model")
	msgs3 := []*schema.AgenticMessage{
		{
			Role: schema.AgenticRoleTypeAssistant,
			Extra: map[string]interface{}{
				"_eino_msg_id": "not-a-uuid",
			},
		},
	}
	meta3, _ := sm3.CommitTurn(context.Background(), msgs3)
	if meta3.ResponseID != "" {
		t.Errorf("expected empty responseID for invalid UUID, got %q", meta3.ResponseID)
	}
}

func TestCommitTurn_CanceledContextSupport(t *testing.T) {
	st := newMockStore()
	convID, _ := st.CreateConversation(context.Background(), "master")
	sm := conversation.NewSessionManager(st, convID, "gpt-4o")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel context immediately

	msgs := []*schema.AgenticMessage{
		{
			Role: schema.AgenticRoleTypeUser,
			ContentBlocks: []*schema.ContentBlock{
				{
					Type:          schema.ContentBlockTypeUserInputText,
					UserInputText: &schema.UserInputText{Text: "Test canceled context"},
				},
			},
		},
	}

	meta, err := sm.CommitTurn(ctx, msgs)
	if err != nil {
		t.Fatalf("CommitTurn failed under canceled context: %v", err)
	}
	if meta == nil {
		t.Fatal("expected non-nil meta")
	}
}

func TestCommitTurn_Empty(t *testing.T) {
	st := newMockStore()
	sm := conversation.NewSessionManager(st, 1, "gpt-4o")

	meta, err := sm.CommitTurn(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta != nil {
		t.Errorf("expected nil meta for empty messages, got %v", meta)
	}
}

func TestSessionManager_SetPreviousResponseIDAndLastTurnMeta(t *testing.T) {
	st := newMockStore()
	sm := conversation.NewSessionManager(st, 1, "gpt-4o")

	if sm.LastTurnMeta() != nil {
		t.Errorf("expected nil LastTurnMeta initially")
	}

	sm.SetPreviousResponseID("custom-prev-id")

	msgs := []*schema.AgenticMessage{
		{
			Role: schema.AgenticRoleTypeUser,
			ContentBlocks: []*schema.ContentBlock{
				{
					Type:          schema.ContentBlockTypeUserInputText,
					UserInputText: &schema.UserInputText{Text: "Hi"},
				},
			},
		},
	}
	meta, err := sm.CommitTurn(context.Background(), msgs)
	if err != nil {
		t.Fatalf("CommitTurn failed: %v", err)
	}
	if meta.PreviousResponseID != "custom-prev-id" {
		t.Errorf("expected PreviousResponseID custom-prev-id, got %q", meta.PreviousResponseID)
	}
	if sm.LastTurnMeta() != meta {
		t.Errorf("expected LastTurnMeta to return committed meta")
	}
}

func TestSanitizeSummaryMessage(t *testing.T) {
	msg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{
				Type:             schema.ContentBlockTypeAssistantGenText,
				AssistantGenText: &schema.AssistantGenText{Text: "Legacy summary"},
			},
			{
				Type: schema.ContentBlockTypeReasoning,
			},
		},
	}
	conversation.SanitizeSummaryMessage(msg)

	if msg.Role != schema.AgenticRoleTypeUser {
		t.Errorf("expected user role, got %v", msg.Role)
	}
	if len(msg.ContentBlocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(msg.ContentBlocks))
	}
	if msg.ContentBlocks[0].Type != schema.ContentBlockTypeUserInputText || msg.ContentBlocks[0].UserInputText.Text != "Legacy summary" {
		t.Errorf("unexpected content block: %+v", msg.ContentBlocks[0])
	}
}

func TestCommitTurn_FallsBackToEstimateWithoutUsage(t *testing.T) {
	st := newMockStore()
	convID, _ := st.CreateConversation(context.Background(), "master")
	sm := conversation.NewSessionManager(st, convID, "gpt-4o")

	// 44 chars user prompt -> 11 tokens; 4 chars assistant -> 1 token
	turnMsgs := []*schema.AgenticMessage{
		{
			Role: schema.AgenticRoleTypeUser,
			ContentBlocks: []*schema.ContentBlock{
				{
					Type:          schema.ContentBlockTypeUserInputText,
					UserInputText: &schema.UserInputText{Text: "My key is sk-1234567890abcdef1234567890abcdef"},
				},
			},
		},
		{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				{
					Type:             schema.ContentBlockTypeAssistantGenText,
					AssistantGenText: &schema.AssistantGenText{Text: "Got it"},
				},
			},
			// ResponseMeta present but TokenUsage is nil
			ResponseMeta: &schema.AgenticResponseMeta{},
		},
	}

	meta, err := sm.CommitTurn(context.Background(), turnMsgs)
	if err != nil {
		t.Fatalf("CommitTurn failed: %v", err)
	}

	if meta.Tokens != 12 || meta.PromptTokens != 11 || meta.CompletionTokens != 1 {
		t.Errorf("unexpected fallback token counts in meta: %+v", meta)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}()
}
