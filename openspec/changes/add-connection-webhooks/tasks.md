## 1. Domain & Storage

- [ ] 1.1 Webhook declaration fields on recipe descriptors: event catalog, signature scheme, per-event prompt template with whitelisted payload fields, read-flavored default event set
- [ ] 1.2 Connection webhook state: enabled, secret envelope, target binding (agent + thread/channel), selected events; validation
- [ ] 1.3 Migration: webhook columns on `workspace_connections` + delivery-dedupe table (pruned window), with down migration
- [ ] 1.4 Store ports + postgres adapters + fakes for webhook state and dedupe

## 2. Ingress Pipeline

- [ ] 2.1 Ingest handler: resolve workspace + connection, constant-time signature verification per the recipe's scheme, generic 404 for unknown targets before any store access
- [ ] 2.2 Dedupe: delivery-id check after verification, ack-after-persist semantics, replay rejection
- [ ] 2.3 Queue + async processing: bounded queue, per-connection concurrency limit, provider-friendly fast ack
- [ ] 2.4 Template renderer: whitelisted-field interpolation, labeled-data wrapping, missing-field fail-closed error surfaced on the connection
- [ ] 2.5 Routing: render → run machinery for the bound agent in the bound thread/channel; service-authority attribution (authority: service, connection + event) in run metadata and traces
- [ ] 2.6 Secret generation and rotation: workspace-AAD encrypted, display-once, rotation invalidates prior secret

## 3. API & Wiring

- [ ] 3.1 Routes: enable/disable webhooks, rotate secret, update target binding and selected events (guarded by `domain.IntegrationsWrite`); public ingest route (workspace + connection scoped)
- [ ] 3.2 Composition-root wiring: pipeline dependencies injected granularly; runner unaffected

## 4. Web

- [ ] 4.1 ASCII gallery for the connection webhooks section (enable, secret reveal-once, rotate, target picker, event checkboxes) — approval gate before any UI build
- [ ] 4.2 Implement the approved webhooks section in the connection manage surface; design-contract conformance
- [ ] 4.3 Ingest setup helper copy: exact provider configuration steps (URL + secret + events) from the recipe

## 5. Verification

- [ ] 5.1 Unit tests: signature verification (valid/invalid/tampered), dedupe/replay, queue bounds, template render (missing field fail-closed), rotation invalidation
- [ ] 5.2 HTTP tests: guards on webhook management; public ingest rejects unsigned/unknown without enumeration help
- [ ] 5.3 Recipe live-verification (GitHub, GitLab): signature scheme headers, delivery ids, payload fields for the declared events; pin the values
- [ ] 5.4 End-to-end: signed delivery → Atlas turn in bound channel with labeled event data; run metadata shows service authority
- [ ] 5.5 `go build ./...`, `go vet ./...`, `go test ./...` green; web typecheck + tests green
- [ ] 5.6 Manual pass: real GitHub webhook on a test repo — PR opened → agent turn in channel; redelivery deduped; disable stops ingestion
