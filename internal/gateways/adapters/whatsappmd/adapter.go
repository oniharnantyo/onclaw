// Package whatsappmd implements gateways.PlatformAdapter for WhatsApp over
// the multi-device protocol (add-whatsapp-gateway design D1, lane
// "multi_device"): a personal WhatsApp account linked through whatsmeow
// bridges its direct messages into the gateway service. The lane is
// text-first by verified platform facts (design Context): message editing
// and chat-presence typing work, native buttons are dead — so approvals run
// as the plain text-reply card intercepted by the router (design D3) and
// Capabilities reports CanEdit=true, CanButton=false.
//
// Every whatsmeow import lives inside this package; the exported API speaks
// stdlib/plain types only, so cloud-only deployments never pull the tree
// (design D6/D5). The device session is the lane's only credential (design
// D5): it lives in whatsmeow's own sqlstore bridged over the workspace's
// PostgreSQL and never passes through the gateway config.
package whatsappmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/oniharnantyo/onclaw/internal/gateways"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// connectBackoffBase/Max are the reconnect/backoff wrapper's default
// bounds: whatsmeow auto-reconnects transient socket drops itself; the
// supervisor retries the cases where the internal loop gives up (dial
// failures, refused streams that are not logouts). Exponential from base
// to max.
const (
	connectBackoffBase = 2 * time.Second
	connectBackoffMax  = 60 * time.Second
)

// dedupRingSize bounds the inbound message-id ring (whatsmeow delivery is
// mostly idempotent, but reconnects can replay the tail — the ring covers
// the retry window, mirroring the Telegram adapter's ring).
const dedupRingSize = 512

// Adapter implements gateways.PlatformAdapter for one WhatsApp multi-device
// gateway: constructed by the composition root's adapter factory (design
// D10), started/stopped by the gateway lifecycle manager, delivering
// normalized DM traffic to the service through the InboundHandler. Pairing
// (QR / 8-digit code) is a pollable state machine on the adapter for the
// admin API (StartPairing/Regenerate/Logout/PairingStatus).
type Adapter struct {
	gatewayID string
	handler   gateways.InboundHandler
	// db is the composition root's PostgreSQL pool bridged into whatsmeow's
	// sqlstore (design D6). Only the real device path touches it; tests
	// inject a device client instead. The pool is owned by the composition
	// root and never closed here.
	db *sql.DB

	// device is the protocol client — built once at Start (real path) or
	// injected wholesale via the test option. Guarded by mu; never replaced
	// once set.
	device deviceClient

	// connectBackoff are the supervisor's retry bounds (option-tunable for
	// tests).
	connectBackoffBase time.Duration
	connectBackoffMax  time.Duration

	mu           sync.Mutex
	state        PairingState    // account/link lifecycle (pairing.go)
	connection   ConnectionState // live socket state
	pairing      bool            // a pairing goroutine is active
	pairGen      int             // pairing attempt token (stale goroutines no-op)
	pairCancel   context.CancelFunc
	pairPhone    string // phone digits for the pair-code flow, "" = QR only
	pairSnapshot pairSnapshot
	started      bool
	stopped      bool
	linkDead     bool // a permanent disconnect (logout/revoke) ended the link

	// deviceCtx drives the supervisor and pairing goroutines: detached from
	// request contexts (mirrors the Telegram adapter's poll loop).
	deviceCtx context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	// reconnect wakes the supervisor (buffered, coalesced).
	reconnect chan struct{}

	// chatJIDs remembers the last-seen chat JID per normalized digits so
	// replies target the same server (phone vs LID) the message came from.
	// Guarded by mu.
	chatJIDs map[string]types.JID

	// lastInbound per chat feeds the mark-read half of SendTyping
	// (chat-presence typing is tied to it). Guarded by lastInboundMu.
	lastInboundMu sync.Mutex
	lastInbound   map[string]inboundRef

	// Inbound dedup ring. Guarded by seenMu.
	seenMu    sync.Mutex
	seen      map[string]struct{}
	seenOrder []string
}

// inboundRef is the mark-read anchor for one chat: the last inbound message.
type inboundRef struct {
	id     string
	sender types.JID
}

// Option customizes an Adapter.
type Option func(*Adapter)

// WithConnectBackoff overrides the supervisor's retry bounds (tests, tuned
// deployments). Non-positive values are ignored.
func WithConnectBackoff(base, max time.Duration) Option {
	return func(a *Adapter) {
		if base > 0 {
			a.connectBackoffBase = base
		}
		if max > 0 {
			a.connectBackoffMax = max
		}
	}
}

