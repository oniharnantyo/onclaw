-- Schema v21: vault items (add-browser-login-handover design D5). One
-- table: metadata in the clear (name, origin, username, added_by), the
-- secret as a sealed BLOB (AES-GCM ciphertext — the key lives in the OS
-- keyring with a keyfile fallback, never in the database). v1 stores kind
-- 'login' only; the CHECK vocabulary makes future kinds an explicit
-- migration. The (origin, username) pair is the service's natural upsert
-- key (same origin+username ⇒ password update); it is queried, not unique
-- — ids are store-assigned hex per the app-managed identity convention.
-- Timestamps are TEXT (RFC3339Nano) like every other table. No backfill;
-- rollback is DROP TABLE.

CREATE TABLE vault_items (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL CHECK (kind IN ('login')),
    name        TEXT NOT NULL,
    origin      TEXT NOT NULL,
    username    TEXT NOT NULL,
    added_by    TEXT NOT NULL,
    secret_blob BLOB NOT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
