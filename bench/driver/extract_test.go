package driver

import (
	"encoding/json"
	"testing"
)

// mustOutput builds an output array from JSON so the test shapes stay
// readable and snake_case-exact against the wire format.
func mustOutput(t *testing.T, raw string) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("test fixture is not valid JSON: %v", err)
	}
	return resp
}

func TestExtractTurnPairsCallsAcrossMixedOutput(t *testing.T) {
	resp := mustOutput(t, `{
	  "status": "completed",
	  "output": [
	    {"type": "reasoning", "summary": []},
	    {"type": "function_call", "name": "files.write", "call_id": "call-b", "arguments": "{\"path\":\"a.txt\"}"},
	    {"type": "message", "content": [
	      {"type": "output_text", "text": "wrote the file"},
	      {"type": "output_text", "text": "done"}
	    ]},
	    {"type": "onclaw.function_call_output", "call_id": "call-a", "name": "web.search", "result": [{"title":"hit"}]},
	    {"type": "function_call", "name": "web.search", "call_id": "call-a", "arguments": "{\"q\":\"go\"}"},
	    {"type": "onclaw.function_call_output", "call_id": "call-b", "name": "files.write", "result": "ok"}
	  ]
	}`)

	turn := ExtractTurn(resp)
	if turn.Status != "completed" {
		t.Fatalf("status = %q, want completed", turn.Status)
	}
	if turn.AnswerText != "wrote the file\ndone" {
		t.Fatalf("answer = %q", turn.AnswerText)
	}
	if len(turn.ToolCalls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(turn.ToolCalls))
	}
	// Calls appear in first-seen order; outputs pair by call_id regardless of
	// the output item's position relative to its call item.
	if turn.ToolCalls[0].Name != "files.write" || turn.ToolCalls[0].CallID != "call-b" || turn.ToolCalls[0].Output != "ok" {
		t.Fatalf("call[0] = %+v", turn.ToolCalls[0])
	}
	if turn.ToolCalls[0].ArgsJSON != `{"path":"a.txt"}` {
		t.Fatalf("call[0].ArgsJSON = %q", turn.ToolCalls[0].ArgsJSON)
	}
	if turn.ToolCalls[1].Name != "web.search" || turn.ToolCalls[1].CallID != "call-a" {
		t.Fatalf("call[1] = %+v", turn.ToolCalls[1])
	}
	// A non-string result (JSON array) is re-encoded like the eval stringify.
	if turn.ToolCalls[1].Output != `[{"title":"hit"}]` {
		t.Fatalf("call[1].Output = %q", turn.ToolCalls[1].Output)
	}
}

func TestExtractTurnUnpairedAndEmptyOutputs(t *testing.T) {
	resp := mustOutput(t, `{
	  "status": "in_progress",
	  "output": [
	    {"type": "function_call", "name": "shell", "call_id": "call-1", "arguments": "{\"cmd\":\"ls\"}"},
	    {"type": "onclaw.function_call_output", "call_id": "call-unknown", "result": "orphan"},
	    {"type": "function_call", "name": "memory.search", "call_id": "call-2", "arguments": "{}"},
	    {"type": "onclaw.function_call_output", "call_id": "call-2"}
	  ]
	}`)

	turn := ExtractTurn(resp)
	if len(turn.ToolCalls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(turn.ToolCalls))
	}
	if turn.ToolCalls[0].Output != "" {
		t.Fatalf("unpaired call output = %q, want empty", turn.ToolCalls[0].Output)
	}
	if turn.ToolCalls[1].Output != "" {
		t.Fatalf("missing-result output = %q, want empty", turn.ToolCalls[1].Output)
	}
	if turn.Status != "in_progress" || turn.AnswerText != "" {
		t.Fatalf("turn = %+v", turn)
	}
}

func TestExtractTurnOutputFieldFallback(t *testing.T) {
	resp := mustOutput(t, `{
	  "status": "completed",
	  "output": [
	    {"type": "function_call", "name": "files.read", "call_id": "c1", "arguments": "{}"},
	    {"type": "onclaw.function_call_output", "call_id": "c1", "output": "file body"}
	  ]
	}`)
	turn := ExtractTurn(resp)
	if turn.ToolCalls[0].Output != "file body" {
		t.Fatalf("output fallback = %q, want %q", turn.ToolCalls[0].Output, "file body")
	}
}

func TestExtractTurnEmptyAndMalformed(t *testing.T) {
	if turn := ExtractTurn(nil); turn.Status != "" || turn.AnswerText != "" || len(turn.ToolCalls) != 0 {
		t.Fatalf("nil response = %+v", turn)
	}
	if turn := ExtractTurn(map[string]any{}); turn.Status != "" || len(turn.ToolCalls) != 0 {
		t.Fatalf("empty response = %+v", turn)
	}
	// Non-string JSON scalars stringify through encoding; a number result
	// becomes its JSON text.
	turn := ExtractTurn(mustOutput(t, `{"output":[{"type":"function_call","name":"t","call_id":"c","arguments":"{}"},{"type":"onclaw.function_call_output","call_id":"c","result":42}]}`))
	if turn.ToolCalls[0].Output != "42" {
		t.Fatalf("number output = %q, want 42", turn.ToolCalls[0].Output)
	}
}

func TestStringify(t *testing.T) {
	if got := stringify(nil); got != "" {
		t.Fatalf("stringify(nil) = %q", got)
	}
	if got := stringify("plain"); got != "plain" {
		t.Fatalf(`stringify("plain") = %q`, got)
	}
	if got := stringify(map[string]any{"k": float64(1)}); got != `{"k":1}` {
		t.Fatalf("stringify(map) = %q", got)
	}
}
