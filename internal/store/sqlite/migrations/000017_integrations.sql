-- Schema v17: integrations (add-integrations design D1/D4).
-- Add nullable service column to mcp_servers; extend auth JSON with
-- integration_credential and credential_header_style (no table change, JSON
-- columns absorb new fields). Rollback = ALTER TABLE DROP COLUMN service.
ALTER TABLE mcp_servers ADD COLUMN service TEXT DEFAULT '';
