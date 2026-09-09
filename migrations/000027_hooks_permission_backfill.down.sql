-- Remove the hooks permissions from built-in roles

UPDATE roles
SET permissions = array_remove(array_remove(permissions, 'hooks.read'), 'hooks.write')
WHERE built_in = true;
