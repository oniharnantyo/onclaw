-- Remove the tools.write permission from built-in roles

UPDATE roles
SET permissions = array_remove(permissions, 'tools.write')
WHERE built_in = true;
