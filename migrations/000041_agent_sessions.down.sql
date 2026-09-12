-- Reverse 000041 (agent-session-index design D1): drop the agent session
-- index. Rows carry only derived bookkeeping (titles, last-activity times);
-- transcripts live in session_events, so nothing else needs restoring.

DROP TABLE IF EXISTS agent_sessions;
