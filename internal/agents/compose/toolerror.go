package compose

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ToolErrorResult converts tool invocation errors into error results the
// model can read and react to (spec agent-tools "Tool failure feeds back"):
// a failing tool must not kill the turn — a 404 page, a jail rejection, a
// rate limit are turn data the agent should reason over (and often retry),
// not system failures. Cancellation stays a real error — the turn is being
// torn down, there is no next model call to answer the result. Interrupt
// signals are not failures either and must reach the ADK untouched.
//
// Both endpoint flavors are wrapped: the ADK builds separate invokable and
// streamable middleware chains from each handler, and the ToolsNode executes
// tool calls through the STREAMABLE one (invokable-only tools are auto-wrapped)
// — wrapping only WrapInvokableToolCall never sees the error.
//
// The middleware is per-turn (a fresh instance rides every composition) and
// doubles as the failure registry: it records each failed call's id and error
// text so the event drain can persist the terminal tool record as failed with
// the real error, while the model reads the JSON error payload as the result.
type ToolErrorResult struct {
	adk.BaseChatModelAgentMiddleware

	mu       sync.Mutex
	failures map[string]string // call id → error text
}

// NewToolErrorResult returns the per-turn middleware; append it last in the
// handler list so it wraps every tool endpoint.
func NewToolErrorResult() *ToolErrorResult {
	return &ToolErrorResult{failures: make(map[string]string)}
}

// FailureOf returns the recorded error text of a failed call, if any.
func (m *ToolErrorResult) FailureOf(callID string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	text, ok := m.failures[callID]
	return text, ok
}

// RecordFailure notes one failed call for the drain's terminal record.
func (m *ToolErrorResult) RecordFailure(callID, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures[callID] = text
}

func (m *ToolErrorResult) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		out, err := endpoint(ctx, argumentsInJSON, opts...)
		if err == nil || ctx.Err() != nil {
			return out, err
		}
		var interrupt *adk.InterruptSignal
		if errors.As(err, &interrupt) {
			return out, err
		}
		if tCtx != nil {
			m.RecordFailure(tCtx.CallID, err.Error())
		}
		return errorResultPayload(err, tCtx.Name), nil
	}, nil
}

func (m *ToolErrorResult) WrapStreamableToolCall(_ context.Context, endpoint adk.StreamableToolCallEndpoint, tCtx *adk.ToolContext) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		sr, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			var interrupt *adk.InterruptSignal
			if ctx.Err() != nil || errors.As(err, &interrupt) {
				return nil, err
			}
			if tCtx != nil {
				m.RecordFailure(tCtx.CallID, err.Error())
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
					if tCtx != nil {
						m.RecordFailure(tCtx.CallID, rerr.Error())
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
