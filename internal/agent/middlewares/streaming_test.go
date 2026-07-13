package middlewares

import (
	"context"
	"testing"
)

func TestStreamingContextRoundTrip(t *testing.T) {
	// Default: streaming is disabled when the flag is unset.
	if StreamingFromContext(context.Background()) {
		t.Error("expected streaming to default to false")
	}

	// Explicit true.
	ctx := WithStreaming(context.Background(), true)
	if !StreamingFromContext(ctx) {
		t.Error("expected streaming true after WithStreaming(true)")
	}

	// Explicit false.
	ctx = WithStreaming(context.Background(), false)
	if StreamingFromContext(ctx) {
		t.Error("expected streaming false after WithStreaming(false)")
	}

	// An unrelated context value must not leak into the streaming flag.
	type otherKey string
	unrelated := context.WithValue(context.Background(), otherKey("other"), "value")
	if StreamingFromContext(unrelated) {
		t.Error("expected streaming false for an unrelated context value")
	}
}
