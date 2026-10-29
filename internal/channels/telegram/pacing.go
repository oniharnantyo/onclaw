package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/channels/telegram/markdown"
)

// ErrDeferred reports a send refused locally because the chat is in a flood
// cooldown (design D8): the content is parked for redelivery when the
// cooldown ends. Callers treat it as accepted-but-held, never as failure.
var ErrDeferred = errors.New("telegram: send deferred by flood cooldown")

// slotMode is how an outbound unit takes the chat's pacing slot (design D8):
// sends wait, interim preview edits skip when the slot is busy, the final
// edit never gates.
type slotMode int

const (
	slotWait  slotMode = iota // block until the slot frees
	slotTry                   // take the slot only when free; skip otherwise
	slotForce                 // take the slot without waiting
)

// outbound is one unit of output for the pipeline. Text is wire-ready: the
// MarkdownV2 conversion happens at the entry points (chunking runs on the
// converted string, design D7); src carries the original for the parse-error
// plain fallback.
type outbound struct {
	chatID int64
	msgID  int64 // >0 edits msgID, 0 sends a fresh message
	src    string
	text   string
	md     bool
	markup json.RawMessage
}

// chatPacer is one chat's outbound discipline (design D8): a single pacing
// slot shared by sends and edits, a fail-closed flood cooldown with pending
// redelivery, and the 429 inline retry under FloodInlineMax.
type chatPacer struct {
	mu            sync.Mutex
	next          time.Time // the slot frees at
	cooldownUntil time.Time // zero when clear
	cooldownTimer *time.Timer
	pending       []outbound
}

func (p *chatPacer) inCooldown(now time.Time) bool {
	return now.Before(p.cooldownUntil)
}

// pacer returns (creating) the chat's pacer.
func (s *Service) pacer(chatID int64) *chatPacer {
	s.paceMu.Lock()
	defer s.paceMu.Unlock()
	p, ok := s.pacers[chatID]
	if !ok {
		p = &chatPacer{}
		s.pacers[chatID] = p
	}
	return p
}

// cooling reports whether the chat is in a flood cooldown (the pump skips
// interim preview edits instead of parking them).
func (s *Service) cooling(chatID int64) bool {
	p := s.pacer(chatID)
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inCooldown(time.Now())
}

// takeSlot applies the slot mode. false from slotTry means skipped.
func (s *Service) takeSlot(ctx context.Context, p *chatPacer, mode slotMode) (taken, ok bool) {
	switch mode {
	case slotTry:
		p.mu.Lock()
		defer p.mu.Unlock()
		if time.Until(p.next) > 0 {
			return false, true // busy: skip this interim edit
		}
		p.next = time.Now().Add(s.t.pacingSlot)
		return true, true
	case slotForce:
		p.mu.Lock()
		p.next = time.Now().Add(s.t.pacingSlot)
		p.mu.Unlock()
		return true, true
	default: // slotWait
		p.mu.Lock()
		wait := time.Until(p.next)
		if wait > 0 {
			p.next = p.next.Add(s.t.pacingSlot)
		} else {
			p.next = time.Now().Add(s.t.pacingSlot)
			wait = 0
		}
		p.mu.Unlock()
		if wait == 0 {
			return true, true
		}
		return true, sleepCtx(ctx, wait)
	}
}

// enterCooldown fails the chat closed for min(retryAfter, CooldownCap) and
// schedules the pending-output redelivery (design D8).
func (s *Service) enterCooldown(p *chatPacer, retryAfter time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if retryAfter <= 0 {
		retryAfter = s.t.backoffMin
	}
	if retryAfter > s.t.cooldownCap {
		retryAfter = s.t.cooldownCap
	}
	if until := time.Now().Add(retryAfter); until.After(p.cooldownUntil) {
		p.cooldownUntil = until
	}
	slog.Warn("telegram: flood penalty; entering cooldown", "retry_after", retryAfter)
	if p.cooldownTimer != nil {
		p.cooldownTimer.Stop()
	}
	p.cooldownTimer = time.AfterFunc(retryAfter, func() { s.flushPending(p) })
}

// deliver performs one outbound unit through the pipeline: cooldown gate
// (park and defer), slot gate, MarkdownV2-first with plain fallback on a
// parse rejection, and the 429 inline retry / fail-closed rule. It returns
// the sent message id (0 for edits, skips, and deferrals) so the streaming
// preview chain can anchor on it.
func (s *Service) deliver(ctx context.Context, o outbound, mode slotMode) (int64, error) {
	client, _, err := s.botFor(ctx)
	if err != nil {
		return 0, err
	}
	p := s.pacer(o.chatID)

	p.mu.Lock()
	cool := p.inCooldown(time.Now())
	p.mu.Unlock()
	if cool {
		p.mu.Lock()
		p.pending = append(p.pending, o)
		p.mu.Unlock()
		return 0, ErrDeferred
	}

	taken, ok := s.takeSlot(ctx, p, mode)
	if !ok {
		return 0, ctx.Err()
	}
	if !taken {
		return 0, nil // skipped interim edit (slot busy) — not an error
	}

	var msgID int64
	send := func(ctx context.Context, o outbound) error {
		if o.msgID != 0 {
			_, err := client.EditMessageText(ctx, botapi.EditMessageTextParams{
				ChatID: o.chatID, MessageID: o.msgID, Text: o.text, ParseMode: parseModeOf(o),
			})
			return err
		}
		msg, err := client.SendMessage(ctx, botapi.SendMessageParams{
			ChatID: o.chatID, Text: o.text, ParseMode: parseModeOf(o), ReplyMarkup: o.markup,
		})
		if err == nil && msg != nil {
			msgID = msg.MessageID
		}
		return err
	}

	err = send(ctx, o)
	var apiErr *botapi.Error
	if errors.As(err, &apiErr) && apiErr.Kind == botapi.KindParse {
		// Telegram could not parse the MarkdownV2: resend stripped plain
		// text of the same content (same rule on edits, design D7).
		err = send(ctx, plainOf(o))
	}
	if errors.As(err, &apiErr) && apiErr.Kind == botapi.KindRateLimited {
		if apiErr.RetryAfter > 0 && apiErr.RetryAfter <= s.t.floodInlineMax {
			// Sleep inline and retry once. Only this outbound path sleeps
			// on a flood penalty — the inbound path never does (design D8).
			if sleepCtx(ctx, apiErr.RetryAfter) {
				err = send(ctx, o)
			}
		}
		if errors.As(err, &apiErr) && apiErr.Kind == botapi.KindRateLimited {
			s.enterCooldown(p, apiErr.RetryAfter)
			p.mu.Lock()
			p.pending = append(p.pending, o)
			p.mu.Unlock()
			return 0, ErrDeferred
		}
	}
	return msgID, err
}

