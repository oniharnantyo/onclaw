package whatsappmd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// fakeDevice is the in-memory deviceClient for tests (tasks 5.5): every
// protocol call is recorded or scripted, pairing runs through a
// fake-controlled QR channel, and events are dispatched by hand — the real
// protocol is never spun. Test-owned doubles in this package carry the
// fake prefix to stay distinct from any other doubles.

// fakeSend is one recorded outbound message.
type fakeSend struct {
	To     types.JID
	Text   string
	EditOf string // non-empty when the send was an edit of this message id
}

// fakePresence is one recorded chat-presence change.
type fakePresence struct {
	To    types.JID
	State types.ChatPresence
}

// fakeRead is one recorded mark-read call.
type fakeRead struct {
	IDs    []types.MessageID
	Chat   types.JID
	Sender types.JID
}

// fakeDevice implements deviceClient.
type fakeDevice struct {
	mu sync.Mutex

	// State knobs.
	session   *types.JID
	connected bool
	loggedIn  bool

	// Scripting hooks.
	connectErr error // sticky connect failure; clear to resume
	logoutErr  error // scripted Logout failure (exercises the force path)

	// Recordings.
	connectCalls int
	sends        []fakeSend
	presence     []fakePresence
	reads        []fakeRead
	pairPhones   []string

	// Pairing fixtures. Each GetQRChannel attempt gets a fresh channel and
	// a fresh connect gate; Connect closes the gate and auto-pushes the
	// next scripted code — mirroring whatsmeow, where the server emits the
	// first QR right after connecting. This keeps protocol order
	// deterministic: codes never precede their attempt's registration.
	qrCh        chan whatsmeow.QRChannelItem
	connectGate chan struct{}
	gateClosed  bool
	codes       []string
	codeIdx     int
	pairCode    string
	handler     func(evt any)
	download    func(msg *waE2E.Message) ([]byte, error)
}

func newFakeDevice() *fakeDevice {
	return &fakeDevice{
		pairCode: "4821-9376",
	}
}

// scriptPairingCodes sets the first-QR codes successive pairing attempts
// auto-emit on connect.
func (f *fakeDevice) scriptPairingCodes(codes ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes = codes
	f.codeIdx = 0
}

func (f *fakeDevice) HasSession() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.session != nil
}

func (f *fakeDevice) Connect(_ context.Context) error {
	f.mu.Lock()
	f.connectCalls++
	err := f.connectErr
	gate := f.connectGate
	already := f.gateClosed
	f.gateClosed = true
	var code string
	var ch chan whatsmeow.QRChannelItem
	if err == nil && f.codeIdx < len(f.codes) {
		code = f.codes[f.codeIdx]
		f.codeIdx++
		ch = f.qrCh
	}
	f.connected = true
	if f.session != nil {
		f.loggedIn = true
	}
	f.mu.Unlock()
	if err != nil {
		return err
	}
	// The socket is up: QR/pair-code traffic may now flow.
	if gate != nil && !already {
		close(gate)
	}
	if code != "" {
		// whatsmeow: the first QR arrives right after connecting.
		ch <- whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventCode, Code: code, Timeout: 60 * time.Second}
	}
	return nil
}

func (f *fakeDevice) Disconnect() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected = false
	f.loggedIn = false
}

func (f *fakeDevice) IsConnected() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connected
}

func (f *fakeDevice) IsLoggedIn() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loggedIn
}

func (f *fakeDevice) Logout(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.logoutErr != nil {
		return f.logoutErr
	}
	f.connected = false
	f.loggedIn = false
	f.session = nil
	return nil
}

func (f *fakeDevice) DeleteSession(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.session = nil
	return nil
}

func (f *fakeDevice) LinkedJID() (types.JID, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.session == nil {
		return types.JID{}, false
	}
	return *f.session, true
}

func (f *fakeDevice) SendMessage(_ context.Context, to types.JID, message *waE2E.Message) (whatsmeow.SendResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	send := fakeSend{To: to}
	switch {
	case message.GetEditedMessage() != nil:
		proto := message.GetEditedMessage().GetMessage().GetProtocolMessage()
		send.EditOf = proto.GetKey().GetID()
		send.Text = proto.GetEditedMessage().GetConversation()
	default:
		send.Text = message.GetConversation()
	}
	f.sends = append(f.sends, send)
	return whatsmeow.SendResponse{ID: types.MessageID(fmt.Sprintf("out-%d", len(f.sends))), Timestamp: time.Now()}, nil
}

func (f *fakeDevice) BuildEdit(chat types.JID, id types.MessageID, newContent *waE2E.Message) *waE2E.Message {
	return &waE2E.Message{
		EditedMessage: &waE2E.FutureProofMessage{
			Message: &waE2E.Message{
				ProtocolMessage: &waE2E.ProtocolMessage{
					Key: &waCommon.MessageKey{
						FromMe:    proto.Bool(true),
						ID:        proto.String(string(id)),
						RemoteJID: proto.String(chat.String()),
					},
					EditedMessage: newContent,
				},
			},
		},
	}
}

