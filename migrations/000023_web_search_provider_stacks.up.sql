-- web.search provider stacks (design.md D8): re-nest the flat config
-- {provider, api_key, base_url, api_key_hint} into a single-entry provider
-- stack {entries: [{id, name, provider, api_key, base_url, api_key_hint}]}.
-- Credential envelopes are opaque strings (the encryption AAD is the
-- workspace id, unchanged by this migration), so this is a pure JSONB
-- string move — no re-encryption.
--
-- Generated entry ids are 8-char lowercase hex (substr of md5), matching the
-- crypto/rand 8-hex ids the Go settings service assigns, so migrated and
-- freshly-saved entries are indistinguishable.
--
-- Idempotent: rows already carrying an 'entries' key are left untouched.
-- provider = 'duckduckgo', missing, or empty becomes {"entries": []} —
-- explicitly unconfigured (the credential-free scraping default is removed).
-- request_timeout_seconds is intentionally absent; the new code applies its
-- default (10s).

UPDATE workspace_tool_settings
SET config = CASE
    WHEN jsonb_typeof(config) = 'object'
         AND config->>'provider' IS NOT NULL
         AND config->>'provider' <> ''
         AND config->>'provider' <> 'duckduckgo'
    THEN jsonb_build_object(
        'entries', jsonb_build_array(
            -- strip_nulls drops base_url / api_key_hint when absent so the
            -- entry carries only the keys the flat row actually had.
            jsonb_strip_nulls(jsonb_build_object(
                'id', substr(md5(random()::text || clock_timestamp()::text || workspace_id::text), 1, 8),
                'name', CASE config->>'provider'
                            WHEN 'tavily'     THEN 'Tavily'
                            WHEN 'brave'      THEN 'Brave'
                            WHEN 'exa'        THEN 'Exa'
                            WHEN 'perplexity' THEN 'Perplexity'
                            WHEN 'firecrawl'  THEN 'Firecrawl'
                            WHEN 'searxng'    THEN 'SearXNG'
                            -- Unknown provider ids keep the raw id as the
                            -- label; the settings service surfaces them as
                            -- invalid while preserving the stored envelope.
                            ELSE config->>'provider'
                        END || ' 1',
                'provider', config->'provider',
                'api_key', config->'api_key',
                'base_url', config->'base_url',
                'api_key_hint', config->'api_key_hint'
            ))
        )
    )
    ELSE '{"entries": []}'::jsonb
END
WHERE tool_key = 'web.search'
  AND jsonb_typeof(config) = 'object'
  AND NOT (config ? 'entries');
