package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/schema"
)

// inputTokenCounter mirrors Eino's default summarization token counter
// (summarization.defaultTypedTokenCounter) but anchors the baseline on the most
// recent assistant message's PromptTokens instead of TotalTokens. This measures
// true context input fill rather than input plus the prior completion, so a long
// assistant completion does not inflate the trigger for a long-output (coding)
// agent. The increment shape (chars/4 estimate for newer messages and tool
// definitions) is preserved from the default.
func inputTokenCounter(_ context.Context, input *summarization.TypedTokenCounterInput[*schema.AgenticMessage]) (int, error) {
	var (
		baseTokens     int
		incrementStart int
	)

	for i := len(input.Messages) - 1; i >= 0; i-- {
		if tokens := assistantPromptTokens(input.Messages[i]); tokens > 0 {
			baseTokens = tokens
			incrementStart = i + 1
			break
		}
	}

	var incrementTokens int
	for _, msg := range input.Messages[incrementStart:] {
		incrementTokens += estimateAgenticMessageTokens(msg)
	}

	for _, tl := range input.Tools {
		tl_ := *tl
		tl_.Extra = nil
		text, err := json.Marshal(tl_)
		if err != nil {
			return 0, fmt.Errorf("failed to marshal tool info: %w", err)
		}
		incrementTokens += estimateTokenCount(len(text))
	}

	return baseTokens + incrementTokens, nil
}

func assistantPromptTokens(msg *schema.AgenticMessage) int {
	if msg == nil {
		return 0
	}
	if msg.Role != schema.AgenticRoleTypeAssistant || msg.ResponseMeta == nil || msg.ResponseMeta.TokenUsage == nil {
		return 0
	}
	return msg.ResponseMeta.TokenUsage.PromptTokens
}

const multimodalTokenEstimate = 2000

// estimateAgenticMessageTokens mirrors summarization.estimateAgenticMessageTokens:
// total character length of text content divided by 4, plus a fixed estimate per
// multimodal block.
func estimateAgenticMessageTokens(msg *schema.AgenticMessage) int {
	if msg == nil {
		return 0
	}
	var totalLen int
	var multimodalTokens int

	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		switch block.Type {
		case schema.ContentBlockTypeAssistantGenText:
			if block.AssistantGenText != nil {
				totalLen += len(block.AssistantGenText.Text)
			}
		case schema.ContentBlockTypeFunctionToolCall:
			if block.FunctionToolCall != nil {
				totalLen += len(block.FunctionToolCall.Name) + len(block.FunctionToolCall.Arguments)
			}
		case schema.ContentBlockTypeReasoning:
			if block.Reasoning != nil {
				totalLen += len(block.Reasoning.Text)
			}
		case schema.ContentBlockTypeUserInputText:
			if block.UserInputText != nil {
				totalLen += len(block.UserInputText.Text)
			}
		case schema.ContentBlockTypeFunctionToolResult:
			for _, cb := range block.FunctionToolResult.Content {
				if cb == nil {
					continue
				}
				switch cb.Type {
				case schema.FunctionToolResultContentBlockTypeText:
					if cb.Text != nil {
						totalLen += len(cb.Text.Text)
					}
				default:
					multimodalTokens += multimodalTokenEstimate
				}
			}
		case schema.ContentBlockTypeToolSearchResult:
			if block.ToolSearchFunctionToolResult != nil && block.ToolSearchFunctionToolResult.Result != nil {
				for _, tl := range block.ToolSearchFunctionToolResult.Result.Tools {
					totalLen += len(tl.Name) + len(tl.Desc)
					if b, err := json.Marshal(tl.ParamsOneOf); err == nil {
						totalLen += len(b)
					}
				}
			}
		case schema.ContentBlockTypeUserInputImage, schema.ContentBlockTypeUserInputFile,
			schema.ContentBlockTypeUserInputAudio, schema.ContentBlockTypeUserInputVideo,
			schema.ContentBlockTypeAssistantGenImage, schema.ContentBlockTypeAssistantGenAudio,
			schema.ContentBlockTypeAssistantGenVideo:
			multimodalTokens += multimodalTokenEstimate
		}
	}

	return estimateTokenCount(totalLen) + multimodalTokens
}

func estimateTokenCount(charLen int) int {
	return charLen / 4
}

// buildTranscriptPath returns the per-conversation transcript file path for the
// compacted range. It lives under the agent workspace so the filesystem
// middleware can later read it back via the path Eino appends to the summary.
func buildTranscriptPath(workspace string, conversationID int64) string {
	return filepath.Join(workspace, ".onclaw", "transcripts", fmt.Sprintf("conversation-%d.txt", conversationID))
}
