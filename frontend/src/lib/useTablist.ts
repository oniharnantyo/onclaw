import type { KeyboardEvent as ReactKeyboardEvent } from "react";

/**
 * useTablist (fix-ui-audit-findings D7b): the one shared ARIA tab pattern
 * behind the app's five tab strips (settings nav, AgentConfigPane,
 * IdentitySection, DocumentConfigDialog, right-bar preview tabs). Each tab
 * gets a stable id, `aria-selected`, and a roving `tabindex` (the active tab
 * 0, the others −1) so Tab reaches the strip once and arrows move within it;
 * arrow keys move the selection — Left/Up to the previous tab, Right/Down to
 * the next, covering both horizontal and vertical strips — and Home/End jump
 * to the first/last. Arrows STOP at the strip's ends (no wrap-around: a
 * wrap silently re-announces the opposite end and the strips here are short
 * enough that Home/End already reach it in one press); the selection moves
 * and the newly active tab receives focus together. Each panel gets
 * `role="tabpanel"` + `aria-labelledby` pointing at its tab's id.
 *
 * The hook owns only the tab wiring; visibility of panels (the `hidden`
 * class toggles) stays with the caller. `active` may name no tab in `ids`
 * (the right bar's pinned identity state) — every tab then renders
 * unselected with tabindex −1, and an arrow key from that state enters the
 * strip at its near end (Right → first, Left → last).
 */
export interface UseTablistOptions<T extends string> {
  /** Prefix for the generated tab/panel DOM ids (e.g. "settings-nav"). */
  idPrefix: string;
  /** The tab ids in strip order. */
  ids: readonly T[];
  /** The active tab's id, or null when no tab in `ids` is selected. */
  active: T | null;
  /** Selection changes (arrow keys). Click handling stays with the caller. */
  onChange: (id: T) => void;
}

export interface TablistTabProps {
  id: string;
  role: "tab";
  "aria-selected": boolean;
  tabIndex: 0 | -1;
  onKeyDown: (event: ReactKeyboardEvent<HTMLElement>) => void;
}

export interface TablistPanelProps {
  id: string;
  role: "tabpanel";
  "aria-labelledby": string;
}

export function useTablist<T extends string>({
  idPrefix,
  ids,
  active,
  onChange,
}: UseTablistOptions<T>): {
  tab: (id: T) => TablistTabProps;
  panel: (id: T) => TablistPanelProps;
} {
  const tabId = (id: T): string => `${idPrefix}-tab-${id}`;
  const panelId = (id: T): string => `${idPrefix}-panel-${id}`;

  /** Selects the tab at `index` (already clamped by the caller) and moves
   * focus to it — roving tabindex keeps the two in lockstep. */
  const select = (index: number, event: ReactKeyboardEvent): void => {
    const next = ids[index];

    if (next === undefined || next === active) return;
    event.preventDefault();
    onChange(next);
    document.getElementById(tabId(next))?.focus();
  };

  const tab = (id: T): TablistTabProps => ({
    id: tabId(id),
    role: "tab",
    "aria-selected": id === active,
    tabIndex: id === active ? 0 : -1,
    onKeyDown: (event: ReactKeyboardEvent<HTMLElement>): void => {
      // An active id outside the strip (pinned-identity state) starts the
      // walk from off-strip (-1): an arrow then enters the strip at its
      // near end — Right → first, Left → last.
      const at = active === null ? -1 : ids.indexOf(active);

      switch (event.key) {
        case "ArrowRight":
        case "ArrowDown":
          if (at < 0) select(0, event);
          else if (at < ids.length - 1) select(at + 1, event);
          break;
        case "ArrowLeft":
        case "ArrowUp":
          if (at < 0) select(ids.length - 1, event);
          else if (at > 0) select(at - 1, event);
          break;
        case "Home":
          select(0, event);
          break;
        case "End":
          select(ids.length - 1, event);
          break;
        default:
          break;
      }
    },
  });

  const panel = (id: T): TablistPanelProps => ({
    id: panelId(id),
    role: "tabpanel",
    "aria-labelledby": tabId(id),
  });

  return { tab, panel };
}
