# Proposal

## Why

Webhook-driven agents exist only as connection webhooks: the event catalog, turn template, and auth scheme are compile-time recipe data, and management is buried inside the Integrations connection card. Any service outside the recipe catalog — an internal script, a CI job, a self-hosted SaaS — cannot trigger an agent at all. The immediate driver is GitLab MR review (an MR-opened event should wake a reviewer agent), but the need is general: workspace admins want a dedicated settings surface where they create webhook endpoints themselves, declare what a JSON payload must match, and bind it to an agent.

## What Changes

- **New standalone webhook endpoints** at workspace level, created and managed by admins — independent of the connection/recipe catalog. Each endpoint declares: name, auth credential + scheme, event source, delivery-id source, a filter expression over the JSON payload, an optional turn template, and a target binding (agent + thread/channel).
- **Expression filtering** of inbound JSON payloads before any run: a sandboxed expression (expr-lang/expr — comparisons, logic, field access only) compiled and validated at save time; non-match and evaluation error both acknowledge `200 {"status":"ignored","reason":"filter"}` without a run. A dry-run tester (sample payload → match / ignored / error) ships with the editor.
- **Auth ladder for general senders**: per-endpoint secret, generated (reveal-once + rotate, the connection-webhook flow) or bring-your-own; schemes are plain token (GitLab's `X-Gitlab-Token`), body HMAC-SHA256 (GitHub), and a new timestamped HMAC (`t=<unix>,v1=<hex>` over `{ts}.{body}`, ±300s skew, replay rejected via the durable dedupe store) as the recommended default for generic senders.
- **Dedicated Settings → Webhooks pane**: list (standalone endpoints managed here; connection-bound webhooks surfaced read-in), structured create/edit form per the design contract (labeled fields, monospace expression input, no raw-JSON textarea), secret reveal-once + rotate, enable/pause, per-endpoint delivery log (event, filter result, run id | ignored reason), dry-run tester.
- **New public ingest path** `/api/ingest/hooks/{workspace_slug}/{webhook_id}` reusing the existing verify → dedupe → bounded-queue → agent-run pipeline, with standalone-webhook service attribution and a smaller default body cap plus a per-endpoint rate limit (429 + `Retry-After`).
- Payload contract: requests are JSON; the default agent turn is the payload embedded as labeled, truncated JSON — templates are optional and stay fail-closed (validated placeholders at save).

Deliberately out of scope (recorded decisions): unifying the connection-webhook table into the new registry (v2 — v1 is a deliberate split-brain: standalone managed in the pane, connection webhooks surfaced read-in); outbound webhooks; event coalescing/debounce; payload filters beyond the expression (no script transforms); agent capabilities remain connection-sourced — the GitLab MR review verbs (diffs, MR notes, `merge_request.update`) are a separate change stacked on top.

## Capabilities

### New Capabilities

- `workspace-webhooks`: standalone workspace webhook endpoints — endpoint lifecycle (create/enable/rotate/pause/disable with reveal-once and bring-your-own secrets), verified and deduplicated ingress with the three-scheme auth ladder, save-time-validated expression filtering over JSON payloads, event-to-turn routing to a bound agent and thread/channel target with service-authority attribution, delivery logging, and the Settings → Webhooks management surface including the dry-run tester.

### Modified Capabilities

- `web-app/settings`: the Settings navigation requirement's section enumeration gains **Webhooks** (fourteen sections; the pane's own behavior is specified under `workspace-webhooks`, matching how the Gateways and Hooks panes are specced in their own capabilities).

## Impact

- **Backend**: new `internal/webhooks` surface (or sibling package) for the standalone endpoint service — lifecycle, filter compile/eval, dry-run; new domain entity + store port + Postgres adapter + migration; `VerifySignature` extended with the timestamped-HMAC scheme (replay rows in the existing delivery dedupe table); ingest route + handler parallel to `/api/ingest/webhooks/...`; run-submit attribution carries webhook identity instead of connection identity; new `expr-lang/expr` dependency (sandboxed configuration).
- **Frontend**: new `webhooks` settings section in `web/src/screens/settings/` (pane registry in `SettingsPage.tsx`), router entry `/settings/webhooks`.
- **APIs**: workspace-scoped CRUD + rotate + enable/pause + dry-run + delivery-log endpoints under `/integrations/webhooks` (or sibling path), permission-gated `integrations.write`; public ingest route.
- **Security**: signature verification and dedupe semantics shared with connection webhooks; rate limiting and a ~1 MB default body cap for the new ingress; fail-closed template rendering reused unchanged.
- **Specs**: `web-app/settings` navigation delta; new `workspace-webhooks` spec.
