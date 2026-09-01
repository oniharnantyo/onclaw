-- Backing UNIQUE constraint for composite foreign key from agents
ALTER TABLE workspace_providers
    ADD CONSTRAINT uq_workspace_providers_workspace_id_id UNIQUE (workspace_id, id);

CREATE TABLE agents (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    slug           text NOT NULL,
    name           text NOT NULL,
    role           text NOT NULL DEFAULT '',
    description    text NOT NULL DEFAULT '',
    brief          text NOT NULL DEFAULT '',
    identity       text NOT NULL DEFAULT '',
    soul           text NOT NULL DEFAULT '',
    provider_id    uuid NOT NULL,
    model          text NOT NULL,
    temperature    numeric(3,2) NOT NULL DEFAULT 1.00 CHECK (temperature >= 0.0 AND temperature <= 2.0),
    max_tokens     integer CHECK (max_tokens > 0),
    effort         text,
    autonomy       text NOT NULL DEFAULT 'approval' CHECK (autonomy IN ('approval', 'suggest', 'full')),
    tools          text[] NOT NULL DEFAULT '{}',
    skills         text[] NOT NULL DEFAULT '{}',
    mcp            text[] NOT NULL DEFAULT '{}',
    avatar         jsonb NOT NULL DEFAULT '{}',
    prompts_status text NOT NULL DEFAULT 'generating' CHECK (prompts_status IN ('generating', 'ready', 'failed')),
    prompts_error  text,
    created_by     uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_by     uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_agents_workspace_id_slug UNIQUE (workspace_id, slug),
    CONSTRAINT fk_agents_workspace_providers FOREIGN KEY (workspace_id, provider_id) REFERENCES workspace_providers(workspace_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_agents_workspace_id ON agents(workspace_id);