// withDeviceClient replaces the protocol client wholesale (tests inject the
// fake; unexported so the whatsmeow-shaped seam never leaks into other
// packages).
func withDeviceClient(dc deviceClient) Option {
	return func(a *Adapter) { a.device = dc }
}

// NewAdapter builds the WhatsApp multi-device adapter for one gateway. The
// handler is required — the adapter delivers all normalized traffic through
// it. db is the composition root's shared PostgreSQL pool (design D6): the
// real device path bridges whatsmeow's sqlstore over it; it must be scoped
// per gateway (see buildRealDevice) when an instance serves several md
// gateways.
//
// Lifecycle coexistence with the manager (wiring note for the composition
// root): Manager.Sync reconciles config snapshots and no-ops unchanged rows
// (gatewayUnchanged), so ordinary Sync calls never disturb a pairing in
// flight. A disable/enable or lane flip tears the adapter down and rebuilds
// it — Stop cancels the pairing context, so the in-flight pairing aborts
// cleanly and the pane can start a new one after re-enable.
func NewAdapter(gatewayID string, db *sql.DB, handler gateways.InboundHandler, opts ...Option) (*Adapter, error) {
	if gatewayID == "" {
		return nil, fmt.Errorf("%w: whatsappmd adapter needs a gateway id", ErrInvalid)
	}
	if handler == nil {
		return nil, fmt.Errorf("%w: whatsappmd adapter needs an inbound handler", ErrInvalid)
	}
	a := &Adapter{
		gatewayID:          gatewayID,
		handler:            handler,
		db:                 db,
		state:              PairingNotStarted,
		connection:         ConnectionDisconnected,
		connectBackoffBase: connectBackoffBase,
		connectBackoffMax:  connectBackoffMax,
		reconnect:          make(chan struct{}, 1),
		chatJIDs:           make(map[string]types.JID),
		lastInbound:        make(map[string]inboundRef),
		seen:               make(map[string]struct{}),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a, nil
}

// ErrInvalid mirrors domain.ErrInvalid without importing domain — the
// adapter package stays a leaf against the gateway core.
var ErrInvalid = errors.New("invalid request")

// Start implements PlatformAdapter: it builds the device client (bridging
// the sqlstore — design D6), then connects under the supervisor's backoff
// when the store holds a paired session. An unpaired adapter stays idle
// until StartPairing runs the pairing flow — connecting without a session
// and without a pairing stream only earns a server-side refusal. Idempotent
// per instance.
func (a *Adapter) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return errors.New("whatsappmd adapter: cannot start after stop")
	}
	if a.started {
		a.mu.Unlock()
		return nil
	}

	device := a.device
	if device == nil {
		d, err := buildRealDevice(ctx, a.db, slogLogger{module: "whatsmeow"})
		if err != nil {
			a.mu.Unlock()
			return err
		}
		device = d
		a.device = device
	}
	device.SetEventHandler(a.handleEvent)

	a.started = true
	a.deviceCtx, a.cancel = context.WithCancel(context.WithoutCancel(ctx))
	linked := device.HasSession()
	if linked {
		// The link already exists in the store (design D5): the account
		// state is "connected" even while the socket is still coming up —
		// ConnectionState carries the socket truth.
		a.state = PairingConnected
		a.connection = ConnectionConnecting
		a.linkDead = false
	}
	deviceCtx := a.deviceCtx
	if linked {
		a.wg.Add(1)
	}
	a.mu.Unlock()

	if linked {
		go a.connectSupervisor(deviceCtx)
	}
	return nil
}

// Stop implements PlatformAdapter: cancel the supervisor/pairing goroutines,
// wait for them, and disconnect. The device session stays in the store —
// Logout is the explicit teardown (design D5/D6).
func (a *Adapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return nil
	}
	a.stopped = true
	cancel := a.cancel
	device := a.device
	if a.state == PairingWaiting && !a.hasSessionLocked() {
		// A pairing died with the adapter: normalize so the admin API never
		// reports a phantom waiting QR.
		a.state = PairingNotStarted
		a.pairSnapshot = pairSnapshot{}
		a.pairing = false
	}
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if device != nil {
		device.Disconnect()
	}
	a.mu.Lock()
	a.connection = ConnectionDisconnected
	a.mu.Unlock()
	return nil
}

// Capabilities implements PlatformAdapter (add-whatsapp-gateway design D2):
// the multi-device lane edits messages in place (whatsmeow message edit) but
// native buttons are dead (design Context) — approvals run as the plain
// text-reply card the router intercepts (design D3).
func (a *Adapter) Capabilities() gateways.AdapterCapabilities {
	return gateways.AdapterCapabilities{CanEdit: true, CanButton: false}
}

