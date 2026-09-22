import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { cx } from "../../lib/helpers";
import { Icon } from "../../components/ui/Icon";
import { Chip } from "../../components/ui/Chip";
import { Modal } from "../../components/ui/Modal";
import { ErrorState } from "../../components/ErrorState";
import { formatApiError, ApiError } from "../../lib/api";
import {
  accessLevelLabel,
  connectionServiceName,
  connectionStatusView,
  connectionsApi,
  serviceIconKey,
  useCanManageIntegrations,
  type ApiConnection,
  type ApiIntegrationRecipe,
} from "../../lib/connectionsApi";
import { ConnectServiceDialog } from "../../modals/ConnectServiceDialog";
import serverErrorSvg from "../../assets/server-error.svg";

export interface IntegrationsSectionProps {
  tenant: any;
  onToast?: (text: string, kind?: string) => void;
  /** Unused since the pane went API-backed; kept for SettingsPage compat. */
  onUpdate?: (fn: any) => void;
  /** Override the derived integrations.write check (tests). */
  canWrite?: boolean;
}

function SectionLabel({ children }: { children: any }) {
  return (
    <p className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-muted">
      {children}
    </p>
  );
}

/**
 * Integrations settings surface (add-workspace-connections 4.1): the recipe
 * gallery (available cards with Integrate, coming-soon cards disabled and
 * truthful, the Custom MCP advanced card leading to MCP management) above the
 * Connected section (status, access level, attached agents, last-4 hint,
 * manage/disconnect). Reads ride workspace membership; Integrate, probe, and
 * disconnect need integrations.write — everyone else sees the gallery
 * read-only.
 */
