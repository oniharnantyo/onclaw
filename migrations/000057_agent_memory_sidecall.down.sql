ALTER TABLE agents
    DROP COLUMN IF EXISTS memory_sidecall_provider_id,
    DROP COLUMN IF EXISTS memory_sidecall_model;
