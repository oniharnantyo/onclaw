package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestUIChart_ValidEchoRoundTrip pins the echo contract (adopt-assistant-ui-
// elements D3): the tool validates the schema and returns the args back as
// the result envelope — with `$type` as the FIRST JSON key, which the
// frontend registry dispatches on, and the field names the frontend card
// parses (points, optional visible).
func TestUIChart_ValidEchoRoundTrip(t *testing.T) {
	tool, err := NewUIChart()
	if err != nil {
		t.Fatalf("new ui.chart: %v", err)
	}

	out, err := invoke(t, tool, `{
		"label": "Deploys this week",
		"value": 42,
		"delta": -3,
		"variant": "area",
		"points": [5, 9, 28],
		"visible": 2
	}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}

	// $type first: the raw envelope must open with the dispatch key.
	if !strings.HasPrefix(out, `{"$type":"chart"`) {
		t.Fatalf("expected $type as the first JSON key, got %s", out)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if decoded["$type"] != "chart" {
		t.Fatalf("expected $type chart, got %v", decoded["$type"])
	}
	if decoded["label"] != "Deploys this week" {
		t.Fatalf("expected the label echoed, got %v", decoded["label"])
	}
	if decoded["value"] != float64(42) || decoded["delta"] != float64(-3) {
		t.Fatalf("expected value/delta echoed, got %v / %v", decoded["value"], decoded["delta"])
	}
	if decoded["variant"] != "area" {
		t.Fatalf("expected variant echoed, got %v", decoded["variant"])
	}
	points, ok := decoded["points"].([]any)
	if !ok || len(points) != 3 {
		t.Fatalf("expected 3 echoed points, got %v", decoded["points"])
	}
	if points[0] != float64(5) || points[1] != float64(9) || points[2] != float64(28) {
		t.Fatalf("unexpected echoed points: %v", points)
	}
	if decoded["visible"] != float64(2) {
		t.Fatalf("expected visible echoed, got %v", decoded["visible"])
	}

	// visible is optional: an echo without it carries no visible key.
	out, err = invoke(t, tool, `{
		"label": "Errors",
		"value": 0.5,
		"points": [1]
	}`)
	if err != nil {
		t.Fatalf("invoke without visible: %v", err)
	}
	var minimal map[string]any
	if err := json.Unmarshal([]byte(out), &minimal); err != nil {
		t.Fatalf("decode minimal result: %v", err)
	}
	if _, has := minimal["visible"]; has {
		t.Fatalf("expected no visible key on a visibility-less echo, got %v", minimal["visible"])
	}
	if _, has := minimal["delta"]; has {
		t.Fatalf("expected no delta key on a delta-less echo, got %v", minimal["delta"])
	}
}

// TestUIChart_MalformedRejected pins strict validation: every schema
// violation is a tool error the model can correct — never a partial echo.
func TestUIChart_MalformedRejected(t *testing.T) {
	tool, err := NewUIChart()
	if err != nil {
		t.Fatalf("new ui.chart: %v", err)
	}

	rejections := []string{
		`{"value": 1, "points": [2]}`,                                 // missing label
		`{"label": "L", "points": [2]}`,                               // missing value
		`{"label": "L", "value": 1}`,                                  // missing points
		`{"label": "L", "value": 1, "points": []}`,                    // empty points
		`{"label": "L", "value": 1, "points": ["two"]}`,               // non-numeric point
		`{"label": "L", "value": 1, "points": [2], "visible": 0}`,     // zero visible
		`{"label": "L", "value": 1, "points": [2], "visible": -3}`,    // negative visible
		`{"label": "L", "value": 1, "points": [2], "note": "extra"}`,  // unknown field
		`{"label": "L", "value": "high", "points": [2]}`,              // non-numeric value
		`{"label": "L", "value": 1, "variant": "pie", "points": [2]}`, // unknown variant
	}
	for i, args := range rejections {
		if _, err := invoke(t, tool, args); err == nil {
			t.Fatalf("rejection %d: expected an error for %s", i, args)
		}
	}
}
