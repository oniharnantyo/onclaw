# Tasks

## 1. Domain & storage

- [x] 1.1 Domain entity `WorkspaceWebhook` (name, scheme, credential envelope, event source, delivery-id source, filter expression, template, target binding agent+kind+id, enabled) with validation rules and error sentinels mirroring `domain/webhooks.go`; unit tests for validation edges (bad scheme, empty target, template placeholder shape, expression non-blank only when declared) — verify `go test ./internal/domain/`
- [x] 1.2 Migration: `workspace_webhooks` + `webhook_delivery_log` tables (up/down pair, workspace FK, encrypted credential column) — verify `go run . migrate up` on a scratch DB then `migrate status`
- [x] 1.3 Store port `WorkspaceWebhookStore` (full-state update path, delivery-log append + bounded list + prune) + in-memory fake + Postgres adapter — verify fake-based store tests and `go test -tags=integration ./internal/store/postgres/`

## 2. Filter engine

- [x] 2.1 Add `github.com/expr-lang/expr` dependency; build the sandboxed filter package: compile at save (return parser errors verbatim), eval against `{payload map, event}` with function calls denied — verify table tests: valid/invalid expressions, `event == "merge_request" && project.path_with_namespace == "team/api"`, missing-field eval error, denied builtin call
- [x] 2.2 Dry-run evaluation function (sample payload → match | ignored | error verdict, no side effects) — verify unit tests cover all three verdicts

## 3. Auth schemes & ingress

- [x] 3.1 Extend signature verification with the timestamped scheme (`t=<unix>,v1=<hex>` over `{ts}.{body}`, ±300s skew, constant-time) — verify table tests: valid signature, stale timestamp, malformed header, wrong key, malformed hex
- [x] 3.2 Timestamped replay rejection: record signature hash in the delivery dedupe store within the skew window; second identical signature rejected — verify unit + integration tests
- [x] 3.3 Ingest handler + route `POST /api/ingest/hooks/:workspaceSlug/:webhookId`: JSON-only body, 1 MB cap, generic unenumerable rejection (unknown/paused/bad signature identical), delivery-id dedupe, per-endpoint fixed-window rate limit (429 + Retry-After), enqueue — verify httptest suite mirroring `webhooks_http_test.go` incl. the no-enumeration parity assertions

## 4. Pipeline wiring

- [x] 4.1 Standalone resolution + run submitter: endpoint → scheme/secret/filter/template/target; filter non-match → 200 ignored `reason=filter`; attribution carries endpoint id + resolved event on a service-origin run; channel drain reuses the scheduler-shaped poster — verify unit tests for the submitter and the ignored path
- [x] 4.2 Turn rendering: default labeled truncated-JSON turn; declared templates validated against the save-time sample payload and fail closed on missing field — verify renderer tests + one end-to-end queue→submitter test with a bound fake runner
- [x] 4.3 Delivery log rows written for every processed delivery (submitted+run id | filtered | duplicate | rejected | failed) and hourly prune wired — verify store + pipeline tests

## 5. Management service & HTTP API

- [x] 5.1 Service: create (generated reveal-once vs bring-your-own secret), rotate (immediate invalidation), enable/pause, delete, update config (recompiles filter, revalidates template against sample), list (standalone + connection webhooks flagged `source: connection`) — verify service tests with fake store incl. BYO secret never re-revealed and rotate-invalidates-now
- [x] 5.2 Workspace-scoped handlers + routes: CRUD, rotate, enable/pause, dry-run, delivery log; `integrations.write` permission guards; ErrInvalid→400 mapping — verify httptest suite (authz negative cases included)

## 6. Web pane

- [x] 6.1 Register the `webhooks` settings section (`/settings/webhooks`) with the design-contract nav pattern — verify web build + route render test
- [x] 6.2 List view: standalone rows managed inline, connection rows flagged and linking to Integrations; loading/empty/error states; responsive 360→1920 no overflow — verify component tests + visual pass
- [x] 6.3 Create/edit form: labeled fields (scheme, credential, event source, delivery-id source, monospace filter input with inline save-time error, optional template + sample payload, agent + thread/channel target), reveal-once modal, rotate/pause/delete actions — verify component tests incl. single-reveal assertion and validation error rendering
- [x] 6.4 Dry-run panel + per-endpoint delivery log drawer (outcome badges, run link) — verify component tests with mocked API responses

## 7. End-to-end & docs

- [x] 7.1 Smoke section: create endpoint (capture secret), signed timestamped-HMAC delivery → run reaches bound fake target, replay rejected, filtered delivery 200-ignored, rate limit 429 — verify `./scripts/smoke.sh` new section green
- [x] 7.2 Live pass (user-gated): point a real GitLab project webhook (plain token) at a standalone endpoint with `event == "merge_request"` filter and confirm the MR-opened run lands in the bound channel — verify delivered run visible in transcript with webhook attribution
- [x] 7.3 Spec sync: run `openspec validate --change add-workspace-webhooks --strict` clean and sync deltas — verify validator output + specs synced
