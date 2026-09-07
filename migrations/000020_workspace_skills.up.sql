-- Workspace skill registry: one row per workspace-level skill. The multi-file
-- skill body lives on disk under <ONCLAW_DIR>/workspaces/<slug>/skills/<name>/;
-- system-tier skills stay embedded (no rows) and agent-tier skills stay
-- unregistered (disk presence is their state).
CREATE TABLE workspace_skills (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    version      text NOT NULL DEFAULT '0.1.0',
    source       text NOT NULL CHECK (source IN ('authored', 'upload', 'git', 'fork')),
    enabled      boolean NOT NULL DEFAULT true,
    dependencies jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_workspace_skills_workspace_id_name UNIQUE (workspace_id, name)
);

CREATE INDEX idx_workspace_skills_workspace_id ON workspace_skills(workspace_id);

-- BREAKING (design.md D2): the per-agent disabled_skills denylist is removed;
-- skill activation is governed by tier rules (system always, workspace by the
-- registry enabled master switch, agent by directory presence). Data loss is
-- accepted: the denylist was inert in the product.
ALTER TABLE agents
    DROP COLUMN IF EXISTS disabled_skills;
