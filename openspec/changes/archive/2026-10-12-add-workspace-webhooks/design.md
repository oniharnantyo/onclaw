# Design

## Context

The connection-webhook pipeline (`internal/webhooks`: verify, dedupe, bounded queue, `RunnerPort` run submitter, fail-closed `Render`, AES-GCM secret cipher) is complete and specced, but every knob — event catalog, template, scheme — is recipe data fixed at compile time, and ingestion is addressed by connection id. Standalone triggers need the same pipeline addressed by a workspace-owned endpoint with user-declared config. See proposal.md for motivation; the behavior contract is the `workspace-webhooks` delta spec.

## Goals / Non-Goals

Goals: one pipeline, two ingress addresses (connection-id and standalone-endpoint-id); user-declared auth, filter, and template validated at save time; a management surface that answers "did the event reach the agent, and if not, why not".

Non-Goals: no unification of the connection-webhook store (v2); no outbound webhooks; no coalescing/debounce; no script transforms; no change to how agents acquire tools (connections stay the capability surface).

## Decisions

- **D1 — Standalone entity, not a recipe kind.** A `workspace_webhooks` row is its own aggregate (name, scheme, credential envelope, event source, delivery-id source, filter expression, template, target binding, enabled). A "custom recipe" was rejected because recipes are compile-time Go data — user-defined recipes would need a registry rewrite and still couple trigger config to a connection; the entity keeps triggers and capabilities (connections) cleanly separated.
- **D2 — Deliberate split-brain v1.** The Webhooks pane manages standalone endpoints and surfaces connection-bound webhooks read-in (driving the existing service for rotate/pause where the admin already has `integrations.write`), linking to Integrations for full management. A big-bang unified table was rejected: it would migrate `ConnectionWebhook` state and regress a shipped, gated-on-live-pass flow for no user-visible gain yet. Unification remains a v2 migration once the standalone surface has live use.
- **D3 — expr-lang/expr as the filter engine.** Compiled once at save (invalid → 400 with the parser's message, nothing persisted), evaluated per delivery against `{payload fields, event}`. Sandbox config denies function calls and limits the env to the comparison/logic/access builtins; eval errors and `false` both mean "ignored" (fail-closed for triggering, provider-friendly 200 ack). Alternatives: cel-go (heavier, its schema-typing advantage is moot on arbitrary JSON), hand-rolled parser (owns edge cases for no gain), Hermes-style declarative operator lists (less expressive than the user-requested expression; `event == "x" && repo.name == "test"` is native expr). Strings are double-quoted in the dialect; the save-time validator's message covers the single-quote case. A dry-run endpoint evaluates a sample payload server-side — one implementation of the semantics, never a client-side re-derivation.
- **D4 — Auth ladder extends the existing verifier.** `VerifySignature` gains the timestamped scheme: `t=<unix>,v1=<hex(HMAC-SHA256(key, "{ts}.{body}"))>`, ±300s skew, constant-time compare. Replay within the window is rejected by recording `hash(signature)` in the same durable delivery-dedupe store the pipeline already prunes hourly — cluster-safe where goclaw's per-process nonce cache is not. Secrets: the existing AES-GCM cipher and reveal-once envelope, plus an optional caller-supplied secret (stored, last-4 hinted, never re-revealed). Plain token and body-HMAC schemes keep GitLab/GitHub compatibility unchanged.
- **D5 — Separate public address.** `POST /api/ingest/hooks/{workspace_slug}/{webhook_id}` — a distinct id-space keeps the connection ingest contract untouched and lets the handler fail with the same generic unenumerable error for unknown endpoint, disabled, bad scheme material, or failed verification. Body cap defaults to 1 MB (recipe payloads legitimately exceed what generic senders need); a per-endpoint fixed-window rate limit answers `429` + `Retry-After`, in-memory per process for v1 (documented multi-node softness, the goclaw posture).
- **D6 — Pipeline reuse via a second submitter binding.** The queue and ingress are reused; the standalone path binds its own resolution (endpoint → scheme/secret/filter/template/target) and its own `RunnerPort` adapter carrying webhook attribution — the run keeps `Origin=service` with the endpoint id and resolved event where connection webhooks carry connection id/service. Write-tier tools gate through integration-authority exactly as connection webhook runs do; no new authority concept.
- **D7 — Turn rendering reuses the fail-closed renderer.** A declared template validates its placeholders against the admin-supplied sample payload at save (the whitelist discipline, moved to save time); a delivery missing a field drops with `ErrRenderFailed`-class error, never a half-filled turn. No template means the payload rides as labeled, truncated JSON (cap ≈ 4 KB, the Hermes-dumped-payload precedent) prefixed by the endpoint name and resolved event.
- **D8 — Delivery log is a bounded, pruned table.** One row per processed delivery: resolved event, outcome (`submitted` with run id | `filtered` | `duplicate` | `rejected` | `failed`), timestamp. Pruned by the existing hourly sweep pattern; retention small and fixed (e.g., last 100 per endpoint) — enough to answer "why didn't my event fire" without a log-analysis product.
- **D9 — The pane is registry-driven and API-backed.** New `webhooks` entry in the settings section registry (`/settings/webhooks`), structured form fields per the design contract, reveal-once moment reusing the connection-webhook enable pattern, monospace expression field with inline save-time validation, dry-run panel, per-endpoint delivery log drawer, connection-webhook rows rendered from the same list endpoint flagged `source: connection` with a link to Integrations.

## Risks / Trade-offs

- [New third-party dependency (`expr-lang/expr`)] → pinned, MIT, widely deployed (Argo); sandbox denies calls/IO; compile only at save.
- [Expression eval as an attack surface] → payload size cap upstream, sandboxed AST env, eval on already-parsed JSON, no user functions; worst case is one wasted parse of a ≤1 MB body.
- [Clock skew breaks timestamped senders] → ±300s window documented in setup help; plain-token and body-HMAC schemes remain available; setup copy says "sync NTP".
- [Split-brain confuses admins] → connection rows are visually flagged and link to Integrations; the v2 unification is recorded in the proposal as the intended end state.
- [In-memory rate limit is per-process] → soft limit by design; the durable bounds (queue depth, per-endpoint queue lane) remain the hard backstop.

## Migration Plan

Purely additive: one migration creating `workspace_webhooks` + `webhook_delivery_log`; new routes; new pane. No writes to existing connection-webhook tables. Rollback is route/pane disable plus table drop — no data migration backwards.

## Open Questions

None blocking. Defaults chosen for later tuning: rate limit 60 req/min per endpoint, body cap 1 MB, delivery log 100 rows, replay window 300s.
