package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// toolErrorResultMiddleware converts tool invocation errors into error
// results the model can read and react to. A failing tool must not kill the
// run: a 404 page, a jail rejection, a rate limit are turn data the agent
// should reason over (and often retry), not system failures. Cancellation
// stays a real error — the run is being torn down, there is no next turn to
// answer the result. Interrupt signals are not failures either: they are the
// approval flow's control channel (tool.Interrupt pauses the run for user
// consent) and must reach the ADK untouched.
//
// Both endpoint flavors are wrapped: the ADK builds separate invokable and
// streamable middleware chains from each handler, and the ToolsNode executes
// tool calls through the STREAMABLE one (invokable-only tools are auto-wrapped)
// — wrapping only WrapInvokableToolCall never sees the error.
type toolErrorResultMiddleware struct {
	adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
}

// newToolErrorResultMiddleware returns the ADK middleware; append it last in
// buildMiddlewares so it wraps every tool endpoint, including the ones the
// filesystem middleware registers (its jail rejections killed runs before
// this wrapper existed).
func newToolErrorResultMiddleware() adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage] {
	return &toolErrorResultMiddleware{}
}

func (m *toolErrorResultMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		out, err := endpoint(ctx, argumentsInJSON, opts...)
		if err == nil || ctx.Err() != nil {
			return out, err
		}
		var interrupt *adk.InterruptSignal
		if errors.As(err, &interrupt) {
			return out, err
		}
		return errorResultPayload(err, tCtx.Name), nil
	}, nil
}

func (m *toolErrorResultMiddleware) WrapStreamableToolCall(_ context.Context, endpoint adk.StreamableToolCallEndpoint, tCtx *adk.ToolContext) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		sr, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			var interrupt *adk.InterruptSignal
			if ctx.Err() != nil || errors.As(err, &interrupt) {
				return nil, err
			}
			return errorResultStream(errorResultPayload(err, tCtx.Name)), nil
		}
		// Pass frames through; a mid-stream error frame becomes the error
		// result instead of poisoning the collected tool output.
		out, sender := schema.Pipe[string](1)
		go func() {
			defer sender.Close()
			for {
				chunk, rerr := sr.Recv()
				if rerr != nil {
					if errors.Is(rerr, io.EOF) {
						return
					}
					var interrupt *adk.InterruptSignal
					if ctx.Err() != nil || errors.As(rerr, &interrupt) {
						sr.Close()
						return
					}
					_ = sender.Send(errorResultPayload(rerr, tCtx.Name), nil)
					return
				}
				if sender.Send(chunk, nil) {
					sr.Close()
					return
				}
			}
		}()
		return out, nil
	}, nil
}

// errorResultPayload renders a tool failure as the JSON result handed to the
// model: the failure text plus which tool produced it.
func errorResultPayload(err error, toolName string) string {
	payload, merr := json.Marshal(map[string]string{"error": err.Error(), "tool": toolName})
	if merr != nil {
		return `{"error":` + strconv.Quote(err.Error()) + `}`
	}
	return string(payload)
}

// errorResultStream wraps a rendered error result in a single-frame stream.
func errorResultStream(payload string) *schema.StreamReader[string] {
	sr, sender := schema.Pipe[string](1)
	_ = sender.Send(payload, nil)
	sender.Close()
	return sr
}
