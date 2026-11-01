-- Schema v19: per-session attachment foldering. The bytes under
-- ~/.buddies/attachments/ gain a session-scoped layout: an upload made with
-- a session open lands at attachments/<session_id>/<key>; an upload made
-- before any session exists (the fresh-chat composer, where the send path
-- creates the session) keeps the flat attachments/<key> layout. The row
-- records which layout the file took, so content reads resolve the path
-- from the registry — the capability key stays the only address any caller
-- sees. Empty session_id covers every pre-convention row (DEFAULT '' back-
-- fills them in place, flat layout) and every session-less upload, keeping
-- the schema's founding principle: an upload exists before (and
-- independently of) the session that eventually references it.

ALTER TABLE attachments ADD COLUMN session_id TEXT NOT NULL DEFAULT '';
