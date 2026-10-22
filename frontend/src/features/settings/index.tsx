import { useEffect, useState } from "react";
import type { ReactElement } from "react";

import { Icon } from "../../components/ui/Icon";
import { cn } from "../../lib/cn";
import { useNavStore } from "../../stores/nav";
import HooksPane from "./components/HooksPane";
import McpPane from "./components/McpPane";
import ProvidersPane from "./components/ProvidersPane";
import SkillsPane from "./components/SkillsPane";
import ToolsPane from "./components/ToolsPane";
import type { PaneKey, SettingsPaneConfig } from "./types";

/**
 * Sidebar entries in onclaw-v2's flat section order (no grouping, no
 * search — the list is short enough to scan). Icons are kit Icon names.
 */
export const settingsPanes: SettingsPaneConfig[] = [
  {
    key: "providers",
    label: "Providers",
    icon: "sliders",
    description: "Add model providers and manage their API credentials.",
  },
  {
    key: "mcp",
    label: "MCP servers",
    icon: "plug",
    description: "Model Context Protocol servers exposing extra tools.",
  },
  {
    key: "skills",
    label: "Skills",
    icon: "spark",
    description: "Directory-backed skills agents can use, with global toggles.",
  },
  {
    key: "tools",
    label: "Tools",
    icon: "zap",
    description: "The tool catalog with a global enable/disable per tool.",
  },
  {
    key: "hooks",
    label: "Hooks",
    icon: "activity",
    description: "Lifecycle hooks that run commands on gateway events.",
  },
];

const paneComponents: Record<
  PaneKey,
  (props: { pane: SettingsPaneConfig }) => ReactElement
> = {
  providers: ProvidersPane,
  skills: SkillsPane,
  tools: ToolsPane,
  hooks: HooksPane,
  mcp: McpPane,
};

// Sidebar rows ride onclaw-v2's settings nav grammar: h-9 rounded-md rows
// whose active state is a 14% accent tint with medium-weight fg text and an
// accent-colored icon; idle rows are fg2 with a 6% ink hover.
const NAV_ROW =
  "mx-2 flex h-9 shrink-0 items-center gap-2.5 rounded-md px-3 text-left text-[13px] transition-colors";

const NAV_ROW_ACTIVE =
  "bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] font-medium text-fg";

const NAV_ROW_IDLE =
  "text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg";

// SettingsView is the settings menu as a full-bleed page in onclaw-v2's
// layout: an explicit Back header (the shell renders nothing else around
// it), a flat w-56 section nav over a hairline, and a centered max-w-5xl
// content column whose h2 names the active pane. Only the active pane
// mounts; panes render their own section headers and bodies below the h2.
export default function SettingsView(): ReactElement {
  const [active, setActive] = useState<PaneKey>("providers");

  // SAFETY: `active` is always one of the static config keys, so the lookup
  // below never misses.
  const activePane = settingsPanes.find(
    (pane: SettingsPaneConfig): boolean => pane.key === active
  ) as SettingsPaneConfig;

  // The store records the focused pane's label for cross-menu consumers.
  // The label is derived from `active`, so the effect follows it.
  useEffect(() => {
    useNavStore.getState().setSettingsPaneLabel(activePane.label);
  }, [active]);

  const ActivePane = paneComponents[active];

  return (
    <div
      className="flex h-full min-h-0 flex-1 flex-col overflow-hidden md:flex-row"
      data-testid="settings-view"
    >
      <nav
        className="od-scroll flex w-full shrink-0 flex-col gap-0.5 overflow-x-auto border-b border-linesoft py-3 md:w-56 md:flex-col md:overflow-x-hidden md:overflow-y-auto md:border-b-0 md:border-r md:py-3 md:px-0 lg:pt-10"
        role="tablist"
        aria-label="Settings sections"
        data-testid="settings-nav"
      >
        {settingsPanes.map((entry: SettingsPaneConfig) => {
          const isActive = active === entry.key;

          return (
            <button
              key={entry.key}
              type="button"
              role="tab"
              aria-selected={isActive}
              data-testid={`settings-nav-item-${entry.key}`}
              className={cn(NAV_ROW, isActive ? NAV_ROW_ACTIVE : NAV_ROW_IDLE)}
              onClick={() => setActive(entry.key)}
            >
              <Icon
                name={entry.icon}
                size={15}
                className={isActive ? "text-accent" : "text-muted"}
              />
              <span className="whitespace-nowrap">{entry.label}</span>
            </button>
          );
        })}
      </nav>

      {/* Content column: the pane body reads on the page surface; the h2
          names the pane exactly as onclaw-v2's content area does. */}
      <div className="od-scroll min-w-0 flex-1 overflow-y-auto p-4 md:p-6 lg:p-8 lg:pt-10">
        <div className="mx-auto max-w-5xl">
          <div className="mb-6">
            <h2
              className="text-xl font-semibold text-fg"
              data-testid="settings-pane-title"
            >
              {activePane.label}
            </h2>
          </div>

          <ActivePane pane={activePane} />
        </div>
      </div>
    </div>
  );
}
