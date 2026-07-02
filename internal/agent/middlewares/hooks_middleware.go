package middlewares

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/hooks"
)

// SessionState holds the session identifier and source channel, shared with the agent.
type SessionState struct {
	Channel   string
	SessionID string
}

// HooksMiddleware intercepts agent prompt, tool execution, and stop lifecycle events.
type HooksMiddleware struct {
	adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	Dispatcher *hooks.Dispatcher
	AgentName  string
	Session    *SessionState
}

// NewHooksMiddleware creates a new HooksMiddleware.
func NewHooksMiddleware(d *hooks.Dispatcher, agentName string, session *SessionState) *HooksMiddleware {
	return &HooksMiddleware{
		Dispatcher: d,
		AgentName:  agentName,
		Session:    session,
	}
}

// BeforeAgent implements the user_prompt_submit lifecycle event hook.
func (m *HooksMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext[*schema.AgenticMessage]) (context.Context, *adk.ChatModelAgentContext[*schema.AgenticMessage], error) {
	var prompt string
	if len(runCtx.AgentInput.Messages) > 0 {
		prompt = runCtx.AgentInput.Messages[len(runCtx.AgentInput.Messages)-1].String()
	}

	payload := hooks.Payload{
		Agent:     m.AgentName,
		Channel:   m.Session.Channel,
		SessionID: m.Session.SessionID,
		Prompt:    prompt,
	}

	dec, err := m.Dispatcher.Fire(ctx, hooks.EventUserPromptSubmit, payload)
	if dec == hooks.DecisionBlock {
		reason := "prompt blocked by agent hooks"
		if err != nil {
			reason = err.Error()
		}
		if len(runCtx.AgentInput.Messages) > 0 {
			runCtx.AgentInput.Messages[len(runCtx.AgentInput.Messages)-1] = schema.UserAgenticMessage("[Blocked] " + reason)
		} else {
			runCtx.AgentInput.Messages = append(runCtx.AgentInput.Messages, schema.UserAgenticMessage("[Blocked] "+reason))
		}
	}

	return ctx, runCtx, nil
}

// WrapInvokableToolCall implements the pre_tool_use and post_tool_use hooks for synchronous tools.
func (m *HooksMiddleware) WrapInvokableToolCall(ctx context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		var args map[string]interface{}
		_ = json.Unmarshal([]byte(argumentsInJSON), &args)

		payload := hooks.Payload{
			Agent:     m.AgentName,
			Channel:   m.Session.Channel,
			SessionID: m.Session.SessionID,
			ToolName:  tCtx.Name,
			ToolArgs:  args,
		}

		dec, err := m.Dispatcher.Fire(ctx, hooks.EventPreToolUse, payload)
		if dec == hooks.DecisionBlock {
			reason := "tool execution blocked by agent hooks"
			if err != nil {
				reason = err.Error()
			}
			return fmt.Sprintf("Tool call blocked: %s", reason), nil
		}

		res, err := endpoint(ctx, argumentsInJSON, opts...)

		postPayload := hooks.Payload{
			Agent:      m.AgentName,
			Channel:    m.Session.Channel,
			SessionID:  m.Session.SessionID,
			ToolName:   tCtx.Name,
			ToolArgs:   args,
			ToolResult: res,
		}
		if err != nil {
			postPayload.Error = err.Error()
		}
		_, _ = m.Dispatcher.Fire(ctx, hooks.EventPostToolUse, postPayload)

		return res, err
	}, nil
}

// WrapStreamableToolCall implements the pre_tool_use and post_tool_use hooks for streaming tools.
func (m *HooksMiddleware) WrapStreamableToolCall(ctx context.Context, endpoint adk.StreamableToolCallEndpoint, tCtx *adk.ToolContext) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		var args map[string]interface{}
		_ = json.Unmarshal([]byte(argumentsInJSON), &args)

		payload := hooks.Payload{
			Agent:     m.AgentName,
			Channel:   m.Session.Channel,
			SessionID: m.Session.SessionID,
			ToolName:  tCtx.Name,
			ToolArgs:  args,
		}

		dec, err := m.Dispatcher.Fire(ctx, hooks.EventPreToolUse, payload)
		if dec == hooks.DecisionBlock {
			reason := "tool execution blocked by agent hooks"
			if err != nil {
				reason = err.Error()
			}
			sr, sw := schema.Pipe[string](1)
			sw.Send(fmt.Sprintf("Tool call blocked: %s", reason), nil)
			sw.Close()
			return sr, nil
		}

		sr, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			postPayload := hooks.Payload{
				Agent:     m.AgentName,
				Channel:   m.Session.Channel,
				SessionID: m.Session.SessionID,
				ToolName:  tCtx.Name,
				ToolArgs:  args,
				Error:     err.Error(),
			}
			_, _ = m.Dispatcher.Fire(ctx, hooks.EventPostToolUse, postPayload)
			return nil, err
		}

		outReader, outWriter := schema.Pipe[string](1)
		go func() {
			defer outWriter.Close()
			var accumulated []string
			for {
				chunk, err := sr.Recv()
				if err != nil {
					if errors.Is(err, io.EOF) {
						break
					}
					outWriter.Send("", err)
					postPayload := hooks.Payload{
						Agent:      m.AgentName,
						Channel:    m.Session.Channel,
						SessionID:  m.Session.SessionID,
						ToolName:   tCtx.Name,
						ToolArgs:   args,
						ToolResult: strings.Join(accumulated, ""),
						Error:      err.Error(),
					}
					_, _ = m.Dispatcher.Fire(ctx, hooks.EventPostToolUse, postPayload)
					return
				}
				accumulated = append(accumulated, chunk)
				outWriter.Send(chunk, nil)
			}

			postPayload := hooks.Payload{
				Agent:      m.AgentName,
				Channel:    m.Session.Channel,
				SessionID:  m.Session.SessionID,
				ToolName:   tCtx.Name,
				ToolArgs:   args,
				ToolResult: strings.Join(accumulated, ""),
			}
			_, _ = m.Dispatcher.Fire(ctx, hooks.EventPostToolUse, postPayload)
		}()

		return outReader, nil
	}, nil
}

// AfterAgent implements the stop lifecycle event hook on normal turn completion.
func (m *HooksMiddleware) AfterAgent(ctx context.Context, state *adk.TypedChatModelAgentState[*schema.AgenticMessage]) (context.Context, error) {
	payload := hooks.Payload{
		Agent:     m.AgentName,
		Channel:   m.Session.Channel,
		SessionID: m.Session.SessionID,
	}
	_, _ = m.Dispatcher.Fire(ctx, hooks.EventStop, payload)

	return ctx, nil
}
