package agents

import (
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// TestHistory_HydrationFidelity verifies that a persisted session hydrates
// with the same fields the live stream delivered: tool arguments, tool
// results, latency from the span pair, and reasoning on assistant messages
// (design D4).
func TestHistory_HydrationFidelity(t *testing.T) {
	st, runner, ctx := setupHistoryTest(t)
	ws, ag := createTestWorkspaceAndAgent(t, ctx, st, "ws-fid", "ag-fid")
	sessionID := "sess-fid-1"

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)

	t1 := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 9, 4, 10, 0, 1, 0, time.UTC)
	t3 := time.Date(2026, 9, 4, 10, 0, 4, 0, time.UTC)
	t4 := time.Date(2026, 9, 4, 10, 0, 5, 0, time.UTC)

	events := []*adk.SessionEvent[*schema.AgenticMessage]{
		// User message.
		{
			EventID:   "fid-user",
			TurnID:    "turn-fid",
			Timestamp: t1,
			Message:   schema.UserAgenticMessage("list the files"),
		},
		// Assistant message requesting the tool call: reasoning + tool call
		// block carrying call id and arguments. Joins to the spans via
		// AssistantMessageEventID.
		{
			EventID:   "fid-assistant-1",
			TurnID:    "turn-fid",
			Timestamp: t1,
			Message: &schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.Reasoning{Text: "I should list the files first."}),
					schema.NewContentBlock(&schema.FunctionToolCall{
						CallID:    "call-fid-1",
						Name:      "list_files",
						Arguments: `{"path":"/tmp"}`,
					}),
				},
			},
		},
		// Tool call start span.
		{
			EventID:   "fid-tool-start",
			TurnID:    "turn-fid",
			Timestamp: t2,
			Kind:      adk.SessionEventSpanToolCallStart,
			Span: &adk.SpanEvent{
				Kind:      adk.SpanKindTool,
				StartedAt: t2,
				Tool: &adk.ToolSpanMeta{
					ToolUseID:               "call-fid-1",
					Name:                    "list_files",
					AssistantMessageEventID: "fid-assistant-1",
				},
			},
		},
		// Tool result message. Joins to the end span via
		// ToolResultMessageEventID.
		{
			EventID:   "fid-tool-result",
			TurnID:    "turn-fid",
			Timestamp: t3,
			Message: &schema.AgenticMessage{
				Role: schema.AgenticRoleType("tool"),
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.FunctionToolResult{
						CallID: "call-fid-1",
						Name:   "list_files",
						Content: []*schema.FunctionToolResultContentBlock{
							{Type: schema.FunctionToolResultContentBlockTypeText, Text: &schema.UserInputText{Text: "a.txt\nb.txt"}},
						},
					}),
				},
			},
		},
		// Tool call end span.
		{
			EventID:   "fid-tool-end",
			TurnID:    "turn-fid",
			Timestamp: t3,
			Kind:      adk.SessionEventSpanToolCallEnd,
			Span: &adk.SpanEvent{
				Kind:      adk.SpanKindTool,
				StartedAt: t2,
				EndedAt:   t3,
				Status:    "ok",
				Tool: &adk.ToolSpanMeta{
					ToolUseID:                "call-fid-1",
					Name:                     "list_files",
					ToolCallStartEventID:     "fid-tool-start",
					AssistantMessageEventID:  "fid-assistant-1",
					ToolResultMessageEventID: "fid-tool-result",
				},
			},
		},
		// Final assistant message with reasoning + text.
		{
			EventID:   "fid-assistant-2",
			TurnID:    "turn-fid",
			Timestamp: t4,
			Message: &schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.Reasoning{Text: "Two files found."}),
					schema.NewContentBlock(&schema.AssistantGenText{Text: "Found 2 files."}),
				},
			},
		},
	}

	if err := adapter.AppendEvents(ctx, sessionID, events); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	res, err := runner.History(ctx, HistoryRequest{
		WorkspaceID: ws.ID,
		AgentID:     ag.ID,
		SessionID:   sessionID,
	})
	if err != nil {
		t.Fatalf("History failed: %v", err)
	}

	var started, finished, assistant2 *TranscriptEvent
	for i := range res.Events {
		switch res.Events[i].Kind {
		case TranscriptEventToolCallStarted:
			if started == nil {
				started = &res.Events[i]
			}
		case TranscriptEventToolCallFinished:
			if finished == nil {
				finished = &res.Events[i]
			}
		case TranscriptEventMessageCompleted:
			if res.Events[i].Message != nil && res.Events[i].Message.Role == "assistant" && res.Events[i].Message.Content == "Found 2 files." {
				assistant2 = &res.Events[i]
			}
		}
	}
	if started == nil || finished == nil || assistant2 == nil {
		t.Fatalf("missing projected events: started=%v finished=%v assistant2=%v (events: %+v)", started != nil, finished != nil, assistant2 != nil, res.Events)
	}

	// tool_call_started carries the arguments joined via AssistantMessageEventID.
	if started.ToolCall == nil {
		t.Fatalf("started.ToolCall is nil")
	}
	if started.ToolCall.CallID != "call-fid-1" || started.ToolCall.Name != "list_files" {
		t.Errorf("started ToolCall identity mismatch: %+v", started.ToolCall)
	}
	if started.ToolCall.Arguments != `{"path":"/tmp"}` {
		t.Errorf("started ToolCall.Arguments got %q, want %q", started.ToolCall.Arguments, `{"path":"/tmp"}`)
	}

	// tool_call_finished carries the result joined via ToolResultMessageEventID
	// plus latency from the span pair's timestamps (t2 → t3 = 3s).
	if finished.ToolResult == nil {
		t.Fatalf("finished.ToolResult is nil")
	}
	if finished.ToolResult.CallID != "call-fid-1" || finished.ToolResult.Name != "list_files" {
		t.Errorf("finished ToolResult identity mismatch: %+v", finished.ToolResult)
	}
	if finished.ToolResult.Result != "a.txt\nb.txt" {
		t.Errorf("finished ToolResult.Result got %q, want %q", finished.ToolResult.Result, "a.txt\nb.txt")
	}
	if finished.ToolResult.IsError {
		t.Errorf("finished ToolResult.IsError got true, want false")
	}
	if want := 3 * time.Second; finished.ToolResult.Latency != want {
		t.Errorf("finished ToolResult.Latency got %v, want %v", finished.ToolResult.Latency, want)
	}

	// The completed assistant message carries reasoning extracted from its
	// persisted reasoning blocks.
	if assistant2.Message.ReasoningContent != "Two files found." {
		t.Errorf("assistant Message.ReasoningContent got %q, want %q", assistant2.Message.ReasoningContent, "Two files found.")
	}
}

