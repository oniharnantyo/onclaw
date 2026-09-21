package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestUIPreview_ValidEchoRoundTrip pins the echo contract (adopt-assistant-
// ui-elements D3) for the preview: url, html, and the optional title echo
// back under `$type` first — exactly the fields the frontend card parses.
// The card fixes the frame height itself, so no height exists anywhere.
func TestUIPreview_ValidEchoRoundTrip(t *testing.T) {
	tool, err := NewUIPreview()
	if err != nil {
		t.Fatalf("new ui.preview: %v", err)
	}

	out, err := invoke(t, tool, `{
		"url": "https://dash.example.com/reports/1042",
		"html": "The incident dashboard for report 1042.",
		"title": "Incident dashboard"
	}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}

	if !strings.HasPrefix(out, `{"$type":"preview"`) {
		t.Fatalf("expected $type as the first JSON key, got %s", out)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if decoded["$type"] != "preview" {
		t.Fatalf("expected $type preview, got %v", decoded["$type"])
	}
	if decoded["url"] != "https://dash.example.com/reports/1042" {
		t.Fatalf("expected url echoed, got %v", decoded["url"])
	}
	if decoded["html"] != "The incident dashboard for report 1042." {
		t.Fatalf("expected html echoed, got %v", decoded["html"])
	}
	if decoded["title"] != "Incident dashboard" {
		t.Fatalf("expected title echoed, got %v", decoded["title"])
	}

	// Title is optional: an echo without it carries no title key.
	out, err = invoke(t, tool, `{
		"url": "http://localhost:3000/preview",
		"html": "A local draft."
	}`)
	if err != nil {
		t.Fatalf("invoke without title: %v", err)
	}
	var minimal map[string]any
	if err := json.Unmarshal([]byte(out), &minimal); err != nil {
		t.Fatalf("decode minimal result: %v", err)
	}
	if _, has := minimal["title"]; has {
		t.Fatalf("expected no title key on a title-less echo, got %v", minimal["title"])
	}
}

// TestUIPreview_MalformedRejected pins strict validation — including the
// scheme gate: the card auto-loads the URL in a sandboxed frame, so only
// absolute http(s) addresses echo.
func TestUIPreview_MalformedRejected(t *testing.T) {
	tool, err := NewUIPreview()
	if err != nil {
		t.Fatalf("new ui.preview: %v", err)
	}

	rejections := []string{
		`{"url": "https://ok.example.com", "html": ""}`, // missing html
		`{"html": "H"}`, // missing url
		`{"url": "javascript:alert(1)", "html": "H"}`,                   // non-http scheme
		`{"url": "data:text/html,hi", "html": "H"}`,                     // data scheme
		`{"url": "//cdn.example.com/x", "html": "H"}`,                   // scheme-relative
		`{"url": "https://ok.example.com", "html": "H", "height": 480}`, // retired height field
		`{"url": "https://ok.example.com", "html": "H", "note": "x"}`,   // unknown field
	}
	for i, args := range rejections {
		if _, err := invoke(t, tool, args); err == nil {
			t.Fatalf("rejection %d: expected an error for %s", i, args)
		}
	}
}
