// Badge-dot watcher (add-subagent-runs-surface 6.2): when a session task of
// the ACTIVE chat reaches a terminal status while the Subagents source is NOT
// actively viewed, the panel header toggle gets its dot badge — the same
// store `panel.badge` mechanism the panel-affordance watcher uses
// (store/index.ts, "Panel badge watcher"). All it may do is set the badge:
// never open, never touch tabs[]. The badge clears when the panel opens
// (openPanelTab / setPanelOpen).
//
// It lives at module scope (a useStore.subscribe), not in the source
// component — the transitions it watches happen exactly when the source is
// not mounted (panel closed, or another tab active).

import { useStore } from "../../../../../store";
import type { TaskEntry } from "../../../../../lib/tasks";
import { terminalBadgeDue } from "./view";

// Watch state, keyed to the ACTIVE chat: a chat switch (or boot) re-baselines
// — the task states already sitting in the store when you arrive are history,
// not finishes. Per chat, the last-seen entries snapshot.
let watchKey: string | null = null;
const seenEntries = new Map<string, TaskEntry[]>();

const activelyViewing = (s: any): boolean => {
  if (!s.panel.open) return false;
  const active = s.panel.tabs.find((t: any) => t.id === s.panel.activeId) || s.panel.tabs[s.panel.tabs.length - 1];
  return active?.kind === 'subagents';
};

function watch(s: any): void {
  const { tenantId, chatId } = s.pos;
  const key = chatId ? `${tenantId}::${chatId}` : null;
  if (!key) return;
  if (watchKey !== key) {
    watchKey = key;
    seenEntries.clear();
  }
  const entries: TaskEntry[] | undefined = s.tasks[chatId]?.entries;
  if (
    entries && entries.length > 0 &&
    terminalBadgeDue(seenEntries.get(chatId), entries) &&
    !activelyViewing(s) &&
    !s.panel.badge
  ) {
    useStore.setState({ panel: { ...s.panel, badge: true } });
  }
  seenEntries.set(chatId, entries || []);
}

let installed = false;

/** Idempotent install — the registration module calls this once on import. */
export function installTaskTerminalBadgeWatcher(): void {
  if (installed) return;
  installed = true;
  useStore.subscribe(watch);
}

/** Test seam: forget the baseline so suites start clean (the registries have
 * the same seam in resetPanelRegistries). Production never calls this. */
export function resetTaskBadgeWatcherForTests(): void {
  watchKey = null;
  seenEntries.clear();
}
