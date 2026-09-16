package whatsappmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types/events"
)

func TestPairingLifecycleTransitions(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	device.scriptPairingCodes("qr-first")
	a := newTestAdapter(t, device)

	// Fresh adapter: pairing never attempted.
	if st := a.PairingStatus(); st.State != PairingNotStarted {
		t.Fatalf("fresh state = %q, want not_started", st.State)
	}

	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Pairing starts: the QR stream goes live (whatsmeow emits the first
	// code right after connecting).
	if err := a.StartPairing(ctx, ""); err != nil {
		t.Fatalf("start pairing: %v", err)
	}
	if st := a.PairingStatus(); st.State != PairingWaiting {
		t.Fatalf("state after StartPairing = %q, want waiting", st.State)
	}
	// A second StartPairing while waiting is refused.
	if err := a.StartPairing(ctx, ""); err == nil {
		t.Error("second StartPairing: expected error")
	}

	waitFor(t, time.Second, func() bool {
		st := a.PairingStatus()
		return st.QRContent == "qr-first" && strings.HasPrefix(st.QRDataURL, "data:image/png;base64,")
	})
	if st := a.PairingStatus(); st.PairExpiresAt.IsZero() {
		t.Error("PairExpiresAt not set while waiting")
	}

	// The phone scans: the link lands and the state settles on connected.
	device.scanQR(userJID("12025550123"))
	waitFor(t, time.Second, func() bool { return a.PairingStatus().State == PairingConnected })
	waitFor(t, time.Second, func() bool { return a.ConnectionState() == ConnectionConnected })
	if st := a.PairingStatus(); st.LinkedNumber != "12025550123" {
		t.Errorf("LinkedNumber = %q, want 12025550123", st.LinkedNumber)
	}

	// Logout tears the link down for good.
	if err := a.Logout(ctx); err != nil {
		t.Fatalf("logout: %v", err)
	}
	st := a.PairingStatus()
	if st.State != PairingLoggedOut {
		t.Errorf("state after logout = %q, want logged_out", st.State)
	}
	if st.LinkedNumber != "" || st.QRContent != "" {
		t.Errorf("logout left stale pairing data: %+v", st)
	}
	if device.HasSession() {
		t.Error("session survived logout")
	}
	if got := a.ConnectionState(); got != ConnectionDisconnected {
		t.Errorf("connection after logout = %q, want disconnected", got)
	}
}

func TestPairCodeFlow(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	device.scriptPairingCodes("qr-code")
	a := newTestAdapter(t, device)
	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	// The phone number normalizes to digits for the pair-code request,
	// resolved after the first QR item (the documented PairPhone timing).
	if err := a.StartPairing(ctx, "+1 (202) 555-0123"); err != nil {
		t.Fatalf("start pairing: %v", err)
	}
	waitFor(t, time.Second, func() bool { return a.PairingStatus().PairCode == "4821-9376" })

	if calls := device.pairPhoneCalls(); len(calls) != 1 || calls[0] != "12025550123" {
		t.Errorf("PairPhone calls = %v, want [12025550123]", calls)
	}
}

func TestPairingRegenerate(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	device.scriptPairingCodes("qr-1", "qr-2")
	a := newTestAdapter(t, device)
	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := a.StartPairing(ctx, ""); err != nil {
		t.Fatalf("start pairing: %v", err)
	}
	waitFor(t, time.Second, func() bool { return a.PairingStatus().QRContent == "qr-1" })

	// Regenerate: cancel + fresh QR, still waiting, still unpaired.
	if err := a.Regenerate(ctx); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if st := a.PairingStatus(); st.State != PairingWaiting {
		t.Fatalf("state after regenerate = %q, want waiting", st.State)
	}
	waitFor(t, time.Second, func() bool { return a.PairingStatus().QRContent == "qr-2" })
	if device.HasSession() {
		t.Error("regenerate linked a device")
	}
}

func TestPairingWindowExpiry(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	a := newTestAdapter(t, device)
	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := a.StartPairing(ctx, ""); err != nil {
		t.Fatalf("start pairing: %v", err)
	}

	device.expireQR()
	waitFor(t, time.Second, func() bool { return a.PairingStatus().State == PairingLoggedOut })
	st := a.PairingStatus()
	if !strings.Contains(st.Error, "expired") {
		t.Errorf("expiry error = %q, want it to mention the expired window", st.Error)
	}
	if got := a.ConnectionState(); got != ConnectionDisconnected {
		t.Errorf("connection = %q, want disconnected", got)
	}

	// An expired pairing is a clean slate: a new attempt is allowed.
	if err := a.StartPairing(ctx, ""); err != nil {
		t.Fatalf("restart pairing after expiry: %v", err)
	}
}

func TestPairingFailureEvent(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	a := newTestAdapter(t, device)
	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := a.StartPairing(ctx, ""); err != nil {
		t.Fatalf("start pairing: %v", err)
	}

	device.failQR("err-scanned-without-multidevice")
	waitFor(t, time.Second, func() bool { return a.PairingStatus().State == PairingLoggedOut })
	if st := a.PairingStatus(); !strings.Contains(st.Error, "scanned-without-multidevice") {
		t.Errorf("failure error = %q, want the protocol reason", st.Error)
	}
}

func TestLogoutForcePathWhenServerRefuses(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	device.link(userJID("12025550123"))
	device.logoutErr = errors.New("unlink refused by server")
	a := newTestAdapter(t, device)
	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	// The documented force path still tears the socket and local session
	// down; the error surfaces to the caller.
	if err := a.Logout(ctx); err == nil {
		t.Fatal("logout: expected the scripted error")
	}
	if device.HasSession() {
		t.Error("local session survived the forced logout")
	}
	if st := a.PairingStatus(); st.State != PairingLoggedOut {
		t.Errorf("state = %q, want logged_out", st.State)
	}
}

func TestRemoteLogoutMarksLinkLost(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	device.link(userJID("12025550123"))
	a := newTestAdapter(t, device)
	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	device.emit(&events.Connected{})
	waitFor(t, time.Second, func() bool { return a.ConnectionState() == ConnectionConnected })

	// The phone unlinked us: the session is gone for this adapter until a
	// fresh pairing.
	device.emit(&events.LoggedOut{})
	st := a.PairingStatus()
	if st.State != PairingLoggedOut {
		t.Errorf("state after LoggedOut = %q, want logged_out", st.State)
	}
	if got := a.ConnectionState(); got != ConnectionDisconnected {
		t.Errorf("connection after LoggedOut = %q, want disconnected", got)
	}
	if !strings.Contains(st.Error, "logged out") {
		t.Errorf("link-lost reason = %q", st.Error)
	}
}

func TestConnectFailureLoggedOutMarksLinkLost(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	device.link(userJID("12025550123"))
	a := newTestAdapter(t, device)
	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	device.emit(&events.ConnectFailure{Reason: events.ConnectFailureLoggedOut})
	waitFor(t, time.Second, func() bool { return a.PairingStatus().State == PairingLoggedOut })
}

func TestSupervisorReconnectsAfterDrop(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	device.link(userJID("12025550123"))
	device.connectErr = errors.New("dial failed: connection refused")
	a := newTestAdapter(t, device)
	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	// First attempts fail; the adapter reports the connecting state.
	waitFor(t, time.Second, func() bool { return device.connectCount() >= 1 })
	if got := a.ConnectionState(); got != ConnectionConnecting {
		t.Errorf("connection during outage = %q, want connecting", got)
	}

	// The network recovers: the backoff loop reconnects.
	device.connectOK()
	waitFor(t, 2*time.Second, func() bool { return device.IsConnected() })
	device.emit(&events.Connected{})
	waitFor(t, time.Second, func() bool { return a.ConnectionState() == ConnectionConnected })

	// A remote drop wakes the supervisor, which restores the socket.
	device.drop()
	waitFor(t, 2*time.Second, func() bool { return device.IsConnected() })
}

func TestStartPairingRequiresStartedAdapter(t *testing.T) {
	a := newTestAdapter(t, newFakeDevice())
	if err := a.StartPairing(context.Background(), ""); err == nil {
		t.Error("StartPairing before Start: expected error")
	}
}

func TestStartPairingRefusedWhenLinked(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	device.link(userJID("12025550123"))
	a := newTestAdapter(t, device)
	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := a.StartPairing(ctx, ""); err == nil {
		t.Error("StartPairing on a linked device: expected error")
	}
}

func TestStopDuringPairingNormalizesState(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	a := newTestAdapter(t, device)
	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := a.StartPairing(ctx, ""); err != nil {
		t.Fatalf("start pairing: %v", err)
	}

	// Stopping mid-pairing aborts the attempt; the admin API must not see a
	// phantom waiting QR afterwards.
	if err := a.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if st := a.PairingStatus(); st.State != PairingNotStarted {
		t.Errorf("state after stop = %q, want not_started", st.State)
	}
	if err := a.StartPairing(ctx, ""); err == nil {
		t.Error("StartPairing after stop: expected error")
	}
}
