/**
 * Shell wiring tests for clicked-notification deep links (add-desktop-
 * notifications task 6.3, design D5): the `notifications:open` event
 * navigates to the named group directly, resolves a session's agent from the
 * all-agents listing before openChat, and — from an open settings view —
 * flips the nav store to the chat layer exactly like the settings:open
 * signal. The lib client is mocked wholesale; the handler is captured from
 * the subscription.
 */
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  Session,
} from "../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../lib/client";
import { client } from "../lib/client";
import { useNavStore } from "../stores/nav";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  notifications: {
    onNotificationOpen: MockFn;
  };
  chat: {
    allSessions: MockFn;
  };
};

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

// The deep link reaches the shell through the client's notifications
// surface; the mock captures the registered handler so a test can fire it
// without the Wails runtime.
let handler:
  | ((event: { kind: string; id: string }) => void)
  | null = null;

let allSessionsResult: Session[] = [];

vi.mock("../lib/client", () => ({
  client: {
    notifications: {
      onNotificationOpen: vi.fn(
        (h: (event: { kind: string; id: string }) => void) => {
          handler = h;

          return (): void => {
            handler = null;
          };
        }
      ),
    },
    chat: {
      allSessions: vi.fn(async (): Promise<Session[]> => allSessionsResult),
    },
  },
}));

import { subscribeNotificationOpen } from "../notificationOpen";

/** The session rows the all-agents listing serves; AgentID drives the nav. */
function session(id: string, agentID: string): Session {
  return {
    ID: id,
    AgentID: agentID,
    Title: id,
    Model: "",
    Effort: "",
    CreatedAt: "2026-01-01T00:00:00Z",
    LastActivityAt: "2026-01-01T00:00:00Z",
  };
}

describe("notificationOpen", () => {
  beforeEach(() => {
    handler = null;
    allSessionsResult = [];
    vi.clearAllMocks();
    // The user typically clicks a notification while another menu (settings)
    // is open; the store reset pins that starting point per test.
    useNavStore.setState({ menu: "settings", chatTarget: null });
  });

  it("opens the group view for a group deep link, leaving settings", () => {
    const off = subscribeNotificationOpen();
    expect(handler).toBeTruthy();

    handler?.({ kind: "group", id: "g1" });

    expect(useNavStore.getState().menu).toBe("chat");
    expect(useNavStore.getState().chatTarget).toEqual({ groupID: "g1" });
    off();
  });

  it("opens the session for a session deep link, resolving its agent", async () => {
    allSessionsResult = [session("s2", "a1")];
    const off = subscribeNotificationOpen();

    handler?.({ kind: "session", id: "s2" });

    // The resolution rides the async listing; wait for the navigation.
    await vi.waitFor(() =>
      expect(useNavStore.getState().chatTarget).toEqual({
        agentID: "a1",
        sessionID: "s2",
      })
    );
    expect(useNavStore.getState().menu).toBe("chat");
    off();
  });

  it("navigates nowhere when the session is unknown", async () => {
    allSessionsResult = [session("s1", "a1")];
    const off = subscribeNotificationOpen();

    handler?.({ kind: "session", id: "deleted" });

    await new Promise((resolve) => setTimeout(resolve, 0));

    // Best-effort contract: no navigation, prior state untouched.
    expect(useNavStore.getState().chatTarget).toBeNull();
    expect(useNavStore.getState().menu).toBe("settings");
    off();
  });

  it("navigates nowhere when the agent listing fails", async () => {
    mockedClient.chat.allSessions.mockRejectedValueOnce(
      new Error("bridge unavailable")
    );
    const off = subscribeNotificationOpen();

    handler?.({ kind: "session", id: "s1" });

    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(useNavStore.getState().chatTarget).toBeNull();
    off();
  });

  it("detaches the handler on unsubscribe so later events are dropped", () => {
    const off = subscribeNotificationOpen();

    off();

    expect(handler).toBeNull();
  });
});
