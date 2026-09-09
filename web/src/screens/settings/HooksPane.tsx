import { useEffect, useState } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../../components/ui/Icon";
import { Toggle } from "../../components/ui/Toggle";
import { Chip } from "../../components/ui/Chip";
import { Modal } from "../../components/ui/Modal";
import { ErrorState } from "../../components/ErrorState";
import {
  api,
  ApiError,
  formatApiError,
  type ApiHook,
  type ApiHookExecution,
  type ApiHookPayload,
  type ApiHookSaveResult,
} from "../../lib/api";
import { useCanWriteTools } from "../../lib/tools";
import {
  hookEventMeta,
  hookHandlerLabel,
  hookStatusView,
  matcherSummary,
} from "../../lib/hooksUi";
import { HookDialog } from "../../modals/HookDialog";
import serverErrorSvg from "../../assets/server-error.svg";

export interface HooksPaneProps {
  tenant: any;
  /** Unused since the pane went API-backed; kept for SettingsPage compat. */
  onUpdate?: (fn: any) => void;
  onToast?: (text: string, kind?: string) => void;
  /** Override the derived hooks.write check (tests). hooks.write is granted
   * to the same built-in roles as tools.write, so the check is shared. */
  canWrite?: boolean;
}

interface DialogState {
  mode: 'add' | 'edit';
  hook?: ApiHook;
}

function formatTime(raw: string): string {
  const t = new Date(raw);
  if (!raw || Number.isNaN(t.getTime())) return raw || '';
  return t.toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });
}

