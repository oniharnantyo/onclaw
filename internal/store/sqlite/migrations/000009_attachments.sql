-- Schema v9: attachment uploads (integrate-agent-chat design D5). One row per
-- uploaded file, keyed by the unguessable capability key the transcript
-- events and the serving path carry; the bytes live beside the database at
-- ~/.buddies/attachments/<key>. Columns are exactly the transcript metadata
-- (key, name, mime, size). No FK to sessions: an upload exists before (and
-- independently of) the message that references it, so an abandoned upload is
-- an inert row plus a file — never a constraint violation. Conventions as in
-- schema v1: store-assigned random hex ids, no timestamps the feature does
-- not need.

CREATE TABLE attachments (
    key  TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    mime TEXT NOT NULL DEFAULT '',
    size INTEGER NOT NULL DEFAULT 0
);
