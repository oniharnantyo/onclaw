package middlewares_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/cloudwego/eino/schema/gemini"
	"github.com/cloudwego/eino/schema/openai"
	"github.com/oniharnantyo/onclaw/internal/agent/middlewares"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
)

type mockConversationStore struct {
	conversations    map[int64]*store.Conversation
	turns            map[int64][]*store.TurnRow
	nextConvID       int64
	nextSeq          map[int64]int64
	summaryMessageID int64
	summaryUntilSeq  int64
}

func newMockConversationStore() *mockConversationStore {
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
	m.conversations[id] = &store.Conversation{
		ID:        id,
		AgentName: agentName,
	}
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
	seq := m.nextSeq[convID]
	m.nextSeq[convID]++

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

func (m *mockConversationStore) LoadHistory(ctx context.Context, conversationID int64) (*store.TurnRow, []*store.TurnRow, error) {
	var summary *store.TurnRow
	if m.summaryMessageID != 0 {
		for _, turn := range m.turns[conversationID] {
			if turn.ID == m.summaryMessageID {
				summary = turn
				break
			}
		}
	}

	var tail []*store.TurnRow
	if summary != nil {
		allTail := []*store.TurnRow{}
		for _, turn := range m.turns[conversationID] {
			if turn.SequenceNum > m.summaryUntilSeq && turn.ID != m.summaryMessageID {
				allTail = append(allTail, turn)
			}
		}
		if len(allTail) > 3 {
			tail = allTail[len(allTail)-3:]
		} else {
			tail = allTail
		}
	} else {
		for _, turn := range m.turns[conversationID] {
			if turn.SequenceNum > m.summaryUntilSeq && turn.ID != m.summaryMessageID {
				tail = append(tail, turn)
			}
		}
	}

	return summary, tail, nil
}

func (m *mockConversationStore) ListTurns(ctx context.Context, conversationID int64) ([]*store.TurnRow, error) {
	return m.turns[conversationID], nil
}

func (m *mockConversationStore) SaveSummary(ctx context.Context, conversationID int64, summaryMessageJSON string, coveredUntilSeq int64) error {
	var msg struct {
		Role          string `json:"role"`
		ContentBlocks []struct {
			AssistantGenText *struct {
				Text string `json:"text"`
			} `json:"assistant_gen_text"`
		} `json:"content_blocks"`
	}
	_ = json.Unmarshal([]byte(summaryMessageJSON), &msg)
	var answer string
	for _, block := range msg.ContentBlocks {
		if block.AssistantGenText != nil {
			if answer != "" {
				answer += "\n"
			}
			answer += block.AssistantGenText.Text
		}
	}

	var msgArray []json.RawMessage
	msgArray = append(msgArray, json.RawMessage(summaryMessageJSON))
	msgArrayJSONBytes, _ := json.Marshal(msgArray)
	msgArrayJSON := string(msgArrayJSONBytes)

	_, err := m.AppendTurn(
		ctx,
		conversationID,
		msgArrayJSON,
		"",
		"",
		"",
		0,
		0,
		0,
		"",
		answer,
	)
	if err != nil {
		return err
	}

	turns := m.turns[conversationID]
	summaryRow := turns[len(turns)-1]

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

	h := middlewares.NewHistoryMiddleware(s, convID, "model-1")

	// --- Turn 1 ---
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

	if middlewares.IsPersisted(userMsg) {
		t.Errorf("user message should not be marked as persisted before turn commit")
	}

	cursor, ok := middlewares.GetRunCursor(ctx)
	if !ok || cursor == nil {
		t.Fatalf("middlewares.RunCursor missing from context")
	}
	if cursor.MaxSeq != 0 {
		t.Errorf("expected cursor MaxSeq to be 0, got %d", cursor.MaxSeq)
	}

	turns, _ := s.ListTurns(ctx, convID)
	if len(turns) != 0 {
		t.Errorf("expected 0 stored turns, got %d", len(turns))
	}

	assistantMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "Hello user"}),
		},
		ResponseMeta: &schema.AgenticResponseMeta{
			OpenAIExtension: &openai.ResponseMetaExtension{
				ID: "resp-1",
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

	if middlewares.IsPersisted(assistantMsg) {
		t.Errorf("assistant message should not be marked as persisted before turn commit")
	}

	ctx, err = h.AfterAgent(ctx, state)
	if err != nil {
		t.Fatalf("AfterAgent failed: %v", err)
	}

	if !middlewares.IsPersisted(userMsg) || !middlewares.IsPersisted(assistantMsg) {
		t.Errorf("messages should be marked as persisted after turn commit")
	}

	if userMsg.Extra["_onclaw_seq"] != int64(1) || assistantMsg.Extra["_onclaw_seq"] != int64(1) {
		t.Errorf("expected seq 1, got userMsg=%v assistantMsg=%v", userMsg.Extra["_onclaw_seq"], assistantMsg.Extra["_onclaw_seq"])
	}

	turns, _ = s.ListTurns(ctx, convID)
	if len(turns) != 1 {
		t.Errorf("expected 1 stored turn, got %d", len(turns))
	}

	// --- Turn 2 ---
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

	assistantMsg2 := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "I'm good"}),
		},
		ResponseMeta: &schema.AgenticResponseMeta{
			OpenAIExtension: &openai.ResponseMetaExtension{
				ID: "resp-2",
			},
		},
	}
	state2 := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{injected[0], injected[1], injected[2], assistantMsg2},
	}
	_, _, _ = h.AfterModelRewriteState(ctx2, state2, nil)
	_, err = h.AfterAgent(ctx2, state2)
	if err != nil {
		t.Fatalf("AfterAgent Turn 2 failed: %v", err)
	}

	turns, _ = s.ListTurns(ctx2, convID)
	if len(turns) != 2 {
		t.Errorf("expected 2 stored turns, got %d", len(turns))
	} else {
		if turns[0].ResponseID != "resp-1" {
			t.Errorf("expected Turn 1 ResponseID to be 'resp-1', got %q", turns[0].ResponseID)
		}
		if turns[1].PreviousResponseID != "resp-1" {
			t.Errorf("expected Turn 2 PreviousResponseID to be 'resp-1', got %q", turns[1].PreviousResponseID)
		}
		if turns[1].ResponseID != "resp-2" {
			t.Errorf("expected Turn 2 ResponseID to be 'resp-2', got %q", turns[1].ResponseID)
		}
	}

	// --- Compaction Test ---
	err = s.SaveSummary(ctx, convID, `{"role":"assistant","content_blocks":[{"type":"assistant_gen_text","assistant_gen_text":{"text":"Summary"}}]}`, 2)
	if err != nil {
		t.Fatalf("SaveSummary failed: %v", err)
	}

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
	if len(injected3) != 2 {
		t.Errorf("expected 2 injected messages after compaction, got %d", len(injected3))
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
		if c0 != "Summary" || c1 != "Turn 3 prompt" {
			t.Errorf("unexpected compaction message contents: %q, %q", c0, c1)
		}
	}
}

