# Tasks

## 1. Store: migration 000023 and webhook rows

- [x] 1.1 Add store types for webhook endpoints, ordered verifier rows, bounded delivery rows, and the listener-address settings row; add `Webhooks()` (or equivalent) accessors to `store.Store` — verify with unit tests for CRUD round-trips and unique-name rejection on `go test ./internal/store/...`
- [x] 1.2 Write the v23 migration (`000023_webhooks.sql`: three tables + the `gateway_settings.webhook_addr` settings column, additive) and bump the schema-version test pins 22 → 23 (6 existing test files / 10 assertions, plus a pin in the new migration test) — verify `go test ./internal/store/...` passes with old-version databases migrating cleanly
- [x] 1.3 Implement per-endpoint delivery retention (oldest-evicted, ~50) on the delivery append path — verify with a test that inserts past the bound and asserts eviction order and bounded count

## 2. Origins and dispatch seams

- [x] 2.1 Add `hooks.OriginWebhook` to the origin vocabulary and confirm the turn-environment/hook paths treat it as an autonomous origin alongside scheduler/telegram — verify `go test ./internal/agents/...` including the origin-matcher tests
- [x] 2.2 Define the webhooks package seams (runtime submit for agent targets; group send for group targets; gateway state read; notifications sink) as narrow interfaces satisfied structurally — verify the interfaces compile against the real implementations with `var _ Seam = (*Real)(nil)` assertions

## 3. Verifier engines and presets

- [x] 3.1 Implement the five verifier engines (path, header, raw-hmac, stdweb, ts-hmac) with constant-time comparison everywhere, including the path token — verify with table tests; every comparison path has a wrong-secret case
- [x] 3.2 Pin per-preset verification vectors from the senders' official docs (GitHub `sha256=<hex>`, Shopify/Linear encodings, Standard Webhooks `{id}.{ts}.{body}` + `whsec_` decode, Stripe `t=,v1=` composition, Slack/Zoom `v0:{ts}:{body}`) — verify `go test ./internal/webhooks/...` passes with the documented vectors
- [x] 3.3 Implement the preset configs (GitLab, GitHub, Stripe, Slack, Zoom, Shopify, Linear, Header) as data that seeds a verifier list, plus the ≥1-verifier save validation — verify with tests that each preset produces the engine config its vector tests already accept
- [x] 3.4 Implement the stdweb replay window and the fail-closed rule (expected signature header present + invalid → 401, no fall-through) — verify with tests covering: valid signature, tampered body, expired timestamp, signature-plus-path-token fall-through refusal

## 4. Receiver and accept pipeline

- [x] 4.1 Implement the receiver's accept pipeline: resolve endpoint → verify credentials → gateway gate → size cap → dedupe → record → dispatch, with the status map (202 accepted, 200 duplicate, 401, 404, 413, 503+Retry-After, 4xx dead target) — verify with httptest table tests over the full pipeline
- [x] 4.2 Attach the receiver to the gateway (start on Running, bounded teardown on Stop/app close) and wire listener-address configuration applying without restart — verify with unit tests over the attach/teardown lifecycle and a config-change test
- [x] 4.3 Implement raw-body read (cap-first), verify-then-parse ordering, and the payload-block builder (fenced JSON / fenced raw text) — verify with tests for JSON bodies, form bodies, oversized bodies

## 5. Dispatch: agent fire-sessions and group ingress

- [x] 5.1 Implement the agent-target adapter mirroring `scheduleRuns`: CreateSession → SetTitle(`<endpoint> — <time>`) → Submit with `OriginWebhook`, payload block appended to the template — verify with a seam-faked unit test asserting session title, prompt shape, and origin
- [x] 5.2 Implement the group-target adapter calling the groups service's send path with the rendered text — verify with a seam-faked unit test asserting one user-authored send with the rendered template
- [x] 5.3 Wire the completion announcer for agent targets: turn-wait, excerpt, Webhooks-category notification routed to the session; group targets get none — verify with seam-faked tests (completed/cancelled/failed outcomes; notification enqueue; group path enqueues nothing)
- [x] 5.4 Handle dead targets on dispatch (agent or group deleted between resolve and submit): record failed, answer 4xx — verify with a test for both target kinds

## 6. Deliveries log and self-test

- [x] 6.1 Record every pipeline outcome into the delivery log with status, outcome, produced session/message id, verifier reference, and sender delivery id — verify pipeline integration tests assert the row contents per status branch
- [x] 6.2 Implement the Test action: compose, sign with the endpoint's credentials, POST through the real listener, log `via: self-test`, surface errors — verify with an httptest-based test proving an accepted self-test and a surfaced failure case

## 7. Wails service and composition

- [x] 7.1 Implement the webhook service (endpoint CRUD with write-only secrets and last-4 hints, enabled toggle, deliveries read, test, listener state) and bind it in both hosts at the composition root — verify `go build ./...` for both tag sets and service unit tests for the secret-presence projection and blank-save-keeps rule
- [x] 7.2 Regenerate bindings (`task common:generate:bindings`) and hand-update `transport.ts` per house convention — verify the frontend build typechecks against the new bindings
- [x] 7.3 Wire shutdown ordering: receiver teardown bounded-joins before the gateway stops (scheduler/telegram close convention) — verify the app close test asserts no goroutine leak complaint and clean ordering

## 8. Frontend: Channels-pane Webhooks section

- [x] 8.1 Build the Webhooks list section (endpoints with enabled state, last-delivery outcome, listener status) in the Channels pane — verify with component tests and both-theme snapshot/sign-off per the ui-conventions testids convention
- [x] 8.2 Build the add/edit wizard: basics (name, agent-or-group target picker, template), preset gallery with per-preset secret field + where-to-find hint, generated URL with copy, secrets shown-once — verify with component tests for each preset card and the round-trip edit flow
- [x] 8.3 Build the verifier editor (add/edit/regenerate/remove) and the delivery log view (status, outcome, via, link to session/group) — verify with component tests including the auth-failed diagnosability display
- [x] 8.4 Wire the Test button and its success/failure surfacing — verify with component tests over the mocked service

## 9. Integration and handover

- [x] 9.1 Full gate: `gofmt -w .`, `go vet ./...`, `go test ./...` green with the new packages included — verify all three commands pass clean
- [ ] 9.2 Live smoke in server mode: create a GitLab-preset endpoint, send a signed curl delivery to the agent target and a mention-template delivery to a group target, confirm session + transcript + delivery log + duplicate retry behavior — verify recorded evidence of each observation
- [x] 9.3 Update `docs/` per the documentation rules (new trigger surface concept) and close with the handover summary — verify docs match shipped behavior
