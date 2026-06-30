package middlewares

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
)

type fakeChatModel struct {
	generateFunc func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error)
	streamFunc   func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error)
}

func (f *fakeChatModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	if f.generateFunc != nil {
		return f.generateFunc(ctx, input, opts...)
	}
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "Default fake response"}),
		},
	}, nil
}

func (f *fakeChatModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	if f.streamFunc != nil {
		return f.streamFunc(ctx, input, opts...)
	}
	msg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "Default fake streaming response"}),
		},
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

type mockConversationStore struct {
	conversations    map[int64]*store.Conversation
	messages         map[int64][]*store.MessageRow
	nextConvID       int64
	nextSeq          map[int64]int64
	summaryMessageID int64
	summaryUntilSeq  int64
}

func newMockConversationStore() *mockConversationStore {
	return &mockConversationStore{
		conversations: make(map[int64]*store.Conversation),
		messages:      make(map[int64][]*store.MessageRow),
		nextSeq:       make(map[int64]int64),
		nextConvID:    1,
	}
}

func (m *mockConversationStore) CreateConversation(ctx context.Context, agentName string) (int64, error) {
	id := m.nextConvID
	m.nextConvID++
	m.conversations[id] = &store.Conversation{
		ID:        id,
		AgentName: agentName,
	}
	m.nextSeq[id] = 1
	return id, nil
}

func (m *mockConversationStore) AppendMessage(ctx context.Context, conversationID int64, role string, messageJSON string) (int64, error) {
	seq := m.nextSeq[conversationID]
	m.nextSeq[conversationID]++

	row := &store.MessageRow{
		ID:             int64(len(m.messages[conversationID]) + 1),
		ConversationID: conversationID,
		Seq:            seq,
		Role:           role,
		Message:        messageJSON,
	}
	m.messages[conversationID] = append(m.messages[conversationID], row)
	return seq, nil
}

func (m *mockConversationStore) LoadHistory(ctx context.Context, conversationID int64) (*store.MessageRow, []*store.MessageRow, error) {
	var summary *store.MessageRow
	if m.summaryMessageID != 0 {
		for _, msg := range m.messages[conversationID] {
			if msg.ID == m.summaryMessageID {
				summary = msg
				break
			}
		}
	}

	var tail []*store.MessageRow
	for _, msg := range m.messages[conversationID] {
		if msg.Seq > m.summaryUntilSeq && msg.ID != m.summaryMessageID {
			tail = append(tail, msg)
		}
	}

	return summary, tail, nil
}

func (m *mockConversationStore) ListMessages(ctx context.Context, conversationID int64) ([]*store.MessageRow, error) {
	return m.messages[conversationID], nil
}

func (m *mockConversationStore) SaveSummary(ctx context.Context, conversationID int64, summaryMessageJSON string, coveredUntilSeq int64) error {
	_, err := m.AppendMessage(ctx, conversationID, "assistant", summaryMessageJSON)
	if err != nil {
		return err
	}

	// The last appended message is our summary
	msgs := m.messages[conversationID]
	summaryRow := msgs[len(msgs)-1]

	m.summaryMessageID = summaryRow.ID
	m.summaryUntilSeq = coveredUntilSeq
	return nil
}

func (m *mockConversationStore) ListConversations(ctx context.Context) ([]*store.ConversationRow, error) {
	return nil, nil
}

