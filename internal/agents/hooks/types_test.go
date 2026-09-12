package hooks

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseDecisionJSON(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantOK       bool
		wantDecision string
		wantReason   string
	}{
		{name: "allow", body: `{"decision":"allow"}`, wantOK: true, wantDecision: "allow"},
		{name: "block with reason", body: `{"decision":"block","reason":"not on fridays"}`, wantOK: true, wantDecision: "block", wantReason: "not on fridays"},
		{name: "surrounding whitespace is trimmed", body: "\n\t  {\"decision\":\"block\"}  \n", wantOK: true, wantDecision: "block"},
		{name: "unknown fields tolerated", body: `{"decision":"allow","extra":{"a":1}}`, wantOK: true, wantDecision: "allow"},
		{name: "empty body", body: "", wantOK: false},
		{name: "whitespace only", body: "   \n", wantOK: false},
		{name: "bare word is not an object", body: "allow", wantOK: false},
		{name: "log line is not a decision", body: "INFO hook started, deciding soon...", wantOK: false},
		{name: "array is not an object", body: `[{"decision":"allow"}]`, wantOK: false},
		{name: "missing decision field", body: `{"reason":"x"}`, wantOK: false},
		{name: "uppercase decision rejected", body: `{"decision":"BLOCK"}`, wantOK: false},
		{name: "unknown decision value", body: `{"decision":"deny"}`, wantOK: false},
		{name: "non-string reason", body: `{"decision":"block","reason":5}`, wantOK: false},
		{name: "trailing garbage after object", body: `{"decision":"allow"} trailing`, wantOK: false},
		{name: "two decision objects", body: `{"decision":"block"} {"decision":"allow"}`, wantOK: false},
		{name: "truncated json", body: `{"decision":"block"`, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseDecisionJSON([]byte(tt.body))
			if ok != tt.wantOK {
				t.Fatalf("ParseDecisionJSON(%q) ok = %v, want %v", tt.body, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.Decision != tt.wantDecision {
				t.Errorf("decision = %q, want %q", got.Decision, tt.wantDecision)
			}
			if got.Reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

// TestEventWireKeys pins the binding JSON keys of the event payload: these
// hit webhook bodies and command stdin, so renaming a tag breaks consumers.
func TestEventWireKeys(t *testing.T) {
	ev := Event{
		Event:      "pre_tool_use",
		DeliveryID: "d-1",
		Origin:     "user",
		Workspace:  EventRef{ID: "w-1", Name: "Acme"},
		Agent:      EventRef{ID: "a-1", Name: "Atlas"},
		SessionID:  "s-1",
		User:       &EventRef{ID: "u-1", Name: "Dana"},
		Tool:       &EventTool{Name: "shell.run", CallID: "c-1", Args: `{"cmd":"ls"}`},
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"event", "delivery_id", "origin", "workspace", "agent", "session_id", "user", "tool"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("event JSON missing key %q: %s", key, raw)
		}
	}
	if _, ok := fields["status"]; ok {
		t.Errorf("status must be omitted when empty: %s", raw)
	}

	finished := Event{Event: "run_finished", Status: "failed", Origin: "scheduler"}
	raw, err = json.Marshal(finished)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"status":"failed"`) {
		t.Errorf("run_finished status missing: %s", raw)
	}
	if strings.Contains(string(raw), `"user"`) || strings.Contains(string(raw), `"tool"`) {
		t.Errorf("absent user/tool must be omitted: %s", raw)
	}
}