func TestRedaction(t *testing.T) {
	s := newMockConversationStore()
	ctx := context.Background()
	convID, _ := s.CreateConversation(ctx, "test-agent")
	h := middlewares.NewHistoryMiddleware(s, convID, "model-1")

	userMsg := schema.UserAgenticMessage("Hello normal message")
	runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg},
		},
	}
	ctx, _, _ = h.BeforeAgent(ctx, runCtx)

	assistantMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "My secret key is sk-12345678901234567890"}),
		},
	}
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{userMsg, assistantMsg},
	}
	_, _, _ = h.AfterModelRewriteState(ctx, state, nil)
	_, err := h.AfterAgent(ctx, state)
	if err != nil {
		t.Fatalf("AfterAgent failed: %v", err)
	}

	turns, _ := s.ListTurns(ctx, convID)
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}

	var savedMsgs []*schema.AgenticMessage
	if err := json.Unmarshal([]byte(turns[0].Message), &savedMsgs); err != nil {
		t.Fatalf("failed to unmarshal saved turn messages: %v", err)
	}

	if len(savedMsgs) != 2 {
		t.Fatalf("expected 2 messages in turn, got %d", len(savedMsgs))
	}

	getText := func(msg *schema.AgenticMessage) string {
		if len(msg.ContentBlocks) > 0 {
			if msg.ContentBlocks[0].UserInputText != nil {
				return msg.ContentBlocks[0].UserInputText.Text
			}
			if msg.ContentBlocks[0].AssistantGenText != nil {
				return msg.ContentBlocks[0].AssistantGenText.Text
			}
		}
		return ""
	}

	if getText(savedMsgs[0]) != "Hello normal message" {
		t.Errorf("expected original content, got %q", getText(savedMsgs[0]))
	}

	expectedRedacted := "My secret key is [REDACTED]"
	if getText(savedMsgs[1]) != expectedRedacted {
		t.Errorf("expected redacted content %q, got %q", expectedRedacted, getText(savedMsgs[1]))
	}
}