func TestHistoryMiddleware(t *testing.T) {
	s := newMockConversationStore()
	ctx := context.Background()

	convID, err := s.CreateConversation(ctx, "test-agent")
	if err != nil {
		t.Fatalf("failed to create conversation: %v", err)
	}

	h := NewHistoryMiddleware(s, convID)

	// --- Turn 1 ---
	// BeforeAgent: input user message
	userMsg := schema.UserAgenticMessage("Hello agent")
	runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg},
		},
	}

	ctx, runCtx, err = h.BeforeAgent(ctx, runCtx)
	if err != nil {
		t.Fatalf("BeforeAgent failed: %v", err)
	}

	// Verify user message is marked as persisted and has seq
	if !IsPersisted(userMsg) {
		t.Errorf("user message should be marked as persisted")
	}
	seqVal, ok := userMsg.Extra["_onclaw_seq"].(int64)
	if !ok || seqVal != 1 {
		t.Errorf("expected user message seq to be 1, got %v", userMsg.Extra["_onclaw_seq"])
	}

	// Verify cursor is in context
	cursor, ok := ctx.Value(cursorKey).(*RunCursor)
	if !ok || cursor == nil {
		t.Fatalf("RunCursor missing from context")
	}
	if cursor.MaxSeq != 1 {
		t.Errorf("expected cursor MaxSeq to be 1, got %d", cursor.MaxSeq)
	}

	// Verify store has 1 message
	msgs, _ := s.ListMessages(ctx, convID)
	if len(msgs) != 1 {
		t.Errorf("expected 1 stored message, got %d", len(msgs))
	}

	// AfterModelRewriteState: model emits assistant message
	assistantMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "Hello user"}),
		},
	}
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{userMsg, assistantMsg},
	}

	ctx, state, err = h.AfterModelRewriteState(ctx, state, nil)
	if err != nil {
		t.Fatalf("AfterModelRewriteState failed: %v", err)
	}

	// Verify assistant message is now persisted
	if !IsPersisted(assistantMsg) {
		t.Errorf("assistant message should be marked as persisted")
	}
	if assistantMsg.Extra["_onclaw_seq"] != int64(2) {
		t.Errorf("expected assistant msg seq to be 2, got %v", assistantMsg.Extra["_onclaw_seq"])
	}
	if cursor.MaxSeq != 2 {
		t.Errorf("expected cursor MaxSeq to be 2, got %d", cursor.MaxSeq)
	}

	// AfterAgent: final check
	ctx, err = h.AfterAgent(ctx, state)
	if err != nil {
		t.Fatalf("AfterAgent failed: %v", err)
	}

	// Verify store now has 2 messages
	msgs, _ = s.ListMessages(ctx, convID)
	if len(msgs) != 2 {
		t.Errorf("expected 2 stored messages, got %d", len(msgs))
	}

	// --- Turn 2 ---
	// Create another turn to ensure history is loaded
	userMsg2 := schema.UserAgenticMessage("How are you?")
	runCtx2 := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg2},
		},
	}

	ctx2, runCtx2, err := h.BeforeAgent(context.Background(), runCtx2)
	if err != nil {
		t.Fatalf("BeforeAgent Turn 2 failed: %v", err)
	}

	injected := runCtx2.AgentInput.Messages
	if len(injected) != 3 {
		t.Errorf("expected 3 messages after injection, got %d", len(injected))
	} else {
		// Helper to extract content block text
		getText := func(msg *schema.AgenticMessage) string {
			if len(msg.ContentBlocks) > 0 && msg.ContentBlocks[0].AssistantGenText != nil {
				return msg.ContentBlocks[0].AssistantGenText.Text
			}
			if len(msg.ContentBlocks) > 0 && msg.ContentBlocks[0].UserInputText != nil {
				return msg.ContentBlocks[0].UserInputText.Text
			}
			return ""
		}
		c0 := getText(injected[0])
		c1 := getText(injected[1])
		c2 := getText(injected[2])
		if c0 != "Hello agent" || c1 != "Hello user" || c2 != "How are you?" {
			t.Errorf("unexpected message contents: %q, %q, %q", c0, c1, c2)
		}
	}

	// Verify userMsg2 is saved exactly once
	msgs, _ = s.ListMessages(ctx2, convID)
	if len(msgs) != 3 {
		t.Errorf("expected 3 stored messages, got %d", len(msgs))
	}

	// --- Compaction Test ---
	// Let's compact the history of convID. We save a summary representing message 1 & 2.
	err = s.SaveSummary(ctx, convID, `{"role":"assistant","content_blocks":[{"type":"assistant_gen_text","assistant_gen_text":{"text":"Summary"}}]}`, 2)
	if err != nil {
		t.Fatalf("SaveSummary failed: %v", err)
	}

	// Now run Turn 3
	userMsg3 := schema.UserAgenticMessage("Turn 3 prompt")
	runCtx3 := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg3},
		},
	}

	_, runCtx3, err = h.BeforeAgent(context.Background(), runCtx3)
	if err != nil {
		t.Fatalf("BeforeAgent Turn 3 failed: %v", err)
	}

	injected3 := runCtx3.AgentInput.Messages
	if len(injected3) != 3 {
		t.Errorf("expected 3 injected messages after compaction, got %d", len(injected3))
	} else {
		getText := func(msg *schema.AgenticMessage) string {
			if len(msg.ContentBlocks) > 0 && msg.ContentBlocks[0].AssistantGenText != nil {
				return msg.ContentBlocks[0].AssistantGenText.Text
			}
			if len(msg.ContentBlocks) > 0 && msg.ContentBlocks[0].UserInputText != nil {
				return msg.ContentBlocks[0].UserInputText.Text
			}
			return ""
		}
		c0 := getText(injected3[0])
		c1 := getText(injected3[1])
		c2 := getText(injected3[2])
		if c0 != "Summary" || c1 != "How are you?" || c2 != "Turn 3 prompt" {
			t.Errorf("unexpected compaction message contents: %q, %q, %q", c0, c1, c2)
		}
	}
}

