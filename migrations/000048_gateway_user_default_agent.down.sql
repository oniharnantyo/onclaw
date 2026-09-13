DROP TABLE IF EXISTS gateway_active_sessions;

ALTER TABLE gateway_user_links
    DROP COLUMN IF EXISTS default_agent_id;
