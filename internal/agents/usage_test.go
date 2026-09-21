package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agents/hooks"
)

// sendSpanModelEnd emits a model-request-end session event carrying the given
// provider usage onto the generator.
func sendSpanModelEnd(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]], in, out, total int) {
	gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
		SessionEventVariant: &adk.SessionEventVariant[*schema.AgenticMessage]{
			Event: &adk.SessionEvent[*schema.AgenticMessage]{
				Kind:   adk.SessionEventSpanModelRequestEnd,
				TurnID: "turn-1",
				Span: &adk.SpanEvent{
					Kind: adk.SpanKindModel,
					Model: &adk.ModelSpanMeta{
						Usage: &adk.ModelUsage{
							InputTokens:  in,
							OutputTokens: out,
							Raw:          &schema.TokenUsage{PromptTokens: in, CompletionTokens: out, TotalTokens: total},
						},
					},
				},
			},
		},
	})
}

func drainUsageEvents(t *testing.T, send func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]])) []TranscriptEvent {
	t.Helper()
	iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	go func() {
		send(gen)
		gen.Close()
	}()

	stream := NewEventStream(16)
	// drainAgentEvents fans each event out via the manager's Broadcast; a
	// manager with no live runs makes that a no-op for this unit test. The
	// nil hook chain disables the run_finished terminal seam here. The
	// ephemeral session store is the in-memory stand-in every run carries —
	// the turn-end breakdown measurement replays it.
	r := &Runner{runMgr: newRunManager(context.Background(), 0), memoryWorker: newQueuedMemoryWorker()}
	r.drainAgentEvents(t.Context(), iter, stream, RunKey{}, "turn-1", "", nil, hooks.Event{}, &compactionState{}, nil, nil, nil, ExecRequest{}, false, NewEphemeralSessionAdapter())
	// drainAgentEvents returns after the terminal event; the caller (streamRun
	// in production) closes the stream.
	stream.Close()

	var events []TranscriptEvent
	for {
		ev, err := stream.Recv()
		if err != nil {
			break
		}
		events = append(events, *ev)
	}
	return events
}

func terminalEvent(events []TranscriptEvent) *TranscriptEvent {
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Kind {
		case TranscriptEventTurnCompleted, TranscriptEventError, TranscriptEventCancelled:
			return &events[i]
		}
	}
	return nil
}

func TestUsageStampedOnCompletedTurn(t *testing.T) {
	events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
		sendSpanModelEnd(gen, 30, 20, 50)
		sendSpanModelEnd(gen, 10, 5, 15)
	})

	terminal := terminalEvent(events)
	if terminal == nil || terminal.Kind != TranscriptEventTurnCompleted {
		t.Fatalf("terminal event = %+v, want turn_completed", terminal)
	}
	if terminal.Usage == nil {
		t.Fatal("terminal usage is nil")
	}
	// Accumulated across both model requests of the turn.
	if terminal.Usage.InputTokens != 40 || terminal.Usage.OutputTokens != 25 || terminal.Usage.TotalTokens != 65 {
		t.Fatalf("usage = %+v, want in=40 out=25 total=65", terminal.Usage)
	}
}

func TestUsageStampedOnErrorTurn(t *testing.T) {
	events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
		sendSpanModelEnd(gen, 12, 0, 12)
		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
			Err: errors.New("model exploded"),
		})
	})

	terminal := terminalEvent(events)
	if terminal == nil || terminal.Kind != TranscriptEventError {
		t.Fatalf("terminal event = %+v, want error", terminal)
	}
	if terminal.Usage == nil || terminal.Usage.InputTokens != 12 {
		t.Fatalf("usage = %+v, want in=12", terminal.Usage)
	}
}

func TestUsageAbsentWhenProviderReportsNone(t *testing.T) {
	events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
			SessionEventVariant: &adk.SessionEventVariant[*schema.AgenticMessage]{
				Event: &adk.SessionEvent[*schema.AgenticMessage]{
					Kind:      adk.SessionEventMessage,
					TurnID:    "turn-1",
					Timestamp: time.Now(),
					Message:   schema.UserAgenticMessage("hi"),
				},
			},
		})
	})

	terminal := terminalEvent(events)
	if terminal == nil || terminal.Kind != TranscriptEventTurnCompleted {
		t.Fatalf("terminal event = %+v, want turn_completed", terminal)
	}
	if terminal.Usage != nil {
		t.Fatalf("usage = %+v, want nil when the provider reports none", terminal.Usage)
	}
}

func TestUsageOf_NilOnAllZeroPayload(t *testing.T) {
	if got := usageOf(UsagePayload{}); got != nil {
		t.Fatalf("usageOf(all-zero) = %+v, want nil", got)
	}
}

// usageFrame builds a streaming frame carrying text and the call's token
// usage (providers report usage on the final frame of a stream).
func usageFrame(text string, prompt, completion, total int) *schema.AgenticMessage {
	return &schema.AgenticMessage{
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: text}),
		},
		ResponseMeta: &schema.AgenticResponseMeta{
			TokenUsage: &schema.TokenUsage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total},
		},
	}
}

func TestUsage_FinalCallInputOnMultiCallSpanTurn(t *testing.T) {
	events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
		sendSpanModelEnd(gen, 85000, 1000, 86000)
		sendSpanModelEnd(gen, 50000, 2000, 52000)
	})

	terminal := terminalEvent(events)
	if terminal == nil || terminal.Usage == nil {
		t.Fatalf("terminal = %+v, want usage-bearing terminal event", terminal)
	}
	// Input sums every call for billing; final-call input is the last call's.
	if terminal.Usage.InputTokens != 135000 {
		t.Fatalf("input = %d, want 135000", terminal.Usage.InputTokens)
	}
	if terminal.Usage.FinalInputTokens != 50000 {
		t.Fatalf("final input = %d, want 50000", terminal.Usage.FinalInputTokens)
	}
}

func TestUsage_FinalCallInputEqualsTotalOnSingleCallTurn(t *testing.T) {
	events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
		sendSpanModelEnd(gen, 30000, 1500, 31500)
	})

	terminal := terminalEvent(events)
	if terminal == nil || terminal.Usage == nil {
		t.Fatalf("terminal = %+v, want usage-bearing terminal event", terminal)
	}
	if terminal.Usage.InputTokens != 30000 || terminal.Usage.FinalInputTokens != 30000 {
		t.Fatalf("input = %d final = %d, want both 30000", terminal.Usage.InputTokens, terminal.Usage.FinalInputTokens)
	}
}

func TestUsage_FinalCallInputOnMultiCallStreamingTurn(t *testing.T) {
	events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
		gen.Send(agenticStreamEvent(
			usageFrame("first ", 40000, 500, 40500),
			usageFrame("call", 85000, 1000, 86000),
		))
		gen.Send(agenticStreamEvent(
			usageFrame("final ", 45000, 800, 45800),
			usageFrame("call", 50000, 2000, 52000),
		))
	})

	terminal := terminalEvent(events)
	if terminal == nil || terminal.Usage == nil {
		t.Fatalf("terminal = %+v, want usage-bearing terminal event", terminal)
	}
	// Only each stream's final frame carries usage; summed across both calls,
	// and the chronologically last call's input wins the final-call slot.
	if terminal.Usage.InputTokens != 135000 {
		t.Fatalf("input = %d, want 135000", terminal.Usage.InputTokens)
	}
	if terminal.Usage.FinalInputTokens != 50000 {
		t.Fatalf("final input = %d, want 50000", terminal.Usage.FinalInputTokens)
	}
}
