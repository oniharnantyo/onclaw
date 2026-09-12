package openresponses

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
)

func TestResponseIDRoundTrip(t *testing.T) {
	id := MintResponseID("sess_abc-123", "9f0c1a2e-1111-2222-3333-444455556666")
	session, turn, err := DecodeResponseID(id)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if session != "sess_abc-123" || turn != "9f0c1a2e-1111-2222-3333-444455556666" {
		t.Fatalf("got session=%q turn=%q", session, turn)
	}

	for _, bad := range []string{"", "resp_", "garbage", "resp_nounderscore", "resp__tail"} {
		if _, _, err := DecodeResponseID(bad); err == nil {
			t.Errorf("DecodeResponseID(%q) should fail", bad)
		}
	}
}

func TestFlattenInput(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		fail bool
	}{
		{"string", `"hello"`, "hello", false},
		{"parts", `[{"type":"message","role":"user","content":[{"type":"input_text","text":"a"},{"type":"input_text","text":"b"}]}]`, "a\nb", false},
		{"string content", `[{"type":"message","role":"user","content":"plain"}]`, "plain", false},
		{"image part", `[{"type":"message","content":[{"type":"input_image"}]}]`, "", true},
	}
	for _, tc := range cases {
		got, err := FlattenInput([]byte(tc.in))
		if tc.fail {
			if err == nil {
				t.Errorf("%s: expected error", tc.name)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q err %v, want %q", tc.name, got, err, tc.want)
		}
	}
}

func collect(t *testing.T, events []*agents.TranscriptEvent) (*Response, []map[string]any) {
	t.Helper()
	resp := NewResponse("sess-1", "atlas", nil)
	var wire []map[string]any
	tr := NewTranslator(resp, func(ev map[string]any) { wire = append(wire, ev) })
	for _, ev := range events {
		tr.Handle(ev)
	}
	return resp, wire
}

func TestTranslator_TextTurnStream(t *testing.T) {
	resp := NewResponse("sess-1", "atlas", nil)
	var wire []map[string]any
	tr := NewTranslator(resp, func(ev map[string]any) { wire = append(wire, ev) })

	for _, ev := range []*agents.TranscriptEvent{
		{Kind: agents.TranscriptEventTurnStarted},
		{Kind: agents.TranscriptEventTextDelta, TextDelta: "he", TurnID: "turn-9"},
		{Kind: agents.TranscriptEventTextDelta, TextDelta: "llo"},
		{Kind: agents.TranscriptEventMessageCompleted, Message: &agents.CompletedMessage{Role: "assistant", Content: "hello"}},
		{Kind: agents.TranscriptEventTurnCompleted, Usage: &agents.UsagePayload{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}},
	} {
		if tr.Handle(ev) {
			break
		}
	}

	if resp.Status != StatusCompleted {
		t.Fatalf("status = %s", resp.Status)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("output = %d items, want 1", len(resp.Output))
	}

	// Lifecycle: created, in_progress, item added, part added, 2 deltas,
	// text done, part done, item done, completed = 10 events.
	wantTypes := []string{
		"response.created", "response.in_progress",
		"response.output_item.added", "response.content_part.added",
		"response.output_text.delta", "response.output_text.delta",
		"response.output_text.done", "response.content_part.done",
		"response.output_item.done", "response.completed",
	}
	if len(wire) != len(wantTypes) {
		t.Fatalf("got %d events, want %d: %v", len(wire), len(wantTypes), typesOf(wire))
	}
	for i, wt := range wantTypes {
		if wire[i]["type"] != wt {
			t.Errorf("event %d = %v, want %s", i, wire[i]["type"], wt)
		}
	}

	// Sequence numbers strictly increasing from 0.
	for i, ev := range wire {
		if ev["sequence_number"] != i {
			t.Errorf("event %d sequence_number = %v", i, ev["sequence_number"])
		}
	}

	// Terminal usage.
	if resp.Usage == nil || resp.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	// Minted ID reflects the observed turn.
	if _, turn, _ := DecodeResponseID(resp.ID); turn != "turn-9" {
		t.Fatalf("turn = %q, want turn-9", turn)
	}
}

