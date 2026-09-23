import { useEffect, useState } from "react";
import { cx } from "../lib/helpers";
import { Chip } from "../components/ui/Chip";
import { Toggle } from "../components/ui/Toggle";
import { labelCls } from "../components/ui/constants";
import { Icon } from "../components/ui/Icon";
import {
  accessLevelLabel,
  connectionAttachId,
  connectionKind,
  connectionKindLabel,
  connectionServiceName,
  connectionStatusView,
  connectionTierChips,
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
 * entries over the existing `enabled_mcps` attach mechanism — the toggle
 * stores the materialized server id, or the raw connection id for HTTP-kind
 * connections (add-connection-http: they contribute verb tools directly and
 * have no server row) — with the access level shown per attachment. Managed
 * servers also appear in the MCP Servers section below by design
 * (materialization, MCP kind only); this section adds the service identity
 * and access level. The read rides workspace membership and is optional
 * context: a failed fetch leaves the section out, never blocking agent
 * configuration.
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
          const kind = connectionKind(c, recipes);
          // Pausing is a materialized-server switch — HTTP-kind rows carry
          // server_enabled: false structurally, not as a workspace pause.
          const paused = kind === 'mcp' && c.server_enabled === false;
          const st = connectionStatusView(c, kind);
          const attachId = connectionAttachId(c);
          const attached = enabledMcps.includes(attachId);
          // add-integration-authority 3.1: the API-projected read/write tool
          // split beside the access level — what Members can and cannot drive
          // through this attachment. Hidden when the backend projected no
          // tiers; the numbers are never computed here.
          const tierChips = connectionTierChips(c.tier_counts);
          return (
            <div
              key={c.id}
              data-testid={'agent-connection-' + c.id}
              className={cx('rounded-md border border-line px-3 py-2.5', paused && 'opacity-70')}
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
                    <Chip mono>{connectionKindLabel(kind)}</Chip>
                    <Chip mono>{accessLevelLabel(c.access_level)}</Chip>
                    {tierChips.length > 0 && (
                      <span
                        className="inline-flex items-center gap-1"
                        data-testid={'agent-connection-tiers-' + c.id}
                      >
                        {tierChips.map((label) => (
                          <Chip key={label} mono>
                            {label}
                          </Chip>
                        ))}
                      </span>
                    )}
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
                  onChange={() => onToggle(attachId)}
                />
              </div>
              {attached && paused && (
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
