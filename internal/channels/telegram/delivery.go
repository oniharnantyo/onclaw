package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/channels/telegram/markdown"
	"github.com/oniharnantyo/buddies/internal/sessions"
)

// noticeMax bounds the short failure/cancel notices.
const noticeMax = 300

// pumpTurn is the delivery pump (design D6): subscribe to the just-submitted
// session, accumulate text deltas, push a preview edit every EditInterval,
// finalize on assistant_message (fresh send past FreshFinalAfter), deliver
// error/cancel notices, the approval card, and the turn's artifacts. Only
// events stamped with the submitted turn's id are delivered (the initiator
// rule); the subscription is released and the queue drained at turn end.
func (s *Service) pumpTurn(chatID int64, sessionID string, turn TurnView, agentID string) {
	ctx := s.root

	sub, err := s.rt.Subscribe(ctx, sessionID)
	if err != nil {
		// The turn still runs and persists; the channel just cannot stream
		// it. Keep the chat's bookkeeping consistent.
		slog.Warn("telegram: subscribe failed; streaming disabled for this turn", "session", sessionID, "error", err)
		<-turn.Done()
		s.finishTurn(chatID)
		return
	}

	// Subscribe happens right after Submit returns; events the runtime
	// emitted in between are covered by the final assistant_message,
	// which always arrives on the stream.
	stopTyping := s.startTyping(ctx, chatID)

	p := &pump{
		svc:       s,
		chatID:    chatID,
		turn:      turn,
		agentID:   agentID,
		sessionID: sessionID,
		start:     time.Now(),
		lastEdit:  time.Now(),
	}

	for {
		select {
		case ev, ok := <-sub.Events():
			if !ok {
				// The subscription closed under us: the turn's own Done
				// is the terminal signal.
				<-turn.Done()
				close(stopTyping)
				p.finalize("")
				s.finishTurn(chatID)
				return
			}
			if ev.TurnID != turn.TurnID() {
				continue // initiator rule: other turns' events are not ours to deliver
			}
			if p.event(ev) {
				close(stopTyping)
				sub.Close()
				s.finishTurn(chatID)
				return
			}
		case <-turn.Done():
			// Terminal without a terminal event (a runtime edge): sweep
			// the buffer, then end.
			p.drain(sub)
			close(stopTyping)
			sub.Close()
			s.finishTurn(chatID)
			return
		case <-ctx.Done():
			close(stopTyping)
			sub.Close()
			s.finishTurn(chatID)
			return
		}
	}
}

// pump is one turn's delivery state.
type pump struct {
	svc       *Service
	chatID    int64
	turn      TurnView
	agentID   string
	sessionID string
	start     time.Time
	buf       strings.Builder
	preview   int64  // the streaming preview message; 0 until the first push
	previewX  string // the wire text currently shown in the preview
	lastEdit  time.Time
}

// event handles one turn event and reports whether the pump is done.
func (p *pump) event(ev sessions.Event) bool {
	switch ev.Kind {
	case sessions.KindTextDelta:
		p.buf.WriteString(ev.Text)
		p.editPreview()
	case sessions.KindApprovalRequired:
		p.svc.sendApprovalCard(p.svc.root, p.chatID, p.sessionID, ev.Approval)
	case sessions.KindAssistantMessage:
		p.finalize(ev.Text)
	case sessions.KindTurnError:
		p.svc.noticeAsync(p.chatID, "Turn failed: "+truncate(ev.Error, noticeMax))
		return true
	case sessions.KindTurnCancelled:
		p.svc.noticeAsync(p.chatID, "Turn cancelled.")
		return true
	case sessions.KindTurnCompleted:
		p.deliverArtifacts(ev)
		return true
	}
	return false
}

// drain sweeps any events still buffered after an abrupt turn completion.
func (p *pump) drain(sub agents.Subscription) {
	for {
		select {
		case ev, ok := <-sub.Events():
			if !ok {
				return
			}
			if ev.TurnID == p.turn.TurnID() && p.event(ev) {
				return
			}
		default:
			return
		}
	}
}

// editPreview pushes the accumulated text as a streaming preview: the first
// push sends a fresh message, later ones edit it — at most once per
// EditInterval, skipping when the pacing slot is busy or the chat is in a
// flood cooldown (design D6/D8: interim output is expendable, the final
// still delivers).
func (p *pump) editPreview() {
	if time.Since(p.lastEdit) < p.svc.t.editInterval {
		return
	}
	if p.buf.Len() == 0 {
		return
	}
	p.lastEdit = time.Now()
	if p.svc.cooling(p.chatID) {
		return // keep buffering; the next tick delivers
	}

	text := p.buf.String()
	converted := markdown.ToMarkdownV2(text)
	part := converted
	if parts := markdown.Chunk(converted, ChunkLimit); len(parts) > 0 {
		part = parts[0] // a preview shows the head; chunk markers ride the final
	}

	ctx := p.svc.root
	if p.preview == 0 {
		msgID, err := p.svc.deliver(ctx, outbound{chatID: p.chatID, src: text, text: part, md: true}, slotWait)
		if err != nil || msgID == 0 {
			return // transient preview loss; the final still delivers
		}
		p.preview = msgID
		p.previewX = part
		return
	}
	if part == p.previewX {
		return
	}
	if _, err := p.svc.deliver(ctx, outbound{chatID: p.chatID, msgID: p.preview, src: text, text: part, md: true}, slotTry); err == nil {
		p.previewX = part
	} // skipped or deferred edits are fine — interim output only
}