func TestTranslator_ToolTraceAndApproval(t *testing.T) {
	resp := NewResponse("sess-1", "atlas", nil)
	var wire []map[string]any
	tr := NewTranslator(resp, func(ev map[string]any) { wire = append(wire, ev) })

	stopped := false
	for _, ev := range []*agents.TranscriptEvent{
		{Kind: agents.TranscriptEventTurnStarted},
		{Kind: agents.TranscriptEventToolCallStarted, ToolCall: &agents.ToolCallPayload{CallID: "c1", Name: "web.fetch", Arguments: "{}"}},
		{Kind: agents.TranscriptEventToolCallFinished, ToolResult: &agents.ToolResultPayload{CallID: "c1", Name: "web.fetch", Result: "payload", IsError: false}},
		{Kind: agents.TranscriptEventApprovalRequired, Approval: &agents.ApprovalPayload{InterruptID: "int-1", Command: "rm -rf /"}},
	} {
		if tr.Handle(ev) {
			stopped = true
			break
		}
	}

	if !stopped {
		t.Fatal("approval should stop the stream")
	}
	if resp.Status != StatusIncomplete {
		t.Fatalf("status = %s, want incomplete", resp.Status)
	}

	// Output: function_call item + trace item (approval event is not an item).
	if len(resp.Output) != 2 {
		t.Fatalf("output = %d items, want 2", len(resp.Output))
	}
	if resp.Output[0]["type"] != "function_call" || resp.Output[0]["name"] != "web.fetch" {
		t.Fatalf("output[0] = %+v", resp.Output[0])
	}
	trace := resp.Output[1]
	if trace["type"] != "onclaw.function_call_output" || trace["result"] != "payload" || trace["call_id"] != "c1" {
		t.Fatalf("trace = %+v", trace)
	}

	// The approval custom event is present and no terminal response event fired.
	last := wire[len(wire)-1]
	if last["type"] != "onclaw:approval_required" || last["interrupt_id"] != "int-1" {
		t.Fatalf("last event = %+v", last)
	}
	for _, ev := range wire {
		switch ev["type"] {
		case "response.completed", "response.failed", "response.incomplete":
			t.Fatalf("terminal event %v must not be emitted on approval", ev["type"])
		}
	}
}

func TestTranslator_ErrorTurn(t *testing.T) {
	resp, wire := collect(t, []*agents.TranscriptEvent{
		{Kind: agents.TranscriptEventTurnStarted},
		{Kind: agents.TranscriptEventError, Error: "provider down"},
	})
	if resp.Status != StatusFailed || resp.Error == nil || resp.Error.Code != "model_error" {
		t.Fatalf("resp = %+v", resp)
	}
	if wire[len(wire)-1]["type"] != "response.failed" {
		t.Fatalf("terminal = %v", wire[len(wire)-1]["type"])
	}
}

// usageProbe decodes the usage block off a serialized terminal wire event.
type usageProbe struct {
	Response struct {
		Usage *struct {
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
			TotalTokens      int `json:"total_tokens"`
			FinalInputTokens int `json:"final_input_tokens"`
		} `json:"usage"`
	} `json:"response"`
}

func TestTranslator_TerminalEventsCarryFinalInputTokens(t *testing.T) {
	const wantFinal = 50000
	cases := []struct {
		name       string
		terminal   *agents.TranscriptEvent
		wantType   string
		wantStatus string
	}{
		{
			name:       "completed",
			terminal:   &agents.TranscriptEvent{Kind: agents.TranscriptEventTurnCompleted, Usage: &agents.UsagePayload{InputTokens: 135000, OutputTokens: 2000, TotalTokens: 137000, FinalInputTokens: wantFinal}},
			wantType:   "response.completed",
			wantStatus: StatusCompleted,
		},
		{
			name:       "failed",
			terminal:   &agents.TranscriptEvent{Kind: agents.TranscriptEventError, Error: "provider down", Usage: &agents.UsagePayload{InputTokens: 135000, OutputTokens: 2000, TotalTokens: 137000, FinalInputTokens: wantFinal}},
			wantType:   "response.failed",
			wantStatus: StatusFailed,
		},
		{
			name:       "incomplete",
			terminal:   &agents.TranscriptEvent{Kind: agents.TranscriptEventCancelled, CancelReason: "stream interrupted", Usage: &agents.UsagePayload{InputTokens: 135000, OutputTokens: 2000, TotalTokens: 137000, FinalInputTokens: wantFinal}},
			wantType:   "response.incomplete",
			wantStatus: StatusIncomplete,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, wire := collect(t, []*agents.TranscriptEvent{
				{Kind: agents.TranscriptEventTurnStarted},
				tc.terminal,
			})
			if resp.Status != tc.wantStatus {
				t.Fatalf("status = %s, want %s", resp.Status, tc.wantStatus)
			}
			last := wire[len(wire)-1]
			if last["type"] != tc.wantType {
				t.Fatalf("terminal wire event = %v, want %s", last["type"], tc.wantType)
			}
			var probe usageProbe
			if err := json.Unmarshal(JSON(last), &probe); err != nil {
				t.Fatalf("unmarshal terminal event: %v", err)
			}
			if probe.Response.Usage == nil {
				t.Fatalf("%s must carry a usage block", tc.wantType)
			}
			if probe.Response.Usage.FinalInputTokens != wantFinal {
				t.Fatalf("%s usage.final_input_tokens = %d, want %d", tc.wantType, probe.Response.Usage.FinalInputTokens, wantFinal)
			}
			if probe.Response.Usage.InputTokens != 135000 {
				t.Fatalf("%s usage.input_tokens = %d, want 135000", tc.wantType, probe.Response.Usage.InputTokens)
			}
		})
	}
}

