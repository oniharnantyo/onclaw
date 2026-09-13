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
  type ApiAgent,
  type ApiMcpServer,
  type McpServerPayload,
} from "../../lib/api";
import { useCanWriteTools } from "../../lib/tools";
import serverErrorSvg from "../../assets/server-error.svg";
import { McpServerDialog } from "../../modals/McpServerDialog";

export interface McpPaneProps {
  tenant: any;
  /** Unused since the pane went API-backed; kept for SettingsPage compat. */
  onUpdate?: (fn: any) => void;
  onToast?: (text: string, kind?: string) => void;
  /** Override the derived tools.write check (tests). */
  canWrite?: boolean;
}

interface StatusView {
  dot: string;
  label: string;
  errored: boolean;
}

// Display status: the enabled master switch wins (Paused), then the probed
// status — `connected`/`ok` green, `error` red, anything else unknown gray.
function statusView(s: ApiMcpServer): StatusView {
  if (!s.enabled) return { dot: 'bg-muted', label: 'Paused', errored: false };
  if (s.status === 'error') return { dot: 'bg-danger', label: 'Error', errored: true };
  if (s.status === 'ok' || s.status === 'connected')
    return { dot: 'bg-success', label: 'Connected', errored: false };
  return { dot: 'bg-muted', label: 'Unknown', errored: false };
}

// Design D1: agents reference opted-in MCP servers by UUID in `enabled_mcps`.
function mcpRefs(a: any): string[] {
  return a?.enabled_mcps ?? [];
}