// finalize delivers the turn's final answer (design D6): edit the preview —
// or send fresh when nothing streamed yet or the stream ran past
// FreshFinalAfter (edit timestamps go stale).
func (p *pump) finalize(text string) {
	if text == "" {
		if p.buf.Len() > 0 {
			text = p.buf.String()
		} else {
			return // nothing streamed and no final text: other kinds covered it
		}
	}
	if p.preview != 0 && p.previewX == markdown.ToMarkdownV2(text) {
		return // the preview already shows the final
	}

	ctx := p.svc.root
	if p.preview != 0 && time.Since(p.start) < p.svc.t.freshFinalAfter {
		// Final edits never gate the pipeline (design D8).
		o := outbound{chatID: p.chatID, msgID: p.preview, src: text, text: markdown.ToMarkdownV2(text), md: true}
		if _, err := p.svc.deliver(ctx, o, slotForce); err != nil && !errors.Is(err, ErrDeferred) {
			slog.Warn("telegram: final edit failed", "chat", p.chatID, "error", err)
		}
		return
	}
	// Fresh send (first message, or a stale long stream): a full send with
	// chunking; the stale preview stays as the trail marker.
	if err := p.svc.sendMarkdown(ctx, p.chatID, text, nil); err != nil && !errors.Is(err, ErrDeferred) {
		slog.Warn("telegram: final send failed", "chat", p.chatID, "error", err)
	}
}

// deliverArtifacts sends the turn's artifact files (design D12): up to
// MaxArtifactsPerTurn documents, each under MaxArtifactBytes; oversize and
// unreadable files get a chat note (never silent).
func (p *pump) deliverArtifacts(ev sessions.Event) {
	if len(ev.Artifacts) == 0 || p.svc.arts == nil {
		return
	}
	ctx := p.svc.root
	sent := 0
	for _, art := range ev.Artifacts {
		if sent >= MaxArtifactsPerTurn {
			p.svc.noticeAsync(p.chatID, "More artifacts were produced but the per-turn cap is 10 files.")
			return
		}
		if art.Size > MaxArtifactBytes {
			p.svc.noticeAsync(p.chatID, "Skipped "+art.Path+" (over 50 MB).")
			continue
		}
		data, err := p.svc.arts.Read(ctx, p.agentID, art.Path)
		if err != nil {
			slog.Warn("telegram: artifact read failed", "path", art.Path, "error", err)
			p.svc.noticeAsync(p.chatID, "Could not read "+art.Path+" to send it.")
			continue
		}
		if err := p.svc.sendDocument(ctx, p.chatID, path.Base(art.Path), data, ""); err != nil {
			if errors.Is(err, ErrDeferred) {
				slog.Warn("telegram: artifact deferred by cooldown", "path", art.Path)
				continue
			}
			slog.Warn("telegram: artifact send failed", "path", art.Path, "error", err)
			p.svc.noticeAsync(p.chatID, "Could not send "+art.Path+".")
			continue
		}
		sent++
	}
}

// startTyping refreshes the typing indicator while the turn runs (design
// D12/task 4.5); closing the returned channel stops it.
func (s *Service) startTyping(ctx context.Context, chatID int64) chan struct{} {
	done := make(chan struct{})
	client, _, err := s.botFor(ctx)
	if err != nil || client == nil {
		close(done)
		return done
	}
	if err := client.SendChatAction(ctx, chatID, botapi.ActionTyping); err != nil {
		slog.Debug("telegram: typing action failed", "error", err)
	}
	go func() {
		ticker := time.NewTicker(s.t.typingRefresh)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := client.SendChatAction(ctx, chatID, botapi.ActionTyping); err != nil {
					slog.Debug("telegram: typing refresh failed", "error", err)
				}
			}
		}
	}()
	return done
}

// truncate clips s to at most n runes with an ellipsis.
func truncate(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "…"
}

// approveKeyboard builds the inline keyboard for one approval card
// (design D12): callback_data is appr:<sessionID>:<interruptID>:<decision>.
func approveKeyboard(sessionID, interruptID string) json.RawMessage {
	data := "appr:" + sessionID + ":" + interruptID + ":"
	kb := map[string]any{
		"inline_keyboard": [][]map[string]string{
			{
				{"text": "Approve", "callback_data": data + "approve"},
				{"text": "Deny", "callback_data": data + "deny"},
			},
		},
	}
	raw, err := json.Marshal(kb)
	if err != nil {
		return nil
	}
	return raw
}
