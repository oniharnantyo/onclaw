# data-store — Delta Spec

## ADDED Requirements

### Requirement: Webhook trigger configuration and deliveries SHALL be persisted

The database SHALL store webhook endpoints as rows carrying a store-assigned id, a user-visible name unique among endpoints, the target (an agent id or a group id, one required), the prompt template, the enabled flag, and creation/update timestamps; each endpoint's credentials as ordered verifier rows carrying the engine kind, the preset label (when configured from a preset), the engine's non-secret config, and the secret material; and each endpoint's delivery log as bounded rows carrying the attempt timestamp, the HTTP status answered, the outcome, the produced session id or transcript message id, the authenticating verifier reference, and the sender delivery id when present. The listener address SHALL be stored with the gateway's trigger configuration. The schema version SHALL increment and migrate at startup per the existing migration rules, and per-endpoint delivery retention SHALL evict oldest-first.

#### Scenario: Fresh database migrates

- **WHEN** the app starts on an existing pre-webhooks database
- **THEN** migration creates the webhook tables, the schema version advances, services start, and pre-existing data is untouched

#### Scenario: Log retention

- **WHEN** an endpoint accrues more deliveries than the retention bound
- **THEN** the oldest rows are evicted and the log stays bounded
