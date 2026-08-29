DROP INDEX IF EXISTS idx_workspaces_master;
ALTER TABLE workspaces DROP COLUMN IF EXISTS is_master;
