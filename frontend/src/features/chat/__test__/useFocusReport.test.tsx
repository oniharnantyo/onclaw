/**
 * Component tests for the focus-report hook (add-desktop-notifications task
 * 6.2, design D3): the hook reports the active chat context on mount and on
 * every selection change, names a group when the group view is open, and
 * carries the clear semantics (empty id) when nothing is active. The lib
 * client is mocked wholesale; reportFocus is a recording mock.
 */
import { act } from "react";
import type { ReactElement } from "react";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type { Client } from "../../../lib/client";
import { useChatStore } from "../store";
import type { ChatStore } from "../types";
import { useFocusReport } from "../useFocusReport";
import { renderUI } from "./test-utils";

vi.mock("../../../lib/client", () => ({
  client: {
    notifications: {
      reportFocus: vi.fn(async (): Promise<void> => undefined),
    },
  },
}));

import { client } from "../../../lib/client";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  notifications: {
    reportFocus: MockFn;
  };
};

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

/**
 * The hook reads the chat store's active selection; the harness subscribes
 * to the same fields so a store update re-renders and re-runs the effect.
 */
function Harness(): ReactElement {
  useFocusReport();
  const activeSessionID = useChatStore((s: ChatStore) => s.activeSessionID);
  const activeGroupID = useChatStore((s: ChatStore) => s.activeGroupID);

  return (
    <div
      data-testid="focus-harness"
      data-session={activeSessionID ?? ""}
      data-group={activeGroupID ?? ""}
    />
  );
}

/** The focus calls so far, as [kind, id] pairs. */
function calls(): [string, string][] {
  return mockedClient.notifications.reportFocus.mock.calls.map(
    (call: unknown[]): [string, string] => [String(call[0]), String(call[1])]
  );
}

describe("useFocusReport", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useChatStore.setState({
      activeAgentID: null,
      activeSessionID: null,
      activeGroupID: null,
    });
  });

  it("reports the active session on mount", async () => {
    useChatStore.setState({ activeSessionID: "s1" });

    await renderUI(<Harness />);

    expect(calls()).toEqual([["session", "s1"]]);
  });

  it("re-reports on every active session change", async () => {
    useChatStore.setState({ activeSessionID: "s1" });
    await renderUI(<Harness />);

    await act(async () => {
      useChatStore.setState({ activeSessionID: "s2" });
    });

    expect(calls()).toEqual([
      ["session", "s1"],
      ["session", "s2"],
    ]);
  });

  it("reports the group when the group view is the active context", async () => {
    useChatStore.setState({ activeSessionID: "s1" });
    await renderUI(<Harness />);

    await act(async () => {
      // Leaving the session for a group clears both session fields, as the
      // store's openGroup does.
      useChatStore.setState({ activeSessionID: null, activeGroupID: "g1" });
    });

    expect(calls()).toEqual([
      ["session", "s1"],
      ["group", "g1"],
    ]);
  });

  it("reports the clear semantics (empty id) when nothing is active", async () => {
    useChatStore.setState({ activeSessionID: "s1" });
    await renderUI(<Harness />);

    await act(async () => {
      useChatStore.setState({ activeSessionID: null });
    });

    // The empty-id report is the engine's "nothing on screen" contract —
    // an uninformative report just delivers.
    expect(calls()).toEqual([
      ["session", "s1"],
      ["session", ""],
    ]);
  });

  it("never crashes on a rejecting reportFocus", async () => {
    mockedClient.notifications.reportFocus.mockRejectedValueOnce(
      new Error("bridge unavailable")
    );

    useChatStore.setState({ activeSessionID: "s1" });
    await renderUI(<Harness />);

    expect(calls()).toEqual([["session", "s1"]]);
  });
});
