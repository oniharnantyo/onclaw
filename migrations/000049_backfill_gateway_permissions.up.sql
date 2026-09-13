-- Backfill gateways.write to existing built-in roles that grant it since the
-- Telegram gateway feature (000047/000048). Roles created before it keep the
-- older permission snapshot and cannot manage workspace gateways. Pairing is
-- member-level and needs no permission row.

-- 1. Add to Superadmin
UPDATE roles
SET permissions = array_append(permissions, 'gateways.write')
WHERE built_in = true AND name = 'Superadmin'
  AND NOT ('gateways.write' = ANY(permissions));

-- 2. Add to Owner
UPDATE roles
SET permissions = array_append(permissions, 'gateways.write')
WHERE built_in = true AND name = 'Owner'
  AND NOT ('gateways.write' = ANY(permissions));

-- 3. Add to Admin
UPDATE roles
SET permissions = array_append(permissions, 'gateways.write')
WHERE built_in = true AND name = 'Admin'
  AND NOT ('gateways.write' = ANY(permissions));
