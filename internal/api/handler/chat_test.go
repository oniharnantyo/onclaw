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
	"github.com/oniharnantyo/onclaw/internal/store"
)

type dummyEventIterator struct{}

func (d *dummyEventIterator) Next() (*schema.AgenticMessage, bool) { return nil, false }
func (d *dummyEventIterator) Err() error                           { return nil }

type dummyAgent struct {
	runFn        func(ctx context.Context, userInput string, contentBlocks ...*schema.ContentBlock) agent.EventIterator
	lastTurnMeta *store.TurnMeta
}

func (d *dummyAgent) Run(ctx context.Context, userInput string, contentBlocks ...*schema.ContentBlock) agent.EventIterator {
	if d.runFn != nil {
		return d.runFn(ctx, userInput, contentBlocks...)
	}
	return &dummyEventIterator{}
}

func (d *dummyAgent) LastTurnMeta() *store.TurnMeta {
	return d.lastTurnMeta
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
	var capturedPrompt string
	var capturedBlocks []*schema.ContentBlock

	f.svc.SetResolve(func(ctx context.Context, agentName, providerName, modelName, reasoning, workspace string, convID int64) (service.AssembledAgent, string, error) {
		return &dummyAgent{
			runFn: func(ctx context.Context, userInput string, contentBlocks ...*schema.ContentBlock) agent.EventIterator {
				capturedPrompt = userInput
				capturedBlocks = contentBlocks
				return &dummyEventIterator{}
			},
		}, "/tmp", nil
	})

	body, _ := json.Marshal(service.ChatInput{Prompt: "hello prompt only"})
	req := makeReq(http.MethodPost, "/api/chat", string(body))
	w := httptest.NewRecorder()
	f.h.Chat(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if capturedPrompt != "hello prompt only" {
		t.Errorf("expected prompt 'hello prompt only', got %q", capturedPrompt)
	}
	if len(capturedBlocks) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(capturedBlocks))
	}
}

func TestChat_WithImageBlock(t *testing.T) {
	f := newHFixture(t)
	var capturedBlocks []*schema.ContentBlock

	f.svc.SetResolve(func(ctx context.Context, agentName, providerName, modelName, reasoning, workspace string, convID int64) (service.AssembledAgent, string, error) {
		return &dummyAgent{
			runFn: func(ctx context.Context, userInput string, contentBlocks ...*schema.ContentBlock) agent.EventIterator {
				capturedBlocks = contentBlocks
				return &dummyEventIterator{}
			},
		}, "/tmp", nil
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
	if len(capturedBlocks) != 1 {
		t.Errorf("expected 1 block, got %d", len(capturedBlocks))
	} else {
		img := capturedBlocks[0].UserInputImage
		if img == nil || img.Base64Data != "abc" || img.MIMEType != "image/png" {
			t.Errorf("unexpected image block content: %+v", img)
		}
	}
}

func TestChat_WithFileBlock(t *testing.T) {
	f := newHFixture(t)
	var capturedBlocks []*schema.ContentBlock

	f.svc.SetResolve(func(ctx context.Context, agentName, providerName, modelName, reasoning, workspace string, convID int64) (service.AssembledAgent, string, error) {
		return &dummyAgent{
			runFn: func(ctx context.Context, userInput string, contentBlocks ...*schema.ContentBlock) agent.EventIterator {
				capturedBlocks = contentBlocks
				return &dummyEventIterator{}
			},
		}, "/tmp", nil
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
	if len(capturedBlocks) != 1 {
		t.Errorf("expected 1 block, got %d", len(capturedBlocks))
	} else {
		file := capturedBlocks[0].UserInputFile
		if file == nil || file.Name != "file.txt" || file.Base64Data != "abc" || file.MIMEType != "text/plain" {
			t.Errorf("unexpected file block content: %+v", file)
		}
	}
}

func TestChat_InputFloorExceedsSafetyLimit(t *testing.T) {
	f := newHFixture(t)
	// The resolve (assembly) path fails fast when the static tool/system floor
	// exceeds the safety limit; the handler must translate that into a 400.
	f.svc.SetResolve(func(ctx context.Context, agentName, providerName, modelName, reasoning, workspace string, convID int64) (service.AssembledAgent, string, error) {
		return nil, "", fmt.Errorf("input floor 5000 tokens reaches safety limit 3700 tokens (context window 7400): %w", middlewares.ErrInputFloorExceedsSafetyLimit)
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

	f.svc.SetResolve(func(ctx context.Context, agentName, providerName, modelName, reasoning, workspace string, convID int64) (service.AssembledAgent, string, error) {
		return &dummyAgent{
			runFn: func(ctx context.Context, userInput string, contentBlocks ...*schema.ContentBlock) agent.EventIterator {
				if id, ok := middlewares.GetPreviousResponseID(ctx); ok {
					capturedPrevID = id
				}
				return &dummyEventIterator{}
			},
			lastTurnMeta: &store.TurnMeta{
				ConversationID:     convID,
				SequenceNum:        2,
				ResponseID:         "resp-2",
				PreviousResponseID: "resp-1",
				Model:              "gpt-4",
				Tokens:             30,
			},
		}, "/tmp", nil
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
	if !strings.Contains(respBody, `"response_id":"resp-2"`) {
		t.Errorf("expected turn event to contain response_id 'resp-2'")
	}
	if !strings.Contains(respBody, `"previous_response_id":"resp-1"`) {
		t.Errorf("expected turn event to contain previous_response_id 'resp-1'")
	}
}
