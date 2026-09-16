-- Multi-bot gateways: plural accounts per workspace+platform, bound agent, and group binding owner.
ALTER TABLE workspace_gateways DROP CONSTRAINT IF EXISTS uq_workspace_gateways_workspace_platform;

ALTER TABLE workspace_gateways
    ADD COLUMN identity text NOT NULL DEFAULT '',
    ADD COLUMN agent_id uuid REFERENCES agents(id) ON DELETE CASCADE;

-- Backfill identity and agent_id for existing rows
UPDATE workspace_gateways
SET identity = COALESCE(NULLIF(bot_username, ''), id::text),
    agent_id = COALESCE(default_agent_id, (SELECT id FROM agents WHERE workspace_id = workspace_gateways.workspace_id ORDER BY created_at ASC LIMIT 1));

ALTER TABLE workspace_gateways ALTER COLUMN agent_id SET NOT NULL;
ALTER TABLE workspace_gateways ADD CONSTRAINT uq_workspace_gateways_identity UNIQUE (workspace_id, platform, identity);
ALTER TABLE workspace_gateways DROP COLUMN IF EXISTS default_agent_id;

-- Chat bindings gain owning gateway_id
ALTER TABLE gateway_chat_bindings
    ADD COLUMN gateway_id uuid REFERENCES workspace_gateways(id) ON DELETE CASCADE;

UPDATE gateway_chat_bindings b
SET gateway_id = (SELECT g.id FROM workspace_gateways g WHERE g.workspace_id = b.workspace_id AND g.platform = b.platform LIMIT 1);

-- User links retire default_agent_id
ALTER TABLE gateway_user_links DROP COLUMN IF EXISTS default_agent_id;
