-- Backfill channels.read and channels.write to existing built-in roles that
-- grant them since the agent channels feature (000030). Roles created before
-- it keep the older permission snapshot and cannot access workspace channels.

-- 1. Add to Superadmin
UPDATE roles
SET permissions = array_append(permissions, 'channels.read')
WHERE built_in = true AND name = 'Superadmin'
  AND NOT ('channels.read' = ANY(permissions));

UPDATE roles
SET permissions = array_append(permissions, 'channels.write')
WHERE built_in = true AND name = 'Superadmin'
  AND NOT ('channels.write' = ANY(permissions));

-- 2. Add to Owner
UPDATE roles
SET permissions = array_append(permissions, 'channels.read')
WHERE built_in = true AND name = 'Owner'
  AND NOT ('channels.read' = ANY(permissions));

UPDATE roles
SET permissions = array_append(permissions, 'channels.write')
WHERE built_in = true AND name = 'Owner'
  AND NOT ('channels.write' = ANY(permissions));

-- 3. Add to Admin
UPDATE roles
SET permissions = array_append(permissions, 'channels.read')
WHERE built_in = true AND name = 'Admin'
  AND NOT ('channels.read' = ANY(permissions));

UPDATE roles
SET permissions = array_append(permissions, 'channels.write')
WHERE built_in = true AND name = 'Admin'
  AND NOT ('channels.write' = ANY(permissions));