func TestTranslator_NilUsageOmitsBlock(t *testing.T) {
	resp, wire := collect(t, []*agents.TranscriptEvent{
		{Kind: agents.TranscriptEventTurnStarted},
		{Kind: agents.TranscriptEventTurnCompleted},
	})
	if resp.Usage != nil {
		t.Fatalf("usage = %+v, want nil", resp.Usage)
	}
	b := JSON(wire[len(wire)-1])
	if strings.Contains(string(b), "usage") {
		t.Fatalf("terminal event must omit the usage block entirely: %s", b)
	}
}

func TestTranslator_ReasoningDeltaPassthrough(t *testing.T) {
	resp, wire := collect(t, []*agents.TranscriptEvent{
		{Kind: agents.TranscriptEventTurnStarted},
		{Kind: agents.TranscriptEventReasoningDelta, ReasoningDelta: "think "},
		{Kind: agents.TranscriptEventReasoningDelta, ReasoningDelta: "hard"},
		{Kind: agents.TranscriptEventMessageCompleted, Message: &agents.CompletedMessage{Role: "assistant", Content: "answer"}},
		{Kind: agents.TranscriptEventTurnCompleted},
	})

	var deltas []string
	for _, ev := range wire {
		if ev["type"] == "onclaw:reasoning_delta" {
			deltas = append(deltas, ev["delta"].(string))
		}
	}
	if len(deltas) != 2 || deltas[0]+deltas[1] != "think hard" {
		t.Fatalf("reasoning deltas = %v, want [think  hard]", deltas)
	}

	// Reasoning is a stream notification, never an output item.
	for _, item := range resp.Output {
		if it := item["type"].(string); strings.Contains(it, "reasoning") {
			t.Fatalf("reasoning leaked into output item %q", it)
		}
	}
}

