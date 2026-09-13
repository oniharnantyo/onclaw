-- Remove the gateways.write permission from built-in roles
UPDATE roles
SET permissions = array_remove(permissions, 'gateways.write')
WHERE built_in = true;
