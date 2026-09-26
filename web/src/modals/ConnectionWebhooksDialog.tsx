import { useEffect, useState } from "react";
import { Modal } from "../components/ui/Modal";
import { Icon } from "../components/ui/Icon";
import { inputCls, labelCls } from "../components/ui/constants";
import {
  connectionServiceName,
  connectionsApi,
  type ApiConnection,
  type ApiConnectionWebhookView,
  type ApiIntegrationRecipe,
} from "../lib/connectionsApi";
import {
  api,
  formatApiError,
  listAgentSessions,
  type ApiAgent,
  type ApiAgentSession,
  type ApiChannel,
} from "../lib/api";

export interface ConnectionWebhooksDialogProps {
  tenant: any;
  connection: ApiConnection;
  /** The workspace recipe registry — resolves display name and webhook catalog. */
  recipes: ApiIntegrationRecipe[];
  onClose: () => void;
  onToast?: (text: string, kind?: string) => void;
  /** Fired after a fully successful enable/save/rotate/disable — the gallery re-reads the list. */
  onSaved?: () => void;
}

function SectionLabel({ children }: { children: any }) {
  return (
    <p className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-muted">
      {children}
    </p>
  );
}

/**
 * Connection webhooks surface (add-connection-webhooks 4.2/4.3): enable,
 * retarget, re-event, rotate, and disable a service's inbound webhook. The
 * secret is write-only — enable and rotate reveal it exactly once through the
 * banner; reads carry only the last-4 `secret_hint` (same shape as the token
 * hint on the cards). The thread target binds `session_id` (the agent-facing
 * handle), not the session row id. Saves diff against the loaded view and
 * apply target first, then events; every failure keeps the dialog open with
 * an inline alert — nothing closes on error.
 */
