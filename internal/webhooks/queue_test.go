package webhooks

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// The queue tests run in-package: Delivery is an opaque job type to external
// consumers, and the bounds tests need to construct bare jobs.

func testDelivery(connectionID string) *Delivery {
	return &Delivery{connectionID: connectionID}
}

func TestQueue_DepthBound(t *testing.T) {
	release := make(chan struct{})
	q := NewQueue(2, func(context.Context, *Delivery) { <-release }, WithPerConnectionLimit(1))
	defer func() { close(release); q.Close() }()

	// Fill the backlog to depth with blocked jobs for one connection.
	for i := 0; i < 2; i++ {
		if !q.Enqueue(testDelivery("conn-a")) {
			t.Fatalf("enqueue %d of a depth-2 queue reported full", i+1)
		}
	}
	// The backlog is at depth: the next enqueue is backpressure.
	if q.Enqueue(testDelivery("conn-a")) {
		t.Fatal("enqueue at depth must report false")
	}
}

func TestQueue_PerConnectionLimit(t *testing.T) {
	const limit = 2
	const jobs = 6
	var concurrently, maxSeen, completed atomic.Int32
	q := NewQueue(64, func(context.Context, *Delivery) {
		n := concurrently.Add(1)
		for {
			max := maxSeen.Load()
			if n <= max || maxSeen.CompareAndSwap(max, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		concurrently.Add(-1)
		completed.Add(1)
	}, WithPerConnectionLimit(limit))
	defer q.Close()

	for i := 0; i < jobs; i++ {
		if !q.Enqueue(testDelivery("conn-a")) {
			t.Fatalf("enqueue %d reported full unexpectedly", i+1)
		}
	}
	// Wait for every job to complete, then assert the cap held.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && completed.Load() < jobs {
		time.Sleep(5 * time.Millisecond)
	}
	if got := completed.Load(); got != jobs {
		t.Fatalf("only %d of %d jobs completed", got, jobs)
	}
	if got := maxSeen.Load(); got > limit {
		t.Fatalf("per-connection limit exceeded: %d concurrent for conn-a (limit %d)", got, limit)
	}
	if got := maxSeen.Load(); got < 2 {
		t.Fatalf("expected the limit to be reachable (max concurrent %d)", got)
	}
}

func TestQueue_DistinctConnectionsNeverStarve(t *testing.T) {
	// conn-a's jobs park forever until released; conn-b must still process.
	releaseA := make(chan struct{})
	var bDone = make(chan struct{})
	q := NewQueue(64, func(_ context.Context, job *Delivery) {
		if job.connectionID == "conn-a" {
			<-releaseA
			return
		}
		close(bDone)
	}, WithPerConnectionLimit(1))
	defer func() { close(releaseA); q.Close() }()

	for i := 0; i < 3; i++ {
		if !q.Enqueue(testDelivery("conn-a")) {
			t.Fatalf("enqueue %d reported full unexpectedly", i+1)
		}
	}
	if !q.Enqueue(testDelivery("conn-b")) {
		t.Fatal("a distinct connection must not be rejected behind conn-a's backlog")
	}
	select {
	case <-bDone:
	case <-time.After(2 * time.Second):
		t.Fatal("conn-b starved behind conn-a's parked deliveries")
	}
}

func TestQueue_CloseWaitsForInFlight(t *testing.T) {
	var completed atomic.Int32
	q := NewQueue(8, func(context.Context, *Delivery) {
		time.Sleep(50 * time.Millisecond)
		completed.Add(1)
	})
	if !q.Enqueue(testDelivery("conn-a")) {
		t.Fatal("enqueue failed")
	}
	q.Close()
	if got := completed.Load(); got != 1 {
		t.Fatalf("Close must wait for in-flight work, completed=%d", got)
	}
	// Enqueue after Close reports false.
	if q.Enqueue(testDelivery("conn-a")) {
		t.Fatal("enqueue after Close must report false")
	}
}
