-- Backfill providers.read and providers.write to existing built-in roles

-- 1. Add to Superadmin
UPDATE roles
SET permissions = array_append(array_append(permissions, 'providers.read'), 'providers.write')
WHERE built_in = true AND name = 'Superadmin' 
  AND NOT ('providers.read' = ANY(permissions));

-- 2. Add to Owner
UPDATE roles
SET permissions = array_append(array_append(permissions, 'providers.read'), 'providers.write')
WHERE built_in = true AND name = 'Owner'
  AND NOT ('providers.read' = ANY(permissions));

-- 3. Add to Admin
UPDATE roles
SET permissions = array_append(array_append(permissions, 'providers.read'), 'providers.write')
WHERE built_in = true AND name = 'Admin'
  AND NOT ('providers.read' = ANY(permissions));

-- 4. Add providers.read to Member
UPDATE roles
SET permissions = array_append(permissions, 'providers.read')
WHERE built_in = true AND name = 'Member'
  AND NOT ('providers.read' = ANY(permissions));
