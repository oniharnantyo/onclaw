-- Backfill tools.write to existing built-in roles that grant it since the
-- workspace tool settings feature (000019). Roles created before it keep the
-- older permission snapshot and cannot configure workspace tools.

-- 1. Add to Superadmin
UPDATE roles
SET permissions = array_append(permissions, 'tools.write')
WHERE built_in = true AND name = 'Superadmin'
  AND NOT ('tools.write' = ANY(permissions));

-- 2. Add to Owner
UPDATE roles
SET permissions = array_append(permissions, 'tools.write')
WHERE built_in = true AND name = 'Owner'
  AND NOT ('tools.write' = ANY(permissions));

-- 3. Add to Admin
UPDATE roles
SET permissions = array_append(permissions, 'tools.write')
WHERE built_in = true AND name = 'Admin'
  AND NOT ('tools.write' = ANY(permissions));