function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '0 ms';
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(1).replace(/\.0$/, '')} s`;
}

// Decision badge for execution rows: block red, failure warn, allow green.
function DecisionBadge({ decision }: { decision: string }) {
  return (
    <span
      data-testid={'hook-decision-' + decision}
      className={cx(
        'shrink-0 rounded-full px-2 py-0.5 font-mono text-[10px] font-semibold',
        decision === 'block'
          ? 'bg-[color-mix(in_oklab,var(--danger)_14%,transparent)] text-danger'
          : decision === 'failure'
            ? 'bg-[color-mix(in_oklab,var(--warn)_16%,transparent)] text-[color-mix(in_oklab,var(--warn),black_30%)]'
            : 'bg-[color-mix(in_oklab,var(--success)_14%,transparent)] text-[color-mix(in_oklab,var(--success),black_25%)]'
      )}
    >
      {decision}
    </span>
  );
}

function executionDetail(exec: ApiHookExecution): string {
  const parts: string[] = [];
  if (exec.detail) parts.push(exec.detail);
  if (typeof exec.exit_code === 'number') parts.push(`exit ${exec.exit_code}`);
  if (typeof exec.http_status === 'number') parts.push(`HTTP ${exec.http_status}`);
  if (typeof exec.token_count === 'number') parts.push(`${exec.token_count} tokens`);
  return parts.join(' · ');
}

// Shared row of one recorded hook evaluation (per-hook history + the
// workspace-wide recent-executions section).
function ExecutionRow({ exec }: { exec: ApiHookExecution }) {
  const detail = executionDetail(exec);
  return (
    <div className="flex items-center gap-2.5 py-1.5" data-testid={'hook-exec-' + exec.id}>
      <DecisionBadge decision={exec.decision} />
      <span className="w-28 shrink-0 truncate font-mono text-[11px] text-fg2">{exec.hook_name}</span>
      <span className="w-32 shrink-0 truncate font-mono text-[11px] text-muted">{exec.event}</span>
      <span className="shrink-0 font-mono text-[11px] text-muted">{formatDuration(exec.duration_ms)}</span>
      {detail && <span className="min-w-0 flex-1 truncate text-[11px] text-muted" title={detail}>{detail}</span>}
      <span className="ml-auto shrink-0 font-mono text-[10px] text-muted">{formatTime(exec.created_at)}</span>
    </div>
  );
}

// HooksPane renders the workspace's agent lifecycle hooks in evaluation order
// (D14: the list order IS the execution order — drag to reorder, first block
// wins), the read-only instance section (D13: mandatory visibility, no
// control from below), the per-hook execution history, and the workspace-wide
// recent-executions audit trail. Writes are limited to hooks.write holders.
export function HooksPane({ tenant, onToast = () => {}, canWrite }: HooksPaneProps) {
  const derivedCanWrite = useCanWriteTools(tenant);
  const writer = canWrite !== undefined ? canWrite : derivedCanWrite;

  const wsId = tenant?.sub || tenant?.id;
  const [instanceHooks, setInstanceHooks] = useState<ApiHook[]>([]);
  const [hooks, setHooks] = useState<ApiHook[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<ApiError | Error | null>(null);

  const [dialogState, setDialogState] = useState<DialogState | null>(null);
  const [deleting, setDeleting] = useState<ApiHook | null>(null);
  const [busy, setBusy] = useState(false);

  // Per-hook execution history, cached per expansion; the workspace-wide
  // recent trail sits under the list.
  const [historyByHook, setHistoryByHook] = useState<Record<string, ApiHookExecution[]>>({});
  const [open, setOpen] = useState<string[]>([]);
  const [recent, setRecent] = useState<ApiHookExecution[]>([]);

  const load = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      // The recent trail enriches the pane but never blocks the list.
      const [listRes, recentRes] = await Promise.all([
        api.hooks.list(wsId),
        api.hooks.executions(wsId, 10).catch(() => null),
      ]);
      setInstanceHooks(listRes.instance || []);
      setHooks(listRes.hooks || []);
      if (recentRes) setRecent(recentRes.executions || []);
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
    if (wsId) void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reload per workspace only
  }, [wsId]);

  const replaceHook = (next: ApiHook) => {
    setHooks((prev) => prev.map((h) => (h.id === next.id ? next : h)));
  };

  const handleToggle = async (hook: ApiHook) => {
    try {
      const res = await api.hooks.update(wsId, hook.id, { enabled: !hook.enabled });
      if (res?.hook) replaceHook(res.hook);
      onToast(res?.hook?.enabled ? `${hook.name} enabled` : `${hook.name} disabled — it no longer fires`);
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to update ${hook.name}`), 'danger');
    }
  };

  const handleDelete = async (hook: ApiHook) => {
    setBusy(true);
    try {
      await api.hooks.delete(wsId, hook.id);
      setHooks((prev) => prev.filter((h) => h.id !== hook.id));
      // The audit trail survives the delete (D16) — refresh the recent list.
      api.hooks
        .executions(wsId, 10)
        .then((res) => setRecent(res.executions || []))
        .catch(() => {});
      setDeleting(null);
      onToast(`${hook.name} deleted — its execution history is kept`);
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to delete ${hook.name}`), 'danger');
    } finally {
      setBusy(false);
    }
  };

  const toggleExpanded = (id: string) => {
    setOpen((o) => (o.includes(id) ? o.filter((x) => x !== id) : [...o, id]));
    if (!historyByHook[id] && !open.includes(id)) {
      api.hooks
        .hookExecutions(wsId, id, 50)
        .then((res) => setHistoryByHook((prev) => ({ ...prev, [id]: res.executions || [] })))
        .catch(() => setHistoryByHook((prev) => ({ ...prev, [id]: [] })));
    }
  };

  // HTML5 drag reorder (D14): the dropped order is the new execution order.
  const [dragIndex, setDragIndex] = useState<number | null>(null);
  const handleDrop = async (targetIndex: number) => {
    if (dragIndex === null || dragIndex === targetIndex) return;
    const next = [...hooks];
    const [moved] = next.splice(dragIndex, 1);
    next.splice(targetIndex, 0, moved);
    setHooks(next);
    setDragIndex(null);
    try {
      await api.hooks.reorder(
        wsId,
        next.map((h) => h.id)
      );
      onToast(`${moved.name} moved to position ${targetIndex + 1}`);
    } catch (err: unknown) {
      onToast(formatApiError(err, 'Failed to reorder hooks'), 'danger');
      void load();
    }
  };

  const handleDialogSave = async (payload: ApiHookPayload): Promise<ApiHookSaveResult | void> => {
    if (dialogState?.mode === 'edit' && dialogState.hook) {
      const res = await api.hooks.update(wsId, dialogState.hook.id, payload);
      if (res?.hook) replaceHook(res.hook);
      onToast(`${payload.name} updated`);
      return res;
    }
    const res = await api.hooks.create(wsId, payload);
    if (res?.hook) {
      setHooks((prev) => [...prev, res.hook]);
      onToast(`${res.hook.name} added`);
    }
    return res;
  };

  const expanded = (id: string) => open.includes(id);

  return (
    <div className="max-w-xl" data-od-id="pane-hooks" data-testid="pane-hooks">
      {(hooks.length > 0 || instanceHooks.length > 0) && writer && (
        <div className="mb-4 flex justify-end">
          <button
            type="button"
            data-od-id="btn-hook-add"
            data-testid="btn-hook-add"
            onClick={() => setDialogState({ mode: 'add' })}
            className="h-9 shrink-0 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
          >
            Add hook
          </button>
        </div>
      )}

      {loading && hooks.length === 0 && instanceHooks.length === 0 ? (
        <div className="space-y-3">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-16 animate-pulse rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]" />
          ))}
        </div>
      ) : loadError ? (
        <div className="py-6">
          <ErrorState
            illustration={serverErrorSvg}
            title="Couldn't load hooks"
            detail={loadError.message}
            primaryAction={{ label: 'Retry', onClick: load }}
          />
        </div>
      ) : (
        <div className="space-y-2.5">
          {hooks.map((hook, i) => {
            const st = hookStatusView(hook);
            const meta = hookEventMeta(hook.event);
            const history = historyByHook[hook.id];
            return (
              <div
                key={hook.id}
                draggable={writer}
                onDragStart={(e) => {
                  setDragIndex(i);
                  e.dataTransfer?.setData('text/plain', String(i));
                }}
                onDragOver={(e) => {
                  if (dragIndex !== null && dragIndex !== i) e.preventDefault();
                }}
                onDrop={(e) => {
                  e.preventDefault();
                  void handleDrop(i);
                }}
                onDragEnd={() => setDragIndex(null)}
                className={cx(
                  'rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))]',
                  !hook.enabled && 'opacity-70',
                  writer && 'cursor-grab active:cursor-grabbing'
                )}
                data-od-id={'hook-' + hook.id}
                data-testid={'hook-' + hook.id}
              >
                <div className="flex items-center gap-3 px-4 py-3">
                  {writer && (
                    <span
                      aria-hidden="true"
                      title="Drag to reorder"
                      data-testid={'hook-handle-' + hook.id}
                      className="shrink-0 font-mono text-[11px] leading-none text-muted"
                    >
                      ⠿
                    </span>
                  )}
                  <span className="w-4 shrink-0 text-right font-mono text-[11px] text-muted" data-testid={'hook-position-' + hook.id}>
                    {i + 1}
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <p className="text-[14px] font-medium text-fg">{hook.name}</p>
                      <span
                        className={cx(
                          'inline-flex items-center gap-1.5 text-[11px]',
                          st.errored ? 'text-danger' : hook.enabled ? 'text-[color-mix(in_oklab,var(--success),black_25%)]' : 'text-muted'
                        )}
                        data-testid={'hook-status-' + hook.id}
                        title={st.errored ? hook.status_error || 'Last delivery failed' : st.label}
                      >
                        <span className={cx('h-1.5 w-1.5 rounded-full', st.dot)} />
                        {st.label}
                      </span>
                      {meta.blocking && (
                        <span
                          className="rounded-full bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] px-2 py-0.5 font-mono text-[10px] text-danger"
                          title="This hook can block what it applies to"
                        >
                          can block
                        </span>
                      )}
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-1.5">
                      <Chip mono>{hook.event}</Chip>
                      <Chip mono>{hookHandlerLabel(hook.handler_type)}</Chip>
                      <Chip mono>{matcherSummary(hook)}</Chip>
                    </div>
                    {st.errored && hook.status_error && (
                      <p className="mt-1 truncate text-[11px] text-danger" title={hook.status_error}>
                        {hook.status_error}
                      </p>
                    )}
                  </div>
                  <Chip className="shrink-0">Workspace</Chip>
                  {writer && (
                    <>
                      <Toggle on={hook.enabled} label={'Enable ' + hook.name} onChange={() => void handleToggle(hook)} />
                      <button
                        type="button"
                        aria-label={'Edit ' + hook.name}
                        data-od-id={'btn-edit-' + hook.id}
                        data-testid={'btn-edit-' + hook.id}
                        onClick={() => setDialogState({ mode: 'edit', hook })}
                        className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
                      >
                        <Icon name="edit" size={13} />
                      </button>
                      <button
                        type="button"
                        aria-label={'Delete ' + hook.name}
                        data-od-id={'hook-delete-' + hook.id}
                        data-testid={'hook-delete-' + hook.id}
                        onClick={() => setDeleting(hook)}
                        className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger"
                      >
                        <Icon name="x" size={13} />
                      </button>
                    </>
                  )}
                  <button
                    type="button"
                    onClick={() => toggleExpanded(hook.id)}
                    aria-expanded={expanded(hook.id)}
                    aria-label={'Execution history for ' + hook.name}
                    data-testid={'hook-expand-' + hook.id}
                    className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
                  >
                    <Icon name="chevright" size={13} className={cx('transition-transform', expanded(hook.id) && 'rotate-90')} />
                  </button>
                </div>
                {expanded(hook.id) && (
                  <div className="border-t border-[var(--border-soft)] px-4 py-3">
                    <p className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-muted">
                      Execution history
                    </p>
                    {history === undefined ? (
                      <p className="text-[12px] text-muted">Loading…</p>
                    ) : history.length === 0 ? (
                      <p className="text-[12px] text-muted">No recorded evaluations yet.</p>
                    ) : (
                      <div className="divide-y divide-[var(--border-soft)]">
                        {history.map((exec) => (
                          <ExecutionRow key={exec.id} exec={exec} />
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
          {hooks.length > 0 && (
            <p className="text-[11px] text-muted" data-testid="hooks-order-hint">
              Top to bottom — first block wins. Drag to reorder; instance hooks always evaluate first.
            </p>
          )}

          {hooks.length === 0 && (
            <div className="py-8 text-center" data-od-id="hooks-empty" data-testid="hooks-empty">
              <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
                <Icon name="shield" size={20} />
              </div>
              <p className="text-[14px] font-medium text-fg">No hooks configured</p>
              <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
                Lifecycle hooks observe or block agent runs — gates run top to bottom and the first block wins.
              </p>
              {writer && (
                <button
                  type="button"
                  data-od-id="btn-hook-empty-add"
                  data-testid="btn-hook-empty-add"
                  onClick={() => setDialogState({ mode: 'add' })}
                  className="mt-3 inline-flex h-8 items-center gap-1.5 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
                >
                  <Icon name="plus" size={13} /> Add your first hook
                </button>
              )}
            </div>
          )}

          {instanceHooks.length > 0 && (
            <div data-testid="hooks-instance-section">
              <p className="mb-2 mt-4 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-muted">
                Instance
              </p>
              <div className="space-y-2.5">
                {instanceHooks.map((hook) => {
                  const st = hookStatusView(hook);
                  const meta = hookEventMeta(hook.event);
                  return (
                    <div
                      key={hook.id}
                      className="rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] px-4 py-3"
                      data-od-id={'hook-instance-' + hook.id}
                      data-testid={'hook-instance-' + hook.id}
                    >
                      <div className="flex items-center gap-3">
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <p className="text-[14px] font-medium text-fg">{hook.name}</p>
                            <span
                              className={cx('inline-flex items-center gap-1.5 text-[11px]', st.errored ? 'text-danger' : 'text-[color-mix(in_oklab,var(--success),black_25%)]')}
                              data-testid={'hook-instance-status-' + hook.id}
                              title={st.errored ? hook.status_error || 'Last delivery failed' : st.label}
                            >
                              <span className={cx('h-1.5 w-1.5 rounded-full', st.dot)} />
                              {st.label}
                            </span>
                            {meta.blocking && (
                              <span className="rounded-full bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] px-2 py-0.5 font-mono text-[10px] text-danger">
                                can block
                              </span>
                            )}
                          </div>
                          <div className="mt-1 flex flex-wrap items-center gap-1.5">
                            <Chip mono>{hook.event}</Chip>
                            <Chip mono>{hookHandlerLabel(hook.handler_type)}</Chip>
                            <Chip mono>{matcherSummary(hook)}</Chip>
                          </div>
                        </div>
                        <Chip className="shrink-0">Instance</Chip>
                      </div>
                    </div>
                  );
                })}
              </div>
              <p className="mt-2 text-[11px] leading-4 text-muted">
                Instance hooks apply to every workspace and cannot be disabled or changed from here.
              </p>
            </div>
          )}

          {recent.length > 0 && (
            <div className="mt-6" data-testid="hooks-recent-executions">
              <p className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-muted">
                Recent executions
              </p>
              <div className="rounded-md border border-line px-4 py-2">
                <div className="divide-y divide-[var(--border-soft)]">
                  {recent.map((exec) => (
                    <ExecutionRow key={exec.id} exec={exec} />
                  ))}
                </div>
              </div>
              <p className="mt-2 text-[11px] leading-4 text-muted">
                Dry runs from the dialog's Test section are never recorded here.
              </p>
            </div>
          )}
        </div>
      )}

      {dialogState && (
        <HookDialog
          hook={dialogState.mode === 'edit' ? dialogState.hook ?? null : null}
          wsSlug={wsId}
          existingNames={hooks.map((h) => h.name)}
          onClose={() => setDialogState(null)}
          onSave={handleDialogSave}
        />
      )}

      {deleting ? (
        <Modal
          title={`Delete ${deleting.name}`}
          onClose={() => setDeleting(null)}
          odId="modal-hook-delete"
          data-testid="modal-hook-delete"
          footer={
            <>
              <button
                type="button"
                onClick={() => setDeleting(null)}
                data-testid="btn-hook-delete-cancel"
                className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void handleDelete(deleting)}
                disabled={busy}
                data-testid="btn-hook-delete-confirm"
                className="flex h-9 items-center rounded-md bg-danger px-4 text-[13px] font-semibold text-white transition-colors hover:opacity-90 disabled:opacity-40"
              >
                {busy ? 'Deleting…' : 'Delete'}
              </button>
            </>
          }
        >
          <p className="p-5 text-[13px] leading-5 text-fg2">
            {deleting.name} is removed from this workspace's hooks. Its execution history is preserved and still shows
            in the workspace-wide trail. This cannot be undone.
          </p>
        </Modal>
      ) : null}
    </div>
  );
}
