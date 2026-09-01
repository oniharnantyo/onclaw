ALTER TABLE workspaces
    DROP COLUMN IF EXISTS policy,
    DROP COLUMN IF EXISTS language;
