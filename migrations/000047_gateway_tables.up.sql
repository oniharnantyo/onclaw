-- Gateway tables (integrate-telegram-gateway design D12): workspace-scoped
-- gateway configuration, member identity pairing, single-use pairing
-- tokens, agent-scoped chat bindings, and the delivery outbox. Validation
-- lives in the domain layer; the schema pins the shapes: one gateway per
-- (workspace, platform), transport/webhook exclusivity, one identity link
-- per (platform, platform_user_id, workspace), one binding per
-- (platform, platform_chat_id) GLOBALLY (a platform chat belongs to exactly
-- one workspace's agent — design D2), and pending-only outbox claims.

CREATE TABLE workspace_gateways (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    platform              text NOT NULL,
    bot_token_ciphertext  text NOT NULL,
    bot_username          text NOT NULL DEFAULT '',
    enabled               boolean NOT NULL DEFAULT false,
    transport             text NOT NULL CHECK (transport IN ('webhook', 'long_polling')),
    webhook_url           text NOT NULL DEFAULT '',
    default_agent_id      uuid REFERENCES agents(id) ON DELETE SET NULL,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_workspace_gateways_workspace_platform UNIQUE (workspace_id, platform),
    CONSTRAINT chk_workspace_gateways_transport_shape CHECK (
        (transport = 'webhook' AND webhook_url <> '') OR
        (transport = 'long_polling')
    )
);

-- Member identity links (design D6): the immutable platform user id is the
-- identity key; the username is display-only. The natural PK covers the
-- pairing lookup (routing resolves the paired member per platform identity)
-- and the re-pair conflict path.
CREATE TABLE gateway_user_links (
    platform          text NOT NULL,
    platform_user_id  text NOT NULL,
    workspace_id      uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id           uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    platform_username text NOT NULL DEFAULT '',
    created_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (platform, platform_user_id, workspace_id)
);

-- The pairing modal's per-member listing ("current linked identity").
CREATE INDEX idx_gateway_user_links_workspace_user
    ON gateway_user_links(workspace_id, user_id);

-- Pairing tokens (design D6): single-use, crypto-random, expiring. The
-- token itself is the lookup key; consumed_at marks the single use.
CREATE TABLE gateway_pairing_tokens (
    token       text PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Chat bindings (design D2): agent-scoped, unique per platform chat across
-- ALL workspaces — a group chat routes to exactly one agent. The agent FK
-- cascades (deleting an agent unbinds its chats); the workspace FK pins the
-- row to the tenant.
CREATE TABLE gateway_chat_bindings (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    platform         text NOT NULL,
    platform_chat_id text NOT NULL,
    agent_id         uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    created_by       uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_gateway_chat_bindings_platform_chat UNIQUE (platform, platform_chat_id)
);

CREATE INDEX idx_gateway_chat_bindings_workspace
    ON gateway_chat_bindings(workspace_id);

-- Delivery outbox (design D9): rows are written before the send and
-- redelivered after a crash (at-least-once). workspace_id is denormalized
-- (house tenant rule) without its own FK, mirroring scheduler_runs; payload
-- is opaque JSON owned by the gateway service.
CREATE TABLE gateway_outbox (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  uuid NOT NULL,
    session_id    text NOT NULL,
    payload       jsonb NOT NULL,
    status        text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'dead')),
    attempts      integer NOT NULL DEFAULT 0,
    deliver_after timestamptz NOT NULL DEFAULT now(),
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- The claim scan's access path (ClaimDue) and the retention prune's.
CREATE INDEX idx_gateway_outbox_due
    ON gateway_outbox(deliver_after)
    WHERE status = 'pending';

CREATE INDEX idx_gateway_outbox_prune
    ON gateway_outbox(created_at)
    WHERE status = 'delivered';
