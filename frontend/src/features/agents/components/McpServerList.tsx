import type { ReactElement } from "react";

import type { MCPServer } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { Chip } from "../../../components/ui/Chip";
import { Toggle } from "../../../components/ui/Toggle";
import { cn } from "../../../lib/cn";

export interface McpServerListProps {
  servers: MCPServer[];
  /**
   * The agent's stored server list (agent.MCPServers semantics): empty means
   * every server, a non-empty list restricts the agent to the named ids.
   */
  selected: string[];
  /** Receives the next stored list; it persists on save. */
  onChange: (next: string[]) => void;
  /** Row-toggle test ids: `<prefix>-<server id>`. */
  testIdPrefix: string;
}

/**
 * MCP server rows for an agent's configuration (onclaw-v2 parity, design
 * D3): one bordered row per workspace-registered server — name, mono
 * transport line, a `paused` chip when the server is off at the workspace
 * level, and a right-aligned Toggle for this agent's access. Shared by the
 * config modal's Capabilities tab and the create wizard's Capabilities step.
 *
 * The stored list is an allowlist with the same reading as Tools and Skills:
 * empty means every server, so the default renders every toggle on and the
 * first toggle off materializes the remaining ids. An empty list cannot mean
 * "none", so the last server left on is pinned (`always on`, toggle
 * disabled) — pausing the server in Settings is how it leaves every agent.
 *
 * The list is display-only until Save: the toggles edit a local selection
 * that rides the agent update, mirroring the tool and skill chips.
 */
export function McpServerList({
  servers,
  selected,
  onChange,
  testIdPrefix,
}: McpServerListProps): ReactElement {
  const value =
    selected.length === 0
      ? servers.map((s: MCPServer): string => s.ID)
      : selected;

  const onCount = servers.filter((s: MCPServer): boolean =>
    value.includes(s.ID)
  ).length;

  const toggle = (id: string): void => {
    onChange(
      value.includes(id)
        ? value.filter((m: string): boolean => m !== id)
        : [...value, id]
    );
  };

  return (
    <ul className="mt-2 divide-y divide-linesoft rounded-md border border-line">
      {servers.map((s: MCPServer): ReactElement => {
        const on = value.includes(s.ID);
        const pinned = on && onCount === 1;

        return (
          <li
            key={s.ID}
            className={cn(
              "flex items-center gap-3 px-3 py-2.5",
              !s.Enabled && "opacity-70"
            )}
          >
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="truncate text-[13px] font-medium text-fg">
                  {s.Name}
                </span>
                {!s.Enabled && <Chip>paused</Chip>}
                {pinned && <Chip>always on</Chip>}
              </div>
              <p className="truncate font-mono text-[11px] text-muted">
                {s.Transport}
              </p>
            </div>
            <Toggle
              on={on}
              disabled={pinned}
              onChange={(): void => toggle(s.ID)}
              label={`Enable ${s.Name} for this agent`}
              testId={`${testIdPrefix}-${s.ID}`}
            />
          </li>
        );
      })}
    </ul>
  );
}
