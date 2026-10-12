import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { cx, relativeTime } from "../../lib/helpers";
import { Icon } from "../../components/ui/Icon";
import { Toggle } from "../../components/ui/Toggle";
import { Chip } from "../../components/ui/Chip";
import { Modal } from "../../components/ui/Modal";
import { ErrorState } from "../../components/ErrorState";
import { inputCls, labelCls } from "../../components/ui/constants";
import {
  api,
  ApiError,
  formatApiError,
  listAgentSessions,
  type ApiAgent,
  type ApiAgentSession,
  type ApiChannel,
} from "../../lib/api";
import { useCanManageIntegrations } from "../../lib/connectionsApi";
import {
  signatureHeaderFor,
  webhookRunNavState,
  workspaceWebhooksApi,
  type ApiConnectionWebhookRow,
  type ApiStandaloneWebhookRow,
  type ApiWebhookDelivery,
  type ApiWebhookEventSource,
  type ApiWebhookRow,
  type ApiWorkspaceWebhook,
  type CreateWebhookPayload,
  type UpdateWebhookPayload,
  type WebhookDryRunVerdict,
  type WebhookScheme,
} from "../../lib/workspaceWebhooksApi";
import serverErrorSvg from "../../assets/server-error.svg";

export interface WebhooksPaneProps {
  tenant: any;
  /** Unused — the pane is API-backed; kept for SettingsPage compat. */
  onUpdate?: (fn: any) => void;
  onToast?: (text: string, kind?: string) => void;
  /** Override the derived integrations.write check (tests). */
  canWrite?: boolean;
}

const copyText = (text: string, onToast: (t: string, k?: string) => void, okMsg: string) => {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(
      () => onToast(okMsg),
      () => onToast("Clipboard blocked by the browser", "danger")
    );
  } else {
    onToast("Clipboard blocked by the browser", "danger");
  }
};

const monoInputCls =
  "h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 font-mono text-[12px] text-fg2 focus:border-accent";
const monoAreaCls =
  "w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 py-2 font-mono text-[12px] leading-4 text-fg2 focus:border-accent";

const SCHEMES: { id: WebhookScheme; label: string; help: string }[] = [
  {
    id: "plain_token",
    label: "Plain token",
    help: "Sender puts the secret in the X-Gitlab-Token header — GitLab and most simple senders.",
  },
  {
    id: "hmac_sha256",
    label: "HMAC-SHA256",
    help: "Sender signs the raw body — sha256=<hex> in X-Hub-Signature-256, GitHub-style.",
  },
  {
    id: "timestamped_hmac",
    label: "Timestamped HMAC",
    help: "t=<unix>,v1=<hex> over \"{t}.{body}\" in X-Webhook-Signature, ±300s clock skew — sync NTP.",
  },
];

const schemeLabel = (s: WebhookScheme) => SCHEMES.find((x) => x.id === s)?.label || s;

const EVENT_SOURCE_KINDS: { id: ApiWebhookEventSource["kind"]; label: string }[] = [
  { id: "header", label: "Request header" },
  { id: "payload_field", label: "Payload field" },
  { id: "fixed", label: "Fixed value" },
];

const DELIVERY_OUTCOMES: { id: ApiWebhookDelivery["outcome"]; label: string; cls: string }[] = [
  {
    id: "submitted",
    label: "Submitted",
    cls: "text-[color-mix(in_oklab,var(--success),black_25%)] bg-[color-mix(in_oklab,var(--success)_10%,transparent)] border-[color-mix(in_oklab,var(--success)_35%,transparent)]",
  },
  {
    id: "filtered",
    label: "Filtered",
    cls: "text-muted bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] border-line",
  },
  {
    id: "duplicate",
    label: "Duplicate",
    cls: "text-[color-mix(in_oklab,var(--warn),black_38%)] bg-[color-mix(in_oklab,var(--warn)_8%,transparent)] border-[color-mix(in_oklab,var(--warn)_35%,transparent)]",
  },
  {
    id: "rejected",
    label: "Rejected",
    cls: "text-danger bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] border-[color-mix(in_oklab,var(--danger)_35%,transparent)]",
  },
  {
    id: "failed",
    label: "Failed",
    cls: "text-danger bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] border-[color-mix(in_oklab,var(--danger)_35%,transparent)]",
  },
];

const deliveryOutcomeView = (o: ApiWebhookDelivery["outcome"]) =>
  DELIVERY_OUTCOMES.find((d) => d.id === o) || DELIVERY_OUTCOMES[1];

/**
 * Webhooks settings surface (add-workspace-webhooks, design D9): standalone
 * webhook endpoints managed end to end here, connection-bound webhooks
 * surfaced read-in with a link into Integrations. The secret is write-only —
 * create and rotate reveal it exactly once through the banner; reads carry
 * only the last-4 `secret_hint`. Every management action needs
 * integrations.write (same gate as the Integrations surface); everyone else
 * sees the list read-only. Configuration is structured labeled fields only —
 * the filter expression is a monospace input whose save-time 400 carries the
 * parser's message verbatim inline.
 */
