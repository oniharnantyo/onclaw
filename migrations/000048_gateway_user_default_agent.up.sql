-- Gateway per-user default agent + active session suffix cursor
-- (integrate-telegram-gateway design D3).

-- The member's per-user default agent choice for Telegram direct messages,
-- set with /agent <name> from the paired platform identity. Nullable: an
-- absent value falls back to the gateway's configured default agent.
ALTER TABLE gateway_user_links
    ADD COLUMN default_agent_id uuid REFERENCES agents(id) ON DELETE SET NULL;

-- Active session suffix cursor (design D3): the deterministic gateway
-- session keys are tg_dm_<platform_user_id>_<agent_id> and
-- tg_group_<chat_id>_<agent_id>, and the ACTIVE key is the base key (suffix
-- 0) or the highest _<n> suffix. /new mints the next suffix. One row per
-- (platform, platform chat, agent); the row persists across restarts so a
-- restarted gateway resumes the highest suffix instead of rewinding onto a
-- live transcript. DM chats key on the platform user id, group chats on the
-- chat id — the same column serves both.
CREATE TABLE gateway_active_sessions (
    platform          text NOT NULL,
    platform_chat_id  text NOT NULL,
    agent_id          uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    workspace_id      uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    suffix            bigint NOT NULL DEFAULT 0,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (platform, platform_chat_id, agent_id)
);