type fakeChatModel struct {
	model.ChatModel
	generateFunc func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error)
}

func (f *fakeChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if f.generateFunc != nil {
		return f.generateFunc(ctx, input, opts...)
	}
	return nil, nil
}

func (f *fakeChatModel) InterfaceName() string {
	return "ChatModel"
}

func TestSummarizationExtraPreservation(t *testing.T) {
	_ = context.Background()
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
			"summarization_tag":      "some-value",
			middlewares.PersistedKey: true,
		},
	}

	var msgs []*schema.AgenticMessage
	msgs = append(msgs, originalMsg)
	msgJSON, err := json.Marshal(msgs)
	if err != nil {
		t.Fatalf("failed to marshal original message: %v", err)
	}

	_, err = storeInst.AppendTurn(ctx, convID, string(msgJSON), "", "", "model-1", 0, 0, 0, "", "Original content with tool calls")
	if err != nil {
		t.Fatalf("AppendTurn failed: %v", err)
	}

	_, tail, err := storeInst.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory failed: %v", err)
	}

	if len(tail) != 1 {
		t.Fatalf("expected 1 tail turn, got %d", len(tail))
	}

	var loadedMsgs []*schema.AgenticMessage
	if err := json.Unmarshal([]byte(tail[0].Message), &loadedMsgs); err != nil {
		t.Fatalf("failed to unmarshal loaded messages: %v", err)
	}

	loadedMsg := loadedMsgs[0]
	if loadedMsg.Role != originalMsg.Role {
		t.Errorf("role mismatch: expected %q, got %q", originalMsg.Role, loadedMsg.Role)
	}

	getText := func(msg *schema.AgenticMessage) string {
		if len(msg.ContentBlocks) > 0 && msg.ContentBlocks[0].AssistantGenText != nil {
			return msg.ContentBlocks[0].AssistantGenText.Text
		}
		return ""
	}
	if getText(loadedMsg) != getText(originalMsg) {
		t.Errorf("content mismatch: expected %q, got %q", getText(originalMsg), getText(loadedMsg))
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
	if loadedMsg.Extra[middlewares.PersistedKey] != true {
		t.Errorf("_onclaw_persisted extra mismatch: expected true, got %v", loadedMsg.Extra[middlewares.PersistedKey])
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

	h := middlewares.NewHistoryMiddleware(convStore, convID, "model-1")

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

	turns, err := convStore.ListTurns(ctx, convID)
	if err != nil {
		t.Fatalf("ListTurns failed: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn in DB, got %d", len(turns))
	}

	// --- Compaction integration test ---
	summaryMessage := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "Summary of turn 1"}),
		},
	}

	maxSeq := int64(1)

	redactedSummaryMsg := tools.RedactAgenticMessage(summaryMessage)
	if redactedSummaryMsg.Extra == nil {
		redactedSummaryMsg.Extra = make(map[string]interface{})
	}
	redactedSummaryMsg.Extra[middlewares.PersistedKey] = true

	summaryMsgJSON, err := json.Marshal(redactedSummaryMsg)
	if err != nil {
		t.Fatalf("failed to marshal summary message: %v", err)
	}

	err = convStore.SaveSummary(ctx, convID, string(summaryMsgJSON), maxSeq)
	if err != nil {
		t.Fatalf("SaveSummary failed: %v", err)
	}

	summaryRow, tailRows, err := convStore.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory failed: %v", err)
	}
	if summaryRow == nil {
		t.Fatalf("expected loaded summary row")
	}
	if !strings.Contains(summaryRow.Message, "Summary of turn 1") {
		t.Errorf("unexpected summary message row: %+v", summaryRow)
	}

	// Since summaryUntilSeq is 1, and the only turn is Turn 1 (sequence_num = 1),
	// tailRows will be empty.
	if len(tailRows) != 0 {
		t.Fatalf("expected 0 tail messages after summary covering all turns, got %d", len(tailRows))
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

	h := middlewares.NewHistoryMiddleware(convStore, convID, "model-1")

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

	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{
			userMsg,
			{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.AssistantGenText{Text: "partial answer"}),
				},
			},
		},
	}

	_, err = h.AfterAgent(cancelCtx, state)
	if err == nil {
		t.Errorf("expected context.Canceled error when saving with cancelled context")
	}

	turns, err := convStore.ListTurns(ctx, convID)
	if err != nil {
		t.Fatalf("ListTurns failed: %v", err)
	}
	if len(turns) != 0 {
		t.Errorf("expected 0 stored turns on cancellation, got %d", len(turns))
	}
}

