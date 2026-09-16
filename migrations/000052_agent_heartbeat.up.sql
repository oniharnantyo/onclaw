-- Agent heartbeats (add-agent-heartbeat design D1): one opt-in proactive
-- wakeup per agent — a periodic ambient tick in which the agent reviews its
-- HEARTBEAT checklist plus workspace activity and either reports or stays
-- silent. Not columns on agents: next_tick_at/last_tick/failure_streak churn
-- every tick and must not touch the agent row's updated_at semantics.
-- Validation lives in the domain layer; the schema only pins the shape:
-- exactly one heartbeat per agent, nullable active-hours pairing, delivery
-- target type, and the partial index the due-claim scan rides on. Enabled
-- defaults to false — heartbeats are opt-in and no rows exist until first
-- enable, so deploy order is unconstrained.

CREATE TABLE agent_heartbeats (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id       uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    created_by     uuid REFERENCES users(id) ON DELETE SET NULL,
    prompt         text NOT NULL DEFAULT '',
    expr           text NOT NULL,
    active_start   text,
    active_end     text,
    delivery       jsonb NOT NULL,
    enabled        boolean NOT NULL DEFAULT false,
    next_tick_at   timestamptz,
    last_tick      jsonb,
    failure_streak integer NOT NULL DEFAULT 0,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_agent_heartbeats_workspace_id_agent_id UNIQUE (workspace_id, agent_id),
    CONSTRAINT chk_agent_heartbeats_delivery_type CHECK (delivery->>'type' IN ('creator_dm', 'channel')),
    CONSTRAINT chk_agent_heartbeats_active_hours_shape CHECK (
        (active_start IS NULL AND active_end IS NULL) OR
        (active_start IS NOT NULL AND active_end IS NOT NULL)
    )
);

-- The claim query's access path: enabled heartbeats with a pending tick.
-- Uniqueness is not indexable beyond the table constraint — the domain
-- validator owns expression validity and the five-minute cadence floor,
-- never a DB constraint.
CREATE INDEX idx_agent_heartbeats_due
    ON agent_heartbeats(next_tick_at)
    WHERE enabled AND next_tick_at IS NOT NULL;

-- Per-tick run records (add-agent-heartbeat design D15, mirroring
-- scheduler_runs): one row per execution of a heartbeat, carrying trigger,
-- status, token usage and delivery outcome. The transcript itself persists
-- as session events addressable via session_id ("hb_<agentID>" — every tick
-- appends to the agent's one shared session); workspace_id and agent_id are
-- denormalized (house tenant rule) without their own FKs — the cascading
-- heartbeat FK already pins the row to one workspace and agent.
CREATE TABLE heartbeat_runs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    uuid NOT NULL,
    heartbeat_id    uuid NOT NULL REFERENCES agent_heartbeats(id) ON DELETE CASCADE,
    agent_id        uuid NOT NULL,
    session_id      text NOT NULL,
    trigger         text NOT NULL CHECK (trigger IN ('tick', 'manual')),
    status          text NOT NULL CHECK (status IN ('running', 'completed', 'failed', 'cancelled', 'blocked', 'skipped')),
    started_at      timestamptz NOT NULL,
    duration_ms     bigint NOT NULL DEFAULT 0,
    tokens_used     integer NOT NULL DEFAULT 0,
    delivery_status text NOT NULL DEFAULT '',
    error           text NOT NULL DEFAULT '',
    trace_id        text NOT NULL DEFAULT ''
);

-- Runs listing: newest first per heartbeat.
CREATE INDEX idx_heartbeat_runs_heartbeat_started
    ON heartbeat_runs(heartbeat_id, started_at DESC);
