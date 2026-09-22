-- Connection webhook render-failure surface (add-connection-webhooks
-- task 2.4, design.md risks): the last fail-closed render drop is persisted
-- on the connection so the manage surface can surface it — a missing
-- whitelisted field drops the delivery with an explicit error instead of
-- silently emitting an empty turn, and the operator sees why.
--
-- Nullable text: NULL is the clear shape. The ingress writes a JSON envelope
-- {"event": "...", "error": "...", "at": "..."} naming the dropped delivery's
-- event id, the render failure, and the drop timestamp; the next successful
-- delivery render clears it back to NULL. It rides the webhook state's
-- full-write path (UpdateState) — no other writer exists.

ALTER TABLE workspace_connections
    ADD COLUMN webhook_last_error text;
