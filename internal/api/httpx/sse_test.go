package httpx_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
)

// TestSSEWriter_ConcurrentWriteEventIsSafe asserts that WriteEvent is safe to
// call from multiple goroutines. An SSE stream is written from both the handler
// loop (message/turn/done) and the agent's event-sink callbacks (usage/
// compaction), the latter fired from eino's execution goroutines. Without
// serialization, concurrent writes interleave the chunked-transfer framing and
// corrupt the stream — observed downstream as "Invalid character in chunk size"
// followed by a torn-down connection and a dead tool (context canceled).
//
// Run under `go test -race`: it fails without the mutex (data race on the
// ResponseWriter internals) and passes with it.
func TestSSEWriter_ConcurrentWriteEventIsSafe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse, err := httpx.NewSSEWriter(w)
		if err != nil {
			t.Errorf("NewSSEWriter: %v", err)
			return
		}

		var wg sync.WaitGroup
		const goroutines, events = 8, 50
		for g := 0; g < goroutines; g++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for i := 0; i < events; i++ {
					if err := sse.WriteEvent("message", map[string]int{"g": id, "i": i}); err != nil {
						t.Errorf("WriteEvent: %v", err)
						return
					}
				}
			}(g)
		}
		wg.Wait()
	}))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "text/plain", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("drain response: %v", err)
	}
}
