-- Agent-level memory side-call model (integrate-agent-zero-memory follow-up):
-- an agent may define its own provider+model for the memory pipeline's
-- cheap-model calls (curation gate, gister, intent gate). Both columns empty
-- (the default) means inherit — the workspace memory settings record's
-- side_call_model, falling back to the model the agent itself runs.
ALTER TABLE agents
    ADD COLUMN memory_sidecall_provider_id text NOT NULL DEFAULT '',
    ADD COLUMN memory_sidecall_model text NOT NULL DEFAULT '';
