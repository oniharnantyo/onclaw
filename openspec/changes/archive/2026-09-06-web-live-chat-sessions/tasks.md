# Tasks — web-live-chat-sessions

## 1. Backend: session birth

- [x] 1.1 In `resolveSession` (internal/server/handlers/v1.go), when `metadata.onclaw_session` names a session with no persisted events in the key's workspace, return the client-chosen ID as a persistent session instead of not-found; leave `previous_response_id` strictly bind-only (malformed → 400, unresolvable → 404)
- [x] 1.2 Verify the runner's persistent-vs-ephemeral adapter selection keys off the request binding (not an ID-shape heuristic), so client-minted IDs run on the persistent adapter; add a regression test asserting the turn's events persist under the client-chosen ID
- [x] 1.3 Update `internal/server/v1_test.go`: replace the hand-seeded-session setup for metadata binding with a birth turn; add cases — birth on first use, append on second bind, chaining cannot bootstrap (404, no session created), cross-workspace isolation (foreign ID births a local session, foreign events unreadable)
- [x] 1.4 Update `scripts/smoke.sh` with a `/v1` section: mint key → birth turn (stream) → chained turn with `previous_response_id` → assert session events retrievable on the native endpoint (closes the long-standing zero-coverage gap)

## 2. Backend: session key exchange

- [x] 2.1 Add `POST /api/v1/workspaces/:ws/api-keys/exchange` in the api-keys handler group: JWT-authenticated, resolves `:ws` via the standard workspace middleware, requires membership only (no `workspace.write`), reuses existing key-creation machinery (hashed at rest, plaintext returned once, `created_by` = caller)
- [x] 2.2 Handler tests: member exchange succeeds with workspace-scoped key usable on `/v1`; non-member rejected without workspace-existence leak; unauthenticated 401; exchanged key revoked via settings stops authenticating
- [x] 2.3 Route registration in `internal/server/router.go` inside the wsGroup api-keys section

## 3. Web: session identity and binding

- [x] 3.1 Store: add `resp` (response id) capture on agent messages and a `sess` (onclaw_session id) on chat sessions; lazy migration — on a session's next live turn, replace non-`sess_` ids with `sess_<crypto.randomUUID()>` and persist
- [x] 3.2 `runTurn` (web/src/lib/openresponses.ts): capture the minted response id at the first stream event (`response.created`), surface it via an `onResponseId` callback or equivalent so the in-flight turn is cancellable; keep `onDone(responseId)`
- [x] 3.3 `respondFor` (web/src/chat/runtime.tsx): birth turn sends `sessionId` (metadata) when the session has no server binding; subsequent turns send `previousResponseId` from the last recorded `resp`; record the returned response id on the assistant message
- [x] 3.4 `/reset` handler: mint a fresh session id for the thread so the next live turn births a new session; `/reset` keeps clearing local messages only (no server call)
- [x] 3.5 Vitest coverage for the binding state machine: birth → chain → reset-forks; run the touched suites (`pnpm test` on runtime/store/openresponses tests only — ~66 pre-existing localStorage failures at clean HEAD are out of scope)

## 4. Web: cancel, regenerate, approvals

- [x] 4.1 Live cancel: stop control splits the in-flight `resp_<session>_<turn>` id and calls `api.agents` cancel route; running state clears on stream end; partial text and completed tool cards persist in the transcript
- [x] 4.2 Regenerate live: `onReload` re-runs `runTurn` with the original user text (chained like any turn), appending a real variant with the existing branch/`n / total` mechanics; delete the `REPLY_TEMPLATES` branch from the live path
- [x] 4.3 Approval cards: thread `session_id`/`interrupt_id` from `onclaw:approval_required` into the card; Approve/Deny calls the existing `resolveApproval` with real addresses
- [x] 4.4 Approval pickup: while a pending approval card exists for the bound session, poll the session-events endpoint (~2s) and replace the card with the resumed turn's tool output; the same path must render reloaded pending approvals as actionable (per web-app/chat Tool approval prompt)

## 5. Web: key provisioning and mock retirement

- [x] 5.1 Exchange client in `lib/api.ts` + per-workspace key store (`onclaw.api_key.<workspaceId>`); provision on workspace entry when absent; on `/v1` auth failure clear the slot, re-exchange once, then surface the connect state; drop all `onclaw.api_key.*` on logout
- [x] 5.2 Remove the `onclaw.api_key` global-slot reads and the ApiKeyDialog "Use for live chat" localStorage write; settings key creation remains for programmatic keys only
- [x] 5.3 Retire the mock branch in `respondFor` (and channel-mention mock replies): no usable key → explicit connect/retry state in the chat (no canned replies in the live UI); keep canned templates in `lib/constants` for fixtures
- [x] 5.4 Transcript hydration: opening a bound session fetches session events and replaces the local thread (server authoritative), preserving an in-flight optimistic user message; legacy unbound sessions hydrate nothing; add a ChatRoute test for second-browser convergence

## 6. Verification

- [x] 6.1 `go build ./... && go vet ./... && go test ./...` green; integration tests (`-tags=integration`, TEST_DATABASE_URL) green
- [x] 6.2 `./scripts/smoke.sh` fully green including the new `/v1` section
- [x] 6.3 Web build clean; touched suites green (pnpm, not npm)
- [ ] 6.4 Manual pass: birth → memory across turns → reload → chain; stop mid-stream stops the server run; approval card resolves and replaces with output; workspace switch uses the right key; second browser hydrates the transcript
