-- Catalog mapping hint (fix-image-attachment-lane D3): an optional
-- community-catalog provider id on compatible gateway provider configs
-- (openai-compatible / anthropic-compatible). Canonical provider types map on
-- their own and ignore the column; an empty value leaves resolution unmapped
-- (unknown) exactly as before.

ALTER TABLE workspace_providers
    ADD COLUMN catalog_provider text NOT NULL DEFAULT '';