func TestTranslator_ToolTraceShapePinned(t *testing.T) {
	resp, wire := collect(t, []*agents.TranscriptEvent{
		{Kind: agents.TranscriptEventTurnStarted},
		{Kind: agents.TranscriptEventToolCallStarted, ToolCall: &agents.ToolCallPayload{CallID: "c1", Name: "web.fetch", Arguments: `{"url":"https://x"}`}},
		{Kind: agents.TranscriptEventToolCallFinished, ToolResult: &agents.ToolResultPayload{
			CallID:  "c1",
			Name:    "web.fetch",
			Result:  "payload",
			Latency: 1500 * time.Millisecond,
			IsError: true,
		}},
		{Kind: agents.TranscriptEventTurnCompleted},
	})

	// Wire lifecycle: function_call rides output_item.added (arguments at
	// added) then done; the result is a dot item onclaw.function_call_output
	// inside its own added/done pair — never a top-level colon event.
	var added, done []map[string]any
	for _, ev := range wire {
		switch ev["type"] {
		case "response.output_item.added":
			added = append(added, ev["item"].(map[string]any))
		case "response.output_item.done":
			done = append(done, ev["item"].(map[string]any))
		case "onclaw:function_call_output":
			t.Fatal("onclaw.function_call_output must be an item type, not a top-level event")
		}
	}
	if len(added) != 2 || len(done) != 2 {
		t.Fatalf("added = %d, done = %d, want 2 each", len(added), len(done))
	}

	fc := added[0]
	if fc["type"] != "function_call" || fc["call_id"] != "c1" || fc["arguments"] != `{"url":"https://x"}` {
		t.Fatalf("function_call added item = %+v", fc)
	}
	traceAdded := added[1]
	if traceAdded["type"] != "onclaw.function_call_output" {
		t.Fatalf("trace added item type = %v", traceAdded["type"])
	}
	// Latency and error flags land on the trace item.
	if traceAdded["latency_ms"] != int64(1500) {
		t.Fatalf("latency_ms = %v (%T), want 1500", traceAdded["latency_ms"], traceAdded["latency_ms"])
	}
	if traceAdded["is_error"] != true {
		t.Fatalf("is_error = %v, want true", traceAdded["is_error"])
	}
	traceDone := done[1]
	if traceDone["type"] != "onclaw.function_call_output" || traceDone["status"] != "completed" {
		t.Fatalf("trace done item = %+v", traceDone)
	}

	// Aggregated output carries the closed items.
	if len(resp.Output) != 2 {
		t.Fatalf("output = %d items, want 2", len(resp.Output))
	}
	if resp.Output[1]["latency_ms"] != int64(1500) || resp.Output[1]["is_error"] != true {
		t.Fatalf("aggregated trace = %+v", resp.Output[1])
	}
}

func TestTranslator_ToolTraceOmitsLatencyWhenZero(t *testing.T) {
	_, wire := collect(t, []*agents.TranscriptEvent{
		{Kind: agents.TranscriptEventTurnStarted},
		{Kind: agents.TranscriptEventToolCallStarted, ToolCall: &agents.ToolCallPayload{CallID: "c1", Name: "web.fetch"}},
		{Kind: agents.TranscriptEventToolCallFinished, ToolResult: &agents.ToolResultPayload{CallID: "c1", Name: "web.fetch", Result: "ok"}},
		{Kind: agents.TranscriptEventTurnCompleted},
	})
	for _, ev := range wire {
		if ev["type"] == "response.output_item.added" {
			if item, ok := ev["item"].(map[string]any); ok && item["type"] == "onclaw.function_call_output" {
				if _, present := item["latency_ms"]; present {
					t.Fatalf("latency_ms must be omitted when zero: %+v", item)
				}
				if _, present := item["is_error"]; present {
					t.Fatalf("is_error must be omitted when false: %+v", item)
				}
				return
			}
		}
	}
	t.Fatal("no onclaw.function_call_output added item found")
}

func TestTranslator_ContextCompactedCarriesTokens(t *testing.T) {
	resp, wire := collect(t, []*agents.TranscriptEvent{
		{Kind: agents.TranscriptEventTurnStarted},
		{Kind: agents.TranscriptEventContextCompacted, Compaction: &agents.CompactionPayload{
			TokensBefore: 154000,
			TokensAfter:  9200,
		}},
		{Kind: agents.TranscriptEventTurnCompleted, Usage: &agents.UsagePayload{InputTokens: 40, OutputTokens: 30, TotalTokens: 70}},
	})

	// The compacted frame sits between created/in_progress and completed and
	// carries the display-only token estimates with type + sequence_number.
	var compacted map[string]any
	for _, ev := range wire {
		if ev["type"] == "onclaw:context_compacted" {
			compacted = ev
			break
		}
	}
	if compacted == nil {
		t.Fatalf("no onclaw:context_compacted frame in %v", typesOf(wire))
	}
	if compacted["tokens_before"] != 154000 || compacted["tokens_after"] != 9200 {
		t.Fatalf("compacted frame = %+v, want tokens 154000 -> 9200", compacted)
	}
	if _, ok := compacted["sequence_number"]; !ok {
		t.Fatalf("compacted frame missing sequence_number: %+v", compacted)
	}

	// Compaction is a notification: never an output item.
	if len(resp.Output) != 0 {
		t.Fatalf("output = %d items, want 0", len(resp.Output))
	}
	// The turn still terminates with the summarizer usage.
	if resp.Status != StatusCompleted || resp.Usage == nil || resp.Usage.TotalTokens != 70 {
		t.Fatalf("terminal = %s usage %+v", resp.Status, resp.Usage)
	}
	if wire[len(wire)-1]["type"] != "response.completed" {
		t.Fatalf("terminal wire event = %v", wire[len(wire)-1]["type"])
	}
}

