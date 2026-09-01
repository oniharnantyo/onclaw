DROP TABLE IF EXISTS agents;

ALTER TABLE workspace_providers
    DROP CONSTRAINT IF EXISTS uq_workspace_providers_workspace_id_id;
