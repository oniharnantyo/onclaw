package memory

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ExtractAndFlush extracts durable facts from candidates and flushes them to the searchable archive.
func ExtractAndFlush(
	ctx context.Context,
	chatModel model.AgenticModel,
	memoryStore MemoryStore,
	embedder *Embedder,
	kvStore store.KVStore,
	agentName string,
	conversationID int64,
	messages []*schema.AgenticMessage,
	skipSecurityScan bool,
) error {
	cursorKey := fmt.Sprintf("memory_cursor:%d", conversationID)
	var lastCursor int64
	if kvStore != nil {
		if val, err := kvStore.Get(ctx, cursorKey); err == nil && val != "" {
			lastCursor, _ = strconv.ParseInt(val, 10, 64)
		}
	}

	var candidates []*schema.AgenticMessage
	var maxSeq int64 = lastCursor

	for _, msg := range messages {
		if msg == nil {
			continue
		}
		if msg.Extra != nil {
			if processed, ok := msg.Extra["_onclaw_memcursor"].(bool); ok && processed {
				continue
			}
		}
		var seq int64
		if msg.Extra != nil {
			if s, ok := msg.Extra["_onclaw_seq"].(int64); ok {
				seq = s
			} else if sf, ok := msg.Extra["_onclaw_seq"].(float64); ok {
				seq = int64(sf)
			}
		}
		if seq > 0 && seq <= lastCursor {
			continue
		}
		if seq > maxSeq {
			maxSeq = seq
		}
		candidates = append(candidates, msg)
	}

	if len(candidates) == 0 {
		return nil
	}

	segmentText := formatMessages(candidates)
	if strings.TrimSpace(segmentText) == "" {
		return nil
	}

	var extractedText string
	var llmFailed bool

	if chatModel != nil {
		prompt := fmt.Sprintf(`Analyze the following conversation segment and extract a list of new durable facts, user preferences, and project decisions.
Keep the facts extremely concise and direct.
Do NOT include temporary conversational details, pleasantries, or questions.
Format each fact as a separate line starting with a bullet "- ".
If no new long-term facts or preferences are found, reply only with "NONE".

Conversation Segment:
%s`, segmentText)

		resp, err := chatModel.Generate(ctx, []*schema.AgenticMessage{
			schema.UserAgenticMessage(prompt),
		})
		if err != nil {
			llmFailed = true
		} else if resp != nil {
			extractedText = getAgenticMessageText(resp)
		} else {
			llmFailed = true
		}
	} else {
		llmFailed = true
	}

	if llmFailed {
		extractedText = extractiveFallback(candidates)
	}

	lines := strings.Split(extractedText, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			line = strings.TrimPrefix(line, "- ")
		} else if strings.HasPrefix(line, "* ") {
			line = strings.TrimPrefix(line, "* ")
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.ToUpper(line) == "NONE" {
			continue
		}

		if !skipSecurityScan {
			if err := ScanContent(line); err != nil {
				continue
			}
		}

		var vector []float32
		if embedder != nil {
			vector, _ = embedder.Embed(ctx, line)
		}

		doc := &MemoryDocument{
			Agent:     agentName,
			Scope:     "global",
			Kind:      "episodic",
			Content:   line,
			Source:    fmt.Sprintf("conversation_%d", conversationID),
			CreatedAt: nowString(),
		}
		_, _ = memoryStore.IndexDocument(ctx, doc, vector)
	}

	for _, msg := range candidates {
		if msg.Extra == nil {
			msg.Extra = make(map[string]interface{})
		}
		msg.Extra["_onclaw_memcursor"] = true
	}

	if kvStore != nil && maxSeq > lastCursor {
		_ = kvStore.Set(ctx, cursorKey, strconv.FormatInt(maxSeq, 10))
	}

	return nil
}

func formatMessages(msgs []*schema.AgenticMessage) string {
	var sb strings.Builder
	for _, msg := range msgs {
		if msg.Role == "user" || msg.Role == "assistant" {
			content := getAgenticMessageText(msg)
			sb.WriteString(fmt.Sprintf("%s: %s\n", msg.Role, content))
		}
	}
	return sb.String()
}

func extractiveFallback(msgs []*schema.AgenticMessage) string {
	var lines []string
	for _, msg := range msgs {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		content := getAgenticMessageText(msg)
		for _, line := range strings.Split(content, "\n") {
			lineLower := strings.ToLower(line)
			if strings.Contains(lineLower, "prefer") ||
				strings.Contains(lineLower, "always") ||
				strings.Contains(lineLower, "remember") ||
				strings.Contains(lineLower, "must") ||
				strings.Contains(lineLower, "should") ||
				strings.Contains(lineLower, "need") ||
				strings.Contains(lineLower, "decide") {
				lines = append(lines, fmt.Sprintf("- %s", strings.TrimSpace(line)))
			}
		}
	}
	if len(lines) == 0 {
		return "NONE"
	}
	return strings.Join(lines, "\n")
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

func nowString() string {
	return time.Now().Format(time.RFC3339)
}
