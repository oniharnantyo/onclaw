## MODIFIED Requirements

### Requirement: API keys are encrypted at rest, never plaintext

The system SHALL encrypt every stored API key with AES-256-GCM directly using the data-encryption key (DEK) before writing it to the `config_secrets` table. Each encrypted value SHALL carry only its own random nonce. The database SHALL NOT contain any plaintext API key.

#### Scenario: Stored secret is not plaintext

- **WHEN** a key is stored for profile `claude` and the raw `config_secrets.value` column is inspected
- **THEN** the column contains only a base64 blob of `nonce ‖ ciphertext ‖ tag`, never the raw key

#### Scenario: Plaintext key never appears in DB dumps

- **WHEN** the database file is grepped or dumped (e.g. `strings onclaw.db`)
- **THEN** no API key plaintext is recoverable
