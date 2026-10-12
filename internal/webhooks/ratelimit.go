package webhooks

import (
	"fmt"
	"sync"
	"time"
)

// StandaloneRateLimit is the per-endpoint request allowance per
// StandaloneRateWindow (add-workspace-webhooks design.md D5 defaults): a
// standalone endpoint answers at most 60 verified deliveries per minute with
// runs; excess answers 429 + Retry-After.
const StandaloneRateLimit = 60

// StandaloneRateWindow is the fixed-window size the per-endpoint allowance
// resets on (design.md D5 defaults: 60 req/min per endpoint). The limit is
// in-memory per process for v1 — a documented multi-node softness; the
// durable bounds (queue depth, per-endpoint queue lane) remain the hard
// backstop.
const StandaloneRateWindow = time.Minute

// rateWindow is one key's fixed-window counters: the window start and the
// requests allowed so far inside it.
type rateWindow struct {
	start time.Time
	count int
}

// RateLimiter is the in-memory per-endpoint fixed-window limiter the
// standalone ingress runs after verification (design.md D5): each key —
// the webhook endpoint id — gets its configured limit of allowances per
// window; an exhausted window answers with the time remaining until it
// resets. Thread-safe. Memory stays bounded to the endpoints seen recently:
// idle windows are evicted opportunistically as requests arrive.
type RateLimiter struct {
	limit  int
	window time.Duration

	mu      sync.Mutex
	windows map[string]*rateWindow
	ops     int
	// now is the window clock (tests pin it).
	now func() time.Time
}

// NewRateLimiter builds a fixed-window limiter allowing limit requests per
// window per key. A non-positive limit or window would deny or never reset —
// callers pass the StandaloneRateLimit/StandaloneRateWindow defaults or a
// deliberate override; degenerate values fall back to the defaults rather
// than wiring a permanently-blocked or never-resetting limiter.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	if limit <= 0 {
		limit = StandaloneRateLimit
	}
	if window <= 0 {
		window = StandaloneRateWindow
	}
	return &RateLimiter{
		limit:   limit,
		window:  window,
		windows: make(map[string]*rateWindow),
		now:     time.Now,
	}
}

// Allow reports whether one more request for key fits the current fixed
// window, counting the allowance when it does. When the window is exhausted
// it reports false with the duration until the window resets — the
// transport's Retry-After. Opportunistic maintenance evicts windows idle for
// a full window length, so the map stays bounded by recently-active keys.
func (r *RateLimiter) Allow(key string) (allowed bool, retryAfter time.Duration) {
	now := r.now()

	r.mu.Lock()
	defer r.mu.Unlock()

	r.ops++
	if r.ops%rateLimitEvictEvery == 0 {
		r.evictLocked(now)
	}

	w, ok := r.windows[key]
	if !ok || now.Sub(w.start) >= r.window {
		// First sighting or a fully elapsed window: start fresh. The new
		// window's start is the current instant — a fixed window anchored at
		// first use, not wall-clock boundaries; the guarantee is "at most
		// limit per any window-span", which is the documented soft-limit
		// posture (design.md risks).
		w = &rateWindow{start: now}
		r.windows[key] = w
	}

	if w.count >= r.limit {
		return false, w.start.Add(r.window).Sub(now)
	}
	w.count++
	return true, 0
}

// rateLimitEvictEvery is how many Allow calls run one idle-window sweep —
// opportunistic amortized maintenance, not a background goroutine: the
// limiter is a per-request hot path and a ticker would keep a goroutine
// alive for every process lifetime for no correctness gain (an idle window
// is dead weight, not a leak).
const rateLimitEvictEvery = 64

// evictLocked drops windows idle for a full window length. Caller holds mu.
func (r *RateLimiter) evictLocked(now time.Time) {
	for key, w := range r.windows {
		if now.Sub(w.start) >= r.window {
			delete(r.windows, key)
		}
	}
}

// RetryAfterSeconds renders a retry-after duration for the Retry-After
// header, which carries whole seconds: always round up, never below one —
// a sub-second remainder must not tell the sender "retry now".
func RetryAfterSeconds(d time.Duration) int {
	if d <= 0 {
		return 1
	}
	seconds := int((d + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

// rateLimitDetail is the delivery-log detail recorded when a verified
// delivery lands on an exhausted window.
func rateLimitDetail(retryAfter time.Duration) string {
	return fmt.Sprintf("rate limited, retry after %ds", RetryAfterSeconds(retryAfter))
}
