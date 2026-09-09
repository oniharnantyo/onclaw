ALTER TABLE instance_hooks DROP COLUMN IF EXISTS if_rule;
ALTER TABLE workspace_hooks DROP COLUMN IF EXISTS if_rule;
ALTER TABLE agent_hooks DROP COLUMN IF EXISTS if_rule;
