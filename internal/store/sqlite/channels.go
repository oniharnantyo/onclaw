package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

type channelStore struct{ e executor }

const channelColumns = `id, enabled, bot_token, proxy_url, created_at, updated_at`

const channelChatColumns = `chat_id, kind, title, session_id, pinned_agent_id, is_home, free_response`

// GetChannel returns the channel row with the given slug id.
func (s *channelStore) GetChannel(ctx context.Context, id string) (*store.Channel, error) {
	row := s.e.QueryRowContext(ctx, `SELECT `+channelColumns+` FROM channels WHERE id = ?`, id)
	ch, err := scanChannel(row)
	if err != nil {
		return nil, readErr("get channel", err)
	}
	return ch, nil
}

// SaveChannel upserts the channel atomically: one INSERT ... ON CONFLICT
// statement either inserts the row or rewrites the configuration columns,
// keeping the original created_at (add-telegram-channel task 1.3).
func (s *channelStore) SaveChannel(ctx context.Context, ch *store.Channel) error {
	stampCreated(&ch.CreatedAt, &ch.UpdatedAt)
	if ch.UpdatedAt.IsZero() {
		ch.UpdatedAt = nowUTC()
	}

	_, err := s.e.ExecContext(ctx, `
INSERT INTO channels (`+channelColumns+`)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
enabled = excluded.enabled, bot_token = excluded.bot_token,
proxy_url = excluded.proxy_url, updated_at = excluded.updated_at`,
		ch.ID, boolInt(ch.Enabled), ch.BotToken, ch.ProxyURL,
		formatTime(ch.CreatedAt), formatTime(ch.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("save channel %s: %w", ch.ID, err)
	}
	return nil
}

func (s *channelStore) ListChannelUsers(ctx context.Context) ([]store.ChannelUser, error) {
	rows, err := s.e.QueryContext(ctx, `
SELECT telegram_user_id, display_name, created_at FROM channel_users
ORDER BY created_at, telegram_user_id`)
	if err != nil {
		return nil, fmt.Errorf("list channel users: %w", err)
	}
	defer rows.Close()

	users := make([]store.ChannelUser, 0)
	for rows.Next() {
		var u store.ChannelUser
		var createdAt string
		if err := rows.Scan(&u.TelegramUserID, &u.DisplayName, &createdAt); err != nil {
			return nil, fmt.Errorf("list channel users: %w", err)
		}
		if u.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, fmt.Errorf("list channel users: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list channel users: %w", err)
	}
	return users, nil
}

func (s *channelStore) AddChannelUser(ctx context.Context, u store.ChannelUser) error {
	if u.CreatedAt.IsZero() {
		u.CreatedAt = nowUTC()
	}
	_, err := s.e.ExecContext(ctx, `
INSERT INTO channel_users (telegram_user_id, display_name, created_at)
VALUES (?, ?, ?)`,
		u.TelegramUserID, u.DisplayName, formatTime(u.CreatedAt),
	)
	if err != nil {
		// telegram_user_id is the PRIMARY KEY: the allowlist holds one row
		// per user.
		if isUniqueConstraintErr(err) {
			return fmt.Errorf("add channel user %d: %w", u.TelegramUserID, store.ErrDuplicate)
		}
		return fmt.Errorf("add channel user %d: %w", u.TelegramUserID, err)
	}
	return nil
}

func (s *channelStore) RemoveChannelUser(ctx context.Context, telegramUserID int64) error {
	return removeByID(ctx, s.e, "remove channel user", telegramUserID,
		`DELETE FROM channel_users WHERE telegram_user_id = ?`)
}

func (s *channelStore) GetChannelChat(ctx context.Context, chatID int64) (*store.ChannelChat, error) {
	row := s.e.QueryRowContext(ctx, `SELECT `+channelChatColumns+` FROM channel_chats WHERE chat_id = ?`, chatID)
	chat, err := scanChannelChat(row)
	if err != nil {
		return nil, readErr("get channel chat", err)
	}
	return chat, nil
}

func (s *channelStore) ListChannelChats(ctx context.Context) ([]store.ChannelChat, error) {
	rows, err := s.e.QueryContext(ctx, `SELECT `+channelChatColumns+` FROM channel_chats ORDER BY chat_id`)
	if err != nil {
		return nil, fmt.Errorf("list channel chats: %w", err)
	}
	defer rows.Close()

	chats := make([]store.ChannelChat, 0)
	for rows.Next() {
		chat, err := scanChannelChat(rows)
		if err != nil {
			return nil, fmt.Errorf("list channel chats: %w", err)
		}
		chats = append(chats, *chat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list channel chats: %w", err)
	}
	return chats, nil
}

// SaveChannelChat upserts one chat by its chat id. Saving the home chat
// first clears every other row's home marker, so at most one chat is home
// (design D3: single home). Callers wanting the clear-and-save pair atomic
// run them inside WithTx.
func (s *channelStore) SaveChannelChat(ctx context.Context, c store.ChannelChat) error {
	if c.IsHome {
		if _, err := s.e.ExecContext(ctx, `UPDATE channel_chats SET is_home = 0 WHERE is_home = 1 AND chat_id != ?`, c.ChatID); err != nil {
			return fmt.Errorf("save channel chat %d: %w", c.ChatID, err)
		}
	}
	_, err := s.e.ExecContext(ctx, `
INSERT INTO channel_chats (`+channelChatColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(chat_id) DO UPDATE SET
kind = excluded.kind, title = excluded.title, session_id = excluded.session_id,
pinned_agent_id = excluded.pinned_agent_id, is_home = excluded.is_home,
free_response = excluded.free_response`,
		c.ChatID, c.Kind, c.Title, c.SessionID, nullAgentID(c.PinnedAgentID),
		boolInt(c.IsHome), boolInt(c.FreeResponse),
	)
	if err != nil {
		return fmt.Errorf("save channel chat %d: %w", c.ChatID, err)
	}
	return nil
}

func (s *channelStore) ListPendingPairings(ctx context.Context) ([]store.ChannelPairing, error) {
	rows, err := s.e.QueryContext(ctx, `
SELECT telegram_user_id, display_name, requested_at FROM channel_pairings
ORDER BY requested_at, telegram_user_id`)
	if err != nil {
		return nil, fmt.Errorf("list channel pairings: %w", err)
	}
	defer rows.Close()

	pairings := make([]store.ChannelPairing, 0)
	for rows.Next() {
		var p store.ChannelPairing
		var requestedAt string
		if err := rows.Scan(&p.TelegramUserID, &p.DisplayName, &requestedAt); err != nil {
			return nil, fmt.Errorf("list channel pairings: %w", err)
		}
		if p.RequestedAt, err = parseTime(requestedAt); err != nil {
			return nil, fmt.Errorf("list channel pairings: %w", err)
		}
		pairings = append(pairings, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list channel pairings: %w", err)
	}
	return pairings, nil
}

func (s *channelStore) AddChannelPairing(ctx context.Context, p store.ChannelPairing) error {
	if p.RequestedAt.IsZero() {
		p.RequestedAt = nowUTC()
	}
	_, err := s.e.ExecContext(ctx, `
INSERT INTO channel_pairings (telegram_user_id, display_name, requested_at)
VALUES (?, ?, ?)`,
		p.TelegramUserID, p.DisplayName, formatTime(p.RequestedAt),
	)
	if err != nil {
		// telegram_user_id is the PRIMARY KEY: one pending pairing per user.
		if isUniqueConstraintErr(err) {
			return fmt.Errorf("add channel pairing %d: %w", p.TelegramUserID, store.ErrDuplicate)
		}
		return fmt.Errorf("add channel pairing %d: %w", p.TelegramUserID, err)
	}
	return nil
}

func (s *channelStore) RemoveChannelPairing(ctx context.Context, telegramUserID int64) error {
	return removeByID(ctx, s.e, "remove channel pairing", telegramUserID,
		`DELETE FROM channel_pairings WHERE telegram_user_id = ?`)
}

// AddUpdateReceipt records one processed update idempotently: the composite
// primary key plus INSERT OR IGNORE makes a redelivered marker a no-op
// (design D9).
func (s *channelStore) AddUpdateReceipt(ctx context.Context, r store.ChannelUpdateReceipt) error {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = nowUTC()
	}
	if _, err := s.e.ExecContext(ctx, `
INSERT OR IGNORE INTO channel_update_receipts (bot_username, update_id, created_at)
VALUES (?, ?, ?)`,
		r.BotUsername, r.UpdateID, formatTime(r.CreatedAt),
	); err != nil {
		return fmt.Errorf("add channel update receipt %s/%d: %w", r.BotUsername, r.UpdateID, err)
	}
	return nil
}

func (s *channelStore) HasUpdateReceipt(ctx context.Context, botUsername string, updateID int64) (bool, error) {
	var n int
	if err := s.e.QueryRowContext(ctx, `
SELECT COUNT(*) FROM channel_update_receipts WHERE bot_username = ? AND update_id = ?`,
		botUsername, updateID).Scan(&n); err != nil {
		return false, fmt.Errorf("check channel update receipt %s/%d: %w", botUsername, updateID, err)
	}
	return n > 0, nil
}

func (s *channelStore) PruneUpdateReceipts(ctx context.Context, olderThan time.Time) error {
	if _, err := s.e.ExecContext(ctx,
		`DELETE FROM channel_update_receipts WHERE created_at < ?`, formatTime(olderThan.UTC()),
	); err != nil {
		return fmt.Errorf("prune channel update receipts: %w", err)
	}
	return nil
}

// removeByID runs one DELETE and maps zero rows affected onto the store
// sentinel — the shared absent-row path of the keyed sub-aggregates.
func removeByID(ctx context.Context, e executor, op string, id int64, query string) error {
	res, err := e.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%s %d: %w", op, id, store.ErrNotFound)
	}
	return nil
}

// nullAgentID encodes the nullable pinned-agent column: nil and the empty
// string are both "no override" and store as NULL.
func nullAgentID(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}

// scanChannel scans one channels row from a single- or multi-row result.
func scanChannel(row scanner) (*store.Channel, error) {
	var ch store.Channel
	var createdAt, updatedAt string
	if err := row.Scan(&ch.ID, &ch.Enabled, &ch.BotToken, &ch.ProxyURL, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	var err error
	if ch.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if ch.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &ch, nil
}

// scanChannelChat scans one channel_chats row from a single- or multi-row
// result.
func scanChannelChat(row scanner) (*store.ChannelChat, error) {
	var c store.ChannelChat
	var pinnedAgentID sql.NullString
	if err := row.Scan(&c.ChatID, &c.Kind, &c.Title, &c.SessionID, &pinnedAgentID,
		&c.IsHome, &c.FreeResponse); err != nil {
		return nil, err
	}
	if pinnedAgentID.Valid {
		c.PinnedAgentID = &pinnedAgentID.String
	}
	return &c, nil
}
