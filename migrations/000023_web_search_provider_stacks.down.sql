-- Best-effort rollback of web.search provider stacks (design.md D8): restore
-- the first entry to the pre-000023 flat shape {provider, api_key, base_url,
-- api_key_hint}. Only entry 0 survives — the flat shape cannot represent a
-- stack, so additional entries are dropped. Rows with an empty entries list
-- are NOT fully reversible: their pre-migration content was either the
-- duckduckgo scraping default (removed by this change) or nothing, so they
-- stay explicitly unconfigured as {"entries": []}. credential envelopes are
-- moved verbatim; no re-encryption.

UPDATE workspace_tool_settings
SET config = CASE
    WHEN jsonb_typeof(config->'entries') = 'array'
         AND jsonb_array_length(config->'entries') > 0
    THEN jsonb_strip_nulls(jsonb_build_object(
            'provider', config->'entries'->0->'provider',
            'api_key', config->'entries'->0->'api_key',
            'base_url', config->'entries'->0->'base_url',
            'api_key_hint', config->'entries'->0->'api_key_hint'
         ))
    ELSE config
END
WHERE tool_key = 'web.search'
  AND jsonb_typeof(config) = 'object'
  AND config ? 'entries';
