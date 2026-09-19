-- Rollback assumes no inherit agents exist yet (see the migration plan:
-- revert before the feature is used); SET NOT NULL fails loudly otherwise.
ALTER TABLE workspaces
    DROP CONSTRAINT IF EXISTS fk_workspaces_default_provider,
    DROP COLUMN IF EXISTS default_model,
    DROP COLUMN IF EXISTS default_provider_id;

ALTER TABLE agents
    ALTER COLUMN provider_id SET NOT NULL,
    ALTER COLUMN model SET NOT NULL;
