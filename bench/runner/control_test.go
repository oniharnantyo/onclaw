package runner

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestControlServerDecisionShapes verifies /block and /allow return exactly
// the canonical decision bodies the harness's hook handlers parse
// (ParseDecisionJSON): the whole trimmed body must be one JSON object with
// decision exactly "allow" or "block", no trailing data.
func TestControlServerDecisionShapes(t *testing.T) {
	ctrl, err := startControlServer()
	if err != nil {
		t.Fatalf("startControlServer: %v", err)
	}
	defer ctrl.Close()

	if got := ctrl.BaseURL(); !strings.HasPrefix(got, "http://127.0.0.1:") {
		t.Fatalf("BaseURL = %q, want a loopback http URL with an ephemeral port", got)
	}

	tests := []struct {
		path     string
		decision string
	}{
		{"/block", "block"},
		{"/allow", "allow"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			resp, err := http.Get(ctrl.BaseURL() + tc.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tc.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
				t.Fatalf("content-type = %q, want application/json", ct)
			}

			// Strict whole-object rule: no trailing data after the object.
			var buf bytes.Buffer
			if _, err := buf.ReadFrom(resp.Body); err != nil {
				t.Fatalf("reading body: %v", err)
			}
			dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
			var parsed struct {
				Decision string `json:"decision"`
				Reason   string `json:"reason"`
			}
			if err := dec.Decode(&parsed); err != nil {
				t.Fatalf("body %q does not parse as one JSON object: %v", buf.String(), err)
			}
			if dec.More() {
				t.Fatalf("body %q carries trailing data after the decision object", buf.String())
			}
			if parsed.Decision != tc.decision {
				t.Fatalf("decision = %q, want %q", parsed.Decision, tc.decision)
			}
			if strings.TrimSpace(parsed.Reason) == "" {
				t.Fatalf("reason is empty; a block/allow decision should carry its reason")
			}
		})
	}

	// The POST path is the one the harness's http hook handler uses.
	resp, err := http.Post(ctrl.BaseURL()+"/block", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST /block: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /block status = %d, want 200", resp.StatusCode)
	}
}

// TestControlServerUnknownPath pins the 404 on non-control paths so a
// mistyped {{control_url}} suffix fails loudly on the server side.
func TestControlServerUnknownPath(t *testing.T) {
	ctrl, err := startControlServer()
	if err != nil {
		t.Fatalf("startControlServer: %v", err)
	}
	defer ctrl.Close()

	resp, err := http.Get(ctrl.BaseURL() + "/nope")
	if err != nil {
		t.Fatalf("GET /nope: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown path status = %d, want 404", resp.StatusCode)
	}
}
