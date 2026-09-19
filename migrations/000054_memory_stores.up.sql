-- Memory stores (integrate-agent-zero-memory D4/D5/D6/D9): the episodic gist
-- timeline (memory_events) and the curated semantic fact store (memory_notes)
-- alongside the kept documents. Every row carries the two-axis scope —
-- workspace_id is the tenant partition and visibility ∈ {shared, user, agent}
-- is the within-tenant tier with owner columns pinned by CHECK constraints —
-- and the provenance birth tuple (origin, event_time, learned_at,
-- source_event_id), NOT NULL because no backfill path exists (D5). Owner FKs
-- CASCADE rather than SET NULL: nulling an owner would violate the visibility
-- CHECKs, so the row dies with its owner instead. Corrections supersede and
-- deletion tombstones — nothing is overwritten or erased (D6). Lexical
-- indexes (tsvector + trigram) back the hybrid read path; vectors are wave 3
-- (D9).

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE memory_events (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id        uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    session_id      text NOT NULL,
    turn_id         text NOT NULL,
    visibility      text NOT NULL CHECK (visibility IN ('shared', 'user', 'agent')),
    user_id         uuid REFERENCES users(id) ON DELETE CASCADE,
    origin          text NOT NULL CHECK (origin IN ('manual', 'dialogue', 'infer', 'doc')),
    event_time      timestamptz NOT NULL,
    learned_at      timestamptz NOT NULL DEFAULT now(),
    source_event_id text NOT NULL,
    description     text NOT NULL,
    outcome         text NOT NULL DEFAULT '',
    participants    jsonb NOT NULL DEFAULT '[]',
    tombstoned_at   timestamptz,
    -- Owner shape: the user column is set exactly when the tier is user; for
    -- the agent tier the producing agent_id is the owner (participant rule).
    CONSTRAINT chk_memory_events_owner CHECK (
        (visibility = 'user' AND user_id IS NOT NULL) OR
        (visibility IN ('shared', 'agent') AND user_id IS NULL)
    ),
    CONSTRAINT chk_memory_events_origin CHECK (origin IN ('manual', 'dialogue', 'infer', 'doc'))
);

CREATE TABLE memory_notes (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    visibility      text NOT NULL CHECK (visibility IN ('shared', 'user', 'agent')),
    user_id         uuid REFERENCES users(id) ON DELETE CASCADE,
    agent_id        uuid REFERENCES agents(id) ON DELETE CASCADE,
    origin          text NOT NULL CHECK (origin IN ('manual', 'dialogue', 'infer', 'doc')),
    event_time      timestamptz NOT NULL,
    learned_at      timestamptz NOT NULL DEFAULT now(),
    source_event_id text NOT NULL,
    content         text NOT NULL,
    importance      integer NOT NULL DEFAULT 0 CHECK (importance >= 0),
    pinned          boolean NOT NULL DEFAULT false,
    topic           text,
    conflict_flag   text,
    supersedes      uuid REFERENCES memory_notes(id),
    superseded_by   uuid REFERENCES memory_notes(id),
    promoted_by     uuid REFERENCES users(id) ON DELETE SET NULL,
    promoted_at     timestamptz,
    tombstoned_at   timestamptz,
    -- Owner shape per tier: exactly one owner column set for user/agent rows,
    -- none for shared rows. promoted_by/promoted_at record the audited human
    -- widening (promotion); SET NULL is safe there — the column is
    -- CHECK-free.
    CONSTRAINT chk_memory_notes_owner CHECK (
        (visibility = 'user' AND user_id IS NOT NULL AND agent_id IS NULL) OR
        (visibility = 'agent' AND agent_id IS NOT NULL AND user_id IS NULL) OR
        (visibility = 'shared' AND user_id IS NULL AND agent_id IS NULL)
    ),
    CONSTRAINT chk_memory_notes_origin CHECK (origin IN ('manual', 'dialogue', 'infer', 'doc'))
);

-- Tenant partition and the visibility tier composite every scope-filtered
-- read rides on.
CREATE INDEX idx_memory_events_workspace ON memory_events(workspace_id);
CREATE INDEX idx_memory_events_workspace_visibility ON memory_events(workspace_id, visibility);
CREATE INDEX idx_memory_notes_workspace ON memory_notes(workspace_id);
CREATE INDEX idx_memory_notes_workspace_visibility ON memory_notes(workspace_id, visibility);
CREATE INDEX idx_memory_notes_workspace_topic ON memory_notes(workspace_id, topic);

-- The gister's incremental window cursor and the chip's per-turn counts.
CREATE INDEX idx_memory_events_session ON memory_events(session_id);

-- Hybrid lexical retrieval (D9): tsvector expression indexes matched by the
-- search predicates plus trigram GIN for the ILIKE fallback. Expression
-- indexes and queries must keep the exact same to_tsvector arguments.
CREATE INDEX idx_memory_events_search
    ON memory_events USING gin (to_tsvector('english', description || ' ' || outcome));
CREATE INDEX idx_memory_events_trgm
    ON memory_events USING gin (description gin_trgm_ops, outcome gin_trgm_ops);
CREATE INDEX idx_memory_notes_search
    ON memory_notes USING gin (to_tsvector('english', content));
CREATE INDEX idx_memory_notes_trgm
    ON memory_notes USING gin (content gin_trgm_ops);