export function IntegrationsSection({ tenant, onToast = () => {}, onUpdate, canWrite }: IntegrationsSectionProps) {
  const navigate = useNavigate();
  const derivedWriter = useCanManageIntegrations(tenant);
  const writer = canWrite !== undefined ? canWrite : derivedWriter;

  const [recipes, setRecipes] = useState<ApiIntegrationRecipe[]>([]);
  const [connections, setConnections] = useState<ApiConnection[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<ApiError | Error | null>(null);

  const [connecting, setConnecting] = useState<ApiIntegrationRecipe | null>(null);
  const [disconnecting, setDisconnecting] = useState<ApiConnection | null>(null);
  const [busy, setBusy] = useState(false);
  const [probingId, setProbingId] = useState<string | null>(null);

  const ws = tenant?.sub || tenant?.id;

  const load = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const [recipeRes, connRes] = await Promise.all([
        connectionsApi.recipes(ws),
        connectionsApi.list(ws),
      ]);
      setRecipes(recipeRes?.recipes || []);
      setConnections(connRes?.connections || []);
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
    if (ws) void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reload per workspace only
  }, [ws]);

  const connectedByService = new Map(connections.map((c) => [c.service, c]));
  const available = recipes.filter((r) => r.availability === 'available');
  const comingSoon = recipes.filter((r) => r.availability !== 'available');
  const serviceName = (c: ApiConnection) => connectionServiceName(c, recipes);

  const handleProbe = async (c: ApiConnection) => {
    setProbingId(c.id);
    const name = serviceName(c);
    try {
      const res = await connectionsApi.probe(ws, c.id);
      if (res?.connection) {
        setConnections((prev) => prev.map((x) => (x.id === res.connection.id ? res.connection : x)));
        if (res.connection.status === 'error') {
          onToast(`${name}: ${res.connection.status_error || 'still unreachable'}`, 'danger');
        } else {
          onToast(`${name} is connected`);
        }
      }
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to check ${name}`), 'danger');
    } finally {
      setProbingId(null);
    }
  };

  const handleDisconnect = async (c: ApiConnection) => {
    setBusy(true);
    const name = serviceName(c);
    try {
      await connectionsApi.disconnect(ws, c.id);
      setConnections((prev) => prev.filter((x) => x.id !== c.id));
      setDisconnecting(null);
      onToast(`${name} disconnected`);
    } catch (err: unknown) {
      // Permission rejections (integrations.write) and rare conflicts surface
      // with their envelope messages.
      onToast(formatApiError(err, `Failed to disconnect ${name}`), 'danger');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="max-w-xl space-y-8" data-od-id="pane-integrations" data-testid="pane-integrations">
      {loading && recipes.length === 0 && connections.length === 0 ? (
        <div className="space-y-3">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-16 animate-pulse rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]" />
          ))}
        </div>
      ) : loadError ? (
        <div className="py-6">
          <ErrorState
            illustration={serverErrorSvg}
            title="Couldn't load integrations"
            detail={loadError.message}
            primaryAction={{ label: 'Retry', onClick: load }}
          />
        </div>
      ) : (
        <>
          {/* Connected section — one card per live connection. */}
          <section data-testid="connections-section">
            <SectionLabel>Connected</SectionLabel>
            {connections.length === 0 ? (
              <div className="rounded-md border border-dashed border-line px-4 py-5 text-center" data-testid="connections-empty">
                <p className="text-[13px] font-medium text-fg">No services connected yet</p>
                <p className="mx-auto mt-1 max-w-sm text-[12px] leading-4 text-muted">
                  Pick a service below, paste an access token, and its tools become attachable to every agent.
                </p>
              </div>
            ) : (
              <div className="space-y-2.5">
                {connections.map((c) => {
                  const st = connectionStatusView(c);
                  const agents = c.attached_agents || [];
                  const name = serviceName(c);
                  return (
                    <div
                      key={c.id}
                      className="rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))]"
                      data-testid={'connection-' + c.id}
                    >
                      <div className="flex items-center gap-3 px-4 py-3">
                        <span
                          className={cx(
                            'flex h-9 w-9 shrink-0 items-center justify-center rounded-md',
                            st.errored
                              ? 'bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] text-danger'
                              : 'bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent'
                          )}
                        >
                          <Icon name={serviceIconKey(c.service, recipes.find((r) => r.id === c.service)?.icon)} size={16} />
                        </span>
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <p className="text-[14px] font-medium text-fg">{name}</p>
                            <Chip mono>{accessLevelLabel(c.access_level)}</Chip>
                          </div>
                          <div className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-0.5">
                            <span
                              className={cx(
                                'inline-flex items-center gap-1.5 text-[11px]',
                                st.errored ? 'text-danger' : 'text-[color-mix(in_oklab,var(--success),black_25%)]'
                              )}
                              data-testid={'connection-status-' + c.id}
                            >
                              <span className={cx('h-1.5 w-1.5 rounded-full', st.dot)} />
                              {st.label}
                            </span>
                            {typeof c.token_hint === 'string' && c.token_hint !== '' && (
                              <span
                                className="inline-flex items-center gap-1 font-mono text-[11px] text-muted"
                                data-testid={'connection-hint-' + c.id}
                                title="Token last 4 — never shown in full"
                              >
                                <Icon name="lock" size={10} />
                                ····{c.token_hint}
                              </span>
                            )}
                            <span
                              className="truncate font-mono text-[11px] text-muted"
                              data-testid={'connection-agents-' + c.id}
                              title={agents.join(', ') || undefined}
                            >
                              {agents.length === 0 ? 'no agents attached' : agents.join(', ')}
                            </span>
                          </div>
                          {st.errored && c.status_error && (
                            <p className="mt-0.5 truncate text-[11px] text-danger" title={c.status_error}>
                              {c.status_error}
                            </p>
                          )}
                        </div>
                        <div className="flex shrink-0 items-center gap-1.5">
                          <button
                            type="button"
                            data-testid={'btn-connection-probe-' + c.id}
                            onClick={() => void handleProbe(c)}
                            disabled={probingId === c.id}
                            className="h-8 rounded-md border border-line px-2.5 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-50"
                          >
                            {probingId === c.id ? 'Checking…' : 'Check connection'}
                          </button>
                          {writer && (
                            <button
                              type="button"
                              data-testid={'btn-connection-disconnect-' + c.id}
                              onClick={() => setDisconnecting(c)}
                              className="h-8 rounded-md px-2.5 text-[12px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger"
                            >
                              Disconnect
                            </button>
                          )}
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </section>

          {/* Gallery — available recipes. */}
          {available.length > 0 && (
            <section data-testid="recipes-available">
              <SectionLabel>Available services</SectionLabel>
              <div className="space-y-2.5">
                {available.map((r) => {
                  const conn = connectedByService.get(r.id);
                  return (
                    <div
                      key={r.id}
                      className="flex items-center gap-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3"
                      data-testid={'recipe-' + r.id}
                    >
                      <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted">
                        <Icon name={serviceIconKey(r.id, r.icon)} size={16} />
                      </span>
                      <div className="min-w-0 flex-1">
                        <p className="text-[14px] font-medium text-fg">{r.service}</p>
                        <p className="truncate font-mono text-[11px] text-muted">{r.transport}</p>
                      </div>
                      {conn ? (
                        <span className="flex shrink-0 items-center gap-1.5 text-[12px] text-[color-mix(in_oklab,var(--success),black_25%)]">
                          <Icon name="check" size={13} /> Connected
                        </span>
                      ) : writer ? (
                        <button
                          type="button"
                          data-testid={'btn-integrate-' + r.id}
                          onClick={() => setConnecting(r)}
                          className="h-8 shrink-0 rounded-md bg-accent px-3.5 text-[12px] font-semibold text-accenton transition-colors hover:opacity-90"
                        >
                          Integrate
                        </button>
                      ) : (
                        <span className="shrink-0 text-[11px] text-muted" title="Connecting requires integrations.write">
                          Admins manage connections
                        </span>
                      )}
                    </div>
                  );
                })}
              </div>
            </section>
          )}

          {/* Coming-soon gallery — visible, disabled, truthful (D2). */}
          {comingSoon.length > 0 && (
            <section data-testid="recipes-coming-soon">
              <SectionLabel>Coming soon</SectionLabel>
              <div className="space-y-2.5">
                {comingSoon.map((r) => (
                  <div
                    key={r.id}
                    className="flex items-center gap-3 rounded-md border border-line px-4 py-3 opacity-70"
                    data-testid={'recipe-coming-soon-' + r.id}
                  >
                    <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
                      <Icon name={serviceIconKey(r.id, r.icon)} size={16} />
                    </span>
                    <div className="min-w-0 flex-1">
                      <p className="text-[14px] font-medium text-fg2">{r.service}</p>
                      <p className="text-[11px] leading-4 text-muted">
                        {/* Server-declared notes are the truthful copy; the
                            fallback keeps the card honest for recipes that
                            ship without one. */}
                        {r.notes ||
                          (r.auth_kind === 'oauth'
                            ? 'Needs an OAuth sign-in flow OnClaw doesn’t support yet — no dates promised.'
                            : 'Not available in this deployment yet.')}
                      </p>
                    </div>
                    <span className="shrink-0 rounded-md border border-line px-2.5 py-1 text-[11px] font-medium text-muted">
                      Coming soon
                    </span>
                  </div>
                ))}
              </div>
            </section>
          )}

          {/* Advanced path: hand-configured MCP servers (self-managed GitLab,
              internal endpoints). Manage lives in the existing MCP pane. */}
          <section data-testid="custom-mcp-card">
            <SectionLabel>Advanced</SectionLabel>
            <div className="flex items-center gap-3 rounded-md border border-line px-4 py-3">
              <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted">
                <Icon name="plug" size={16} />
              </span>
              <div className="min-w-0 flex-1">
                <p className="text-[14px] font-medium text-fg">Custom MCP server</p>
                <p className="text-[11px] leading-4 text-muted">
                  Self-managed GitLab, internal tools, anything MCP — configure endpoints by hand.
                </p>
              </div>
              <button
                type="button"
                data-testid="btn-custom-mcp"
                onClick={() => navigate('/settings/mcp')}
                className="h-8 shrink-0 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
              >
                Open MCP servers
              </button>
            </div>
          </section>
        </>
      )}

      {connecting && (
        <ConnectServiceDialog
          recipe={connecting}
          tenant={tenant}
          onClose={() => setConnecting(null)}
          onConnected={() => {
            onToast(`${connecting.service} connected`);
            void load();
          }}
          onToast={onToast}
        />
      )}

      {/* Disconnect confirmation states the cascade before anything is sent:
          the connection, its materialized server, and the token are removed;
          attached agents lose the tools; runs keep working. */}
      {disconnecting ? (
        <Modal
          title={`Disconnect ${serviceName(disconnecting)}?`}
          onClose={() => setDisconnecting(null)}
          odId="modal-connection-disconnect"
          data-testid="modal-connection-disconnect"
          footer={
            <>
              <button
                type="button"
                onClick={() => setDisconnecting(null)}
                data-testid="btn-disconnect-cancel"
                className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void handleDisconnect(disconnecting)}
                disabled={busy}
                data-testid="btn-disconnect-confirm"
                className="flex h-9 items-center rounded-md bg-danger px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
              >
                {busy ? 'Disconnecting…' : 'Disconnect'}
              </button>
            </>
          }
        >
          <p className="p-5 text-[13px] leading-5 text-fg2">
            Disconnecting removes the {serviceName(disconnecting)} connection, its materialized MCP server, and the stored
            token — the token cannot be recovered.
            {(disconnecting.attached_agents || []).length > 0
              ? ` ${(disconnecting.attached_agents || []).join(', ')} will lose its tools on the next run.`
              : ' No agents are attached to it.'}{' '}
            Their other tools keep working and runs will not fail. This cannot be undone.
          </p>
        </Modal>
      ) : null}
    </div>
  );
}

