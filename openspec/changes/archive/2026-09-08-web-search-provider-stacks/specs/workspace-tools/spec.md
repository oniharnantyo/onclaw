## MODIFIED Requirements

### Requirement: Search provider configuration
`web.search` SHALL be configurable per workspace through its tool settings as an ordered list of named provider entries: each entry carries a unique non-empty name, a provider chosen from the provider registry, and the credential that provider requires. The registry SHALL ship `tavily`, `brave`, `exa`, `perplexity`, and `firecrawl` (API key) and `searxng` (base URL); a credential-free scraping provider SHALL NOT exist. The same provider MAY appear in multiple entries with different credentials. A workspace may store any number of entries (bounded by a platform cap well above the request window); list order is priority. Request resolution SHALL build a failover chain from the first three entries in list order (a positional window; the window size is a platform constant): each attempt is bounded by the tool's per-attempt request timeout (`request_timeout_seconds`, positive integer, default 10, maximum 60); any attempt error — non-200 status, network failure, or response decode failure — SHALL advance to the next entry in the window; a successful response with zero results SHALL be returned as a valid answer without advancing; when every entry in the window fails, the request SHALL fail with the last error prefixed by the failing entry's name. Entries below the window SHALL never serve while they sit below it. When a workspace has no entries, the resolver SHALL fall back to the instance configuration (environment) as a single-entry chain; when neither entries nor a usable env provider exist, `web.search` SHALL fail construction with an explicit "not configured" error and SHALL NOT fall back to any credential-free backend. Enabling `web.search` SHALL require at least one fully valid entry (unique non-empty name, known provider, credential present per kind); enabling with none, or saving duplicate entry names, SHALL be rejected with a 422 naming the offending entry. The result shape SHALL be identical across providers.

#### Scenario: Workspace provider wins
- **WHEN** the workspace configures entries ("Tavily 1", "Tavily 2", "Exa 1") while the instance env selects a different provider
- **THEN** `web.search` resolves through the workspace's entry chain in list order

#### Scenario: Multiple keys of one provider
- **WHEN** the workspace configures "Tavily 1" (key A), "Tavily 2" (key B), and "Exa 1" (key C) and Tavily 1 returns a 429
- **THEN** the request advances through Tavily 2 and answers from the first successful entry without surfacing the 429

#### Scenario: Window is positional
- **WHEN** five entries are stored and the first three all fail
- **THEN** the request fails naming the third entry, and the fourth and fifth entries never serve while they sit below the window

#### Scenario: Reorder promotes a standby entry
- **WHEN** the user moves the fifth entry into the top three and saves
- **THEN** subsequent requests try that entry inside the window

#### Scenario: Empty result set is a valid answer
- **WHEN** the first entry responds 200 with zero results
- **THEN** the empty result is returned without advancing to the next entry

#### Scenario: Per-attempt timeout advances the chain
- **WHEN** an entry exceeds `request_timeout_seconds`
- **THEN** the attempt is abandoned and the next entry in the window is tried

#### Scenario: Unconfigured fails fast
- **WHEN** a workspace has no entries and the instance env names no usable provider
- **THEN** `web.search` fails construction with an explicit "not configured" error before any network request

#### Scenario: Instance fallback
- **WHEN** the workspace has no entries and the instance env selects `tavily` with a key
- **THEN** `web.search` resolves through Tavily as a single-entry chain

#### Scenario: Env provider without credential errors
- **WHEN** the env selects a key-requiring provider and its credential is absent
- **THEN** `web.search` fails construction naming the missing credential

#### Scenario: Disabled until configured
- **WHEN** a fresh workspace has no search entries configured
- **THEN** the tools view reports `web.search` as not configured and the enable attempt is rejected until a valid entry is saved

#### Scenario: Enable gating and unique names
- **WHEN** a save enables `web.search` with zero valid entries, or stores two entries named "Tavily 1"
- **THEN** the response is a 422 naming the offending entry and the stored settings are unchanged

#### Scenario: Flat settings migrated
- **WHEN** migration 000023 runs on a row storing the flat `{provider: tavily, api_key}` shape
- **THEN** the row becomes a single auto-named entry carrying the same credential envelope, and a row selecting `duckduckgo` becomes an empty entries list (explicitly unconfigured)
