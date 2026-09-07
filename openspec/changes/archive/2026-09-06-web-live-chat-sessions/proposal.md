# Proposal: web-live-chat-sessions

## Why

The web chat already runs live turns against `/v1/responses` through the openai SDK, but every turn is ephemeral: no API path anywhere births a persisted session, so agents have no conversation memory, approval cards cannot be resolved, cancel only clears local timers, and transcripts diverge across browsers. Meanwhile the browser holds a manually-pasted, global, full-power tenant API key (or silently falls back to fake mock replies when it is absent).

## What Changes

- **Session birth (auto-create):** an unknown `metadata.onclaw_session` on `POST /v1/responses` births a persistent session scoped to the key's workspace, instead of failing with not-found. Ephemeral semantics are unchanged for requests with no session binding at all. `previous_response_id` keeps its strict semantics (malformed → 400; resolvable to a non-existent session → 404; never births).
- **Web binding style (hybrid, convention-following):** the web chat sends `metadata.onclaw_session` (client-minted `sess_<uuid>`) only on the birth turn of a chat, then chains every subsequent turn via `previous_response_id` — the OpenResponses/OpenAI convention. `/reset` mints a fresh session id.
- **JWT→key exchange endpoint:** a new authenticated `/api/v1` endpoint mints (and returns, once) a workspace-scoped API key for the caller, membership-checked against the named workspace. The web auto-provisions its chat key per workspace on entry — no manual paste.
- **Web chat sessions get stable ids:** local thread sessions migrate (lazily, on next live turn) from client counters (`s1`, `s2`, …) to `sess_<uuid>` values usable as `onclaw_session`.
- **Approval resolution goes live:** the `onclaw:approval_required` payload's `session_id`/`response_id` are threaded into the approval card, so Approve/Deny calls the existing native approvals endpoint with real addresses; persisted sessions make cards reload-safe.
- **Live cancel:** the composer stop button calls the native session-scoped cancel endpoint (in-flight turn id captured from the stream's minted response id) instead of only clearing local timers.
- **Regenerate live:** message reload (`onReload`) re-runs the turn through `runTurn` instead of picking canned `REPLY_TEMPLATES`.
- **Transcript hydration:** opening a chat whose session is bound loads the server transcript from the existing session-events endpoint and renders it.
- **Mock path retired from the live UI:** with no usable key the chat shows a clear connect/error state; canned fake replies no longer run in the app (test fixtures keep them).

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `openresponses`: session binding requirement changes — unknown `metadata.onclaw_session` births a persistent session in the key's workspace (create-on-first-use) instead of failing; the previous_response_id path stays bind-only. Authentication requirement gains the JWT→key exchange: an authenticated native endpoint mints a workspace-scoped key for a member.
- `web-app`: chat turns bind to persistent server sessions (metadata birth + `previous_response_id` chaining), chat keys auto-provision per workspace via the exchange endpoint, approvals resolve against real sessions, stop cancels the live run, regenerate runs live turns, chat opens hydrate the server transcript, and the mock-reply path is removed from the live UI in favor of an explicit connect state.

## Impact

- **Backend:** `internal/server/handlers/v1.go` (`resolveSession` birth path), a new exchange handler + route in `internal/server` (`router.go`, likely `internal/server/handlers/api_keys.go`), `internal/openresponses` request handling unchanged wire-shape (no new fields). Spec deltas in `openspec/specs/openresponses` and `openspec/specs/web-app`.
- **Web:** `web/src/chat/runtime.tsx` (binding, cancel, regenerate), `web/src/lib/openresponses.ts` (session/response plumbing), `web/src/store` (session id migration, per-message response ids, per-workspace key slots), `web/src/components/chat/ChatView.tsx` + `AgentMessage` (approval addressing), hydration fetch in the chat data path, key provisioning at workspace entry (replacing the manual "Use for live chat" flow).
- **Security posture:** the browser's chat credential becomes workspace-scoped and auto-minted rather than a global manually-pasted tenant key; plaintext keys still returned once at creation.
- **Verification:** smoke suite gains `/v1` coverage (session birth → chain → approval route); web judged by touched suites + build (~66 pre-existing vitest localStorage failures at clean HEAD).