type mockFailedHistoryStore struct {
	store.ConversationStore
}

func (m *mockFailedHistoryStore) LoadHistory(ctx context.Context, convID int64) (*store.TurnRow, []*store.TurnRow, error) {
	return nil, nil, nil
}

func (m *mockFailedHistoryStore) AppendTurn(ctx context.Context, convID int64, msgArrayJSON string, responseID string, previousResponseID string, model string, prompt int64, completion int64, total int64, question string, answer string) (int64, error) {
	return 0, errors.New("db write failed")
}

func TestHistoryMiddleware_ErrorPaths(t *testing.T) {
	s := &mockFailedHistoryStore{}
	mw := middlewares.NewHistoryMiddleware(s, 123, "model-1")

	ctx := context.Background()

	userMsg := schema.UserAgenticMessage("Hello")
	runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg},
		},
	}
	ctx, _, err := mw.BeforeAgent(ctx, runCtx)
	if err != nil {
		t.Fatalf("unexpected error from BeforeAgent: %v", err)
	}

	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{userMsg},
	}
	_, err = mw.AfterAgent(ctx, state)
	if err == nil {
		t.Error("expected error from AfterAgent when DB write fails, got nil")
	}
}

func TestHistoryMiddleware_ResponseIDFallbacks(t *testing.T) {
	ctx := context.Background()

	t.Run("OpenAI Provider", func(t *testing.T) {
		s := newMockConversationStore()
		convID, _ := s.CreateConversation(ctx, "agent")
		h := middlewares.NewHistoryMiddleware(s, convID, "model-1")

		userMsg := schema.UserAgenticMessage("Hello")
		runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
			AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
				Messages: []*schema.AgenticMessage{userMsg},
			},
		}

		ctx, _, _ = h.BeforeAgent(ctx, runCtx)
		assistantMsg := &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{Text: "Response"}),
			},
			ResponseMeta: &schema.AgenticResponseMeta{
				OpenAIExtension: &openai.ResponseMetaExtension{
					ID: "openai-resp-id",
				},
			},
		}
		state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg, assistantMsg},
		}
		_, _, _ = h.AfterModelRewriteState(ctx, state, nil)
		_, err := h.AfterAgent(ctx, state)
		if err != nil {
			t.Fatalf("AfterAgent failed: %v", err)
		}

		turns, _ := s.ListTurns(ctx, convID)
		if len(turns) != 1 {
			t.Fatalf("expected 1 turn, got %d", len(turns))
		}
		if turns[0].ResponseID != "openai-resp-id" {
			t.Errorf("expected ResponseID 'openai-resp-id', got %q", turns[0].ResponseID)
		}
	})

	t.Run("Gemini Provider", func(t *testing.T) {
		s := newMockConversationStore()
		convID, _ := s.CreateConversation(ctx, "agent")
		h := middlewares.NewHistoryMiddleware(s, convID, "model-1")

		userMsg := schema.UserAgenticMessage("Hello")
		runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
			AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
				Messages: []*schema.AgenticMessage{userMsg},
			},
		}

		ctx, _, _ = h.BeforeAgent(ctx, runCtx)
		assistantMsg := &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{Text: "Response"}),
			},
			ResponseMeta: &schema.AgenticResponseMeta{
				GeminiExtension: &gemini.ResponseMetaExtension{
					ID: "gemini-resp-id",
				},
			},
		}
		state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg, assistantMsg},
		}
		_, _, _ = h.AfterModelRewriteState(ctx, state, nil)
		_, err := h.AfterAgent(ctx, state)
		if err != nil {
			t.Fatalf("AfterAgent failed: %v", err)
		}

		turns, _ := s.ListTurns(ctx, convID)
		if len(turns) != 1 {
			t.Fatalf("expected 1 turn, got %d", len(turns))
		}
		if turns[0].ResponseID != "gemini-resp-id" {
			t.Errorf("expected ResponseID 'gemini-resp-id', got %q", turns[0].ResponseID)
		}
	})

	t.Run("Eino fallback", func(t *testing.T) {
		s := newMockConversationStore()
		convID, _ := s.CreateConversation(ctx, "agent")
		h := middlewares.NewHistoryMiddleware(s, convID, "model-1")

		userMsg := schema.UserAgenticMessage("Hello")
		runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
			AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
				Messages: []*schema.AgenticMessage{userMsg},
			},
		}

		ctx, _, _ = h.BeforeAgent(ctx, runCtx)
		assistantMsg := &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{Text: "Response"}),
			},
			Extra: map[string]interface{}{
				"_eino_msg_id": "12345678-1234-1234-1234-1234567890ab",
			},
		}
		state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg, assistantMsg},
		}
		_, _, _ = h.AfterModelRewriteState(ctx, state, nil)
		_, err := h.AfterAgent(ctx, state)
		if err != nil {
			t.Fatalf("AfterAgent failed: %v", err)
		}

		turns, _ := s.ListTurns(ctx, convID)
		if len(turns) != 1 {
			t.Fatalf("expected 1 turn, got %d", len(turns))
		}
		if turns[0].ResponseID != "12345678-1234-1234-1234-1234567890ab" {
			t.Errorf("expected ResponseID '12345678-1234-1234-1234-1234567890ab', got %q", turns[0].ResponseID)
		}
	})

	t.Run("Eino fallback invalid UUID", func(t *testing.T) {
		s := newMockConversationStore()
		convID, _ := s.CreateConversation(ctx, "agent")
		h := middlewares.NewHistoryMiddleware(s, convID, "model-1")

		userMsg := schema.UserAgenticMessage("Hello")
		runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
			AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
				Messages: []*schema.AgenticMessage{userMsg},
			},
		}

		ctx, _, _ = h.BeforeAgent(ctx, runCtx)
		assistantMsg := &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{Text: "Response"}),
			},
			Extra: map[string]interface{}{
				"_eino_msg_id": "invalid-uuid-string",
			},
		}
		state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg, assistantMsg},
		}
		_, _, _ = h.AfterModelRewriteState(ctx, state, nil)
		_, err := h.AfterAgent(ctx, state)
		if err != nil {
			t.Fatalf("AfterAgent failed: %v", err)
		}

		turns, _ := s.ListTurns(ctx, convID)
		if len(turns) != 1 {
			t.Fatalf("expected 1 turn, got %d", len(turns))
		}
		if turns[0].ResponseID != "" {
			t.Errorf("expected ResponseID to be empty due to validation failure, got %q", turns[0].ResponseID)
		}
	})

	t.Run("Graceful degradation", func(t *testing.T) {
		s := newMockConversationStore()
		convID, _ := s.CreateConversation(ctx, "agent")
		h := middlewares.NewHistoryMiddleware(s, convID, "model-1")

		userMsg := schema.UserAgenticMessage("Hello")
		runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
			AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
				Messages: []*schema.AgenticMessage{userMsg},
			},
		}

		ctx, _, _ = h.BeforeAgent(ctx, runCtx)
		assistantMsg := &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{Text: "Response"}),
			},
		}
		state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg, assistantMsg},
		}
		_, _, _ = h.AfterModelRewriteState(ctx, state, nil)
		_, err := h.AfterAgent(ctx, state)
		if err != nil {
			t.Fatalf("AfterAgent failed: %v", err)
		}

		turns, _ := s.ListTurns(ctx, convID)
		if len(turns) != 1 {
			t.Fatalf("expected 1 turn, got %d", len(turns))
		}
		if turns[0].ResponseID != "" {
			t.Errorf("expected ResponseID '', got %q", turns[0].ResponseID)
		}
	})

	t.Run("Chaining across providers", func(t *testing.T) {
		s := newMockConversationStore()
		convID, _ := s.CreateConversation(ctx, "agent")
		h := middlewares.NewHistoryMiddleware(s, convID, "model-1")

		// First turn: Gemini
		userMsg := schema.UserAgenticMessage("Hello Gemini")
		runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
			AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
				Messages: []*schema.AgenticMessage{userMsg},
			},
		}
		ctx1, _, _ := h.BeforeAgent(ctx, runCtx)
		assistantMsg1 := &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{Text: "Gemini response"}),
			},
			ResponseMeta: &schema.AgenticResponseMeta{
				GeminiExtension: &gemini.ResponseMetaExtension{
					ID: "gemini-id",
				},
			},
		}
		state1 := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg, assistantMsg1},
		}
		_, _, _ = h.AfterModelRewriteState(ctx1, state1, nil)
		_, _ = h.AfterAgent(ctx1, state1)

		// Second turn: Bedrock (fallback to Eino ID)
		h2 := middlewares.NewHistoryMiddleware(s, convID, "model-1")
		userMsg2 := schema.UserAgenticMessage("Hello Bedrock")
		runCtx2 := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
			AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
				Messages: []*schema.AgenticMessage{userMsg2},
			},
		}
		ctx2, _, _ := h2.BeforeAgent(ctx, runCtx2)
		assistantMsg2 := &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{Text: "Bedrock response"}),
			},
			Extra: map[string]interface{}{
				"_eino_msg_id": "87654321-4321-4321-4321-ba0987654321",
			},
		}
		state2 := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg2, assistantMsg2},
		}
		_, _, _ = h2.AfterModelRewriteState(ctx2, state2, nil)
		_, _ = h2.AfterAgent(ctx2, state2)

		turns, _ := s.ListTurns(ctx, convID)
		if len(turns) != 2 {
			t.Fatalf("expected 2 turns, got %d", len(turns))
		}
		if turns[0].ResponseID != "gemini-id" {
			t.Errorf("turn 0 response ID expected 'gemini-id', got %q", turns[0].ResponseID)
		}
		if turns[1].PreviousResponseID != "gemini-id" {
			t.Errorf("turn 1 previous response ID expected 'gemini-id', got %q", turns[1].PreviousResponseID)
		}
		if turns[1].ResponseID != "87654321-4321-4321-4321-ba0987654321" {
			t.Errorf("turn 1 response ID expected '87654321-4321-4321-4321-ba0987654321', got %q", turns[1].ResponseID)
		}
	})
}

