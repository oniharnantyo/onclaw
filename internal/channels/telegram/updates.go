package telegram

import (
	"context"
	"log/slog"
	"time"

	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/store"
)

// pruneInterval is the lazy retention sweep cadence for the in-memory seen
// set and the store's receipts (design D9: receipts older than ReceiptTTL go).
const pruneInterval = time.Hour

// handleBatch admits one polled batch: each update is deduped against the
// in-memory seen set and the store's receipts (the Bot API redelivers
// unacked updates for 24h), receipted, and then dispatched — or held when the
// channel is holding (gateway Paused, design D9). The caller advances the
// offset after the batch is admitted (ack-after-processing).
func (s *Service) handleBatch(ctx context.Context, client BotAPI, updates []botapi.Update) {
	s.pruneLazily(ctx)

	botUser := s.botUsername()
	for _, u := range updates {
		if s.alreadySeen(u.ID) {
			continue
		}
		done, err := s.chans.HasUpdateReceipt(ctx, botUser, u.ID)
		if err != nil {
			// Fail closed: an untrustworthy dedupe state must not risk a
			// double dispatch, so the redelivered copy is skipped and the
			// offset (already advanced by the caller) moves past it.
			slog.Warn("telegram: receipt lookup failed; skipping update", "update_id", u.ID, "error", err)
			s.markSeen(u.ID)
			continue
		}
		if done {
			s.markSeen(u.ID)
			continue
		}
		s.markSeen(u.ID)
		if err := s.chans.AddUpdateReceipt(ctx, store.ChannelUpdateReceipt{BotUsername: botUser, UpdateID: u.ID}); err != nil {
			// The in-memory set already guards this process; a receipt
			// failure only weakens cross-restart dedupe.
			slog.Warn("telegram: receipt write failed", "update_id", u.ID, "error", err)
		}

		s.admit(ctx, u)
	}
}

// admit dispatches one update, or holds it when the channel is holding
// (design D9's bounded queue: dispatch on resume, oldest dropped over cap).
// Live dispatches and the resume drain share the dispatch mutex, so a drain
// cannot interleave with a concurrently polled batch and break per-chat
// order.
func (s *Service) admit(ctx context.Context, u botapi.Update) {
	s.mu.Lock()
	if s.holding {
		if len(s.hold) >= HoldQueueCap {
			dropped := s.hold[0]
			s.hold = s.hold[1:]
			slog.Warn("telegram: hold queue full; dropping oldest update", "update_id", dropped.ID)
		}
		s.hold = append(s.hold, u)
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	s.dispatchUpdate(ctx, u)
}

// dispatchHeld drains the hold queue in admission order after a resume. The
// dispatch mutex is held for the whole drain so a concurrently polled batch
// cannot interleave between held updates and break per-chat order; the poll
// loop simply waits for the drain to finish.
func (s *Service) dispatchHeld(held []botapi.Update) {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	ctx := s.root
	for _, u := range held {
		if ctx.Err() != nil {
			return
		}
		s.dispatchUpdate(ctx, u)
	}
}

// dispatchUpdate routes one admitted update: callback taps to the approval
// path, messages to the command/authz/dispatch pipeline. Update shapes the
// channel does not consume are dropped silently.
func (s *Service) dispatchUpdate(ctx context.Context, u botapi.Update) {
	switch {
	case u.CallbackQuery != nil:
		s.handleCallback(ctx, u.CallbackQuery)
	case u.Message != nil:
		s.handleMessage(ctx, u.Message)
	}
}

// alreadySeen reports and marks the process-local dedupe set.
func (s *Service) alreadySeen(updateID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[updateID]; ok {
		return true
	}
	s.seen[updateID] = time.Now()
	return false
}

// markSeen records an update as observed without the check (the receipt path
// already established it as processed).
func (s *Service) markSeen(updateID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[updateID]; !ok {
		s.seen[updateID] = time.Now()
	}
}

// botUsername returns the connected bot's @username (the receipt key's other
// half); empty before the first connect — receipts keyed by the empty
// username are still consistent within the process's lifetime.
func (s *Service) botUsername() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.me == nil {
		return ""
	}
	return s.me.Username
}

// cachedMe returns the connected bot's identity, nil before the first
// connect.
func (s *Service) cachedMe() *botapi.User {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.me
}

// pruneLazily sweeps the seen set and the store receipts on the hourly
// cadence, dropping entries older than ReceiptTTL (design D9).
func (s *Service) pruneLazily(ctx context.Context) {
	s.mu.Lock()
	if time.Since(s.lastPrune) < pruneInterval {
		s.mu.Unlock()
		return
	}
	s.lastPrune = time.Now()
	s.mu.Unlock()

	cutoff := time.Now().Add(-ReceiptTTL)
	s.mu.Lock()
	for id, at := range s.seen {
		if at.Before(cutoff) {
			delete(s.seen, id)
		}
	}
	s.mu.Unlock()
	if err := s.chans.PruneUpdateReceipts(ctx, cutoff); err != nil {
		slog.Warn("telegram: receipt prune failed", "error", err)
	}
}
