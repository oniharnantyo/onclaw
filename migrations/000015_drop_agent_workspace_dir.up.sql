-- Workspace paths are derived at the point of use from the configured root and
-- the (immutable) slugs; the recorded column could only go stale.
ALTER TABLE agents DROP COLUMN workspace_dir;
