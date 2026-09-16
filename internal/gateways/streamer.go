package gateways

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Streaming cadence defaults (design D5): edits fire at most once per
// debounce interval, the typing indicator heartbeats every typing interval
// while the turn runs.
const (
	DefaultDebounceInterval = 1200 * time.Millisecond
	DefaultTypingInterval   = 4500 * time.Millisecond
	// placeholderText is the message posted before the first delta arrives.
	placeholderText = "…"
)

// StreamSession identifies the chat-side coordinates of one running turn.
type StreamSession struct {
	GatewayID   string
	WorkspaceID string
	SessionID   string
	ChatID      string
}

// StreamResult reports what one drained turn produced. Approval is non-nil
// when the turn paused on an approval interrupt — the stream ends after that
// event and the caller routes the payload through the ApprovalBridge.
// Delivery errors are surfaced, never swallowed: the adapter owns the
// plain-text fallback and rate-limit backoff per its contract, so any error
// reaching here is one the adapter could not recover from and the caller
// must know about (the outbox records final replies, not mid-stream edits).
type StreamResult struct {
	// Text is the turn's final markdown text ("" when nothing streamed).
	Text string
	// Approval is the pending interrupt when the turn paused for approval.
	Approval *agents.ApprovalPayload
	// Err is the drain failure, if any.
	Err error
}

// Streamer drives one turn's live delivery onto a chat: a placeholder
// message edited on a debounce cadence, a typing heartbeat, tool-call notes,
// and the final flush that splits oversized replies across messages
// (design D5). NEW message deliveries — final replies and mid-stream
// rollover parts — ride the durable outbox (design D9); debounced edits are
// direct adapter refreshes. The delivery shape adapts to the platform's
// capabilities (add-whatsapp-gateway design D2): without CanEdit the
// placeholder/edit stream is skipped entirely — nothing is sent
// mid-stream, the typing heartbeat stays, and the final reply rides the
// outbox as one message.
type Streamer struct {
	adapter  PlatformAdapter
	outbox   *Outbox
	debounce time.Duration
	typing   time.Duration
	flavor   RenderFlavor
	caps     AdapterCapabilities
}

