-- Repair 000045 (fix-session-event-ordering design D3): the event append
-- path allocated seq from a history load capped at 100 rows, so sessions
-- past 100 events collapsed to tied seq values (production: 60 rows at
-- seq=100, 16 at seq=101 in one session). Re-sequence every session by
-- (occurred_at, event_id): occurred_at is written once at append time and
-- never rewritten, so it is the trustworthy chronological order, and
-- event_id breaks exact ties deterministically. Idempotent in effect — a
-- healthy log already carries this numbering, so the seq <> new_seq guard
-- rewrites nothing.

WITH renumbered AS (
    SELECT
        session_id,
        event_id,
        ROW_NUMBER() OVER (
            PARTITION BY session_id
            ORDER BY occurred_at ASC, event_id ASC
        ) - 1 AS new_seq
    FROM session_events
)
UPDATE session_events AS se
SET seq = r.new_seq
FROM renumbered AS r
WHERE se.session_id = r.session_id
  AND se.event_id = r.event_id
  AND se.seq <> r.new_seq;
