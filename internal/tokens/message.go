package tokens

import (
	"github.com/cloudwego/eino/schema"
)

// MessageCharCount returns the total length (in bytes) of the text-bearing
// content blocks of an AgenticMessage. It walks user input, generated text,
// tool calls, and tool results, and feeds Estimate for coarse token budgeting
// when model-reported TokenUsage is unavailable.
func MessageCharCount(msg *schema.AgenticMessage) int {
	if msg == nil {
		return 0
	}
	var count int
	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		if block.UserInputText != nil {
			count += len(block.UserInputText.Text)
		}
		if block.AssistantGenText != nil {
			count += len(block.AssistantGenText.Text)
		}
		if block.Reasoning != nil {
			count += len(block.Reasoning.Text)
		}
		if block.FunctionToolCall != nil {
			count += len(block.FunctionToolCall.Name)
			count += len(block.FunctionToolCall.CallID)
			count += len(block.FunctionToolCall.Arguments)
		}
		if block.FunctionToolResult != nil {
			count += len(block.FunctionToolResult.Name)
			count += len(block.FunctionToolResult.CallID)
			for _, cb := range block.FunctionToolResult.Content {
				if cb != nil && cb.Text != nil {
					count += len(cb.Text.Text)
				}
			}
		}
	}
	return count
}

// EstimateMessage approximates the number of model tokens in an AgenticMessage
// by applying the chars/4 Estimate heuristic to its text content. It is the
// fallback for messages that lack model-reported TokenUsage.
func EstimateMessage(msg *schema.AgenticMessage) int {
	return Estimate(MessageCharCount(msg))
}
