-- Remove the channels permissions from built-in roles

UPDATE roles
SET permissions = array_remove(array_remove(permissions, 'channels.read'), 'channels.write')
WHERE built_in = true;
