import {
  Bot,
  CalendarClock,
  MessagesSquare,
  PanelLeftClose,
  Settings,
} from "lucide-react";
import { useEffect, type ReactElement } from "react";

import { IconButton } from "./components";
import { ThemeCycleButton } from "./components/ui/ThemeCycleButton";
import { Toasts } from "./components/ui/Toasts";
import AgentsView from "./features/agents";
import ChatView from "./features/chat";
import { SessionRail } from "./features/chat/components/SessionRail";
import SettingsView from "./features/settings";
import SchedulesView from "./features/scheduler";
import { cn } from "./lib/cn";
import { subscribeSettingsShortcut } from "./settingsShortcut";
import { useNavStore, type Menu } from "./stores/nav";
import { usePlatformStore } from "./stores/platform";

interface MenuItem {
  key: Menu;
  label: string;
  icon: typeof Bot;
}

const menus: MenuItem[] = [
  { key: "chat", label: "Chat", icon: MessagesSquare },
  { key: "agents", label: "Agents", icon: Bot },
  { key: "schedules", label: "Schedules", icon: CalendarClock },
];

// Settings sits below the theme toggle in the desktop rail's bottom cluster;
// the mobile top bar keeps it in the horizontal nav after Chat and Agents.
const barMenus: MenuItem[] = [
  ...menus,
  { key: "settings", label: "Settings", icon: Settings },
];

// Rail row grammar, lifted from onclaw-v2's web/src/components/nav/Rail.tsx:
// flat rows whose active state is an accent tint + weight — the ink-black
// slab stays retired (design D1). The collapsed rail is narrower than
// onclaw's icon rail, so rows fill the width instead of fixing at w-11.
const RAIL_ROW =
  "relative flex h-11 items-center rounded-[12px] transition-colors";

const RAIL_ROW_EXPANDED = "w-[calc(100%-16px)] mx-2 px-3 gap-3 text-left";

const RAIL_ROW_COLLAPSED = "w-full justify-center";

const RAIL_ROW_ACTIVE =
  "bg-[color-mix(in_oklab,var(--accent)_16%,transparent)] font-semibold text-accenttext";

const RAIL_ROW_IDLE =
  "text-muted hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2";

function Wordmark({ className = "" }: { className?: string }): ReactElement {
  return (
    <div className={`font-display font-bold tracking-tight ${className}`}>
      buddies
    </div>
  );
}

function NavItems({
  orientation,
  collapsed = false,
}: {
  orientation: "rail" | "bar";
  collapsed?: boolean;
}): ReactElement {
  const menu = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.menu
  );

  const setMenu = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.setMenu
  );

  if (orientation === "rail") {
    return (
      <div className="flex w-full flex-col gap-0.5">
        {menus.map((m: MenuItem) => {
          const active = menu === m.key;

          return (
            <button
              key={m.key}
              type="button"
              aria-current={active ? "page" : undefined}
              // Icon-only when collapsed, so the name moves onto the button.
              aria-label={collapsed ? m.label : undefined}
              title={collapsed ? m.label : undefined}
              className={cn(
                RAIL_ROW,
                collapsed ? RAIL_ROW_COLLAPSED : RAIL_ROW_EXPANDED,
                active ? RAIL_ROW_ACTIVE : RAIL_ROW_IDLE
              )}
              onClick={() => setMenu(m.key)}
            >
              <m.icon
                aria-hidden="true"
                strokeWidth={1.7}
                className="size-[19px] shrink-0"
              />
              {!collapsed && (
                <span className="min-w-0 flex-1 truncate text-[13px]">
                  {m.label}
                </span>
              )}
            </button>
          );
        })}
      </div>
    );
  }

  return (
    <nav className="flex flex-1 items-center gap-1 px-2 lg:hidden">
      {barMenus.map((m: MenuItem) => {
        const active = menu === m.key;

        return (
          <button
            key={m.key}
            type="button"
            aria-label={m.label}
            aria-current={active ? "page" : undefined}
            className={cn(
              "flex h-8 items-center gap-1.5 rounded-md px-2 text-[12px] transition-colors",
              active
                ? RAIL_ROW_ACTIVE
                : "text-muted hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
            )}
            onClick={() => setMenu(m.key)}
          >
            <m.icon aria-hidden="true" className="size-4" />
            {/* Labels return at md; below that the bar also carries the theme
                toggle, and 375px cannot fit three labeled buttons plus it. */}
            <span className="hidden md:inline">{m.label}</span>
          </button>
        );
      })}
    </nav>
  );
}

