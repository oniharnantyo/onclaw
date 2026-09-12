-- Channel teams (channel-teams design D7): the facilitator membership role,
-- work sessions as first-class rows, and the session linkage on feed
-- messages. Migration number 0040 continues after v1's 000031 wave; the
-- 000032–000039 gap is intentional (golang-migrate tolerates gaps).

-- D2: a per-channel role on the roster. At most one facilitator per channel,
-- enforced by the partial unique index below (store-level pre-checks map the
-- violation to domain.ErrChannelFacilitatorExists).
ALTER TABLE channel_members
    ADD COLUMN role text NOT NULL DEFAULT 'member'
    CHECK (role IN ('member', 'facilitator'));
CREATE UNIQUE INDEX uq_channel_members_channel_facilitator
    ON channel_members(channel_id) WHERE role = 'facilitator';

-- D1: work sessions are objects, not conventions. One row per bounded
-- engagement, rooted at the kickoff message, with the lifecycle state machine
-- open → paused(awaiting-human | budget-exhausted) → closed(summary); closed
-- is terminal. Active-session uniqueness is enforced by the store's
-- lock-and-check insert, not a schema constraint.
CREATE TABLE channel_work_sessions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id      uuid NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    root_message_id uuid NOT NULL REFERENCES channel_messages(id) ON DELETE CASCADE,
    goal            text NOT NULL,
    status          text NOT NULL DEFAULT 'open'
                    CHECK (status IN ('open', 'paused', 'closed')),
    pause_reason    text CHECK (pause_reason IN ('awaiting-human', 'budget-exhausted')),
    budget          int  NOT NULL DEFAULT 12,
    hops_used       int  NOT NULL DEFAULT 0,
    summary         text,
    closed_at       timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_channel_work_sessions_channel ON channel_work_sessions(channel_id, status);

-- D1: feed rows in a session link to it; the kickoff flag marks the message
-- a session was kicked off from (never parsed from text). The FK is on the
-- session side, so the kickoff message is inserted first and stamped with
-- work_session_id by the store once the session row exists.
ALTER TABLE channel_messages
    ADD COLUMN work_session_id uuid REFERENCES channel_work_sessions(id) ON DELETE SET NULL,
    ADD COLUMN is_kickoff      boolean NOT NULL DEFAULT false;
