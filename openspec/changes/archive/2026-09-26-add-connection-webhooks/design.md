## Context

The gateway pipeline (Telegram/WhatsApp) already solves inbound platform events: signature validation, pairing, queueing, routing to a bound agent, approvals, and lifecycle sync. Connections (add-workspace-connections) provide the per-service knowledge home (recipes) and the credential home that webhook secrets naturally extend. What's missing is the push direction for *service* events — the difference between an agent that answers about GitHub and an agent that reacts when GitHub happens.

## Goals / Non-Goals

**Goals:**
- Ingress safety identical in spirit to gateways: verify before work, queue before route, attribute honestly.
- Provider plumbing (secret rotation, delivery ids, signature schemes) absorbed by recipes so adding a service's webhooks is data.
- Event content treated as untrusted data in prompt rendering — the injection-aware stance used for tool results.

**Non-Goals:**
- Write-flavored event automation (auto-merge on label, etc.) — needs the authority gate first.
- Bi-directional subscription management (creating/c deleting provider webhooks via API from OnClaw) — v1 is configure-at-provider with displayed URL/secret; auto-provisioning is a later nicety.
- Event transformers beyond the recipe template (no user-authored pipelines).
- Cross-workspace event fan-out (one delivery → one bound target).

## Decisions

- **D1 — Modeled on the gateway pipeline, not inside it.** A sibling ingress package reuses the gateway *shape* (verify → dedupe → queue → route → run) with per-service signature verifiers and template renderers behind recipe declarations. Rationale: gateway code is chat-platform-specific (pairing, renderers, voice notes); sharing the skeleton without the entanglement keeps both evolvable.
- **D2 — Secret lifecycle mirrors gateway pairing secrets.** Generated at enablement, workspace-AAD encrypted, shown once, rotatable (rotation invalidates the previous secret immediately — providers accept one secret per webhook). The ingest URL is deterministic from workspace + connection, so no lookup indirection exists to forge.
- **D3 — Dedupe is a small store, not a cache.** Recent delivery ids (pruned window) in a dedicated table; acknowledgment happens after verification and dedupe, before run completion, so provider retries on timeout do not double-fire while genuinely slow runs still complete.
- **D4 — Template rendering with labeled data.** Recipe templates interpolate a whitelisted set of payload fields into a turn input that wraps them in explicit data markers (the stance used for attachment and tool-result content). A malicious PR title can speak, but it arrives as quoted event data, indistinguishable in trust from any other tool result — and the run's tool gates still apply.
- **D5 — Service authority as run metadata.** Event runs carry `authority: service` + connection/event identity instead of a user id; traces, transcripts, and any future gating key off this. V1 behavior is otherwise identical to a user run from the bound target — the authority gate change is where service runs become read-tiered with approval escalation.
- **D6 — Queue with provider-friendly ack.** Providers expect fast 2xx; the pipeline verifies, dedupes, persists, acks, then processes asynchronously with the existing run machinery (bound agent, bound thread/channel), bounded like gateway queue depth.

## Risks / Trade-offs

- [Providers redeliver aggressively on timeout] → ack-after-persist + dedupe makes redelivery idempotent.
- [Recipe templates drift from provider payload shapes] → whitelisted-field interpolation fails closed (missing field → explicit render error surfaced on the connection) rather than silently emitting empty turns.
- [Flurry of events (bulk merge) floods a channel] → queue bounds + per-connection concurrency limit; coalescing identical-event bursts is a noted follow-on.
- [Public endpoint abuse] → constant-time signature check before any store access; unknown connection → generic 404 without enumeration help.

## Migration Plan

1. Migration: webhook columns on `workspace_connections` (enabled, secret envelope, target binding, selected events) and the delivery-dedupe table; inert until a connection enables webhooks.
2. Rollback: down migration; disabled-by-rollback webhooks simply stop ingesting (provider-side webhooks keep failing delivery and are reconfigured or removed by the operator).

## Open Questions

None blocking. GitHub/GitLab signature schemes and event payload fields are pinned by live verification at apply time (task 5.3 pattern).
