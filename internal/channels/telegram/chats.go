package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/store"
)

// chatState is one chat's in-memory runtime: the inbound debounce timer, the
// FIFO turn queue, and the identity of the turn this channel submitted (the
// initiator rule's local half). Guarded by mu.
type chatState struct {
	mu sync.Mutex

	debounce     *time.Timer
	pendingText  string
	pendingAtts  []string
	pendingInlet inlet

	queue    []queuedItem
	draining bool // one drainer at a time (design D5's dequeue trigger)

	turnID  string // the turn this channel submitted; "" when idle
	agentID string // the agent the current turn runs on (artifact reads)
}

// queuedItem is one message waiting for the chat's session to free up.
type queuedItem struct {
	inlet inlet
	text  string
	atts  []string
}

// errNoAgent marks the resolution order finding nothing to run (design D7):
// no chat pin and no gateway default.
var errNoAgent = errors.New("no agent configured: pin one on the chat or set the gateway default agent")

// chatState returns (creating) the in-memory state for one chat.
func (s *Service) chatState(chatID int64) *chatState {
	s.chatsMu.Lock()
	defer s.chatsMu.Unlock()
	cs, ok := s.chats[chatID]
	if !ok {
		cs = &chatState{}
		s.chats[chatID] = cs
	}
	return cs
}

// handleMessage routes one admitted message update through authorization,
// commands, media, and the debounced text pipeline.
func (s *Service) handleMessage(ctx context.Context, msg *botapi.Message) {
	if msg.Chat == nil || msg.From == nil {
		return
	}
	me := s.cachedMe()
	if me == nil {
		// Off the poll loop (e.g. a hold-drain racing the first connect)
		// the identity resolves lazily; without it the group gates cannot
		// run and the message is dropped.
		_, resolved, err := s.botFor(ctx)
		if err != nil || resolved == nil {
			return
		}
		me = resolved
	}

	d := s.authorize(ctx, msg, me)
	if d.unknownSender {
		s.handlePairing(ctx, msg)
		return
	}
	if !d.allow {
		return
	}

	if cmd, ok := parseCommand(messageText(msg), me.Username); ok {
		s.handleCommand(ctx, d.at, cmd, me)
		return
	}

	if len(msg.Photo) > 0 || msg.Document != nil {
		// A media message flushes any pending text first so per-chat order
		// holds (the debounce merge is a text-paste concern).
		s.flushDebounce(d.at.chatID)
		s.handleMedia(ctx, d.at, msg)
		return
	}

	text := messageText(msg)
	if text == "" {
		return // stickers, voice notes (non-goal), service messages
	}
	s.admitText(d.at, text, nil)
}

// admitText starts (or extends) the chat's inbound debounce: multi-part
// pastes arriving within DebounceDelay merge into one dispatch (design D5).
func (s *Service) admitText(at inlet, text string, atts []string) {
	cs := s.chatState(at.chatID)
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.debounce != nil {
		cs.pendingText += "\n" + text
		cs.pendingAtts = append(cs.pendingAtts, atts...)
		return
	}
	cs.pendingText = text
	cs.pendingAtts = atts
	cs.pendingInlet = at
	cs.debounce = time.AfterFunc(s.t.debounce, func() { s.flushDebounce(at.chatID) })
}

// flushDebounce dispatches the merged text (or does nothing when no debounce
// is pending).
func (s *Service) flushDebounce(chatID int64) {
	cs := s.chatState(chatID)
	cs.mu.Lock()
	if cs.debounce == nil {
		cs.mu.Unlock()
		return
	}
	cs.debounce = nil
	item := queuedItem{inlet: cs.pendingInlet, text: cs.pendingText, atts: cs.pendingAtts}
	cs.pendingText, cs.pendingAtts = "", nil
	cs.mu.Unlock()

	s.dispatchItem(s.root, item)
}

// dispatchItem resolves the chat's row and persistent session, then submits
// the item as a channel-origin turn — or enqueues it when the session is
// busy (design D5: the FIFO queue is a caller concern; Submit rejects
// concurrent turns with ErrTurnInProgress).
func (s *Service) dispatchItem(ctx context.Context, item queuedItem) {
	if ctx.Err() != nil {
		return
	}
	chat, err := s.ensureChatRow(ctx, item.inlet)
	if err != nil {
		slog.Warn("telegram: chat row resolve failed", "chat", item.inlet.chatID, "error", err)
		s.noticeAsync(item.inlet.chatID, "Could not reach the chat registry; message dropped.")
		return
	}
	sessionID, agentID, err := s.ensureSession(ctx, chat)
	if err != nil {
		s.noticeAsync(item.inlet.chatID, "Could not start a session: "+err.Error())
		return
	}

	cs := s.chatState(item.inlet.chatID)
	cs.mu.Lock()
	if cs.turnID != "" {
		// This channel's own turn is running; its pump's terminal event
		// is the dequeue trigger — no separate drainer needed.
		s.enqueueLocked(cs, queuedItem{inlet: item.inlet, text: item.text, atts: item.atts})
		cs.mu.Unlock()
		return
	}
	cs.mu.Unlock()

	started, busy := s.submitTurn(ctx, chat.ChatID, sessionID, agentID, item)
	if started || !busy {
		return // started, dropped with a notice, or deferred — all reported
	}
	// A foreign turn raced in between the check and the Submit: queue the
	// item behind it; the drainer waits the foreign turn out.
	cs = s.chatState(item.inlet.chatID)
	cs.mu.Lock()
	s.enqueueLocked(cs, item)
	cs.mu.Unlock()
	go s.drainChatQueue(item.inlet.chatID)
}

