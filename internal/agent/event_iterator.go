package agent

import (
	"context"
	"io"
	"log/slog"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/schema"
)

type eventIterator struct {
	ctx           context.Context
	iterator      *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]
	currentStream *schema.StreamReader[*schema.AgenticMessage]
	err           error
	onTurnError   func(error)
	// onStopFlush is called on normal termination with the final message list;
	// used to flush memory in short sessions (EventStop / D3 task 4.4).
	onStopFlush func([]*schema.AgenticMessage)
	// collectedMsgs accumulates messages for the onStopFlush call.
	collectedMsgs        []*schema.AgenticMessage
	accumulatedStreamMsg *schema.AgenticMessage
}

func (it *eventIterator) Next() (Event, bool) {
	if it.err != nil {
		return Event{}, false
	}
	if err := it.ctx.Err(); err != nil {
		slog.Warn("agent_iterator_ctx_cancelled", "err", err)
		it.err = err
		return Event{}, false
	}

	// 1. Drain current message stream if any
	if it.currentStream != nil {
		chunk, err := it.currentStream.Recv()
		if err == nil {
			it.accumulatedStreamMsg = mergeMessageChunk(it.accumulatedStreamMsg, chunk)
			return Event{Message: chunk}, true
		}
		if err == io.EOF {
			if it.accumulatedStreamMsg != nil {
				it.collectedMsgs = append(it.collectedMsgs, it.accumulatedStreamMsg)
				it.accumulatedStreamMsg = nil
			}
		}
		it.currentStream.Close()
		it.currentStream = nil
		if err != io.EOF {
			it.err = err
			return Event{}, false
		}
	}

	// 2. Fetch the next event from Eino agent
	for {
		event, ok := it.iterator.Next()
		if !ok {
			// Normal session end — fire EventStop flush.
			if it.onStopFlush != nil {
				it.onStopFlush(it.collectedMsgs)
				it.onStopFlush = nil // fire once only
			}
			return Event{}, false
		}

		if event.Err != nil {
			slog.Warn("agent_iterator_event_error", "err", event.Err)
			if it.onTurnError != nil {
				it.onTurnError(event.Err)
			}
			it.err = event.Err
			return Event{}, false
		}

		if event.Action != nil {
			if event.Action.Interrupted != nil {
				return Event{}, false
			}
			if event.Action.CustomizedAction != nil {
				if sig, ok := parseCompactionSignal(event.Action.CustomizedAction); ok {
					return Event{Compaction: sig}, true
				}
			}
		}

		if event.Output != nil && event.Output.MessageOutput != nil {
			mv := event.Output.MessageOutput
			if mv.IsStreaming && mv.MessageStream != nil {
				it.currentStream = mv.MessageStream
				it.accumulatedStreamMsg = nil
				return it.Next()
			} else if mv.Message != nil {
				it.collectedMsgs = append(it.collectedMsgs, mv.Message)
				return Event{Message: mv.Message}, true
			}
		}
	}
}

func (it *eventIterator) Err() error {
	return it.err
}

func (it *eventIterator) CollectedTurn() []*schema.AgenticMessage {
	return it.collectedMsgs
}

func mergeMessageChunk(accumulated *schema.AgenticMessage, chunk *schema.AgenticMessage) *schema.AgenticMessage {
	if accumulated == nil {
		return cloneMessage(chunk)
	}

	if accumulated.Role == "" {
		accumulated.Role = chunk.Role
	}

	for _, cb := range chunk.ContentBlocks {
		if cb == nil {
			continue
		}
		if len(accumulated.ContentBlocks) == 0 {
			accumulated.ContentBlocks = append(accumulated.ContentBlocks, cloneContentBlock(cb))
			continue
		}

		lastBlock := accumulated.ContentBlocks[len(accumulated.ContentBlocks)-1]
		if lastBlock.Type == cb.Type {
			switch cb.Type {
			case schema.ContentBlockTypeAssistantGenText:
				if lastBlock.AssistantGenText != nil && cb.AssistantGenText != nil {
					lastBlock.AssistantGenText.Text += cb.AssistantGenText.Text
				}
			case schema.ContentBlockTypeFunctionToolCall:
				if lastBlock.FunctionToolCall != nil && cb.FunctionToolCall != nil {
					if cb.FunctionToolCall.CallID != "" {
						lastBlock.FunctionToolCall.CallID = cb.FunctionToolCall.CallID
					}
					if cb.FunctionToolCall.Name != "" {
						lastBlock.FunctionToolCall.Name = cb.FunctionToolCall.Name
					}
					lastBlock.FunctionToolCall.Arguments += cb.FunctionToolCall.Arguments
				}
			case schema.ContentBlockTypeReasoning:
				if lastBlock.Reasoning != nil && cb.Reasoning != nil {
					lastBlock.Reasoning.Text += cb.Reasoning.Text
					if cb.Reasoning.Signature != "" {
						lastBlock.Reasoning.Signature = cb.Reasoning.Signature
					}
					if cb.Reasoning.OpenAIExtension != nil {
						lastBlock.Reasoning.OpenAIExtension = cb.Reasoning.OpenAIExtension
					}
				}
			default:
				accumulated.ContentBlocks = append(accumulated.ContentBlocks, cloneContentBlock(cb))
			}
		} else {
			accumulated.ContentBlocks = append(accumulated.ContentBlocks, cloneContentBlock(cb))
		}
	}

	if chunk.Extra != nil {
		if accumulated.Extra == nil {
			accumulated.Extra = make(map[string]interface{})
		}
		for k, v := range chunk.Extra {
			accumulated.Extra[k] = v
		}
	}

	if chunk.ResponseMeta != nil {
		if accumulated.ResponseMeta == nil {
			accumulated.ResponseMeta = &schema.AgenticResponseMeta{}
		}
		if chunk.ResponseMeta.TokenUsage != nil {
			accumulated.ResponseMeta.TokenUsage = chunk.ResponseMeta.TokenUsage
		}
		if chunk.ResponseMeta.OpenAIExtension != nil {
			accumulated.ResponseMeta.OpenAIExtension = chunk.ResponseMeta.OpenAIExtension
		}
		if chunk.ResponseMeta.GeminiExtension != nil {
			accumulated.ResponseMeta.GeminiExtension = chunk.ResponseMeta.GeminiExtension
		}
	}

	return accumulated
}

