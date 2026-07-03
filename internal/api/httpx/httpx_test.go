package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
)

func TestJSON(t *testing.T) {
	w := httptest.NewRecorder()
	httpx.JSON(w, http.StatusCreated, map[string]string{"foo": "bar"})

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, w.Code)
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", w.Header().Get("Content-Type"))
	}
	var res map[string]string
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if res["foo"] != "bar" {
		t.Errorf("expected value %q, got %q", "bar", res["foo"])
	}
}

func TestError(t *testing.T) {
	w := httptest.NewRecorder()
	httpx.Error(w, http.StatusBadRequest, "bad request message")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	var res map[string]string
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if res["error"] != "bad request message" {
		t.Errorf("expected error %q, got %q", "bad request message", res["error"])
	}
}

type nonFlushingResponseWriter struct {
	header http.Header
}

func (n *nonFlushingResponseWriter) Header() http.Header {
	if n.header == nil {
		n.header = make(http.Header)
	}
	return n.header
}

func (n *nonFlushingResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (n *nonFlushingResponseWriter) WriteHeader(statusCode int)  {}

type flushingResponseWriter struct {
	nonFlushingResponseWriter
	flushed bool
	written strings.Builder
}

func (f *flushingResponseWriter) Flush() {
	f.flushed = true
}

func (f *flushingResponseWriter) Write(b []byte) (int, error) {
	return f.written.Write(b)
}

func TestSSEWriter(t *testing.T) {
	// Test error case (does not support flushing)
	nf := &nonFlushingResponseWriter{}
	_, err := httpx.NewSSEWriter(nf)
	if err == nil {
		t.Error("expected error when ResponseWriter does not support Flushing")
	}

	// Test success case
	frw := &flushingResponseWriter{}
	sse, err := httpx.NewSSEWriter(frw)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !frw.flushed {
		t.Error("expected Flush to have been called")
	}
	if frw.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("unexpected content type: %q", frw.Header().Get("Content-Type"))
	}

	// Write event
	err = sse.WriteEvent("test-event", map[string]string{"msg": "hello"})
	if err != nil {
		t.Fatalf("failed to write event: %v", err)
	}
	output := frw.written.String()
	if !strings.Contains(output, "event: test-event") {
		t.Errorf("expected event: test-event in output %q", output)
	}
	if !strings.Contains(output, `data: {"msg":"hello"}`) {
		t.Errorf("expected data in output %q", output)
	}

	// Write event without event name
	frw.written.Reset()
	err = sse.WriteEvent("", map[string]string{"foo": "bar"})
	if err != nil {
		t.Fatalf("failed to write event: %v", err)
	}
	output = frw.written.String()
	if strings.Contains(output, "event:") {
		t.Errorf("expected no event line, got %q", output)
	}
	if !strings.Contains(output, `data: {"foo":"bar"}`) {
		t.Errorf("expected data in output %q", output)
	}
}
