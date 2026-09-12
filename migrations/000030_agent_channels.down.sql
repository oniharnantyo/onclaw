-- Reverse 000030 (design.md D6): drop the channel feed, membership roster,
-- and rooms. Channel data has no pre-000030 representation, so there is
-- nothing to restore.

DROP TABLE IF EXISTS channel_messages;
DROP TABLE IF EXISTS channel_members;
DROP TABLE IF EXISTS channels;
