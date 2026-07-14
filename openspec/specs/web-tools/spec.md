# web-tools

## Purpose

Provide builtin web tools for search and URL fetching, with swappable and extensible providers, secure credential resolution, fallback error handling, SSRF protection, and structured runtime configuration.
## Requirements
### Requirement: web_search tool with swappable providers

The system SHALL provide a `web_search` builtin tool in a `Web` category that accepts a query
and an optional max-results limit, and returns a list of results each with a title, URL, and
snippet. The search backend SHALL be selectable by name through the Web category config. A
default `duckduckgo` provider that requires no API key SHALL always be available.

#### Scenario: Default search works without configuration

- **WHEN** `web_search` is called and no Web category configuration is present
- **THEN** the tool uses the `duckduckgo` provider and returns results

#### Scenario: A configured provider is used

- **WHEN** the Web category config sets `search_provider` to a registered provider
- **THEN** `web_search` dispatches to that provider

### Requirement: web_fetch tool with SSRF protection

The system SHALL provide a `web_fetch` builtin tool in the `Web` category that accepts an
http(s) URL and optional headers and returns the fetched content. The fetch backend SHALL be
selectable through the Web category config with a default `http` provider. The tool SHALL block
requests to private/internal network ranges, link-local addresses, and cloud-metadata endpoints,
and SHALL re-validate each redirect destination against the same blocklist.

#### Scenario: A public URL is fetched

- **WHEN** `web_fetch` is called with a public http(s) URL
- **THEN** the tool returns the page content

#### Scenario: A private or metadata address is blocked

- **WHEN** `web_fetch` is called with a URL that resolves to a private range, link-local
  address, or cloud-metadata endpoint (including via redirect)
- **THEN** the tool blocks the request and returns an error without fetching

### Requirement: Web providers are extensible via a factory registry

The system SHALL expose a provider factory registry so additional search and fetch backends can
be registered without modifying the `web_search`/`web_fetch` tools. The Web category config
SHALL accept any registered provider name for `search_provider` and `fetch_provider`; a name
that is not registered SHALL be rejected at config load.

#### Scenario: A newly registered provider is selectable

- **WHEN** a search or fetch provider is registered and named in the Web category config
- **THEN** the corresponding tool dispatches to it

#### Scenario: An unknown provider name is rejected

- **WHEN** the Web category config names a provider that is not registered
- **THEN** the configuration is rejected with a clear error

### Requirement: Configured web providers fall back to defaults on failure

The system SHALL fall back to its always-available default provider (`duckduckgo` for search,
`http` for fetch) when the configured search or fetch provider is unavailable, misconfigured
(e.g. missing API key or binary), or returns an error, and SHALL prepend a notice to the result
indicating which provider failed and why. When the default provider is reachable, the system
SHALL return a result to the agent rather than a hard failure.

#### Scenario: A missing API key falls back to the default

- **WHEN** `search_provider` is an API-key provider whose key is absent
- **THEN** `web_search` falls back to `duckduckgo` and the result begins with a fallback notice

#### Scenario: A fetch provider whose binary is missing falls back to http

- **WHEN** `fetch_provider` is `lightpanda` but the binary cannot be found or exits non-zero
- **THEN** `web_fetch` falls back to `http` and the result begins with a fallback notice

### Requirement: Web provider API keys are resolved securely

The system SHALL resolve web-provider API keys by checking an environment variable first
(`ONCLAW_WEB_<PROVIDER>_API_KEY`), then the encrypted SecretStore (decrypted via the KeyManager),
reusing the same resolution mechanism as LLM provider profiles. API keys SHALL NOT be stored in
the Web category config, which is plaintext at rest.

#### Scenario: A key supplied via the environment is used

- **WHEN** the `ONCLAW_WEB_<PROVIDER>_API_KEY` environment variable is set for a configured
  provider
- **THEN** that provider uses the supplied key

#### Scenario: A key is never persisted as plaintext config

- **WHEN** a web-provider API key is configured
- **THEN** it is persisted only in the encrypted SecretStore and never appears in
  `tool_group_config`

### Requirement: Web category exposes structured configuration

The `Web` category SHALL register a JSON-schema configuration surface exposing provider
selection and non-secret tunables (including user agent, request timeout, max response bytes,
`google_cx`, and `lightpanda_bin_path`). The configuration SHALL be persisted in
`tool_group_config` and SHALL take effect on subsequent agent runs without a process restart,
consistent with the tools-management config contract.

#### Scenario: Web configuration applies without restart

- **WHEN** Web category configuration is written while the process is running
- **THEN** subsequent agent runs use the new provider and settings without a restart

### Requirement: Web tool failures are recoverable observations

`web_fetch` and `web_search` SHALL return a fetch or search failure as a tool-result observation
with no fatal error, so the agent turn continues and the model can recover (retry, try another URL,
or report to the user). This applies both when the configured provider fails (already covered by the
fallback notice) and in the terminal case where the default fallback provider also fails. The
observation SHALL name the requested URL or query and the reason. Context cancellation SHALL be
propagated and not converted.

#### Scenario: A fetch failure is an observation

- **WHEN** the agent calls `web_fetch` and every available fetch provider fails (e.g. the URL is
  unreachable or returns an error status)
- **THEN** the tool returns a human-readable observation naming the URL and the failure reason with
  no fatal error, and the agent turn continues

#### Scenario: A search failure is an observation

- **WHEN** the agent calls `web_search` and every available search provider fails (e.g. network
  error or rate limit)
- **THEN** the tool returns a human-readable observation naming the query and the failure reason
  with no fatal error, and the agent turn continues

#### Scenario: A successful fetch still returns content

- **WHEN** the agent calls `web_fetch` and a provider succeeds (directly or via fallback)
- **THEN** the tool returns the fetched content as before, with any fallback notice prepended

#### Scenario: Context cancellation is propagated

- **WHEN** a fetch or search is in flight and the context is cancelled or its deadline expires
- **THEN** the cancellation is returned as a fatal error and is not converted to an observation

