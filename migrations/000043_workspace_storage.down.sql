-- Reverse 000043 (add-chat-attachments design D16): drop the workspace
-- storage configuration. Workspaces without a row already use the
-- instance-default local storage, so removal restores pre-000043 behavior.

DROP TABLE IF EXISTS workspace_storage;
