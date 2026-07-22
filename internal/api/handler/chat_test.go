package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agent"
	"github.com/oniharnantyo/onclaw/internal/agent/middlewares"
	"github.com/oniharnantyo/onclaw/internal/api/service"
	"github.com/oniharnantyo/onclaw/internal/conversation"
)

type dummyEventIterator struct {
	msgs []*schema.AgenticMessage
}

func (d *dummyEventIterator) Next() (agent.Event, bool)               { return agent.Event{}, false }
func (d *dummyEventIterator) Err() error                              { return nil }
func (d *dummyEventIterator) CollectedTurn() []*schema.AgenticMessage { return d.msgs }

type dummyAgent struct {
	runFn func(ctx context.Context, messages []*schema.AgenticMessage) agent.EventIterator
}

func (d *dummyAgent) Run(ctx context.Context, messages []*schema.AgenticMessage) agent.EventIterator {
	if d.runFn != nil {
		return d.runFn(ctx, messages)
	}
	return &dummyEventIterator{}
}

func (d *dummyAgent) ContextWindow() int {
	return 64000
}

func (d *dummyAgent) AgentName() string {
	return "test"
}

func TestChat_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPost, "/api/chat", "bad-json")
	w := httptest.NewRecorder()
	f.h.Chat(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChat_EmptyPrompt(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(service.ChatInput{Prompt: ""})
	req := makeReq(http.MethodPost, "/api/chat", string(body))
	w := httptest.NewRecorder()
	f.h.Chat(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChat_PromptOnly(t *testing.T) {
	f := newHFixture(t)
	var capturedMessages []*schema.AgenticMessage

	f.svc.SetResolve(func(ctx context.Context, agentName, providerName, modelName, reasoning, workspace string, convID int64) (service.AssembledAgent, *conversation.SessionManager, string, error) {
		sessionMgr := conversation.NewSessionManager(&hFakeConversationStore{}, convID, "gpt-4")
		ag := &dummyAgent{
			runFn: func(ctx context.Context, messages []*schema.AgenticMessage) agent.EventIterator {
				capturedMessages = messages
				return &dummyEventIterator{}
			},
		}
		return ag, sessionMgr, "/tmp", nil
	})

	body, _ := json.Marshal(service.ChatInput{Prompt: "hello prompt only"})
	req := makeReq(http.MethodPost, "/api/chat", string(body))
	w := httptest.NewRecorder()
	f.h.Chat(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if len(capturedMessages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(capturedMessages))
	}
}

func TestChat_WithImageBlock(t *testing.T) {
	f := newHFixture(t)
	var capturedMessages []*schema.AgenticMessage

	f.svc.SetResolve(func(ctx context.Context, agentName, providerName, modelName, reasoning, workspace string, convID int64) (service.AssembledAgent, *conversation.SessionManager, string, error) {
		sessionMgr := conversation.NewSessionManager(&hFakeConversationStore{}, convID, "gpt-4")
		ag := &dummyAgent{
			runFn: func(ctx context.Context, messages []*schema.AgenticMessage) agent.EventIterator {
				capturedMessages = messages
				return &dummyEventIterator{}
			},
		}
		return ag, sessionMgr, "/tmp", nil
	})

	body, _ := json.Marshal(service.ChatInput{
		Prompt: "hello image",
		ContentBlocks: []*schema.ContentBlock{
			{
				Type: schema.ContentBlockTypeUserInputImage,
				UserInputImage: &schema.UserInputImage{
					Base64Data: "abc",
					MIMEType:   "image/png",
				},
			},
		},
	})
	req := makeReq(http.MethodPost, "/api/chat", string(body))
	w := httptest.NewRecorder()
	f.h.Chat(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if len(capturedMessages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(capturedMessages))
	}
}

func TestChat_WithFileBlock(t *testing.T) {
	f := newHFixture(t)
	var capturedMessages []*schema.AgenticMessage

	f.svc.SetResolve(func(ctx context.Context, agentName, providerName, modelName, reasoning, workspace string, convID int64) (service.AssembledAgent, *conversation.SessionManager, string, error) {
		sessionMgr := conversation.NewSessionManager(&hFakeConversationStore{}, convID, "gpt-4")
		ag := &dummyAgent{
			runFn: func(ctx context.Context, messages []*schema.AgenticMessage) agent.EventIterator {
				capturedMessages = messages
				return &dummyEventIterator{}
			},
		}
		return ag, sessionMgr, "/tmp", nil
	})

	body, _ := json.Marshal(service.ChatInput{
		Prompt: "hello file",
		ContentBlocks: []*schema.ContentBlock{
			{
				Type: schema.ContentBlockTypeUserInputFile,
				UserInputFile: &schema.UserInputFile{
					Name:       "file.txt",
					Base64Data: "abc",
					MIMEType:   "text/plain",
				},
			},
		},
	})
	req := makeReq(http.MethodPost, "/api/chat", string(body))
	w := httptest.NewRecorder()
	f.h.Chat(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if len(capturedMessages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(capturedMessages))
	}
}

func TestChat_InputFloorExceedsSafetyLimit(t *testing.T) {
	f := newHFixture(t)
	f.svc.SetResolve(func(ctx context.Context, agentName, providerName, modelName, reasoning, workspace string, convID int64) (service.AssembledAgent, *conversation.SessionManager, string, error) {
		return nil, nil, "", fmt.Errorf("input floor 5000 tokens reaches safety limit 3700 tokens (context window 7400): %w", middlewares.ErrInputFloorExceedsSafetyLimit)
	})

	body, _ := json.Marshal(service.ChatInput{Prompt: "hello"})
	req := makeReq(http.MethodPost, "/api/chat", string(body))
	w := httptest.NewRecorder()
	f.h.Chat(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for input floor error, got %d", w.Code)
	}
}

func TestChat_TurnEventAndPreviousResponseID(t *testing.T) {
	f := newHFixture(t)
	var capturedPrevID string

	f.svc.SetResolve(func(ctx context.Context, agentName, providerName, modelName, reasoning, workspace string, convID int64) (service.AssembledAgent, *conversation.SessionManager, string, error) {
		sessionMgr := conversation.NewSessionManager(&hFakeConversationStore{}, convID, "gpt-4")
		ag := &dummyAgent{
			runFn: func(ctx context.Context, messages []*schema.AgenticMessage) agent.EventIterator {
				if id, ok := conversation.GetPreviousResponseID(ctx); ok {
					capturedPrevID = id
				}
				turnMsgs := append(messages, &schema.AgenticMessage{
					Role: schema.AgenticRoleTypeAssistant,
					ContentBlocks: []*schema.ContentBlock{
						schema.NewContentBlock(&schema.AssistantGenText{Text: "Ok"}),
					},
					Extra: map[string]interface{}{
						"_eino_msg_id": "123e4567-e89b-12d3-a456-426614174000",
					},
				})
				return &dummyEventIterator{msgs: turnMsgs}
			},
		}
		return ag, sessionMgr, "/tmp", nil
	})

	body, _ := json.Marshal(service.ChatInput{
		Prompt:             "hello prev",
		PreviousResponseID: "resp-1",
	})
	req := makeReq(http.MethodPost, "/api/chat", string(body))
	w := httptest.NewRecorder()
	f.h.Chat(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	if capturedPrevID != "resp-1" {
		t.Errorf("expected capturedPreviousResponseID to be 'resp-1', got %q", capturedPrevID)
	}

	respBody := w.Body.String()
	if !strings.Contains(respBody, "event: turn") {
		t.Errorf("expected SSE body to contain 'event: turn', got %q", respBody)
	}
}

// capturingConvStore records the question column and message JSON passed to
// AppendTurn so tests can assert the driving user prompt is persisted as part of
// the turn (not just the assistant's output).
type capturingConvStore struct {
	hFakeConversationStore
	question string
	turnMsgs string
}

func (c *capturingConvStore) AppendTurn(_ context.Context, _ int64, turnMsgs, _, _, _ string, _, _, _ int64, question, _ string) (int64, error) {
	c.turnMsgs = turnMsgs
	c.question = question
	return 1, nil
}

// TestChat_PersistsUserQueryInTurn guards the regression where CollectedTurn()
// carries only agent outputs (assistant/tool) and the driving user prompt was
// dropped from the committed turn — leaving the question column empty and the
// persisted message array missing the user message.
func TestChat_PersistsUserQueryInTurn(t *testing.T) {
	f := newHFixture(t)
	capture := &capturingConvStore{}

	f.svc.SetResolve(func(ctx context.Context, agentName, providerName, modelName, reasoning, workspace string, convID int64) (service.AssembledAgent, *conversation.SessionManager, string, error) {
		sessionMgr := conversation.NewSessionManager(capture, convID, "gpt-4")
		ag := &dummyAgent{
			runFn: func(ctx context.Context, messages []*schema.AgenticMessage) agent.EventIterator {
				// CollectedTurn yields ONLY the assistant output — mirroring the real
				// iterator, which never echoes the input user message.
				return &dummyEventIterator{msgs: []*schema.AgenticMessage{
					{
						Role: schema.AgenticRoleTypeAssistant,
						ContentBlocks: []*schema.ContentBlock{
							schema.NewContentBlock(&schema.AssistantGenText{Text: "response"}),
						},
						Extra: map[string]interface{}{
							"_eino_msg_id": "123e4567-e89b-12d3-a456-426614174000",
						},
					},
				}}
			},
		}
		return ag, sessionMgr, "/tmp", nil
	})

	body, _ := json.Marshal(service.ChatInput{Prompt: "what is the meaning of life"})
	req := makeReq(http.MethodPost, "/api/chat", string(body))
	w := httptest.NewRecorder()
	f.h.Chat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if capture.question != "what is the meaning of life" {
		t.Errorf("expected question column to hold the user prompt, got %q", capture.question)
	}
	if !strings.Contains(capture.turnMsgs, "what is the meaning of life") {
		t.Errorf("expected persisted message JSON to contain the user prompt, got %q", capture.turnMsgs)
	}
}
