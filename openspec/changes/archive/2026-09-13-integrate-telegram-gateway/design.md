## Context

OnClaw's execution stack already provides everything a Telegram ingress needs: the `Runner` mints persistent session runs from any origin (`OriginUser`, `OriginScheduler`, `OriginChannel` ride `ExecRequest.Origin`), session events/checkpoints persist in Postgres, the approval interrupt (`tool.Interrupt` → `TranscriptEventApprovalRequired`) resumes through `Runner.Resume`, and the attachment pipeline normalizes uploads into three lanes. What does not exist is any ingress outside the web app. See proposal.md — Why.

Hard constraints from the repo:
- Injected dependencies are never nil; constructors take granular stores; defaultable knobs are functional options.
- Extension points are interfaces with registry-registered built-ins — the gateway should be one more registration, not a special case.
- The session store deliberately filters `chan_` rows out of the agent-session index and refuses unknown binding prefixes (channel-session-leak fix) — new `tg_` prefixes must be registered with those validators deliberately.
- Tenant isolation is enforced at the data layer; every query carries a workspace scope.

## Goals / Non-Goals

**Goals:**
- Telegram as a standalone ingress into **agent sessions only** — DMs and bound groups, nothing else.
- Pairing that reuses OnClaw RBAC instead of inventing a chat-side permission system.
- Full streaming fidelity within Telegram's rate limits, with zero uncaught parse errors.
- Delivery reliability (at-least-once outbox) and runaway protection (loop guard, circuit breaker).
- Minimal runner surface change: origin tagging plus session-index exclusion rules.

**Non-Goals:**
- No channel integration of any kind: no chokepoint, summon decider, feed fan-out, work sessions, or forum-topic mapping.
- No multi-agent groups (one agent per group binding).
- No Slack/Discord adapters in this change (the gateway core is shaped for them; their adapters come later).
- No ambient listening: Telegram privacy mode stays ON; untargeted group chatter never reaches ingestion.
- No outbound TTS, Mini Apps, or inline-mode commands in v1.
- No per-turn cross-surface session continuity (Telegram DM and web chat are separate sessions).

## Decisions

### D1 — Gateway core with one adapter, shaped for more
`internal/gateways/` holds the platform-neutral core: `service.go` (orchestration), `router.go` (DM vs group dispatch), `streamer.go` (debounce/heartbeat/flush), `render.go` (markdown→HTML + tag balancing + chunk splitting), `outbox.go`, `pairing.go`, `approvals.go`. `adapters/telegram` implements a narrow `PlatformAdapter` interface (Start/Stop, send/edit message, typing, approval card, file download). Alternative considered: a monolithic telegram-only package — rejected because the streaming/approval/outbox machinery is platform-independent and the next adapter (Slack) would fork it.

### D2 — Binding model: agent-scoped, unique per platform chat
`gateway_chat_bindings(platform, platform_chat_id) → agent_id` with a unique constraint on `(platform, platform_chat_id)`. No `message_thread_id` column, no channel id. Alternative considered: nullable thread-id columns for future topic mapping — rejected; the scope lock says channels/topics are out, and the migration is trivial to add later if that ever changes.

### D3 — Session keys are deterministic, suffix-bumped on `/new`
`tg_dm_<telegram_user_id>_<agent_id>` / `tg_group_<chat_id>_<agent_id>`; the active key is the base key or the highest `_<n>` suffix, so routing a reply needs no lookup. `/new` archives by minting `_<n+1>`. The store's binding validators gain `tg_dm_`/`tg_group_` as accepted prefixes; `tg_group_` rows are excluded from the per-user session index (same treatment as `sched_`), `tg_dm_` rows register under the paired member. Alternative considered: timestamped keys — rejected; they need an active-session pointer and complicate deterministic routing.

### D4 — Queueing lives in the gateway, not the RunManager
The RunManager's one-run-per-session guard stays untouched. The gateway wraps submission: on the guard's busy rejection it enqueues the message (bounded depth) and retries submission when the run's terminal outcome is observed via the existing event-stream drain. Alternative considered: a runtime busy-mode knob — rejected; it spreads policy across the runtime for one caller's benefit and risks other callers' 409 semantics.

