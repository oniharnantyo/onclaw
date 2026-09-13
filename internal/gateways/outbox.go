package gateways

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Outbox defaults (design D9): a small attempt budget inside a 24 h
// freshness window, a 7-day retention prune, a 5 s delivery poll, and
// exponential reschedule backoff capped under the poll interval's scale.
const (
	DefaultOutboxMaxAttempts  = 5
	DefaultOutboxFreshness    = 24 * time.Hour
	DefaultOutboxRetention    = 7 * 24 * time.Hour
	DefaultOutboxPollInterval = 5 * time.Second
	outboxBackoffBase         = 30 * time.Second
	outboxBackoffCap          = 15 * time.Minute
	// outboxClaimBatch bounds one delivery cycle's claim size.
	outboxClaimBatch = 32
)

// DuplicateNoticePrefix is prepended to a message whose send outcome was
// ambiguous (the entry was claimed before and the process died mid-send, or
// a redelivery attempt follows a failed one): at-least-once means the chat
// may see it twice, and the prefix makes that visible and honest instead of
// silent (design D9).
const DuplicateNoticePrefix = "⚠️ Duplicate notice: delivery was interrupted, so this message may have been shown before.\n\n"

// OutboxPayload is the JSON body stored on an outbox entry: the chat-side
// coordinates the delivery worker needs after a restart (the row itself
// carries only workspace/session; platform chat coordinates live in the
// payload, owned by the gateway service).
type OutboxPayload struct {
	GatewayID      string `json:"gateway_id"`
	ChatID         string `json:"chat_id"`
	HTML           string `json:"html"`
	DisablePreview bool   `json:"disable_preview,omitempty"`
}

// MessageSender is the narrow send seam the outbox delivers through
// (PlatformAdapter satisfies it structurally).
type MessageSender interface {
	SendMessage(ctx context.Context, chatID, html string, opts SendOptions) (string, error)
}

// SenderResolver resolves the send seam for one gateway id: the payload
// carries the gateway that generated the reply, and multi-workspace
// instances run one adapter per workspace gateway — the resolver picks the
// right bot for each pending entry (Service.Adapter is the production
// resolver).
type SenderResolver func(gatewayID string) (MessageSender, bool)

// Outbox is the durable delivery outbox (design D9): every outbound reply is
// written pending before it is sent; a delivery worker claims due entries,
// sends them, and marks them delivered. A crash between generating a reply
// and delivering it is recovered by the next delivery cycle (startup
// included) — at-least-once.
type Outbox struct {
	outbox       store.GatewayOutbox
	senders      SenderResolver
	maxAttempts  int
	freshness    time.Duration
	retention    time.Duration
	pollInterval time.Duration
	backoffBase  time.Duration
}

