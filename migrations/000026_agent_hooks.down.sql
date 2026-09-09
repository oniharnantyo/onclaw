-- Reverse 000026 (design.md D16): drop the triggers and audit log first, then
-- the three hook tables in reverse creation order. Execution history is lost
-- on rollback — accepted in the migration plan because the feature is new.

DROP TRIGGER IF EXISTS trg_agent_hooks_detach_executions ON agent_hooks;
DROP TRIGGER IF EXISTS trg_workspace_hooks_detach_executions ON workspace_hooks;
DROP TRIGGER IF EXISTS trg_instance_hooks_detach_executions ON instance_hooks;
DROP FUNCTION IF EXISTS hook_executions_detach_deleted_hook();

DROP TABLE IF EXISTS hook_executions;
DROP TABLE IF EXISTS agent_hooks;
DROP TABLE IF EXISTS workspace_hooks;
DROP TABLE IF EXISTS instance_hooks;
