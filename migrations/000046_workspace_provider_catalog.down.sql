-- Reverse 000046 (fix-image-attachment-lane D3): drop the optional
-- community-catalog mapping hint column from provider configs.

ALTER TABLE workspace_providers
    DROP COLUMN IF EXISTS catalog_provider;
