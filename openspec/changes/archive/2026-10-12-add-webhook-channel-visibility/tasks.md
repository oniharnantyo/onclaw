# Tasks

## 1. Channel discovery (backend) — SUPERSEDED by wave 5 (feed rendering)

- [x] 1.1 Store query: delivery-log rows for all standalone endpoints targeting one channel (join `workspace_webhooks` on `target_kind='channel'` and `target_id`, newest first, bounded 50; projection: webhook id + name, event, outcome, run id, detail, created_at) on the port, fake, and postgres adapters + fake and integration tests — verify `go test ./internal/store/...` and `go test -tags=integration ./internal/store/postgres/ -run WorkspaceWebhook`
- [x] 1.2 Service + route + handler: `GET /channels/:id/webhook-runs` behind `ChannelsRead` returning the bounded listing; 404 for unknown channel; member-negative authz test; unknown-channel ≡ foreign-channel — verify httptest suite
- [x] 1.3 REMOVED (live-pass verdict): the per-channel listing chain (store op, service, route, handler, projection) and its smoke checks — superseded by the room's native feed rendering; verify `go build ./...` and the webhooks suites stay green

## 2. Web surfaces

- [x] 2.1 API client: channel webhook-runs method — verify typecheck
- [x] 2.2 REMOVED (live-pass verdict): the room activity strip (component, mount, API method) — superseded by the room's native feed rendering; verify typecheck and the room suites stay green
- [x] 2.3 Webhooks pane delivery-log run link opens the run transcript (replaces the unfiltered `/runs` link) — verify component test

## 3. End-to-end & docs

- [x] 3.1 Smoke: extend the standalone section — channel-target delivery then `GET /channels/:id/webhook-runs` shows the submitted entry with endpoint name and run id — verify `./scripts/smoke.sh`
- [x] 3.2 Live pass (user-gated): fire a merge_request delivery and see the agent's reply render in the #integrations room; open the run transcript from the run history — verify the transcript shows the labeled webhook turn
- [x] 3.3 Spec sync: `openspec validate --change add-webhook-channel-visibility --strict` clean and deltas synced — verify validator output

## 4. Run telemetry & history (live-pass follow-up)

- [x] 4.1 Domain + migration: run status/duration/tokens on the delivery-log entry (`WorkspaceWebhookRunStatus*` constants), the channel projection, and the workspace run projection; 000076 adds the three columns with defaults (legacy rows read as completed) — verify `go build ./...` and `go test -tags=integration ./internal/store/postgres/ -run WorkspaceWebhook`
- [x] 4.2 Store: `FinishDeliveryRun` (best-effort row finish by run id) and `ListRuns` (workspace-wide, submitted-only, agent-name join) on the port, fake, and postgres adapters — verify `go test ./internal/store/...`
- [x] 4.3 Submitter: unconditional detached drain deriving status/tokens/duration with the scheduler drain's semantics; channel final-text post unchanged; row finished best-effort — verify runtime test asserts the finish call
- [x] 4.4 Service + route + handler: `GET /workspaces/:slug/webhook-runs` behind `integrations.write`, bounded, submitted-only; authz tests (owner 200, member 403, unauthenticated 401) — verify httptest suite
- [x] 4.5 Run history merge: fetch the webhook feed beside the scheduler feed (failure-tolerant), map rows to the run-history shape with `webhook · <endpoint>` trigger chips; transcript opening via the run-card handoff; webhook origin chips on the opened transcript — verify RunsView tests
- [x] 4.6 Room strip collapsed/invocation reshape — SUPERSEDED by 5.x (strip removed)
- [x] 4.7 Smoke: the workspace webhook-runs feed lists the section's submitted delivery with endpoint name and a run status — verify `./scripts/smoke.sh`

## 5. Room feed rendering (live-pass verdict: the channel must just work)

- [x] 5.1 Room feed history: channel rooms load the channel message feed on open (`loadChannelFeed`) and render its entries as conversation messages — agent rows as the invoked agent's message, own user rows as own, others as member messages — merged with the session transcript in order — verify component/route tests
- [x] 5.2 Room feed live: channel rooms subscribe the channel's live event stream while open (`streamChannelEvents` folding through `applyChannelEvent`) so a delivery reply that completes mid-view appears without a reload — verify route test with a folded event
- [x] 5.3 Smoke: drop the superseded per-channel listing checks; the section's channel delivery stays proven via the workspace feed and the delivery log — verify `./scripts/smoke.sh`
