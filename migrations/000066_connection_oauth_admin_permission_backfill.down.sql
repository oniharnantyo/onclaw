-- Reverse of 000066_connection_oauth_admin_permission_backfill.up.sql.

UPDATE roles
SET permissions = array_remove(permissions, 'admin.integrations.write')
WHERE built_in = true AND name = 'Superadmin';
