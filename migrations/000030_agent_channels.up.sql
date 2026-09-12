-- Agent channels (design.md D5/D6): a channel is a room with a shared feed,
-- not a shared session. Three tables: the room, one heterogeneous membership
-- roster (users + agents with exactly-one reference), and the shared feed of
-- attributed messages. Every table denormalizes workspace_id (house tenant
-- rule); seq is a per-table identity assigned by the database and returned on
-- insert; mentions/run_summary are jsonb treated as opaque by the schema.

CREATE TABLE channels (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         text NOT NULL,              -- display name ("Production Ops")
    slug         text NOT NULL,              -- URL + #handle ("ops")
    purpose      text NOT NULL DEFAULT '',   -- header line
    conventions  text NOT NULL DEFAULT '',   -- freeform CHANNEL.md section (textarea exception)
    created_by   uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_channels_workspace_id_slug UNIQUE (workspace_id, slug)
);

CREATE TABLE channel_members (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id     uuid NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    member_type    text NOT NULL CHECK (member_type IN ('user', 'agent')),
    user_id        uuid REFERENCES users(id)  ON DELETE CASCADE,
    agent_id       uuid REFERENCES agents(id) ON DELETE CASCADE,
    specialization text NOT NULL DEFAULT '',  -- per-channel role note
    added_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_channel_members_one_ref
        CHECK ((user_id IS NOT NULL)::int + (agent_id IS NOT NULL)::int = 1)
);
CREATE UNIQUE INDEX uq_channel_members_channel_user  ON channel_members(channel_id, user_id)  WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX uq_channel_members_channel_agent ON channel_members(channel_id, agent_id) WHERE agent_id IS NOT NULL;

CREATE TABLE channel_messages (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id      uuid NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    seq             bigint GENERATED ALWAYS AS IDENTITY,
    author_type     text NOT NULL CHECK (author_type IN ('user', 'agent')),
    author_user_id  uuid REFERENCES users(id)  ON DELETE CASCADE,
    author_agent_id uuid REFERENCES agents(id) ON DELETE CASCADE,
    body            text NOT NULL,
    mentions        jsonb NOT NULL DEFAULT '[]',  -- resolved [{"type","id","handle"}]
    session_id      text,              -- run link (with turn_id); NULL for human msgs
    turn_id         text,
    run_summary     jsonb,             -- footprint, written at run finish: {"tools":{"grafana.query":2},"duration_ms":..}
    root_message_id uuid REFERENCES channel_messages(id) ON DELETE SET NULL,
    chain_depth     int  NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_channel_messages_author
        CHECK ((author_user_id IS NOT NULL)::int + (author_agent_id IS NOT NULL)::int = 1)
);
CREATE INDEX idx_channel_messages_channel_seq ON channel_messages(channel_id, seq DESC);
CREATE INDEX idx_channel_messages_root        ON channel_messages(root_message_id);
