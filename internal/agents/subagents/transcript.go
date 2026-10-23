package subagents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// TranscriptStep names one kind of line in a materialized transcript.
type TranscriptStep string

const (
	StepTask      TranscriptStep = "task"
	StepText      TranscriptStep = "text"
	StepReasoning TranscriptStep = "reasoning"
	StepTool      TranscriptStep = "tool"
	StepResult    TranscriptStep = "result"
)

// TranscriptEntry is one JSONL record of a materialized transcript. The
// registry writes the task header and the terminal result; eino streams the
// child's own records between them through FormatAgenticTranscriptEntry.
type TranscriptEntry struct {
	Step    TranscriptStep `json:"step"`
	Content string         `json:"content,omitempty"`
	Status  string         `json:"status,omitempty"`
}

// FormatAgenticTranscriptEntry formats an AgenticMessage into one or more JSONL transcript entries.
func FormatAgenticTranscriptEntry(_ context.Context, _ string, message *schema.AgenticMessage) (string, error) {
	if message == nil {
		return "", nil
	}
	var entries []TranscriptEntry
	switch message.Role {
	case schema.AgenticRoleTypeUser:
		for _, b := range message.ContentBlocks {
			if b == nil {
				continue
			}
			if b.Type == schema.ContentBlockTypeUserInputText && b.UserInputText != nil {
				entries = append(entries, TranscriptEntry{Step: StepTask, Content: b.UserInputText.Text})
			}
			if b.Type == schema.ContentBlockTypeFunctionToolResult && b.FunctionToolResult != nil {
				var sb strings.Builder
				for _, cb := range b.FunctionToolResult.Content {
					if cb != nil && cb.Type == schema.FunctionToolResultContentBlockTypeText && cb.Text != nil {
						sb.WriteString(cb.Text.Text)
					}
				}
				entries = append(entries, TranscriptEntry{Step: StepTool, Content: fmt.Sprintf("result %s: %s", b.FunctionToolResult.Name, sb.String())})
			}
		}
	case schema.AgenticRoleTypeAssistant:
		for _, b := range message.ContentBlocks {
			if b == nil {
				continue
			}
			if b.Type == schema.ContentBlockTypeReasoning && b.Reasoning != nil && b.Reasoning.Text != "" {
				entries = append(entries, TranscriptEntry{Step: StepReasoning, Content: b.Reasoning.Text})
			}
			if b.Type == schema.ContentBlockTypeAssistantGenText && b.AssistantGenText != nil && b.AssistantGenText.Text != "" {
				entries = append(entries, TranscriptEntry{Step: StepText, Content: b.AssistantGenText.Text})
			}
			if b.Type == schema.ContentBlockTypeFunctionToolCall && b.FunctionToolCall != nil {
				entries = append(entries, TranscriptEntry{Step: StepTool, Content: fmt.Sprintf("call %s %s", b.FunctionToolCall.Name, b.FunctionToolCall.Arguments)})
			}
		}
	}
	if len(entries) == 0 {
		return "", nil
	}
	var sb strings.Builder
	for _, entry := range entries {
		b, err := json.Marshal(entry)
		if err != nil {
			return "", err
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}