func TestRedaction(t *testing.T) {
	s := newMockConversationStore()
	ctx := context.Background()
	convID, _ := s.CreateConversation(ctx, "test-agent")
	h := NewHistoryMiddleware(s, convID)

	// 1. Normal message without secrets
	msg := schema.UserAgenticMessage("Hello normal message")
	_, err := h.saveMessage(ctx, msg)
	if err != nil {
		t.Fatalf("saveMessage failed: %v", err)
	}

	// 2. Secret message containing key pattern
	secretMsg := schema.UserAgenticMessage("My secret key is sk-12345678901234567890")
	_, err = h.saveMessage(ctx, secretMsg)
	if err != nil {
		t.Fatalf("saveMessage failed: %v", err)
	}

	msgs, _ := s.ListMessages(ctx, convID)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	var savedNormal schema.AgenticMessage
	if err := json.Unmarshal([]byte(msgs[0].Message), &savedNormal); err != nil {
		t.Fatalf("failed to unmarshal saved normal message: %v", err)
	}
	getText := func(msg *schema.AgenticMessage) string {
		if len(msg.ContentBlocks) > 0 && msg.ContentBlocks[0].UserInputText != nil {
			return msg.ContentBlocks[0].UserInputText.Text
		}
		return ""
	}
	if getText(&savedNormal) != "Hello normal message" {
		t.Errorf("expected original content, got %q", getText(&savedNormal))
	}

	var savedSecret schema.AgenticMessage
	if err := json.Unmarshal([]byte(msgs[1].Message), &savedSecret); err != nil {
		t.Fatalf("failed to unmarshal saved secret message: %v", err)
	}
	expectedRedacted := "My secret key is [REDACTED]"
	if getText(&savedSecret) != expectedRedacted {
		t.Errorf("expected redacted content %q, got %q", expectedRedacted, getText(&savedSecret))
	}
}

func TestSummarizationExtraPreservation(t *testing.T) {
	ctx := context.Background()

	fm := &fakeChatModel{
		generateFunc: func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
			return &schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.AssistantGenText{Text: "This is a summary"}),
				},
			}, nil
		},
	}

	sm, err := summarization.NewTyped[*schema.AgenticMessage](ctx, &summarization.TypedConfig[*schema.AgenticMessage]{
		Model: fm,
		Trigger: &summarization.TriggerCondition{
			ContextTokens: 5,
		},
		TokenCounter: func(ctx context.Context, input *summarization.TypedTokenCounterInput[*schema.AgenticMessage]) (int, error) {
			return 10, nil // always trigger
		},
	})
	if err != nil {
		t.Fatalf("failed to create summarization: %v", err)
	}

	msg1 := schema.UserAgenticMessage("Msg 1")
	msg1.Extra = map[string]interface{}{persistedKey: true, "_onclaw_seq": int64(1)}

	msg2 := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "Msg 2"}),
		},
		Extra: map[string]interface{}{persistedKey: true, "_onclaw_seq": int64(2)},
	}

	msg3 := schema.UserAgenticMessage("Msg 3")
	msg3.Extra = map[string]interface{}{persistedKey: true, "_onclaw_seq": int64(3)}

	msg4 := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "Msg 4"}),
		},
		Extra: map[string]interface{}{persistedKey: true, "_onclaw_seq": int64(4)},
	}

	runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{msg1, msg2, msg3, msg4},
		},
	}

	_, outCtx, err := sm.BeforeAgent(ctx, runCtx)
	if err != nil {
		t.Fatalf("BeforeAgent failed: %v", err)
	}

	outMsgs := outCtx.AgentInput.Messages
	t.Logf("Compacted messages count: %d", len(outMsgs))

	foundMsg3 := false
	foundMsg4 := false
	for _, m := range outMsgs {
		var content string
		if len(m.ContentBlocks) > 0 {
			if m.ContentBlocks[0].UserInputText != nil {
				content = m.ContentBlocks[0].UserInputText.Text
			} else if m.ContentBlocks[0].AssistantGenText != nil {
				content = m.ContentBlocks[0].AssistantGenText.Text
			}
		}

		if content == "Msg 3" {
			foundMsg3 = true
			if !IsPersisted(m) {
				t.Errorf("Msg 3 lost its persisted flag!")
			}
			if seq, ok := m.Extra["_onclaw_seq"].(int64); !ok || seq != 3 {
				t.Errorf("Msg 3 lost or got wrong seq: %v", m.Extra["_onclaw_seq"])
			}
		}
		if content == "Msg 4" {
			foundMsg4 = true
			if !IsPersisted(m) {
				t.Errorf("Msg 4 lost its persisted flag!")
			}
			if seq, ok := m.Extra["_onclaw_seq"].(int64); !ok || seq != 4 {
				t.Errorf("Msg 4 lost or got wrong seq: %v", m.Extra["_onclaw_seq"])
			}
		}
	}
	if !foundMsg3 || !foundMsg4 {
		t.Errorf("retained messages Msg 3 or Msg 4 not found in output")
	}
}

