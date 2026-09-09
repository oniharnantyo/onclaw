package agents

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// reasoningFrame builds an AgenticMessage carrying one reasoning chunk.
func reasoningFrame(text string) *schema.AgenticMessage {
	return &schema.AgenticMessage{
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.Reasoning{Text: text}),
		},
	}
}

// agenticStreamEvent wraps AgenticMessage frames in a streaming message-output
// agent event, mirroring how the ADK runner delivers model streams.
func agenticStreamEvent(frames ...*schema.AgenticMessage) *adk.TypedAgentEvent[*schema.AgenticMessage] {
	return &adk.TypedAgentEvent[*schema.AgenticMessage]{
		Output: &adk.TypedAgentOutput[*schema.AgenticMessage]{
			MessageOutput: &adk.TypedMessageVariant[*schema.AgenticMessage]{
				IsStreaming:   true,
				AgenticRole:   schema.AgenticRoleTypeAssistant,
				MessageStream: schema.StreamReaderFromArray(frames),
			},
		},
	}
}

func filterEvents(events []TranscriptEvent, kind TranscriptEventKind) []TranscriptEvent {
	var out []TranscriptEvent
	for _, ev := range events {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

func TestDrainAgentEvents_StreamReasoningDeltasBeforeCompletedMessage(t *testing.T) {
	frames := []*schema.AgenticMessage{
		reasoningFrame("Let me"),
		reasoningFrame(" think"),
		{
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.Reasoning{Text: " it through"}),
				schema.NewContentBlock(&schema.AssistantGenText{Text: "Answer"}),
			},
		},
	}
	events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
		gen.Send(agenticStreamEvent(frames...))
	})

	deltas := filterEvents(events, TranscriptEventReasoningDelta)
	if len(deltas) != 3 {
		t.Fatalf("reasoning deltas = %d, want 3: %+v", len(deltas), deltas)
	}
	var got strings.Builder
	for _, d := range deltas {
		got.WriteString(d.ReasoningDelta)
	}
	if got.String() != "Let me think it through" {
		t.Fatalf("reasoning = %q, want %q", got.String(), "Let me think it through")
	}

	completed := filterEvents(events, TranscriptEventMessageCompleted)
	if len(completed) != 1 {
		t.Fatalf("completed messages = %d, want 1", len(completed))
	}
	msg := completed[0].Message
	if msg.Content != "Answer" {
		t.Fatalf("content = %q, want %q (text and reasoning must stay distinct)", msg.Content, "Answer")
	}
	if msg.ReasoningContent != "Let me think it through" {
		t.Fatalf("reasoning_content = %q, want %q", msg.ReasoningContent, "Let me think it through")
	}

	// Every reasoning delta precedes the completed message.
	completedIdx := indexOfKind(events, TranscriptEventMessageCompleted)
	for i, ev := range events {
		if ev.Kind == TranscriptEventReasoningDelta && i > completedIdx {
			t.Fatalf("reasoning delta at %d after completed message at %d", i, completedIdx)
		}
	}

	// Reasoning never leaks into text deltas.
	for _, ev := range filterEvents(events, TranscriptEventTextDelta) {
		if strings.Contains(ev.TextDelta, "Let me") {
			t.Fatalf("reasoning leaked into text delta: %q", ev.TextDelta)
		}
	}
}

func TestDrainAgentEvents_NonStreamingReasoningEmittedOnce(t *testing.T) {
	msg := &schema.AgenticMessage{
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.Reasoning{Text: "deliberate"}),
			schema.NewContentBlock(&schema.AssistantGenText{Text: "final"}),
		},
	}
	events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
			Output: &adk.TypedAgentOutput[*schema.AgenticMessage]{
				MessageOutput: &adk.TypedMessageVariant[*schema.AgenticMessage]{
					AgenticRole: schema.AgenticRoleTypeAssistant,
					Message:     msg,
				},
			},
		})
	})

	deltas := filterEvents(events, TranscriptEventReasoningDelta)
	if len(deltas) != 1 || deltas[0].ReasoningDelta != "deliberate" {
		t.Fatalf("reasoning deltas = %+v, want one 'deliberate'", deltas)
	}
	completed := filterEvents(events, TranscriptEventMessageCompleted)
	if len(completed) != 1 || completed[0].Message.ReasoningContent != "deliberate" {
		t.Fatalf("completed = %+v, want reasoning_content 'deliberate'", completed)
	}
	// The single reasoning delta precedes the completed message.
	if indexOfKind(events, TranscriptEventReasoningDelta) > indexOfKind(events, TranscriptEventMessageCompleted) {
		t.Fatal("reasoning delta must precede the completed message")
	}
}

func indexOfKind(events []TranscriptEvent, kind TranscriptEventKind) int {
	for i, ev := range events {
		if ev.Kind == kind {
			return i
		}
	}
	return -1
}

// sendSpanToolCall emits a tool-call span session event of the given kind.
func sendSpanToolCall(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]], kind adk.SessionEventKind, callID string) {
	gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
		SessionEventVariant: &adk.SessionEventVariant[*schema.AgenticMessage]{
			Event: &adk.SessionEvent[*schema.AgenticMessage]{
				Kind:   kind,
				TurnID: "turn-1",
				Span: &adk.SpanEvent{
					Kind: adk.SpanKindTool,
					Tool: &adk.ToolSpanMeta{ToolUseID: callID, Name: "files.list"},
				},
			},
		},
	})
}

func TestDrainAgentEvents_SpanToolLatencyStamped(t *testing.T) {
	events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
		sendSpanToolCall(gen, adk.SessionEventSpanToolCallStart, "call-1")
		time.Sleep(2 * time.Millisecond)
		sendSpanToolCall(gen, adk.SessionEventSpanToolCallEnd, "call-1")
	})

	finished := filterEvents(events, TranscriptEventToolCallFinished)
	if len(finished) != 1 {
		t.Fatalf("tool_call_finished = %d, want 1", len(finished))
	}
	if finished[0].ToolResult == nil || finished[0].ToolResult.Latency <= 0 {
		t.Fatalf("latency = %+v, want > 0", finished[0].ToolResult)
	}
}

func TestDrainAgentEvents_MessageDrivenToolLatencyStamped(t *testing.T) {
	call := &schema.AgenticMessage{
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.FunctionToolCall{CallID: "call-2", Name: "files.list", Arguments: "{}"}),
		},
	}
	result := &schema.AgenticMessage{
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.FunctionToolResult{
				CallID: "call-2",
				Name:   "files.list",
				Content: []*schema.FunctionToolResultContentBlock{
					{Text: &schema.UserInputText{Text: "a.txt"}},
				},
			}),
		},
	}
	events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
		gen.Send(agenticStreamEvent(call))
		time.Sleep(2 * time.Millisecond)
		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
			Output: &adk.TypedAgentOutput[*schema.AgenticMessage]{
				MessageOutput: &adk.TypedMessageVariant[*schema.AgenticMessage]{
					AgenticRole: schema.AgenticRoleTypeUser,
					Message:     result,
				},
			},
		})
	})

	finished := filterEvents(events, TranscriptEventToolCallFinished)
	if len(finished) != 1 {
		t.Fatalf("tool_call_finished = %d, want 1", len(finished))
	}
	if finished[0].ToolResult == nil || finished[0].ToolResult.Latency <= 0 {
		t.Fatalf("latency = %+v, want > 0", finished[0].ToolResult)
	}
}
