import { useEffect, useRef, useState } from "react";
import type { ChangeEvent, ReactElement } from "react";

import { PaneHeader } from "../../components";
import BrowserUsePane from "./components/BrowserUsePane";
import ComputerUsePane from "./components/ComputerUsePane";
import { Icon } from "../../components/ui/Icon";
import { Input } from "../../components/ui/Input";
import { MicroLabel } from "../../components/ui/MicroLabel";
import { cn } from "../../lib/cn";
import { useTablist } from "../../lib/useTablist";
import { useNavStore, type SettingsPaneRequest } from "../../stores/nav";
import IntegrationsPane from "../integrations/IntegrationsPane";
import ChannelsPane from "./components/ChannelsPane";
import HooksPane from "./components/HooksPane";
import McpPane from "./components/McpPane";
import NotificationsPane from "./components/NotificationsPane";
import ProvidersPane from "./components/ProvidersPane";
import SkillsPane from "./components/SkillsPane";
import ToolsPane from "./components/ToolsPane";
import type { PaneKey, SettingsPaneConfig, SettingsPaneSection } from "./types";

/**
 * Sidebar sections in the add-integrations nav layout: the app-level
 * surfaces (Notifications) lead under APP (add-desktop-notifications
 * design D7), followed by Providers under MODELS, Integrations under
 * INTEGRATIONS, and the automation surfaces (Skills, Tools, Hooks, MCP)
 * under AUTOMATION. Icons are kit Icon names.
 */
