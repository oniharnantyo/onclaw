package membus

import (
	"context"
	"time"
)

// TimerWorker publishes tick events at a configured interval.
// It is not a Worker itself — it is a producer that publishes events to the bus.
type TimerWorker struct {
	bus      *Bus
	interval time.Duration
	event    Event
	stopCh   chan struct{}
}

// NewTimerWorker creates a TimerWorker that publishes the given event at the specified interval.
func NewTimerWorker(bus *Bus, interval time.Duration, event Event) *TimerWorker {
	return &TimerWorker{
		bus:      bus,
		interval: interval,
		event:    event,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the periodic tick publishing. Respects context cancellation.
func (t *TimerWorker) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(t.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				t.bus.Publish(t.event)
			case <-t.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop signals the timer to stop publishing.
func (t *TimerWorker) Stop() {
	close(t.stopCh)
}
