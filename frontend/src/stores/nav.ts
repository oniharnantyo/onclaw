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
  /** The agent whose chat to open; absent when the target is a group. */
  agentID?: string;
  sessionID?: string;
  /** The agent group to open (add-agent-groups design D9); the chat store's
   * init consumes it through openGroup. */
  groupID?: string;
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
  /** Pane a cross-menu action asked settings to open (e.g. the MCP pane's
   * "open settings" on the Browser Use row), tagged with a nonce so the
   * settings view applies each request exactly once. Null when nothing
   * requested. */
  settingsPaneRequest: SettingsPaneRequest | null;
  /** Whether the desktop nav rail is collapsed to icons. Initialized from
   * localStorage so a restored session opens with the stored width. */
  railCollapsed: boolean;
  toggleRail: () => void;
  /** Re-reads the stored preference into the store — the shell calls it on
   * mount so the rendered rail always agrees with storage. */
  syncRailPref: () => void;
  setSettingsPaneLabel: (label: string) => void;
  /** Opens settings focused on one pane: sets the menu and records the
   * pane request (nonce-stamped) for the settings view to apply once. */
  openSettingsPane: (paneKey: string) => void;
  setMenu: (menu: Menu) => void;
  openChat: (agentID: string, sessionID?: string) => void;
  /** Opens a group conversation (add-agent-groups design D9): the chat view
   * consumes the target through the store's openGroup. */
  openGroup: (groupID: string) => void;
  clearChatTarget: () => void;
}

type NavSetState = StoreApi<NavState>["setState"];

/** One cross-menu "open this settings pane" request; the nonce increments
 * per request so re-opening the same pane re-applies. */
export interface SettingsPaneRequest {
  pane: string;
  nonce: number;
}

let settingsPaneNonce = 0;

export const useNavStore = create<NavState>((set: NavSetState) => ({
  menu: "chat",
  returnMenu: "chat",
  chatTarget: null,
  settingsPaneLabel: "Providers",
  settingsPaneRequest: null,
  openSettingsPane: (paneKey: string): void =>
    set((state: NavState): Partial<NavState> => ({
      menu: "settings",
      returnMenu: state.menu === "settings" ? state.returnMenu : state.menu,
      settingsPaneRequest: { pane: paneKey, nonce: ++settingsPaneNonce },
    })),
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
  openGroup: (groupID: string): void =>
    set({ menu: "chat", chatTarget: { groupID } }),
  clearChatTarget: (): void => set({ chatTarget: null }),
}));
