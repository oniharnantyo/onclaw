package whatsappmd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
)

// PairingState is the account/link lifecycle the admin API polls
// (add-whatsapp-gateway tasks 5.4): not_started (no pairing attempted yet),
// waiting (QR/pair code live), connected (a device is linked), logged_out
// (no link — never paired, pairing expired, or explicitly logged out).
type PairingState string

const (
	PairingNotStarted PairingState = "not_started"
	PairingWaiting    PairingState = "waiting"
	PairingConnected  PairingState = "connected"
	PairingLoggedOut  PairingState = "logged_out"
)

// ConnectionState is the live socket state of the linked device (design D10:
// the md-lane health probe surfaces this). Disconnected with a live link is
// recoverable — the supervisor reconnects; a lost link is PairingLoggedOut.
type ConnectionState string

const (
	ConnectionDisconnected ConnectionState = "disconnected"
	ConnectionConnecting   ConnectionState = "connecting"
	ConnectionConnected    ConnectionState = "connected"
)

// pairingWindow approximates whatsmeow's QR lifetime (six rotating codes:
// 60s for the first, 20s each after — ~160s) plus slack, for the initial
// expiry shown before the first code arrives.
const pairingWindow = 165 * time.Second

// pairCodeClient identifies this companion in the pair-code flow.
const pairCodeClientName = "Chrome (Linux)"

// PairingStatus is the pollable pairing state for the admin API (tasks 5.4).
// Plain types only — the whatsmeow-shaped machinery never leaves this
// package (design D6).
type PairingStatus struct {
	State      PairingState
	Connection ConnectionState
	// QRContent is the raw QR payload; QRDataURL the same payload rendered
	// as a PNG data URL (the web pane's preferred form). Populated while
	// State is PairingWaiting.
	QRContent string
	QRDataURL string
	// PairCode is the 8-digit pairing code ("1234-5678") when a phone
	// number was supplied to StartPairing.
	PairCode string
	// PairExpiresAt is when the current QR/code expires (zero if not
	// waiting).
	PairExpiresAt time.Time
	// LinkedNumber is the paired account's phone digits, once linked.
	LinkedNumber string
	// Error carries the last pairing/link failure detail for the pane
	// (protocol reasons only — never session keys or credentials).
	Error string
}

// pairSnapshot is the adapter's pairing scratch state (QR, code, expiry,
// error), reset per attempt.
type pairSnapshot struct {
	qrContent     string
	qrDataURL     string
	pairCode      string
	pairExpiresAt time.Time
	errMsg        string
}

// StartPairing runs the multi-device pairing flow: it starts the QR stream
// and, when phone is a non-empty account phone number, also requests an
// 8-digit pair code (whatsmeow resolves it once the socket is up — the web
// contract shows both, design D12). The call returns immediately; the pane
// polls PairingStatus. Use Regenerate for a fresh attempt or Logout to tear
// the link down.
func (a *Adapter) StartPairing(ctx context.Context, phone string) error {
	a.mu.Lock()
	switch {
	case a.stopped:
		a.mu.Unlock()
		return errors.New("whatsappmd adapter: cannot pair after stop")
	case !a.started:
		a.mu.Unlock()
		return errors.New("whatsappmd adapter: start the adapter before pairing")
	case a.hasSessionLocked():
		a.mu.Unlock()
		return errors.New("whatsappmd adapter: a device is already linked; log out before pairing")
	case a.pairing:
		a.mu.Unlock()
		return errors.New("whatsappmd adapter: pairing already in progress; regenerate instead")
	}
	a.pairGen++
	gen := a.pairGen
	a.pairing = true
	a.state = PairingWaiting
	a.linkDead = false
	a.pairSnapshot = pairSnapshot{pairExpiresAt: time.Now().Add(pairingWindow)}
	digits := jidDigits(phone)
	a.pairPhone = digits
	deviceCtx := a.deviceCtx
	if deviceCtx == nil {
		deviceCtx = context.Background()
	}
	pairCtx, cancel := context.WithCancel(deviceCtx)
	a.pairCancel = cancel
	a.wg.Add(1)
	a.mu.Unlock()

	go a.runPairing(pairCtx, gen, digits)
	return nil
}

