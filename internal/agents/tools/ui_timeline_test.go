package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestUITimeline_ValidEchoRoundTrip pins the echo contract (adopt-assistant-
// ui-elements D3) for the timeline: args echo back under `$type` first, with
// the events in the passed order and the field names the frontend card
// parses (state, optional detail).
func TestUITimeline_ValidEchoRoundTrip(t *testing.T) {
	tool, err := NewUITimeline()
	if err != nil {
		t.Fatalf("new ui.timeline: %v", err)
	}

	out, err := invoke(t, tool, `{
		"title": "Incident 1042",
		"events": [
			{"label": "Alert fired", "at": "2026-09-20T08:01:00Z", "state": "settled", "detail": "paged via on-call rotation"},
			{"label": "Rollback started", "at": "08:04"},
			{"label": "Postmortem due", "state": "reference", "detail": "owner: atlas"}
		]
	}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}

	if !strings.HasPrefix(out, `{"$type":"timeline"`) {
		t.Fatalf("expected $type as the first JSON key, got %s", out)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if decoded["$type"] != "timeline" {
		t.Fatalf("expected $type timeline, got %v", decoded["$type"])
	}
	if decoded["title"] != "Incident 1042" {
		t.Fatalf("expected title echoed, got %v", decoded["title"])
	}
	events, ok := decoded["events"].([]any)
	if !ok || len(events) != 3 {
		t.Fatalf("expected 3 echoed events, got %v", decoded["events"])
	}
	first, _ := events[0].(map[string]any)
	if first["label"] != "Alert fired" || first["state"] != "settled" {
		t.Fatalf("unexpected first event: %v", first)
	}
	if first["detail"] != "paged via on-call rotation" {
		t.Fatalf("expected first event detail echoed, got %v", first["detail"])
	}
	// The reference event echoes its state and detail; the bare
	// label+at case echoes with neither a state nor a detail key.
	third, _ := events[2].(map[string]any)
	if third["state"] != "reference" || third["detail"] != "owner: atlas" {
		t.Fatalf("unexpected third event: %v", third)
	}
	second, _ := events[1].(map[string]any)
	if second["at"] != "08:04" {
		t.Fatalf("expected at echoed verbatim, got %v", second["at"])
	}
	if _, has := second["state"]; has {
		t.Fatalf("expected no state key on a stateless event, got %v", second["state"])
	}
	if _, has := second["detail"]; has {
		t.Fatalf("expected no detail key on a detail-less event, got %v", second["detail"])
	}
}

// TestUITimeline_MalformedRejected pins strict validation.
func TestUITimeline_MalformedRejected(t *testing.T) {
	tool, err := NewUITimeline()
	if err != nil {
		t.Fatalf("new ui.timeline: %v", err)
	}

	rejections := []string{
		`{"title": "T"}`,                                  // missing events
		`{"events": []}`,                                  // empty events
		`{"events": [{"at": "08:00"}]}`,                   // missing label
		`{"events": [{"label": " "}]}`,                    // blank label
		`{"events": [{"label": "L", "state": "done"}]}`,   // unknown state
		`{"events": [{"label": "L", "kind": "settled"}]}`, // legacy kind key
		`{"events": [{"label": "L"}], "note": "extra"}`,   // unknown field
		`{"events": [{"label": 7}]}`,                      // non-string label
	}
	for i, args := range rejections {
		if _, err := invoke(t, tool, args); err == nil {
			t.Fatalf("rejection %d: expected an error for %s", i, args)
		}
	}
}
