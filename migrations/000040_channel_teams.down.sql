-- Reverse 000040 (channel-teams design D7): drop the session linkage on feed
-- messages, the work session table, and the facilitator role. Roster rows
-- revert to the pre-000040 shape (the role column did not exist); channel
-- data has no other pre-000040 representation to restore.

ALTER TABLE channel_messages
    DROP COLUMN IF EXISTS work_session_id,
    DROP COLUMN IF EXISTS is_kickoff;

DROP TABLE IF EXISTS channel_work_sessions;

DROP INDEX IF EXISTS uq_channel_members_channel_facilitator;
ALTER TABLE channel_members
    DROP COLUMN IF EXISTS role;
