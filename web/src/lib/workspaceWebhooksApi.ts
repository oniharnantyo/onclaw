import { request } from './api';
import type { ApiConnectionWebhookView } from './connectionsApi';

// ---------------------------------------------------------------------------
// Workspace webhook endpoints (add-workspace-webhooks): standalone endpoints
// under /workspaces/:slug/integrations/webhooks/, every route gated
// integrations.write server-side. ONE module owns every shape and route so
// contract drift with the backend is a one-file fix (same convention as
// connectionsApi.ts, whose connection-webhook view this list also surfaces
// read-in). Secrets are write-only: the plaintext rides ONLY the create and
// rotate responses (reveal-once); every read carries the last-4 `secret_hint`.
// ---------------------------------------------------------------------------

/** D4 auth ladder: the three signature schemes a standalone endpoint declares. */
export type WebhookScheme = 'plain_token' | 'hmac_sha256' | 'timestamped_hmac';

/** Where the resolved event value comes from: a request header name, a payload
 * field path, or a fixed literal. Exactly one of name/path/value is meaningful
 * per kind — the wire carries all three and the server validates the pair. */
export interface ApiWebhookEventSource {
  kind: 'header' | 'payload_field' | 'fixed';
  /** kind=header: the request header carrying the event type. */
  name?: string;
  /** kind=payload_field: dotted path into the JSON payload. */
  path?: string;
  /** kind=fixed: the constant event value. */
  value?: string;
}

/** One standalone endpoint (domain.WorkspaceWebhook JSON). The plaintext
 * secret never rides this — `secret_hint` is the last-4 display shape. */
export interface ApiWorkspaceWebhook {
  id: string;
  workspace_id?: string;
  name: string;
  scheme: WebhookScheme;
  /** Last 4 of the stored secret — the only secret shape any read carries. */
  secret_hint?: string | null;
  event_source: ApiWebhookEventSource;
  /** Request header name the delivery id dedupes on (e.g. X-Gitlab-Delivery). */
  delivery_id_source: string;
  /** Optional expr-lang filter over {payload fields, event}; validated at save. */
  filter_expression?: string;
  /** Optional turn template; placeholders validated against the sample payload. */
  turn_template?: string;
  /** Admin-supplied JSON sample the template is validated against at save. */
  sample_payload?: string;
  target_agent_id: string;
  target_kind: 'thread' | 'channel';
  /** The channel id, or the agent-facing session handle for threads. */
  target_id: string;
  enabled: boolean;
  created_at?: string;
  updated_at?: string;
}

/** The plaintext secret — reveal-once, only on create and rotate responses. */
export interface WebhookSecretReveal {
  secret: string;
  webhook: ApiWorkspaceWebhook;
  ingest_url: string;
}

/** List row, standalone flavor: a fully-managed endpoint plus its ingest URL. */
export interface ApiStandaloneWebhookRow {
  source: 'standalone';
  webhook: ApiWorkspaceWebhook;
  ingest_url: string;
}

/** List row, connection flavor (design D2): surfaced read-in from the
 * connection's webhook binding — managed in Integrations, not here. */
export interface ApiConnectionWebhookRow {
  source: 'connection';
  connection_id: string;
  service: string;
  webhook: ApiConnectionWebhookView;
}

export type ApiWebhookRow = ApiStandaloneWebhookRow | ApiConnectionWebhookRow;

/** POST/PUT body. `secret` rides create only (bring-your-own — omitting it
 * makes the server generate one) and is never accepted on update. */
export interface CreateWebhookPayload {
  name: string;
  scheme: WebhookScheme;
  secret?: string;
  event_source: ApiWebhookEventSource;
  delivery_id_source: string;
  filter_expression?: string;
  turn_template?: string;
  sample_payload?: string;
  target_agent_id: string;
  target_kind: 'thread' | 'channel';
  target_id: string;
  enabled: boolean;
}

/** Full-state PUT body — same shape minus the secret (immutable after create). */
export type UpdateWebhookPayload = Omit<CreateWebhookPayload, 'secret'>;

/** Dry-run verdict (spec: match, ignored, or evaluation error, no side effects). */
export interface WebhookDryRunVerdict {
  outcome: 'match' | 'ignored' | 'error';
  message?: string;
}

/** One delivery-log row (D8): the bounded per-endpoint history, newest first. */
export interface ApiWebhookDelivery {
  id: string;
  webhook_id: string;
  /** The resolved event value. */
  event: string;
  outcome: 'submitted' | 'filtered' | 'duplicate' | 'rejected' | 'failed';
  /** Present when the delivery produced an agent run. */
  run_id?: string;
  detail?: string;
  created_at: string;
}

