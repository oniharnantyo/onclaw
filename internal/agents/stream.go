package agents

import (
	"io"
	"sync"
)

// EventStream is the live-view tap of an agent execution: a bounded buffer of
// TranscriptEvents mapped from the run by the runner's drain goroutine.
//
// The tap is best-effort, not a pipeline. Send uses drop-new semantics: when
// the buffer is full — a slow or absent consumer — the event is discarded and
// Send returns false immediately instead of blocking, so an unwatched stream
// never stalls the run. Durable history is written independently by the
// session adapter inside the run, and a consumer that missed tap events
// recovers them from history by cursor (?after=). With an attached consumer
// reading at least as fast as the run produces, the stream is lossless.
//
// Cancel (like Close) closes the view only: Recv drains the buffered events
// and returns io.EOF. It never cancels the run — run-level cancellation lives
// behind Runner.CancelRun.
type EventStream struct {
	events chan *TranscriptEvent
	mu     sync.Mutex
	closed bool
	drops  int
}

// NewEventStream creates an initialized EventStream with the given buffer size.
func NewEventStream(bufferSize int) *EventStream {
	if bufferSize < 1 {
		bufferSize = 64
	}
	return &EventStream{
		events: make(chan *TranscriptEvent, bufferSize),
	}
}

// Send enqueues an event onto the stream. It never blocks: drop-new semantics
// discard the event and return false when the buffer is full (drops counted,
// see Dropped) or the stream is closed (view abandoned, not counted).
func (s *EventStream) Send(event *TranscriptEvent) bool {
	// The lock is held across the send, but the send is a non-blocking select,
	// so Send stays wait-free for the producer and races with Close cleanly.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case s.events <- event:
		return true
	default:
		s.drops++
		return false
	}
}

// Recv retrieves the next TranscriptEvent from the stream.
// It returns io.EOF after the stream has been closed and all events consumed.
func (s *EventStream) Recv() (*TranscriptEvent, error) {
	event, ok := <-s.events
	if !ok {
		return nil, io.EOF
	}
	return event, nil
}

// Dropped reports how many events were discarded because the buffer was full.
func (s *EventStream) Dropped() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drops
}

// Close closes the event channel if not already closed. Buffered events
// remain readable via Recv until EOF.
func (s *EventStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.events)
	return nil
}

// Cancel closes the view: Recv drains to EOF. It affects only the tap and
// never the run — run-level cancellation is Runner.CancelRun.
func (s *EventStream) Cancel() {
	_ = s.Close()
}
