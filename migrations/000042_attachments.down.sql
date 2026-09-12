-- Reverse 000042 (add-chat-attachments design D3): drop the attachments
-- table. Blob bytes live in the storage drivers under capability keys, not
-- in the database, so nothing else needs restoring.

DROP TABLE IF EXISTS attachments;