func TestTranslator_ContextCompactedOmitsZeroAndEmpty(t *testing.T) {
	// Nil Compaction (legacy event shape) and zero estimates omit the keys
	// entirely, mirroring the latency_ms style.
	_, wire := collect(t, []*agents.TranscriptEvent{
		{Kind: agents.TranscriptEventContextCompacted},
		{Kind: agents.TranscriptEventContextCompacted, Compaction: &agents.CompactionPayload{}},
		{Kind: agents.TranscriptEventTurnCompleted},
	})
	var frames []map[string]any
	for _, ev := range wire {
		if ev["type"] == "onclaw:context_compacted" {
			frames = append(frames, ev)
		}
	}
	if len(frames) != 2 {
		t.Fatalf("got %d compacted frames, want 2", len(frames))
	}
	for i, frame := range frames {
		if _, present := frame["tokens_before"]; present {
			t.Fatalf("frame %d must omit zero tokens_before: %+v", i, frame)
		}
		if _, present := frame["tokens_after"]; present {
			t.Fatalf("frame %d must omit zero tokens_after: %+v", i, frame)
		}
	}
}

func TestTranslator_CompactAggregateShape(t *testing.T) {
	// The non-streaming compact turn folds to a completed response carrying
	// the summarizer usage and NO output items; the wire is just created,
	// in_progress, the compacted notification, and completed.
	resp, wire := collect(t, []*agents.TranscriptEvent{
		{Kind: agents.TranscriptEventTurnStarted},
		{Kind: agents.TranscriptEventContextCompacted, Compaction: &agents.CompactionPayload{
			Summary:      "digest",
			TokensBefore: 1000,
			TokensAfter:  100,
		}},
		{Kind: agents.TranscriptEventTurnCompleted, Usage: &agents.UsagePayload{InputTokens: 5, OutputTokens: 6, TotalTokens: 11}},
	})
	if resp.Status != StatusCompleted {
		t.Fatalf("status = %s", resp.Status)
	}
	if len(resp.Output) != 0 {
		t.Fatalf("output = %d items, want 0 (compaction emits no items)", len(resp.Output))
	}
	if resp.Usage == nil || resp.Usage.InputTokens != 5 || resp.Usage.OutputTokens != 6 {
		t.Fatalf("usage = %+v, want the summarizer's 5/6", resp.Usage)
	}
	if got := typesOf(wire); len(got) != 4 {
		t.Fatalf("wire = %v, want [created in_progress compacted completed]", got)
	}
}

func typesOf(events []map[string]any) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev["type"].(string))
	}
	return out
}

