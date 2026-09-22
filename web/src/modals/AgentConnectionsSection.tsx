import { useEffect, useState } from "react";
import { cx } from "../lib/helpers";
import { Chip } from "../components/ui/Chip";
import { Toggle } from "../components/ui/Toggle";
import { labelCls } from "../components/ui/constants";
import { Icon } from "../components/ui/Icon";
import {
  accessLevelLabel,
  connectionServiceName,
  connectionStatusView,
  connectionsApi,
  serviceIconKey,
  type ApiConnection,
  type ApiIntegrationRecipe,
} from "../lib/connectionsApi";

export interface AgentConnectionsSectionProps {
  /** Workspace slug the connections live in. */
  targetWsId: string;
  /** The agent draft's MCP opt-ins (materialized server UUIDs). */
  enabledMcps: string[];
  /** Toggles a server UUID in the draft — the modal's existing opt-in handler. */
  onToggle: (serverId: string) => void;
}

/**
 * Integrations section of the agent config Capabilities tab
 * (add-workspace-connections 4.4): managed connections listed as attachable
 * entries over the existing `enabled_mcps` attach mechanism — a connection's
 * materialized server id is exactly what the toggle stores — with the access
 * level shown per attachment. Managed servers also appear in the MCP Servers
 * section below by design (materialization); this section adds the service
 * identity and access level. The read rides workspace membership and is
 * optional context: a failed fetch leaves the section out, never blocking
 * agent configuration.
 */
export function AgentConnectionsSection({ targetWsId, enabledMcps, onToggle }: AgentConnectionsSectionProps) {
  const [connections, setConnections] = useState<ApiConnection[]>([]);
  const [recipes, setRecipes] = useState<ApiIntegrationRecipe[]>([]);

  useEffect(() => {
    let mounted = true;
    if (!targetWsId) return;
    // Both reads ride workspace membership; either failing just leaves the
    // section out (or with raw ids) — never blocking agent configuration.
    connectionsApi
      .list(targetWsId)
      .then((res) => {
        if (mounted && res?.connections) setConnections(res.connections);
      })
      .catch(() => {});
    connectionsApi
      .recipes(targetWsId)
      .then((res) => {
        if (mounted && res?.recipes) setRecipes(res.recipes);
      })
      .catch(() => {});
    return () => {
      mounted = false;
    };
  }, [targetWsId]);

  if (connections.length === 0) return null;

  return (
    <div data-testid="agent-connections-section">
      <span className={labelCls}>Integrations</span>
      <div className="space-y-2">
        {connections.map((c) => {
          const st = connectionStatusView(c);
          const serverId = c.server_id || '';
          const attached = Boolean(serverId) && enabledMcps.includes(serverId);
          return (
            <div
              key={c.id}
              data-testid={'agent-connection-' + c.id}
              className={cx(
                'rounded-md border border-line px-3 py-2.5',
                c.server_enabled === false && 'opacity-70'
              )}
            >
              <div className="flex items-center gap-3">
                <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-[color-mix(in_oklab,var(--accent)_12%,transparent)] text-accent">
                  <Icon name={serviceIconKey(c.service, recipes.find((r) => r.id === c.service)?.icon)} size={15} />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <p className="truncate text-[13px] font-medium text-fg">
                      {connectionServiceName(c, recipes)}
                    </p>
                    <Chip mono>{accessLevelLabel(c.access_level)}</Chip>
                    <span
                      className={cx(
                        'inline-flex items-center gap-1.5 text-[11px]',
                        st.errored
                          ? 'text-danger'
                          : st.expired
                            ? 'text-[color-mix(in_oklab,var(--warn),black_25%)]'
                            : 'text-[color-mix(in_oklab,var(--success),black_25%)]'
                      )}
                      data-testid={'agent-connection-status-' + c.id}
                    >
                      <span className={cx('h-1.5 w-1.5 rounded-full', st.dot)} />
                      {st.label}
                    </span>
                  </div>
                </div>
                <Toggle
                  on={attached}
                  label={'Opt this agent into ' + connectionServiceName(c, recipes)}
                  onChange={() => serverId && onToggle(serverId)}
                />
              </div>
              {attached && c.server_enabled === false && (
                <p
                  className="mt-1.5 flex items-center gap-1 text-[11px] leading-4 text-[color-mix(in_oklab,var(--warn),black_38%)]"
                  data-testid={'agent-connection-paused-warn-' + c.id}
                >
                  <Icon name="alert" size={11} />
                  Paused in Settings → Integrations — it contributes no tools until resumed.
                </p>
              )}
              {attached && st.expired && (
                <p
                  className="mt-1.5 flex items-center gap-1 text-[11px] leading-4 text-[color-mix(in_oklab,var(--warn),black_38%)]"
                  data-testid={'agent-connection-expired-warn-' + c.id}
                >
                  <Icon name="alert" size={11} />
                  Sign-in expired — reauthorize it in Settings → Integrations to restore its tools.
                </p>
              )}
            </div>
          );
        })}
      </div>
      <p className="mt-1.5 text-[11px] leading-4 text-muted">
        Managed service connections attach like MCP servers — the toggle stores the connection for this agent on save.
        Manage or disconnect them in Settings → Integrations.
      </p>
    </div>
  );
}