// ConnectionState reports the live socket state of the linked device
// (add-whatsapp-gateway design D10: the md-lane health probe reads this).
func (a *Adapter) ConnectionState() ConnectionState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.connection
}

// connectSupervisor keeps the linked device's socket up: it retries connect
// with capped exponential backoff whenever the device is expected to be
// linked but disconnected. whatsmeow auto-reconnects transient drops
// internally; this wrapper covers the cases where the internal loop gives
// up. Runs until the adapter's device context is canceled.
func (a *Adapter) connectSupervisor(ctx context.Context) {
	defer a.wg.Done()
	delay := a.connectBackoffBase
	for {
		a.attemptConnect()
		select {
		case <-ctx.Done():
			return
		case <-a.reconnect:
			// An event (connected/disconnected) refreshed the picture:
			// restart the backoff ladder.
			delay = a.connectBackoffBase
		case <-time.After(delay):
			if delay < a.connectBackoffMax {
				delay *= 2
			}
		}
		if a.isConnected() {
			delay = a.connectBackoffBase
		}
	}
}

// attemptConnect performs one supervisor connect attempt, skipping it while
// a pairing owns the socket, the link is gone, or the device is already up.
func (a *Adapter) attemptConnect() {
	a.mu.Lock()
	device := a.device
	busy := a.pairing || a.linkDead || !a.started
	a.mu.Unlock()
	if device == nil || busy || !device.HasSession() || device.IsConnected() {
		return
	}
	a.setConnection(ConnectionConnecting)
	if err := device.Connect(a.deviceContext()); err != nil {
		slog.Warn("whatsappmd adapter: connect failed",
			"gateway_id", a.gatewayID, "err", err)
	}
}

// deviceContext returns the adapter's device context (a.deviceCtx snapshot
// with Background fallback for pre-Start calls).
func (a *Adapter) deviceContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deviceCtx == nil {
		return context.Background()
	}
	return a.deviceCtx
}

// hasSessionLocked reports a paired device identity; a.mu must be held.
func (a *Adapter) hasSessionLocked() bool {
	return a.device != nil && a.device.HasSession()
}

// setConnection updates the socket state.
func (a *Adapter) setConnection(c ConnectionState) {
	a.mu.Lock()
	a.connection = c
	a.mu.Unlock()
}

// isConnected snapshots the device's live socket state.
func (a *Adapter) isConnected() bool {
	a.mu.Lock()
	device := a.device
	a.mu.Unlock()
	return device != nil && device.IsConnected()
}

// wakeReconnect nudges the supervisor (non-blocking, coalesced).
func (a *Adapter) wakeReconnect() {
	select {
	case a.reconnect <- struct{}{}:
	default:
	}
}

// handleEvent is the whatsmeow event pump: connection lifecycle maintains
// the adapter's state machine, messages normalize into gateway traffic.
func (a *Adapter) handleEvent(evt any) {
	switch e := evt.(type) {
	case *events.Message:
		a.onMessage(e)
	case *events.Connected:
		a.mu.Lock()
		a.connection = ConnectionConnected
		if a.hasSessionLocked() && a.state != PairingLoggedOut {
			a.state = PairingConnected
		}
		a.mu.Unlock()
		a.wakeReconnect()
	case *events.Disconnected:
		a.mu.Lock()
		a.connection = ConnectionDisconnected
		a.mu.Unlock()
		a.wakeReconnect()
	case *events.LoggedOut:
		a.markLinkLost("the linked device was logged out")
	case *events.StreamReplaced:
		a.markLinkLost("the session was taken over by another client")
	case *events.ClientOutdated:
		a.markLinkLost("the client is outdated")
	case *events.ConnectFailure:
		a.mu.Lock()
		a.connection = ConnectionDisconnected
		dead := e.Reason.IsLoggedOut()
		if dead {
			a.linkDead = true
		}
		a.mu.Unlock()
		if dead {
			a.markLinkLost(e.Reason.String())
			return
		}
		a.wakeReconnect()
	}
}

// markLinkLost records that the account link itself ended (logout, revoke,
// takeover): the pairing state machine drops to logged_out and the
// supervisor stops retrying — only a fresh pairing restores the link.
func (a *Adapter) markLinkLost(reason string) {
	a.mu.Lock()
	a.linkDead = true
	a.state = PairingLoggedOut
	a.connection = ConnectionDisconnected
	a.pairing = false
	a.pairSnapshot = pairSnapshot{errMsg: reason}
	a.mu.Unlock()
}
