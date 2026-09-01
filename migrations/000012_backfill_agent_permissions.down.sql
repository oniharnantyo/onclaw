-- Remove agents and skills permissions from built-in roles

UPDATE roles
SET permissions = array_remove(array_remove(array_remove(array_remove(permissions, 'agents.read'), 'agents.write'), 'skills.read'), 'skills.write')
WHERE built_in = true;
