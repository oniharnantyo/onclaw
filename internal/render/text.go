package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/schema"
)

type textRenderer struct {
	w             io.Writer
	activeToolIdx int
	toolName      string
	toolArgs      strings.Builder
}

// Text constructs a new Renderer that formats output for CLI text rendering.
func Text(w io.Writer) Renderer {
	return &textRenderer{
		w:             w,
		activeToolIdx: -1,
	}
}

func (r *textRenderer) Render(msg *schema.AgenticMessage) error {
	if msg == nil {
		return nil
	}

	if msg.Role == schema.AgenticRoleTypeAssistant {
		for _, block := range msg.ContentBlocks {
			if block == nil {
				continue
			}

			// Handle text generation
			if block.AssistantGenText != nil {
				// Flush any pending tool call first
				if err := r.Flush(); err != nil {
					return err
				}
				if _, err := r.w.Write([]byte(block.AssistantGenText.Text)); err != nil {
					return err
				}
			}

			// Handle tool calls
			if block.FunctionToolCall != nil {
				idx := 0
				if block.StreamingMeta != nil {
					idx = block.StreamingMeta.Index
				}
				if r.activeToolIdx != idx {
					// Flush the previous tool call if any
					if err := r.Flush(); err != nil {
						return err
					}
					r.activeToolIdx = idx
					r.toolName = block.FunctionToolCall.Name
					r.toolArgs.Reset()
				}
				r.toolArgs.WriteString(block.FunctionToolCall.Arguments)
			}
		}
	}

	if msg.Role == schema.AgenticRoleTypeUser {
		// Flush any assistant tool call before printing tool results
		if err := r.Flush(); err != nil {
			return err
		}
		for _, block := range msg.ContentBlocks {
			if block == nil || block.FunctionToolResult == nil {
				continue
			}
			for _, cb := range block.FunctionToolResult.Content {
				if cb == nil || cb.Text == nil {
					continue
				}
				msgStr := fmt.Sprintf("\n[Tool Result] %s\n", cb.Text.Text)
				if _, err := r.w.Write([]byte(msgStr)); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (r *textRenderer) Flush() error {
	if r.activeToolIdx != -1 {
		msg := fmt.Sprintf("\n[Tool Call] Calling tool %q with arguments: %s\n", r.toolName, r.toolArgs.String())
		if _, err := r.w.Write([]byte(msg)); err != nil {
			return err
		}
		r.activeToolIdx = -1
		r.toolName = ""
		r.toolArgs.Reset()
	}
	return nil
}