func TestFlattenInputParts_AcceptedShapes(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantText string
		wantAtts []InputAttachment
	}{
		{
			name:     "capability image beside text",
			in:       `[{"type":"message","role":"user","content":[{"type":"input_text","text":"look"},{"type":"input_image","image_url":"/api/v1/files/abc123","detail":"high"}]}]`,
			wantText: "look",
			wantAtts: []InputAttachment{{Kind: "image", URL: "/api/v1/files/abc123", Detail: "high"}},
		},
		{
			name:     "inline data URL image",
			in:       `[{"type":"message","content":[{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]}]`,
			wantAtts: []InputAttachment{{Kind: "image", URL: "data:image/png;base64,AAAA", Inline: true}},
		},
		{
			name:     "absolute-origin capability URL normalizes to its path",
			in:       `[{"type":"message","content":[{"type":"input_image","image_url":"https://onclaw.example.com/api/v1/files/k1"}]}]`,
			wantAtts: []InputAttachment{{Kind: "image", URL: "/api/v1/files/k1"}},
		},
		{
			name:     "image_url object form",
			in:       `[{"type":"message","content":[{"type":"input_image","image_url":{"url":"/api/v1/files/k2"}}]}]`,
			wantAtts: []InputAttachment{{Kind: "image", URL: "/api/v1/files/k2"}},
		},
		{
			name:     "input_file capability URL with filename",
			in:       `[{"type":"message","content":[{"type":"input_file","file_url":"/api/v1/files/k3","filename":"report.pdf"}]}]`,
			wantAtts: []InputAttachment{{Kind: "file", URL: "/api/v1/files/k3", Filename: "report.pdf"}},
		},
		{
			name:     "file_data alias is the inline file form",
			in:       `[{"type":"message","content":[{"type":"input_file","file_data":"data:application/pdf;base64,AAAA","filename":"r.pdf"}]}]`,
			wantAtts: []InputAttachment{{Kind: "file", URL: "data:application/pdf;base64,AAAA", Filename: "r.pdf", Inline: true}},
		},
	}
	for _, tc := range cases {
		text, atts, err := FlattenInputParts([]byte(tc.in))
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
			continue
		}
		if text != tc.wantText {
			t.Errorf("%s: text = %q, want %q", tc.name, text, tc.wantText)
		}
		if !reflect.DeepEqual(atts, tc.wantAtts) {
			t.Errorf("%s: atts = %+v, want %+v", tc.name, atts, tc.wantAtts)
		}
	}

	// Capability references expose their store key; inline ones do not.
	cap := InputAttachment{Kind: "image", URL: "/api/v1/files/abc123"}
	if key, ok := cap.CapabilityKey(); !ok || key != "abc123" {
		t.Errorf("CapabilityKey = %q, %v; want abc123, true", key, ok)
	}
	if _, ok := (InputAttachment{Kind: "image", URL: "data:image/png;base64,AAAA", Inline: true}).CapabilityKey(); ok {
		t.Error("inline attachments must not report a capability key")
	}
}

func TestFlattenInputParts_RejectedShapes(t *testing.T) {
	cases := []struct{ name, in, wantContains string }{
		{
			name:         "file_id rejected",
			in:           `[{"type":"message","content":[{"type":"input_file","file_id":"file-123"}]}]`,
			wantContains: "file_id",
		},
		{
			name:         "remote file URL rejected",
			in:           `[{"type":"message","content":[{"type":"input_file","file_url":"https://example.com/doc.pdf","filename":"doc.pdf"}]}]`,
			wantContains: "remote URLs are not accepted",
		},
		{
			name:         "remote image URL rejected",
			in:           `[{"type":"message","content":[{"type":"input_image","image_url":"https://example.com/x.png"}]}]`,
			wantContains: "remote URLs are not accepted",
		},
		{
			name:         "image part without URL rejected",
			in:           `[{"type":"message","content":[{"type":"input_image"}]}]`,
			wantContains: "input_image",
		},
		{
			name:         "file part without URL rejected",
			in:           `[{"type":"message","content":[{"type":"input_file","filename":"x.pdf"}]}]`,
			wantContains: "input_file",
		},
	}
	for _, tc := range cases {
		_, atts, err := FlattenInputParts([]byte(tc.in))
		if err == nil {
			t.Errorf("%s: expected error, got atts %+v", tc.name, atts)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantContains) {
			t.Errorf("%s: error %q must contain %q", tc.name, err.Error(), tc.wantContains)
		}
	}
}

// TestFlattenInputParts_PassthroughByteForByte pins the openresponses spec
// guarantee: string-only and text-part-only inputs flatten to exactly what
// they always did, with no attachments — the string path is byte-for-byte
// identical to the pre-parts behavior.
func TestFlattenInputParts_PassthroughByteForByte(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"string", `"hello"`, "hello"},
		{"empty", ``, ""},
		{"text-only parts", `[{"type":"message","role":"user","content":[{"type":"input_text","text":"a"},{"type":"input_text","text":"b"}]}]`, "a\nb"},
		{"string content", `[{"type":"message","role":"user","content":"plain"}]`, "plain"},
	}
	for _, tc := range cases {
		got, atts, err := FlattenInputParts([]byte(tc.in))
		if err != nil || got != tc.want || len(atts) != 0 {
			t.Errorf("%s: parts flatten = %q, %d atts, err %v; want %q, 0 atts, nil", tc.name, got, len(atts), err, tc.want)
		}
		viaOld, err := FlattenInput([]byte(tc.in))
		if err != nil || viaOld != tc.want {
			t.Errorf("%s: FlattenInput diverged: %q, err %v", tc.name, viaOld, err)
		}
	}
}
