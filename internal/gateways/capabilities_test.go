package gateways

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Capability-matrix tests (add-whatsapp-gateway design D2, tasks 2.2/2.3/2.8a):
// a constructor-parameterized fake adapter drives the streamer and the
// approval bridge through the CanEdit × CanButton cells. The Telegram cell
// (true, true) golden behavior is asserted by the pre-existing streamer and
// approvals tests, which run unchanged.

// -------------------------------------------------------------------------
// Streamer under CanEdit=false: no placeholder, no mid-stream traffic, one
// final outbox-committed reply, typing heartbeat kept.
// -------------------------------------------------------------------------

func TestStreamerWithoutEditSkipsPlaceholderAndEdits(t *testing.T) {
	adapter := newTestPlatformAdapterWithCaps(false, true)
	streamer := NewStreamer(adapter, newTestOutbox(t), WithRenderFlavor(WhatsAppFlavor), WithDebounceInterval(time.Millisecond))
	session := StreamSession{GatewayID: "gw1", WorkspaceID: "ws1", SessionID: "sess1", ChatID: "chat1"}

	// Deltas spaced well beyond the debounce window: a Telegram-shaped run
	// would edit mid-stream, the no-edit platform must send nothing until
	// the final flush.
	stream := agents.NewEventStream(64)
	stream.Send(textDelta("hello **world**"))
	time.Sleep(20 * time.Millisecond)
	stream.Send(textDelta(" and more"))
	stream.Close()

	result := streamer.Stream(context.Background(), session, stream)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}

	sent := adapter.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("no-edit platform must deliver exactly one message, sent: %#v", sent)
	}
	if sent[0].HTML != "hello *world* and more" {
		t.Fatalf("final message must carry the WhatsApp-flavored body, got %q", sent[0].HTML)
	}
	if sent[0].Flavor != FlavorWhatsAppMD {
		t.Fatalf("send must carry the flavor tag, got %q", sent[0].Flavor)
	}
	if len(adapter.editedMessages()) != 0 {
		t.Fatalf("no-edit platform must never edit, edits: %#v", adapter.editedMessages())
	}
	if !strings.Contains(result.Text, "and more") {
		t.Fatalf("result text lost the reply: %q", result.Text)
	}
}

func TestStreamerWithoutEditKeepsTypingHeartbeat(t *testing.T) {
	adapter := newTestPlatformAdapterWithCaps(false, false)
	streamer := NewStreamer(adapter, newTestOutbox(t), WithDebounceInterval(time.Hour), WithTypingInterval(20*time.Millisecond))
	session := StreamSession{ChatID: "chat1"}

	stream := agents.NewEventStream(64)
	go func() {
		for i := 0; i < 4; i++ {
			stream.Send(textDelta("chunk "))
			time.Sleep(30 * time.Millisecond)
		}
		stream.Send(&agents.TranscriptEvent{Kind: agents.TranscriptEventTurnCompleted})
		stream.Close()
	}()

	result := streamer.Stream(context.Background(), session, stream)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if got := adapter.typingCount(); got < 2 {
		t.Fatalf("no-edit platform must keep the typing heartbeat, got %d", got)
	}
}

func TestStreamerWithoutEditCommitsFinalReplyInOutbox(t *testing.T) {
	outboxStore := newOutboxTestStore(t)
	adapter := newTestPlatformAdapterWithCaps(false, true)
	streamer := NewStreamer(adapter, NewOutbox(outboxStore, staticSenders(adapter)), WithRenderFlavor(WhatsAppFlavor), WithDebounceInterval(time.Hour))
	session := StreamSession{GatewayID: "gw1", WorkspaceID: "ws1", SessionID: "sess1", ChatID: "chat1"}

	stream := newTestStream(
		textDelta("final only"),
		&agents.TranscriptEvent{Kind: agents.TranscriptEventTurnCompleted},
	)
	if result := streamer.Stream(context.Background(), session, stream); result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}

	// The send was confirmed inline, so the committed row was marked
	// delivered: a restart claims nothing and re-sends nothing.
	fresh := newTestPlatformAdapterWithCaps(false, true)
	restarted := NewOutbox(outboxStore, staticSenders(fresh))
	delivered, err := restarted.DeliverDue(context.Background(), time.Now().UTC().Add(time.Minute))
	if err != nil {
		t.Fatalf("DeliverDue: %v", err)
	}
	if delivered != 0 || len(fresh.sentMessages()) != 0 {
		t.Fatalf("delivered rows must not be resent: delivered=%d sends=%d", delivered, len(fresh.sentMessages()))
	}
}

// -------------------------------------------------------------------------
// Approval bridge across the capability matrix.
// -------------------------------------------------------------------------

func TestApprovalBridgeCanButtonFalseExposesPendingTextDecision(t *testing.T) {
	bridge, _, _ := newApprovalTestEnvWithCaps(t, true, false) // multi-device cell
	session := approvalTestSession()
	if err := bridge.Present(context.Background(), session, testExecRequest(), agents.ApprovalPayload{InterruptID: "int-1"}); err != nil {
		t.Fatalf("Present: %v", err)
	}

	interruptID, cardID, ok := bridge.PendingTextDecision(session.SessionID)
	if !ok {
		t.Fatalf("text-reply platform must expose the pending card")
	}
	if interruptID != "int-1" {
		t.Fatalf("unexpected interrupt id %q", interruptID)
	}
	if cardID == "" {
		t.Fatalf("card message id must be reported")
	}
}