export function WebhooksPane({ tenant, onToast = () => {}, canWrite }: WebhooksPaneProps) {
  const derivedCanManage = useCanManageIntegrations(tenant);
  const writer = canWrite !== undefined ? canWrite : derivedCanManage;

  const [rows, setRows] = useState<ApiWebhookRow[]>([]);
  const [agents, setAgents] = useState<ApiAgent[]>([]);
  const [channels, setChannels] = useState<ApiChannel[]>([]);

  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<ApiError | Error | null>(null);

  // { mode: 'create' } | { mode: 'edit'; webhook; ingestUrl }
  const [editor, setEditor] = useState<
    { mode: "create" } | { mode: "edit"; webhook: ApiWorkspaceWebhook; ingestUrl: string } | null
  >(null);
  // Reveal-once secret from create/rotate — local state only, never re-read.
  const [reveal, setReveal] = useState<{ secret: string; webhook: ApiWorkspaceWebhook; ingestUrl: string } | null>(null);

  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState<string | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [deliveriesMap, setDeliveriesMap] = useState<Record<string, ApiWebhookDelivery[]>>({});
  const [deliveriesLoading, setDeliveriesLoading] = useState<Record<string, boolean>>({});
  const [deliveriesError, setDeliveriesError] = useState<Record<string, string>>({});
  const [dryRunPayload, setDryRunPayload] = useState<Record<string, string>>({});
  const [dryRunVerdict, setDryRunVerdict] = useState<
    Record<string, WebhookDryRunVerdict | { outcome: "invalid_json"; message: string }>
  >({});
  const [dryRunBusy, setDryRunBusy] = useState<Record<string, boolean>>({});

  const targetWsId = tenant?.sub || tenant?.id;

  const load = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const [listRes, agentRes, channelRes] = await Promise.all([
        workspaceWebhooksApi.list(targetWsId),
        api.agents.list(targetWsId).catch(() => ({ agents: [] })),
        api.channels.list(targetWsId).catch(() => ({ channels: [] })),
      ]);
      setRows(listRes?.webhooks ?? []);
      setAgents(agentRes?.agents || []);
      setChannels(channelRes?.channels || []);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 0) {
        return;
      }
      setLoadError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (targetWsId) void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reload per workspace only
  }, [targetWsId, writer]);

  const agentName = (id: string | null | undefined) =>
    agents.find((a) => a.id === id)?.name || id || "";

  const standaloneRows = rows.filter((r): r is ApiStandaloneWebhookRow => r.source === "standalone");
  const connectionRows = rows.filter((r): r is ApiConnectionWebhookRow => r.source === "connection");

  const patchStandalone = (id: string, webhook: ApiWorkspaceWebhook, ingest_url: string) => {
    setRows((prev) =>
      prev.map((r) => (r.source === "standalone" && r.webhook.id === id ? { ...r, webhook, ingest_url } : r))
    );
  };

  const handleToggleEnabled = async (row: ApiStandaloneWebhookRow, next: boolean) => {
    const id = row.webhook.id;
    setBusyAction("toggle-" + id);
    try {
      const res = next
        ? await workspaceWebhooksApi.enable(targetWsId, id)
        : await workspaceWebhooksApi.disable(targetWsId, id);
      patchStandalone(id, res.webhook, res.ingest_url);
      onToast(
        next ? `${row.webhook.name} resumed` : `${row.webhook.name} paused — deliveries are acknowledged but ignored`
      );
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to update the webhook"), "danger");
    } finally {
      setBusyAction(null);
    }
  };

  const handleRotate = async (row: ApiStandaloneWebhookRow) => {
    const id = row.webhook.id;
    setBusyAction("rotate-" + id);
    try {
      const res = await workspaceWebhooksApi.rotate(targetWsId, id);
      patchStandalone(id, res.webhook, res.ingest_url);
      setReveal({ secret: res.secret, webhook: res.webhook, ingestUrl: res.ingest_url });
      onToast("Secret rotated — copy it now, it won't be shown again");
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to rotate the secret"), "danger");
    } finally {
      setBusyAction(null);
    }
  };

  const handleDelete = async (row: ApiStandaloneWebhookRow) => {
    const id = row.webhook.id;
    if (confirmingDelete !== id) {
      setConfirmingDelete(id);
      return;
    }
    setBusyAction("delete-" + id);
    try {
      await workspaceWebhooksApi.remove(targetWsId, id);
      setRows((prev) => prev.filter((r) => !(r.source === "standalone" && r.webhook.id === id)));
      setConfirmingDelete(null);
      if (expandedId === id) setExpandedId(null);
      onToast(`${row.webhook.name} deleted`);
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to delete the webhook"), "danger");
    } finally {
      setBusyAction(null);
    }
  };

  const handleExpand = async (row: ApiStandaloneWebhookRow) => {
    const id = row.webhook.id;
    if (expandedId === id) {
      setExpandedId(null);
      return;
    }
    setExpandedId(id);
    if (!deliveriesMap[id]) {
      setDeliveriesLoading((m) => ({ ...m, [id]: true }));
      try {
        const res = await workspaceWebhooksApi.deliveries(targetWsId, id);
        setDeliveriesMap((m) => ({ ...m, [id]: res.entries ?? [] }));
      } catch (err: unknown) {
        setDeliveriesError((m) => ({ ...m, [id]: formatApiError(err, "Couldn't load the delivery log") }));
      } finally {
        setDeliveriesLoading((m) => ({ ...m, [id]: false }));
      }
    }
  };

  const handleDryRun = async (row: ApiStandaloneWebhookRow) => {
    const id = row.webhook.id;
    const raw = (dryRunPayload[id] || "").trim();
    let payload: unknown;
    try {
      payload = JSON.parse(raw);
    } catch {
      setDryRunVerdict((v) => ({ ...v, [id]: { outcome: "invalid_json", message: "Payload must be valid JSON" } }));
      return;
    }
    setDryRunBusy((b) => ({ ...b, [id]: true }));
    try {
      const res = await workspaceWebhooksApi.dryRun(targetWsId, id, payload);
      setDryRunVerdict((v) => ({ ...v, [id]: res.verdict }));
    } catch (err: unknown) {
      setDryRunVerdict((v) => ({
        ...v,
        [id]: { outcome: "error", message: formatApiError(err, "Dry-run failed") },
      }));
    } finally {
      setDryRunBusy((b) => ({ ...b, [id]: false }));
    }
  };

  const hasRows = rows.length > 0;

  return (
    <div className="w-full" data-od-id="pane-webhooks" data-testid="pane-webhooks">
      <div className="flex items-start justify-between gap-3 mb-4">
        <div>
          <h3 className="text-[15px] font-semibold text-fg">Webhook endpoints</h3>
          <p className="mt-0.5 text-[12px] text-muted">
            Verified JSON deliveries — GitLab pushes, alert managers — become turns for a bound agent in a thread or channel.
          </p>
        </div>
        {writer && (
          <button
            type="button"
            data-od-id="btn-webhook-add"
            data-testid="btn-webhook-add"
            onClick={() => setEditor({ mode: "create" })}
            className="h-8 shrink-0 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
          >
            ＋ New webhook
          </button>
        )}
      </div>

      {loading && rows.length === 0 ? (
        <div className="space-y-3" data-testid="webhooks-loading">
          {[0, 1, 2].map((i) => (
            <div
              key={i}
              className="h-16 animate-pulse rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]"
            />
          ))}
        </div>
      ) : loadError ? (
        <div className="py-6">
          <ErrorState
            illustration={serverErrorSvg}
            title="Couldn't load the webhooks"
            detail={loadError.message}
            primaryAction={{ label: "Retry", onClick: () => void load() }}
          />
        </div>
      ) : !hasRows ? (
        <div
          className="rounded-md border border-dashed border-line p-6 text-center"
          data-testid="webhooks-empty"
        >
          <div className="mx-auto flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] text-muted">
            <Icon name="embed" size={20} />
          </div>
          <h4 className="mt-3 text-[14px] font-medium text-fg">No webhook endpoints</h4>
          <p className="mx-auto mt-1 max-w-md text-[12px] text-muted">
            Webhooks turn verified JSON deliveries into agent runs: each endpoint carries its own secret and
            signature scheme, an optional filter over the payload, and a binding to one agent in a thread or
            channel. Connection webhooks you enable in Integrations appear here too.
          </p>
          {writer && (
            <button
              type="button"
              data-testid="btn-webhook-empty-create"
              onClick={() => setEditor({ mode: "create" })}
              className="mt-4 inline-flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90"
            >
              Create a webhook
            </button>
          )}
        </div>
      ) : (
        <ul className="space-y-3">
          {standaloneRows.map((row) => (
            <StandaloneRow
              key={row.webhook.id}
              row={row}
              writer={writer}
              agentLabel={agentName(row.webhook.target_agent_id)}
              channels={channels}
              busyAction={busyAction}
              confirmingDelete={confirmingDelete === row.webhook.id}
              deliveries={deliveriesMap[row.webhook.id]}
              deliveriesLoading={Boolean(deliveriesLoading[row.webhook.id])}
              deliveriesError={deliveriesError[row.webhook.id]}
              dryRunPayload={dryRunPayload[row.webhook.id] || ""}
              dryRunVerdict={dryRunVerdict[row.webhook.id]}
              dryRunBusy={Boolean(dryRunBusy[row.webhook.id])}
              expanded={expandedId === row.webhook.id}
              onToggleEnabled={() => void handleToggleEnabled(row, !row.webhook.enabled)}
              onRotate={() => void handleRotate(row)}
              onDelete={() => void handleDelete(row)}
              onEdit={() => setEditor({ mode: "edit", webhook: row.webhook, ingestUrl: row.ingest_url })}
              onExpand={() => void handleExpand(row)}
              onCopyIngest={() =>
                copyText(row.ingest_url, onToast, "Ingest URL copied to clipboard")
              }
              onDryRunPayload={(v) => setDryRunPayload((p) => ({ ...p, [row.webhook.id]: v }))}
              onDryRun={() => void handleDryRun(row)}
            />
          ))}
          {connectionRows.map((row) => (
            <ConnectionRow key={"connection-" + row.connection_id} row={row} />
          ))}
        </ul>
      )}

      {editor && (
        <WebhookEditorModal
          tenant={tenant}
          mode={editor.mode}
          webhook={editor.mode === "edit" ? editor.webhook : null}
          ingestUrl={editor.mode === "edit" ? editor.ingestUrl : ""}
          agents={agents}
          channels={channels}
          onClose={() => setEditor(null)}
          onToast={onToast}
          onCreated={(webhook, ingestUrl, secret) => {
            setEditor(null);
            void load();
            if (secret) {
              setReveal({ secret, webhook, ingestUrl });
              onToast("Webhook created — copy the secret now, it won't be shown again");
            } else {
              onToast(`Webhook ${webhook.name} created`);
            }
          }}
          onUpdated={(webhook, ingestUrl) => {
            setEditor(null);
            patchStandalone(webhook.id, webhook, ingestUrl);
            onToast(`${webhook.name} updated`);
          }}
        />
      )}

      {reveal && (
        <SecretRevealModal
          webhook={reveal.webhook}
          secret={reveal.secret}
          ingestUrl={reveal.ingestUrl}
          onClose={() => setReveal(null)}
          onToast={onToast}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Standalone row + expansion (dry-run tester, delivery log)
// ---------------------------------------------------------------------------

interface StandaloneRowProps {
  row: ApiStandaloneWebhookRow;
  writer: boolean;
  agentLabel: string;
  channels: ApiChannel[];
  busyAction: string | null;
  confirmingDelete: boolean;
  expanded: boolean;
  deliveries?: ApiWebhookDelivery[];
  deliveriesLoading: boolean;
  deliveriesError?: string;
  dryRunPayload: string;
  dryRunVerdict?: WebhookDryRunVerdict | { outcome: "invalid_json"; message: string };
  dryRunBusy: boolean;
  onToggleEnabled: () => void;
  onRotate: () => void;
  onDelete: () => void;
  onEdit: () => void;
  onExpand: () => void;
  onCopyIngest: () => void;
  onDryRunPayload: (v: string) => void;
  onDryRun: () => void;
}

function StandaloneRow(props: StandaloneRowProps) {
  const { row, writer, agentLabel, busyAction, confirmingDelete, expanded } = props;
  const wh = row.webhook;
  const id = wh.id;
  const busy = busyAction !== null;
  const scheme = signatureHeaderFor(wh.scheme);
  const targetLabel =
    wh.target_kind === "channel"
      ? props.channels.find((c) => c.id === wh.target_id)?.name || wh.target_id
      : wh.target_id;

  return (
    <li
      className="rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))]"
      data-od-id={"webhook-row-" + id}
      data-testid={"webhook-row-" + id}
    >
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <p className="truncate text-[13px] font-medium text-fg" data-testid={"webhook-name-" + id}>
              {wh.name}
            </p>
            <Chip mono>
              <span data-testid={"webhook-scheme-" + id}>{schemeLabel(wh.scheme)}</span>
            </Chip>
            {wh.secret_hint ? (
              <span
                className="inline-flex items-center gap-1 font-mono text-[11px] text-muted"
                title="Secret last 4 — never shown in full"
                data-testid={"webhook-hint-" + id}
              >
                <Icon name="lock" size={10} />
                ····{wh.secret_hint}
              </span>
            ) : null}
            {wh.enabled ? (
              <Chip>
                <span data-testid={"webhook-state-" + id}>Active</span>
              </Chip>
            ) : (
              <Chip>
                <span data-testid={"webhook-state-" + id}>Paused</span>
              </Chip>
            )}
          </div>
          <p className="mt-1 truncate text-[11px] text-muted" data-testid={"webhook-target-" + id}>
            {agentLabel} → {wh.target_kind === "channel" ? "#" : "thread "}
            {targetLabel}
          </p>
        </div>

        <div className="flex min-w-0 items-center gap-2">
          <code
            title={row.ingest_url}
            className="hidden max-w-[260px] truncate rounded-[6px] border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-2 py-1 font-mono text-[11px] text-fg2 lg:block"
            data-testid={"webhook-ingest-" + id}
          >
            {row.ingest_url}
          </code>
          <button
            type="button"
            onClick={props.onCopyIngest}
            aria-label="Copy ingest URL"
            data-testid={"btn-copy-ingest-" + id}
            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
          >
            <Icon name="copy" size={13} />
          </button>
          {writer && (
            <Toggle
              on={wh.enabled}
              label={`Enable ${wh.name}`}
              onChange={() => props.onToggleEnabled()}
            />
          )}
        </div>

        <div className="flex w-full items-center gap-2 border-t border-[var(--border-soft)] pt-2 md:w-auto md:border-0 md:pt-0">
          {writer && (
            <>
              <button
                type="button"
                data-testid={"btn-rotate-" + id}
                onClick={props.onRotate}
                disabled={busy}
                className="h-7 rounded-[6px] px-2 text-[11px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg disabled:opacity-50"
              >
                {busyAction === "rotate-" + id ? "Rotating…" : "Rotate"}
              </button>
              <button
                type="button"
                data-testid={wh.enabled ? "btn-pause-" + id : "btn-resume-" + id}
                onClick={props.onToggleEnabled}
                disabled={busy}
                className="h-7 rounded-[6px] px-2 text-[11px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg disabled:opacity-50"
              >
                {busyAction === "toggle-" + id ? "Saving…" : wh.enabled ? "Pause" : "Resume"}
              </button>
            </>
          )}
          <button
            type="button"
            data-testid={"btn-deliveries-" + id}
            onClick={props.onExpand}
            className="h-7 rounded-[6px] px-2 text-[11px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            {expanded ? "Hide log" : "Deliveries"}
          </button>
          {writer && (
            <>
              <button
                type="button"
                data-testid={"btn-edit-" + id}
                onClick={props.onEdit}
                disabled={busy}
                className="h-7 rounded-[6px] px-2 text-[11px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg disabled:opacity-50"
              >
                Edit
              </button>
              {confirmingDelete ? (
                <button
                  type="button"
                  data-testid={"btn-delete-confirm-" + id}
                  onClick={props.onDelete}
                  disabled={busy}
                  className="h-7 rounded-[6px] border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] px-2 text-[11px] font-medium text-danger transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] disabled:opacity-50"
                >
                  Confirm delete
                </button>
              ) : (
                <button
                  type="button"
                  data-testid={"btn-delete-" + id}
                  onClick={props.onDelete}
                  disabled={busy}
                  className="h-7 rounded-[6px] px-2 text-[11px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger disabled:opacity-50"
                >
                  {busyAction === "delete-" + id ? "Deleting…" : "Delete"}
                </button>
              )}
            </>
          )}
        </div>
      </div>

      {expanded && (
        <div
          className="border-t border-[var(--border-soft)] px-4 py-3"
          data-testid={"webhook-expand-" + id}
        >
          {writer && (
            <div className="mb-4">
              <p className="mb-1 text-[12px] font-medium text-fg2">Dry-run the filter</p>
              <p className="mb-2 text-[11px] text-muted">
                Evaluates a sample payload against the expression — no side effects, nothing is enqueued.
              </p>
              <textarea
                aria-label={`Dry-run payload for ${wh.name}`}
                data-testid={"input-dryrun-payload-" + id}
                rows={3}
                value={props.dryRunPayload}
                onChange={(e) => props.onDryRunPayload(e.target.value)}
                placeholder='{"event": "merge_request", "project": {"path_with_namespace": "team/api"}}'
                className={monoAreaCls}
              />
              <div className="mt-2 flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  data-testid={"btn-dryrun-" + id}
                  onClick={props.onDryRun}
                  disabled={props.dryRunBusy || !props.dryRunPayload.trim()}
                  className="h-7 rounded-md border border-line px-2.5 text-[11px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-40"
                >
                  {props.dryRunBusy ? "Running…" : "Run test"}
                </button>
                {props.dryRunVerdict && (
                  <span
                    data-testid={"webhook-verdict-" + id}
                    className={cx(
                      "inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] font-medium",
                      props.dryRunVerdict.outcome === "match"
                        ? "text-[color-mix(in_oklab,var(--success),black_25%)] bg-[color-mix(in_oklab,var(--success)_10%,transparent)] border-[color-mix(in_oklab,var(--success)_35%,transparent)]"
                        : props.dryRunVerdict.outcome === "ignored"
                        ? "text-[color-mix(in_oklab,var(--warn),black_38%)] bg-[color-mix(in_oklab,var(--warn)_8%,transparent)] border-[color-mix(in_oklab,var(--warn)_35%,transparent)]"
                        : "text-danger bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] border-[color-mix(in_oklab,var(--danger)_35%,transparent)]"
                    )}
                  >
                    {props.dryRunVerdict.outcome === "match"
                      ? "Match"
                      : props.dryRunVerdict.outcome === "ignored"
                      ? "Ignored"
                      : "Error"}
                    {props.dryRunVerdict.message ? ` — ${props.dryRunVerdict.message}` : ""}
                  </span>
                )}
              </div>
            </div>
          )}

          <div>
            <p className="mb-2 text-[12px] font-medium text-fg2">Delivery log</p>
            {props.deliveriesLoading ? (
              <p className="text-[11px] text-muted" data-testid={"deliveries-loading-" + id}>
                Loading deliveries…
              </p>
            ) : props.deliveriesError ? (
              <p className="text-[11px] text-danger" data-testid={"deliveries-error-" + id}>
                {props.deliveriesError}
              </p>
            ) : !props.deliveries || props.deliveries.length === 0 ? (
              <p className="text-[11px] text-muted" data-testid={"deliveries-empty-" + id}>
                No deliveries yet — events the sender posts land here with their outcome.
              </p>
            ) : (
              <ul className="divide-y divide-[var(--border-soft)] rounded-md border border-line">
                {props.deliveries.map((d) => {
                  const out = deliveryOutcomeView(d.outcome);
                  return (
                    <li key={d.id} className="px-3 py-2" data-testid={"delivery-" + d.id}>
                      <div className="flex flex-wrap items-center gap-2">
                        <span
                          data-testid={"delivery-outcome-" + d.id}
                          className={cx(
                            "inline-flex items-center rounded-full border px-1.5 py-0.5 text-[10px] font-medium",
                            out.cls
                          )}
                        >
                          {out.label}
                        </span>
                        <span className="font-mono text-[11px] text-fg2">{d.event}</span>
                        <span className="text-[11px] text-muted">{relativeTime(d.created_at)}</span>
                        {d.run_id && (
                          <Link
                            // Open the run's transcript (add-webhook-channel-visibility):
                            // runs hydrate under the endpoint's agent chat, so the link
                            // carries the run handoff state instead of pointing at the
                            // unfiltered runs screen. No bound agent → legacy fallback.
                            to={
                              wh.target_agent_id
                                ? "/c/" + encodeURIComponent(wh.target_agent_id)
                                : "/runs"
                            }
                            state={
                              wh.target_agent_id
                                ? webhookRunNavState(d.run_id, wh.name)
                                : undefined
                            }
                            title="Open transcript"
                            data-testid={"delivery-run-" + d.id}
                            className="font-mono text-[11px] text-accent hover:underline"
                          >
                            {d.run_id}
                          </Link>
                        )}
                      </div>
                      {d.detail && <p className="mt-0.5 text-[11px] text-muted">{d.detail}</p>}
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </div>
      )}
    </li>
  );
}

// ---------------------------------------------------------------------------
// Connection-bound row (design D2): surfaced read-in, managed in Integrations
// ---------------------------------------------------------------------------

function ConnectionRow({ row }: { row: ApiConnectionWebhookRow }) {
  const wh = row.webhook;
  return (
    <li
      className="rounded-md border border-dashed border-line bg-[color-mix(in_oklab,var(--fg)_2%,transparent)] px-4 py-3"
      data-od-id={"webhook-connection-row-" + row.connection_id}
      data-testid={"webhook-connection-row-" + row.connection_id}
    >
      <div className="flex flex-wrap items-center gap-2">
        <Chip>
          <span data-testid={"webhook-connection-flag-" + row.connection_id}>Connection webhook</span>
        </Chip>
        <p className="text-[13px] font-medium text-fg" data-testid={"webhook-connection-service-" + row.connection_id}>
          {row.service}
        </p>
        {wh.enabled ? <Chip>Active</Chip> : <Chip>Paused</Chip>}
        {wh.secret_hint ? (
          <span className="inline-flex items-center gap-1 font-mono text-[11px] text-muted">
            <Icon name="lock" size={10} />
            ····{wh.secret_hint}
          </span>
        ) : null}
      </div>
      <p className="mt-1 text-[11px] text-muted">
        Read-in from the {row.service} connection — rotate, pause, and event selection live in Integrations.
      </p>
      <Link
        to="/settings/integrations"
        data-testid={"link-manage-in-integrations-" + row.connection_id}
        className="mt-1.5 inline-flex items-center gap-1 text-[12px] font-medium text-accent hover:underline"
      >
        Manage in Integrations
        <Icon name="chevright" size={11} />
      </Link>
    </li>
  );
}

// ---------------------------------------------------------------------------
// Create / edit modal — structured labeled fields only
// ---------------------------------------------------------------------------

interface WebhookEditorModalProps {
  tenant: any;
  mode: "create" | "edit";
  webhook: ApiWorkspaceWebhook | null;
  ingestUrl: string;
  agents: ApiAgent[];
  channels: ApiChannel[];
  onClose: () => void;
  onToast: (text: string, kind?: string) => void;
  onCreated: (webhook: ApiWorkspaceWebhook, ingestUrl: string, secret?: string) => void;
  onUpdated: (webhook: ApiWorkspaceWebhook, ingestUrl: string) => void;
}

function WebhookEditorModal({
  tenant,
  mode,
  webhook,
  ingestUrl,
  agents,
  channels,
  onClose,
  onToast,
  onCreated,
  onUpdated,
}: WebhookEditorModalProps) {
  const ws = tenant?.sub || tenant?.id;
  const editing = mode === "edit" && webhook !== null;

  const [name, setName] = useState(webhook?.name || "");
  const [scheme, setScheme] = useState<WebhookScheme>(webhook?.scheme || "plain_token");
  const [secret, setSecret] = useState("");
  const [eventKind, setEventKind] = useState<ApiWebhookEventSource["kind"]>(
    webhook?.event_source?.kind || "header"
  );
  const [eventHeader, setEventHeader] = useState(webhook?.event_source?.name || "");
  const [eventPath, setEventPath] = useState(webhook?.event_source?.path || "");
  const [eventValue, setEventValue] = useState(webhook?.event_source?.value || "");
  const [deliveryIdSource, setDeliveryIdSource] = useState(webhook?.delivery_id_source || "");
  const [filterExpression, setFilterExpression] = useState(webhook?.filter_expression || "");
  const [filterError, setFilterError] = useState("");
  const [turnTemplate, setTurnTemplate] = useState(webhook?.turn_template || "");
  const [samplePayload, setSamplePayload] = useState(webhook?.sample_payload || "");
  const [agentId, setAgentId] = useState(webhook?.target_agent_id || "");
  const [targetKind, setTargetKind] = useState<"thread" | "channel">(
    webhook?.target_kind === "channel" ? "channel" : "thread"
  );
  const [targetId, setTargetId] = useState(webhook?.target_id || "");
  const [sessions, setSessions] = useState<ApiAgentSession[]>([]);
  const [sessionsError, setSessionsError] = useState<string | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);

  // Per-agent threads: refetched whenever the bound agent changes.
  useEffect(() => {
    if (targetKind !== "thread" || !agentId) return;
    const agent = agents.find((a) => a.id === agentId);
    if (!agent) return;
    let mounted = true;
    listAgentSessions(ws, agent.slug)
      .then((res) => {
        if (!mounted) return;
        setSessions(res?.sessions ?? []);
        setSessionsError(null);
      })
      .catch(() => {
        if (mounted) setSessionsError("Threads couldn't be loaded — reopen this dialog to retry.");
      });
    return () => {
      mounted = false;
    };
  }, [agentId, targetKind, agents, ws]);

  // A different agent has different threads — drop the stale thread target
  // unless it is the bound one coming back around.
  const handleAgentChange = (id: string) => {
    setAgentId(id);
    if (targetKind === "thread" && id !== webhook?.target_agent_id) setTargetId("");
  };

  const switchKind = (kind: "thread" | "channel") => {
    setTargetKind(kind);
    setTargetId(kind === webhook?.target_kind ? (webhook?.target_id ?? "") : "");
  };

  const validate = (): boolean => {
    const next: Record<string, string> = {};
    if (!name.trim()) next.name = "A name is required";
    if (!deliveryIdSource.trim()) next.deliveryIdSource = "A delivery ID header is required";
    if (eventKind === "header" && !eventHeader.trim()) next.eventSource = "A header name is required";
    if (eventKind === "payload_field" && !eventPath.trim()) next.eventSource = "A payload field path is required";
    if (eventKind === "fixed" && !eventValue.trim()) next.eventSource = "A fixed value is required";
    if (!agentId) next.agent = "Choose an agent";
    if (!targetId) next.target = targetKind === "channel" ? "Choose a channel" : "Choose a thread";
    setErrors(next);
    return Object.keys(next).length === 0;
  };

  const buildPayload = (): CreateWebhookPayload | UpdateWebhookPayload => {
    const event_source: ApiWebhookEventSource = { kind: eventKind };
    if (eventKind === "header") event_source.name = eventHeader.trim();
    if (eventKind === "payload_field") event_source.path = eventPath.trim();
    if (eventKind === "fixed") event_source.value = eventValue.trim();
    const base: UpdateWebhookPayload = {
      name: name.trim(),
      scheme,
      event_source,
      delivery_id_source: deliveryIdSource.trim(),
      target_agent_id: agentId,
      target_kind: targetKind,
      target_id: targetId,
      enabled: webhook?.enabled ?? true,
    };
    if (filterExpression.trim()) base.filter_expression = filterExpression.trim();
    if (turnTemplate.trim()) base.turn_template = turnTemplate;
    if (samplePayload.trim()) base.sample_payload = samplePayload;
    if (editing) return base;
    const payload: CreateWebhookPayload = { ...base };
    if (secret.trim()) payload.secret = secret.trim();
    return payload;
  };

  const handleSubmit = async () => {
    if (saving) return;
    setFilterError("");
    if (!validate()) return;
    setSaving(true);
    try {
      if (editing) {
        const res = await workspaceWebhooksApi.update(ws, webhook!.id, buildPayload() as UpdateWebhookPayload);
        onUpdated(res.webhook, res.ingest_url);
      } else {
        const res = await workspaceWebhooksApi.create(ws, buildPayload() as CreateWebhookPayload);
        onCreated(res.webhook, res.ingest_url, res.secret);
      }
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 400) {
        // The parser's message rides the 400 — render it verbatim on the field.
        setFilterError(err.message);
      } else {
        onToast(formatApiError(err, editing ? "Failed to save the webhook" : "Failed to create the webhook"), "danger");
      }
    } finally {
      setSaving(false);
    }
  };

  const cancelBtn = (
    <button
      type="button"
      onClick={onClose}
      data-testid="btn-webhook-cancel"
      className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
    >
      Cancel
    </button>
  );

  return (
    <Modal
      title={editing ? `Edit ${webhook!.name}` : "New webhook"}
      onClose={onClose}
      wide
      odId="modal-webhook"
      data-testid="modal-webhook"
      footer={
        <>
          {cancelBtn}
          <button
            type="button"
            onClick={() => void handleSubmit()}
            disabled={saving}
            data-testid={editing ? "btn-webhook-save" : "btn-webhook-create-confirm"}
            className="flex h-9 items-center gap-2 rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
          >
            {saving && <Icon name="loader" size={13} className="animate-spin" />}
            {saving ? "Saving…" : editing ? "Save changes" : "Create webhook"}
          </button>
        </>
      }
    >
      <div className="grid grid-cols-1 gap-x-4 gap-y-5 p-5 sm:grid-cols-2">
        <div>
          <label className={labelCls} htmlFor="wh-name">Webhook name</label>
          <input
            id="wh-name"
            className={inputCls}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="gitlab-mr-review"
            data-testid="input-webhook-name"
          />
          {errors.name && (
            <p className="mt-1 text-[11px] text-danger" data-testid="webhook-name-error">
              {errors.name}
            </p>
          )}
        </div>

        <div>
          <label className={labelCls} htmlFor="wh-scheme">Signature scheme</label>
          <select
            id="wh-scheme"
            className={inputCls}
            value={scheme}
            onChange={(e) => setScheme(e.target.value as WebhookScheme)}
            data-testid="select-webhook-scheme"
          >
            {SCHEMES.map((s) => (
              <option key={s.id} value={s.id}>{s.label}</option>
            ))}
          </select>
          <p className="mt-1 text-[11px] text-muted" data-testid="webhook-scheme-help">
            {SCHEMES.find((s) => s.id === scheme)?.help}
          </p>
        </div>

        {editing ? (
          <div>
            <p className={labelCls}>Secret</p>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
              {webhook!.secret_hint ? (
                <span
                  className="inline-flex items-center gap-1 font-mono text-[11px] text-muted"
                  title="Secret last 4 — never shown in full"
                  data-testid="webhook-edit-hint"
                >
                  <Icon name="lock" size={10} />
                  ····{webhook!.secret_hint}
                </span>
              ) : null}
              <p className="text-[11px] text-muted">Immutable — rotate from the list to replace it.</p>
            </div>
          </div>
        ) : (
          <div>
            <label className={labelCls} htmlFor="wh-secret">Secret (optional)</label>
            <input
              id="wh-secret"
              type="password"
              autoComplete="off"
              className={monoInputCls}
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              placeholder="Generated for you when left empty"
              data-testid="input-webhook-secret"
            />
            <p className="mt-1 text-[11px] text-muted">
              Leave empty and one is generated — shown once after saving. Paste a secret you already chose
              in the sender's dashboard to bring your own; it is never displayed again.
            </p>
          </div>
        )}

        <div>
          <label className={labelCls} htmlFor="wh-delivery-id">Delivery ID header</label>
          <input
            id="wh-delivery-id"
            className={monoInputCls}
            value={deliveryIdSource}
            onChange={(e) => setDeliveryIdSource(e.target.value)}
            placeholder="X-Gitlab-Delivery"
            data-testid="input-webhook-delivery-id"
          />
          <p className="mt-1 text-[11px] text-muted">
            Header carrying the sender's delivery id — the same id is acknowledged once, provider retries
            never trigger a second run.
          </p>
          {errors.deliveryIdSource && (
            <p className="mt-1 text-[11px] text-danger" data-testid="webhook-delivery-id-error">
              {errors.deliveryIdSource}
            </p>
          )}
        </div>

        <div className="sm:col-span-2" data-testid="webhooks-event-source">
          <p className={labelCls}>Event source</p>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-[200px_1fr]">
            <select
              aria-label="Event source kind"
              className={inputCls}
              value={eventKind}
              onChange={(e) => setEventKind(e.target.value as ApiWebhookEventSource["kind"])}
              data-testid="select-webhook-event-source-kind"
            >
              {EVENT_SOURCE_KINDS.map((k) => (
                <option key={k.id} value={k.id}>{k.label}</option>
              ))}
            </select>
            {eventKind === "header" && (
              <input
                aria-label="Event header name"
                className={monoInputCls}
                value={eventHeader}
                onChange={(e) => setEventHeader(e.target.value)}
                placeholder="X-Gitlab-Event"
                data-testid="input-webhook-event-header"
              />
            )}
            {eventKind === "payload_field" && (
              <input
                aria-label="Event payload field path"
                className={monoInputCls}
                value={eventPath}
                onChange={(e) => setEventPath(e.target.value)}
                placeholder="object_kind"
                data-testid="input-webhook-event-path"
              />
            )}
            {eventKind === "fixed" && (
              <input
                aria-label="Fixed event value"
                className={monoInputCls}
                value={eventValue}
                onChange={(e) => setEventValue(e.target.value)}
                placeholder="merge_request"
                data-testid="input-webhook-event-value"
              />
            )}
          </div>
          {errors.eventSource && (
            <p className="mt-1 text-[11px] text-danger" data-testid="webhook-event-source-error">
              {errors.eventSource}
            </p>
          )}
        </div>

        <div className="sm:col-span-2">
          <label className={labelCls} htmlFor="wh-filter">Filter expression (optional)</label>
          <input
            id="wh-filter"
            className={monoInputCls}
            value={filterExpression}
            onChange={(e) => {
              setFilterExpression(e.target.value);
              if (filterError) setFilterError("");
            }}
            placeholder={'event == "merge_request" && project.path_with_namespace == "team/api"'}
            data-testid="input-webhook-filter"
          />
          {filterError ? (
            <p className="mt-1 text-[11px] text-danger" data-testid="webhook-filter-error" role="alert">
              {filterError}
            </p>
          ) : (
            <p className="mt-1 text-[11px] text-muted">
              Comparisons, boolean logic, and field access over the payload and the resolved event — validated
              when you save. Non-matching deliveries are acknowledged without a run.
            </p>
          )}
        </div>

        <div className="grid grid-cols-1 gap-x-4 gap-y-5 sm:col-span-2 sm:grid-cols-2">
          <div>
            <label className={labelCls} htmlFor="wh-template">Turn template (optional)</label>
            <textarea
              id="wh-template"
              rows={3}
              className={monoAreaCls}
              value={turnTemplate}
              onChange={(e) => setTurnTemplate(e.target.value)}
              placeholder="MR {project.path_with_namespace}: {object_attributes.title}"
              data-testid="input-webhook-template"
            />
            <p className="mt-1 text-[11px] text-muted">
              Placeholders are validated against the sample payload at save; a delivery missing a field is
              dropped rather than half-filled. Empty uses the labeled payload as-is.
            </p>
          </div>
          <div>
            <label className={labelCls} htmlFor="wh-sample">Sample payload (optional)</label>
            <textarea
              id="wh-sample"
              rows={3}
              className={monoAreaCls}
              value={samplePayload}
              onChange={(e) => setSamplePayload(e.target.value)}
              placeholder={"{\n  \"object_kind\": \"merge_request\",\n  \"project\": { \"path_with_namespace\": \"team/api\" }\n}"}
              data-testid="input-webhook-sample"
            />
          </div>
        </div>

        <div className="sm:col-span-2" data-testid="webhooks-target">
          <p className={labelCls}>Delivery target</p>
          <div className="grid grid-cols-1 gap-x-4 gap-y-3 sm:grid-cols-2">
            <div>
              <label className={labelCls} htmlFor="wh-agent">Agent</label>
              <select
                id="wh-agent"
                className={inputCls}
                value={agentId}
                onChange={(e) => handleAgentChange(e.target.value)}
                data-testid="select-webhook-agent"
              >
                {!agentId && <option value="">Choose an agent</option>}
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>{a.name}</option>
                ))}
              </select>
              {errors.agent && (
                <p className="mt-1 text-[11px] text-danger" data-testid="webhook-agent-error">
                  {errors.agent}
                </p>
              )}
            </div>
            <div className="space-y-3">
              <div>
                <span className={labelCls}>Deliver to</span>
                <div className="flex items-center gap-5">
                  <label className="flex cursor-pointer items-center gap-1.5 text-[13px] text-fg2">
                    <input
                      type="radio"
                      name="wh-target-kind"
                      checked={targetKind === "channel"}
                      onChange={() => switchKind("channel")}
                      data-testid="radio-webhook-channel"
                    />
                    Channel
                  </label>
                  <label className="flex cursor-pointer items-center gap-1.5 text-[13px] text-fg2">
                    <input
                      type="radio"
                      name="wh-target-kind"
                      checked={targetKind === "thread"}
                      onChange={() => switchKind("thread")}
                      data-testid="radio-webhook-thread"
                    />
                    Thread
                  </label>
                </div>
              </div>
              {targetKind === "channel" ? (
                <div>
                  <label className={labelCls} htmlFor="wh-channel">Channel</label>
                  <select
                    id="wh-channel"
                    className={inputCls}
                    value={targetId}
                    onChange={(e) => setTargetId(e.target.value)}
                    data-testid="select-webhook-channel"
                  >
                    {!targetId && <option value="">Choose a channel</option>}
                    {channels.map((c) => (
                      <option key={c.id} value={c.id}>{`#${c.slug}`}</option>
                    ))}
                  </select>
                </div>
              ) : (
                <div>
                  <label className={labelCls} htmlFor="wh-thread">Thread</label>
                  {sessionsError ? (
                    <p className="text-[12px] leading-4 text-muted">{sessionsError}</p>
                  ) : (
                    <select
                      id="wh-thread"
                      className={inputCls}
                      value={targetId}
                      onChange={(e) => setTargetId(e.target.value)}
                      data-testid="select-webhook-thread"
                    >
                      {!targetId && <option value="">Choose a thread</option>}
                      {sessions.map((s) => (
                        <option key={s.session_id} value={s.session_id}>{s.title || s.session_id}</option>
                      ))}
                    </select>
                  )}
                </div>
              )}
            </div>
            {errors.target && (
              <p className="text-[11px] text-danger sm:col-span-2" data-testid="webhook-target-error">
                {errors.target}
              </p>
            )}
          </div>
        </div>

        <p className="text-[11px] leading-4 text-muted sm:col-span-2">
          Sender setup: POST JSON to the ingest URL and sign with{" "}
          <code className="font-mono text-fg2">{signatureHeaderFor(scheme)}</code>. The URL appears in the
          list once the endpoint is created.
        </p>
      </div>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Reveal-once secret modal (create + rotate) — same banner contract as the
// connection webhooks dialog
// ---------------------------------------------------------------------------

function SecretRevealModal({
  webhook,
  secret,
  ingestUrl,
  onClose,
  onToast,
}: {
  webhook: ApiWorkspaceWebhook;
  secret: string;
  ingestUrl: string;
  onClose: () => void;
  onToast: (text: string, kind?: string) => void;
}) {
  const [copied, setCopied] = useState(false);
  const [copiedUrl, setCopiedUrl] = useState(false);

  const copySecret = async () => {
    try {
      await navigator.clipboard.writeText(secret);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
      onToast("Secret copied to clipboard");
    } catch {
      onToast("Couldn't copy — select the secret manually", "danger");
    }
  };

  const copyUrl = async () => {
    try {
      await navigator.clipboard.writeText(ingestUrl);
      setCopiedUrl(true);
      window.setTimeout(() => setCopiedUrl(false), 1500);
      onToast("Ingest URL copied to clipboard");
    } catch {
      onToast("Couldn't copy — select the URL manually", "danger");
    }
  };

  return (
    <Modal
      title={`${webhook.name} secret`}
      onClose={onClose}
      odId="modal-webhook-secret"
      data-testid="modal-webhook-secret"
      footer={
        <button
          type="button"
          onClick={onClose}
          data-testid="btn-webhook-reveal-done"
          className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90"
        >
          Done
        </button>
      }
    >
      <div className="space-y-4 p-5">
        <div
          data-testid="webhooks-secret-reveal"
          className="rounded-md border border-[color-mix(in_oklab,var(--accent)_35%,transparent)] bg-[color-mix(in_oklab,var(--accent)_8%,transparent)] p-3"
        >
          <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-fg2">
            SECRET — COPY NOW, SHOWN ONLY ONCE
          </p>
          <code
            data-testid="webhooks-secret-value"
            className="mt-1.5 block break-all font-mono text-[12px] leading-4 text-fg"
          >
            {secret}
          </code>
          <div className="mt-2 flex items-center gap-2.5">
            <button
              type="button"
              onClick={() => void copySecret()}
              data-testid="btn-copy-secret"
              className="flex h-7 items-center gap-1.5 rounded-md border border-line bg-surface px-2.5 text-[11px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
            >
              <Icon name={copied ? "check" : "copy"} size={11} />
              Copy
            </button>
            <p className="text-[11px] leading-4 text-muted">
              Configure it as the sender's {signatureHeaderFor(webhook.scheme)} credential.
            </p>
          </div>
        </div>

        <div>
          <p className="mb-1 text-[11px] font-medium text-fg2">Ingest URL</p>
          <div className="flex items-center gap-2">
            <code
              title={ingestUrl}
              className="min-w-0 flex-1 truncate rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-2.5 py-1.5 font-mono text-[11px] text-fg2"
            >
              {ingestUrl}
            </code>
            <button
              type="button"
              onClick={() => void copyUrl()}
              aria-label="Copy ingest URL"
              data-testid="btn-copy-reveal-ingest-url"
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-line text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
            >
              <Icon name={copiedUrl ? "check" : "copy"} size={13} />
            </button>
          </div>
          <p className="mt-2 text-[11px] leading-4 text-muted">
            Point the sender at this URL. Later you'll only see the secret's last 4 — rotating replaces it
            and invalidates the old one immediately.
          </p>
        </div>
      </div>
    </Modal>
  );
}