func cloneMessage(msg *schema.AgenticMessage) *schema.AgenticMessage {
	if msg == nil {
		return nil
	}
	res := &schema.AgenticMessage{
		Role: msg.Role,
	}
	if msg.ContentBlocks != nil {
		res.ContentBlocks = make([]*schema.ContentBlock, len(msg.ContentBlocks))
		for i, cb := range msg.ContentBlocks {
			res.ContentBlocks[i] = cloneContentBlock(cb)
		}
	}
	if msg.Extra != nil {
		res.Extra = make(map[string]interface{})
		for k, v := range msg.Extra {
			res.Extra[k] = v
		}
	}
	if msg.ResponseMeta != nil {
		res.ResponseMeta = &schema.AgenticResponseMeta{
			OpenAIExtension: msg.ResponseMeta.OpenAIExtension,
			GeminiExtension: msg.ResponseMeta.GeminiExtension,
		}
		if msg.ResponseMeta.TokenUsage != nil {
			res.ResponseMeta.TokenUsage = &schema.TokenUsage{
				PromptTokens:     msg.ResponseMeta.TokenUsage.PromptTokens,
				CompletionTokens: msg.ResponseMeta.TokenUsage.CompletionTokens,
				TotalTokens:      msg.ResponseMeta.TokenUsage.TotalTokens,
			}
		}
	}
	return res
}

func cloneContentBlock(cb *schema.ContentBlock) *schema.ContentBlock {
	if cb == nil {
		return nil
	}
	res := &schema.ContentBlock{
		Type: cb.Type,
	}
	if cb.AssistantGenText != nil {
		res.AssistantGenText = &schema.AssistantGenText{
			Text: cb.AssistantGenText.Text,
		}
	}
	if cb.FunctionToolCall != nil {
		res.FunctionToolCall = &schema.FunctionToolCall{
			CallID:    cb.FunctionToolCall.CallID,
			Name:      cb.FunctionToolCall.Name,
			Arguments: cb.FunctionToolCall.Arguments,
		}
	}
	if cb.FunctionToolResult != nil {
		res.FunctionToolResult = &schema.FunctionToolResult{
			CallID: cb.FunctionToolResult.CallID,
			Name:   cb.FunctionToolResult.Name,
		}
		if cb.FunctionToolResult.Content != nil {
			res.FunctionToolResult.Content = make([]*schema.FunctionToolResultContentBlock, len(cb.FunctionToolResult.Content))
			for i, rcb := range cb.FunctionToolResult.Content {
				res.FunctionToolResult.Content[i] = &schema.FunctionToolResultContentBlock{
					Type: rcb.Type,
					Text: rcb.Text,
				}
			}
		}
	}
	if cb.Reasoning != nil {
		res.Reasoning = &schema.Reasoning{
			Text:            cb.Reasoning.Text,
			Signature:       cb.Reasoning.Signature,
			OpenAIExtension: cb.Reasoning.OpenAIExtension,
		}
	}
	return res
}

// parseCompactionSignal maps a summarization middleware CustomizedAction to a
// compaction progress signal. Returns ok=false for any non-summarization action
// so other middleware actions pass through untouched.
//
// Progress mapping (per design): before_summarize -> start + 10%,
// generate_summary -> 60%, after_summarize -> 100% + complete.
func parseCompactionSignal(action any) (*CompactionSignal, bool) {
	ca, ok := action.(*summarization.TypedCustomizedAction[*schema.AgenticMessage])
	if !ok {
		return nil, false
	}
	switch ca.Type {
	case summarization.ActionTypeBeforeSummarize:
		return &CompactionSignal{Status: CompactionStarted, Progress: 10}, true
	case summarization.ActionTypeGenerateSummary:
		return &CompactionSignal{Progress: 60}, true
	case summarization.ActionTypeAfterSummarize:
		return &CompactionSignal{Status: CompactionCompleted, Progress: 100}, true
	}
	return nil, false
}
