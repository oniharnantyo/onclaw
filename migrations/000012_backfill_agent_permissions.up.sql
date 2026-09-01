-- Backfill agents.read, agents.write, skills.read, skills.write to existing built-in roles

-- 1. Add to Superadmin
UPDATE roles
SET permissions = array_append(array_append(array_append(array_append(permissions, 'agents.read'), 'agents.write'), 'skills.read'), 'skills.write')
WHERE built_in = true AND name = 'Superadmin'
  AND NOT ('agents.read' = ANY(permissions));

-- 2. Add to Owner
UPDATE roles
SET permissions = array_append(array_append(array_append(array_append(permissions, 'agents.read'), 'agents.write'), 'skills.read'), 'skills.write')
WHERE built_in = true AND name = 'Owner'
  AND NOT ('agents.read' = ANY(permissions));

-- 3. Add to Admin
UPDATE roles
SET permissions = array_append(array_append(array_append(array_append(permissions, 'agents.read'), 'agents.write'), 'skills.read'), 'skills.write')
WHERE built_in = true AND name = 'Admin'
  AND NOT ('agents.read' = ANY(permissions));

-- 4. Add agents.read and skills.read to Member
UPDATE roles
SET permissions = array_append(array_append(permissions, 'agents.read'), 'skills.read')
WHERE built_in = true AND name = 'Member'
  AND NOT ('agents.read' = ANY(permissions));
