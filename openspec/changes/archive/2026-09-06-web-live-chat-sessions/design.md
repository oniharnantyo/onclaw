# Design — web-live-chat-sessions

## Context

The `/v1` OpenResponses facade and the web's openai-SDK `runTurn` are live (see proposal Why). Three facts constrain the design:

- `resolveSession` (internal/server/handlers/v1.go) rejects any binding to a session with no persisted events, and the only unbound path is the no-store `EphemeralSessionAdapter` — so no API path births a persistent session.
- Sessions have no registry table: existence is proven by session-event rows, so birth requires no schema change — the first persisted append *is* the birth.
- The approval interrupt ends the `/v1` stream (response left `incomplete`), and `ResolveApproval` returns `{resumed: true}` once the resume is initiated; the continued turn's output lands in session history, not back on the caller's socket.

## Goals / Non-Goals

**Goals:** web chats with server-side memory under the standard `previous_response_id` convention; real approval resolution, cancel, and regenerate; per-workspace auto-provisioned chat keys; transcript convergence across browsers.

**Non-Goals:** a sessions *listing* API; history truncation/forking endpoints (edit/regenerate divergence is accepted, see Risks); permission-scoped `/v1` keys (keys remain tenant-scoped); removing the ephemeral path or its adapter; new `/v1` request fields.

## Decisions

**D1 — Birth attaches to the metadata path, not a new field.** An unknown `metadata.onclaw_session` births the session (create-on-first-use, workspace-scoped). Alternatives rejected: a `store` flag (user declined; extra wire concept), persist-by-default (flips the ephemeral posture of the synced spec and strands `EphemeralSessionAdapter` as dead code), and a native JWT create endpoint (two surfaces to keep in sync for what one relaxation achieves). Implementation: in `resolveSession`, when metadata is present and `sessionExists` is false, return the ID as-is instead of not-found. The runner already persists when `SessionID` is not ephemeral-shaped — verify the ephemeral detection keys off the binding decision, not a `sess_` prefix, so client-chosen IDs flow to the persistent adapter. `previous_response_id` stays strictly bind-only (malformed → 400; unresolvable → 404).

**D2 — Hybrid binding: metadata once, then chain.** The web mints `sess_<uuid>` (crypto.randomUUID) per chat session and sends it as metadata on the birth turn only; every later turn chains via `previous_response_id`, which `runTurn` already supports and `onDone(responseId)` already delivers. The web records the response ID on the assistant message it produced (new `resp` field). Rationale over metadata-every-turn: the user wants the OpenResponses convention followed on the wire; over persist-by-default: the web keeps session identity at birth, which it needs for native addressing (cancel, hydration, approvals) and for `/reset` (mint a fresh ID — the old session is simply abandoned).

**D3 — Exchange endpoint: `POST /api/v1/workspaces/:ws/api-keys/exchange`.** JWT-authenticated; resolves `:ws` like every tenant route; membership (any role) suffices — no `workspace.write`. It reuses the existing key-creation machinery verbatim (hashed at rest, plaintext returned once, `created_by` = the exchanging user). Rationale: the JWT carries no workspace claim (see openresponses-facade exploration), so the workspace comes from the path; and chat is a Member-level activity, so requiring Owner/Admin would lock members out of live chat. The endpoint stays inside the existing `api-keys` route group and handler file.

**D4 — Key storage and lifecycle.** Per-workspace slot `onclaw.api_key.<workspaceId>` in localStorage; exchanged once per workspace per browser, re-exchanged only when a `/v1` call fails auth (clear slot → re-run exchange once → surface the connect state if it fails again). Logout removes every `onclaw.api_key.*`. The manual ApiKeyDialog flow remains for programmatic keys but loses its "Use for live chat" localStorage write (that slot scheme is superseded).

**D5 — Turn identity for cancel.** `runTurn` captures the minted response ID at the first stream event (`response.created`), not only at `response.completed` — the turn must be cancellable while in flight. The web splits the ID client-side (`resp_<session>_<turn>`, split at the last underscore — the codec is a published OnClaw convention) and calls the existing `POST /workspaces/:ws/agents/:agent/sessions/:session/runs/:turn/cancel`. No route changes.

**D6 — Approval pickup via event polling, not a second stream.** After `ResolveApproval` (or on render of a pending card), the web polls the existing session-events endpoint (~2s while any card is pending) and replaces the card when the resumed turn's tool output appears. Rationale: the `/v1` stream is over by the time the card exists, and the native surface is where approval state lives; one polling path uniformly serves live interruptions and approvals pending from a previous page load. This retires the "decorative" approval path — the card stores `session_id`/`interrupt_id` from the `onclaw:approval_required` payload.

**D7 — Hydration is replacement, not merge.** Each chat session maps 1:1 to an `onclaw_session` ID. Opening a bound session fetches the translated transcript events and *replaces* the local thread (server is authoritative), preserving only an in-flight optimistic user message. Legacy sessions (counter IDs like `s1`) hydrate nothing and lazily mint on their next live turn. Deduplication is therefore structural (one authority), not heuristic.

**D8 — Mock retirement is a runtime cut, not a fixture purge.** `respondFor` loses its mock branch and channel-mention paths route through `runTurn` (the mentioned agent's slug is the model). Canned reply templates stay in `lib/constants` for test fixtures. Regenerate (`onReload`) re-runs via `runTurn` using the original user text; the new variant chains like any turn.

## Risks / Trade-offs

- [Typo'd or colliding `onclaw_session` silently forks a new session] → IDs are minted, never typed; workspace scoping bounds the blast radius (cross-workspace isolation scenario in the delta spec).
- [Local tree operations diverge from linear server history: edit-and-resubmit and regenerate append abandoned variants to the session's full replay, and later turns replay them as context] → Accepted for this change; `/reset` is the supported "clean slate" (fresh session). A future fork/truncate capability can reconcile if variant noise proves harmful.
- [Member-minted exchange keys carry full tenant `/v1` power] → Matches the existing "key = tenant" model for all keys; revocation is immediate and visible in settings. If per-identity scoping is wanted later, it is a key-model change, not an exchange-endpoint change.
- [Browser storage cleared mid-chat loses response IDs] → Next turn births a fresh session; context resets. Same failure mode as losing any local-first state.
- [Event polling adds load per pending approval] → Bounded: polls only while a card is pending, single session scope, stops on resolution.

## Migration Plan

1. Land backend (birth relaxation + exchange endpoint) — additive; existing clients unaffected; no DB migration.
2. Land web (binding, cancel, regenerate, hydration, provisioning, mock cut) — behind nothing; the web tolerates an old backend by showing the connect state (exchange 404s → provisioning failure path).
3. Rollback: revert web first (mock path returns with the revert); backend revert is safe since birth only adds rows the old code would have rejected.

## Open Questions

None blocking. The regenerate/edit history-divergence acceptance (D8/Risks) should be revisited if variant noise degrades reply quality in practice.
