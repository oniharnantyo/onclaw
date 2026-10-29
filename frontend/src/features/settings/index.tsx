import { useEffect, useState } from "react";
import type { ChangeEvent, ReactElement } from "react";

import { Icon } from "../../components/ui/Icon";
import { Input } from "../../components/ui/Input";
import { MicroLabel } from "../../components/ui/MicroLabel";
import { cn } from "../../lib/cn";
import { useNavStore } from "../../stores/nav";
import IntegrationsPane from "../integrations/IntegrationsPane";
import HooksPane from "./components/HooksPane";
import McpPane from "./components/McpPane";
import ProvidersPane from "./components/ProvidersPane";
import SkillsPane from "./components/SkillsPane";
import ToolsPane from "./components/ToolsPane";
import type { PaneKey, SettingsPaneConfig, SettingsPaneSection } from "./types";

/**
 * Sidebar sections in the add-integrations nav layout: Providers under
 * MODELS, Integrations under INTEGRATIONS, and the automation surfaces
 * (Skills, Tools, Hooks, MCP) under AUTOMATION. Icons are kit Icon names.
 */
export const settingsPaneSections: SettingsPaneSection[] = [
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
  skills: SkillsPane,
  tools: ToolsPane,
  hooks: HooksPane,
  mcp: McpPane,
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
// a centered max-w-5xl content column whose h2 names the active pane. Only
// the active pane mounts; panes render their own section headers and bodies
// below the h2.
export default function SettingsView(): ReactElement {
  const [active, setActive] = useState<PaneKey>("providers");
  const [query, setQuery] = useState<string>("");

  // SAFETY: `active` is always one of the static config keys, so the lookup
  // below never misses.
  const activePane = settingsPanes.find(
    (pane: SettingsPaneConfig): boolean => pane.key === active
  ) as SettingsPaneConfig;

  const visibleSections = filterSections(settingsPaneSections, query);

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
                  role="tab"
                  aria-selected={isActive}
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
                    className={isActive ? "text-accent" : "text-muted"}
                  />
                  <span className="whitespace-nowrap">{entry.label}</span>
                </button>
              );
            })}
          </div>
        ))}
      </nav>

      {/* Content column: the pane body reads on the page surface; the h2
          names the pane and the muted line carries its description, per the
          settings-nav spec. */}
      <div className="od-scroll min-w-0 flex-1 overflow-y-auto p-4 md:p-6 lg:p-8 lg:pt-10">
        <div className="mx-auto max-w-5xl">
          <div className="mb-6">
            <h2
              className="text-xl font-semibold text-fg"
              data-testid="settings-pane-title"
            >
              {activePane.label}
            </h2>
            <p className="mt-1 text-[13px] text-muted">
              {activePane.description}
            </p>
          </div>

          <ActivePane pane={activePane} />
        </div>
      </div>
    </div>
  );
}
