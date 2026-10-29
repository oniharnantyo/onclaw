-- Schema v18: Telegram channel (add-telegram-channel design D3/D4). Five
-- tables following the schedules aggregate pattern: the channel config row
-- (id is a slug, "telegram" today; bot_token in the clear per the
-- providers.api_key precedent), the DM allowlist (telegram_user_id unique),
-- per-chat state (chat_id unique, one is_home row max enforced by the store,
-- nullable pinned_agent_id), pending pairings (telegram_user_id unique), and
-- the processed-update receipts keyed by (bot_username, update_id) — the Bot
-- API redelivers unacked updates for 24h, so the pair is the dedupe marker.
-- kind carries the dm|group CHECK. No backfill; rollback is DROP TABLE in
-- reverse creation order.
CREATE TABLE channels (
    id         TEXT PRIMARY KEY,
    enabled    INTEGER NOT NULL DEFAULT 0,
    bot_token  TEXT NOT NULL DEFAULT '',
    proxy_url  TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE channel_users (
    telegram_user_id INTEGER PRIMARY KEY,
    display_name     TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL
);

CREATE TABLE channel_chats (
    chat_id         INTEGER PRIMARY KEY,
    kind            TEXT NOT NULL CHECK (kind IN ('dm', 'group')),
    title           TEXT NOT NULL DEFAULT '',
    session_id      TEXT NOT NULL DEFAULT '',
    pinned_agent_id TEXT,
    is_home         INTEGER NOT NULL DEFAULT 0,
    free_response   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE channel_pairings (
    telegram_user_id INTEGER PRIMARY KEY,
    display_name     TEXT NOT NULL DEFAULT '',
    requested_at     TEXT NOT NULL
);

CREATE TABLE channel_update_receipts (
    bot_username TEXT NOT NULL,
    update_id    INTEGER NOT NULL,
    created_at   TEXT NOT NULL,
    PRIMARY KEY (bot_username, update_id)
);
