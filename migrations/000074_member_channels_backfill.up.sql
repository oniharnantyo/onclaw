-- Catalog surgery for existing role rows (fix-role-permission-audit tasks 2.2,
-- design D5): backfill channels.read + channels.write into every built-in
-- Member role row — the decided member grant (list channels, post messages,
-- manage channel membership) — and strip the retired roles.write from ALL role
-- rows (no endpoint ever enforced it; role CRUD does not exist). Built-in rows
-- are identified by built_in = true + the role name (the UNIQUE
-- (workspace_id, name) pair), the same identification the 000008/000027/
-- 000031/000066 backfill precedent uses. Idempotent: the backfill guards with
-- NOT (... = ANY(permissions)) and array_remove is a no-op when absent, so
-- re-running converges to the same sets.

-- 1. Member gains channel participation (if not already present).
UPDATE roles
SET permissions = array_append(permissions, 'channels.read')
WHERE built_in = true AND name = 'Member'
  AND NOT ('channels.read' = ANY(permissions));

UPDATE roles
SET permissions = array_append(permissions, 'channels.write')
WHERE built_in = true AND name = 'Member'
  AND NOT ('channels.write' = ANY(permissions));

-- 2. roles.write leaves every permission set (built-in and custom alike).
UPDATE roles
SET permissions = array_remove(permissions, 'roles.write');
