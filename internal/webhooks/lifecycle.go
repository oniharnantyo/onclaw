package webhooks

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/store"
)

// StopJoinWait bounds the listener teardown join (the telegram channel's
// convention): a hung HTTP handler must not block app shutdown past this.
const StopJoinWait = 5 * time.Second

// DefaultAddr is the listener address when no settings row was ever saved:
// loopback, disambiguated from server mode's 8080 (add-webhook-triggers
// design D7).
const DefaultAddr = "127.0.0.1:8787"

// ListenerState is the receiver's readable serving state: the service layer
// surfaces it, and a bind failure reads as Serving=false with BindError set
// (the browser-use lockfile precedent: never a silent dead port).
type ListenerState struct {
	// Serving reports whether the listener currently accepts connections.
	Serving bool
	// BoundAddr is the address the listener actually bound (an ephemeral
	// ":0" bind resolves to its real port here). Empty while not serving.
	BoundAddr string
	// BindError carries the latest bind failure's text, cleared on the next
	// successful bind.
	BindError string
}

// Service owns the receiver's listener lifecycle (add-webhook-triggers
// design D1): it attaches to the gateway the way the scheduler's ticker
// does — subscribe-first, listener up on Running (or already there), kept
// up through Paused so the pause gate can answer 503, closed on Stopped —
// with a bounded teardown, plus a Reconfigure that applies a changed listen
// address without an app restart.
type Service struct {
	rec *Receiver

	mu      sync.Mutex
	att     *attachment     // the one gateway attachment; nil after Stop
	gw      gateway.Manager // the attached gateway, for Reconfigure's state read
	addr    string          // the configured listen address
	srv     *http.Server
	ln      net.Listener
	wg      sync.WaitGroup // tracks the serve goroutine
	bindErr string
}

// WithListenAddr overrides the Service's listener address from DefaultAddr.
// Production wiring leaves the default (design D7); embedders and tests
// point the listener elsewhere — an ephemeral "127.0.0.1:0" binds a free
// port whose resolved address reads back through ListenerState.BoundAddr.
// An empty addr is ignored.
func WithListenAddr(addr string) Option {
	return func(r *Receiver) { r.listenAddr = addr }
}

// NewService builds the webhook service over its receiver's dependencies.
// The listener address defaults to DefaultAddr (WithListenAddr overrides);
// the settings save path calls Reconfigure to apply a changed one.
func NewService(st store.Store, runs Runs, groups GroupSender, opts ...Option) *Service {
	rec := NewReceiver(st, runs, groups, nil, opts...)
	addr := DefaultAddr
	if rec.listenAddr != "" {
		addr = rec.listenAddr
	}
	return &Service{
		rec:  rec,
		addr: addr,
	}
}

// Attach subscribes to the gateway (subscribe-first, the scheduler ticker
// pattern) and starts the listener on the first transition to Running or
// Paused — or immediately when the gateway is already there. Paused keeps
// the listener up: the pipeline's pause gate must be able to answer 503.
// A transition to Stopped closes the listener; a later Running starts a
// fresh one. Attach does not block on the gateway.
func (s *Service) Attach(gw gateway.Manager) {
	// Re-attaching (never done by the composition root) replaces the
	// previous attachment: stop its listener, drop its subscription.
	s.mu.Lock()
	prev := s.att
	s.att = nil
	s.mu.Unlock()
	if prev != nil {
		prev.detach()
	}
	s.stopListener()

	s.rec.setGateway(gw)

	att := &attachment{svc: s}
	cancel := gw.Subscribe(func(_, to gateway.State) { att.onState(to) })
	att.mu.Lock()
	att.sub = cancel
	att.mu.Unlock()

	s.mu.Lock()
	s.att = att
	s.gw = gw
	s.mu.Unlock()

	// Subscribe before reading the state so a concurrent transition cannot
	// fall between the two (the ticker's rule): the listener and this check
	// are both start-on-live, and starting twice is a no-op.
	switch gw.State() {
	case gateway.StateRunning, gateway.StatePaused:
		s.startListener()
	}
}

