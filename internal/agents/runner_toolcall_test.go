package agents

import (
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// agenticMessageEvent delivers a complete AgenticMessage as a non-streaming
// message-output agent event, mirroring how the ADK runner delivers finished
// model and tool messages.
func agenticMessageEvent(msg *schema.AgenticMessage, role schema.AgenticRoleType) *adk.TypedAgentEvent[*schema.AgenticMessage] {
	return &adk.TypedAgentEvent[*schema.AgenticMessage]{
		Output: &adk.TypedAgentOutput[*schema.AgenticMessage]{
			MessageOutput: &adk.TypedMessageVariant[*schema.AgenticMessage]{
				AgenticRole: role,
				Message:     msg,
			},
		},
	}
}

// toolCallMessage builds an AgenticMessage carrying one FunctionToolCall
// block per given call.
func toolCallMessage(calls ...schema.FunctionToolCall) *schema.AgenticMessage {
	blocks := make([]*schema.ContentBlock, 0, len(calls))
	for i := range calls {
		blocks = append(blocks, schema.NewContentBlock(&calls[i]))
	}
	return &schema.AgenticMessage{ContentBlocks: blocks}
}

// sendNamedSpanToolCall emits a tool-call span session event of the given kind
// with an explicit tool name.
func sendNamedSpanToolCall(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]], kind adk.SessionEventKind, callID, name string) {
	gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
		SessionEventVariant: &adk.SessionEventVariant[*schema.AgenticMessage]{
			Event: &adk.SessionEvent[*schema.AgenticMessage]{
				Kind:   kind,
				TurnID: "turn-1",
				Span: &adk.SpanEvent{
					Kind: adk.SpanKindTool,
					Tool: &adk.ToolSpanMeta{ToolUseID: callID, Name: name},
				},
			},
		},
	})
}

// TestDrainAgentEvents_ParallelToolCallsStartedOnceWithArgs pins the
// cross-lane dedupe of a parallel tool-call round: the assistant message
// carrying the calls wins the starts, so the span lane's starts for the same
// call IDs are no-ops — exactly one started per call carrying that call's
// arguments, and one finished per call (fix-duplicate-tool-call-cards D1).
func TestDrainAgentEvents_ParallelToolCallsStartedOnceWithArgs(t *testing.T) {
	type wantCall struct {
		name string
		args string
	}
	want := map[string]wantCall{
		"call-execute":   {"execute", `{"command":"ls -la"}`},
		"call-websearch": {"web.search", `{"query":"onclaw release notes"}`},
	}

	events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
		gen.Send(agenticMessageEvent(toolCallMessage(
			schema.FunctionToolCall{CallID: "call-execute", Name: "execute", Arguments: `{"command":"ls -la"}`},
			schema.FunctionToolCall{CallID: "call-websearch", Name: "web.search", Arguments: `{"query":"onclaw release notes"}`},
		), schema.AgenticRoleTypeAssistant))
		sendNamedSpanToolCall(gen, adk.SessionEventSpanToolCallStart, "call-execute", "execute")
		sendNamedSpanToolCall(gen, adk.SessionEventSpanToolCallEnd, "call-execute", "execute")
		sendNamedSpanToolCall(gen, adk.SessionEventSpanToolCallStart, "call-websearch", "web.search")
		sendNamedSpanToolCall(gen, adk.SessionEventSpanToolCallEnd, "call-websearch", "web.search")
	})

	started := filterEvents(events, TranscriptEventToolCallStarted)
	if len(started) != len(want) {
		t.Fatalf("tool_call_started events = %d, want %d: %+v", len(started), len(want), started)
	}
	for _, ev := range started {
		if ev.ToolCall == nil {
			t.Fatalf("tool_call_started without payload: %+v", ev)
		}
		call, ok := want[ev.ToolCall.CallID]
		if !ok {
			t.Fatalf("unexpected tool_call_started for call %q", ev.ToolCall.CallID)
		}
		if ev.ToolCall.Name != call.name {
			t.Fatalf("call %s name = %q, want %q", ev.ToolCall.CallID, ev.ToolCall.Name, call.name)
		}
		if ev.ToolCall.Arguments != call.args {
			t.Fatalf("call %s arguments = %q, want %q", ev.ToolCall.CallID, ev.ToolCall.Arguments, call.args)
		}
		delete(want, ev.ToolCall.CallID)
	}
	if len(want) != 0 {
		t.Fatalf("missing tool_call_started for calls: %v", want)
	}

	finished := filterEvents(events, TranscriptEventToolCallFinished)
	if len(finished) != 2 {
		t.Fatalf("tool_call_finished events = %d, want 2: %+v", len(finished), finished)
	}
	seen := map[string]bool{}
	for _, ev := range finished {
		if ev.ToolResult == nil {
			t.Fatalf("tool_call_finished without payload: %+v", ev)
		}
		if seen[ev.ToolResult.CallID] {
			t.Fatalf("duplicate tool_call_finished for call %q", ev.ToolResult.CallID)
		}
		seen[ev.ToolResult.CallID] = true
	}
}

