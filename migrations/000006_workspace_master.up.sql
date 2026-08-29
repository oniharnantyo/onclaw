ALTER TABLE workspaces ADD COLUMN is_master boolean NOT NULL DEFAULT false;
CREATE UNIQUE INDEX idx_workspaces_master ON workspaces (is_master) WHERE is_master = true;