// Stop closes the listener and detaches from the gateway. It is idempotent
// and returns once the serve goroutine has exited or the bounded join wait
// elapsed. Safe to call before Attach and before the gateway ever runs.
func (s *Service) Stop() {
	s.mu.Lock()
	att := s.att
	s.att = nil
	s.mu.Unlock()
	if att != nil {
		att.detach()
	}
	s.stopListener()
}

// Reconfigure applies a changed listener address without an app restart
// (add-webhook-triggers spec "Receiver lifecycle"): a live listener rebinds
// to the new address; a bound-failed listener retries the bind so a
// corrected address takes effect immediately. An unchanged (or empty)
// address is a no-op. While the gateway is Stopped the listener stays down
// either way.
func (s *Service) Reconfigure(addr string) {
	s.mu.Lock()
	changed := addr != "" && addr != s.addr
	if changed {
		s.addr = addr
	}
	wasServing := s.ln != nil
	gw := s.gw
	s.mu.Unlock()

	if !changed {
		return
	}
	if wasServing {
		s.stopListener()
	}
	if gw != nil && gw.State() != gateway.StateStopped {
		s.startListener()
	}
}

// State snapshots the listener's serving state.
func (s *Service) State() ListenerState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ListenerState{
		Serving:   s.ln != nil,
		BoundAddr: s.boundAddrLocked(),
		BindError: s.bindErr,
	}
}

// Receiver exposes the accept pipeline — the http.Handler tests and
// embedders serve webhook traffic with when they own the listener.
func (s *Service) Receiver() *Receiver { return s.rec }

// attachment is one gateway attachment's live machinery (the scheduler
// ticker pattern).
type attachment struct {
	mu  sync.Mutex
	sub func() // gateway subscription cancel; nil once detached
	svc *Service
}

// onState is the gateway listener: Running and Paused keep the listener up
// (Paused must answer 503 with Retry-After, so the listener serves through a
// pause — this differs from the scheduler, which keeps ticking), Stopped
// closes it.
func (a *attachment) onState(to gateway.State) {
	switch to {
	case gateway.StateRunning, gateway.StatePaused:
		a.svc.startListener()
	case gateway.StateStopped:
		a.svc.stopListener()
	}
}

// detach cancels the gateway subscription, leaving the listener untouched.
func (a *attachment) detach() {
	a.mu.Lock()
	sub := a.sub
	a.sub = nil
	a.mu.Unlock()
	if sub != nil {
		sub()
	}
}

// startListener starts the HTTP listener unless one is already serving. A
// bind failure surfaces as readable state (Serving false + BindError): the
// gateway keeps running and a later Reconfigure or state transition retries.
func (s *Service) startListener() {
	s.mu.Lock()
	if s.ln != nil {
		s.mu.Unlock()
		return
	}
	addr := s.addr
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		s.bindErr = err.Error()
		s.mu.Unlock()
		slog.Warn("webhooks: listener bind failed", "addr", addr, "error", err)
		return
	}
	s.bindErr = ""
	s.ln = ln
	srv := &http.Server{Handler: s.rec}
	s.srv = srv
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Warn("webhooks: listener serve failed", "error", err)
		}
	}()
}

// stopListener closes the listener and joins the serve goroutine, bounded by
// StopJoinWait (in-flight deliveries get connection errors — the normal
// "receiver down" condition every sender already handles). Safe to call
// repeatedly and before any listener started.
func (s *Service) stopListener() {
	s.mu.Lock()
	srv := s.srv
	ln := s.ln
	s.srv = nil
	s.ln = nil
	s.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), StopJoinWait)
		defer cancel()
		_ = srv.Shutdown(ctx) // bounded; in-flight handlers get the deadline
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(StopJoinWait):
		slog.Warn("webhooks: teardown join timed out; a hung handler outlived Stop")
	}
}

// boundAddrLocked resolves the listener's real address; mu must be held.
func (s *Service) boundAddrLocked() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}
