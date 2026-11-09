# Spec Delta: fix-bridge-secret-redaction — settings

## MODIFIED Requirements

### Requirement: Tools pane

The settings menu SHALL list the available tools with their descriptions and SHALL allow enabling or disabling each tool. Disabled tools MUST NOT be offered to agents. The pane SHALL also configure the web_search provider chain: providers are chosen from the catalog (brave, exa, perplexity, and firecrawl, each requiring an API key; searxng requiring a base URL), ordered into a failover chain, and persisted for the runtime. A chain read SHALL NOT return an entry's API key — it SHALL expose only whether the key is set and at most a short non-secret hint derived from it (the last 4 characters); the base-URL credential is not a secret and continues to round-trip. Saving a NEW chain provider without its required credential SHALL be rejected. Saving an entry that carries its stable identifier with a blank API key SHALL keep the stored credential; a non-blank key replaces it. A saved entry whose provider changes while its key field is blank SHALL keep the stored credential (the key is re-validated against the new provider's requirement after the merge).

#### Scenario: Toggle a tool

- **WHEN** the user disables a tool that an agent had enabled
- **THEN** the agent's subsequent turns no longer offer that tool

#### Scenario: Configure the search chain

- **WHEN** the user adds brave with an API key and then searxng with a base URL
- **THEN** the chain is saved in that order and web_search tries brave before searxng

#### Scenario: Missing credential is rejected

- **WHEN** the user adds a chain provider that requires an API key and submits without one
- **THEN** the form rejects the submission and explains which credential is missing

#### Scenario: Chain read shows the hint, not the key

- **WHEN** the frontend reads the web_search chain with a stored brave API key
- **THEN** the response contains the key-set state and the last-4 hint, never the key itself

#### Scenario: Edit without retyping the key

- **WHEN** the user reorders an existing chain or renames an entry, leaving the key field blank
- **THEN** the save succeeds and the stored API key is unchanged

#### Scenario: Runtime still resolves the chain

- **WHEN** web_search runs after any redacted read and blank-key save
- **THEN** each entry authenticates with its stored key exactly as before redaction
