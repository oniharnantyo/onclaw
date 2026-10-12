package webhooks

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// The limiter tests run in-package: the window clock is pinned by reaching
// into the unexported now field, the same privilege queue_test.go uses for
// bare Delivery construction.

func TestRateLimiter_AllowsUpToLimitThenBlocks(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)

	for i := 0; i < 3; i++ {
		allowed, retry := rl.Allow("wh-1")
		if !allowed {
			t.Fatalf("allowance %d of 3 denied (retry in %v)", i+1, retry)
		}
		if retry != 0 {
			t.Fatalf("an allowed request must carry no retry-after, got %v", retry)
		}
	}
	allowed, retry := rl.Allow("wh-1")
	if allowed {
		t.Fatal("the 4th request inside the window must be denied")
	}
	if retry <= 0 || retry > time.Minute {
		t.Fatalf("retry-after must be the remainder of the window, got %v", retry)
	}
}

func TestRateLimiter_WindowReset(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	now := time.Unix(1_700_000_000, 0)
	rl.now = func() time.Time { return now }

	if allowed, _ := rl.Allow("wh-1"); !allowed {
		t.Fatal("first request must be allowed")
	}
	if allowed, _ := rl.Allow("wh-1"); allowed {
		t.Fatal("second request inside the window must be denied")
	}

	// Cross the window boundary: the allowance resets.
	now = now.Add(time.Minute)
	if allowed, retry := rl.Allow("wh-1"); !allowed || retry != 0 {
		t.Fatalf("a fresh window must allow again (allowed=%v retry=%v)", allowed, retry)
	}
}

func TestRateLimiter_RetryAfterShrinksWithElapsedTime(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	now := time.Unix(1_700_000_000, 0)
	rl.now = func() time.Time { return now }

	rl.Allow("wh-1")
	_, full := rl.Allow("wh-1")
	now = now.Add(20 * time.Second)
	_, shrunk := rl.Allow("wh-1")
	if shrunk != full-20*time.Second {
		t.Fatalf("retry-after must track elapsed time: full %v, after 20s %v", full, shrunk)
	}
}

func TestRateLimiter_KeysAreIndependent(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	if allowed, _ := rl.Allow("wh-a"); !allowed {
		t.Fatal("wh-a's first request must be allowed")
	}
	if allowed, _ := rl.Allow("wh-b"); !allowed {
		t.Fatal("wh-b must not contend with wh-a's exhausted window")
	}
	if allowed, _ := rl.Allow("wh-a"); allowed {
		t.Fatal("wh-a must still be denied inside its window")
	}
}

func TestRateLimiter_BoundedMemoryEvictsIdleWindows(t *testing.T) {
	rl := NewRateLimiter(StandaloneRateLimit, time.Minute)
	now := time.Unix(1_700_000_000, 0)
	rl.now = func() time.Time { return now }

	// 200 distinct keys, then every window goes idle.
	const keys = 200
	for i := 0; i < keys; i++ {
		rl.Allow(fmt.Sprintf("wh-%03d", i))
	}

	// Advance past every window's lifetime and keep only one key active: the
	// opportunistic sweep its Allows trigger must drop every idle window —
	// only the active key's fresh window survives.
	now = now.Add(2 * time.Minute)
	for i := 0; i < 64; i++ {
		rl.Allow("wh-active")
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if got := len(rl.windows); got != 1 {
		t.Fatalf("only the active key's window must survive the sweep, %d remain", got)
	}
}

func TestRateLimiter_DegenerateConfigFallsBackToDefaults(t *testing.T) {
	rl := NewRateLimiter(0, 0)
	if rl.limit != StandaloneRateLimit || rl.window != StandaloneRateWindow {
		t.Fatalf("degenerate config must fall back to the defaults, got limit=%d window=%v", rl.limit, rl.window)
	}
}

func TestRateLimiter_ConcurrentAllowNeverOvershoots(t *testing.T) {
	rl := NewRateLimiter(8, time.Minute)
	const goroutines = 32
	var allowedCount int
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if allowed, _ := rl.Allow("wh-race"); allowed {
				mu.Lock()
				allowedCount++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if allowedCount != 8 {
		t.Fatalf("exactly 8 of %d concurrent requests must be allowed, got %d", goroutines, allowedCount)
	}
}

func TestRetryAfterSeconds_RoundsUpNeverZero(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want int
	}{
		{0, 1},
		{-time.Second, 1},
		{time.Millisecond, 1},
		{time.Second, 1},
		{time.Second + time.Nanosecond, 2},
		{61 * time.Second, 61},
	}
	for _, tc := range cases {
		if got := RetryAfterSeconds(tc.in); got != tc.want {
			t.Errorf("RetryAfterSeconds(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
