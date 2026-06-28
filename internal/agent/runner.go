package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// RunAgent runs a single turn of the agent, streaming output to stdout/writer and logging to transcript.
func RunAgent(ctx context.Context, a *Agent, userInput string, stdout io.Writer, transcriptPath string) error {
	// Debug: Log agent configuration and system prompt
	slog.Debug("agent_run",
		"agent_name", a.Config.Name,
		"workspace", a.Workspace,
		"system_prompt", a.Config.SystemPrompt,
	)

	// Debug: Log user message being sent to agent
	slog.Debug("agent_user_input",
		"agent_name", a.Config.Name,
		"user_input", userInput,
		"input_length", len(userInput),
	)

	// 1. Log the user message to the transcript
	_ = AppendToTranscript(transcriptPath, &TranscriptEntry{
		Type:    "user",
		Content: userInput,
	})

	// 2. Prepare Agent Input
	input := &adk.TypedAgentInput[*schema.Message]{
		Messages: []*schema.Message{
			schema.UserMessage(userInput),
		},
	}

	// 3. Start Agent Run
	iterator := a.EinoAgent.Run(ctx, input)

	for {
		// Honor context cancellation
		if err := ctx.Err(); err != nil {
			_ = AppendToTranscript(transcriptPath, &TranscriptEntry{
				Type: "interrupted",
			})
			return err
		}

		event, ok := iterator.Next()
		if !ok {
			break
		}

		if event.Err != nil {
			_ = AppendToTranscript(transcriptPath, &TranscriptEntry{
				Type:  "error",
				Error: event.Err.Error(),
			})
			return event.Err
		}

		// Handle agent action interrupts
		if event.Action != nil && event.Action.Interrupted != nil {
			_ = AppendToTranscript(transcriptPath, &TranscriptEntry{
				Type: "interrupted",
			})
			fmt.Fprintln(stdout, "\n[Agent interrupted]")
			return nil
		}

		// Process output if present
		if event.Output != nil && event.Output.MessageOutput != nil {
			mv := event.Output.MessageOutput

			if mv.Role == schema.Assistant {
				var fullContent strings.Builder
				var toolCalls []schema.ToolCall

				if mv.IsStreaming && mv.MessageStream != nil {
					for {
						chunk, err := mv.MessageStream.Recv()
						if err == io.EOF {
							break
						}
						if err != nil {
							return err
						}
						_, _ = stdout.Write([]byte(chunk.Content))
						fullContent.WriteString(chunk.Content)
						if len(chunk.ToolCalls) > 0 {
							toolCalls = append(toolCalls, chunk.ToolCalls...)
						}
					}
				} else if mv.Message != nil {
					_, _ = stdout.Write([]byte(mv.Message.Content))
					fullContent.WriteString(mv.Message.Content)
					toolCalls = mv.Message.ToolCalls
				}

				assistantText := fullContent.String()
				if assistantText != "" {
					_ = AppendToTranscript(transcriptPath, &TranscriptEntry{
						Type:    "assistant",
						Content: assistantText,
					})
				}

				for _, tc := range toolCalls {
					msg := fmt.Sprintf("\n[Tool Call] Calling tool %q with arguments: %s\n", tc.Function.Name, tc.Function.Arguments)
					_, _ = stdout.Write([]byte(msg))

					_ = AppendToTranscript(transcriptPath, &TranscriptEntry{
						Type:      "tool_call",
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					})
				}
			}

			if mv.Role == schema.Tool {
				var content string
				if mv.IsStreaming && mv.MessageStream != nil {
					var sb strings.Builder
					for {
						chunk, err := mv.MessageStream.Recv()
						if err == io.EOF {
							break
						}
						if err != nil {
							return err
						}
						sb.WriteString(chunk.Content)
					}
					content = sb.String()
				} else if mv.Message != nil {
					content = mv.Message.Content
				}

				msg := fmt.Sprintf("\n[Tool Result] %s\n", content)
				_, _ = stdout.Write([]byte(msg))

				_ = AppendToTranscript(transcriptPath, &TranscriptEntry{
					Type:   "tool_result",
					Name:   mv.ToolName,
					Result: content,
				})
			}
		}
	}

	// If finished because context is cancelled, make sure we log it
	if err := ctx.Err(); err != nil {
		_ = AppendToTranscript(transcriptPath, &TranscriptEntry{
			Type: "interrupted",
		})
		return err
	}

	return nil
}