export const settingsPaneSections: SettingsPaneSection[] = [
  {
    id: "app",
    label: "APP",
    panes: [
      {
        key: "notifications",
        label: "Notifications",
        icon: "bell",
        description:
          "Desktop notifications for agent turns, attention requests, schedules, and groups.",
      },
    ],
  },
  {
    id: "models",
    label: "MODELS",
    panes: [
      {
        key: "providers",
        label: "Providers",
        icon: "sliders",
        description: "Add model providers and manage their API credentials.",
      },
    ],
  },
  {
    id: "integrations",
    label: "INTEGRATIONS",
    panes: [
      {
        key: "integrations",
        label: "Integrations",
        icon: "globe",
        description:
          "Connect external services — GitLab, GitHub, Google Workspace, and more.",
      },
      {
        key: "channels",
        label: "Channels",
        icon: "message",
        description:
          "The Telegram bot and webhook endpoints — the two inbound channel triggers.",
      },
      {
        key: "browseruse",
        label: "Browser Use",
        icon: "monitor",
        description:
          "The built-in Browser Use MCP server — engine settings, profiles, and profile import.",
      },
      {
        key: "computeruse",
        label: "Computer Use",
        icon: "eye",
        description:
          "The built-in Computer Use MCP server — OS permissions, driver status, and desktop-control state.",
      },
    ],
  },
  {
    id: "automation",
    label: "AUTOMATION",
    panes: [
      {
        key: "skills",
        label: "Skills",
        icon: "spark",
        description:
          "Directory-backed skills agents can use, with global toggles.",
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
      {
        key: "mcp",
        label: "MCP servers",
        icon: "plug",
        description: "Model Context Protocol servers exposing extra tools.",
      },
    ],
  },
];

/** Flat pane list in nav order, derived from the sections. */
export const settingsPanes: SettingsPaneConfig[] = settingsPaneSections.flatMap(
  (section: SettingsPaneSection): SettingsPaneConfig[] => section.panes
);

const paneComponents: Record<
  PaneKey,
  (props: { pane: SettingsPaneConfig }) => ReactElement
> = {
  providers: ProvidersPane,
  integrations: IntegrationsPane,
  channels: ChannelsPane,
  browseruse: BrowserUsePane,
  computeruse: ComputerUsePane,
  skills: SkillsPane,
  tools: ToolsPane,
  hooks: HooksPane,
  mcp: McpPane,
  notifications: NotificationsPane,
};

/**
 * Filters the sections down to panes whose label contains the query
 * (case-insensitively); sections with no matching panes drop out. An empty
 * (or whitespace-only) query returns the sections unchanged.
 */
function filterSections(
  sections: SettingsPaneSection[],
  query: string
): SettingsPaneSection[] {
  const needle = query.trim().toLowerCase();

  if (needle === "") {
    return sections;
  }

  return sections.flatMap(
    (section: SettingsPaneSection): SettingsPaneSection[] => {
      const panes = section.panes.filter((pane: SettingsPaneConfig): boolean =>
        pane.label.toLowerCase().includes(needle)
      );

      // Sections with no matching panes drop out of the nav entirely.
      return panes.length > 0 ? [{ ...section, panes }] : [];
    }
  );
}

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
// it), a grouped w-56 section nav with a label filter over a hairline, and
// a centered max-w-5xl content column whose PaneHeader names the active
// pane in the shared view-header style. Only the active pane mounts; panes
// render their own section headers and bodies below it.
export default function SettingsView(): ReactElement {
  const [active, setActive] = useState<PaneKey>("providers");
  const [query, setQuery] = useState<string>("");

  // A cross-menu open request (e.g. the MCP pane's "open settings" on the
  // Browser Use row) names one pane; the nonce lets the view apply each
  // request exactly once without consuming shared state.
  const requestedPane = useNavStore(
    (state: { settingsPaneRequest: SettingsPaneRequest | null }) =>
      state.settingsPaneRequest
  );

  const appliedRequest = useRef<number | null>(null);

  useEffect(() => {
    if (requestedPane === null) return;

    // Apply each request exactly once per pane navigation (the nonce makes
    // re-opening the same pane re-apply, and stale mounted settings views
    // applying the same request is harmless — they are hidden).
    if (appliedRequest.current === requestedPane.nonce) return;

    appliedRequest.current = requestedPane.nonce;
    // SAFETY: a settings pane request names one of the static config keys
    // (openSettingsPane callers pass pane keys, never arbitrary strings).
    setActive(requestedPane.pane as PaneKey);
  }, [requestedPane]);

  // SAFETY: `active` is always one of the static config keys, so the lookup
  // below never misses.
  const activePane = settingsPanes.find(
    (pane: SettingsPaneConfig): boolean => pane.key === active
  ) as SettingsPaneConfig;

  const visibleSections = filterSections(settingsPaneSections, query);

  // The nav is a real tablist (fix-ui-audit-findings 3.2, design D7b): the
  // shared pattern gives each pane row a roving tabindex and arrow/Home/End
  // movement (arrows stop at the ends; Home/End jump), and the active pane
  // body is the labelled panel.
  const { tab, panel } = useTablist<PaneKey>({
    idPrefix: "settings-nav",
    ids: visibleSections.flatMap((section: SettingsPaneSection): PaneKey[] =>
      section.panes.map((pane: SettingsPaneConfig): PaneKey => pane.key)
    ),
    active,
    onChange: setActive,
  });

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
        className="titlebar-drag od-scroll flex w-full shrink-0 flex-col gap-0.5 overflow-x-auto border-b border-linesoft py-3 md:w-56 md:flex-col md:overflow-x-hidden md:overflow-y-auto md:border-b-0 md:border-r md:py-3 md:px-0 lg:pt-10"
        role="tablist"
        aria-label="Settings sections"
        data-testid="settings-nav"
      >
        <div className="relative mx-2 mb-1 shrink-0">
          <Icon
            name="search"
            size={14}
            className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted"
          />
          <Input
            type="search"
            value={query}
            placeholder="Search panes"
            data-testid="settings-nav-search"
            onChange={(event: ChangeEvent<HTMLInputElement>): void =>
              setQuery(event.target.value)
            }
            className="pl-8 text-[13px]"
          />
        </div>

        {visibleSections.map((section: SettingsPaneSection) => (
          <div
            key={section.id}
            className="flex flex-col gap-0.5"
            data-testid={`settings-nav-section-${section.id}`}
          >
            <div className="px-3 pb-1 pt-2">
              <MicroLabel>{section.label}</MicroLabel>
            </div>
            {section.panes.map((entry: SettingsPaneConfig) => {
              const isActive = active === entry.key;

              return (
                <button
                  key={entry.key}
                  type="button"
                  {...tab(entry.key)}
                  data-testid={`settings-nav-item-${entry.key}`}
                  className={cn(
                    NAV_ROW,
                    isActive ? NAV_ROW_ACTIVE : NAV_ROW_IDLE
                  )}
                  onClick={() => setActive(entry.key)}
                >
                  <Icon
                    name={entry.icon}
                    size={15}
                    className={isActive ? "text-accenttext" : "text-muted"}
                  />
                  <span className="whitespace-nowrap">{entry.label}</span>
                </button>
              );
            })}
          </div>
        ))}
      </nav>

      {/* Content column: the pane body reads on the page surface; the header
          names the pane in the shared PaneHeader treatment — the same style
          as the knowledge/scheduler views — and the muted line carries its
          description. */}
      <div className="titlebar-drag od-scroll min-w-0 flex-1 overflow-y-auto p-4 md:p-6 lg:p-8 lg:pt-10">
        <div className="mx-auto max-w-5xl">
          <div className="mb-6">
            <PaneHeader
              title={activePane.label}
              description={activePane.description}
            />
          </div>

          <div {...panel(active)}>
            <ActivePane pane={activePane} />
          </div>
        </div>
      </div>
    </div>
  );
}
