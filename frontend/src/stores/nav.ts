/**
 * Cross-menu navigation state (shared contract): which menu is active and an
 * optional "open this agent's chat" target set by the agents/settings menus.
 * App.tsx reads `menu` for routing; the chat view consumes `chatTarget` and
 * clears it once handled. The nav rail's collapse preference lives here too,
 * because two distant consumers render from it: the shell sizes the rail,
 * and the collapsed state's expand control is hosted by the chat header.
 */
import { create, type StoreApi } from "zustand";

export type Menu = "chat" | "agents" | "schedules" | "knowledge" | "settings";

export interface ChatTarget {
  agentID: string;
  sessionID?: string;
}

// Shell UI preference, not settings-store data (same reasoning as
// window.json): localStorage under the shell's key, "1"/"0" values.
const RAIL_PREF_KEY = "buddies.railCollapsed";

function readRailPref(): boolean {
  try {
    return window.localStorage.getItem(RAIL_PREF_KEY) === "1";
  } catch {
    // No storage (or no window): default to expanded.
    return false;
  }
}

interface NavState {
  menu: Menu;
  /** The menu to return to from settings — whatever was active when
   * settings opened (the settings view's Back button reads it). */
  returnMenu: Menu;
  chatTarget: ChatTarget | null;
  /** Label of the settings pane in focus, published for cross-menu
   * consumers; the settings view writes it on mount and pane switch.
   * Defaults to the settings view's initial pane. */
  settingsPaneLabel: string;
  /** Whether the desktop nav rail is collapsed to icons. Initialized from
   * localStorage so a restored session opens with the stored width. */
  railCollapsed: boolean;
  toggleRail: () => void;
  /** Re-reads the stored preference into the store — the shell calls it on
   * mount so the rendered rail always agrees with storage. */
  syncRailPref: () => void;
  setSettingsPaneLabel: (label: string) => void;
  setMenu: (menu: Menu) => void;
  openChat: (agentID: string, sessionID?: string) => void;
  clearChatTarget: () => void;
}

type NavSetState = StoreApi<NavState>["setState"];

export const useNavStore = create<NavState>((set: NavSetState) => ({
  menu: "chat",
  returnMenu: "chat",
  chatTarget: null,
  settingsPaneLabel: "Providers",
  railCollapsed: readRailPref(),
  toggleRail: (): void =>
    set((state: NavState): Partial<NavState> => {
      const railCollapsed = !state.railCollapsed;

      try {
        window.localStorage.setItem(RAIL_PREF_KEY, railCollapsed ? "1" : "0");
      } catch {
        // Storage writes can be denied; the in-session state still flips.
      }

      return { railCollapsed };
    }),
  syncRailPref: (): void => set({ railCollapsed: readRailPref() }),
  setSettingsPaneLabel: (label: string): void =>
    set({ settingsPaneLabel: label }),
  // Entering settings records the menu it was entered from; any other
  // transition keeps the recorded return target untouched.
  setMenu: (menu: Menu): void =>
    set((state: NavState): Partial<NavState> =>
      menu === "settings" && state.menu !== "settings"
        ? { menu, returnMenu: state.menu }
        : { menu }
    ),
  openChat: (agentID: string, sessionID?: string): void =>
    set({ menu: "chat", chatTarget: { agentID, sessionID } }),
  clearChatTarget: (): void => set({ chatTarget: null }),
}));
