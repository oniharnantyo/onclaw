-- Chat attachments (add-chat-attachments design D3): one row per uploaded
-- file. The blob bytes live in a storage-port driver under a random
-- capability key (storage_key — the wire token for serving, not enumerable);
-- the row records which backend holds the bytes so reads survive
-- Local→S3→Local flips (design D16). No FK from messages to attachments:
-- session events carry references in payloads, so nothing dangles when rows
-- are swept later.

CREATE TABLE attachments (
    id           uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    storage_key  text NOT NULL,
    backend      text NOT NULL,
    name         text NOT NULL,
    mime         text NOT NULL,
    size         bigint NOT NULL,
    lane         text NOT NULL,
    created_by   uuid NOT NULL REFERENCES users(id),
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_attachments_workspace ON attachments(workspace_id);