// submitTurn performs one Submit attempt with session-vanish recovery and
// records the initiator turn on the chat state. busy reports an
// ErrTurnInProgress rejection (the item was queued only when the caller is
// the dispatch path — drains peek and retry instead).
func (s *Service) submitTurn(ctx context.Context, chatID int64, sessionID, agentID string, item queuedItem) (started, busy bool) {
	turn, err := s.rt.Submit(ctx, agents.SendRequest{
		SessionID:      sessionID,
		Message:        item.text,
		AttachmentKeys: item.atts,
		Origin:         hooks.OriginTelegram,
	})
	if errors.Is(err, agents.ErrTurnInProgress) {
		return false, true
	}
	if errors.Is(err, store.ErrNotFound) {
		// The pinned session was deleted out from under the chat: clear
		// the binding, re-create, retry once.
		slog.Info("telegram: pinned session vanished; re-creating", "chat", chatID, "session", sessionID)
		if chat, chatErr := s.ensureChatRow(ctx, item.inlet); chatErr == nil {
			_ = s.clearSessionBinding(ctx, chat)
			if sessionID, agentID, chatErr = s.ensureSession(ctx, chat); chatErr == nil {
				return s.submitTurn(ctx, chatID, sessionID, agentID, item)
			}
			s.noticeAsync(chatID, "Could not re-create the session: "+chatErr.Error())
			return false, false
		}
	}
	if err != nil {
		slog.Warn("telegram: submit failed", "chat", chatID, "error", err)
		s.noticeAsync(chatID, "Could not start a turn: "+err.Error())
		return false, false
	}

	cs := s.chatState(chatID)
	cs.mu.Lock()
	cs.turnID = turn.TurnID()
	cs.agentID = agentID
	cs.mu.Unlock()

	go s.pumpTurn(chatID, sessionID, turn, agentID)
	return true, false
}

// enqueueLocked appends to the chat's FIFO queue. The caller holds cs.mu.
// Over the cap the oldest item drops with a chat notice (design D5).
func (s *Service) enqueueLocked(cs *chatState, item queuedItem) {
	if len(cs.queue) >= ChatQueueCap {
		cs.queue = cs.queue[1:] // drop the oldest
		chatID := item.inlet.chatID
		slog.Warn("telegram: chat queue full; dropping oldest", "chat", chatID)
		go s.noticeAsync(chatID, "Message queue is full; the oldest queued message was dropped.")
	}
	cs.queue = append(cs.queue, item)
}

// finishTurn clears the chat's initiator record and drains the queue — the
// delivery pump's terminal event is the dequeue trigger (design D5).
func (s *Service) finishTurn(chatID int64) {
	cs := s.chatState(chatID)
	cs.mu.Lock()
	cs.turnID = ""
	cs.agentID = ""
	cs.mu.Unlock()
	go s.drainChatQueue(chatID)
}

// queueWaitAttempts bounds the Busy wait (~5 minutes) before a queued item
// gives up with a notice.
const queueWaitAttempts = 300

// drainChatQueue submits queued items in order until the queue empties or a
// turn is running again. It tolerates foreign turns: a session busy with a
// UI-origin turn is waited out (bounded), not raced. At most one drainer per
// chat is active — finishTurn and dispatch-time enqueues may both call, and
// the running drainer covers them.
func (s *Service) drainChatQueue(chatID int64) {
	cs := s.chatState(chatID)
	cs.mu.Lock()
	if cs.draining {
		cs.mu.Unlock()
		return
	}
	cs.draining = true
	cs.mu.Unlock()
	defer func() {
		cs.mu.Lock()
		cs.draining = false
		cs.mu.Unlock()
	}()

	ctx := s.root
	for {
		if ctx.Err() != nil {
			return
		}
		cs.mu.Lock()
		if len(cs.queue) == 0 {
			cs.mu.Unlock()
			return
		}
		item := cs.queue[0] // peeked: popped only when resolved
		cs.mu.Unlock()

		chat, err := s.ensureChatRow(ctx, item.inlet)
		if err != nil {
			s.popQueued(cs, "chat registry unreachable")
			continue
		}
		sessionID, agentID, err := s.ensureSession(ctx, chat)
		if err != nil {
			s.popQueued(cs, err.Error())
			continue
		}

		waits := 0
		for {
			started, busy := s.submitTurn(ctx, chat.ChatID, sessionID, agentID, item)
			if started {
				s.popQueued(cs, "")
				return // the new turn's pump drains the rest
			}
			if !busy {
				s.popQueued(cs, "") // dropped with a notice by submitTurn
				break
			}
			waits++
			if waits >= queueWaitAttempts {
				s.popQueued(cs, "the session stayed busy too long")
				break
			}
			if !sleepCtx(ctx, s.t.queueWaitRetry) {
				return
			}
		}
	}
}