// NewStreamer creates a Streamer over the platform adapter and the delivery
// outbox the final replies are committed to. The render flavor defaults to
// the Telegram wire format (byte-identical historical behavior); platform
// compositions override it with WithRenderFlavor.
func NewStreamer(adapter PlatformAdapter, outbox *Outbox, opts ...StreamerOption) *Streamer {
	s := &Streamer{
		adapter:  adapter,
		outbox:   outbox,
		debounce: DefaultDebounceInterval,
		typing:   DefaultTypingInterval,
		flavor:   TelegramFlavor,
		caps:     adapter.Capabilities(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// StreamerOption customizes the streamer's cadence knobs.
type StreamerOption func(*Streamer)

// WithDebounceInterval overrides the edit debounce (default 1.2 s).
func WithDebounceInterval(d time.Duration) StreamerOption {
	return func(s *Streamer) {
		if d > 0 {
			s.debounce = d
		}
	}
}

// WithTypingInterval overrides the typing heartbeat (default 4.5 s).
func WithTypingInterval(d time.Duration) StreamerOption {
	return func(s *Streamer) {
		if d > 0 {
			s.typing = d
		}
	}
}

// WithRenderFlavor overrides the wire format the turn is rendered and
// delivered in (add-whatsapp-gateway design D9).
func WithRenderFlavor(f RenderFlavor) StreamerOption {
	return func(s *Streamer) {
		s.flavor = f
	}
}

// messageBuffer is one platform message under construction: its streaming
// markdown, the tool-call notes appended below the text, and the platform
// message id being edited in place.
type messageBuffer struct {
	text     *StreamRenderer
	notes    []string
	msgID    string
	lastEdit time.Time
}

// markdown renders the buffer's full body: model text plus tool-call notes.
func (m *messageBuffer) markdown() string {
	md := m.text.Markdown()
	if len(m.notes) == 0 {
		return md
	}
	if md == "" {
		return strings.Join(m.notes, "\n")
	}
	return md + "\n\n" + strings.Join(m.notes, "\n")
}

// addNote appends a tool-call note; a matching "running…" note for the same
// tool name is replaced so the body shows the finished latency instead.
func (m *messageBuffer) addNote(note string) {
	m.notes = append(m.notes, note)
}

func (m *messageBuffer) addToolFinished(name string, latency time.Duration) {
	note := "🔧 `" + name + "`"
	if latency > 0 {
		note += " · " + latency.Round(time.Millisecond).String()
	}
	running := "🔧 `" + name + "` running…"
	for i := len(m.notes) - 1; i >= 0; i-- {
		if m.notes[i] == running {
			m.notes[i] = note
			return
		}
	}
	m.addNote(note)
}

// Stream drains the turn's EventStream into the chat and returns what the
// turn produced. It always returns a StreamResult — a drained-but-failed
// turn reports the failure instead of dropping the transcript silently.
func (s *Streamer) Stream(ctx context.Context, session StreamSession, stream *agents.EventStream) StreamResult {
	chat := session.ChatID

	// Capability negotiation (add-whatsapp-gateway design D2): editable
	// platforms open with a placeholder message that the debounced edits
	// rewrite; elsewhere the reply is delivered once, at the end.
	placeholder := ""
	if s.caps.CanEdit {
		var err error
		placeholder, err = s.adapter.SendMessage(ctx, chat, s.flavor.Render(placeholderText), s.flavor.Name, SendOptions{DisablePreview: true})
		if err != nil {
			return StreamResult{Err: fmt.Errorf("gateway stream: send placeholder: %w", err)}
		}
	}

	// Typing heartbeat for the whole drain: best-effort, failures never
	// interrupt the turn.
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	var heartbeatWG sync.WaitGroup
	heartbeatWG.Add(1)
	go func() {
		defer heartbeatWG.Done()
		s.runHeartbeat(heartbeatCtx, chat)
	}()
	defer func() {
		cancelHeartbeat()
		heartbeatWG.Wait()
	}()

	cur := &messageBuffer{text: NewStreamRenderer(), msgID: placeholder}
	done := []string{} // markdown of finalized rollover buffers
	result := StreamResult{}

	for {
		ev, recvErr := stream.Recv()
		if recvErr != nil {
			if !errors.Is(recvErr, io.EOF) {
				result.Err = fmt.Errorf("gateway stream: drain: %w", recvErr)
			}
			break
		}
		if ev == nil {
			break
		}

		switch ev.Kind {
		case agents.TranscriptEventTextDelta:
			cur.text.Write(ev.TextDelta)

		case agents.TranscriptEventMessageCompleted:
			// A completed message longer than the accumulated deltas is
			// authoritative (drop-new tap recovery).
			if ev.Message != nil && runeLen(ev.Message.Content) > runeLen(cur.text.Markdown()) {
				cur.text.Set(ev.Message.Content)
			}

		case agents.TranscriptEventToolCallStarted:
			if ev.ToolCall != nil {
				cur.addNote("🔧 `" + ev.ToolCall.Name + "` running…")
			}

		case agents.TranscriptEventToolCallFinished:
			if ev.ToolResult != nil {
				cur.addToolFinished(ev.ToolResult.Name, ev.ToolResult.Latency)
			}

		case agents.TranscriptEventApprovalRequired:
			if ev.Approval != nil {
				result.Approval = ev.Approval
			}
			// The stream ends after the interrupt; keep draining until EOF
			// so nothing buffered behind it is lost.

		case agents.TranscriptEventPromptBlocked:
			reason := "blocked by a hook"
			if ev.PromptBlocked != nil && ev.PromptBlocked.Reason != "" {
				reason = ev.PromptBlocked.Reason
			}
			cur.addNote("⚠️ Prompt blocked: " + reason)

		case agents.TranscriptEventError:
			result.Err = errors.New("gateway stream: turn failed: " + ev.Error)
			cur.addNote("⚠️ Turn failed: " + ev.Error)

		case agents.TranscriptEventCancelled:
			reason := ev.CancelReason
			if reason == "" {
				reason = "cancelled"
			}
			cur.addNote("⚠️ Turn " + reason)

		case agents.TranscriptEventTurnCompleted:
			done = append(done, cur.markdown())
			s.flush(ctx, session, chat, cur, true)
			cur = nil
		}

		if cur == nil {
			// Turn completed: the terminal flush already ran.
			break
		}

		// Mid-stream delivery only exists on editable platforms (design D2):
		// without CanEdit the buffer accumulates until the final flush.
		if !s.caps.CanEdit {
			continue
		}

		// Mid-stream rollover: a buffer approaching the platform limit is
		// finalized and a fresh buffer continues the reply, so intermediate
		// edits can never overshoot the budget (design D5). The continuation
		// message is created lazily by the next flush — no eager placeholder
		// that could end up empty if the turn ended here.
		if runeLen(cur.markdown()) > s.flavor.Budget-256 {
			s.flush(ctx, session, chat, cur, true)
			done = append(done, cur.markdown())
			cur = &messageBuffer{text: NewStreamRenderer()}
			continue
		}

		if time.Since(cur.lastEdit) >= s.debounce {
			s.flush(ctx, session, chat, cur, false)
		}
	}

	if cur != nil {
		// Non-terminal exit (EOF, drain error, approval pause): final flush.
		s.flush(ctx, session, chat, cur, true)
		done = append(done, cur.markdown())
	}
	result.Text = strings.Join(done, "\n\n")
	return result
}

// runHeartbeat flashes the typing indicator until ctx is done (best-effort).
func (s *Streamer) runHeartbeat(ctx context.Context, chat string) {
	ticker := time.NewTicker(s.typing)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.adapter.SendTyping(ctx, chat)
		}
	}
}

// flush edits the buffer's message with its current content. The final flush
// splits oversized bodies across messages: the first part edits the
// placeholder in place, the rest go out as new messages (design D5). A
// failed intermediate edit self-heals by re-sending the body as a new
// message; a failed final send stays recoverable — the body was committed to
// the outbox before the attempt, so the delivery loop redelivers it.
//
// Delivery reliability (design D9): on a final flush every part is committed
// to the outbox BEFORE the first send attempt and marked delivered only
// after a confirmed send, so a crash in between is redelivered at startup
// (with the duplicate-warning prefix once an attempt was claimed). The
// debounced mid-stream edits deliberately stay direct adapter calls: they
// are idempotent UI refreshes of a message still being built — only NEW
// message deliveries need the at-least-once guarantee.
func (s *Streamer) flush(ctx context.Context, session StreamSession, chat string, m *messageBuffer, final bool) {
	bodyParts := SplitForFlavor(m.markdown(), s.flavor)

	// An empty body has nothing to edit and nothing to send — a
	// continuation buffer that never received text (the turn ended right
	// after a rollover) must not wipe a placeholder or emit an empty
	// message.
	if len(bodyParts) == 1 && runeLen(bodyParts[0]) == 0 {
		return
	}

	// Write-before-send: commit every part, then attempt the sends.
	recorded := make([]*domain.OutboxEntry, len(bodyParts))
	if final {
		for i, part := range bodyParts {
			entry, err := s.outbox.Enqueue(ctx, session.GatewayID, session.WorkspaceID, session.SessionID, chat, part, s.flavor.Name, SendOptions{DisablePreview: true})
			if err != nil {
				// Without the durable record the part cannot be redelivered;
				// deliver it best-effort anyway — liveness over the guarantee.
				slog.Warn("gateway stream: outbox enqueue failed, delivering best-effort", "chat", chat, "error", err)
				continue
			}
			recorded[i] = entry
		}
	}
	// markDelivered records a confirmed send; a missed mark leaves the row
	// pending for the delivery loop's at-least-once redelivery.
	markDelivered := func(i int) {
		entry := recorded[i]
		if entry == nil {
			return
		}
		if err := s.outbox.MarkDelivered(ctx, entry.WorkspaceID, entry.ID); err != nil {
			slog.Warn("gateway stream: outbox mark delivered failed (row will be redelivered)", "entry", entry.ID, "error", err)
		}
	}

	edited := false
	if m.msgID != "" {
		if err := s.adapter.EditMessage(ctx, chat, m.msgID, bodyParts[0], s.flavor.Name); err == nil {
			edited = true
		} else if final {
			slog.Warn("gateway stream: final edit failed", "chat", chat, "error", err)
		} else {
			slog.Debug("gateway stream: edit failed, re-sending body", "chat", chat, "error", err)
		}
	}
	if !edited {
		id, err := s.adapter.SendMessage(ctx, chat, bodyParts[0], s.flavor.Name, SendOptions{DisablePreview: true})
		if err != nil {
			slog.Error("gateway stream: body delivery failed", "chat", chat, "final", final, "error", err)
			return // keep the buffer; the next flush retries — final parts stay committed for redelivery
		}
		m.msgID = id
	}
	if final {
		markDelivered(0)
	}
	m.lastEdit = time.Now()

	if final {
		for i, part := range bodyParts[1:] {
			if _, err := s.adapter.SendMessage(ctx, chat, part, s.flavor.Name, SendOptions{DisablePreview: true}); err != nil {
				slog.Error("gateway stream: overflow part delivery failed", "chat", chat, "error", err)
				break // the committed row is redelivered by the delivery loop
			}
			markDelivered(i + 1)
		}
	}
}
