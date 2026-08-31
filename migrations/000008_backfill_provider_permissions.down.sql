-- Remove providers.read and providers.write from built-in roles

UPDATE roles
SET permissions = array_remove(array_remove(permissions, 'providers.read'), 'providers.write')
WHERE built_in = true;
