-- Scheduler run trace ids (integrate-langfuse-tracing D3): the pinned
-- Langfuse trace id of the run's turn, persisted when the turn sampled in
-- for export so the runs surface can deep-link (D6). Empty for runs that
-- predate tracing, sampled out, or never executed a model call — the runs
-- view renders no Langfuse action without it.

ALTER TABLE scheduler_runs
    ADD COLUMN trace_id text NOT NULL DEFAULT '';
