package whatsappmd

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/gateways"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// -------------------------------------------------------------------------
// Test harness
// -------------------------------------------------------------------------

// stubHandler records the normalized traffic the adapter delivers.
type stubHandler struct {
	mu       sync.Mutex
	messages []gateways.InboundMessage
}

func (h *stubHandler) HandleMessage(_ context.Context, _ string, msg gateways.InboundMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, msg)
}

func (h *stubHandler) HandleCallback(context.Context, string, gateways.Callback) {}

func (h *stubHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.messages)
}

func (h *stubHandler) last() gateways.InboundMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.messages) == 0 {
		return gateways.InboundMessage{}
	}
	return h.messages[len(h.messages)-1]
}

// newTestAdapter builds an adapter over the fake device with fast
// supervisor backoff, never touching a database.
func newTestAdapter(t *testing.T, device *fakeDevice) *Adapter {
	t.Helper()
	a, err := NewAdapter("gw-wa-1", nil, &stubHandler{},
		withDeviceClient(device),
		WithConnectBackoff(time.Millisecond, 5*time.Millisecond))
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}
	return a
}

// waitFor polls cond until it holds or the timeout elapses (the adapter
// drives pairing and reconnect from background goroutines).
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

// userJID builds a phone-addressed user JID.
func userJID(digits string) types.JID {
	return types.NewJID(digits, types.DefaultUserServer)
}

// dmEvent builds a plain text DM (chat == sender, the 1:1 shape).
func dmEvent(id, chatDigits, text string) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: userJID(chatDigits), Sender: userJID(chatDigits)},
			ID:            types.MessageID(id),
			PushName:      "Oni",
		},
		Message: &waE2E.Message{Conversation: proto.String(text)},
	}
}

// -------------------------------------------------------------------------
// Constructor and lifecycle basics
// -------------------------------------------------------------------------

func TestNewAdapterValidation(t *testing.T) {
	device := newFakeDevice()
	if _, err := NewAdapter("", nil, &stubHandler{}, withDeviceClient(device)); err == nil {
		t.Error("empty gateway id: expected error")
	}
	if _, err := NewAdapter("gw", nil, nil, withDeviceClient(device)); err == nil {
		t.Error("nil handler: expected error")
	}
}

func TestStartStopLifecycle(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	a := newTestAdapter(t, device)

	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Idempotent start.
	if err := a.Start(ctx); err != nil {
		t.Fatalf("second start: %v", err)
	}
	if err := a.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := a.Stop(ctx); err != nil {
		t.Fatalf("second stop: %v", err)
	}
	if err := a.Start(ctx); err == nil {
		t.Error("start after stop: expected error")
	}
	if got := a.ConnectionState(); got != ConnectionDisconnected {
		t.Errorf("connection after stop = %q, want disconnected", got)
	}
}

func TestStartUnpairedStaysIdle(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	a := newTestAdapter(t, device)

	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	// No session: no connect attempts and no phantom state.
	if got := device.connectCount(); got != 0 {
		t.Errorf("unpaired adapter made %d connect attempts, want 0", got)
	}
	if st := a.PairingStatus(); st.State != PairingNotStarted {
		t.Errorf("state = %q, want not_started", st.State)
	}
	if got := a.ConnectionState(); got != ConnectionDisconnected {
		t.Errorf("connection = %q, want disconnected", got)
	}
}

func TestStartLinkedConnects(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	device.link(userJID("12025550123"))
	a := newTestAdapter(t, device)

	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	// A linked device reports the account state immediately (the link is
	// the store truth); the supervisor brings the socket up.
	if st := a.PairingStatus(); st.State != PairingConnected {
		t.Errorf("state = %q, want connected", st.State)
	}
	waitFor(t, time.Second, func() bool { return device.connectCount() >= 1 })
	device.emit(&events.Connected{})
	waitFor(t, time.Second, func() bool { return a.ConnectionState() == ConnectionConnected })
}

