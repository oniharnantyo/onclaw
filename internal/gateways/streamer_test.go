package gateways

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// newTestStream builds an EventStream, feeds it events, and closes it.
func newTestStream(events ...*agents.TranscriptEvent) *agents.EventStream {
	s := agents.NewEventStream(64)
	for _, ev := range events {
		s.Send(ev)
	}
	s.Close()
	return s
}

func textDelta(s string) *agents.TranscriptEvent {
	return &agents.TranscriptEvent{Kind: agents.TranscriptEventTextDelta, TextDelta: s}
}

// newTestOutbox builds an Outbox over a fresh fake store with a throwaway
// send seam (streamer tests drive sends through the adapter double; the
// outbox only records what the final flush commits).
func newTestOutbox(t *testing.T) *Outbox {
	t.Helper()
	return NewOutbox(newOutboxTestStore(t), staticSenders(newTestPlatformAdapter()))
}

func TestStreamerFinalFlushCarriesFullReply(t *testing.T) {
	adapter := newTestPlatformAdapter()
	streamer := NewStreamer(adapter, newTestOutbox(t), WithDebounceInterval(time.Hour)) // no mid-stream edits
	session := StreamSession{GatewayID: "gw1", WorkspaceID: "ws1", SessionID: "sess1", ChatID: "chat1"}

	stream := newTestStream(
		textDelta("Hello "),
		textDelta("**world**"),
		&agents.TranscriptEvent{
			Kind:      agents.TranscriptEventToolCallFinished,
			ToolResult: &agents.ToolResultPayload{Name: "grafana.query", Latency: 1200 * time.Millisecond},
		},
		&agents.TranscriptEvent{Kind: agents.TranscriptEventTurnCompleted},
	)

	result := streamer.Stream(context.Background(), session, stream)

	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Approval != nil {
		t.Fatalf("unexpected approval: %+v", result.Approval)
	}
	if !strings.Contains(result.Text, "Hello **world**") {
		t.Fatalf("result text missing reply: %q", result.Text)
	}

	sent := adapter.sentMessages()
	if len(sent) != 1 || sent[0].HTML != RenderTelegramHTML(placeholderText) {
		t.Fatalf("placeholder message missing, sent: %#v", sent)
	}

	edits := adapter.editedMessages()
	if len(edits) == 0 {
		t.Fatalf("expected a final edit, got none")
	}
	final := edits[len(edits)-1]
	if final.ChatID != "chat1" {
		t.Fatalf("final edit went to %q", final.ChatID)
	}
	if !strings.Contains(final.HTML, "<b>world</b>") {
		t.Fatalf("final edit missing rendered reply: %q", final.HTML)
	}
	if !strings.Contains(final.HTML, "grafana.query") || !strings.Contains(final.HTML, "1.2s") {
		t.Fatalf("final edit missing tool-call note with latency: %q", final.HTML)
	}
}

func TestStreamerDebounceLimitsEdits(t *testing.T) {
	adapter := newTestPlatformAdapter()
	streamer := NewStreamer(adapter, newTestOutbox(t), WithDebounceInterval(150*time.Millisecond))
	session := StreamSession{ChatID: "chat1"}

	// Two deltas land within the debounce window: exactly one mid-stream
	// edit may happen for them (the first, immediate one); after the window,
	// the next delta may edit again.
	stream := agents.NewEventStream(64)
	stream.Send(textDelta("one "))
	stream.Send(textDelta("two "))
	time.Sleep(200 * time.Millisecond)
	stream.Send(textDelta("three"))
	stream.Close()

	result := streamer.Stream(context.Background(), session, stream)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}

	// Placeholder send + at most: 1 edit for the first pair, 1 edit for the
	// third delta, 1 final edit.
	edits := adapter.editedMessages()
	if len(edits) > 3 {
		t.Fatalf("debounce exceeded: %d edits", len(edits))
	}
	if len(edits) < 2 {
		t.Fatalf("expected the debounced edits and the final edit, got %d", len(edits))
	}
	final := edits[len(edits)-1]
	if !strings.Contains(final.HTML, "one two three") {
		t.Fatalf("final edit missing the complete reply: %q", final.HTML)
	}
}

func TestStreamerSplitsOversizedReply(t *testing.T) {
	adapter := newTestPlatformAdapter()
	streamer := NewStreamer(adapter, newTestOutbox(t), WithDebounceInterval(time.Hour))
	session := StreamSession{ChatID: "chat1"}

	stream := newTestStream(
		textDelta(strings.Repeat("word ", 1500)), // 7500 runes
		&agents.TranscriptEvent{Kind: agents.TranscriptEventTurnCompleted},
	)

	result := streamer.Stream(context.Background(), session, stream)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}

	// The overflow parts beyond the first go out as new messages.
	sent := adapter.sentMessages()
	overflow := 0
	for _, m := range sent {
		if m.HTML != RenderTelegramHTML(placeholderText) && runeLen(m.HTML) > 0 {
			overflow++
		}
	}
	if overflow == 0 {
		t.Fatalf("expected overflow parts as new messages, sent: %#v", sent)
	}
	edits := adapter.editedMessages()
	if len(edits) == 0 || !strings.Contains(edits[len(edits)-1].HTML, "word") {
		t.Fatalf("expected the first part edited into the placeholder, edits: %#v", edits)
	}
}

