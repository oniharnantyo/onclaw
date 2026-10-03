-- Reverse 000072 (add-skill-curation-from-traces 2.3): drop the candidate
-- review queue, its audit trail, and the agents-table curation override
-- pair. Rows are inert once the curation consumer stops writing;
-- materialized skill directories on disk are unaffected (approval survives
-- the store going away — the directory is the runtime artifact).

DROP TABLE IF EXISTS skill_impact_entries;
DROP TABLE IF EXISTS skill_candidates;

ALTER TABLE agents
    DROP COLUMN IF EXISTS skill_curation_provider_id,
    DROP COLUMN IF EXISTS skill_curation_model;
