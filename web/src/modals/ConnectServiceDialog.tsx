import { useEffect, useState } from "react";
import { cx } from "../lib/helpers";
import { Modal } from "../components/ui/Modal";
import { Chip } from "../components/ui/Chip";
import { Icon } from "../components/ui/Icon";
import { Toggle } from "../components/ui/Toggle";
import { inputCls, labelCls } from "../components/ui/constants";
import {
  accessLevelLabel,
  connectionsApi,
  type ApiConnection,
  type ApiIntegrationRecipe,
  type ConnectionAccessLevel,
} from "../lib/connectionsApi";
import { api, formatApiError, type ApiAgent } from "../lib/api";

export interface ConnectServiceDialogProps {
  recipe: ApiIntegrationRecipe;
  /** Workspace slug the connection is created in. */
  tenant: any;
  onClose: () => void;
  /** Fired once after a successful connect, before the hand-off view. */
  onConnected?: (connection: ApiConnection) => void;
  onToast?: (text: string, kind?: string) => void;
}

const FALLBACK_LEVELS: ConnectionAccessLevel[] = ['read_only', 'read_write'];

/**
 * Guided PAT connect flow (add-workspace-connections 4.2): the recipe's
 * token-creation steps, the access-level catalog preselected to the recipe's
 * first level (read-only), the token input, and the probe-gated Connect
 * action (the server probes before persisting — a failure stores nothing and
 * surfaces the upstream message here). Success hands off to per-agent
 * attachment riding the existing enabled_mcps patch on the materialized
 * server.
 */
