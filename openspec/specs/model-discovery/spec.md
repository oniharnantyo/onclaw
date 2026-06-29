# model-discovery

## Purpose

Enable agents to discover and store model metadata (context window, thinking capability, input modalities) by enumerating available models from provider APIs and enriching them with a cached catalog.

## Requirements

### Requirement: Available models are enumerated from the provider's own API

The system SHALL enumerate the models available to a provider profile by calling the
provider's model-listing API: `GET {base_url}/v1/models` for `openai`, `anthropic`, and
`openai-compatible` kinds, and `GET {base_url}/api/tags` for `ollama`. Enumeration SHALL
be authenticated with the profile's resolved API key where the kind requires one; a
keyless provider SHALL enumerate without authentication.

#### Scenario: OpenAI-compatible models are listed

- **WHEN** discovery enumerates models for an `openai-compatible` profile at base_url `https://api.example.com/v1` with a valid key
- **THEN** the provider's `GET /v1/models` is called with a Bearer token and the returned model ids are presented for selection

#### Scenario: Ollama models are listed via tags

- **WHEN** discovery enumerates models for an `ollama` profile at base_url `http://localhost:11434`
- **THEN** `GET /api/tags` is called (unauthenticated) and the installed model names are presented for selection

### Requirement: Model metadata is resolved via a layered source with sane defaults

The system SHALL resolve each model's metadata (context window, thinking flag, input
modalities) by trying, in order: (1) a provider-native source — Ollama `POST /api/show`
returning `model_info.*.context_length`, or a `context_length` field present in an
OpenAI-compatible `/v1/models` entry; (2) a cached `models.dev` catalog keyed by provider
id then model id, with a global search across all providers on a provider-id miss; (3)
built-in defaults (context window 0, thinking false, modalities `text`). A failure or
absence at any layer SHALL fall through to the next rather than aborting.

#### Scenario: Ollama context length is read from the show endpoint

- **WHEN** resolving metadata for an ollama model whose `/api/show` returns `model_info.llama.context_length` of 8192
- **THEN** the resolved context window is 8192

#### Scenario: models.dev fills in an OpenAI model

- **WHEN** resolving metadata for `gpt-4o` and the provider-native source provides none
- **THEN** the cached `models.dev` entry `openai.models["gpt-4o"]` supplies context_window 128000, thinking false, and input modalities

#### Scenario: Unknown model falls back to defaults

- **WHEN** resolving metadata for a model id absent from both the provider-native source and the cache
- **THEN** the resolved metadata is context_window 0, thinking false, modalities `["text"]`

#### Scenario: A mismatched provider id triggers a global search

- **WHEN** resolving metadata for a model whose provider type does not map to a `models.dev` provider id (e.g. `openai-compatible`)
- **THEN** the resolver searches every `models.dev` provider for the model id and uses the first match

### Requirement: The models.dev catalog is parsed from its documented JSON shape

The system SHALL parse the `models.dev` catalog as a JSON object keyed directly by
provider id (there is no wrapper field), where each provider object contains a `models`
map keyed by model id. For each model, the system SHALL read the context window from
`limit.context`, the thinking/reasoning flag from `reasoning`, and the input modalities
from `modalities.input`. A root-shape or field-path mismatch SHALL NOT silently leave the
catalog empty; parsing SHALL be verified against a real-shaped payload.

#### Scenario: A real-shaped models.dev payload populates the catalog

- **WHEN** the catalog parser is given a payload whose root is `{"openai":{"models":{"gpt-4o":{"reasoning":false,"limit":{"context":128000},"modalities":{"input":["text","image"]}}}}}`
- **THEN** the parsed catalog yields provider `openai`, model `gpt-4o` with context window 128000, thinking false, and input modalities `["text","image"]`

#### Scenario: A model resolves from the catalog when /v1/models lacks the data

- **WHEN** a selected model's `/v1/models` entry provides no context window and the model exists in the parsed `models.dev` catalog
- **THEN** the resolver supplies the catalog's `limit.context`, `reasoning`, and `modalities.input` for that model rather than defaulting to context window 0

### Requirement: The models.dev catalog is cached with a checksum-guarded refresh

The system SHALL cache the `models.dev` catalog at `~/.onclaw/cache/api.json`. A cached
file younger than 12 hours SHALL be used as-is. On expiry, the system SHALL fetch the
catalog, compute its sha256, and only overwrite the cached file — atomically, via a temp
file and rename — when the checksum differs from the stored one; an unchanged checksum
SHALL merely refresh the file's modification time. If the refresh fetch fails, the system
SHALL reuse the existing cached file when present, and only error when no cache exists.

#### Scenario: A fresh cache within TTL is reused

- **WHEN** discovery runs and `api.json` was written 2 hours ago
- **THEN** no network fetch occurs and the cached file is used

#### Scenario: An unchanged catalog is not rewritten

- **WHEN** the cache is older than 12 hours and the fetched catalog's checksum equals the stored checksum
- **THEN** the cached file is not overwritten and only its modification time is refreshed

#### Scenario: A changed catalog is written atomically

- **WHEN** the fetched catalog's checksum differs from the stored checksum
- **THEN** the cached file is replaced atomically and the new checksum is stored

#### Scenario: Network failure falls back to the stale cache

- **WHEN** a refresh fetch fails and a cached `api.json` exists
- **THEN** the existing cache is used and no error is raised
