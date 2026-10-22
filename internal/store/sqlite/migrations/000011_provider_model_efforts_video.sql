-- Schema v11: provider model rows gain an ordered reasoning-effort declaration
-- and a video-input flag (provider-model-capabilities design D2). efforts is ''
-- when undeclared, otherwise a JSON string array in declared order. No backfill:
-- existing rows read as undeclared.
ALTER TABLE provider_models ADD COLUMN efforts TEXT NOT NULL DEFAULT '';
ALTER TABLE provider_models ADD COLUMN video_input INTEGER NOT NULL DEFAULT 0;
