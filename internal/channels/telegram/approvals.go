package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/sessions"
)

// callbackPrefix marks an approval-tap callback_data.
const callbackPrefix = "appr:"

// approvalCommandMax bounds the command text shown on the card.
const approvalCommandMax = 500

// sendApprovalCard offers the pending dangerous command as an inline
// keyboard (design D12). The card rides the normal outbound path.
func (s *Service) sendApprovalCard(ctx context.Context, chatID int64, sessionID string, appr *sessions.Approval) {
	if appr == nil || appr.InterruptID == "" || sessionID == "" {
		return
	}
	text := "Approval required:\n" + truncate(appr.Command, approvalCommandMax)
	markup := approveKeyboard(sessionID, appr.InterruptID)
	if markup == nil {
		return
	}
	if err := s.sendMarkup(ctx, chatID, text, markup); err != nil {
		slog.Warn("telegram: approval card failed", "chat", chatID, "error", err)
	}
}

// handleCallback serves an inline-keyboard tap (design D12): parse the
// decision, re-verify the tapper's allowlist (fail closed on lookup errors),
// resolve through the runtime, and answer the callback. A stale tap
// (ErrApprovalNotPending) answers "already resolved" and changes nothing.
func (s *Service) handleCallback(ctx context.Context, cq *botapi.CallbackQuery) {
	if cq == nil || cq.From == nil || !strings.HasPrefix(cq.Data, callbackPrefix) {
		return
	}
	answer := func() {
		if err := s.answerCallback(ctx, cq.ID); err != nil {
			slog.Warn("telegram: answerCallbackQuery failed", "error", err)
		}
	}

	rest := strings.TrimPrefix(cq.Data, callbackPrefix)
	parts := strings.Split(rest, ":")
	if len(parts) != 3 {
		answer()
		return
	}
	sessionID, interruptID, decision := parts[0], parts[1], parts[2]
	if decision != "approve" && decision != "deny" {
		answer()
		return
	}

	ok, err := s.allowlisted(ctx, cq.From.ID)
	if err != nil {
		// Fail closed: an untrustworthy lookup never resolves.
		slog.Warn("telegram: tap allowlist lookup failed; denying", "user", cq.From.ID, "error", err)
		answer()
		return
	}
	if !ok {
		slog.Info("telegram: unauthorized approval tap ignored", "user", cq.From.ID)
		answer()
		return
	}

	err = s.rt.ResolveApproval(ctx, sessionID, interruptID, decision == "approve")
	if errors.Is(err, agents.ErrApprovalNotPending) {
		// Stale or duplicated tap: the decision already landed. Answer and
		// change nothing (design D12 inherits chat-approvals semantics).
		answer()
		return
	}
	if err != nil {
		slog.Warn("telegram: resolve approval failed", "session", sessionID, "interrupt", interruptID, "error", err)
		answer()
		return
	}
	slog.Info("telegram: approval resolved via Telegram", "session", sessionID, "interrupt", interruptID, "decision", decision)
	answer()
}

// answerCallback acknowledges the tap so Telegram drops the button spinner.
func (s *Service) answerCallback(ctx context.Context, id string) error {
	client, _, err := s.botFor(ctx)
	if err != nil {
		return err
	}
	return client.AnswerCallbackQuery(ctx, id)
}

// sendMarkup sends a plain-text message carrying an inline keyboard through
// the pacing slot (the approval card must not stampede the flood budget).
func (s *Service) sendMarkup(ctx context.Context, chatID int64, text string, markup json.RawMessage) error {
	client, _, err := s.botFor(ctx)
	if err != nil {
		return err
	}
	p := s.pacer(chatID)
	if _, ok := s.takeSlot(ctx, p, slotWait); !ok {
		return ctx.Err()
	}
	_, err = client.SendMessage(ctx, botapi.SendMessageParams{
		ChatID: chatID, Text: text, ReplyMarkup: markup,
	})
	return err
}
