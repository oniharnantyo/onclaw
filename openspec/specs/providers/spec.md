# Providers Spec

## Purpose

TBD

## Requirements

### Requirement: Provider profiles are persisted in a sqlite store

The system SHALL persist provider profiles (name, provider_type, api_base, model) in a
sqlite database, queryable and listable, keyed by a unique profile name. Profile
fields other than the API key SHALL be stored as plaintext.

#### Scenario: Add and retrieve a provider profile

- **WHEN** a profile `claude` is added with provider_type `anthropic` and model `claude-sonnet-4-6`
- **THEN** `provider list` includes `claude`, and `provider` lookup by name returns those fields

#### Scenario: Keyless provider has no secret

- **WHEN** a profile `local` of provider_type `ollama` is added with no API key
- **THEN** the profile is stored and usable, and no `config_secrets` row exists for `local`

#### Scenario: Profile names are unique

- **WHEN** a profile is added with a name that already exists
- **THEN** the system rejects the duplicate and leaves the existing profile unchanged

### Requirement: API keys are encrypted at rest, never plaintext

The system SHALL encrypt every stored API key with AES-256-GCM directly using the data-encryption key (DEK) before writing it to the `config_secrets` table. Each encrypted value SHALL carry only its own random nonce. The database SHALL NOT contain any plaintext API key.

#### Scenario: Stored secret is not plaintext

- **WHEN** a key is stored for profile `claude` and the raw `config_secrets.value` column is inspected
- **THEN** the column contains only a base64 blob of `nonce ‖ ciphertext ‖ tag`, never the raw key

#### Scenario: Plaintext key never appears in DB dumps

- **WHEN** the database file is grepped or dumped (e.g. `strings onclaw.db`)
- **THEN** no API key plaintext is recoverable

### Requirement: A single data-encryption key wraps under a keyfile or a passphrase

The system SHALL encrypt secrets with a random data-encryption key (DEK). The DEK
SHALL be stored wrapped by a key-encryption key derived from either a `master.key`
file (mode 0600) or, when passphrase mode is enabled, Argon2id(passphrase, salt).
Switching key mode SHALL only re-wrap the DEK and SHALL NOT re-encrypt secrets.

#### Scenario: First run initializes a keyfile in unattended mode

- **WHEN** onclaw starts for the first time with no existing DB or key material
- **THEN** a `master.key` is generated at mode 0600, a DEK is generated and wrapped under it, and no passphrase is required

#### Scenario: Switching to passphrase mode does not re-encrypt secrets

- **WHEN** `onclaw unlock` sets a passphrase while keys already exist
- **THEN** the DEK is re-wrapped under the passphrase-derived KEK and existing secrets remain valid and decryptable

### Requirement: Secret resolution precedence is env over database

The system SHALL resolve a provider's API key from `ONCLAW_PROVIDER_<NAME>_API_KEY`
first, then from the encrypted `config_secrets` table. An env-provided key SHALL never
be persisted. When no key is found, the outcome depends on the provider kind: a
key-requiring provider SHALL surface a guided error, while a keyless provider (e.g.
`ollama`) SHALL proceed with an empty key and remain usable. Which kinds are keyless
SHALL be applied consistently by provider setup, secret resolution, and the model-build
path, so a keyless provider never errors solely for lacking a key.

#### Scenario: Env override wins

- **WHEN** `ONCLAW_PROVIDER_CLAUDE_API_KEY` is set and a different key is stored in the DB for `claude`
- **THEN** the env value is used at runtime and the DB value is unchanged

#### Scenario: Missing key on a key-requiring provider produces a guided error

- **WHEN** no env var and no stored secret exist for a selected provider whose kind requires a key (e.g. `openai`, `anthropic`)
- **THEN** the system returns an error instructing the user to run `onclaw provider login <name>`

#### Scenario: Missing key on a keyless provider does not block the build

