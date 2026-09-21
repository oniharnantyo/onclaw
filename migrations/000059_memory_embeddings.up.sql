-- Vector evidence index (wave3-memory-vectors-and-graph D1/D5): the
-- polymorphic home for embeddings over extracted notes, episodic events, and
-- raw turn text. One table for three target kinds (raw turns have no row of
-- their own in the memory schema), with a per-row dimension so an
-- embedding-model change never migrates data — stale dimensions are excluded
-- from the vector channel at read time, never re-embedded (D5). Storage is
-- half precision (halfvec): unchanged retrieval quality at this scale, half
-- the bytes.
--
-- The pgvector extension is installed into pg_catalog, mirroring where
-- pg_trgm lives on this deployment, so the halfvec type and its operators
-- resolve in every schema context — the integration harness runs the full
-- migration chain per fresh schema under a single-entry search_path. A host
-- WITHOUT pgvector fails loudly right here; that gate is intended (design:
-- pgvector extension availability). The down path leaves the extension
-- installed, like 000054's pg_trgm: it drops only what this migration alone
-- owns, and other objects may already depend on the type.

CREATE EXTENSION IF NOT EXISTS vector SCHEMA pg_catalog;

CREATE TABLE memory_embeddings (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    target_type     text NOT NULL CHECK (target_type IN ('note', 'event', 'raw')),
    target_id       text NOT NULL,
    dimension       integer NOT NULL CHECK (dimension > 0),
    embedding       halfvec NOT NULL,
    visibility      text NOT NULL CHECK (visibility IN ('shared', 'user', 'agent')),
    -- Owner columns, shape mirroring memory_events. Raw rows need them: they
    -- have no linked row to inherit scope from, so the participant-rule
    -- snapshot stamps its owner directly (the session's single human, or the
    -- producing agent). Note/event rows stamp the same owners at write time,
    -- but reads take scope from the LIVE linked row — promotion widens
    -- through the row, never through a snapshot.
    user_id         uuid REFERENCES users(id) ON DELETE CASCADE,
    agent_id        uuid REFERENCES agents(id) ON DELETE CASCADE,
    source_event_id text NOT NULL,
    learned_at      timestamptz NOT NULL DEFAULT now(),
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_memory_embeddings_owner CHECK (
        (visibility = 'user' AND user_id IS NOT NULL AND agent_id IS NULL) OR
        (visibility = 'agent' AND agent_id IS NOT NULL AND user_id IS NULL) OR
        (visibility = 'shared' AND user_id IS NULL AND agent_id IS NULL)
    )
);

-- One embedding per (target, dimension): a re-embed at the same dimension
-- replaces the vector (idempotent ingestion), a model change adds a new
-- dimension and the old rows stay lexical-retrievable (D5). The workspace_id
-- leading column doubles as the tenant partition index and the
-- (workspace, target) lookup the supersede/tombstone cleanup rides on, so no
-- separate partition or target index is created.
CREATE UNIQUE INDEX uq_memory_embeddings_target_dimension
    ON memory_embeddings(workspace_id, target_type, target_id, dimension);

-- The dimension composite the vector channel's exclusion filter rides on.
CREATE INDEX idx_memory_embeddings_workspace_dimension
    ON memory_embeddings(workspace_id, dimension);