// parseModeOf returns the wire parse mode for an outbound unit.
func parseModeOf(o outbound) string {
	if o.md {
		return "MarkdownV2"
	}
	return ""
}

// plainOf rebuilds an outbound unit as stripped plain text — the parse-error
// fallback (design D7): same target, same content, parse mode dropped.
func plainOf(o outbound) outbound {
	return outbound{
		chatID: o.chatID, msgID: o.msgID,
		src: o.src, text: markdown.PlainText(o.src),
	}
}

// flushPending redelivers the cooldown's parked output in order (design D8).
// A renewed penalty re-parks what remains.
func (s *Service) flushPending(p *chatPacer) {
	p.mu.Lock()
	pending := p.pending
	p.pending = nil
	p.cooldownTimer = nil
	p.mu.Unlock()

	ctx := s.root
	for _, o := range pending {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.deliver(ctx, o, slotWait); err != nil && !errors.Is(err, ErrDeferred) {
			slog.Warn("telegram: pending redelivery failed", "chat", o.chatID, "error", err)
		}
	}
}

// sendMarkdown sends user-facing text through the full pipeline: MarkdownV2
// conversion, chunking at ChunkLimit on the converted string, sequential
// parts, slot pacing, parse fallback, flood rules. Used by the final answer,
// the announcer, and any MarkdownV2-destined notice.
func (s *Service) sendMarkdown(ctx context.Context, chatID int64, src string, markup json.RawMessage) error {
	converted := markdown.ToMarkdownV2(src)
	for _, part := range markdown.Chunk(converted, ChunkLimit) {
		if _, err := s.deliver(ctx, outbound{chatID: chatID, src: src, text: part, md: true, markup: markup}, slotWait); err != nil {
			return err
		}
	}
	return nil
}

// sendNotice sends a plain-text chat notice through the same pipeline
// (pacing, chunking, flood rules inherited).
func (s *Service) sendNotice(ctx context.Context, chatID int64, text string) error {
	return s.sendText(ctx, chatID, text)
}

// sendText is the plain-text variant of sendMarkdown.
func (s *Service) sendText(ctx context.Context, chatID int64, text string) error {
	for _, part := range chunkPlain(text) {
		if _, err := s.deliver(ctx, outbound{chatID: chatID, src: text, text: part}, slotWait); err != nil {
			return err
		}
	}
	return nil
}

// noticeAsync sends a notice off the caller's critical path.
func (s *Service) noticeAsync(chatID int64, text string) {
	ctx := s.root
	go func() {
		if ctx.Err() != nil {
			return
		}
		if err := s.sendText(ctx, chatID, text); err != nil {
			slog.Warn("telegram: notice failed", "chat", chatID, "error", err)
		}
	}()
}

// plainChunkLimit is the rune budget for a plain-text part. Plain notices are
// short in practice; the rune split is the safe approximation of the UTF-16
// cap (it can only over-split, never overflow).
const plainChunkLimit = ChunkLimit

// chunkPlain splits plain text under the message cap preferring line breaks.
func chunkPlain(text string) []string {
	rs := []rune(text)
	if len(rs) <= plainChunkLimit {
		return []string{text}
	}
	var parts []string
	for len(rs) > plainChunkLimit {
		cut := plainChunkLimit
		for i := plainChunkLimit; i > plainChunkLimit/2; i-- {
			if rs[i-1] == '\n' {
				cut = i
				break
			}
		}
		parts = append(parts, string(rs[:cut]))
		rs = rs[cut:]
	}
	if len(rs) > 0 {
		parts = append(parts, string(rs))
	}
	return parts
}

// sendDocument sends one file through the pipeline with a plain caption
// (design D12's artifact delivery). Cooldowns park it for redelivery like
// any send.
func (s *Service) sendDocument(ctx context.Context, chatID int64, name string, data []byte, caption string) error {
	client, _, err := s.botFor(ctx)
	if err != nil {
		return err
	}
	p := s.pacer(chatID)

	p.mu.Lock()
	cool := p.inCooldown(time.Now())
	p.mu.Unlock()
	if cool {
		// Artifacts are bytes already in hand; parking a reader closure is
		// needlessly indirect — report the deferral to the caller.
		return ErrDeferred
	}
	if _, ok := s.takeSlot(ctx, p, slotWait); !ok {
		return ctx.Err()
	}

	_, err = client.SendDocument(ctx, botapi.SendDocumentParams{
		ChatID:   chatID,
		Document: bytes.NewReader(data),
		FileName: name,
		Caption:  caption,
	})
	return err
}
