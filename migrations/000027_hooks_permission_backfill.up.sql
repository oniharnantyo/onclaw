-- Backfill hooks.read and hooks.write to existing built-in roles that grant
-- them since the agent hooks feature (000026). Roles created before it keep
-- the older permission snapshot and cannot manage workspace hooks.

-- 1. Add to Superadmin
UPDATE roles
SET permissions = array_append(permissions, 'hooks.read')
WHERE built_in = true AND name = 'Superadmin'
  AND NOT ('hooks.read' = ANY(permissions));

UPDATE roles
SET permissions = array_append(permissions, 'hooks.write')
WHERE built_in = true AND name = 'Superadmin'
  AND NOT ('hooks.write' = ANY(permissions));

-- 2. Add to Owner
UPDATE roles
SET permissions = array_append(permissions, 'hooks.read')
WHERE built_in = true AND name = 'Owner'
  AND NOT ('hooks.read' = ANY(permissions));

UPDATE roles
SET permissions = array_append(permissions, 'hooks.write')
WHERE built_in = true AND name = 'Owner'
  AND NOT ('hooks.write' = ANY(permissions));

-- 3. Add to Admin
UPDATE roles
SET permissions = array_append(permissions, 'hooks.read')
WHERE built_in = true AND name = 'Admin'
  AND NOT ('hooks.read' = ANY(permissions));

UPDATE roles
SET permissions = array_append(permissions, 'hooks.write')
WHERE built_in = true AND name = 'Admin'
  AND NOT ('hooks.write' = ANY(permissions));
