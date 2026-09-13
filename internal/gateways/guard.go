package gateways

import (
	"sync"
	"time"
)

// DefaultLoopThreshold / DefaultLoopCooldown are the bot-loop guard bounds
// (design D10): after the threshold of consecutive bot-originated messages
// in one chat, incoming bot messages are dropped for a cooldown window.
const (
	DefaultLoopThreshold = 20
	DefaultLoopCooldown  = 5 * time.Minute
)

// loopState is one chat's runaway-loop tracking.
type loopState struct {
	consecutive int
	windowStart time.Time
	blockedUntil time.Time
}

// LoopGuard is the per-chat bot-loop guard (design D10): bots talking to
// each other through the gateway can mint unbounded runs. The guard counts
// consecutive bot-originated messages per chat; a human message resets the
// count, and crossing the threshold inside the window drops bot messages for
// the cooldown. Human messages are never dropped.
type LoopGuard struct {
	threshold int
	cooldown  time.Duration
	now       func() time.Time

	mu    sync.Mutex
	chats map[string]*loopState
}

// LoopGuardOption customizes a LoopGuard.
type LoopGuardOption func(*LoopGuard)

// WithLoopThreshold overrides the consecutive bot-message threshold.
func WithLoopThreshold(n int) LoopGuardOption {
	return func(g *LoopGuard) {
		if n > 0 {
			g.threshold = n
		}
	}
}

// WithLoopCooldown overrides the drop window.
func WithLoopCooldown(d time.Duration) LoopGuardOption {
	return func(g *LoopGuard) {
		if d > 0 {
			g.cooldown = d
		}
	}
}

// WithLoopClock overrides the clock (tests).
func WithLoopClock(now func() time.Time) LoopGuardOption {
	return func(g *LoopGuard) { g.now = now }
}

// NewLoopGuard builds the guard with the design defaults (20 consecutive in
// 5 minutes).
func NewLoopGuard(opts ...LoopGuardOption) *LoopGuard {
	g := &LoopGuard{
		threshold: DefaultLoopThreshold,
		cooldown:  DefaultLoopCooldown,
		now:       time.Now,
		chats:     make(map[string]*loopState),
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Allow reports whether the message may proceed to routing. Bot-origin
// messages are counted and dropped once the threshold trips; human messages
// always pass and reset the chat's counter.
func (g *LoopGuard) Allow(msg InboundMessage) bool {
	key := msg.Platform + ":" + msg.ChatID

	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()

	if !msg.FromBot {
		// A human spoke — any runaway loop in this chat is broken.
		delete(g.chats, key)
		return true
	}

	st := g.chats[key]
	if st == nil {
		st = &loopState{windowStart: now}
		g.chats[key] = st
	}
	if now.Before(st.blockedUntil) {
		return false
	}
	if st.windowStart.IsZero() || now.Sub(st.windowStart) > g.cooldown {
		st.windowStart = now
		st.consecutive = 0
	}

	st.consecutive++
	if st.consecutive >= g.threshold {
		// Trip: drop bot messages for the cooldown, then start fresh.
		st.blockedUntil = now.Add(g.cooldown)
		st.consecutive = 0
		st.windowStart = now
		return false
	}
	return true
}

// DefaultBreakerThreshold is the circuit breaker's consecutive-failure
// budget (design D10): a gateway whose platform API calls fail this many
// times in a row has its ingestion auto-paused until an admin resumes it.
const DefaultBreakerThreshold = 5

// breakerState is one gateway's circuit state.
type breakerState struct {
	consecutive int
	open        bool
}

// CircuitBreaker auto-pauses a failing gateway (design D10): consecutive
// platform API failures (adapter sends, polling, file downloads — reported
// by the delivery and ingestion paths) trip the breaker after the threshold;
// only an explicit admin action resumes it. While open, the service refuses
// to mint runs or send messages for that gateway.
type CircuitBreaker struct {
	threshold int

	mu    sync.Mutex
	state map[string]*breakerState
}

// BreakerOption customizes a CircuitBreaker.
type BreakerOption func(*CircuitBreaker)

// WithBreakerThreshold overrides the consecutive-failure threshold.
func WithBreakerThreshold(n int) BreakerOption {
	return func(b *CircuitBreaker) {
		if n > 0 {
			b.threshold = n
		}
	}
}

// NewCircuitBreaker builds the breaker with the design default (5).
func NewCircuitBreaker(opts ...BreakerOption) *CircuitBreaker {
	b := &CircuitBreaker{
		threshold: DefaultBreakerThreshold,
		state:     make(map[string]*breakerState),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Allow reports whether the gateway's ingestion is open.
func (b *CircuitBreaker) Allow(gatewayID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.state[gatewayID]
	return st == nil || !st.open
}

// Open reports whether the gateway is currently tripped.
func (b *CircuitBreaker) Open(gatewayID string) bool {
	return !b.Allow(gatewayID)
}

// RecordSuccess clears the consecutive-failure counter (a success proves the
// platform API is healthy again unless the breaker was already tripped — an
// open breaker stays open; only an admin resumes).
func (b *CircuitBreaker) RecordSuccess(gatewayID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if st := b.state[gatewayID]; st != nil && !st.open {
		st.consecutive = 0
	}
}

// RecordFailure counts a platform API failure and trips the breaker when the
// threshold is crossed. It reports whether this call tripped it (the caller
// surfaces the pause to admins).
func (b *CircuitBreaker) RecordFailure(gatewayID string) (tripped bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.state[gatewayID]
	if st == nil {
		st = &breakerState{}
		b.state[gatewayID] = st
	}
	if st.open {
		return false
	}
	st.consecutive++
	if st.consecutive >= b.threshold {
		st.open = true
		return true
	}
	return false
}

// Resume reopens ingestion for the gateway — the admin-only action (design
// D10: "resume only on explicit admin action"). The failure counter resets.
func (b *CircuitBreaker) Resume(gatewayID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.state, gatewayID)
}
