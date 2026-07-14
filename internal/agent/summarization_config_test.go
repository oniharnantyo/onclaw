package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/schema"
)

func userMsg(text string) *schema.AgenticMessage {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeUserInputText, UserInputText: &schema.UserInputText{Text: text}},
		},
	}
}

func assistantMsg(prompt, total int, text string) *schema.AgenticMessage {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ResponseMeta: &schema.AgenticResponseMeta{
			TokenUsage: &schema.TokenUsage{PromptTokens: prompt, TotalTokens: total},
		},
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: text}},
		},
	}
}

// A long completion must not inflate the measured token count: the counter
// anchors on the most recent assistant message's PromptTokens, not TotalTokens.
func TestInputTokenCounterLongCompletion(t *testing.T) {
	msgs := []*schema.AgenticMessage{
		userMsg("hi"),
		assistantMsg(100, 5000, strings.Repeat("x", 4900)), // prompt 100, huge completion
	}

	got, err := inputTokenCounter(context.Background(), &summarization.TypedTokenCounterInput[*schema.AgenticMessage]{Messages: msgs})
	if err != nil {
		t.Fatalf("inputTokenCounter: %v", err)
	}
	if got != 100 {
		t.Errorf("expected baseline 100 (PromptTokens), got %d", got)
	}
}

// Baseline is the LAST assistant message's PromptTokens; messages newer than it
// are estimated at chars/4. A long completion on the last assistant still does
// not inflate the count, and the older assistant is ignored.
func TestInputTokenCounterIncrement(t *testing.T) {
	msgs := []*schema.AgenticMessage{
		userMsg("first"),
		assistantMsg(50, 4000, strings.Repeat("y", 3900)), // older assistant, large completion
		assistantMsg(200, 600, "short"),                  // last assistant: prompt 200
		userMsg(strings.Repeat("z", 400)),                // 400 chars newer user message
	}
	// baseline = 200 (last assistant prompt); increment = 400/4 = 100; older assistant ignored.
	want := 200 + 100
	got, err := inputTokenCounter(context.Background(), &summarization.TypedTokenCounterInput[*schema.AgenticMessage]{Messages: msgs})
	if err != nil {
		t.Fatalf("inputTokenCounter: %v", err)
	}
	if got != want {
		t.Errorf("expected %d, got %d", want, got)
	}
}

func TestBuildTranscriptPath(t *testing.T) {
	p := buildTranscriptPath("/workspace", 42)
	if !strings.Contains(p, "/workspace") || !strings.HasSuffix(p, "conversation-42.txt") {
		t.Errorf("unexpected transcript path: %s", p)
	}
}
