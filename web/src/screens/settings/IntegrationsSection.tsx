import { useEffect, useRef, useState } from "react";
import { useNavigate, useSearchParams, useLocation } from "react-router-dom";
import { cx } from "../../lib/helpers";
import { Icon } from "../../components/ui/Icon";
import { Chip } from "../../components/ui/Chip";
import { Modal } from "../../components/ui/Modal";
import { ErrorState } from "../../components/ErrorState";
import { formatApiError, ApiError } from "../../lib/api";
import {
  accessLevelLabel,
  connectionKind,
  connectionKindLabel,
  connectionServiceName,
  connectionStatusView,
  connectionsApi,
  recipeKind,
  serviceIconKey,
  verbSurfaceCopy,
  useCanManageIntegrations,
  type ApiConnection,
  type ApiIntegrationRecipe,
} from "../../lib/connectionsApi";
import { useIsAdmin } from "../../store/auth";
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
 * Integrations settings surface (add-workspace-connections 4.1,
 * add-connection-oauth 4.1/4.2): the recipe gallery (available cards with
 * Integrate, one-time instance-app setup riding the same dialog, the Custom
 * MCP advanced
 * card leading to MCP management) above the Connected section (status,
 * access level, attached agents, last-4 hint, manage/disconnect — expired
 * OAuth connections offer Reauthorize). Also lands the OAuth consent
 * callback: `?oauth=<recipe_id>&status=connected|failed&detail=…` is resolved
 * (list refreshed or failure surfaced) and cleaned from the URL so reloads
 * don't replay it. Reads ride workspace membership; Integrate, probe,
 * reauthorize, and disconnect need integrations.write — everyone else sees
 * the gallery read-only.
 */
