package tokens_test

import (
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/tokens"
)

func TestMessageCharCount(t *testing.T) {
	tests := []struct {
		name string
		msg  *schema.AgenticMessage
		want int
	}{
		{"nil message", nil, 0},
		{"no content blocks", &schema.AgenticMessage{}, 0},
		{"nil block skipped", &schema.AgenticMessage{
			ContentBlocks: []*schema.ContentBlock{nil, {UserInputText: &schema.UserInputText{Text: "ab"}}},
		}, 2},
		{"user input text", &schema.AgenticMessage{
			ContentBlocks: []*schema.ContentBlock{{UserInputText: &schema.UserInputText{Text: "hello"}}},
		}, 5},
		{"assistant generated text", &schema.AgenticMessage{
			ContentBlocks: []*schema.ContentBlock{{AssistantGenText: &schema.AssistantGenText{Text: "reply!"}}},
		}, 6},
		{"reasoning text counted, signature ignored", &schema.AgenticMessage{
			ContentBlocks: []*schema.ContentBlock{{
				Reasoning: &schema.Reasoning{Text: "thinking", Signature: "opaque-signature-ignored"},
			}},
		}, len("thinking")},
		{"function tool call sums name, call id, arguments", &schema.AgenticMessage{
			ContentBlocks: []*schema.ContentBlock{{
				FunctionToolCall: &schema.FunctionToolCall{Name: "search", CallID: "call_1", Arguments: `{"q":"x"}`},
			}},
		}, len("search") + len("call_1") + len(`{"q":"x"}`)},
		{"function tool result sums name, call id, text content", &schema.AgenticMessage{
			ContentBlocks: []*schema.ContentBlock{{
				FunctionToolResult: &schema.FunctionToolResult{
					Name:   "search",
					CallID: "call_1",
					Content: []*schema.FunctionToolResultContentBlock{
						{Text: &schema.UserInputText{Text: "result-body"}},
						{}, // content block without text is ignored
					},
				},
			}},
		}, len("search") + len("call_1") + len("result-body")},
		{"multiple blocks accumulate", &schema.AgenticMessage{
			ContentBlocks: []*schema.ContentBlock{
				{UserInputText: &schema.UserInputText{Text: "abc"}},
				{AssistantGenText: &schema.AssistantGenText{Text: "de"}},
			},
		}, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tokens.MessageCharCount(tt.msg); got != tt.want {
				t.Errorf("MessageCharCount = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEstimateMessage(t *testing.T) {
	// 40 chars -> chars/4 -> 10 tokens.
	msg := &schema.AgenticMessage{
		ContentBlocks: []*schema.ContentBlock{
			{UserInputText: &schema.UserInputText{Text: "0123456789012345678901234567890123456789"}},
		},
	}
	if got, want := tokens.EstimateMessage(msg), 10; got != want {
		t.Errorf("EstimateMessage = %d, want %d", got, want)
	}
	if got := tokens.EstimateMessage(nil); got != 0 {
		t.Errorf("EstimateMessage(nil) = %d, want 0", got)
	}
}
