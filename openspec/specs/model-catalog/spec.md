# model-catalog Specification

## Purpose
Powers the model and effort dropdowns: resolves model lists and per-model effort metadata for a workspace provider config, with the live provider models API as primary source and the community models.dev catalog as cached fallback, plus a credential-preview variant that lets onboarding list models before a config exists.

## Requirements

### Requirement: Models endpoint (stored credential)
`GET /workspaces/:ws/providers/:id/models` (`providers.read`) SHALL resolve models for a workspace provider config: tier 1 — the provider's models API using the decrypted stored key; tier 2 — if tier 1 fails, the models.dev catalog filtered to that provider type's mapping; if both fail, `source: "none"` with an empty list. The response SHALL be `{source: "live" | "catalog" | "none", models: [...]}`, each entry carrying id, display name, and effort values.

#### Scenario: Live models succeed
- **WHEN** the provider's models API answers for the stored key
- **THEN** 200 with `source: "live"` and the provider's model list, each entry enriched with catalog effort values when the catalog knows the model

#### Scenario: Live fails, catalog covers the type
- **WHEN** the provider's models API is unreachable, and the type maps to a models.dev provider (openai, anthropic, google, openrouter)
- **THEN** 200 with `source: "catalog"`, the catalog model list for that mapping

#### Scenario: Both fail
- **WHEN** both sources fail (e.g. a custom gateway unknown to the catalog)
- **THEN** 200 {source: "none", models: []}; the UI degrades to free-text model entry

### Requirement: Credential-preview endpoint
`POST /api/v1/providers/models-preview` (authenticated, no workspace scope) SHALL accept {type, base_url, api_key} and resolve models exactly like the stored-credential endpoint, using the request body's credential for one call without persisting it. Unknown type SHALL be 400.

#### Scenario: Onboarding previews before birth
- **WHEN** the provider step collects type/base_url/key and requests models
- **THEN** models appear without creating a workspace or provider config

#### Scenario: Unknown type rejected
- **WHEN** type "azure-openai" is submitted
- **THEN** 400 invalid_request naming valid types

### Requirement: Effort resolution
Effort values per model SHALL come from the models.dev catalog's `reasoning_options[type=effort].values` for the model; when the catalog lacks the model (custom gateway models, budget-only models like Gemini's), the static per-type floor applies (provider capability interface); when both are empty the efforts list SHALL be empty (UI hides the effort dropdown).

#### Scenario: Catalog efforts win
- **WHEN** a model exists in the catalog with effort values (gpt-5 → minimal|low|medium|high)
- **THEN** the endpoint returns those values

#### Scenario: Floor applies when catalog lacks the model
- **WHEN** a gateway model unknown to the catalog on an `openai-compatible` config
- **THEN** the static floor for `openai-compatible` applies

#### Scenario: No effort values anywhere
- **WHEN** a model has no effort values from catalog or floor
- **THEN** efforts: [] and the UI hides the effort dropdown

### Requirement: Catalog cache
The models.dev `api.json` SHALL be fetched on first use and cached at `ONCLAW_CACHE_DIR` (default `.onclaw/cache`) with a 24h TTL, written atomically; a failed refresh SHALL serve the stale file; concurrent requests SHALL singleflight the fetch so only one HTTP call is in flight.

#### Scenario: TTL respected
- **WHEN** a request arrives within the 24h TTL of the cached file
- **THEN** no network fetch occurs

#### Scenario: Stale served on refresh failure
- **WHEN** the cache is expired and models.dev is unreachable
- **THEN** the stale catalog is served; the endpoint does not fail

### Requirement: Provider-type mapping
Catalog lookups SHALL map catalog types to models.dev provider ids: openai→openai, anthropic→anthropic, gemini→google, openrouter→openrouter; `openai-compatible` and `anthropic-compatible` SHALL NOT match the catalog (custom gateways).

#### Scenario: Gemini maps to google
- **WHEN** the catalog is consulted for a `gemini` config
- **THEN** the lookup uses the `google` models.dev entry

#### Scenario: Compatible types skip the catalog
- **WHEN** the catalog is consulted for an `openai-compatible` config
- **THEN** no catalog entry is consulted for the model list fallback

### Requirement: Context limit exposure
Catalog model entries SHALL carry the models.dev context limit (`limit.context`) for the model when the catalog publishes one; entries without a published limit SHALL omit it. Compatible provider types (`openai-compatible`, `anthropic-compatible`) SHALL NOT resolve catalog context limits, matching the existing provider-type mapping rule. The agent create/update flow SHALL consume this value to auto-fill an omitted agent `context_window` (see the agents capability).

#### Scenario: Catalog model with a published limit
- **WHEN** the catalog is consulted for a model models.dev publishes with `limit.context`
- **THEN** the entry exposes that limit in tokens

#### Scenario: Catalog model without a limit
- **WHEN** the catalog knows a model but publishes no `limit.context` for it
- **THEN** the entry omits the limit and the agent flow leaves `context_window` unset (runtime default applies)

#### Scenario: Compatible types skip the limit
- **WHEN** the catalog is consulted for an `openai-compatible` config
- **THEN** no catalog context limit is resolved for the model

### Requirement: Input-modality resolution
The model catalog SHALL resolve, for a (provider, model) pair, whether the model accepts non-text input kinds — at minimum image and PDF — as a tri-state result: supported, unsupported, or unknown. Resolution SHALL be provider-scoped (the same model id on different gateways may expose different modalities) and SHALL draw on the community catalog's per-model input-modality and attachment-attachment metadata, cached under the existing catalog refresh cycle. An unmapped provider type without a caller-supplied catalog mapping SHALL resolve to unknown.

#### Scenario: Vision-capable gateway entry
- **WHEN** the catalog lists the provider's model with image in its input modalities and image-input resolution is requested
- **THEN** the result is supported

#### Scenario: Same model id, text-only gateway
- **WHEN** a different gateway's catalog entry for the same model id lists text-only input
- **THEN** image-input resolution for that (provider, model) is unsupported — resolution follows the provider mapping, not the model name

#### Scenario: Unmapped provider resolves unknown
- **WHEN** input-modality resolution is requested for an openai-compatible provider with no catalog mapping supplied
- **THEN** the result is unknown rather than supported or unsupported
