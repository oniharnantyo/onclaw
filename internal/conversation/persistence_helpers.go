package conversation

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/store"
)

const persistedKey = "_onclaw_persisted"

var uuidRegex = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

func unmarshalTurn(row *store.TurnRow) ([]*schema.AgenticMessage, error) {
	var msgs []*schema.AgenticMessage
	if err := json.Unmarshal([]byte(row.Message), &msgs); err != nil {
		return nil, fmt.Errorf("unmarshal turn messages: %w", err)
	}
	for _, msg := range msgs {
		if msg.Extra == nil {
			msg.Extra = make(map[string]interface{})
		}
		msg.Extra[persistedKey] = true
		msg.Extra["_onclaw_seq"] = row.SequenceNum
	}
	return msgs, nil
}

// stripReplayReasoning removes reasoning/thinking content blocks from a loaded
// message in place. It operates only on the freshly unmarshaled, per-turn
// replay copy; the persisted rows are never mutated (scrub-at-load).
func stripReplayReasoning(msg *schema.AgenticMessage) {
	if msg == nil || len(msg.ContentBlocks) == 0 {
		return
	}
	filtered := msg.ContentBlocks[:0]
	for _, b := range msg.ContentBlocks {
		if b != nil && b.Type == schema.ContentBlockTypeReasoning {
			continue
		}
		filtered = append(filtered, b)
	}
	msg.ContentBlocks = filtered
}

// sanitizeCorruptToolCalls scrubs tool-call blocks whose arguments are not a
// single valid JSON value, plus any tool-result blocks orphaned by that scrub —
// including results whose matching call was never present (e.g. two parallel
// calls whose streamed fragments were wrongly merged into one arguments blob,
// which both invalidates that call and dangles the other call's result). Like
// stripReplayReasoning it mutates only the freshly loaded replay copy; persisted
// rows are untouched. Messages left with no blocks are dropped so an empty
// assistant/tool turn is never handed to the provider.
func sanitizeCorruptToolCalls(msgs []*schema.AgenticMessage) []*schema.AgenticMessage {
	if len(msgs) == 0 {
		return msgs
	}

	// Collect CallIDs of assistant tool calls whose arguments are valid JSON
	// (empty arguments are allowed — a call with no parameters). Calls with
	// corrupt arguments are excluded, which also orphans their tool results.
	validCalls := make(map[string]struct{})
	corrupt := false
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		for _, b := range msg.ContentBlocks {
			if b == nil || b.FunctionToolCall == nil {
				continue
			}
			if args := b.FunctionToolCall.Arguments; args != "" && !json.Valid([]byte(args)) {
				corrupt = true
				continue
			}
			if b.FunctionToolCall.CallID != "" {
				validCalls[b.FunctionToolCall.CallID] = struct{}{}
			}
		}
	}
	if !corrupt {
		return msgs
	}

	out := make([]*schema.AgenticMessage, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		filtered := msg.ContentBlocks[:0]
		for _, b := range msg.ContentBlocks {
			if keepReplayBlock(b, validCalls) {
				filtered = append(filtered, b)
			}
		}
		msg.ContentBlocks = filtered
		if len(filtered) > 0 {
			out = append(out, msg)
		}
	}
	return out
}

// keepReplayBlock reports whether a content block survives corrupt-tool-call
// scrubbing: tool calls with invalid JSON arguments are dropped, tool results
// whose CallID has no surviving call are dropped as orphans, everything else is
// kept.
func keepReplayBlock(b *schema.ContentBlock, validCalls map[string]struct{}) bool {
	if b == nil {
		return false
	}
	switch {
	case b.FunctionToolCall != nil:
		args := b.FunctionToolCall.Arguments
		return args == "" || json.Valid([]byte(args))
	case b.FunctionToolResult != nil:
		if b.FunctionToolResult.CallID == "" {
			return true
		}
		_, ok := validCalls[b.FunctionToolResult.CallID]
		return ok
	default:
		return true
	}
}

// SanitizeSummaryMessage ensures the message complies with the SummaryMessage contract.
// It forces the role to user and strips any blocks that are not user_input_text.
// If it encounters an assistant_gen_text block (legacy shape), it converts it to user_input_text.
func SanitizeSummaryMessage(msg *schema.AgenticMessage) {
	if msg == nil {
		return
	}
	msg.Role = schema.AgenticRoleTypeUser
	var validBlocks []*schema.ContentBlock
	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		if block.Type == schema.ContentBlockTypeUserInputText {
			validBlocks = append(validBlocks, block)
		} else if block.Type == schema.ContentBlockTypeAssistantGenText && block.AssistantGenText != nil {
			validBlocks = append(validBlocks, &schema.ContentBlock{
				Type:          schema.ContentBlockTypeUserInputText,
				UserInputText: &schema.UserInputText{Text: block.AssistantGenText.Text},
			})
		}
	}
	msg.ContentBlocks = validBlocks
}

// IsPersisted checks if a message has been saved to the store.
func IsPersisted(msg *schema.AgenticMessage) bool {
	if msg == nil || msg.Extra == nil {
		return false
	}
	val, ok := msg.Extra[persistedKey]
	if !ok {
		return false
	}
	b, ok := val.(bool)
	return ok && b
}

func getAgenticMessageText(msg *schema.AgenticMessage) string {
	if msg == nil {
		return ""
	}
	var sb strings.Builder
	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		if block.UserInputText != nil {
			sb.WriteString(block.UserInputText.Text)
		} else if block.AssistantGenText != nil {
			sb.WriteString(block.AssistantGenText.Text)
		} else if block.FunctionToolResult != nil {
			for _, cb := range block.FunctionToolResult.Content {
				if cb != nil && cb.Text != nil {
					sb.WriteString(cb.Text.Text)
				}
			}
		}
	}
	return sb.String()
}

func extractQuestionAndAnswer(messages []*schema.AgenticMessage) (string, string) {
	var question, answer string
	for _, msg := range messages {
		if msg.Role == schema.AgenticRoleTypeUser {
			if question == "" {
				question = getAgenticMessageText(msg)
			}
		} else if msg.Role == schema.AgenticRoleTypeAssistant {
			answer = getAgenticMessageText(msg)
		}
	}
	return question, answer
}