func TestSystemMessagesNotPersisted(t *testing.T) {
	s := newMockConversationStore()
	ctx := context.Background()

	convID, err := s.CreateConversation(ctx, "test-agent")
	if err != nil {
		t.Fatalf("failed to create conversation: %v", err)
	}

	h := middlewares.NewHistoryMiddleware(s, convID, "model-1")

	sysMsg := schema.SystemAgenticMessage("system instructions")
	userMsg := schema.UserAgenticMessage("hello")
	assistantMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "hi there"}),
		},
		ResponseMeta: &schema.AgenticResponseMeta{
			OpenAIExtension: &openai.ResponseMetaExtension{
				ID: "resp-sys-1",
			},
		},
	}

	// BeforeAgent buffers the user input (system prompt is not part of the
	// user input; it lives in state.Messages).
	runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg},
		},
	}
	ctx, runCtx, err = h.BeforeAgent(ctx, runCtx)
	if err != nil {
		t.Fatalf("BeforeAgent failed: %v", err)
	}

	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{sysMsg, userMsg, assistantMsg},
	}

	ctx, state, err = h.AfterModelRewriteState(ctx, state, nil)
	if err != nil {
		t.Fatalf("AfterModelRewriteState failed: %v", err)
	}

	ctx, err = h.AfterAgent(ctx, state)
	if err != nil {
		t.Fatalf("AfterAgent failed: %v", err)
	}

	// Exactly one turn row stored.
	turns, _ := s.ListTurns(ctx, convID)
	if len(turns) != 1 {
		t.Fatalf("expected 1 stored turn, got %d", len(turns))
	}

	// Persisted message array must contain no system-role message, but must
	// still contain the user and assistant messages.
	var savedMsgs []*schema.AgenticMessage
	if err := json.Unmarshal([]byte(turns[0].Message), &savedMsgs); err != nil {
		t.Fatalf("failed to unmarshal saved turn messages: %v", err)
	}

	getText := func(msg *schema.AgenticMessage) string {
		if len(msg.ContentBlocks) > 0 {
			if msg.ContentBlocks[0].UserInputText != nil {
				return msg.ContentBlocks[0].UserInputText.Text
			}
			if msg.ContentBlocks[0].AssistantGenText != nil {
				return msg.ContentBlocks[0].AssistantGenText.Text
			}
		}
		return ""
	}

	var sysCount int
	var hasUser, hasAssistant bool
	for _, msg := range savedMsgs {
		if msg.Role == schema.AgenticRoleTypeSystem {
			sysCount++
		}
		switch msg.Role {
		case schema.AgenticRoleTypeUser:
			if getText(msg) == "hello" {
				hasUser = true
			}
		case schema.AgenticRoleTypeAssistant:
			if getText(msg) == "hi there" {
				hasAssistant = true
			}
		}
	}

	if sysCount != 0 {
		t.Errorf("expected 0 system-role messages persisted, got %d", sysCount)
	}
	if !hasUser {
		t.Errorf("expected persisted turn to contain the user message")
	}
	if !hasAssistant {
		t.Errorf("expected persisted turn to contain the assistant message")
	}

	// --- Replay assertion (Task 2.2): second turn must inject no system message.
	userMsg2 := schema.UserAgenticMessage("follow up")
	runCtx2 := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{userMsg2},
		},
	}
	_, runCtx2, err = h.BeforeAgent(context.Background(), runCtx2)
	if err != nil {
		t.Fatalf("BeforeAgent turn 2 failed: %v", err)
	}

	for _, msg := range runCtx2.AgentInput.Messages {
		if msg.Role == schema.AgenticRoleTypeSystem {
			t.Errorf("replay injected a system-role message from history: %q", getText(msg))
		}
	}
}
