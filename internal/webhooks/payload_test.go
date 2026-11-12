package webhooks

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPayloadBlock_JSONBody pins the fenced-JSON rendering: a body that
// parses as JSON rides a ```json fence verbatim.
func TestPayloadBlock_JSONBody(t *testing.T) {
	body := []byte(`{"action":"push","ref":"refs/heads/main"}`)
	block := PayloadBlock(body)
	require.Equal(t, "```json\n"+string(body)+"\n```", block)
}

// TestPayloadBlock_RawBody pins the fenced plain-text rendering: a
// form-encoded body (the non-JSON sender scenario) rides a plain fence.
func TestPayloadBlock_RawBody(t *testing.T) {
	body := []byte("status=ok&build=1234")
	block := PayloadBlock(body)
	require.Equal(t, "```\n"+string(body)+"\n```", block)
}

// TestPayloadBlock_Empty pins the degenerate bodies: an empty body is not
// JSON, so it renders as an empty plain fence rather than panicking.
func TestPayloadBlock_Empty(t *testing.T) {
	require.Equal(t, "```\n\n```", PayloadBlock(nil))
}

// TestRenderPrompt pins the composition: the template, a blank line, then
// the payload block — and the block alone when the template is empty.
func TestRenderPrompt(t *testing.T) {
	body := []byte(`{"ok":true}`)
	require.Equal(t, "Summarize this\n\n```json\n"+string(body)+"\n```", RenderPrompt("Summarize this", body))
	require.Equal(t, "```json\n"+string(body)+"\n```", RenderPrompt("", body))
}

// TestPayloadBlock_TrailingNewlineIsStillJSON — json.Valid accepts trailing
// whitespace, so a JSON body with a trailing newline keeps the json fence.
func TestPayloadBlock_TrailingNewlineIsStillJSON(t *testing.T) {
	block := PayloadBlock([]byte("{\"a\":1}\n"))
	require.True(t, strings.HasPrefix(block, "```json\n"), "a JSON body with trailing whitespace must keep the json fence")
}

// TestReadBody_CapFirst pins the size-capped read: bodies at or under the
// limit come back whole; anything that would exceed it is refused unread
// beyond the cap.
func TestReadBody_CapFirst(t *testing.T) {
	tests := []struct {
		name  string
		body  []byte
		limit int64
		want  []byte
		ok    bool
	}{
		{"under limit", []byte("hello"), 10, []byte("hello"), true},
		{"exactly at limit", bytes.Repeat([]byte("a"), 10), 10, bytes.Repeat([]byte("a"), 10), true},
		{"one over limit", bytes.Repeat([]byte("a"), 11), 10, nil, false},
		{"empty body", nil, 10, []byte{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/webhooks/x", bytes.NewReader(tt.body))
			got, ok := readBody(req, tt.limit)
			require.Equal(t, tt.ok, ok)
			if tt.ok {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

// TestReadBody_LargeBodyRefusedEarly — a body far over the limit reads at
// most limit+1 bytes off the wire, not the whole payload.
func TestReadBody_LargeBodyRefusedEarly(t *testing.T) {
	huge := bytes.Repeat([]byte("a"), 4*DefaultBodyLimit)
	req := httptest.NewRequest("POST", "/webhooks/x", bytes.NewReader(huge))
	_, ok := readBody(req, DefaultBodyLimit)
	require.False(t, ok, "a body over the cap must be refused")
}
