-- Workspace default model + agent inheritance (refactor-workspace-settings):
-- a workspace may pin a default (provider, model) pair; an agent inherits it
-- by leaving provider_id/model NULL. NULL falls outside the composite FK,
-- which keeps pinning non-null agent pairs to same-workspace providers.
-- The FK lives on workspaces itself, so the referencing pair is
-- (id, default_provider_id) → workspace_providers(workspace_id, id): the
-- default must name a provider row of this workspace.
ALTER TABLE workspaces
    ADD COLUMN default_provider_id uuid,
    ADD COLUMN default_model text,
    ADD CONSTRAINT fk_workspaces_default_provider FOREIGN KEY (id, default_provider_id) REFERENCES workspace_providers(workspace_id, id) ON DELETE RESTRICT;

ALTER TABLE agents
    ALTER COLUMN provider_id DROP NOT NULL,
    ALTER COLUMN model DROP NOT NULL;
