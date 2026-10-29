package telegram

import (
	"context"
	"strings"

	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/store"
)

// command is a parsed bot command. Unknown commands are not commands: the
// text passes through as an ordinary message.
type command struct {
	name string // new | stop | status
}

// parseCommand recognizes /new, /stop and /status — with the group-suffixed
// form /cmd@botusername. It reports ok=false for unknown or foreign commands
// (the caller dispatches ok=false text as an ordinary message; the
// foreign-command ignore happens in authorization).
func parseCommand(text, botUsername string) (command, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return command{}, false
	}
	token := text
	if i := strings.IndexAny(token, " \t\n"); i >= 0 {
		token = token[:i]
	}
	name := strings.TrimPrefix(token, "/")
	if i := strings.IndexByte(name, '@'); i >= 0 {
		target, ours := name[i+1:], botUsername
		name = name[:i]
		if ours == "" || !strings.EqualFold(target, ours) {
			// Addressed at another bot (or the bot name is not yet
			// known): not our command.
			return command{}, false
		}
	}
	switch strings.ToLower(name) {
	case "new", "stop", "status":
		return command{name: strings.ToLower(name)}, true
	default:
		return command{}, false
	}
}

// handleCommand executes one bot command. Commands bypass the debounce and
// the turn queue (design D5) — /stop must act on a busy session now.
func (s *Service) handleCommand(ctx context.Context, at inlet, cmd command, me *botapi.User) {
	chat, err := s.ensureChatRow(ctx, at)
	if err != nil {
		s.noticeAsync(at.chatID, "Could not reach the chat registry: "+err.Error())
		return
	}

	switch cmd.name {
	case "new":
		if err := s.clearSessionBinding(ctx, chat); err != nil {
			s.noticeAsync(at.chatID, "Could not start a new session: "+err.Error())
			return
		}
		s.noticeAsync(at.chatID, "Started a new session.")
	case "stop":
		if chat.SessionID == "" {
			s.noticeAsync(at.chatID, "Nothing is running.")
			return
		}
		if err := s.rt.Cancel(ctx, chat.SessionID); err != nil {
			s.noticeAsync(at.chatID, "Could not stop the turn: "+err.Error())
			return
		}
		s.noticeAsync(at.chatID, "Stopped.")
	case "status":
		s.noticeAsync(at.chatID, s.statusText(ctx, chat))
	}
}

// statusText renders /status: the resolved agent, the chat's pin, the
// session's model override (the cheaply available model signal), and the
// busy state.
func (s *Service) statusText(ctx context.Context, chat *store.ChannelChat) string {
	agent := "none"
	if resolved, err := s.resolveAgent(ctx, chat); err == nil {
		agent = resolved
	}

	pinned := "none"
	if chat.PinnedAgentID != nil && *chat.PinnedAgentID != "" {
		pinned = *chat.PinnedAgentID
	}

	model := "default"
	busy := "no"
	if chat.SessionID != "" {
		if sess, err := s.sess.Session(ctx, chat.SessionID); err == nil && sess.Model != "" {
			model = sess.Model
		}
		if s.rt.Busy(ctx, chat.SessionID) {
			busy = "yes"
		}
	}

	lines := []string{
		"Agent: " + agent,
		"Pinned: " + pinned,
		"Model: " + model,
		"Busy: " + busy,
	}
	if chat.SessionID == "" {
		lines = append(lines, "Session: none (one starts on your next message)")
	}
	return strings.Join(lines, "\n")
}
