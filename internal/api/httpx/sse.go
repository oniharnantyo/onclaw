package httpx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// SSEWriter wraps http.ResponseWriter to write Server-Sent Events.
//
// WriteEvent is safe for concurrent use: an SSE stream is written from more
// than one goroutine — the handler loop (message/turn/done events) plus the
// agent's event-sink callbacks fired from eino's execution goroutines
// (usage/compaction events). http.ResponseWriter is not concurrency-safe, so
// unsynchronized concurrent writes interleave the chunked-transfer framing and
// corrupt the stream (observed downstream as "Invalid character in chunk size"
// followed by a torn-down connection). The mutex serializes every write+flush.
type SSEWriter struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter initializes headers and returns an SSEWriter.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing (needed for SSE)")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Prevent buffering by middlewares
	w.Header().Set("X-Accel-Buffering", "no")

	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	return &SSEWriter{w: w, flusher: flusher}, nil
}

// WriteEvent serializes data to JSON and sends it as an SSE event. The write
// and flush are serialized by mu so concurrent callers cannot interleave the
// chunked framing. json.Marshal runs outside the lock since it does not touch
// the response writer.
func (s *SSEWriter) WriteEvent(event string, data interface{}) error {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal event data: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if event != "" {
		if _, err := fmt.Fprintf(s.w, "event: %s\n", event); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", string(dataBytes)); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