/** One workspace-wide webhook run row (add-webhook-channel-visibility 4.4):
 * a submitted delivery projected as a run for the run-history screen — the
 * endpoint as the trigger, the bound agent, the run's live status with its
 * finished telemetry. Route is integrations.write-gated. */
export interface ApiWorkspaceWebhookRun {
  id: string;
  webhook_id: string;
  webhook_name: string;
  event: string;
  run_id: string;
  agent_id: string;
  agent_name: string;
  status: 'running' | 'completed' | 'failed' | 'cancelled';
  duration_ms: number;
  tokens_used: number;
  started_at: string;
}

/** The openRun navigation state for a webhook run (add-webhook-channel-visibility)
 * — shared by the channel room's activity strip, the webhooks pane's
 * delivery-log link, and the run-history rows so every surface hands off
 * identically. `originName` rides the schedulerName slot of RunsView's
 * openRun contract: ChatRoute derives the session title ("Run · <name>") and
 * stamps unlabeled transcript turns with it; `originKind: 'webhook'` makes
 * the transcript's origin chip read `webhook · <name>`. */
export function webhookRunNavState(runId: string, originName: string) {
  return { openRun: { sessionId: runId, schedulerName: originName, originKind: 'webhook' as const } };
}

function base(ws: string): string {
  return `/workspaces/${encodeURIComponent(ws)}/integrations/webhooks`;
}

export const workspaceWebhooksApi = {
  list: (ws: string) =>
    request<{ webhooks: ApiWebhookRow[] }>(base(ws), { method: 'GET' }),

  /** Server-generated secrets return in the response exactly once (reveal-once);
   * a caller-supplied secret resolves hint-only — `secret` is absent. */
  create: (ws: string, body: CreateWebhookPayload) =>
    request<WebhookSecretReveal>(base(ws), { method: 'POST', body }),

  get: (ws: string, id: string) =>
    request<{ webhook: ApiWorkspaceWebhook; ingest_url: string }>(
      `${base(ws)}/${encodeURIComponent(id)}`,
      { method: 'GET' }
    ),

  update: (ws: string, id: string, body: UpdateWebhookPayload) =>
    request<{ webhook: ApiWorkspaceWebhook; ingest_url: string }>(
      `${base(ws)}/${encodeURIComponent(id)}`,
      { method: 'PUT', body }
    ),

  /** Stops ingestion permanently; configuration and the delivery log are gone. */
  remove: (ws: string, id: string) =>
    request<{ ok: boolean }>(`${base(ws)}/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  /** Generates a replacement secret, invalidating the prior one immediately. */
  rotate: (ws: string, id: string) =>
    request<WebhookSecretReveal>(`${base(ws)}/${encodeURIComponent(id)}/rotate`, {
      method: 'POST',
    }),

  enable: (ws: string, id: string) =>
    request<{ webhook: ApiWorkspaceWebhook; ingest_url: string }>(
      `${base(ws)}/${encodeURIComponent(id)}/enable`,
      { method: 'POST' }
    ),

  disable: (ws: string, id: string) =>
    request<{ webhook: ApiWorkspaceWebhook; ingest_url: string }>(
      `${base(ws)}/${encodeURIComponent(id)}/disable`,
      { method: 'POST' }
    ),

  /** Evaluates a sample payload against the endpoint's expression server-side —
   * one implementation of the semantics, no side effects. */
  dryRun: (ws: string, id: string, payload: unknown) =>
    request<{ verdict: WebhookDryRunVerdict }>(
      `${base(ws)}/${encodeURIComponent(id)}/dry-run`,
      { method: 'POST', body: { payload } }
    ),

  /** Bounded delivery history, newest first. */
  deliveries: (ws: string, id: string) =>
    request<{ entries: ApiWebhookDelivery[] }>(
      `${base(ws)}/${encodeURIComponent(id)}/deliveries`,
      { method: 'GET' }
    ),

  /** Workspace-wide webhook run listing (add-webhook-channel-visibility 4.4):
   * every submitted delivery projected as a run, newest first — the
   * run-history screen merges it beside the scheduler feed. Route is
   * integrations.write-gated; callers without it must tolerate the 403. */
  workspaceRuns: (ws: string) =>
    request<{ runs: ApiWorkspaceWebhookRun[] }>(
      `/workspaces/${encodeURIComponent(ws)}/webhook-runs`,
      { method: 'GET' }
    ),
};

/** The sender-setup signature header per scheme — the header name the sender
 * configures in its webhook dashboard for this endpoint's scheme. */
export function signatureHeaderFor(scheme: WebhookScheme): string {
  switch (scheme) {
    case 'hmac_sha256':
      return 'X-Hub-Signature-256';
    case 'timestamped_hmac':
      return 'X-Webhook-Signature';
    default:
      return 'X-Gitlab-Token';
  }
}