export function ConnectServiceDialog({
  recipe,
  tenant,
  onClose,
  onConnected,
  onToast = () => {},
}: ConnectServiceDialogProps) {
  const ws = tenant?.sub || tenant?.id;
  const levels = recipe.access_levels.length ? recipe.access_levels : FALLBACK_LEVELS;

  const [accessLevel, setAccessLevel] = useState<ConnectionAccessLevel>(levels[0]);
  const [token, setToken] = useState('');
  const [connecting, setConnecting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Success hand-off state: the connected row plus the agents one may attach.
  const [connected, setConnected] = useState<ApiConnection | null>(null);
  const [agents, setAgents] = useState<ApiAgent[]>([]);

  useEffect(() => {
    if (!connected) return;
    let mounted = true;
    api.agents
      .list(ws)
      .then((res) => {
        if (mounted && res?.agents) setAgents(res.agents);
      })
      .catch(() => {});
    return () => {
      mounted = false;
    };
  }, [connected, ws]);

  const selectedScopes = recipe.scopes.find((s) => s.access_level === accessLevel)?.scopes || [];
  const serverId = connected?.server_id || null;

  const handleConnect = async () => {
    if (!token.trim() || connecting) return;
    setConnecting(true);
    setError(null);
    try {
      const res = await connectionsApi.connect(ws, {
        recipe_id: recipe.id,
        access_level: accessLevel,
        token: token.trim(),
      });
      const connection = res?.connection;
      if (connection) {
        setConnected(connection);
        onConnected?.(connection);
      } else {
        onClose();
      }
    } catch (err: unknown) {
      // Probe failures (upstream message), duplicate-service conflicts, and
      // integrations.write rejections all arrive as error envelopes.
      setError(formatApiError(err, `Couldn't connect ${recipe.service}`));
    } finally {
      setConnecting(false);
    }
  };

  // Attach/detach rides the existing agent MCP opt-in endpoint: the full
  // enabled_mcps array is PATCHed with the materialized server id added or
  // removed. A failure reverts the row and surfaces the envelope message.
  const toggleAttach = async (agent: ApiAgent) => {
    if (!serverId) return;
    const current = agent.enabled_mcps ?? [];
    const attached = current.includes(serverId);
    const next = attached ? current.filter((x) => x !== serverId) : [...current, serverId];
    setAgents((prev) =>
      prev.map((a) => (a.id === agent.id ? { ...a, enabled_mcps: next } : a))
    );
    try {
      await api.agents.patch(ws, agent.slug || agent.id, { enabled_mcps: next });
    } catch (err: unknown) {
      setAgents((prev) =>
        prev.map((a) => (a.id === agent.id ? { ...a, enabled_mcps: current } : a))
      );
      onToast(formatApiError(err, `Couldn't update ${agent.name}`), 'danger');
    }
  };

  return (
    <Modal
      title={connected ? `${recipe.service} connected` : `Connect ${recipe.service}`}
      onClose={onClose}
      odId="modal-connect-service"
      data-testid="modal-connect-service"
      footer={
        connected ? (
          <button
            type="button"
            onClick={onClose}
            data-testid="btn-connect-done"
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90"
          >
            Done
          </button>
        ) : (
          <>
            <button
              type="button"
              onClick={onClose}
              data-testid="btn-connect-cancel"
              className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={() => void handleConnect()}
              disabled={!token.trim() || connecting}
              data-testid="btn-connect-confirm"
              className="flex h-9 items-center gap-2 rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
            >
              {connecting && <Icon name="loader" size={13} className="animate-spin" />}
              {connecting ? 'Connecting…' : 'Connect'}
            </button>
          </>
        )
      }
    >
      {connected ? (
        <div className="p-5" data-testid="connect-success">
          <div className="flex items-start gap-3">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-[color-mix(in_oklab,var(--success)_15%,transparent)] text-[color-mix(in_oklab,var(--success),black_25%)]">
              <Icon name="check" size={16} />
            </span>
            <div className="min-w-0">
              <p className="text-[14px] font-medium text-fg">
                {recipe.service} is connected
                {connected.tool_count ? ` — ${connected.tool_count} ${connected.tool_count === 1 ? 'tool' : 'tools'} exposed` : ''}
                .
              </p>
              <p className="mt-1 text-[12px] leading-4 text-muted">
                Agents gain its tools only when attached — toggle the agents below, or do it later in their config.
              </p>
            </div>
          </div>

          <div className="mt-4 space-y-1.5" data-testid="connect-attach-list">
            {agents.length === 0 ? (
              <p className="text-[12px] text-muted">No agents yet — create one to put {recipe.service} to work.</p>
            ) : (
              agents.map((a) => {
                const attached = serverId ? (a.enabled_mcps ?? []).includes(serverId) : false;
                return (
                  <div
                    key={a.id}
                    data-testid={'connect-attach-' + (a.slug || a.id)}
                    className="flex items-center gap-3 rounded-md border border-line px-3 py-2"
                  >
                    <p className="min-w-0 flex-1 truncate text-[13px] font-medium text-fg">{a.name}</p>
                    <Toggle
                      on={attached}
                      label={'Attach ' + a.name}
                      onChange={() => void toggleAttach(a)}
                    />
                  </div>
                );
              })
            )}
          </div>
        </div>
      ) : (
        <div className="space-y-5 p-5">
          {/* Guided token-creation steps — real copy from the recipe. */}
          <div data-testid="connect-steps">
            <span className={labelCls}>Create a {recipe.service} token</span>
            <ol className="space-y-2">
              {recipe.steps.map((s, i) => (
                <li key={i} className="flex gap-2.5 text-[13px] leading-5 text-fg2">
                  <span className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] font-mono text-[11px] text-muted">
                    {i + 1}
                  </span>
                  <span className="min-w-0">
                    {s.title}
                    {s.detail && <span className="block text-[12px] leading-4 text-muted">{s.detail}</span>}
                    {s.url && (
                      <a
                        href={s.url}
                        target="_blank"
                        rel="noreferrer"
                        className="mt-0.5 inline-flex items-center gap-1 text-[12px] font-medium text-accent hover:underline"
                      >
                        Open {recipe.service} <Icon name="external-link" size={11} />
                      </a>
                    )}
                  </span>
                </li>
              ))}
            </ol>
          </div>

          {/* Access level — catalog-driven, first level (read-only) the
              flow default (D5). */}
          <div>
            <span className={labelCls}>Access level</span>
            <div className="flex flex-wrap gap-2">
              {levels.map((l) => {
                const on = accessLevel === l;
                return (
                  <button
                    key={l}
                    type="button"
                    aria-pressed={on}
                    data-testid={'connect-access-' + l}
                    onClick={() => setAccessLevel(l)}
                    className={cx(
                      'flex h-8 items-center gap-1.5 rounded-md border px-3 text-[12px] font-medium transition-colors',
                      on
                        ? 'border-accent bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg'
                        : 'border-line text-muted hover:border-[color-mix(in_oklab,var(--fg)_26%,transparent)] hover:text-fg2'
                    )}
                  >
                    {accessLevelLabel(l)}
                  </button>
                );
              })}
            </div>
            {/* Recommended scopes for the chosen level — the actual write
                gate rides the token's own scopes, so show what to pick. */}
            {selectedScopes.length > 0 && (
              <div className="mt-2 flex flex-wrap items-center gap-1.5" data-testid="connect-scopes">
                <span className="text-[11px] text-muted">Recommended scopes:</span>
                {selectedScopes.map((s) => (
                  <Chip key={s} mono>
                    {s}
                  </Chip>
                ))}
              </div>
            )}
          </div>

          <div>
            <label className={labelCls} htmlFor="connect-token">
              {recipe.service} access token
            </label>
            <input
              id="connect-token"
              type="password"
              autoComplete="off"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="Paste the token"
              data-testid="input-connect-token"
              className={cx(inputCls, 'font-mono')}
            />
            <p className="mt-1.5 text-[11px] leading-4 text-muted">
              Stored encrypted for this workspace and never shown again — only its last 4 characters.
            </p>
          </div>

          {error && (
            <p
              role="alert"
              data-testid="connect-error"
              className="flex items-start gap-1.5 rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] px-3 py-2 text-[12px] leading-4 text-danger"
            >
              <Icon name="alert" size={13} className="mt-0.5 shrink-0" />
              {error}
            </p>
          )}
        </div>
      )}
    </Modal>
  );
}
