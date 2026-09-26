# Spec Delta

## MODIFIED Requirements

### Requirement: Web search tool providers
The `web.search` tool SHALL resolve queries through a search provider selected per workspace from the workspace's tool settings, falling back to instance configuration when the workspace has none (see the workspace-tools capability, "Search provider configuration"). The provider registry SHALL be extensible: a provider registers the credential kind it requires (`api_key`, `base_url`) and the catalog uses this to render configuration. The result shape SHALL be identical across providers. When neither workspace entries nor a usable instance-env provider exist — or a selected provider is unknown or lacks its credential — the tool SHALL still build: its provider SHALL resolve lazily at first invocation, and every invocation SHALL return an explicit error result naming the missing configuration, surfaced on the tool call in the transcript while the run completes; the unconfigured tool SHALL NOT reach the network and SHALL NOT silently fall back to any credential-free backend.

#### Scenario: Workspace provider selected
- **WHEN** the workspace configures `brave` with an API key
- **THEN** `web.search` in that workspace resolves through Brave in the standard result shape

#### Scenario: Instance fallback preserved
- **WHEN** the workspace has no search settings and the instance env selects `tavily` with a key
- **THEN** `web.search` resolves through Tavily

#### Scenario: Zero-credential default
- **WHEN** neither workspace nor instance configuration exists and a run invokes `web.search`
- **THEN** the invocation returns the explicit "not configured" error result without any network attempt, the tool call shows the error while the run completes, and no credential-free backend is used

#### Scenario: Misconfigured workspace provider degrades to a tool result
- **WHEN** the workspace selects `tavily` without a stored key and a run invokes `web.search`
- **THEN** the invocation returns an error result naming the missing configuration without any network attempt, the tool call shows the error while the run completes, and the tool does not silently fall back
