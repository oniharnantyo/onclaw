package gateways

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// spanPayload marshals a span.model_request_end payload in the ADK
// serializer's shape the reader's tolerant decode expects.
func spanPayload(in, out, total int) []byte {
	b, err := json.Marshal(map[string]any{
		"span": map[string]any{
			"model": map[string]any{
				"usage": map[string]any{
					"input_tokens":  in,
					"output_tokens": out,
					"raw":           map[string]any{"total_tokens": total},
				},
			},
		},
	})
	if err != nil {
		panic(err)
	}
	return b
}

// TestSessionUsageReader_CarriesNoBreakdown pins the gateway /usage
// pass-through (adopt-assistant-ui-elements D7): the reader aggregates the
// raw span.model_request_end rows, which carry provider numbers only, so the
// returned payload never fabricates a context breakdown — segments are
// measured once, at the runner's turn end, and the display-only block rides
// the runner's terminal payload alone.
func TestSessionUsageReader_CarriesNoBreakdown(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	reader := NewSessionUsageReader(st.SessionEvents())

	// No spans yet → nil usage, no block of any kind.
	usage, err := reader.LatestUsage(ctx, "ws", "sess-1")
	if err != nil {
		t.Fatalf("LatestUsage on empty log: %v", err)
	}
	if usage != nil {
		t.Fatalf("usage = %+v, want nil", usage)
	}

	if err := st.SessionEvents().AppendEvents(ctx, "ws", []domain.SessionEvent{
		{SessionID: "sess-1", EventID: "s1", TurnID: "turn-1", Seq: 1, Kind: "span.model_request_end", Payload: spanPayload(30000, 1500, 31500)},
		{SessionID: "sess-1", EventID: "s2", TurnID: "turn-1", Seq: 2, Kind: "span.model_request_end", Payload: spanPayload(50000, 2000, 52000)},
	}); err != nil {
		t.Fatalf("append spans: %v", err)
	}

	usage, err = reader.LatestUsage(ctx, "ws", "sess-1")
	if err != nil {
		t.Fatalf("LatestUsage: %v", err)
	}
	if usage == nil {
		t.Fatal("usage = nil, want the aggregated span numbers")
	}
	if usage.InputTokens != 80000 || usage.OutputTokens != 3500 || usage.TotalTokens != 83500 {
		t.Fatalf("usage = %+v, want in=80000 out=3500 total=83500", usage)
	}
	if usage.FinalInputTokens != 50000 {
		t.Fatalf("final input = %d, want the last call's 50000", usage.FinalInputTokens)
	}
	if usage.ContextBreakdown != nil {
		t.Fatalf("context_breakdown = %+v, want nil — spans never fabricate segments", usage.ContextBreakdown)
	}
}
