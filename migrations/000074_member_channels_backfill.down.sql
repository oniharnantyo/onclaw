-- Reverse of 000074_member_channels_backfill.up.sql: take the two channel
-- permissions back out of built-in Member rows and restore roles.write to the
-- built-in roles that carried it in the pre-migration model (Owner and
-- Superadmin; the pre-migration Admin set never held it). Custom-role rows
-- that may have held roles.write before the strip are not re-granted — the UP
-- direction's all-rows strip is not invertible per row, so the rollback
-- restores the documented built-in model only (same scope discipline as the
-- 000066 down precedent). Loss of roles.write was unobservable — no endpoint
-- ever required it — so the rollback is safe either way.

-- 1. Remove the backfilled channel participation from built-in Member rows.
UPDATE roles
SET permissions = array_remove(permissions, 'channels.read')
WHERE built_in = true AND name = 'Member';

UPDATE roles
SET permissions = array_remove(permissions, 'channels.write')
WHERE built_in = true AND name = 'Member';

-- 2. Restore roles.write on the built-ins that held it (idempotently).
UPDATE roles
SET permissions = array_append(permissions, 'roles.write')
WHERE built_in = true AND name IN ('Owner', 'Superadmin')
  AND NOT ('roles.write' = ANY(permissions));