// McpPane renders the workspace MCP registry from the workspace MCP endpoints
// — no local mock state. Write controls (add/edit/pause/retry/delete) are
// limited to tools.write holders; read-only members still see cards and the
// expandable tool lists.
export function McpPane({ tenant, onToast = () => {}, canWrite }: McpPaneProps) {
  const derivedCanWrite = useCanWriteTools(tenant);
  const writer = canWrite !== undefined ? canWrite : derivedCanWrite;

  const [servers, setServers] = useState<ApiMcpServer[]>([]);
  const [agents, setAgents] = useState<ApiAgent[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<ApiError | Error | null>(null);

  const [open, setOpen] = useState<string[]>([]);
  const [dialogState, setDialogState] = useState<
    { mode: 'add' } | { mode: 'edit'; server: ApiMcpServer } | null
  >(null);
  const [deleting, setDeleting] = useState<ApiMcpServer | null>(null);
  const [probingId, setProbingId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const targetWsId = tenant?.sub || tenant?.id;

  const load = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      // The agents list only enriches the used-by column — never block the
      // registry on it.
      const [mcpRes, agentRes] = await Promise.all([
        api.mcp.list(targetWsId),
        api.agents.list(targetWsId).catch(() => null),
      ]);
      setServers(mcpRes.servers || []);
      if (agentRes) setAgents(agentRes.agents || []);
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
  }, [targetWsId]);

  const replaceServer = (next: ApiMcpServer) => {
    setServers((prev) => prev.map((s) => (s.id === next.id ? next : s)));
  };

  const usersOf = (id: string) => agents.filter((a) => mcpRefs(a).includes(id));
  const expanded = (id: string) => open.includes(id);

  const handleToggle = async (s: ApiMcpServer) => {
    try {
      const res = await api.mcp.update(targetWsId, s.id, { enabled: !s.enabled });
      if (res?.server) replaceServer(res.server);
      onToast(
        res?.server?.enabled
          ? `${s.name} resumed — agents regain its tools`
          : `${s.name} paused — agents lose access on the next run`
      );
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to update ${s.name}`), 'danger');
    }
  };

  const handleRetry = async (s: ApiMcpServer) => {
    setProbingId(s.id);
    try {
      const res = await api.mcp.probe(targetWsId, s.id);
      if (res?.server) replaceServer(res.server);
      if (res?.server?.status === 'error') {
        onToast(`${s.name}: ${res.server.status_error || 'still unreachable'}`, 'danger');
      } else {
        onToast(`${s.name} connected — ${res?.server?.tool_count ?? 0} tools exposed`);
      }
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to re-probe ${s.name}`), 'danger');
    } finally {
      setProbingId(null);
    }
  };

  const handleDelete = async (s: ApiMcpServer) => {
    setBusy(true);
    try {
      await api.mcp.delete(targetWsId, s.id);
      setServers((prev) => prev.filter((x) => x.id !== s.id));
      setDeleting(null);
      onToast(`${s.name} deleted`);
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to delete ${s.name}`), 'danger');
    } finally {
      setBusy(false);
    }
  };

  const handleDialogSave = async (payload: McpServerPayload) => {
    if (dialogState?.mode === 'edit') {
      const res = await api.mcp.update(targetWsId, dialogState.server.id, payload);
      if (res?.server) replaceServer(res.server);
      onToast(`${payload.name} updated`);
    } else {
      const res = await api.mcp.create(targetWsId, payload);
      if (res?.server) {
        setServers((prev) => [...prev, res.server]);
        onToast(
          res.server.status === 'error'
            ? `${res.server.name} added — probe failed: ${res.server.status_error || 'unreachable'}`
            : `${res.server.name} added${res.server.tool_count ? ` — ${res.server.tool_count} tools exposed` : ''}`
        );
      }
    }
  };

  return (
    <div className="max-w-xl" data-od-id="pane-mcp" data-testid="pane-mcp">
      {servers.length > 0 && writer && (
        <div className="mb-4 flex justify-end">
          <button
            type="button"
            data-od-id="btn-mcp-add"
            data-testid="btn-mcp-add"
            onClick={() => setDialogState({ mode: 'add' })}
            className="h-9 shrink-0 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
          >
            Add server
          </button>
        </div>
      )}

      {loading && servers.length === 0 ? (
        <div className="space-y-3">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-16 animate-pulse rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]" />
          ))}
        </div>
      ) : loadError ? (
        <div className="py-6">
          <ErrorState
            illustration={serverErrorSvg}
            title="Couldn't load MCP servers"
            detail={loadError.message}
            primaryAction={{ label: 'Retry', onClick: load }}
          />
        </div>
      ) : (
        <div className="space-y-2.5">
          {servers.map((s) => {
            const st = statusView(s);
            const users = usersOf(s.id);
            const inactive = !s.enabled && users.length > 0;
            // The row contract carries tool_count only; richer responses may
            // include the probed names — render chips when present.
            const rawNames: unknown = (s as any).tools;
            const toolNames = Array.isArray(rawNames)
              ? rawNames.filter((t): t is string => typeof t === 'string')
              : [];
            return (
              <div
                key={s.id}
                className={cx(
                  'rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))]',
                  !s.enabled && 'opacity-70'
                )}
                data-od-id={'mcp-' + s.id}
                data-testid={'mcp-' + s.id}
              >
                <div className="flex items-center gap-3 px-4 py-3">
                  <span
                    className={cx(
                      'flex h-9 w-9 shrink-0 items-center justify-center rounded-md',
                      st.errored
                        ? 'bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] text-danger'
                        : s.enabled
                        ? 'bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent'
                        : 'bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted'
                    )}
                  >
                    <Icon name="plug" size={16} />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <p className="text-[14px] font-medium text-fg">{s.name}</p>
                      <span
                        className={cx(
                          'inline-flex items-center gap-1.5 text-[11px]',
                          st.errored
                            ? 'text-danger'
                            : s.enabled
                            ? 'text-[color-mix(in_oklab,var(--success),black_25%)]'
                            : 'text-muted'
                        )}
                        data-testid={'mcp-status-' + s.id}
                      >
                        <span className={cx('h-1.5 w-1.5 rounded-full', st.dot)} />
                        {st.label}
                      </span>
                    </div>
                    <p className="truncate font-mono text-[11px] text-muted">{s.transport}</p>
                    {st.errored && s.status_error && (
                      <p className="truncate text-[11px] text-danger" title={s.status_error}>
                        {s.status_error}
                      </p>
                    )}
                  </div>
                  <div
                    className="shrink-0 text-right"
                    title={users.map((u) => u.name).join(', ') || undefined}
                  >
                    <p className="font-mono text-[11px] text-fg2">
                      {s.tool_count} {s.tool_count === 1 ? 'tool' : 'tools'}
                    </p>
                    <p
                      className={cx(
                        'font-mono text-[11px]',
                        inactive
                          ? 'text-[color-mix(in_oklab,var(--warn),black_38%)]'
                          : 'text-muted'
                      )}
                    >
                      {users.length === 0
                        ? 'no agents'
                        : users.length + (users.length === 1 ? ' agent' : ' agents') +
                          (inactive ? ' · loses tools' : '')}
                    </p>
                  </div>
                  {writer && (
                    <>
                      {st.errored ? (
                        <button
                          type="button"
                          data-od-id={'mcp-retry-' + s.id}
                          data-testid={'mcp-retry-' + s.id}
                          onClick={() => void handleRetry(s)}
                          disabled={probingId === s.id}
                          className="h-8 shrink-0 rounded-md border border-[color-mix(in_oklab,var(--danger)_45%,transparent)] px-2.5 text-[12px] font-medium text-danger transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] disabled:opacity-50"
                        >
                          {probingId === s.id ? 'Probing…' : 'Retry'}
                        </button>
                      ) : null}
                      <Toggle
                        on={s.enabled}
                        label={'Enable ' + s.name}
                        onChange={() => void handleToggle(s)}
                      />
                      <button
                        type="button"
                        aria-label={'Edit ' + s.name}
                        data-od-id={'btn-edit-' + s.id}
                        data-testid={'btn-edit-' + s.id}
                        onClick={() => setDialogState({ mode: 'edit', server: s })}
                        className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
                      >
                        <Icon name="edit" size={13} />
                      </button>
                      <button
                        type="button"
                        aria-label={'Delete ' + s.name}
                        data-od-id={'mcp-delete-' + s.id}
                        data-testid={'mcp-delete-' + s.id}
                        onClick={() => setDeleting(s)}
                        className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger"
                      >
                        <Icon name="x" size={13} />
                      </button>
                    </>
                  )}
                  <button
                    type="button"
                    onClick={() =>
                      setOpen((o) =>
                        o.includes(s.id) ? o.filter((x) => x !== s.id) : [...o, s.id]
                      )
                    }
                    aria-expanded={expanded(s.id)}
                    aria-label={'Tools exposed by ' + s.name}
                    data-testid={'mcp-expand-' + s.id}
                    className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
                  >
                    <Icon
                      name="chevright"
                      size={13}
                      className={cx('transition-transform', expanded(s.id) && 'rotate-90')}
                    />
                  </button>
                </div>
                {expanded(s.id) && (
                  <div className="border-t border-[var(--border-soft)] px-4 py-3">
                    <p className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-muted">
                      Tools exposed
                    </p>
                    {toolNames.length ? (
                      <div className="flex flex-wrap gap-1.5">
                        {toolNames.map((t) => (
                          <Chip key={t} mono>
                            {t}
                          </Chip>
                        ))}
                      </div>
                    ) : (
                      <p className="text-[12px] text-muted">
                        {s.tool_count > 0
                          ? `${s.tool_count} ${s.tool_count === 1 ? 'tool is' : 'tools are'} exposed to opted-in agents — names show on tool-call cards in transcripts.`
                          : 'No tools listed yet — retry the probe once the server is reachable.'}
                      </p>
                    )}
                  </div>
                )}
              </div>
            );
          })}
          {servers.length === 0 && (
            <div className="py-8 text-center" data-od-id="mcp-empty" data-testid="mcp-empty">
              <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
                <Icon name="plug" size={20} />
              </div>
              <p className="text-[14px] font-medium text-fg">No MCP servers configured</p>
              <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
                Add a Model Context Protocol server to expose new tools to your agents.
              </p>
              {writer && (
                <button
                  type="button"
                  data-od-id="btn-mcp-empty-add"
                  data-testid="btn-mcp-empty-add"
                  onClick={() => setDialogState({ mode: 'add' })}
                  className="mt-3 inline-flex h-8 items-center gap-1.5 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
                >
                  <Icon name="plus" size={13} /> Add your first server
                </button>
              )}
            </div>
          )}
        </div>
      )}

      {dialogState && (
        <McpServerDialog
          server={dialogState.mode === 'edit' ? dialogState.server : null}
          existingServers={servers}
          onClose={() => setDialogState(null)}
          onSave={handleDialogSave}
        />
      )}

      {deleting ? (
        <Modal
          title={`Delete ${deleting.name}`}
          onClose={() => setDeleting(null)}
          odId="modal-mcp-delete"
          data-testid="modal-mcp-delete"
          footer={
            <>
              <button
                type="button"
                onClick={() => setDeleting(null)}
                data-testid="btn-mcp-delete-cancel"
                className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void handleDelete(deleting)}
                disabled={busy}
                data-testid="btn-mcp-delete-confirm"
                className="flex h-9 items-center rounded-md bg-danger px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
              >
                {busy ? 'Deleting…' : 'Delete'}
              </button>
            </>
          }
        >
          <p className="p-5 text-[13px] leading-5 text-fg2">
            {deleting.name} is removed from the workspace registry and its credentials are deleted.
            {usersOf(deleting.id).length > 0
              ? ` ${usersOf(deleting.id)
                  .map((u) => u.name)
                  .join(', ')} still reference it — the stale references contribute no tools.`
              : ' No agents currently reference it.'}{' '}
            This cannot be undone.
          </p>
        </Modal>
      ) : null}
    </div>
  );
}
