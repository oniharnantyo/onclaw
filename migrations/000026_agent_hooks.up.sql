-- Agent lifecycle hooks (design.md D13/D15/D16): three scope tables plus one
-- audit log. workspace_hooks and agent_hooks are workspace-scoped; agent_hooks
-- keeps workspace_id NOT NULL because it doubles as the AES-GCM AAD scope for
-- the encrypted envelopes inside config (agents.id cascades the delete).
-- matcher is a tagged union ({"type": "tools"|"regex", ...}) stored as one
-- JSONB column that IS the UI state; config holds the handler payload with
-- inline encrypted envelope strings treated as opaque jsonb (web-search
-- stacks precedent; the settings service owns the shape). status/status_error
-- persist the last delivery outcome per hook (MCP pattern, 000024); no
-- background health checks, so status MAY be stale between deliveries.
--
-- instance_hooks is the one DELIBERATELY workspace-unscoped table: instance
-- hooks apply to every workspace, cannot be disabled or weakened by any
-- workspace, and are exposed only through the instance-admin permission
-- surface (design.md D13/D15). This is the single, explicitly commented
-- exception to the workspace-scoping rule.
--
-- hook_executions is the audit log and MUST survive hook deletion. hook_id is
-- polymorphic across the three hook tables (hook_level discriminates which),
-- which a single FOREIGN KEY cannot express, so the required ON DELETE SET
-- NULL semantics are implemented with the AFTER DELETE triggers below;
-- hook_name / hook_level / workspace_id are denormalized so records stay
-- interpretable after the hook row is gone.

CREATE TABLE instance_hooks (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key          text NOT NULL,
    source       text NOT NULL CHECK (source IN ('builtin', 'managed')),
    version      int NOT NULL DEFAULT 1,
    name         text NOT NULL,
    event        text NOT NULL,
    matcher      jsonb NOT NULL,
    handler_type text NOT NULL,
    config       jsonb NOT NULL DEFAULT '{}',
    timeout_ms   int NOT NULL,
    on_failure   text NOT NULL DEFAULT 'allow' CHECK (on_failure IN ('allow', 'block')),
    enabled      boolean NOT NULL DEFAULT true,
    position     int NOT NULL DEFAULT 0,
    status       text NOT NULL DEFAULT 'ok',
    status_error text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_instance_hooks_source_key UNIQUE (source, key)
);

COMMENT ON TABLE instance_hooks IS 'The one deliberately workspace-unscoped table: instance hooks apply to every workspace, are not disableable or weakenable by any workspace, and are exposed only through the instance-admin permission surface.';

CREATE TABLE workspace_hooks (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         text NOT NULL,
    event        text NOT NULL,
    matcher      jsonb NOT NULL,
    handler_type text NOT NULL,
    config       jsonb NOT NULL DEFAULT '{}',
    timeout_ms   int NOT NULL,
    on_failure   text NOT NULL DEFAULT 'allow' CHECK (on_failure IN ('allow', 'block')),
    enabled      boolean NOT NULL DEFAULT true,
    position     int NOT NULL DEFAULT 0,
    status       text NOT NULL DEFAULT 'ok',
    status_error text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_workspace_hooks_workspace_id_name UNIQUE (workspace_id, name)
);

CREATE INDEX idx_workspace_hooks_workspace_id ON workspace_hooks(workspace_id);

CREATE TABLE agent_hooks (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id     uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    name         text NOT NULL,
    event        text NOT NULL,
    matcher      jsonb NOT NULL,
    handler_type text NOT NULL,
    config       jsonb NOT NULL DEFAULT '{}',
    timeout_ms   int NOT NULL,
    on_failure   text NOT NULL DEFAULT 'allow' CHECK (on_failure IN ('allow', 'block')),
    enabled      boolean NOT NULL DEFAULT true,
    position     int NOT NULL DEFAULT 0,
    status       text NOT NULL DEFAULT 'ok',
    status_error text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_agent_hooks_agent_id_name UNIQUE (agent_id, name)
);

CREATE INDEX idx_agent_hooks_agent_id ON agent_hooks(agent_id);
CREATE INDEX idx_agent_hooks_workspace_id ON agent_hooks(workspace_id);

CREATE TABLE hook_executions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    hook_id      uuid,
    hook_name    text NOT NULL,
    hook_level   text NOT NULL,
    workspace_id uuid,
    event        text NOT NULL,
    decision     text NOT NULL,
    duration_ms  int NOT NULL DEFAULT 0,
    exit_code    int,
    http_status  int,
    detail       text NOT NULL DEFAULT '',
    token_count  bigint,
    origin       text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_hook_executions_workspace_created ON hook_executions(workspace_id, created_at DESC);
CREATE INDEX idx_hook_executions_hook_id ON hook_executions(hook_id);

-- ON DELETE SET NULL for the polymorphic hook_id (see header comment):
-- deleting a hook row of any level keeps its execution records with hook_id
-- cleared and the denormalized name intact.
CREATE FUNCTION hook_executions_detach_deleted_hook() RETURNS trigger AS $$
BEGIN
    UPDATE hook_executions SET hook_id = NULL WHERE hook_id = OLD.id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_instance_hooks_detach_executions AFTER DELETE ON instance_hooks
    FOR EACH ROW EXECUTE FUNCTION hook_executions_detach_deleted_hook();

CREATE TRIGGER trg_workspace_hooks_detach_executions AFTER DELETE ON workspace_hooks
    FOR EACH ROW EXECUTE FUNCTION hook_executions_detach_deleted_hook();

CREATE TRIGGER trg_agent_hooks_detach_executions AFTER DELETE ON agent_hooks
    FOR EACH ROW EXECUTE FUNCTION hook_executions_detach_deleted_hook();