func TestApprovalBridgeCanButtonTrueNeverInterceptsText(t *testing.T) {
	bridge, _, _ := newApprovalTestEnvWithCaps(t, true, true) // telegram cell
	session := approvalTestSession()
	if err := bridge.Present(context.Background(), session, testExecRequest(), agents.ApprovalPayload{InterruptID: "int-1"}); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if _, _, ok := bridge.PendingTextDecision(session.SessionID); ok {
		t.Fatalf("button platforms decide by callback, never by chat text")
	}
	if _, _, ok := bridge.PendingTextDecision("unknown-session"); ok {
		t.Fatalf("no pending card must be reported for an idle session")
	}
}

func TestApprovalBridgeReceiptFollowUpWhenNoEdit(t *testing.T) {
	bridge, adapter, submitter := newApprovalTestEnvWithCaps(t, false, true) // cloud cell
	ctx := context.Background()
	session := approvalTestSession()
	if err := bridge.Present(ctx, session, testExecRequest(), agents.ApprovalPayload{InterruptID: "int-1", Command: "rm -rf /tmp/x"}); err != nil {
		t.Fatalf("Present: %v", err)
	}

	stream, err := bridge.HandleCallback(ctx, Callback{
		Platform:   PlatformTelegram,
		ChatID:     session.ChatID,
		MessageID:  "card-101",
		FromUserID: "tg-111",
		Data:       EncodeApprovalCallback("int-1", true),
	})
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if stream == nil {
		t.Fatalf("expected the resumed stream")
	}
	if len(submitter.calls()) != 1 || !submitter.calls()[0].Approved {
		t.Fatalf("resume must carry the decision: %#v", submitter.calls())
	}
	// Resolution records the decision as a receipt follow-up — the card
	// cannot be edited on this platform.
	if len(adapter.editedMessages()) != 0 {
		t.Fatalf("no-edit platform must not rewrite the card, edits: %#v", adapter.editedMessages())
	}
	sent := adapter.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].HTML, "approved") || !strings.Contains(sent[0].HTML, "tg-111") {
		t.Fatalf("expected a receipt follow-up naming the decision and actor, sent: %#v", sent)
	}
}

func TestApprovalBridgeCardEditWhenCanEdit(t *testing.T) {
	bridge, adapter, _ := newApprovalTestEnvWithCaps(t, true, false) // multi-device cell
	ctx := context.Background()
	session := approvalTestSession()
	if err := bridge.Present(ctx, session, testExecRequest(), agents.ApprovalPayload{InterruptID: "int-1"}); err != nil {
		t.Fatalf("Present: %v", err)
	}

	if _, err := bridge.HandleCallback(ctx, Callback{
		Platform:   PlatformTelegram,
		ChatID:     session.ChatID,
		MessageID:  "card-101",
		FromUserID: "tg-111",
		Data:       EncodeApprovalCallback("int-1", false),
	}); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	edits := adapter.editedMessages()
	if len(edits) != 1 {
		t.Fatalf("editable platform must record the decision on the card, edits: %#v", edits)
	}
	if !strings.Contains(edits[0].HTML, "denied") || edits[0].MessageID != "card-101" {
		t.Fatalf("decision recorded on the wrong card or body: %#v", edits[0])
	}
	if len(adapter.sentMessages()) != 0 {
		t.Fatalf("editable platform needs no receipt follow-up, sent: %#v", adapter.sentMessages())
	}
}

// -------------------------------------------------------------------------
// Outbox startup conversion: pending rows written before the payload-field
// rename ({"html": ...}) deliver in the new Body+flavor shape
// (add-whatsapp-gateway design D9, Migration Plan step 2).
// -------------------------------------------------------------------------

func TestOutboxLegacyPayloadConvertsOnStartupSweep(t *testing.T) {
	outboxStore := newOutboxTestStore(t)
	ctx := context.Background()

	// A row in the OLD payload shape, written straight to the store the way
	// a pre-upgrade deployment would have left it.
	legacy := []byte(`{"gateway_id":"gw1","chat_id":"chat-42","html":"<b>pre-rename reply</b>","disable_preview":true}`)
	if err := outboxStore.Enqueue(ctx, &domain.OutboxEntry{
		WorkspaceID:  "ws1",
		SessionID:    "sess1",
		Payload:      legacy,
		DeliverAfter: time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	sender := &outboxSender{}
	o := NewOutbox(outboxStore, staticSenders(sender))
	delivered, err := o.DeliverDue(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("DeliverDue: %v", err)
	}
	if delivered != 1 {
		t.Fatalf("delivered = %d, want 1", delivered)
	}
	sends := sender.sent()
	if len(sends) != 1 {
		t.Fatalf("expected exactly one send, got %#v", sends)
	}
	if sends[0].HTML != "<b>pre-rename reply</b>" {
		t.Fatalf("legacy body lost in conversion: %#v", sends[0])
	}
	if sends[0].Flavor != FlavorTelegramHTML {
		t.Fatalf("legacy row must convert to the telegram_html flavor, got %q", sends[0].Flavor)
	}
	if !sends[0].Opts.DisablePreview {
		t.Fatalf("legacy flags must survive the conversion: %#v", sends[0].Opts)
	}
}

func TestOutboxNewPayloadKeepsFlavor(t *testing.T) {
	outboxStore := newOutboxTestStore(t)
	ctx := context.Background()

	sender := &outboxSender{}
	o := NewOutbox(outboxStore, staticSenders(sender))
	if _, err := o.Enqueue(ctx, "gw1", "ws1", "sess1", "chat-9", "*hello*", FlavorWhatsAppMD, SendOptions{}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := o.DeliverDue(ctx, time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatalf("DeliverDue: %v", err)
	}
	sends := sender.sent()
	if len(sends) != 1 || sends[0].Flavor != FlavorWhatsAppMD || sends[0].HTML != "*hello*" {
		t.Fatalf("flavor must round-trip through the payload: %#v", sends)
	}
}