// popQueued removes the queue head; an empty why is a silent pop (the item
// resolved), otherwise the chat learns why its queued message went.
func (s *Service) popQueued(cs *chatState, why string) {
	cs.mu.Lock()
	if len(cs.queue) == 0 {
		cs.mu.Unlock()
		return
	}
	item := cs.queue[0]
	cs.queue = cs.queue[1:]
	cs.mu.Unlock()
	if why != "" {
		s.noticeAsync(item.inlet.chatID, "Queued message dropped: "+why)
	}
}

// ensureChatRow returns the chat's persisted row, creating a DM row on first
// contact (groups must be enrolled by the operator, design D4) and refreshing
// a changed title.
func (s *Service) ensureChatRow(ctx context.Context, at inlet) (*store.ChannelChat, error) {
	row, err := s.chans.GetChannelChat(ctx, at.chatID)
	if errors.Is(err, store.ErrNotFound) {
		if at.kind != store.ChannelChatKindDM {
			return nil, fmt.Errorf("chat %d is not enrolled", at.chatID)
		}
		row = &store.ChannelChat{ChatID: at.chatID, Kind: store.ChannelChatKindDM, Title: at.title}
		if err := s.chans.SaveChannelChat(ctx, *row); err != nil {
			return nil, err
		}
		return row, nil
	}
	if err != nil {
		return nil, err
	}
	if at.title != "" && at.title != row.Title {
		row.Title = at.title
		if err := s.chans.SaveChannelChat(ctx, *row); err != nil {
			return nil, err
		}
	}
	return row, nil
}

// ensureSession returns the chat's persistent session id and its agent
// (design D5): the pinned binding when present, otherwise a fresh titled
// session — "<chat title> — Telegram" — whose binding is persisted
// (restart-safe).
func (s *Service) ensureSession(ctx context.Context, chat *store.ChannelChat) (sessionID, agentID string, err error) {
	if chat.SessionID != "" {
		sess, sessErr := s.sess.Session(ctx, chat.SessionID)
		if sessErr == nil {
			return sess.ID, sess.AgentID, nil
		}
		if !errors.Is(sessErr, store.ErrNotFound) {
			return "", "", sessErr
		}
		// The binding dangles (the session was deleted); fall through and
		// re-create below.
	}

	agentID, err = s.resolveAgent(ctx, chat)
	if err != nil {
		return "", "", err
	}
	sess, err := s.rt.CreateSession(ctx, agentID)
	if err != nil {
		return "", "", fmt.Errorf("create session: %w", err)
	}
	// The title is cosmetic (the rail reads it): a failure does not fail
	// the turn.
	if err := s.sess.SetTitle(ctx, sess.ID, chat.Title+" — Telegram"); err != nil {
		slog.Warn("telegram: session title failed", "session", sess.ID, "error", err)
	}

	persisted := *chat
	persisted.SessionID = sess.ID
	if err := s.chans.SaveChannelChat(ctx, persisted); err != nil {
		return "", "", fmt.Errorf("pin session binding: %w", err)
	}
	return sess.ID, agentID, nil
}

// clearSessionBinding un-pins the chat's session (/new, design D5); the old
// session stays in the rail.
func (s *Service) clearSessionBinding(ctx context.Context, chat *store.ChannelChat) error {
	persisted := *chat
	persisted.SessionID = ""
	if err := s.chans.SaveChannelChat(ctx, persisted); err != nil {
		return err
	}
	*chat = persisted
	return nil
}

// resolveAgent applies the per-chat resolution order (design D7): the chat's
// pinned agent, then the gateway default; neither is a chat-visible error.
func (s *Service) resolveAgent(ctx context.Context, chat *store.ChannelChat) (string, error) {
	if chat.PinnedAgentID != nil && *chat.PinnedAgentID != "" {
		return *chat.PinnedAgentID, nil
	}
	defs, err := s.defs.Get(ctx)
	if err != nil {
		return "", wrapStore("read gateway settings", err)
	}
	if defs != nil && defs.DefaultAgentID != "" {
		return defs.DefaultAgentID, nil
	}
	return "", errNoAgent
}