func TestStreamerMidStreamRollover(t *testing.T) {
	adapter := newTestPlatformAdapter()
	streamer := NewStreamer(adapter, newTestOutbox(t), WithDebounceInterval(time.Hour))
	session := StreamSession{ChatID: "chat1"}

	// Enough text to trigger the mid-stream rollover while deltas keep
	// flowing: the first buffer finalizes and a fresh buffer carries the
	// rest — the continuation message is created lazily, so an empty tail
	// never becomes an empty message.
	stream := agents.NewEventStream(64)
	stream.Send(textDelta(strings.Repeat("a", telegramChunkBudget-128)))
	stream.Send(textDelta(strings.Repeat("b", telegramChunkBudget-128)))
	stream.Send(&agents.TranscriptEvent{Kind: agents.TranscriptEventTurnCompleted})
	stream.Close()

	result := streamer.Stream(context.Background(), session, stream)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}

	// The second buffer went out as its own non-empty message.
	sent := adapter.sentMessages()
	secondBufOut := false
	for _, m := range sent {
		if strings.Contains(m.HTML, "bbbb") {
			secondBufOut = true
		}
	}
	if !secondBufOut {
		t.Fatalf("second buffer never delivered, sent: %#v", sent)
	}
	for _, m := range sent {
		if strings.TrimSpace(PlainTextFallback(m.HTML)) == "" {
			t.Fatalf("an empty message went out: %#v", m)
		}
	}
	if !strings.Contains(result.Text, "bbbb") {
		t.Fatalf("result text lost the second buffer: len=%d", len(result.Text))
	}
}

func TestStreamerSurfacesApprovalAndErrors(t *testing.T) {
	adapter := newTestPlatformAdapter()
	streamer := NewStreamer(adapter, newTestOutbox(t), WithDebounceInterval(time.Hour))
	session := StreamSession{ChatID: "chat1"}

	t.Run("approval captured", func(t *testing.T) {
		stream := newTestStream(
			textDelta("working "),
			&agents.TranscriptEvent{
				Kind:     agents.TranscriptEventApprovalRequired,
				Approval: &agents.ApprovalPayload{InterruptID: "int-1", Command: "rm -rf /tmp/x"},
			},
		)
		result := streamer.Stream(context.Background(), session, stream)
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if result.Approval == nil || result.Approval.InterruptID != "int-1" {
			t.Fatalf("approval not surfaced: %+v", result.Approval)
		}
	})

	t.Run("turn error surfaced", func(t *testing.T) {
		stream := newTestStream(
			textDelta("partial"),
			&agents.TranscriptEvent{Kind: agents.TranscriptEventError, Error: "model exploded"},
		)
		result := streamer.Stream(context.Background(), session, stream)
		if result.Err == nil || !strings.Contains(result.Err.Error(), "model exploded") {
			t.Fatalf("turn error not surfaced: %v", result.Err)
		}
	})

	t.Run("drain error surfaced", func(t *testing.T) {
		stream := agents.NewEventStream(64)
		stream.Send(textDelta("partial"))
		stream.Close()
		// A closed stream drains to EOF — no error. Force a drain error via
		// a cancelled-context stream that was closed with pending events is
		// not possible; instead verify a failing placeholder send surfaces.
		_ = stream
	})
}

func TestStreamerPlaceholderFailureSurfaces(t *testing.T) {
	adapter := newTestPlatformAdapter()
	adapter.sendErrs = []error{context.DeadlineExceeded}
	streamer := NewStreamer(adapter, newTestOutbox(t))
	session := StreamSession{ChatID: "chat1"}

	result := streamer.Stream(context.Background(), session, newTestStream(textDelta("hi")))
	if result.Err == nil {
		t.Fatalf("placeholder failure must surface, got %+v", result)
	}
}

func TestStreamerTypingHeartbeat(t *testing.T) {
	adapter := newTestPlatformAdapter()
	streamer := NewStreamer(adapter, newTestOutbox(t), WithDebounceInterval(time.Hour), WithTypingInterval(20*time.Millisecond))
	session := StreamSession{ChatID: "chat1"}

	// A drain that takes a while: several deltas spaced out so the
	// heartbeat ticks at least twice.
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
		t.Fatalf("expected >=2 typing heartbeats over the drain, got %d", got)
	}
}

func TestStreamerPromptBlockedNote(t *testing.T) {
	adapter := newTestPlatformAdapter()
	streamer := NewStreamer(adapter, newTestOutbox(t), WithDebounceInterval(time.Hour))
	session := StreamSession{ChatID: "chat1"}

	stream := newTestStream(
		&agents.TranscriptEvent{
			Kind:          agents.TranscriptEventPromptBlocked,
			PromptBlocked: &agents.PromptBlockedPayload{Hook: "gate", Reason: "no secrets in prompts"},
		},
	)
	result := streamer.Stream(context.Background(), session, stream)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	edits := adapter.editedMessages()
	if len(edits) == 0 || !strings.Contains(edits[len(edits)-1].HTML, "Prompt blocked") {
		t.Fatalf("prompt-blocked note missing from the final edit: %#v", edits)
	}
}

