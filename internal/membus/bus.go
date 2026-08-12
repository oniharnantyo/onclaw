package membus

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	// DefaultBufferSize is the default event channel buffer size.
	DefaultBufferSize = 64
	// drainTimeout is how long Stop waits for buffered events to be processed.
	drainTimeout = 5 * time.Second
)

// workerEntry holds a registered worker and its per-worker channel.
type workerEntry struct {
	worker Worker
	ch     chan Event
}

// Bus is a lightweight in-process message bus for memory domain events.
// It dispatches events to registered workers asynchronously via buffered channels.
type Bus struct {
	bufferSize int
	eventCh    chan Event
	workers    []workerEntry
	subs       map[string][]*workerEntry // event name -> workers
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	once       sync.Once
}

// New creates a new Bus. bufferSize <= 0 defaults to DefaultBufferSize.
func New(bufferSize int) *Bus {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}
	return &Bus{
		bufferSize: bufferSize,
		eventCh:    make(chan Event, bufferSize),
		subs:       make(map[string][]*workerEntry),
	}
}

// Register adds a worker to the bus. Must be called before Start.
func (b *Bus) Register(w Worker) {
	entry := workerEntry{
		worker: w,
		ch:     make(chan Event, b.bufferSize),
	}
	b.workers = append(b.workers, entry)
	for _, name := range w.Subscribes() {
		b.subs[name] = append(b.subs[name], &b.workers[len(b.workers)-1])
	}
}

// Start begins the dispatcher and worker goroutines. Call Stop to shut down.
func (b *Bus) Start(ctx context.Context) {
	ctx, b.cancel = context.WithCancel(ctx)

	// Start per-worker goroutines.
	for i := range b.workers {
		entry := &b.workers[i]
		b.wg.Add(1)
		go b.runWorker(ctx, entry)
	}

	// Start the dispatcher goroutine.
	b.wg.Add(1)
	go b.dispatch(ctx)
}

// Publish sends an event to the bus. Non-blocking: if the buffer is full, the
// event is dropped and a warning is logged.
func (b *Bus) Publish(event Event) {
	select {
	case b.eventCh <- event:
	default:
		slog.Warn("membus: event dropped (buffer full)",
			"event", event.EventName(),
		)
	}
}

// Stop signals all workers to finish, drains remaining events, and waits for
// all goroutines to exit. After Stop returns, no further events are processed.
func (b *Bus) Stop() {
	b.once.Do(func() {
		// Close the event channel to signal dispatcher to drain and exit.
		close(b.eventCh)
		// Wait for dispatcher + workers to finish (with overall timeout).
		done := make(chan struct{})
		go func() {
			b.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(drainTimeout):
			slog.Warn("membus: drain timeout exceeded, cancelling workers")
			b.cancel()
			<-done
		}
	})
}

// dispatch reads from the main event channel and fans out to subscribing workers.
func (b *Bus) dispatch(ctx context.Context) {
	defer b.wg.Done()
	defer b.closeWorkerChannels()

	for event := range b.eventCh {
		entries := b.subs[event.EventName()]
		for _, entry := range entries {
			select {
			case entry.ch <- event:
			default:
				slog.Warn("membus: worker channel full, dropping event",
					"event", event.EventName(),
				)
			}
		}
	}
}

// closeWorkerChannels closes all per-worker channels after the dispatcher exits.
func (b *Bus) closeWorkerChannels() {
	for i := range b.workers {
		close(b.workers[i].ch)
	}
}

// runWorker processes events sequentially for a single worker.
func (b *Bus) runWorker(ctx context.Context, entry *workerEntry) {
	defer b.wg.Done()
	for event := range entry.ch {
		if err := entry.worker.Handle(ctx, event); err != nil {
			slog.Warn("membus: worker error",
				"event", event.EventName(),
				"error", err,
			)
		}
	}
}