### D5 — Streaming: edit-based, HTML, stack-balanced
Telegram HTML (not MarkdownV2) as the wire format — 18-character escaping makes MarkdownV2 unshippable for raw LLM output. The renderer converts GFM→HTML, tracks open tags on a stack and appends closers for intermediate edits, splits at 4,000 chars (margin under 4,096) preferring paragraph → line → sentence → space boundaries, and closes/reopens code fences across split parts. A `can't parse entities` failure retries once stripped to plain text. Debounce 1.2 s flush + 4.5 s typing heartbeat. Native `sendMessageDraft` streaming (Bot API 9.5) is noted as a future DM-only enhancement, not v1. Alternatives considered: MarkdownV2 with escaping — rejected (failure rate); send-only-final-message — rejected (feels dead on long tool chains).

### D6 — Identity keyed on immutable Telegram ids
`gateway_user_links(platform, platform_user_id, workspace_id) → user_id`; usernames are display-only. Pairing tokens are single-use, crypto-random, expire in 1 hour, and are consumed on use. Runs execute under the paired member's id — no gateway-level identity exists.

### D7 — Approval cards ride the existing interrupt
The streamer detects `TranscriptEventApprovalRequired` mid-drain, renders an inline keyboard (callback data = interrupt id + nonce), and on callback validates the actor is a paired member, then calls the same resume path the web UI uses. Pending-approval state is tracked per session so new turns are refused while a card is unanswered (the runner already serializes turns per session; the gateway just refuses earlier with a friendlier message).

### D8 — Ingress normalization reuses the attachment pipeline
Gateway downloads land through the same attachment store/lane classification as web uploads (image/PDF/text/drop). Voice notes: pluggable STT port (`Transcriber`), fail-soft refusal when unconfigured; the transcript is prefixed `[Voice Note]: ` in the turn input. Bot API's 20 MB download cap is documented; a self-hosted Bot API server (2 GB) is noted as an ops option, not wired in v1.

### D9 — Delivery outbox in Postgres
`gateway_outbox(id, workspace_id, session_id, payload, status, attempts, deliver_after, created_at)` written before the send; a startup sweep redelivers committed-but-unsent rows (bounded attempts within a 24 h freshness window, duplicate-warning prefix on ambiguous sends, 7-day prune). The scheduler's delivery precedent makes this pattern familiar. Alternative considered: fire-and-forget sends — rejected; a crash between generation and send silently loses agent replies.

### D10 — Guardrails are cheap and local
Loop guard: per-chat counter of consecutive bot-originated messages (drop + cooldown after threshold, default 20/5 min). Circuit breaker: consecutive gateway-level API failures auto-pause ingestion; only an admin API call resumes. `migrate_to_chat_id`: on the migration service message, rewrite the binding's `platform_chat_id` and leave session keys alone — the old `tg_group_<old_id>_…` key is abandoned and the next message naturally starts (or resumes, if previously migrated) on the new-id key; admins see the remap in the pane's binding list. Alternative considered: session-key remapping too — rejected; deterministic keys are derived, not stored, and history for the old key remains readable by direct id.

### D11 — Composition and permissions
The gateway is assembled in `internal/cli` (server command) and injected into the router; handlers live in `internal/server/handlers/gateways.go` behind the existing permission guards (admin-gated config/bindings, member-gated pairing). Webhook ingress is a public route validated by Telegram's `secret_token` header. Long polling runs as a goroutine per enabled gateway, started/stopped with the server and on config changes.

### D12 — Schema
One migration: `workspace_gateways` (unique per workspace; token ciphertext; transport + default agent), `gateway_user_links` (PK platform+platform_user_id+workspace), `gateway_pairing_tokens` (token, user, workspace, expires_at), `gateway_chat_bindings` (unique platform+chat, FK agent), `gateway_outbox`. Tokens reuse the existing secret-encryption pattern used for web-search provider credentials.

## Risks / Trade-offs

- **Rate limits under incident load**: debounce + queue + bounded queue depth keep us inside Telegram limits, but very chatty groups will see multi-second edit latency — accepted; correctness over liveness.
- **STT quality/latency**: transcription adds seconds to voice turns — acceptable for v1; provider is pluggable and fail-soft.
- **`migrate_to_chat_id` edge cases**: migration only surfaces when the bot receives the service message; if ingestion is paused at that moment the binding goes stale — the pane surfaces a stale-chat warning (binding healthy check on list).
- **Outbox duplicates**: at-least-once means a crash mid-send can double-deliver with the duplicate prefix — visible and honest, vs. silent loss.
- **HTML conversion fidelity**: exotic GFM (tables, footnotes) degrades to plain text via the fallback path — accepted; tables render as `<pre>` ASCII in v1.
