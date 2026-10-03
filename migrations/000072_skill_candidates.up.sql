-- Skill curation stores (add-skill-curation-from-traces 2.3, design D3/D6):
-- the proposed-skill review queue (skill_candidates) and its append-only
-- audit trail (skill_impact_entries). The DB is the system of record for
-- both — the wiki stays on disk (D3) and the audit trail deliberately does
-- NOT, so the review surface and the proposer's suppression input read one
-- source with no dual-write drift. Every row carries the tenant partition
-- (workspace_id) and app-managed timestamps (000054 house rule).
--
-- skill_candidates models the full review lifecycle in one row (D6):
-- pending -> approved -> provisional (probation tallies) and -> disabled
-- (archived, evidence retained); rejected rows keep their reviewer reason
-- and suppress their cluster. failed marks an extraction the proposer
-- dropped after its bounded retries — the reviewer can retry it from the
-- review surface. Superseding edits version the lineage in the
-- row itself: supersedes_skill_name names the prior skill and
-- superseded_content preserves its content. Evidence pointers (raw event
-- ids, cited wiki patterns) are jsonb arrays, never NULL.
--
-- skill_impact_entries is append-only: nothing overwrites or deletes an
-- entry, so "what happened to this cluster" stays answerable forever.
--
-- The agent-level skill curation side-call override pair (2.1) rides here
-- too — text NOT NULL DEFAULT '' exactly like the memory side-call pair
-- (000057), so both empty columns mean fall-through and the agents store's
-- explicit column list round-trips them.

ALTER TABLE agents
    ADD COLUMN skill_curation_provider_id text NOT NULL DEFAULT '',
    ADD COLUMN skill_curation_model text NOT NULL DEFAULT '';

CREATE TABLE skill_candidates (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id              uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    cluster_id            text NOT NULL,
    skill_name            text NOT NULL,
    status                text NOT NULL DEFAULT 'pending'
                          CHECK (status IN ('pending', 'approved', 'rejected', 'provisional', 'disabled', 'failed')),
    proposed_content      text NOT NULL,
    is_edit               boolean NOT NULL DEFAULT false,
    supersedes_skill_name text NOT NULL DEFAULT '',
    superseded_content    text NOT NULL DEFAULT '',
    evidence_event_ids    jsonb NOT NULL DEFAULT '[]',
    cited_pattern_refs    jsonb NOT NULL DEFAULT '[]',
    reason                text NOT NULL DEFAULT '',
    helpful_count         integer NOT NULL DEFAULT 0 CHECK (helpful_count >= 0),
    harmful_count         integer NOT NULL DEFAULT 0 CHECK (harmful_count >= 0),
    use_count             integer NOT NULL DEFAULT 0 CHECK (use_count >= 0),
    proposed_at           timestamptz NOT NULL,
    decided_at            timestamptz,
    updated_at            timestamptz NOT NULL,
    -- Versioned lineage: an edit must name the skill it supersedes;
    -- first-time proposals carry neither column.
    CONSTRAINT chk_skill_candidates_edit CHECK (
        (is_edit AND supersedes_skill_name <> '') OR
        (NOT is_edit AND supersedes_skill_name = '')
    )
);

CREATE INDEX idx_skill_candidates_workspace_status ON skill_candidates(workspace_id, status);
CREATE INDEX idx_skill_candidates_workspace_cluster ON skill_candidates(workspace_id, cluster_id);
CREATE INDEX idx_skill_candidates_workspace_agent ON skill_candidates(workspace_id, agent_id);

CREATE TABLE skill_impact_entries (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    cluster_id   text NOT NULL,
    skill_name   text NOT NULL,
    verdict      text NOT NULL CHECK (verdict IN ('approved', 'rejected', 'superseded')),
    diff         text NOT NULL DEFAULT '',
    reason       text NOT NULL DEFAULT '',
    reviewer     text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL
);

CREATE INDEX idx_skill_impact_entries_workspace ON skill_impact_entries(workspace_id, created_at);
CREATE INDEX idx_skill_impact_entries_workspace_cluster ON skill_impact_entries(workspace_id, cluster_id);