// TestHistory_HydrationFallbacks verifies the call-id fallbacks: spans without
// join IDs still resolve arguments and results, and an errored span without a
// result message reports the span error with IsError set.
func TestHistory_HydrationFallbacks(t *testing.T) {
	st, runner, ctx := setupHistoryTest(t)
	ws, _ := createTestWorkspaceAndAgent(t, ctx, st, "ws-fid-fb", "ag-fid-fb")
	sessionID := "sess-fid-fb"

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)

	t1 := time.Date(2026, 9, 4, 11, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 9, 4, 11, 0, 2, 0, time.UTC)
	t3 := time.Date(2026, 9, 4, 11, 0, 3, 0, time.UTC)
	t4 := time.Date(2026, 9, 4, 11, 0, 5, 0, time.UTC)

	events := []*adk.SessionEvent[*schema.AgenticMessage]{
		// Assistant message with a tool call block — no span carries its event
		// id, so argument resolution must fall back to call-id matching.
		{
			EventID:   "fb-assistant",
			TurnID:    "turn-fb",
			Timestamp: t1,
			Message: &schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.FunctionToolCall{
						CallID:    "call-fb-1",
						Name:      "shell",
						Arguments: `{"cmd":"ls"}`,
					}),
				},
			},
		},
		// Start span with no AssistantMessageEventID.
		{
			EventID:   "fb-tool-start",
			TurnID:    "turn-fb",
			Timestamp: t2,
			Kind:      adk.SessionEventSpanToolCallStart,
			Span: &adk.SpanEvent{
				Kind:      adk.SpanKindTool,
				StartedAt: t2,
				Tool: &adk.ToolSpanMeta{
					ToolUseID: "call-fb-1",
					Name:      "shell",
				},
			},
		},
		// Tool result message — no join ID on the end span either, so result
		// resolution must fall back to call-id matching.
		{
			EventID:   "fb-tool-result",
			TurnID:    "turn-fb",
			Timestamp: t3,
			Message: &schema.AgenticMessage{
				Role: schema.AgenticRoleType("tool"),
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.FunctionToolResult{
						CallID: "call-fb-1",
						Name:   "shell",
						Content: []*schema.FunctionToolResultContentBlock{
							{Type: schema.FunctionToolResultContentBlockTypeText, Text: &schema.UserInputText{Text: "exit 0"}},
						},
					}),
				},
			},
		},
		// End span: no join IDs, StartedAt snapshot only — latency falls back
		// to the start span located via ToolUseID.
		{
			EventID:   "fb-tool-end",
			TurnID:    "turn-fb",
			Timestamp: t3,
			Kind:      adk.SessionEventSpanToolCallEnd,
			Span: &adk.SpanEvent{
				Kind:    adk.SpanKindTool,
				EndedAt: t3,
				Status:  "ok",
				Tool: &adk.ToolSpanMeta{
					ToolUseID: "call-fb-1",
					Name:      "shell",
				},
			},
		},
		// A second, errored tool call with no result message.
		{
			EventID:   "fb-err-assistant",
			TurnID:    "turn-fb",
			Timestamp: t4,
			Message: &schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.FunctionToolCall{
						CallID:    "call-fb-2",
						Name:      "shell",
						Arguments: `{"cmd":"boom"}`,
					}),
				},
			},
		},
		{
			EventID:   "fb-err-end",
			TurnID:    "turn-fb",
			Timestamp: t4,
			Kind:      adk.SessionEventSpanToolCallEnd,
			Span: &adk.SpanEvent{
				Kind:    adk.SpanKindTool,
				EndedAt: t4,
				Status:  "error",
				Err:     "shell: exit status 1",
				Tool: &adk.ToolSpanMeta{
					ToolUseID: "call-fb-2",
					Name:      "shell",
				},
			},
		},
	}

	if err := adapter.AppendEvents(ctx, sessionID, events); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	res, err := runner.History(ctx, HistoryRequest{
		WorkspaceID: ws.ID,
		SessionID:   sessionID,
	})
	if err != nil {
		t.Fatalf("History failed: %v", err)
	}

	var started, finished, errored *TranscriptEvent
	for i := range res.Events {
		switch {
		case res.Events[i].Kind == TranscriptEventToolCallStarted && started == nil:
			started = &res.Events[i]
		case res.Events[i].Kind == TranscriptEventToolCallFinished && finished == nil:
			finished = &res.Events[i]
		case res.Events[i].Kind == TranscriptEventToolCallFinished && res.Events[i].ToolResult != nil && res.Events[i].ToolResult.CallID == "call-fb-2":
			errored = &res.Events[i]
		}
	}
	if started == nil || finished == nil || errored == nil {
		t.Fatalf("missing projected events (started=%v finished=%v errored=%v)", started != nil, finished != nil, errored != nil)
	}

	// Call-id fallback resolves the arguments without a span join ID.
	if started.ToolCall.Arguments != `{"cmd":"ls"}` {
		t.Errorf("fallback started Arguments got %q, want %q", started.ToolCall.Arguments, `{"cmd":"ls"}`)
	}
	// Call-id fallback resolves the result, and latency comes from the start
	// span joined by ToolUseID (t2 → t3 = 1s).
	if finished.ToolResult.Result != "exit 0" {
		t.Errorf("fallback finished Result got %q, want %q", finished.ToolResult.Result, "exit 0")
	}
	if want := time.Second; finished.ToolResult.Latency != want {
		t.Errorf("fallback finished Latency got %v, want %v", finished.ToolResult.Latency, want)
	}
	// Errored span without a result message reports the span error.
	if !errored.ToolResult.IsError {
		t.Errorf("errored ToolResult.IsError got false, want true")
	}
	if errored.ToolResult.Result != "shell: exit status 1" {
		t.Errorf("errored ToolResult.Result got %q, want %q", errored.ToolResult.Result, "shell: exit status 1")
	}
}
