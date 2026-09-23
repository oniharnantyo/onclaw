-- Backfill admin.integrations.write into the built-in master-tenant
-- Superadmin role (add-connection-oauth 3.3 gap fix). The permission guards
-- the instance OAuth app registry (/api/v1/admin/oauth-apps) and joined the
-- catalog after existing role rows were created; built-in role permissions
-- are copied from catalog constants at role creation with no runtime
-- derivation, so Superadmin rows that predate the feature keep the older
-- snapshot and every instance-admin OAuth call 403s (the 000008/000027/000031
-- backfill precedent). No workspace role gains it — the surface is
-- master-tenant only, and migration 000062 deliberately granted only the
-- workspace-level integrations.write.

UPDATE roles
SET permissions = array_append(permissions, 'admin.integrations.write')
WHERE built_in = true AND name = 'Superadmin'
  AND NOT ('admin.integrations.write' = ANY(permissions));