func (f *fakeDevice) SendChatPresence(_ context.Context, jid types.JID, state types.ChatPresence, _ types.ChatPresenceMedia) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.presence = append(f.presence, fakePresence{To: jid, State: state})
	return nil
}

func (f *fakeDevice) MarkRead(_ context.Context, ids []types.MessageID, _ time.Time, chat, sender types.JID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, fakeRead{IDs: ids, Chat: chat, Sender: sender})
	return nil
}

func (f *fakeDevice) DownloadAny(_ context.Context, msg *waE2E.Message) ([]byte, error) {
	f.mu.Lock()
	download := f.download
	f.mu.Unlock()
	if download == nil {
		return nil, fmt.Errorf("fake device: no download body for %T", msg)
	}
	return download(msg)
}

func (f *fakeDevice) GetQRChannel(context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.session != nil {
		return nil, errFakeQRStoreContainsID
	}
	// A fresh channel and gate per attempt mirrors whatsmeow: a regenerated
	// pairing never shares its stream with the canceled attempt.
	f.qrCh = make(chan whatsmeow.QRChannelItem, 8)
	f.connectGate = make(chan struct{})
	f.gateClosed = false
	return f.qrCh, nil
}

// errFakeQRStoreContainsID mirrors whatsmeow's ErrQRStoreContainsID in the
// fake (a fresh whatsmeow error type would be private upstream).
var errFakeQRStoreContainsID = fmt.Errorf("fake device: store contains a device identity")

func (f *fakeDevice) PairPhone(_ context.Context, phone string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pairPhones = append(f.pairPhones, phone)
	return f.pairCode, nil
}

func (f *fakeDevice) SetEventHandler(handler func(evt any)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handler = handler
}

// -------------------------------------------------------------------------
// Test-side controls
// -------------------------------------------------------------------------

// link simulates a paired account.
func (f *fakeDevice) link(jid types.JID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.session = &jid
}

// connectOK clears a scripted connect failure.
func (f *fakeDevice) connectOK() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connectErr = nil
}

// connectCount snapshots the number of connect attempts (supervisor
// assertions).
func (f *fakeDevice) connectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connectCalls
}

// pairPhoneCalls snapshots the phone numbers PairPhone was called with.
func (f *fakeDevice) pairPhoneCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.pairPhones))
	copy(out, f.pairPhones)
	return out
}

// drop simulates a remote disconnect: the socket dies and whatsmeow emits
// its Disconnected event.
func (f *fakeDevice) drop() {
	f.mu.Lock()
	f.connected = false
	f.loggedIn = false
	f.mu.Unlock()
	f.emit(&events.Disconnected{})
}

// emit dispatches one event to the adapter's handler (synchronous).
func (f *fakeDevice) emit(evt any) {
	f.mu.Lock()
	handler := f.handler
	f.mu.Unlock()
	if handler != nil {
		handler(evt)
	}
}

// pushQR delivers one QR-channel item after the current attempt's Connect
// ran (the gate closed). Tests call this for events that follow the first
// code — success, timeout, errors.
func (f *fakeDevice) pushQR(item whatsmeow.QRChannelItem) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		gate, ch := f.connectGate, f.qrCh
		f.mu.Unlock()
		if gate != nil && ch != nil {
			select {
			case <-gate:
				ch <- item
				return
			case <-time.After(2 * time.Second):
			}
		}
		if time.Now().After(deadline) {
			panic("fake device: pushQR with no pairing connect within 2s")
		}
		time.Sleep(time.Millisecond)
	}
}

// scanQR simulates a completed scan: the session is stored, the QR channel
// closes with success, and the client reports connected+logged-in.
func (f *fakeDevice) scanQR(jid types.JID) {
	f.link(jid)
	f.mu.Lock()
	f.connected = true
	f.loggedIn = true
	f.mu.Unlock()
	f.pushQR(whatsmeow.QRChannelSuccess)
	f.emit(&events.Connected{})
}

// expireQR simulates the pairing window running out.
func (f *fakeDevice) expireQR() {
	f.pushQR(whatsmeow.QRChannelTimeout)
}

// failQR simulates a pairing error (e.g. scanned without multi-device).
func (f *fakeDevice) failQR(event string) {
	f.pushQR(whatsmeow.QRChannelItem{Event: event})
}

// drainQR empties the fake QR channel (post-test hygiene between attempts).
func (f *fakeDevice) drainQR() {
	for {
		select {
		case <-f.qrCh:
		default:
			return
		}
	}
}

// recordedSends snapshots the send log.
func (f *fakeDevice) recordedSends() []fakeSend {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeSend, len(f.sends))
	copy(out, f.sends)
	return out
}

// recordedPresence snapshots the chat-presence log.
func (f *fakeDevice) recordedPresence() []fakePresence {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakePresence, len(f.presence))
	copy(out, f.presence)
	return out
}

// recordedReads snapshots the mark-read log.
func (f *fakeDevice) recordedReads() []fakeRead {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeRead, len(f.reads))
	copy(out, f.reads)
	return out
}
