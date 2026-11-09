/**
 * App-shell wiring for clicked-notification deep links (add-desktop-
 * notifications design D5, task 6.3): the `notifications:open` event names
 * the session or group the clicked notification concerned, and this handler
 * navigates there — openChat/openGroup flip the nav store to the chat layer,
 * so an open Settings view is left exactly like the native menu's
 * settings:open signal flips views. Extracted from App.tsx beside
 * settingsShortcut.ts so the subscription is unit-testable without
 * rendering the shell.
 */
import type {
  NotificationOpenEvent,
} from "./lib/events";
import type { Session } from "../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Unsubscribe } from "./lib/transport";
import { client } from "./lib/client";
import { useNavStore } from "./stores/nav";

/**
 * Subscribes to the notifications deep-link event; the returned function
 * unsubscribes (the shell calls it on unmount).
 */
export function subscribeNotificationOpen(): Unsubscribe {
  return client.notifications.onNotificationOpen(
    (event: NotificationOpenEvent): void => {
      if (event.kind === "group") {
        useNavStore.getState().openGroup(event.id);

        return;
      }

      // kind "session": openChat names the session's agent, and the event
      // carries only the session id — resolve the agent from the all-agents
      // listing (each row carries its AgentID) before navigating.
      void openSession(event.id);
    }
  );
}

/**
 * Navigates to one session by id. Best-effort: an unknown (deleted) session
 * or a failed listing navigates nowhere — the host has already shown the
 * window by the time this runs, and the click must not lose prior state
 * beyond the navigation itself.
 */
async function openSession(sessionID: string): Promise<void> {
  try {
    const sessions = await client.chat.allSessions();
    const agentID = sessions.find(
      (session: Session): boolean => session.ID === sessionID
    )?.AgentID;

    if (agentID) useNavStore.getState().openChat(agentID, sessionID);
  } catch {
    // Deep links are best-effort; the view stays where it was.
  }
}