// Bottom cluster of the desktop rail: theme cycle control with Settings
// underneath, styled like the nav rows so it reads as navigation, not a
// footer link.
function RailSettingsItem({
  collapsed = false,
}: {
  collapsed?: boolean;
}): ReactElement {
  const menu = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.menu
  );

  const setMenu = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.setMenu
  );

  const active = menu === "settings";

  return (
    <button
      type="button"
      aria-current={active ? "page" : undefined}
      aria-label={collapsed ? "Settings" : undefined}
      title={collapsed ? "Settings" : undefined}
      data-testid="rail-settings"
      className={cn(
        RAIL_ROW,
        collapsed ? RAIL_ROW_COLLAPSED : RAIL_ROW_EXPANDED,
        active ? RAIL_ROW_ACTIVE : RAIL_ROW_IDLE
      )}
      onClick={() => setMenu("settings")}
    >
      <Settings
        aria-hidden="true"
        strokeWidth={1.7}
        className="size-[19px] shrink-0"
      />
      {!collapsed && (
        <span className="min-w-0 flex-1 truncate text-[13px]">Settings</span>
      )}
    </button>
  );
}

// Shell routes from the shared nav store so any menu can switch views (e.g.
// the agents menu's "open chat" affordance flips `menu` to "chat").
// The content island runs the full work-area height (no strip row above
// it): at lg+ a quiet strip band overlays the island's top instead —
// pointer-events-none, no copy on chat (the header below carries the live
// state), only the view title on agents, schedules, and settings — because
// the window's top 25px is
// the macOS invisible titlebar (InvisibleTitleBarHeight in main.go) whose
// first click is consumed as a drag
// (nothing interactive may sit there; every island column clears the band
// with its own top padding). On the web there are no traffic lights: the
// wordmark leads the strip. Settings renders inside the content island
// with the persistent navigation rail visible.
function Shell(): ReactElement {
  const menu = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.menu
  );

  const isDesktopApp = usePlatformStore(
    (s: ReturnType<typeof usePlatformStore.getState>) => s.isDesktopApp
  );

  const railCollapsed = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.railCollapsed
  );

  const toggleRail = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.toggleRail
  );

  const syncRailPref = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.syncRailPref
  );

  // The native menu's Settings… item (⌘,) navigates here through the shared
  // nav store (design D5); the shell owns the subscription's lifetime.
  useEffect(() => subscribeSettingsShortcut(), []);

  // The rail preference re-syncs from storage on every shell mount, so the
  // rendered width always agrees with the stored frame.
  useEffect((): void => {
    syncRailPref();
  }, [syncRailPref]);

  return (
    <div className="flex h-dvh w-full flex-col gap-2 p-2">
      {/* Title bar (web at every width; desktop below lg) */}
      <header
        className={cn(
          "glass-chrome flex h-11 shrink-0 select-none items-center gap-2 rounded-lg border border-line bg-surface",
          isDesktopApp ? "ps-20" : "ps-4",
          "lg:hidden"
        )}
      >
        {!isDesktopApp && <Wordmark className="text-lg" />}
        <NavItems orientation="bar" />
        <div className="ml-auto pr-2">
          <ThemeCycleButton variant="icon" testId="theme-cycle" />
        </div>
      </header>

      <div className="relative flex min-h-0 flex-1 gap-2">
        {/* Desktop rail (design D2): collapsible to an icon-only 80px —
         * wide enough that the macOS traffic lights, which overlay its
         * top-left corner, stay inside the island. The preference rides
         * the nav store (buddies.railCollapsed). Surface/ring grammar: a
         * flat bg-surface panel with a hairline border. Its header clears
         * the native invisible-titlebar band (pt-6: the first 25px of the
         * window drag); the collapse toggle sits on the rail header's
         * right, and when collapsed the row stays as
         * a quiet spacer — the expand control moves above the sessions
         * rail's SESSIONS label (SessionRail hosts it). */}
        <aside
          className={cn(
            "glass-chrome hidden shrink-0 flex-col rounded-lg border border-line bg-surface lg:flex",
            railCollapsed ? "w-20" : "w-56"
          )}
        >
          <div className="flex items-center justify-end p-2 pt-6">
            {!railCollapsed && (
              <IconButton
                label="Collapse navigation"
                size="sm"
                testId="btn-toggle-rail"
                icon={<PanelLeftClose aria-hidden="true" className="size-4" />}
                onClick={toggleRail}
              />
            )}
          </div>
          <nav className="flex-1 pt-2">
            <NavItems orientation="rail" collapsed={railCollapsed} />
          </nav>
          <div
            className={cn(
              "flex flex-col gap-1 border-t border-linesoft p-2",
              railCollapsed && "items-center px-1"
            )}
          >
            {/* Three-state cycle control (task 2.5): the icon mirrors the
             * current preference and the label/aria pair agree on it, so
             * 'system' is reachable. Collapsed uses the icon variant — the
             * rail variant's labeled row needs the full 224px. */}
            <ThemeCycleButton
              variant={railCollapsed ? "icon" : "rail"}
              expanded={!railCollapsed}
              testId="rail-theme"
            />
            <RailSettingsItem collapsed={railCollapsed} />
          </div>
        </aside>

        {/* The content island runs the full work-area height. Chat mounts
         * the sessions rail INSIDE it as its left column — the island's
         * border separates the two (design D3's chrome-level rail is
         * retired); the rail's store keeps the selection. */}
        <main
          className="glass-surface min-h-0 min-w-0 flex-1 overflow-auto rounded-lg border border-line bg-surface"
          data-active-menu={menu}
        >
          {menu === "chat" ? (
            <div className="flex h-full min-w-0">
              <SessionRail />
              <ChatView />
            </div>
          ) : menu === "agents" ? (
            <AgentsView />
          ) : menu === "schedules" ? (
            <SchedulesView />
          ) : (
            <SettingsView />
          )}
        </main>

        {/* The strip overlays the island's top band at lg+ as a read-only
         * desk copy (pointer-events-none; nothing interactive may sit in
         * the titlebar drag zone it overlays). Textless on chat — the
         * header below carries the live state — only the view title on
         * agents, schedules, and settings. Below lg the header bar above takes over; on the web
         * the wordmark leads. */}
        <div
          data-testid="titlebar-strip"
          className={cn(
            "pointer-events-none absolute inset-x-0 top-0 z-10 hidden h-10 select-none items-center gap-2 lg:flex",
            isDesktopApp ? (railCollapsed ? "ps-24" : "ps-20") : "ps-4"
          )}
        >
          {!isDesktopApp && <Wordmark className="text-lg" />}
          {menu === "agents" && (
            <div className="font-display font-bold">Agents</div>
          )}
          {menu === "schedules" && (
            <div className="font-display font-bold">Schedules</div>
          )}
          {menu === "settings" && (
            <div className="font-display font-bold">Settings</div>
          )}
        </div>
      </div>
    </div>
  );
}

function App(): ReactElement {
  return (
    <>
      <Shell />
      <Toasts />
    </>
  );
}

export default App;