export function ConnectionWebhooksDialog({
  tenant,
  connection,
  recipes,
  onClose,
  onToast = () => {},
  onSaved,
}: ConnectionWebhooksDialogProps) {
  const ws = tenant?.sub || tenant?.id;
  const name = connectionServiceName(connection, recipes);
  const recipe = recipes.find((r) => r.id === connection.service);
  const hooks = recipe?.webhooks;

  // Seed from the row the dialog was opened with — the fresh read below
  // corrects it; a failed read leaves this honest fallback in place.
  const initial = connection.webhook ?? null;
  const [webhook, setWebhook] = useState<ApiConnectionWebhookView | null>(initial);
  const [agentId, setAgentId] = useState(initial?.target?.agent_id ?? '');
  const [targetKind, setTargetKind] = useState<'channel' | 'thread'>(
    initial?.target?.target_kind === 'channel' ? 'channel' : 'thread'
  );
  const [targetId, setTargetId] = useState(initial?.target?.target_id ?? '');
  // Checked set: the live event selection when enabled, else the recipe's
  // recommended defaults — a fresh enable starts from those.
  const [events, setEvents] = useState<Set<string>>(() =>
    new Set(initial?.enabled ? (initial.events ?? []) : (hooks?.default_events ?? []))
  );
  // Reveal-once secret from enable/rotate — local state only, never re-read.
  const [secret, setSecret] = useState<string | null>(null);
  const [copiedUrl, setCopiedUrl] = useState(false);
  const [copiedSecret, setCopiedSecret] = useState(false);
  const [agents, setAgents] = useState<ApiAgent[]>([]);
  const [agentsLoaded, setAgentsLoaded] = useState(false);
  const [agentsError, setAgentsError] = useState<string | null>(null);
  const [channels, setChannels] = useState<ApiChannel[]>([]);
  const [channelsLoaded, setChannelsLoaded] = useState(false);
  const [channelsError, setChannelsError] = useState<string | null>(null);
  const [sessions, setSessions] = useState<ApiAgentSession[]>([]);
  const [sessionsLoaded, setSessionsLoaded] = useState(false);
  const [sessionsError, setSessionsError] = useState<string | null>(null);
  const [enabling, setEnabling] = useState(false);
  const [saving, setSaving] = useState(false);
  const [disabling, setDisabling] = useState(false);
  const [rotating, setRotating] = useState(false);
  const [confirmingDisable, setConfirmingDisable] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const busy = enabling || saving || disabling || rotating;

  // The target pickers need the whole registry up front; a failed load keeps
  // the primary action parked instead of enabling against a partial list.
  useEffect(() => {
    let mounted = true;
    api.agents
      .list(ws)
      .then((res) => {
        if (!mounted) return;
        setAgents(res?.agents ?? []);
        setAgentsLoaded(true);
      })
      .catch((err: unknown) => {
        if (mounted) setAgentsError(formatApiError(err, "Couldn't load the workspace agents"));
      });
    api.channels
      .list(ws)
      .then((res) => {
        if (!mounted) return;
        setChannels(res?.channels ?? []);
        setChannelsLoaded(true);
      })
      .catch((err: unknown) => {
        if (mounted) setChannelsError(formatApiError(err, "Couldn't load the workspace channels"));
      });
    return () => {
      mounted = false;
    };
  }, [ws]);

  // Fresh webhook state on mount — the gallery row may be stale. A failed
  // read keeps the seed (the row the dialog was opened with).
  useEffect(() => {
    if (!hooks) return;
    let mounted = true;
    const adopt = (wh: ApiConnectionWebhookView | null) => {
      setWebhook(wh);
      setEvents(new Set(wh?.enabled ? (wh.events ?? []) : (hooks.default_events ?? [])));
      if (wh?.target?.agent_id) {
        setAgentId(wh.target.agent_id);
        setTargetKind(wh.target.target_kind === 'channel' ? 'channel' : 'thread');
        setTargetId(wh.target.target_id ?? '');
      }
    };
    connectionsApi.webhook
      .get(ws, connection.id)
      .then((res) => {
        if (mounted) adopt(res?.webhook ?? null);
      })
      .catch(() => {
        if (mounted) adopt(connection.webhook ?? null);
      });
    return () => {
      mounted = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- one-shot load per opened connection
  }, [connection.id, ws]);

  // Per-agent threads: refetched whenever the bound agent changes.
  useEffect(() => {
    if (targetKind !== 'thread' || !agentId) return;
    const agent = agents.find((a) => a.id === agentId);
    if (!agent) return;
    let mounted = true;
    listAgentSessions(ws, agent.slug)
      .then((res) => {
        if (!mounted) return;
        setSessions(res?.sessions ?? []);
        setSessionsLoaded(true);
        setSessionsError(null);
      })
      .catch((err: unknown) => {
        if (mounted) setSessionsError(formatApiError(err, "Couldn't load the agent's threads"));
      });
    return () => {
      mounted = false;
    };
  }, [agentId, targetKind, agents, ws]);

  // The confirm affordance decays — a stray first click never arms disable forever.
  useEffect(() => {
    if (!confirmingDisable) return;
    const t = window.setTimeout(() => setConfirmingDisable(false), 3000);
    return () => window.clearTimeout(t);
  }, [confirmingDisable]);

  // A different agent has different threads — drop the stale thread target
  // unless it is the bound one coming back around.
  const handleAgentChange = (id: string) => {
    setAgentId(id);
    if (targetKind === 'thread' && id !== webhook?.target?.agent_id) setTargetId('');
  };

  // Toggling back to the bound kind restores its target; anything else starts clean.
  const switchKind = (kind: 'channel' | 'thread') => {
    setTargetKind(kind);
    setTargetId(kind === webhook?.target?.target_kind ? (webhook?.target?.target_id ?? '') : '');
  };

  const copyIngestUrl = async () => {
    const url = webhook?.ingest_url;
    if (!url) return;
    try {
      await navigator.clipboard.writeText(url);
      setCopiedUrl(true);
      window.setTimeout(() => setCopiedUrl(false), 1500);
      onToast('Copied');
    } catch {
      onToast("Couldn't copy — select the URL manually", 'danger');
    }
  };

  const copySecret = async () => {
    if (!secret) return;
    try {
      await navigator.clipboard.writeText(secret);
      setCopiedSecret(true);
      window.setTimeout(() => setCopiedSecret(false), 1500);
      onToast('Copied');
    } catch {
      onToast("Couldn't copy — select the secret manually", 'danger');
    }
  };

  // Server 400s unless agent + target + >=1 event — the button is gated to
  // match, so this is a straight pass-through of the current form.
  const handleEnable = async () => {
    if (busy || !agentId || !targetId || events.size === 0) return;
    setEnabling(true);
    setError(null);
    try {
      const res = await connectionsApi.webhook.enable(ws, connection.id, {
        agent_id: agentId,
        target_kind: targetKind,
        target_id: targetId,
        events: [...events],
      });
      setWebhook(res?.webhook ?? null);
      setSecret(res?.secret ?? null);
      onSaved?.();
    } catch (err: unknown) {
      setError(formatApiError(err, `Couldn't enable ${name} webhooks`));
    } finally {
      setEnabling(false);
    }
  };

  const handleSave = async () => {
    if (busy || !dirty) return;
    setSaving(true);
    setError(null);
    try {
      let view = webhook;
      if (targetChanged) {
        const res = await connectionsApi.webhook.updateTarget(ws, connection.id, {
          agent_id: agentId,
          target_kind: targetKind,
          target_id: targetId,
        });
        view = res?.webhook ?? view;
      }
      if (eventsChanged) {
        const res = await connectionsApi.webhook.updateEvents(ws, connection.id, {
          events: [...events],
        });
        view = res?.webhook ?? view;
      }
      setWebhook(view);
      if (view?.events) setEvents(new Set(view.events));
      onToast(`${name} webhooks updated`);
      onSaved?.();
    } catch (err: unknown) {
      setError(formatApiError(err, `Couldn't update ${name} webhooks`));
    } finally {
      setSaving(false);
    }
  };

  // Two-step confirm: the first click arms, the second fires. Stopping
  // ingestion preserves the binding and events server-side.
  const handleDisable = async () => {
    if (busy) return;
    if (!confirmingDisable) {
      setConfirmingDisable(true);
      return;
    }
    setDisabling(true);
    setError(null);
    try {
      const res = await connectionsApi.webhook.disable(ws, connection.id);
      setWebhook(res?.webhook ?? null);
      setSecret(null); // the revealed secret died with the binding
      onToast(`${name} webhooks disabled`);
      onSaved?.();
    } catch (err: unknown) {
      setError(formatApiError(err, `Couldn't disable ${name} webhooks`));
    } finally {
      setDisabling(false);
      setConfirmingDisable(false);
    }
  };

  // Rotation invalidates the prior secret immediately — the new one is
  // revealed once through the same banner as enable.
  const handleRotate = async () => {
    if (busy) return;
    setRotating(true);
    setError(null);
    try {
      const res = await connectionsApi.webhook.rotate(ws, connection.id);
      setWebhook(res?.webhook ?? null);
      setSecret(res?.secret ?? null);
      onSaved?.();
    } catch (err: unknown) {
      setError(formatApiError(err, `Couldn't rotate the ${name} webhook secret`));
    } finally {
      setRotating(false);
    }
  };

  // Diff against the loaded view, not the captured form start — honest if the
  // fresh read corrects the seed while the dialog is open.
  const boundTarget = webhook?.target;
  const boundAgentName = boundTarget
    ? agents.find((a) => a.id === boundTarget.agent_id)?.name || boundTarget.agent_id
    : '';
  const boundTargetLabel = !boundTarget
    ? ''
    : boundTarget.target_kind === 'channel'
      ? channels.find((c) => c.id === boundTarget.target_id)?.name || boundTarget.target_id
      : sessions.find((s) => s.session_id === boundTarget.target_id)?.title || boundTarget.target_id;
  const targetChanged =
    !!boundTarget &&
    (agentId !== boundTarget.agent_id ||
      targetKind !== boundTarget.target_kind ||
      targetId !== boundTarget.target_id);
  const loadedEvents = webhook?.events ?? [];
  const eventsChanged =
    webhook?.enabled === true &&
    (events.size !== loadedEvents.length || [...events].some((e) => !loadedEvents.includes(e)));
  const dirty = targetChanged || eventsChanged;

  // The delivering kind's list must have resolved — enabling against an empty
  // or failed pick list would bind a target the select never offered.
  const targetListReady = targetKind === 'channel' ? channelsLoaded : sessionsLoaded;
  const targetListError = targetKind === 'channel' ? channelsError : sessionsError;
  const canEnable =
    agentsLoaded && !agentsError && !!agentId && targetListReady && !targetListError && !!targetId && events.size > 0;

  // Recipe-gated upstream: the opener only appears for recipes declaring
  // webhooks, so this is a guard, not a designed state.
  if (!hooks) {
    return (
      <Modal
        title={`${name} webhooks`}
        onClose={onClose}
        odId="modal-connection-webhooks"
        data-testid="modal-connection-webhooks"
        footer={
          <button
            type="button"
            onClick={onClose}
            data-testid="btn-webhook-cancel"
            className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
        }
      >
        <div className="space-y-5 p-5">
          <p className="text-[12px] leading-4 text-muted">
            This service doesn't declare webhook support.
          </p>
        </div>
      </Modal>
    );
  }

  const cancelButton = (
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
      title={`${name} webhooks`}
      onClose={onClose}
      odId="modal-connection-webhooks"
      data-testid="modal-connection-webhooks"
      footer={
        webhook?.enabled ? (
          <>
            <button
              type="button"
              onClick={() => void handleDisable()}
              disabled={busy}
              data-testid="btn-webhook-disable"
              className="mr-auto flex h-9 items-center rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] px-3.5 text-[13px] font-medium text-danger transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] disabled:opacity-40"
            >
              {disabling ? 'Disabling…' : confirmingDisable ? 'Confirm disable' : 'Disable webhooks'}
            </button>
            {cancelButton}
            <button
              type="button"
              onClick={() => void handleSave()}
              disabled={!dirty || busy}
              data-testid="btn-webhook-save"
              className="flex h-9 items-center gap-2 rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
            >
              {saving && <Icon name="loader" size={13} className="animate-spin" />}
              {saving ? 'Saving…' : 'Save changes'}
            </button>
          </>
        ) : (
          <>
            {cancelButton}
            <button
              type="button"
              onClick={() => void handleEnable()}
              disabled={!canEnable || busy}
              data-testid="btn-webhook-enable"
              className="flex h-9 items-center gap-2 rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
            >
              {enabling && <Icon name="loader" size={13} className="animate-spin" />}
              {enabling ? 'Enabling…' : 'Enable webhooks'}
            </button>
          </>
        )
      }
    >
      <div className="space-y-5 p-5">
        <div data-testid="webhooks-status">
          {webhook?.enabled ? (
            <p className="text-[12px] leading-4 text-fg2">
              Enabled — {name} events become turns for {boundAgentName} in {boundTargetLabel}.
            </p>
          ) : (
            <p className="text-[12px] leading-4 text-muted">
              Disabled — {name} events are not ingested.
            </p>
          )}
        </div>

        <div data-testid="webhooks-target" className="space-y-3">
          <SectionLabel>Delivery target</SectionLabel>
          <div>
            <label className={labelCls} htmlFor="wh-agent">Agent</label>
            <select
              id="wh-agent"
              className={inputCls}
              value={agentId}
              onChange={(e) => handleAgentChange(e.target.value)}
              disabled={busy}
              data-testid="select-webhook-agent"
            >
              {!agentId && <option value="">Choose an agent</option>}
              {agents.map((a) => (
                <option key={a.id} value={a.id}>{a.name}</option>
              ))}
            </select>
          </div>
          <div>
            <span className={labelCls}>Deliver</span>
            <div className="flex items-center gap-5">
              <label className="flex cursor-pointer items-center gap-1.5 text-[13px] text-fg2">
                <input
                  type="radio"
                  name="wh-target-kind"
                  checked={targetKind === 'channel'}
                  onChange={() => switchKind('channel')}
                  disabled={busy}
                  data-testid="radio-webhook-channel"
                />
                Channel
              </label>
              <label className="flex cursor-pointer items-center gap-1.5 text-[13px] text-fg2">
                <input
                  type="radio"
                  name="wh-target-kind"
                  checked={targetKind === 'thread'}
                  onChange={() => switchKind('thread')}
                  disabled={busy}
                  data-testid="radio-webhook-thread"
                />
                Thread
              </label>
            </div>
          </div>
          {targetKind === 'channel' ? (
            <div>
              <label className={labelCls} htmlFor="wh-channel">Channel</label>
              <select
                id="wh-channel"
                className={inputCls}
                value={targetId}
                onChange={(e) => setTargetId(e.target.value)}
                disabled={busy}
                data-testid="select-webhook-channel"
              >
                {!targetId && <option value="">Choose a channel</option>}
                {channels.map((c) => (
                  <option key={c.id} value={c.id}>{`#${c.slug}`}</option>
                ))}
              </select>
              {channelsError && (
                <p className="mt-1.5 text-[11px] leading-4 text-muted">
                  Channels couldn't be loaded — reopen this dialog to retry.
                </p>
              )}
            </div>
          ) : (
            <div>
              <label className={labelCls} htmlFor="wh-thread">Thread</label>
              {sessionsError ? (
                <p className="text-[12px] leading-4 text-muted">
                  Threads couldn't be loaded — reopen this dialog to retry.
                </p>
              ) : (
                <select
                  id="wh-thread"
                  className={inputCls}
                  value={targetId}
                  onChange={(e) => setTargetId(e.target.value)}
                  disabled={busy}
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
          {agentsError && (
            <p className="text-[12px] leading-4 text-muted">
              Agents couldn't be loaded — reopen this dialog to retry.
            </p>
          )}
        </div>

        <div data-testid="webhooks-events">
          <SectionLabel>Events</SectionLabel>
          <div className="grid grid-cols-2 gap-x-4 gap-y-1.5">
            {hooks.events.map((ev) => {
              const on = events.has(ev);
              return (
                <label key={ev} className="flex cursor-pointer items-center gap-2">
                  <input
                    type="checkbox"
                    checked={on}
                    disabled={busy}
                    onChange={() =>
                      setEvents((prev) => {
                        const next = new Set(prev);
                        if (next.has(ev)) {
                          next.delete(ev);
                        } else {
                          next.add(ev);
                        }
                        return next;
                      })
                    }
                    data-testid={`webhook-event-${ev}`}
                    className="h-3.5 w-3.5"
                  />
                  <span className="font-mono text-[11px] text-fg2">{ev}</span>
                </label>
              );
            })}
          </div>
        </div>

        <div data-testid="webhooks-setup">
          <SectionLabel>Provider setup</SectionLabel>
          <p className="text-[12px] leading-4 text-muted">{hooks.setup.help}</p>
          <div className="mt-3 space-y-3">
            <div>
              <p className="mb-1 text-[11px] font-medium text-fg2">Payload URL</p>
              {webhook?.ingest_url ? (
                <div className="flex items-center gap-2">
                  <code
                    title={webhook.ingest_url}
                    className="min-w-0 flex-1 truncate rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-2.5 py-1.5 font-mono text-[11px] text-fg2"
                  >
                    {webhook.ingest_url}
                  </code>
                  <button
                    type="button"
                    onClick={() => void copyIngestUrl()}
                    aria-label="Copy payload URL"
                    data-testid="btn-copy-ingest-url"
                    className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-line text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
                  >
                    <Icon name={copiedUrl ? 'check' : 'copy'} size={13} />
                  </button>
                </div>
              ) : (
                <p className="text-[11px] leading-4 text-muted">
                  <span className="font-mono">{hooks.setup.url_path_shape}</span> — the workspace
                  URL appears after load.
                </p>
              )}
            </div>
            <div>
              <p className="mb-1 text-[11px] font-medium text-fg2">Secret</p>
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
                {webhook?.enabled && webhook.secret_hint ? (
                  <span
                    className="inline-flex items-center gap-1 font-mono text-[11px] text-muted"
                    title="Secret last 4 — never shown in full"
                  >
                    <Icon name="lock" size={10} />
                    ····{webhook.secret_hint}
                  </span>
                ) : (
                  <p className="text-[11px] leading-4 text-muted">
                    Generated when you enable — shown once.
                  </p>
                )}
                {webhook?.enabled && (
                  <button
                    type="button"
                    onClick={() => void handleRotate()}
                    disabled={busy}
                    data-testid="btn-webhook-rotate"
                    className="flex h-7 items-center gap-1.5 rounded-md border border-line px-2.5 text-[11px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg disabled:opacity-50"
                  >
                    <Icon name="refresh" size={11} className={rotating ? 'animate-spin' : ''} />
                    {rotating ? 'Rotating…' : 'Rotate secret'}
                  </button>
                )}
              </div>
            </div>
          </div>
          <p className="mt-2.5 text-[11px] leading-4 text-muted">
            {name} sends{' '}
            <code className="font-mono text-fg2">{hooks.setup.signature_header}</code> ·{' '}
            <code className="font-mono text-fg2">{hooks.setup.event_type_header}</code> ·{' '}
            <code className="font-mono text-fg2">{hooks.setup.delivery_id_header}</code>
          </p>
        </div>

        {webhook?.enabled && (
          <div data-testid="webhooks-last-error">
            {webhook.last_error ? (
              <p
                className="truncate text-[11px] text-danger"
                title={`${webhook.last_error.event} · ${webhook.last_error.error} · ${webhook.last_error.at}`}
              >
                {webhook.last_error.event} · {webhook.last_error.error} · {webhook.last_error.at}
              </p>
            ) : (
              <p className="text-[11px] text-muted">Last error none</p>
            )}
          </div>
        )}

        {secret && (
          <div
            data-testid="webhooks-secret-reveal"
            className="rounded-md border border-[color-mix(in_oklab,var(--accent)_35%,transparent)] bg-[color-mix(in_oklab,var(--accent)_8%,transparent)] p-3"
          >
            <div className="flex items-start justify-between gap-2">
              <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-fg2">
                SECRET — COPY NOW, SHOWN ONLY ONCE
              </p>
              <button
                type="button"
                onClick={() => setSecret(null)}
                aria-label="Dismiss"
                className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg"
              >
                <Icon name="x" size={12} />
              </button>
            </div>
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
                <Icon name={copiedSecret ? 'check' : 'copy'} size={11} />
                Copy
              </button>
              <p className="text-[11px] leading-4 text-muted">
                Paste it into the {name} webhook's Secret field.
              </p>
            </div>
          </div>
        )}

        {error && (
          <p
            role="alert"
            data-testid="webhooks-error"
            className="flex items-start gap-1.5 rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] px-3 py-2 text-[12px] leading-4 text-danger"
          >
            <Icon name="alert" size={13} className="mt-0.5 shrink-0" />
            {error}
          </p>
        )}
      </div>
    </Modal>
  );
}
