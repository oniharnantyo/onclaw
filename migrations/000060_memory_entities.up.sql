-- Entity–event graph (wave3-memory-vectors-and-graph D7/D8/D10): the
-- associative store. memory_entities holds tenant-scoped entity labels —
-- pointers, not content: deliberately NO visibility column, because all
-- access control lives on the edges and the linked rows (D7), which is what
-- makes "traversal cannot leak" a structural property. Entity creation is
-- idempotent per (workspace, normalized label); the label carries a
-- provenance stamp (origin, learned_at, first source event id) at birth.
--
-- memory_entity_edges is the bipartite graph to extracted notes and events.
-- Each edge inherits the narrowest visibility of what it links at birth and
-- is never widened afterward — widening happens only through the existing
-- human promotion path on the linked row. Edge reads take scope from the
-- LIVE linked row (the same rule the vector channel uses), so traversal
-- reaches exactly as far as the direct read path does. Consolidation folds
-- (D10) re-point edges onto a surviving entity and never delete a row.

CREATE TABLE memory_entities (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    label            text NOT NULL,
    normalized_label text NOT NULL,
    origin           text NOT NULL CHECK (origin IN ('manual', 'dialogue', 'infer', 'doc')),
    source_event_id  text NOT NULL,
    learned_at       timestamptz NOT NULL DEFAULT now(),
    created_at       timestamptz NOT NULL DEFAULT now()
);

-- Identity per workspace: resolving the same normalized label yields the
-- same entity row (the hard idempotency of D7).
CREATE UNIQUE INDEX uq_memory_entities_workspace_normalized
    ON memory_entities(workspace_id, normalized_label);

CREATE INDEX idx_memory_entities_workspace ON memory_entities(workspace_id);

-- Label search coverage (wave3 spec): trigram GIN plus tsvector expression
-- index over the label, following the memory_notes precedent. The seed
-- resolution of D8 (exact normalized first, then prefix/trigram) rides the
-- trigram leg; the tsvector leg is standing coverage for label-shaped
-- queries. Expression index and query must keep the exact same arguments.
CREATE INDEX idx_memory_entities_label_trgm
    ON memory_entities USING gin (label gin_trgm_ops);
CREATE INDEX idx_memory_entities_label_tsv
    ON memory_entities USING gin (to_tsvector('english', label));

CREATE TABLE memory_entity_edges (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id       uuid NOT NULL REFERENCES memory_entities(id) ON DELETE CASCADE,
    target_type     text NOT NULL CHECK (target_type IN ('note', 'event')),
    target_id       text NOT NULL,
    visibility      text NOT NULL CHECK (visibility IN ('shared', 'user', 'agent')),
    origin          text NOT NULL CHECK (origin IN ('manual', 'dialogue', 'infer', 'doc')),
    source_event_id text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);

-- One edge per (entity, target): re-mentioning the same row links nothing
-- twice, so reprocessed ingestion never duplicates graph rows.
CREATE UNIQUE INDEX uq_memory_entity_edges_target
    ON memory_entity_edges(entity_id, target_type, target_id);

-- The depth-1 traversal expansion (D8) reads by entity; the reverse target
-- lookup serves hygiene passes and per-row provenance questions. The
-- workspace partition comes through the entity FK — no query runs without a
-- workspace scope, and the workspace resolves via the memory_entities join.
CREATE INDEX idx_memory_entity_edges_entity ON memory_entity_edges(entity_id);
CREATE INDEX idx_memory_entity_edges_target ON memory_entity_edges(target_type, target_id);