func TestCapabilities(t *testing.T) {
	a := newTestAdapter(t, newFakeDevice())
	caps := a.Capabilities()
	if !caps.CanEdit {
		t.Error("CanEdit = false, want true (whatsmeow message edit works)")
	}
	if caps.CanButton {
		t.Error("CanButton = true, want false (native buttons are dead)")
	}
}

// -------------------------------------------------------------------------
// Send seams
// -------------------------------------------------------------------------

func TestFlavorRefusal(t *testing.T) {
	tests := []struct {
		name    string
		flavor  string
		accepted bool
	}{
		{"empty flavor is the adapter's own format", "", true},
		{"whatsapp markdown", gateways.FlavorWhatsAppMD, true},
		{"telegram html refused", gateways.FlavorTelegramHTML, false},
		{"unknown flavor refused", "text/plain", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			device := newFakeDevice()
			a := newTestAdapter(t, device)
			ctx := context.Background()

			_, sendErr := a.SendMessage(ctx, "12025550123", "hi", tc.flavor, gateways.SendOptions{})
			if editErr := a.EditMessage(ctx, "12025550123", "m-1", "hi", tc.flavor); (sendErr == nil) != tc.accepted || (editErr == nil) != tc.accepted {
				t.Errorf("flavor %q: send err=%v edit err=%v, accepted=%v", tc.flavor, sendErr, editErr, tc.accepted)
			}
			if !tc.accepted && len(device.recordedSends()) != 0 {
				t.Errorf("flavor %q: refused body reached the device", tc.flavor)
			}
		})
	}
}

func TestSendMessageTargetsChatAndStopsTyping(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	a := newTestAdapter(t, device)

	id, err := a.SendMessage(ctx, "12025550123", "hello", gateways.FlavorWhatsAppMD, gateways.SendOptions{})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if id == "" {
		t.Fatal("send returned empty message id")
	}
	sends := device.recordedSends()
	if len(sends) != 1 {
		t.Fatalf("recorded %d sends, want 1", len(sends))
	}
	if got := sends[0].To.User; got != "12025550123" {
		t.Errorf("send target user = %q, want 12025550123", got)
	}
	if sends[0].To.Server != types.DefaultUserServer {
		t.Errorf("send target server = %q, want default user server", sends[0].To.Server)
	}
	// The reply ends the composing indicator the heartbeat opened.
	presence := device.recordedPresence()
	if len(presence) != 1 || presence[0].State != types.ChatPresencePaused {
		t.Errorf("presence after send = %+v, want one paused", presence)
	}
}

func TestSendMessageUsesRememberedChatJID(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	a := newTestAdapter(t, device)

	// An inbound LID-addressed chat must be answered on the same server.
	lid := types.NewJID("998877", types.HiddenUserServer)
	a.onMessage(&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: lid, Sender: lid},
			ID:            "m-lid-1",
		},
		Message: &waE2E.Message{Conversation: proto.String("hi from lid")},
	})

	if _, err := a.SendMessage(ctx, "998877", "hi", "", gateways.SendOptions{}); err != nil {
		t.Fatalf("send: %v", err)
	}
	sends := device.recordedSends()
	if len(sends) != 1 || sends[0].To.Server != types.HiddenUserServer {
		t.Errorf("send target = %+v, want the remembered LID server", sends)
	}
}

func TestEditMessageRewritesInPlace(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	a := newTestAdapter(t, device)

	// A card message id from a send...
	cardID, err := a.SendMessage(ctx, "12025550123", "card", "", gateways.SendOptions{})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := a.EditMessage(ctx, "12025550123", cardID, "card, updated", ""); err != nil {
		t.Fatalf("edit: %v", err)
	}
	sends := device.recordedSends()
	if len(sends) != 2 {
		t.Fatalf("recorded %d sends, want 2", len(sends))
	}
	edit := sends[1]
	if edit.EditOf != cardID {
		t.Errorf("edit targets %q, want %q", edit.EditOf, cardID)
	}
	if edit.Text != "card, updated" {
		t.Errorf("edit body = %q", edit.Text)
	}
}

