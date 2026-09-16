package gateways

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// outboxSender records SendMessage calls; failures are injectable per call.
type outboxSender struct {
	mu      sync.Mutex
	sends   []testSentMessage
	sendErr []error
}

func (s *outboxSender) SendMessage(ctx context.Context, chatID, body, flavor string, opts SendOptions) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sendErr) > 0 {
		err := s.sendErr[0]
		s.sendErr = s.sendErr[1:]
		if err != nil {
			return "", err
		}
	}
	s.sends = append(s.sends, testSentMessage{ChatID: chatID, HTML: body, Flavor: flavor, Opts: opts})
	return "sent-" + string(rune('a'+len(s.sends))), nil
}

func (s *outboxSender) sent() []testSentMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]testSentMessage(nil), s.sends...)
}

// staticSenders resolves every gateway id to one send seam (tests).
func staticSenders(s MessageSender) SenderResolver {
	return func(string) (MessageSender, bool) { return s, true }
}

func newOutboxTestStore(t *testing.T) store.GatewayOutbox {
	t.Helper()
	st := fake.New()
	if err := st.Workspaces().Create(context.Background(), domainWorkspace("ws1")); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return st.GatewayOutbox()
}

func TestOutboxCrashBetweenGenerateAndSendRedelivers(t *testing.T) {
	outboxStore := newOutboxTestStore(t)
	ctx := context.Background()

	// "Instance 1": the reply is generated and written to the outbox, then
	// the process crashes before any delivery cycle runs.
	firstSender := &outboxSender{}
	first := NewOutbox(outboxStore, staticSenders(firstSender))
	if _, err := first.Enqueue(ctx, "gw1", "ws1", "sess1", "chat-42", "<b>The answer</b>", FlavorTelegramHTML, SendOptions{}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(firstSender.sent()) != 0 {
		t.Fatalf("write-before-send: nothing may go out inside Enqueue")
	}

	// "Instance 2" (restart): a fresh service over the same store delivers
	// the committed-but-unsent row on its first cycle.
	secondSender := &outboxSender{}
	second := NewOutbox(outboxStore, staticSenders(secondSender))
	delivered, err := second.DeliverDue(ctx, time.Now().UTC().Add(time.Minute))
	if err != nil {
		t.Fatalf("DeliverDue: %v", err)
	}
	if delivered != 1 {
		t.Fatalf("delivered = %d, want 1", delivered)
	}
	sends := secondSender.sent()
	if len(sends) != 1 {
		t.Fatalf("expected exactly one send, got %#v", sends)
	}
	if sends[0].ChatID != "chat-42" || !strings.Contains(sends[0].HTML, "The answer") {
		t.Fatalf("redelivered payload wrong: %#v", sends[0])
	}
	if strings.HasPrefix(sends[0].HTML, DuplicateNoticePrefix) {
		t.Fatalf("first delivery must not carry the duplicate prefix: %q", sends[0].HTML)
	}
}

func TestOutboxDuplicateWarningOnAmbiguousRedelivery(t *testing.T) {
	outboxStore := newOutboxTestStore(t)
	ctx := context.Background()

	// First send attempt fails after the row was claimed: the outcome of
	// that attempt is ambiguous, so the redelivery warns in the chat.
	sender := &outboxSender{sendErr: []error{errors.New("boom")}}
	o := NewOutbox(outboxStore, staticSenders(sender), WithOutboxBackoff(time.Second))
	if _, err := o.Enqueue(ctx, "gw1", "ws1", "sess1", "chat-42", "the reply", FlavorTelegramHTML, SendOptions{}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	now := time.Now().UTC().Add(time.Minute)
	if _, err := o.DeliverDue(ctx, now); err != nil {
		t.Fatalf("DeliverDue (failing): %v", err)
	}
	if got := len(sender.sent()); got != 0 {
		t.Fatalf("failed send must not record a delivery, got %d", got)
	}

	// Backoff passed: the retry succeeds and must carry the prefix.
	sender.sendErr = nil
	delivered, err := o.DeliverDue(ctx, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("DeliverDue (retry): %v", err)
	}
	if delivered != 1 {
		t.Fatalf("retry delivered = %d, want 1", delivered)
	}
	sends := sender.sent()
	if len(sends) != 1 || !strings.HasPrefix(sends[0].HTML, DuplicateNoticePrefix) {
		t.Fatalf("ambiguous redelivery must carry the duplicate prefix: %#v", sends)
	}
}

func TestOutboxAttemptBudgetExhausts(t *testing.T) {
	outboxStore := newOutboxTestStore(t)
	ctx := context.Background()

	sender := &outboxSender{sendErr: []error{
		errors.New("1"), errors.New("2"), errors.New("3"), errors.New("4"), errors.New("5"),
	}}
	o := NewOutbox(outboxStore, staticSenders(sender), WithOutboxBackoff(time.Second))
	if _, err := o.Enqueue(ctx, "gw1", "ws1", "sess1", "chat-42", "payload", FlavorTelegramHTML, SendOptions{}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	now := time.Now().UTC().Add(time.Minute)
	// Five failing cycles exhaust the budget (Attempts 1..5).
	for i := 0; i < 5; i++ {
		if _, err := o.DeliverDue(ctx, now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
	}
	if got := len(sender.sent()); got != 0 {
		t.Fatalf("no send may have succeeded, got %d", got)
	}

	// The entry is dead: the next cycle neither claims nor sends it.
	sender.sendErr = nil
	delivered, err := o.DeliverDue(ctx, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("DeliverDue after budget: %v", err)
	}
	if delivered != 0 || len(sender.sent()) != 0 {
		t.Fatalf("dead entry must never be retried: delivered=%d sends=%d", delivered, len(sender.sent()))
	}
}

func TestOutboxFreshnessWindowMarksDead(t *testing.T) {
	outboxStore := newOutboxTestStore(t)
	ctx := context.Background()

	o := NewOutbox(outboxStore, staticSenders(&outboxSender{}))
	if _, err := o.Enqueue(ctx, "gw1", "ws1", "sess1", "chat-42", "stale reply", FlavorTelegramHTML, SendOptions{}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Pretend the row was written 25 hours ago: past the freshness window it
	// is marked dead on its first claim, never sent.
	entries, err := outboxStore.ClaimDue(ctx, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("pre-claim reset: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected the entry claimable, got %d", len(entries))
	}
	sender := &outboxSender{}
	stale := NewOutbox(outboxStore, staticSenders(sender), WithOutboxFreshness(time.Second))
	delivered, err := stale.DeliverDue(ctx, time.Now().UTC().Add(25*time.Hour))
	if err != nil {
		t.Fatalf("DeliverDue: %v", err)
	}
	if delivered != 0 || len(sender.sent()) != 0 {
		t.Fatalf("stale entry must not be delivered: %d %d", delivered, len(sender.sent()))
	}
}

func TestOutboxPruneDeliveredAfterRetention(t *testing.T) {
	outboxStore := newOutboxTestStore(t)
	ctx := context.Background()

	o := NewOutbox(outboxStore, staticSenders(&outboxSender{}))
	if _, err := o.Enqueue(ctx, "gw1", "ws1", "sess1", "chat-42", "old reply", FlavorTelegramHTML, SendOptions{}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := o.DeliverDue(ctx, time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatalf("DeliverDue: %v", err)
	}

	// Nothing pruned within retention.
	n, err := o.Prune(ctx, time.Now().UTC())
	if err != nil || n != 0 {
		t.Fatalf("fresh delivered row must survive: pruned=%d err=%v", n, err)
	}
	// Past the 7-day retention it goes.
	n, err = o.Prune(ctx, time.Now().UTC().Add(8*24*time.Hour))
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n != 1 {
		t.Fatalf("pruned = %d, want 1", n)
	}
}

func TestOutboxPayloadRoundTrip(t *testing.T) {
	outboxStore := newOutboxTestStore(t)
	ctx := context.Background()

	sender := &outboxSender{}
	o := NewOutbox(outboxStore, staticSenders(sender))
	if _, err := o.Enqueue(ctx, "gw1", "ws1", "sess1", "chat-7", "<i>html</i>", FlavorTelegramHTML, SendOptions{DisablePreview: true}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := o.DeliverDue(ctx, time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatalf("DeliverDue: %v", err)
	}
	sends := sender.sent()
	if len(sends) != 1 {
		t.Fatalf("sends = %d", len(sends))
	}
	if sends[0].ChatID != "chat-7" || sends[0].HTML != "<i>html</i>" || !sends[0].Opts.DisablePreview {
		t.Fatalf("payload fields lost in round trip: %#v", sends[0])
	}
}