func TestMessageFidelityRoundTrip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-fidelity-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "onclaw.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	defer db.Close()

	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("failed to migrate db: %v", err)
	}

	storeInst := sqlite.NewConversationStore(db)
	ctx := context.Background()

	convID, err := storeInst.CreateConversation(ctx, "test-agent")
	if err != nil {
		t.Fatalf("failed to create conversation: %v", err)
	}

	originalMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{
				Text: "Original content with tool calls",
			}),
			{
				Type: schema.ContentBlockTypeFunctionToolCall,
				FunctionToolCall: &schema.FunctionToolCall{
					CallID:    "tc_1",
					Name:      "read_file",
					Arguments: `{"path":"README.md"}`,
				},
			},
		},
		Extra: map[string]interface{}{
			"summarization_tag": "some-value",
			persistedKey:        true,
		},
	}

	msgJSON, err := json.Marshal(originalMsg)
	if err != nil {
		t.Fatalf("failed to marshal original message: %v", err)
	}

	_, err = storeInst.AppendMessage(ctx, convID, string(originalMsg.Role), string(msgJSON))
	if err != nil {
		t.Fatalf("AppendMessage failed: %v", err)
	}

	_, tail, err := storeInst.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory failed: %v", err)
	}

	if len(tail) != 1 {
		t.Fatalf("expected 1 tail message, got %d", len(tail))
	}

	var loadedMsg schema.AgenticMessage
	if err := json.Unmarshal([]byte(tail[0].Message), &loadedMsg); err != nil {
		t.Fatalf("failed to unmarshal loaded message: %v", err)
	}

	if loadedMsg.Role != originalMsg.Role {
		t.Errorf("role mismatch: expected %q, got %q", originalMsg.Role, loadedMsg.Role)
	}

	getText := func(msg *schema.AgenticMessage) string {
		if len(msg.ContentBlocks) > 0 && msg.ContentBlocks[0].AssistantGenText != nil {
			return msg.ContentBlocks[0].AssistantGenText.Text
		}
		return ""
	}
	if getText(&loadedMsg) != getText(originalMsg) {
		t.Errorf("content mismatch: expected %q, got %q", getText(originalMsg), getText(&loadedMsg))
	}

	if len(loadedMsg.ContentBlocks) != len(originalMsg.ContentBlocks) {
		t.Fatalf("content blocks length mismatch: expected %d, got %d", len(originalMsg.ContentBlocks), len(loadedMsg.ContentBlocks))
	}

	tcLoaded := loadedMsg.ContentBlocks[1].FunctionToolCall
	tcOriginal := originalMsg.ContentBlocks[1].FunctionToolCall
	if tcLoaded.CallID != tcOriginal.CallID || tcLoaded.Name != tcOriginal.Name || tcLoaded.Arguments != tcOriginal.Arguments {
		t.Errorf("tool call mismatch: expected %+v, got %+v", tcOriginal, tcLoaded)
	}

	if loadedMsg.Extra == nil {
		t.Fatalf("loaded message has nil Extra")
	}
	if loadedMsg.Extra["summarization_tag"] != "some-value" {
		t.Errorf("summarization_tag extra mismatch: expected 'some-value', got %v", loadedMsg.Extra["summarization_tag"])
	}
	if loadedMsg.Extra[persistedKey] != true {
		t.Errorf("_onclaw_persisted extra mismatch: expected true, got %v", loadedMsg.Extra[persistedKey])
	}
}