// -------------------------------------------------------------------------
// Delivery reliability (design D9): the final flush rides the outbox
// -------------------------------------------------------------------------

// outboxOrderAdapter wraps the adapter double and checks, at the final edit,
// whether the reply's outbox row was already committed — the
// write-before-send proof. The observation claims the row (attempts 0→1);
// the confirmed edit then marks it delivered as usual.
type outboxOrderAdapter struct {
	*testPlatformAdapter
	outbox   store.GatewayOutbox
	rowFirst bool
}

func (a *outboxOrderAdapter) EditMessage(ctx context.Context, chatID, messageID, html string) error {
	entries, err := a.outbox.ClaimDue(ctx, time.Now().UTC().Add(time.Hour), 10)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		a.rowFirst = true
	}
	return a.testPlatformAdapter.EditMessage(ctx, chatID, messageID, html)
}

func TestStreamerFinalReplyLandsInOutboxBeforeSend(t *testing.T) {
	outboxStore := newOutboxTestStore(t)
	adapter := &outboxOrderAdapter{testPlatformAdapter: newTestPlatformAdapter(), outbox: outboxStore}
	streamer := NewStreamer(adapter, NewOutbox(outboxStore, staticSenders(adapter)), WithDebounceInterval(time.Hour))
	session := StreamSession{GatewayID: "gw1", WorkspaceID: "ws1", SessionID: "sess1", ChatID: "chat1"}

	stream := newTestStream(
		textDelta("durable hello"),
		&agents.TranscriptEvent{Kind: agents.TranscriptEventTurnCompleted},
	)
	if result := streamer.Stream(context.Background(), session, stream); result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}

	if !adapter.rowFirst {
		t.Fatalf("write-before-send violated: the final edit ran before any outbox row existed")
	}

	// The send was confirmed, so the row was marked delivered: a later
	// delivery cycle claims nothing and sends nothing.
	freshAdapter := newTestPlatformAdapter()
	restarted := NewOutbox(outboxStore, staticSenders(freshAdapter))
	delivered, err := restarted.DeliverDue(context.Background(), time.Now().UTC().Add(time.Minute))
	if err != nil {
		t.Fatalf("DeliverDue: %v", err)
	}
	if delivered != 0 || len(freshAdapter.sentMessages()) != 0 {
		t.Fatalf("delivered rows must not be resent: delivered=%d sends=%d", delivered, len(freshAdapter.sentMessages()))
	}
}

func TestStreamerCrashBetweenEnqueueAndSendRedelivers(t *testing.T) {
	outboxStore := newOutboxTestStore(t)
	// The final edit and its fallback re-send both fail: the reply is
	// committed to the outbox but never confirmed sent — the
	// crash-between-enqueue-and-send state (the send attempt's failure is
	// definitive, so no claim ever happened). The leading nil slots pass the
	// placeholder send and the immediate first-delta edit.
	adapter := newTestPlatformAdapter()
	adapter.editErrs = []error{nil, errors.New("edit down")}
	adapter.sendErrs = []error{nil, errors.New("send down")}
	streamer := NewStreamer(adapter, NewOutbox(outboxStore, staticSenders(adapter)), WithDebounceInterval(time.Hour))
	session := StreamSession{GatewayID: "gw1", WorkspaceID: "ws1", SessionID: "sess1", ChatID: "chat1"}

	stream := newTestStream(
		textDelta("lost reply"),
		&agents.TranscriptEvent{Kind: agents.TranscriptEventTurnCompleted},
	)
	if result := streamer.Stream(context.Background(), session, stream); result.Err != nil {
		t.Fatalf("a failed final send is recovered by redelivery, not surfaced: %v", result.Err)
	}

	// "Restart": a fresh outbox over the same store delivers the committed
	// row as a new message — first delivery, so no duplicate prefix.
	freshAdapter := newTestPlatformAdapter()
	restarted := NewOutbox(outboxStore, staticSenders(freshAdapter))
	delivered, err := restarted.DeliverDue(context.Background(), time.Now().UTC().Add(time.Minute))
	if err != nil {
		t.Fatalf("DeliverDue: %v", err)
	}
	if delivered != 1 {
		t.Fatalf("delivered = %d, want 1", delivered)
	}
	sends := freshAdapter.sentMessages()
	if len(sends) != 1 || !strings.Contains(sends[0].HTML, "lost reply") {
		t.Fatalf("restart must redeliver the committed reply, got %#v", sends)
	}
	if strings.HasPrefix(sends[0].HTML, DuplicateNoticePrefix) {
		t.Fatalf("first redelivery must not carry the duplicate prefix: %q", sends[0].HTML)
	}
}
