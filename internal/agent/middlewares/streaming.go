package middlewares

import (
	"context"
)

const streamingCtxKey = contextKey("onclaw_streaming")

// WithStreaming attaches the per-call streaming flag to the context. When
// enabled, the agent run invokes the model's streaming path and emits
// token-level delta chunks; when disabled (the default), the run emits one
// complete message per model call. This is a peer of the previous-response-id
// context option: set by the channel entry point, read inside Agent.Run.
func WithStreaming(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, streamingCtxKey, enabled)
}

// StreamingFromContext reports whether streaming is enabled for this run. It
// returns false when the flag has not been set.
func StreamingFromContext(ctx context.Context) bool {
	enabled, ok := ctx.Value(streamingCtxKey).(bool)
	if !ok {
		return false
	}
	return enabled
}
