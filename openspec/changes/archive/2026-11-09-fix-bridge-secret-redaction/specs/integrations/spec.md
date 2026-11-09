# Spec Delta: fix-bridge-secret-redaction — integrations

## MODIFIED Requirements

### Requirement: Integration credential secrecy

Integration credentials — token credentials and pre-registered client secrets — MUST be returned to no frontend read or listing. The client secret includes both a pre-registered confidential client's secret and a secret issued by dynamic client registration during the authorize flow. Reads and listings SHALL expose only whether a credential is set and at most a short non-secret hint derived from it (the last 4 characters). This MUST hold on every read or listing of an MCP server definition made through the settings service — whatever created the definition (the Integrations pane or the MCP pane) and whichever pane performs the read. An integration credential MUST NOT be stored in the MCP server definition's round-tripping header map; the connection layer SHALL inject it at dial time. Updating a definition with a blank credential — token credential or client secret — SHALL keep the stored value; a non-blank value replaces it. Storing credentials SHALL be unaffected by redaction: the connection layer continues to read them from the stored definition, not from any view.

#### Scenario: Credential never round-trips

- **WHEN** the frontend lists or reads an integration with a stored token
- **THEN** no response contains the credential, only key-set state and the last-4 hint

#### Scenario: Credential is not a raw header

- **WHEN** an integration with a token credential is edited in the raw MCP pane
- **THEN** the credential is not visible among the definition's headers

#### Scenario: Client secret never round-trips

- **WHEN** the frontend reads or lists an OAuth-authenticated server definition with a stored client secret (pre-registered or DCR-issued)
- **THEN** no response contains the secret — the auth settings expose only whether the secret is set and at most a last-4 hint

#### Scenario: Edit preserves the stored client secret

- **WHEN** the frontend saves an edit to an OAuth-authenticated server definition with the client-secret field left blank
- **THEN** the stored client secret is unchanged and the definition still authorizes

#### Scenario: Runtime still authenticates

- **WHEN** an agent connects to the server definition after any of the above reads and edits
- **THEN** the connection uses the stored credential and client secret exactly as before redaction
