/**
 * Focus mirror for desktop-notification suppression (add-desktop-notifications
 * design D3, task 6.2): the frontend owns "what is on screen", so this hook
 * reports the chat view's active context to the backend whenever it changes.
 * A group conversation wins over a session (the store keeps the pair
 * mutually exclusive); with neither active — the fresh-chat empty state, no
 * agent selected — the report carries an empty id, which the engine's
 * contract defines as the clear semantics (nothing on screen, always
 * deliver). The effect re-runs on mount too, so every chat view mount
 * refreshes a possibly-stale mirror (design D3's staleness bound).
 */
import { useEffect } from "react";

import { client } from "../../lib/client";
import { useChatStore } from "./store";
import type { ChatStore } from "./types";

/**
 * Reports the active chat/group selection to the notifications engine.
 * Wired once in the chat feature root (ChatView) — the single surface that
 * observes both selections; never per-view.
 */
export function useFocusReport(): void {
  const activeSessionID = useChatStore(
    (s: ChatStore) => s.activeSessionID
  );
  const activeGroupID = useChatStore((s: ChatStore) => s.activeGroupID);

  useEffect(() => {
    // A group view IS the active conversation when its id is set; otherwise
    // the session id — or empty, clearing the mirror (never-error contract:
    // an uninformative report just delivers).
    const kind = activeGroupID !== null ? "group" : "session";
    const id = activeGroupID ?? activeSessionID ?? "";

    void reportFocus(kind, id);
  }, [activeGroupID, activeSessionID]);
}

/**
 * One best-effort report. The notifications surface is bound in both hosts
 * and never errors by contract, but a frontend served without the bridge
 * (or a rejected call) must never crash the chat view — the mirror going
 * stale only over-suppresses the exact stale conversation.
 */
async function reportFocus(kind: string, id: string): Promise<void> {
  try {
    // SAFETY: the mocked/absent-bindings runtime may lack the surface; the
    // optional chain turns that into a silent skip.
    await client.notifications?.reportFocus(kind, id);
  } catch {
    // Focus mirroring is best-effort: a failed report just stops suppressing.
  }
}
