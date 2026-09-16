-- Reverse 000052 (add-agent-heartbeat design D1): drop the heartbeat run
-- records and agent heartbeats. Heartbeat rows are inert data once the
-- ticker stops (no rows exist until first enable), so removal restores
-- pre-000052 behavior; hb_ session events written before rollback remain as
-- inert transcript rows.

DROP TABLE IF EXISTS heartbeat_runs;
DROP TABLE IF EXISTS agent_heartbeats;