func TestCompactionAndToolTurnIntegration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-integration-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "onclaw.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	defer db.Close()

	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("failed to migrate db: %v", err)
	}

	convStore := sqlite.NewConversationStore(db)
	ctx := context.Background()

	convID, err := convStore.CreateConversation(ctx, "integration-agent")
	if err != nil {
		t.Fatalf("failed to create conversation: %v", err)
	}

	h := NewHistoryMiddleware(convStore, convID)

	userMsg := schema.UserAgenticMessage("Run tool")
	runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg},
		},
	}

	ctx, runCtx, err = h.BeforeAgent(ctx, runCtx)
	if err != nil {
		t.Fatalf("BeforeAgent failed: %v", err)
	}

	assistantMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{
				Type: schema.ContentBlockTypeFunctionToolCall,
				FunctionToolCall: &schema.FunctionToolCall{
					CallID:    "call_123",
					Name:      "test_tool",
					Arguments: `{}`,
				},
			},
		},
	}
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{userMsg, assistantMsg},
	}

	ctx, state, err = h.AfterModelRewriteState(ctx, state, nil)
	if err != nil {
		t.Fatalf("AfterModelRewriteState failed: %v", err)
	}

	toolMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{
			{
				Type: schema.ContentBlockTypeFunctionToolResult,
				FunctionToolResult: &schema.FunctionToolResult{
					CallID: "call_123",
					Name:   "test_tool",
					Content: []*schema.FunctionToolResultContentBlock{
						{
							Type: schema.FunctionToolResultContentBlockTypeText,
							Text: &schema.UserInputText{Text: "tool result"},
						},
					},
				},
			},
		},
	}
	state.Messages = append(state.Messages, toolMsg)

	finalAssistantMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "Here is the tool result"}),
		},
	}
	state.Messages = append(state.Messages, finalAssistantMsg)

	ctx, err = h.AfterAgent(ctx, state)
	if err != nil {
		t.Fatalf("AfterAgent failed: %v", err)
	}

	allMsgs, err := convStore.ListMessages(ctx, convID)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(allMsgs) != 4 {
		t.Fatalf("expected 4 persisted messages, got %d", len(allMsgs))
	}
	expectedRoles := []string{"user", "assistant", "user", "assistant"}
	for i, row := range allMsgs {
		if row.Seq != int64(i+1) {
			t.Errorf("expected msg %d to have seq %d, got %d", i, i+1, row.Seq)
		}
		if row.Role != expectedRoles[i] {
			t.Errorf("expected msg %d to have role %s, got %s", i, expectedRoles[i], row.Role)
		}
	}

	// --- Compaction integration test ---
	summaryMessage := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "Summary of turn 1"}),
		},
	}

	beforeState := adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{userMsg, assistantMsg, toolMsg, finalAssistantMsg},
	}
	afterState := adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{summaryMessage, finalAssistantMsg},
	}

	beforeMap := make(map[*schema.AgenticMessage]bool)
	for _, msg := range beforeState.Messages {
		beforeMap[msg] = true
	}

	var foundSummary *schema.AgenticMessage
	for _, msg := range afterState.Messages {
		if !beforeMap[msg] {
			foundSummary = msg
			break
		}
	}

	if foundSummary == nil {
		t.Fatalf("callback did not find summary message")
	}

	afterMap := make(map[*schema.AgenticMessage]bool)
	for _, msg := range afterState.Messages {
		afterMap[msg] = true
	}

	var maxSeq int64
	for _, msg := range beforeState.Messages {
		if !afterMap[msg] {
			if msg.Extra != nil {
				if seqVal, ok := msg.Extra["_onclaw_seq"].(int64); ok {
					if seqVal > maxSeq {
						maxSeq = seqVal
					}
				}
			}
		}
	}

	if maxSeq != 3 {
		t.Errorf("expected maxSeq of summarized messages to be 3, got %d", maxSeq)
	}

	redactedSummaryMsg := tools.RedactAgenticMessage(foundSummary)
	if redactedSummaryMsg.Extra == nil {
		redactedSummaryMsg.Extra = make(map[string]interface{})
	}
	redactedSummaryMsg.Extra[persistedKey] = true

	summaryMsgJSON, err := json.Marshal(redactedSummaryMsg)
	if err != nil {
		t.Fatalf("failed to marshal summary message: %v", err)
	}

	err = convStore.SaveSummary(ctx, convID, string(summaryMsgJSON), maxSeq)
	if err != nil {
		t.Fatalf("SaveSummary failed: %v", err)
	}

	foundSummary.Extra = map[string]interface{}{persistedKey: true}

	summaryRow, tailRows, err := convStore.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory failed: %v", err)
	}
	if summaryRow == nil {
		t.Fatalf("expected loaded summary row")
	}
	if summaryRow.Role != "assistant" || !strings.Contains(summaryRow.Message, "Summary of turn 1") {
		t.Errorf("unexpected summary message row: %+v", summaryRow)
	}

	if len(tailRows) != 1 {
		t.Fatalf("expected 1 tail message (finalAssistantMsg), got %d", len(tailRows))
	}
	if tailRows[0].Seq != 4 {
		t.Errorf("expected tail message seq 4, got %d", tailRows[0].Seq)
	}

	state2 := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{foundSummary, finalAssistantMsg},
	}
	err = h.saveUnmarkedMessages(ctx, state2.Messages)
	if err != nil {
		t.Fatalf("saveUnmarkedMessages failed: %v", err)
	}

	allMsgs2, err := convStore.ListMessages(ctx, convID)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(allMsgs2) != 5 {
		t.Errorf("expected exactly 5 messages in DB (no duplicates), got %d", len(allMsgs2))
	}
}

func TestCancellationNonPersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-cancel-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "onclaw.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	defer db.Close()

	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("failed to migrate db: %v", err)
	}

	convStore := sqlite.NewConversationStore(db)
	ctx := context.Background()

	convID, err := convStore.CreateConversation(ctx, "cancel-agent")
	if err != nil {
		t.Fatalf("failed to create conversation: %v", err)
	}

	h := NewHistoryMiddleware(convStore, convID)

	userMsg := schema.UserAgenticMessage("Prompt")
	runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg},
		},
	}

	ctx, runCtx, err = h.BeforeAgent(ctx, runCtx)
	if err != nil {
		t.Fatalf("BeforeAgent failed: %v", err)
	}

	cancelCtx, cancelFunc := context.WithCancel(ctx)
	cancelFunc()

	err = h.saveUnmarkedMessages(cancelCtx, []*schema.AgenticMessage{
		{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{Text: "partial answer"}),
			},
		},
	})
	if err == nil {
		t.Errorf("expected context.Canceled error when saving with cancelled context")
	}

	msgs, err := convStore.ListMessages(ctx, convID)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected only 1 message (user message) to be saved, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected saved message to be 'user', got %s", msgs[0].Role)
	}
}

func TestCancellationMidStreamPersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-midstream-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "onclaw.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	defer db.Close()

	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("failed to migrate db: %v", err)
	}

	convStore := sqlite.NewConversationStore(db)
	ctx := context.Background()

	convID, err := convStore.CreateConversation(ctx, "midstream-agent")
	if err != nil {
		t.Fatalf("failed to create conversation: %v", err)
	}

	h := NewHistoryMiddleware(convStore, convID)

	userMsg := schema.UserAgenticMessage("Prompt")
	runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg},
		},
	}

	ctx, runCtx, err = h.BeforeAgent(ctx, runCtx)
	if err != nil {
		t.Fatalf("BeforeAgent failed: %v", err)
	}

	cancelCtx, cancelFunc := context.WithCancel(ctx)
	cancelFunc()

	partialMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "Partial response before cancel..."}),
		},
	}
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{userMsg, partialMsg},
	}

	_, _, err = h.AfterModelRewriteState(cancelCtx, state, nil)
	if err == nil {
		t.Errorf("expected error from AfterModelRewriteState when context is canceled")
	}

	msgs, err := convStore.ListMessages(ctx, convID)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected exactly 1 message (user message), got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected saved message to be 'user', got %s", msgs[0].Role)
	}
}