export function IntegrationsSection({ tenant, onToast = () => {}, onUpdate, canWrite }: IntegrationsSectionProps) {
  const navigate = useNavigate();
  const routerLocation = useLocation();
  const [searchParams] = useSearchParams();
  const derivedWriter = useCanManageIntegrations(tenant);
  const writer = canWrite !== undefined ? canWrite : derivedWriter;
  const isAdmin = useIsAdmin();

  const [recipes, setRecipes] = useState<ApiIntegrationRecipe[]>([]);
  const [connections, setConnections] = useState<ApiConnection[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<ApiError | Error | null>(null);

  const [connecting, setConnecting] = useState<ApiIntegrationRecipe | null>(null);
  const [editingApp, setEditingApp] = useState<ApiIntegrationRecipe | null>(null);
  const [disconnecting, setDisconnecting] = useState<ApiConnection | null>(null);
  const [busy, setBusy] = useState(false);
  const [probingId, setProbingId] = useState<string | null>(null);
  const [reauthorizingId, setReauthorizingId] = useState<string | null>(null);

  // OAuth callback outcome (failed only — success just refreshes the list).
  const [oauthFailure, setOauthFailure] = useState<{ service: string; detail: string | null } | null>(null);
  // StrictMode runs effects twice; the key guards the one-shot resolution.
  const oauthHandled = useRef<string | null>(null);

  const ws = tenant?.sub || tenant?.id;

  const load = async (): Promise<ApiIntegrationRecipe[]> => {
    setLoading(true);
    setLoadError(null);
    try {
      const [recipeRes, connRes] = await Promise.all([
        connectionsApi.recipes(ws),
        connectionsApi.list(ws),
      ]);
      setRecipes(recipeRes?.recipes || []);
      setConnections(connRes?.connections || []);
      return recipeRes?.recipes || [];
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 0) {
        return recipes;
      }
      setLoadError(err instanceof Error ? err : new Error(String(err)));
      return recipes;
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (ws) void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reload per workspace only
  }, [ws]);

  // OAuth consent return (add-connection-oauth 4.1): the provider sent the
  // browser back with the outcome in the query string. Resolve it exactly
  // once, clean the params with a replace navigation so reloading the page
  // never replays the message, then refresh the list (success) or surface
  // the provider's failure detail inline (failure).
  useEffect(() => {
    const recipeId = searchParams.get('oauth');
    if (!recipeId) return;
    const status = searchParams.get('status');
    const detail = searchParams.get('detail');
    const key = `${recipeId}|${status || ''}|${detail || ''}`;
    if (oauthHandled.current === key) return;
    oauthHandled.current = key;

    const rest = new URLSearchParams(searchParams);
    rest.delete('oauth');
    rest.delete('status');
    rest.delete('detail');
    const qs = rest.toString();
    navigate(`${routerLocation.pathname}${qs ? `?${qs}` : ''}`, { replace: true });

    // Both outcomes refresh the gallery — success shows the active connection,
    // failure re-renders current state — and name the recipe from the fresh
    // registry rather than the raw id.
    void (async () => {
      const freshRecipes = await load();
      const name = freshRecipes.find((r) => r.id === recipeId)?.service || recipeId;
      if (status === 'failed') {
        setOauthFailure({ service: recipeId, detail });
        onToast(`${name} couldn't be connected`, 'danger');
      } else {
        setOauthFailure(null);
        onToast(`${name} connected`);
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- one-shot resolution of the callback params
  }, [searchParams]);

  const connectedByService = new Map(connections.map((c) => [c.service, c]));
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

  // Reauthorization (add-connection-oauth 4.2): same consent hand-off as
  // connect — the server returns the authorize URL and the browser goes
  // around the provider; the callback replaces the token set in place.
  const handleReauthorize = async (c: ApiConnection) => {
    if (reauthorizingId) return;
    setReauthorizingId(c.id);
    const name = serviceName(c);
    try {
      const res = await connectionsApi.reauthorize(ws, c.id);
      if (res?.authorize_url) {
        window.location.assign(res.authorize_url);
        return; // navigation in flight
      }
      onToast(`No authorization URL was returned for ${name}`, 'danger');
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to start reauthorization for ${name}`), 'danger');
    } finally {
      setReauthorizingId(null);
    }
  };

  return (
    <div className="max-w-xl space-y-8" data-od-id="pane-integrations" data-testid="pane-integrations">
      {/* OAuth callback failure — inline and dismissible; success needs no
          notice beyond the toast, the refreshed list shows the connection. */}
      {oauthFailure && (
        <div
          role="alert"
          data-testid="oauth-callback-failure"
          className="flex items-start gap-2 rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] px-3 py-2.5"
        >
          <Icon name="alert" size={14} className="mt-0.5 shrink-0 text-danger" />
          <div className="min-w-0 flex-1">
            <p className="text-[13px] font-medium text-danger">
              {recipes.find((r) => r.id === oauthFailure.service)?.service || oauthFailure.service}{' '}
              couldn't be connected
            </p>
            {oauthFailure.detail && (
              <p className="mt-0.5 text-[12px] leading-4 text-danger" data-testid="oauth-callback-failure-detail">
                {oauthFailure.detail}
              </p>
            )}
          </div>
          <button
            type="button"
            data-testid="btn-oauth-failure-dismiss"
            aria-label="Dismiss"
            onClick={() => setOauthFailure(null)}
            className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-danger transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_12%,transparent)]"
          >
            <Icon name="x" size={13} />
          </button>
        </div>
      )}
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
                  Pick a service below and connect it — its tools become attachable to every agent.
                </p>
              </div>
            ) : (
              <div className="space-y-2.5">
                {connections.map((c) => {
                  const kind = connectionKind(c, recipes);
                  const st = connectionStatusView(c, kind);
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
                              : st.expired
                                ? 'bg-[color-mix(in_oklab,var(--warn)_14%,transparent)] text-[color-mix(in_oklab,var(--warn),black_25%)]'
                                : 'bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent'
                          )}
                        >
                          <Icon name={serviceIconKey(c.service, recipes.find((r) => r.id === c.service)?.icon)} size={16} />
                        </span>
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <p className="text-[14px] font-medium text-fg">{name}</p>
                            {/* Connection kind (add-connection-http): subtle
                                mono chip — MCP materialized server vs HTTP
                                verb tools. */}
                            <Chip mono>{connectionKindLabel(kind)}</Chip>
                            <Chip mono>{accessLevelLabel(c.access_level)}</Chip>
                            {/* add-recipe-base-url: the resolved origin rides
                                the card as a display-only mono chip — origins
                                are immutable after connect, so there is no
                                edit affordance anywhere. */}
                            {c.origin ? (
                              <span
                                data-testid={'connection-origin-' + c.id}
                                title="Connected origin — fixed after connect"
                              >
                                <Chip mono>{c.origin}</Chip>
                              </span>
                            ) : null}
                          </div>
                          <div className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-0.5">
                            <span
                              className={cx(
                                'inline-flex items-center gap-1.5 text-[11px]',
                                st.errored
                                  ? 'text-danger'
                                  : st.expired
                                    ? 'text-[color-mix(in_oklab,var(--warn),black_25%)]'
                                    : 'text-[color-mix(in_oklab,var(--success),black_25%)]'
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
                          {(st.errored || st.expired) && c.status_error && (
                            <p
                              className={cx(
                                'mt-0.5 truncate text-[11px]',
                                st.errored ? 'text-danger' : 'text-[color-mix(in_oklab,var(--warn),black_38%)]'
                              )}
                              title={c.status_error}
                            >
                              {c.status_error}
                            </p>
                          )}
                        </div>
                        <div className="flex shrink-0 items-center gap-1.5">
                          {/* Expired OAuth token — recovery is re-consent, not
                              a re-probe of the dead token (D6). */}
                          {st.expired && writer && (
                            <button
                              type="button"
                              data-testid={'btn-connection-reauthorize-' + c.id}
                              onClick={() => void handleReauthorize(c)}
                              disabled={reauthorizingId === c.id}
                              className="h-8 rounded-md bg-accent px-2.5 text-[12px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-50"
                            >
                              {reauthorizingId === c.id ? 'Redirecting…' : 'Reauthorize'}
                            </button>
                          )}
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

          {/* Gallery — every recipe, ready or needing its one-time instance
              app setup (the dialog guides that setup inline). */}
          {recipes.length > 0 && (
            <section data-testid="recipes-available">
              <SectionLabel>Services</SectionLabel>
              <div className="space-y-2.5">
                {recipes.map((r) => {
                  const conn = connectedByService.get(r.id);
                  const kind = recipeKind(r);
                  const needsSetup = r.auth_kind === 'oauth' && r.availability !== 'available';
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
                        <div className="flex flex-wrap items-center gap-2">
                          <p className="text-[14px] font-medium text-fg">{r.service}</p>
                          <Chip mono>{connectionKindLabel(kind)}</Chip>
                        </div>
                        {/* MCP recipes show the materialized server's
                            transport; HTTP recipes name their declared verb
                            surface instead (add-connection-http) — the
                            transport line would be empty for them. */}
                        {kind === 'http' ? (
                          <p className="truncate font-mono text-[11px] text-muted">
                            {verbSurfaceCopy(r) || r.base_url || ''}
                          </p>
                        ) : (
                          <p className="truncate font-mono text-[11px] text-muted">{r.transport}</p>
                        )}
                        {needsSetup && (
                          <p className="text-[11px] leading-4 text-[color-mix(in_oklab,var(--warn),black_25%)]" data-testid={'recipe-setup-needed-' + r.id}>
                            One-time instance setup needed{r.auth_kind === 'oauth' ? ' — signs in with ' + r.service + '.' : '.'}
                          </p>
                        )}
                      </div>
                      {conn ? (
                        <span className="flex shrink-0 items-center gap-1.5 text-[12px] text-[color-mix(in_oklab,var(--success),black_25%)]">
                          <Icon name="check" size={13} /> Connected
                        </span>
                      ) : writer ? (
                        <div className="flex shrink-0 items-center gap-1.5">
                          {r.auth_kind === 'oauth' && !needsSetup && isAdmin && (
                            <button
                              type="button"
                              data-testid={'btn-app-settings-' + r.id}
                              onClick={() => setEditingApp(r)}
                              className="h-8 rounded-md px-2 text-[11px] font-medium text-muted transition-colors hover:text-fg"
                            >
                              App settings
                            </button>
                          )}
                          <button
                            type="button"
                            data-testid={'btn-integrate-' + r.id}
                            onClick={() => setConnecting(r)}
                            className="h-8 shrink-0 rounded-md bg-accent px-3.5 text-[12px] font-semibold text-accenton transition-colors hover:opacity-90"
                          >
                            Integrate
                          </button>
                        </div>
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
          onAppSaved={() => void load()}
          onToast={onToast}
        />
      )}

      {editingApp && (
        <ConnectServiceDialog
          recipe={editingApp}
          tenant={tenant}
          editApp
          onClose={() => setEditingApp(null)}
          onAppSaved={() => void load()}
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
            {/* Kind-aware cascade copy (add-connection-http): HTTP-kind
                connections have no materialized server to remove. */}
            {connectionKind(disconnecting, recipes) === 'http'
              ? `Disconnecting removes the ${serviceName(disconnecting)} connection and the stored token — the token cannot be recovered.`
              : `Disconnecting removes the ${serviceName(disconnecting)} connection, its materialized MCP server, and the stored token — the token cannot be recovered.`}
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

