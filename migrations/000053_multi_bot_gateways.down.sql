-- Down migration for multi-bot gateways
ALTER TABLE gateway_user_links
    ADD COLUMN default_agent_id uuid REFERENCES agents(id) ON DELETE SET NULL;

ALTER TABLE gateway_chat_bindings
    DROP COLUMN IF EXISTS gateway_id;

ALTER TABLE workspace_gateways
    ADD COLUMN default_agent_id uuid REFERENCES agents(id) ON DELETE SET NULL;

UPDATE workspace_gateways
SET default_agent_id = agent_id;

ALTER TABLE workspace_gateways
    DROP CONSTRAINT IF EXISTS uq_workspace_gateways_identity,
    DROP COLUMN IF EXISTS identity,
    DROP COLUMN IF EXISTS agent_id;

ALTER TABLE workspace_gateways
    ADD CONSTRAINT uq_workspace_gateways_workspace_platform UNIQUE (workspace_id, platform);