// Regenerate cancels any pairing in flight and starts a fresh one (fresh QR
// and, when a phone number was supplied before, a fresh pair code). The
// old pairing socket is dropped first — whatsmeow allows one QR stream per
// connection.
func (a *Adapter) Regenerate(ctx context.Context) error {
	a.mu.Lock()
	phone := a.pairPhone
	a.mu.Unlock()

	a.cancelPairing()
	a.mu.Lock()
	device := a.device
	a.mu.Unlock()
	if device != nil {
		device.Disconnect()
	}
	return a.StartPairing(ctx, phone)
}

// Logout tears the link down: whatsmeow unlinks the device from the
// account, disconnects, and deletes the session rows from the bridged store
// (design D5/D6 — the md lane's only credential is gone after this). When
// the server-side unlink fails, the documented force path still tears the
// socket and local session down, so the gateway never believes a dead link
// is live. With no link, Logout just normalizes the state.
func (a *Adapter) Logout(ctx context.Context) error {
	a.cancelPairing()

	a.mu.Lock()
	device := a.device
	a.mu.Unlock()

	var err error
	if device != nil && device.HasSession() {
		err = device.Logout(ctx)
		if err != nil {
			device.Disconnect()
			if delErr := device.DeleteSession(ctx); delErr != nil {
				err = errors.Join(err, fmt.Errorf("delete device session: %w", delErr))
			}
			err = fmt.Errorf("whatsappmd adapter: logout: %w", err)
		}
	}

	a.mu.Lock()
	a.state = PairingLoggedOut
	a.connection = ConnectionDisconnected
	a.linkDead = false
	a.pairing = false
	a.pairPhone = ""
	a.pairSnapshot = pairSnapshot{}
	a.mu.Unlock()
	return err
}

// PairingStatus snapshots the pairing/link state for the admin API.
func (a *Adapter) PairingStatus() PairingStatus {
	a.mu.Lock()
	st := PairingStatus{
		State:         a.state,
		Connection:    a.connection,
		QRContent:     a.pairSnapshot.qrContent,
		QRDataURL:     a.pairSnapshot.qrDataURL,
		PairCode:      a.pairSnapshot.pairCode,
		PairExpiresAt: a.pairSnapshot.pairExpiresAt,
		Error:         a.pairSnapshot.errMsg,
	}
	a.mu.Unlock()
	a.mu.Lock()
	device := a.device
	a.mu.Unlock()
	if device != nil {
		if jid, ok := device.LinkedJID(); ok {
			st.LinkedNumber = jidDigits(jid.User)
		}
	}
	return st
}

