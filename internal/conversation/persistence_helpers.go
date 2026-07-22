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
