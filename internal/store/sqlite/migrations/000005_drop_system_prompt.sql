-- Schema v5: the system_prompt column is removed (design D2). Every legacy
-- prompt has been carried into its agent's workspace IDENTITY.md by the Go
-- backfill that ran between 000004 and this migration; turns compose the
-- persona from the workspace documents from now on.

ALTER TABLE agents DROP COLUMN system_prompt;