func TestEditMessageRequiresMessageID(t *testing.T) {
	a := newTestAdapter(t, newFakeDevice())
	if err := a.EditMessage(context.Background(), "12025550123", "", "x", ""); err == nil {
		t.Error("edit without message id: expected error")
	}
}

func TestSendTypingMarksReadAndComposes(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	a := newTestAdapter(t, device)

	// Without a tracked inbound message: composing only.
	if err := a.SendTyping(ctx, "12025550123"); err != nil {
		t.Fatalf("typing: %v", err)
	}
	if reads := device.recordedReads(); len(reads) != 0 {
		t.Errorf("mark-read before any inbound = %+v, want none", reads)
	}

	a.onMessage(dmEvent("m-in-1", "12025550123", "question"))
	if err := a.SendTyping(ctx, "12025550123"); err != nil {
		t.Fatalf("typing: %v", err)
	}
	reads := device.recordedReads()
	if len(reads) != 1 {
		t.Fatalf("mark-read calls = %d, want 1", len(reads))
	}
	if len(reads[0].IDs) != 1 || reads[0].IDs[0] != "m-in-1" {
		t.Errorf("marked-read ids = %v, want [m-in-1]", reads[0].IDs)
	}
	if reads[0].Sender.User != "12025550123" {
		t.Errorf("mark-read sender = %q", reads[0].Sender.User)
	}
	presence := device.recordedPresence()
	if len(presence) == 0 || presence[len(presence)-1].State != types.ChatPresenceComposing {
		t.Errorf("presence = %+v, want trailing composing", presence)
	}
}

func TestSendApprovalCardIsPlainInstruction(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	a := newTestAdapter(t, device)

	id, err := a.SendApprovalCard(ctx, "12025550123", agents.ApprovalPayload{InterruptID: "int-1", Command: "rm -rf /tmp/scratch"})
	if err != nil {
		t.Fatalf("approval card: %v", err)
	}
	sends := device.recordedSends()
	if len(sends) != 1 {
		t.Fatalf("recorded %d sends, want 1", len(sends))
	}
	body := sends[0].Text
	if !strings.Contains(body, "rm -rf /tmp/scratch") {
		t.Errorf("card body missing command: %q", body)
	}
	if !strings.Contains(body, "APPROVE") || !strings.Contains(body, "DENY") {
		t.Errorf("card body must instruct replying APPROVE or DENY: %q", body)
	}
	if id == "" {
		t.Error("approval card returned empty message id")
	}
}

func TestDownloadFileRoundTrip(t *testing.T) {
	ctx := context.Background()
	device := newFakeDevice()
	a := newTestAdapter(t, device)

	media := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		FileName:   proto.String("report.pdf"),
		Mimetype:   proto.String("application/pdf"),
		FileLength: proto.Uint64(4),
	}}
	device.download = func(m *waE2E.Message) ([]byte, error) {
		if got := m.GetDocumentMessage().GetFileName(); got != "report.pdf" {
			t.Errorf("downloaded message file name = %q, want report.pdf", got)
		}
		return []byte("data"), nil
	}

	got, err := a.DownloadFile(ctx, mediaRef(media))
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(got) != "data" {
		t.Errorf("download body = %q", got)
	}
}

func TestDownloadFileRefusesGarbageReferences(t *testing.T) {
	a := newTestAdapter(t, newFakeDevice())
	ctx := context.Background()
	if _, err := a.DownloadFile(ctx, ""); err == nil {
		t.Error("empty reference: expected error")
	}
	if _, err := a.DownloadFile(ctx, "!!!not base64!!!"); err == nil {
		t.Error("undecodable reference: expected error")
	}
	if _, err := a.DownloadFile(ctx, "bm90IGEgcHJvdG8="); err == nil {
		t.Error("non-proto payload: expected error")
	}
}
