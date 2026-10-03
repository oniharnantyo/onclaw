-- Skill-cluster run membership (add-skill-curation-from-traces 3.1, design
-- D2 two-tier rule): every ingested run — gate-passing or not — is indexed
-- into its similarity cluster as one row here, so the proposer's cluster
-- gate counts qualifying members and samples contrast evidence from the
-- store, never from process memory. One row per run: the unique key
-- (workspace_id, session_id, turn_id) makes re-ingest idempotent. The
-- structured tally the qualifier computed rides the row so scoring and
-- sampling never re-parse raw session events. App-managed timestamps
-- (000054 house rule).

CREATE TABLE skill_cluster_runs (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id           uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id               uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    cluster_id             text NOT NULL,
    session_id             text NOT NULL,
    turn_id                text NOT NULL,
    run_status             text NOT NULL CHECK (run_status IN ('completed', 'failed')),
    origin                 text NOT NULL DEFAULT '',
    qualifying             boolean NOT NULL DEFAULT false,
    tool_calls             integer NOT NULL DEFAULT 0 CHECK (tool_calls >= 0),
    distinct_tools         integer NOT NULL DEFAULT 0 CHECK (distinct_tools >= 0),
    recoveries             integer NOT NULL DEFAULT 0 CHECK (recoveries >= 0),
    error_results          integer NOT NULL DEFAULT 0 CHECK (error_results >= 0),
    total_tool_latency_ms  bigint NOT NULL DEFAULT 0 CHECK (total_tool_latency_ms >= 0),
    window_end_event_id    text NOT NULL DEFAULT '',
    indexed_at             timestamptz NOT NULL,
    CONSTRAINT uq_skill_cluster_runs_run UNIQUE (workspace_id, session_id, turn_id)
);

CREATE INDEX idx_skill_cluster_runs_workspace_cluster ON skill_cluster_runs(workspace_id, cluster_id, qualifying);
