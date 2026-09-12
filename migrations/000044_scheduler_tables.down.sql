-- Reverse 000044 (integrate-scheduler design D12): drop the scheduler run
-- records and schedulers, and remove the granted permission strings from
-- built-in roles. Scheduler rows are inert data once the ticker stops, so
-- removal restores pre-000044 behavior.

UPDATE roles
SET permissions = array_remove(array_remove(permissions, 'scheduler.read'), 'scheduler.write')
WHERE built_in = true;

DROP TABLE IF EXISTS scheduler_runs;
DROP TABLE IF EXISTS schedulers;