// NewOutbox creates the outbox service over the store port and the
// per-gateway send resolver.
func NewOutbox(outbox store.GatewayOutbox, senders SenderResolver, opts ...OutboxOption) *Outbox {
	o := &Outbox{
		outbox:       outbox,
		senders:      senders,
		maxAttempts:  DefaultOutboxMaxAttempts,
		freshness:    DefaultOutboxFreshness,
		retention:    DefaultOutboxRetention,
		pollInterval: DefaultOutboxPollInterval,
		backoffBase:  outboxBackoffBase,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// OutboxOption customizes the outbox service's knobs.
type OutboxOption func(*Outbox)

// WithOutboxMaxAttempts overrides the attempt budget (default 5).
func WithOutboxMaxAttempts(n int) OutboxOption {
	return func(o *Outbox) {
		if n > 0 {
			o.maxAttempts = n
		}
	}
}

// WithOutboxFreshness overrides the freshness window (default 24 h): entries
// older than it are marked dead rather than redelivered.
func WithOutboxFreshness(d time.Duration) OutboxOption {
	return func(o *Outbox) {
		if d > 0 {
			o.freshness = d
		}
	}
}

// WithOutboxRetention overrides the delivered-row retention (default 7 days).
func WithOutboxRetention(d time.Duration) OutboxOption {
	return func(o *Outbox) {
		if d > 0 {
			o.retention = d
		}
	}
}

// WithOutboxPollInterval overrides the background delivery poll (default 5 s).
func WithOutboxPollInterval(d time.Duration) OutboxOption {
	return func(o *Outbox) {
		if d > 0 {
			o.pollInterval = d
		}
	}
}

// WithOutboxBackoff overrides the reschedule backoff base (default 30 s;
// exponential, capped at 15 minutes).
func WithOutboxBackoff(d time.Duration) OutboxOption {
	return func(o *Outbox) {
		if d > 0 {
			o.backoffBase = d
		}
	}
}

// Enqueue writes the delivery record before any send attempt
// (write-before-send): the entry is born pending and due immediately. The
// committed entry is returned so the caller can mark it delivered after its
// own confirmed inline send (the streamer's fast path).
func (o *Outbox) Enqueue(ctx context.Context, gatewayID, workspaceID, sessionID, chatID, html string, opts SendOptions) (*domain.OutboxEntry, error) {
	payload, err := json.Marshal(OutboxPayload{
		GatewayID:      gatewayID,
		ChatID:         chatID,
		HTML:           html,
		DisablePreview: opts.DisablePreview,
	})
	if err != nil {
		return nil, fmt.Errorf("gateway outbox: encode payload: %w", err)
	}
	entry := &domain.OutboxEntry{
		WorkspaceID:  workspaceID,
		SessionID:    sessionID,
		Payload:      payload,
		DeliverAfter: time.Now().UTC(),
	}
	if err := o.outbox.Enqueue(ctx, entry); err != nil {
		return nil, fmt.Errorf("gateway outbox: enqueue: %w", err)
	}
	return entry, nil
}

// MarkDelivered records the confirmed send of an entry the streamer
// delivered inline. A missed mark (crash or store error first) leaves the
// row pending — the delivery loop's at-least-once redelivery covers it.
func (o *Outbox) MarkDelivered(ctx context.Context, workspaceID, id string) error {
	if err := o.outbox.MarkDelivered(ctx, workspaceID, id); err != nil {
		return fmt.Errorf("gateway outbox: mark delivered: %w", err)
	}
	return nil
}

// DeliverDue runs one delivery cycle: claims due entries, delivers each, and
// returns how many were delivered. Entries past the attempt budget or the
// freshness window are marked dead without blocking the rest; delivered rows
// past retention are pruned. now is injectable for deterministic tests.
func (o *Outbox) DeliverDue(ctx context.Context, now time.Time) (int, error) {
	entries, err := o.outbox.ClaimDue(ctx, now, outboxClaimBatch)
	if err != nil {
		return 0, fmt.Errorf("gateway outbox: claim: %w", err)
	}

	delivered := 0
	for _, entry := range entries {
		// Budget and freshness gates (spec: bounded redelivery). ClaimDue
		// already counted this claim in Attempts.
		if entry.Attempts > o.maxAttempts || now.Sub(entry.CreatedAt) > o.freshness {
			if err := o.outbox.MarkDead(ctx, entry.WorkspaceID, entry.ID); err != nil {
				slog.Warn("gateway outbox: marking dead failed", "entry", entry.ID, "error", err)
			}
			continue
		}

		var payload OutboxPayload
		if err := json.Unmarshal(entry.Payload, &payload); err != nil {
			// Poison payload: undeliverable forever, never retried again.
			slog.Error("gateway outbox: corrupt payload, marking dead", "entry", entry.ID, "error", err)
			_ = o.outbox.MarkDead(ctx, entry.WorkspaceID, entry.ID)
			continue
		}

		html := payload.HTML
		if entry.Attempts > 1 {
			// Ambiguous redelivery: a previous attempt was claimed and the
			// outcome unknown (crash mid-send). Warn in the chat.
			html = DuplicateNoticePrefix + html
		}

		sender, ok := o.senders(payload.GatewayID)
		if !ok {
			// The generating gateway is not attached (stopped or disabled
			// between enqueue and delivery): transient, so reschedule like a
			// failed send — the attempt budget and freshness window bound it.
			after := o.rescheduleAfter(now, entry.Attempts)
			slog.Warn("gateway outbox: no adapter for gateway, rescheduled",
				"entry", entry.ID, "gateway", payload.GatewayID, "retry_after", after)
			if err := o.outbox.Reschedule(ctx, entry.WorkspaceID, entry.ID, after); err != nil {
				slog.Warn("gateway outbox: reschedule failed", "entry", entry.ID, "error", err)
			}
			continue
		}

		if _, err := sender.SendMessage(ctx, payload.ChatID, html, SendOptions{DisablePreview: payload.DisablePreview}); err != nil {
			after := o.rescheduleAfter(now, entry.Attempts)
			slog.Warn("gateway outbox: delivery failed, rescheduled",
				"entry", entry.ID, "attempt", entry.Attempts, "retry_after", after, "error", err)
			if err := o.outbox.Reschedule(ctx, entry.WorkspaceID, entry.ID, after); err != nil {
				slog.Warn("gateway outbox: reschedule failed", "entry", entry.ID, "error", err)
			}
			continue
		}

		if err := o.outbox.MarkDelivered(ctx, entry.WorkspaceID, entry.ID); err != nil {
			slog.Warn("gateway outbox: mark delivered failed", "entry", entry.ID, "error", err)
			continue
		}
		delivered++
	}
	return delivered, nil
}

// Prune deletes delivered rows past the retention horizon.
func (o *Outbox) Prune(ctx context.Context, now time.Time) (int64, error) {
	n, err := o.outbox.PruneDelivered(ctx, now.Add(-o.retention))
	if err != nil {
		return 0, fmt.Errorf("gateway outbox: prune: %w", err)
	}
	return n, nil
}

// Start runs the delivery loop until ctx is done: a poll of due entries plus
// a retention prune each cycle. Intended as the composition root's
// background goroutine (startup redelivery sweep included — the first cycle
// recovers committed-but-unsent rows).
func (o *Outbox) Start(ctx context.Context) {
	ticker := time.NewTicker(o.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().UTC()
			if _, err := o.DeliverDue(ctx, now); err != nil {
				slog.Warn("gateway outbox: delivery cycle failed", "error", err)
			}
			if _, err := o.Prune(ctx, now); err != nil {
				slog.Warn("gateway outbox: prune failed", "error", err)
			}
		}
	}
}

// rescheduleAfter computes the next deliver_after: exponential backoff off
// the attempt count, capped. attempts is the count including the failed one.
func (o *Outbox) rescheduleAfter(now time.Time, attempts int) time.Time {
	delay := o.backoffBase
	for i := 1; i < attempts && delay < outboxBackoffCap; i++ {
		delay *= 2
	}
	if delay > outboxBackoffCap {
		delay = outboxBackoffCap
	}
	if delay < time.Second {
		delay = time.Second
	}
	return now.Add(delay)
}
