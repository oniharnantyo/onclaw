-- Workspace blob-storage configuration (add-chat-attachments design D16):
-- one row per workspace; absence of a row means the workspace uses the
-- instance-default local storage. secret_access_key stores the sealed
-- envelope at rest (same mechanism as workspace secrets) — the store
-- persists it opaquely; unsealing happens when the storage resolver builds
-- the driver. Attachment rows record their backend at upload time, so
-- flipping drivers never orphans old blobs.

CREATE TABLE workspace_storage (
    workspace_id      uuid PRIMARY KEY REFERENCES workspaces(id),
    driver            text NOT NULL,
    endpoint          text NOT NULL DEFAULT '',
    region            text NOT NULL DEFAULT '',
    bucket            text NOT NULL DEFAULT '',
    access_key_id     text NOT NULL DEFAULT '',
    secret_access_key text NOT NULL DEFAULT '',
    use_path_style    boolean NOT NULL DEFAULT false,
    updated_at        timestamptz NOT NULL DEFAULT now()
);