// TestDrainAgentEvents_SpanToolCallStartJoinsArgsFromStash covers the span
// lane's start emission across the orderings that can occur against the
// message lane (fix-duplicate-tool-call-cards D1).
func TestDrainAgentEvents_SpanToolCallStartJoinsArgsFromStash(t *testing.T) {
	t.Run("message before span start keeps the message arguments", func(t *testing.T) {
		events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
			gen.Send(agenticMessageEvent(toolCallMessage(
				schema.FunctionToolCall{CallID: "call-1", Name: "files.list", Arguments: `{"path":"docs"}`},
			), schema.AgenticRoleTypeAssistant))
			sendNamedSpanToolCall(gen, adk.SessionEventSpanToolCallStart, "call-1", "files.list")
			sendNamedSpanToolCall(gen, adk.SessionEventSpanToolCallEnd, "call-1", "files.list")
		})

		started := filterEvents(events, TranscriptEventToolCallStarted)
		if len(started) != 1 {
			t.Fatalf("tool_call_started events = %d, want 1: %+v", len(started), started)
		}
		if started[0].ToolCall == nil {
			t.Fatalf("tool_call_started without payload: %+v", started[0])
		}
		if started[0].ToolCall.Arguments != `{"path":"docs"}` {
			t.Fatalf("arguments = %q, want message args %q", started[0].ToolCall.Arguments, `{"path":"docs"}`)
		}
	})

	t.Run("streaming frame before span start keeps the frame arguments", func(t *testing.T) {
		events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
			gen.Send(agenticStreamEvent(toolCallMessage(
				schema.FunctionToolCall{CallID: "call-1", Name: "files.list", Arguments: `{"path":"src"}`},
			)))
			sendNamedSpanToolCall(gen, adk.SessionEventSpanToolCallStart, "call-1", "files.list")
			sendNamedSpanToolCall(gen, adk.SessionEventSpanToolCallEnd, "call-1", "files.list")
		})

		started := filterEvents(events, TranscriptEventToolCallStarted)
		if len(started) != 1 {
			t.Fatalf("tool_call_started events = %d, want 1: %+v", len(started), started)
		}
		if started[0].ToolCall == nil {
			t.Fatalf("tool_call_started without payload: %+v", started[0])
		}
		if started[0].ToolCall.Arguments != `{"path":"src"}` {
			t.Fatalf("arguments = %q, want frame args %q", started[0].ToolCall.Arguments, `{"path":"src"}`)
		}
	})

	t.Run("span start without message degrades to empty args", func(t *testing.T) {
		events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
			sendSpanToolCall(gen, adk.SessionEventSpanToolCallStart, "call-1")
			sendSpanToolCall(gen, adk.SessionEventSpanToolCallEnd, "call-1")
		})

		started := filterEvents(events, TranscriptEventToolCallStarted)
		if len(started) != 1 {
			t.Fatalf("tool_call_started events = %d, want 1: %+v", len(started), started)
		}
		if started[0].ToolCall == nil {
			t.Fatalf("tool_call_started without payload: %+v", started[0])
		}
		if started[0].ToolCall.Arguments != "" {
			t.Fatalf("arguments = %q, want empty (no message carried this call; must not fabricate args)", started[0].ToolCall.Arguments)
		}
		finished := filterEvents(events, TranscriptEventToolCallFinished)
		if len(finished) != 1 {
			t.Fatalf("tool_call_finished events = %d, want 1", len(finished))
		}
	})

	t.Run("empty frame args never phantom-join", func(t *testing.T) {
		// A message whose call carries empty arguments still wins the start
		// (with empty args); the later span start must dedupe and the stash —
		// which only records non-empty args — must not fabricate any.
		events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
			gen.Send(agenticMessageEvent(toolCallMessage(
				schema.FunctionToolCall{CallID: "call-1", Name: "files.list"},
			), schema.AgenticRoleTypeAssistant))
			sendNamedSpanToolCall(gen, adk.SessionEventSpanToolCallStart, "call-1", "files.list")
			sendNamedSpanToolCall(gen, adk.SessionEventSpanToolCallEnd, "call-1", "files.list")
		})

		started := filterEvents(events, TranscriptEventToolCallStarted)
		if len(started) != 1 {
			t.Fatalf("tool_call_started events = %d, want 1: %+v", len(started), started)
		}
		if started[0].ToolCall == nil {
			t.Fatalf("tool_call_started without payload: %+v", started[0])
		}
		if started[0].ToolCall.Arguments != "" {
			t.Fatalf("arguments = %q, want empty (empty frame args must not be joined)", started[0].ToolCall.Arguments)
		}
	})
}

// TestDrainAgentEvents_DuplicateSpanToolCallStartEmitsOnce pins the span
// lane's own dedupe guard: two span starts for the same call ID — as a future
// lane collision would produce — emit exactly one started event
// (fix-duplicate-tool-call-cards D1).
func TestDrainAgentEvents_DuplicateSpanToolCallStartEmitsOnce(t *testing.T) {
	events := drainUsageEvents(t, func(gen *adk.AsyncGenerator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
		sendNamedSpanToolCall(gen, adk.SessionEventSpanToolCallStart, "call-1", "files.list")
		sendNamedSpanToolCall(gen, adk.SessionEventSpanToolCallStart, "call-1", "files.list")
		sendNamedSpanToolCall(gen, adk.SessionEventSpanToolCallEnd, "call-1", "files.list")
	})

	started := filterEvents(events, TranscriptEventToolCallStarted)
	if len(started) != 1 {
		t.Fatalf("tool_call_started events = %d, want 1: %+v", len(started), started)
	}
	if started[0].ToolCall == nil || started[0].ToolCall.CallID != "call-1" {
		t.Fatalf("tool_call_started payload = %+v, want call-1", started[0].ToolCall)
	}
	finished := filterEvents(events, TranscriptEventToolCallFinished)
	if len(finished) != 1 {
		t.Fatalf("tool_call_finished events = %d, want 1", len(finished))
	}
}