- **WHEN** an `ollama` profile has no env var and no stored secret and is built for a session
- **THEN** the build proceeds with an empty API key and resolves to a usable ChatModel, with no guided error

### Requirement: Secrets are never disclosed in config output or logs

The system SHALL NOT place API keys on any `Config` value that is printed.
`onclaw config show` SHALL render every API key as `***`. The system SHALL NOT
write resolved secret values to logs or to the session transcript.

#### Scenario: config show redacts keys

- **WHEN** `onclaw config show` is run after a provider with a key is configured
- **THEN** the output shows `api_key: ***` and no plaintext key appears

#### Scenario: Secrets are absent from logs and transcript

- **WHEN** a turn runs that decrypts and uses a key
- **THEN** no log line and no line in the session `.jsonl` contains the plaintext key

### Requirement: Provider changes apply without restarting a running session

The system SHALL hot-reload provider profiles and secrets into a running session
on the next agent turn after they change, without process restart. Changes SHALL
be detected via filesystem watching of the database files, with a signal fallback.

#### Scenario: Adding a provider is picked up live

- **WHEN** a running session is active and `onclaw provider login` adds a new provider
- **THEN** the new provider is selectable on the next turn without restarting the process

#### Scenario: Reload applies on the next turn, not mid-flight

- **WHEN** a provider change occurs while a request is in flight
- **THEN** the in-flight request completes with the prior provider and the next turn uses the new one

### Requirement: The database file is access-restricted

The system SHALL create the sqlite database file with filesystem mode 0600
(owner read/write only) and SHALL refuse to operate if it cannot secure the file.

#### Scenario: DB file is owner-only

- **WHEN** the database is created or opened
- **THEN** its filesystem permissions are 0600

### Requirement: Storage is interface-backed and replaceable

The system SHALL define `ProfileStore`, `SecretStore` (opaque encrypted blobs),
and `KVStore` interfaces, with the sqlite database as one implementation. The
system SHALL NOT couple storage callers to the sqlite implementation. Secrets
SHALL be encrypted/decrypted outside the store (by a key manager), so the store
holds opaque blobs only.

#### Scenario: A non-sqlite backend can be substituted

- **WHEN** an alternate implementation of the store interfaces is provided (e.g. an in-memory fake)
- **THEN** the provider service and CLI operate against it with no changes to their code

#### Scenario: The secret store holds opaque blobs

- **WHEN** a secret is written through the service
- **THEN** the store receives and returns an encrypted blob and performs no cryptographic operation itself

### Requirement: Provider kinds are pluggable via an adapter registry

The system SHALL resolve a provider's runtime model through an adapter registry
keyed by the profile `kind`, not a hard-coded switch. Adding a new provider kind
SHALL require only registering an adapter factory.

#### Scenario: A new provider kind is added by registration

- **WHEN** an adapter factory is registered for a new kind and a profile of that kind is built
- **THEN** the registry dispatches to the registered adapter and returns a model, with no edits to the build path

### Requirement: Provider profiles carry a settings document and an enabled flag

Each profile SHALL have a `settings` JSON field (default `{}`) for provider-specific extras
and an `enabled` flag (default true). The `settings` document SHALL recognize a
`context_window` integer expressing the model's context-window size in tokens, settable via
`onclaw provider add --context-window`. A disabled profile SHALL NOT be selectable as a
provider until re-enabled.

#### Scenario: Provider-specific settings are stored and surfaced

- **WHEN** a profile is created with extra settings (e.g. custom headers)
- **THEN** those settings persist in the `settings` field and are available when the adapter is built

#### Scenario: A provider's context window is stored in settings

- **WHEN** a profile is created with `--context-window 128000`
- **THEN** the `settings` document contains `context_window: 128000` and no database migration is performed

#### Scenario: A disabled profile is not selectable

- **WHEN** a profile is disabled and the user runs `onclaw run` without naming another provider
- **THEN** the disabled profile is skipped and not used

### Requirement: Secrets are stored in a generic encrypted key-value store