// cancelPairing aborts an in-flight pairing attempt: the attempt token is
// bumped (stale goroutines become no-ops), its context canceled, and the
// waiting snapshot cleared. Unpaired adapters drop back to not_started —
// nothing was ever linked.
func (a *Adapter) cancelPairing() {
	a.mu.Lock()
	a.pairGen++
	a.pairing = false
	cancel := a.pairCancel
	a.pairCancel = nil
	if a.state == PairingWaiting && !a.hasSessionLocked() {
		a.state = PairingNotStarted
	}
	a.pairSnapshot = pairSnapshot{}
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// runPairing drives one pairing attempt: start the QR stream, connect, then
// consume the channel — rendering each rotating code, requesting the pair
// code once the socket is up, and settling the state machine on success,
// timeout, or failure. gen is the attempt token; every state write is
// skipped when a newer attempt (Regenerate) superseded this one.
func (a *Adapter) runPairing(ctx context.Context, gen int, phone string) {
	defer a.wg.Done()

	qrCh, err := a.device.GetQRChannel(ctx)
	if err != nil {
		a.failPairing(gen, fmt.Errorf("start pairing stream: %w", err))
		return
	}
	if err := a.device.Connect(ctx); err != nil {
		a.failPairing(gen, fmt.Errorf("connect for pairing: %w", err))
		return
	}

	// The pair code is requested after the first QR item: the connection is
	// fully established by then (whatsmeow's documented timing).
	codeRequested := phone == ""
	for {
		select {
		case <-ctx.Done():
			// Superseded (Regenerate/Logout/Stop own the state now).
			a.abandonPairing(gen)
			return
		case item, ok := <-qrCh:
			if !ok {
				a.failPairing(gen, errors.New("pairing stream closed unexpectedly"))
				return
			}
			switch {
			case item.Event == whatsmeow.QRChannelEventCode:
				a.storeQR(gen, item)
				if !codeRequested {
					codeRequested = true
					code, err := a.device.PairPhone(ctx, phone)
					if err != nil {
						// The QR stays usable — surface the code failure
						// without failing the whole attempt.
						a.setPairError(gen, fmt.Sprintf("pair code unavailable: %v", err))
					} else {
						a.storePairCode(gen, code)
					}
				}
			case item == whatsmeow.QRChannelSuccess:
				// Pairing complete on the wire; the Connected event settles
				// the state to connected once authentication lands.
				a.finalizePairSuccess(gen)
				return
			case item == whatsmeow.QRChannelTimeout:
				a.failPairing(gen, errors.New("the pairing window expired; start pairing again"))
				return
			case item.Event == whatsmeow.QRChannelEventError || strings.HasPrefix(item.Event, "err-"):
				reason := item.Event
				if item.Error != nil {
					reason = item.Error.Error()
				}
				a.failPairing(gen, fmt.Errorf("pairing failed: %s", reason))
				return
			}
		}
	}
}

// storeQR records a freshly rotated QR code (and its PNG data URL) when the
// attempt still owns the pairing.
func (a *Adapter) storeQR(gen int, item whatsmeow.QRChannelItem) {
	dataURL, err := qrDataURL(item.Code)
	if err != nil {
		slog.Warn("whatsappmd adapter: qr render failed", "gateway_id", a.gatewayID, "err", err)
	}
	a.mu.Lock()
	if a.pairGen != gen {
		a.mu.Unlock()
		return
	}
	a.pairSnapshot.qrContent = item.Code
	a.pairSnapshot.qrDataURL = dataURL
	if item.Timeout > 0 {
		a.pairSnapshot.pairExpiresAt = time.Now().Add(item.Timeout)
	}
	a.mu.Unlock()
}

// storePairCode records the 8-digit pairing code.
func (a *Adapter) storePairCode(gen int, code string) {
	a.mu.Lock()
	if a.pairGen != gen {
		a.mu.Unlock()
		return
	}
	a.pairSnapshot.pairCode = code
	a.mu.Unlock()
}

// setPairError records a soft pairing problem (the attempt continues).
func (a *Adapter) setPairError(gen int, msg string) {
	a.mu.Lock()
	if a.pairGen != gen {
		a.mu.Unlock()
		return
	}
	a.pairSnapshot.errMsg = msg
	a.mu.Unlock()
}

// finalizePairSuccess clears the waiting snapshot after a successful pair;
// the Connected event finishes the state transition to connected.
func (a *Adapter) finalizePairSuccess(gen int) {
	a.mu.Lock()
	if a.pairGen != gen {
		a.mu.Unlock()
		return
	}
	a.pairing = false
	a.pairSnapshot = pairSnapshot{}
	a.mu.Unlock()
}

// failPairing settles a failed/expired attempt: an unpaired account lands on
// logged_out (its terminal, unpaired state) with the reason surfaced to the
// pane.
func (a *Adapter) failPairing(gen int, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pairGen != gen {
		return
	}
	a.pairing = false
	if a.state == PairingWaiting && !a.hasSessionLocked() {
		a.state = PairingLoggedOut
		a.connection = ConnectionDisconnected
		a.pairSnapshot = pairSnapshot{errMsg: err.Error()}
	}
}

// abandonPairing normalizes a pairing whose context died (Stop or a
// superseding operation): no fake waiting QR may survive.
func (a *Adapter) abandonPairing(gen int) {
	a.mu.Lock()
	if a.pairGen != gen {
		a.mu.Unlock()
		return
	}
	a.pairing = false
	if a.state == PairingWaiting && !a.hasSessionLocked() {
		a.state = PairingNotStarted
	}
	a.pairSnapshot = pairSnapshot{}
	a.mu.Unlock()
}

// qrDataURL renders a QR payload as a PNG data URL — the web pane's
// preferred form (design D12).
func qrDataURL(content string) (string, error) {
	img, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		return "", fmt.Errorf("qr encode: %w", err)
	}
	png, err := img.PNG(512)
	if err != nil {
		return "", fmt.Errorf("qr png: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}
