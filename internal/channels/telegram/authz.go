package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/store"
)

// inlet is the routing identity one inbound message carries through the
// dispatch pipeline: which chat, its kind, the display title the persistent
// session is named from, and who sent it.
type inlet struct {
	chatID     int64
	kind       string // a store.ChannelChatKind* value
	title      string
	senderID   int64
	senderName string
}

// decision is the authorization outcome for one message (design D4: default
// deny; every lookup error denies).
type decision struct {
	allow bool
	at    inlet
	// unknownSender marks an un-allowlisted DM sender: the pairing path
	// replies with the sender's numeric id and records a pending pairing.
	unknownSender bool
}

// userDisplay renders a sender's display name for titles and pairing cards.
func userDisplay(u *botapi.User) string {
	if u == nil {
		return ""
	}
	name := u.FirstName
	if u.Username != "" {
		name += " (@" + u.Username + ")"
	}
	return strings.TrimSpace(name)
}

// authorize gates one message (design D4). DMs require the sender on the
// allowlist; groups require the chat row (kind=group), the sender on the
// allowlist, and a trigger gate: the bot mentioned, a reply to the bot, or
// the chat's free-response flag. Any store failure denies (fail closed).
func (s *Service) authorize(ctx context.Context, msg *botapi.Message, me *botapi.User) decision {
	if msg == nil || msg.Chat == nil || msg.From == nil || me == nil {
		return decision{}
	}
	switch msg.Chat.Type {
	case "private":
		return s.authorizeDM(ctx, msg)
	case "group", "supergroup":
		return s.authorizeGroup(ctx, msg, me)
	default:
		return decision{}
	}
}

// authorizeDM applies the DM rule: allowlisted sender passes; an unknown
// sender is flagged for the pairing path; a lookup failure fails closed.
func (s *Service) authorizeDM(ctx context.Context, msg *botapi.Message) decision {
	ok, err := s.allowlisted(ctx, msg.From.ID)
	if err != nil {
		slog.Warn("telegram: allowlist lookup failed; denying", "user", msg.From.ID, "error", err)
		return decision{}
	}
	at := inlet{
		chatID:     msg.Chat.ID,
		kind:       store.ChannelChatKindDM,
		title:      userDisplay(msg.From),
		senderID:   msg.From.ID,
		senderName: userDisplay(msg.From),
	}
	if !ok {
		return decision{at: at, unknownSender: true}
	}
	return decision{allow: true, at: at}
}

// authorizeGroup applies the group rules. The chat row must pre-exist (the
// operator adds groups through the Channels pane), the sender must be
// allowlisted, and the message must trigger: our bot mentioned, a reply to
// our bot, or the chat's free-response flag. A message addressing another
// bot is ignored outright.
func (s *Service) authorizeGroup(ctx context.Context, msg *botapi.Message, me *botapi.User) decision {
	row, err := s.chans.GetChannelChat(ctx, msg.Chat.ID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Warn("telegram: chat lookup failed; denying", "chat", msg.Chat.ID, "error", err)
		}
		return decision{} // not an enrolled group, or lookup failed: silent deny
	}
	if row.Kind != store.ChannelChatKindGroup {
		return decision{}
	}
	ok, err := s.allowlisted(ctx, msg.From.ID)
	if err != nil {
		slog.Warn("telegram: allowlist lookup failed; denying", "user", msg.From.ID, "error", err)
		return decision{}
	}
	if !ok {
		return decision{}
	}

	text := messageText(msg)
	replyToBot := msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil &&
		msg.ReplyToMessage.From.ID == me.ID

	// Gate order (design D4): an @mention of this bot triggers outright;
	// a foreign @handle with no mention of this bot is another bot's
	// business and is ignored; otherwise reply-to-bot or the chat's
	// free-response flag triggers, and anything else is unaddressed
	// chatter — dropped silently, the decision recorded here.
	trigger := mentionsBot(text, me.Username)
	if !trigger && mentionsOtherBot(text, me.Username) {
		return decision{}
	}
	if !trigger && !replyToBot && !row.FreeResponse {
		return decision{}
	}

	title := row.Title
	if title == "" {
		title = msg.Chat.Title
	}
	return decision{
		allow: true,
		at: inlet{
			chatID:     msg.Chat.ID,
			kind:       store.ChannelChatKindGroup,
			title:      title,
			senderID:   msg.From.ID,
			senderName: userDisplay(msg.From),
		},
	}
}

// allowlisted reports whether the user id is on the DM allowlist.
func (s *Service) allowlisted(ctx context.Context, userID int64) (bool, error) {
	users, err := s.chans.ListChannelUsers(ctx)
	if err != nil {
		return false, err
	}
	for _, u := range users {
		if u.TelegramUserID == userID {
			return true, nil
		}
	}
	return false, nil
}

// handlePairing serves an un-allowlisted DM sender (design D4): reply with
// their numeric id — the value the operator enters in the Channels pane — and
// record one pending pairing (one per user, total cap PairingCap).
func (s *Service) handlePairing(ctx context.Context, msg *botapi.Message) {
	display := userDisplay(msg.From)
	notice := "Your Telegram user id is " + fmt.Sprintf("%d", msg.From.ID) +
		". Ask the operator to allowlist it in the Channels pane."
	if err := s.sendNotice(ctx, msg.Chat.ID, notice); err != nil {
		slog.Warn("telegram: pairing reply failed", "user", msg.From.ID, "error", err)
	}

	pairings, err := s.chans.ListPendingPairings(ctx)
	if err != nil {
		slog.Warn("telegram: pairing list failed", "error", err)
		return
	}
	for _, p := range pairings {
		if p.TelegramUserID == msg.From.ID {
			return // one pending pairing per user
		}
	}
	if len(pairings) >= PairingCap {
		slog.Warn("telegram: pairing cap reached; dropping request", "user", msg.From.ID)
		return
	}
	if err := s.chans.AddChannelPairing(ctx, store.ChannelPairing{
		TelegramUserID: msg.From.ID,
		DisplayName:    display,
	}); err != nil {
		slog.Warn("telegram: pairing record failed", "user", msg.From.ID, "error", err)
	}
}

// messageText returns a message's text or caption (whichever carries
// content).
func messageText(msg *botapi.Message) string {
	if msg.Text != "" {
		return msg.Text
	}
	return msg.Caption
}

// mentionRe matches an @handle in message text. The Bot API update carries
// mention entities, but the channel's consumed field set keys off the text
// itself: any @handle other than the connected bot's reads as another bot's
// address and the message is ignored (the conservative reading of design
// D4's foreign-bot filter).
var mentionRe = regexp.MustCompile(`@[A-Za-z0-9_]{3,}`)

// mentionsBot reports whether the text addresses the given bot username.
func mentionsBot(text, botUsername string) bool {
	if botUsername == "" {
		return false
	}
	for _, m := range mentionRe.FindAllString(text, -1) {
		if strings.EqualFold(strings.TrimPrefix(m, "@"), botUsername) {
			return true
		}
	}
	return false
}

// mentionsOtherBot reports whether the text addresses an @handle that is not
// the connected bot (treated as another bot's mention, design D4).
func mentionsOtherBot(text, botUsername string) bool {
	for _, m := range mentionRe.FindAllString(text, -1) {
		if !strings.EqualFold(strings.TrimPrefix(m, "@"), botUsername) {
			return true
		}
	}
	return false
}