The system SHALL store secrets in a key-value table keyed by a namespaced key
(e.g. `provider:<name>`), encrypted at rest, usable by providers and by later
subsystems (plugins, MCP) under the same interface. A keyless provider SHALL have
no secret row.

#### Scenario: Provider key is namespaced under the secret store

- **WHEN** a key is saved for provider `claude`
- **THEN** it is stored under the `provider:claude` key and retrievable by that key

#### Scenario: The same store can hold non-provider secrets

- **WHEN** a future subsystem writes a secret under a different key namespace (e.g. `plugin:weather:token`)
- **THEN** it is stored and retrieved through the same secret-store interface without schema change

### Requirement: Conversation summarization triggers at 80% of the effective context window

The system SHALL trigger conversation summarization when the running token count reaches
`int(0.8 * effectiveContextWindow)`. The effective context window SHALL be the resolved
provider profile's `context_window` setting when it is greater than 0, else the global
`max_context_tokens` when it is greater than 0, else 64000. The system SHALL NOT use a
hardcoded token threshold for summarization.

#### Scenario: A provider window drives the trigger

- **WHEN** the resolved provider profile sets `context_window` to 128000
- **THEN** summarization is configured to trigger at 102400 tokens (80% of 128000)

#### Scenario: An unset window falls back to the default

- **WHEN** the resolved provider profile does not set `context_window` and the global default applies
- **THEN** the effective window is 64000 and summarization is configured to trigger at 51200 tokens

### Requirement: Provider list displays the configured context window

`onclaw provider list` SHALL include each profile's `context_window` when it is set, and SHALL
indicate the default/fallback clearly when it is unset.

#### Scenario: A set context window is shown

- **WHEN** a profile was created with `--context-window 128000` and `onclaw provider list` is run
- **THEN** the output includes `context_window: 128000` for that profile

#### Scenario: An unset context window is shown as the default

- **WHEN** a profile has no `context_window` and `onclaw provider list` is run
- **THEN** the output indicates the context window is unset/default (e.g. `context_window: (default)`)

### Requirement: OpenAI-compatible kinds build a real streaming ChatModel

The system SHALL resolve profiles of kind `openai`, `openai-compatible`, and `ollama` to a
real `model.ChatModel` that performs live, streaming inference against the profile's
`api_base`/model, constructed via eino-ext or an equivalent hand-rolled client. These kinds
SHALL NOT resolve to a no-op stub. A disabled profile SHALL NOT be built.

#### Scenario: An OpenAI-compatible profile produces live output

- **WHEN** a profile of kind `openai-compatible` with a valid base URL, model, and key is built and used for a turn
- **THEN** the model streams real completions over the network and is not a no-op stub

#### Scenario: Ollama uses the OpenAI-compatible path with no key

- **WHEN** a keyless `ollama` profile with a base URL is built
- **THEN** it resolves to a real ChatModel via the OpenAI-compatible path without requiring an API key

#### Scenario: A disabled profile is not built

- **WHEN** the selected profile has `enabled` set false
- **THEN** building it fails with a clear error rather than producing a model

### Requirement: A normalized reasoning-effort value is mapped to the provider's native field

The OpenAI-compatible adapter SHALL read a normalized `reasoning_effort` (`low`, `medium`,
`high`, or unset) from the effective profile's `settings` and SHALL map it to the provider's
native request field. A value the provider does not support SHALL be ignored (no effort sent)
rather than causing an error. This is how an agent's or `--reasoning` selection takes effect
(cross-ref `agent-profiles`).

#### Scenario: A high effort is sent to the provider

- **WHEN** the effective profile carries `reasoning_effort: high` and the provider supports it
- **THEN** the outbound request carries the provider's native high-effort field

#### Scenario: An unsupported effort is dropped

- **WHEN** the effective profile carries `reasoning_effort: high` but the provider kind does not support it
- **THEN** the request is sent with no effort field and does not error
