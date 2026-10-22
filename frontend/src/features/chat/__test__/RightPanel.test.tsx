/**
 * RightPanel tests (task 8.2 + 8.3, spec chat-panels): the panel renders
 * nothing with no tabs, opens a prompt-document preview tab fetched through
 * the existing prompt-doc binding, closes per tab (removing the panel with
 * the last one), deduplicates re-opens, persists its width under
 * `buddies.chatPanelWidth`, resets closed on session switch, docks at xl /
 * overlays below it, and — mid-turn — opens and closes without touching the
 * in-flight turn. The 8.3 sources: workspace file previews (success, empty,
 * refused, failed) fetched through the jailed reader, subagent trajectory
 * paths previewing through the same source, and directory listings that
 * navigate in place and open file tabs per entry. The panel's token surfaces
 * are asserted under both themes.
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import { Kind } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type {
  PromptDocuments,
  WorkspaceEntry,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { ContextUsage } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/agents/models";
import type { Client } from "../../../lib/client";
import type { ChatStore } from "../types";
import { applyEvent } from "../entries";
import {
  promptDocTab,
  RightPanel,
  workspaceDirTab,
  workspaceFileTab,
} from "../components/RightPanel";
import { TRANSCRIPT_PAGE_SIZE, useChatStore } from "../store";
import { renderUI } from "./test-utils";

const docs: PromptDocuments = {
  Identity: "I am Scout.",
  IdentityExists: true,
  Soul: "",
  SoulExists: false,
  Bootstrap: "Boot.",
  BootstrapExists: true,
};

/** The jail's refusal wording (backend "workspaces: refused …"). */
const REFUSED = 'workspaces: refused "/etc/passwd": outside the workspace';

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  agents: {
    getPromptDocuments: MockFn;
  };
  chat: {
    sessions: MockFn;
    subscribe: MockFn;
    transcript: MockFn;
    contextUsage: MockFn;
    workspaceFile: MockFn;
    listWorkspace: MockFn;
  };
};

vi.mock("../../../lib/client", () => ({
  client: {
    agents: {
      getPromptDocuments: vi.fn(async (): Promise<PromptDocuments> => docs),
    },
    chat: {
      sessions: vi.fn(async (): Promise<never[]> => []),
      subscribe: vi.fn(async (): Promise<() => void> => () => undefined),
      transcript: vi.fn(async (): Promise<never[]> => []),
      transcriptPage: vi.fn(async (): Promise<never[]> => []),
      contextUsage: vi.fn(async (): Promise<ContextUsage> => ({
        used: 0,
        limit: 0,
        last_input: 0,
        last_output: 0,
      })),
      workspaceFile: vi.fn(async (): Promise<string> => "file body"),
      listWorkspace: vi.fn(async (): Promise<WorkspaceEntry[]> => []),
    },
  },
}));

import { client } from "../../../lib/client";

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

// jsdom's localStorage would leak state between tests; every test stubs it
// against a Map instead (same fake as ThemeCycleButton.test.tsx).
function stubLocalStorage(initial: Record<string, string> = {}): void {
  const map = new Map(Object.entries(initial));

  vi.stubGlobal("localStorage", {
    getItem: (key: string): string | null => map.get(key) ?? null,
    setItem: (key: string, value: string): void => {
      map.set(key, String(value));
    },
    removeItem: (key: string): void => {
      map.delete(key);
    },
  });
}

/** Opens a prompt-doc tab through the store's public action. */
async function openTab(
  agentID: string,
  doc: "identity" | "soul" | "bootstrap"
): Promise<void> {
  await act(async () => {
    useChatStore.getState().openPanelTab(promptDocTab(agentID, doc));
  });
}

/** Opens a workspace file preview tab through the store's public action. */
async function openFileTab(relPath: string): Promise<void> {
  await act(async () => {
    useChatStore.getState().openPanelTab(workspaceFileTab("a1", relPath));
  });
}

/** Opens a workspace directory listing tab through the store's action. */
async function openDirTab(relPath: string): Promise<void> {
  await act(async () => {
    useChatStore.getState().openPanelTab(workspaceDirTab("a1", relPath));
  });
}

/** Clears open tabs between renders: the store outlives an unmount, so a
 * stale tab would re-mount and fetch in the next render. */
async function clearTabs(): Promise<void> {
  await act(async () => {
    useChatStore.setState({ panelTabs: [] });
  });
}

function openTabCount(): number {
  return screen
    .getByTestId("panel-tabs")
    .querySelectorAll("[data-testid^='panel-tab-close-']").length;
}

/** Nth tab's close button (ids are minted by a module-level counter, so
 * tests address tabs by position, never by id). */
function closeButton(index: number): HTMLElement {
  const btn = screen
    .getByTestId("panel-tabs")
    .querySelectorAll<HTMLElement>("[data-testid^='panel-tab-close-']")[index];

  if (!btn) throw new Error(`no tab close button at index ${index}`);

  return btn;
}

/** Nth tab's title button (excluding its close control). */
function tabButton(index: number): HTMLElement {
  const btn = screen
    .getByTestId("panel-tabs")
    .querySelectorAll<HTMLElement>(
      "[data-testid^='panel-tab-']:not([data-testid^='panel-tab-close-'])"
    )[index];

  if (!btn) throw new Error(`no tab button at index ${index}`);

  return btn;
}

beforeEach(() => {
  vi.clearAllMocks();
  stubLocalStorage();

  return act(async () => {
    useChatStore.setState({
      activeAgentID: "a1",
      activeSessionID: "s1",
      entries: [],
      panelTabs: [],
      loadingSession: false,
      error: null,
      busy: false,
    });
  });
});

describe("RightPanel", () => {
  it("renders nothing with no tabs open", async () => {
    await renderUI(<RightPanel />);

    expect(screen.queryByTestId("chat-right-panel")).toBeNull();
  });

  it("opens a prompt-document tab and previews its content", async () => {
    const { unmount } = await renderUI(<RightPanel />);
    await openTab("a1", "identity");

    expect(screen.getByTestId("chat-right-panel")).toBeTruthy();
    await waitFor(() =>
      expect(screen.getByTestId("panel-preview-content").textContent).toBe(
        "I am Scout."
      )
    );
    expect(mockedClient.agents.getPromptDocuments).toHaveBeenCalledWith("a1");

    await unmount();
  });

  it("shows an empty preview for a missing document", async () => {
    const { unmount } = await renderUI(<RightPanel />);
    await openTab("a1", "soul");

    await waitFor(() =>
      expect(screen.getByTestId("panel-preview-empty")).toBeTruthy()
    );

    await unmount();
  });

  it("surfaces a failed read as an error, not a blank pane", async () => {
    mockedClient.agents.getPromptDocuments.mockImplementationOnce(
      async (): Promise<PromptDocuments> => {
        throw new Error("workspace unreadable");
      }
    );

    const { unmount } = await renderUI(<RightPanel />);
    await openTab("a1", "identity");

    await waitFor(() =>
      expect(screen.getByTestId("panel-preview-error").textContent).toBe(
        "workspace unreadable"
      )
    );

    await unmount();
  });

  it("closes tabs per tab and removes the panel with the last one", async () => {
    const { unmount } = await renderUI(<RightPanel />);
    await openTab("a1", "identity");
    await openTab("a1", "bootstrap");

    expect(openTabCount()).toBe(2);

    await act(async () => {
      fireEvent.click(closeButton(0));
    });

    expect(screen.getByTestId("chat-right-panel")).toBeTruthy();
    expect(openTabCount()).toBe(1);

    await act(async () => {
      fireEvent.click(closeButton(0));
    });

    expect(screen.queryByTestId("chat-right-panel")).toBeNull();

    await unmount();
  });

  it("deduplicates re-opens of the same document", async () => {
    const { unmount } = await renderUI(<RightPanel />);
    await openTab("a1", "identity");
    await openTab("a1", "identity");

    expect(openTabCount()).toBe(1);

    await unmount();
  });

  it("keeps the active tab when a tab other than the last closes", async () => {
    const { unmount } = await renderUI(<RightPanel />);
    await openTab("a1", "identity");
    await openTab("a1", "bootstrap");

    // Focus the first tab, close the second.
    await act(async () => {
      fireEvent.click(tabButton(0));
    });
    await act(async () => {
      fireEvent.click(closeButton(1));
    });

    expect(screen.getByTestId("panel-preview-content").textContent).toBe(
      "I am Scout."
    );

    await unmount();
  });

  it("starts at the persisted width and falls back when it is unreadable", async () => {
    stubLocalStorage({ "buddies.chatPanelWidth": "480" });

    const { unmount } = await renderUI(<RightPanel />);
    await openTab("a1", "identity");

    expect(
      screen
        .getByTestId("chat-right-panel")
        .getAttribute("style")
        ?.replace(/\s+/g, "")
    ).toContain("--panel-width:480px");

    await unmount();

    stubLocalStorage({ "buddies.chatPanelWidth": "not-a-number" });

    const second = await renderUI(<RightPanel />);
    await openTab("a1", "identity");

    expect(
      second
        .getByTestId("chat-right-panel")
        .getAttribute("style")
        ?.replace(/\s+/g, "")
    ).toContain("--panel-width:320px");

    await second.unmount();
  });

  it("persists the dragged width under buddies.chatPanelWidth", async () => {
    const { unmount } = await renderUI(<RightPanel />);
    await openTab("a1", "identity");

    await act(async () => {
      fireEvent.pointerDown(screen.getByTestId("panel-resize"));
      // jsdom rects are all zero, so the right edge sits at x=0: dragging the
      // pointer far right of it drives the width below the minimum clamp.
      window.dispatchEvent(new MouseEvent("pointermove", { clientX: 1000 }));
      window.dispatchEvent(new MouseEvent("pointerup"));
    });

    expect(localStorage.getItem("buddies.chatPanelWidth")).toBe("240");

    await unmount();
  });

  it("resets closed when the session switches", async () => {
    const { unmount } = await renderUI(<RightPanel />);
    await openTab("a1", "identity");

    expect(screen.getByTestId("chat-right-panel")).toBeTruthy();

    await act(async () => {
      await useChatStore.getState().selectSession("s2");
    });

    expect(screen.queryByTestId("chat-right-panel")).toBeNull();
    expect(mockedClient.chat.transcriptPage).toHaveBeenCalledWith(
      "s2",
      0,
      TRANSCRIPT_PAGE_SIZE
    );

    await unmount();
  });

  it("docks as a column at xl and overlays as a sheet below it", async () => {
    const { unmount } = await renderUI(<RightPanel />);
    await openTab("a1", "identity");

    const panel = screen.getByTestId("chat-right-panel");

    // Overlay sheet below xl (full-height, right-anchored, above the view).
    expect(panel.className).toContain("absolute");
    expect(panel.className).toContain("inset-y-0");
    expect(panel.className).toContain("z-30");
    // Docked column at xl+: back in flex flow at the persisted width.
    expect(panel.className).toContain("xl:relative");
    expect(panel.className).toContain("xl:w-[var(--panel-width)]");
    // The resize handle only exists in the docked form.
    expect(screen.getByTestId("panel-resize").className).toContain("hidden");
    expect(screen.getByTestId("panel-resize").className).toContain("xl:block");

    await unmount();
  });

  it("opens and closes mid-turn without touching the in-flight turn", async () => {
    const delta = (text: string): Parameters<typeof applyEvent>[1] => ({
      kind: Kind.KindTextDelta,
      occurred_at: "2026-01-01T00:00:00Z",
      turn_id: "turn-1",
      text,
    });

    await act(async () => {
      useChatStore.setState({
        busy: true,
        activeTurnID: "turn-1",
        entries: applyEvent([], delta("Hel")),
      });
    });

    const { unmount } = await renderUI(<RightPanel />);
    await openTab("a1", "identity");

    expect(screen.getByTestId("chat-right-panel")).toBeTruthy();

    // A delta lands while the panel is open: the turn keeps folding.
    await act(async () => {
      useChatStore.setState((prev: ChatStore) => ({
        entries: applyEvent(prev.entries, delta("lo")),
      }));
    });

    const streaming = useChatStore.getState().entries;
    const last = streaming[streaming.length - 1];

    expect(useChatStore.getState().busy).toBe(true);

    if (last?.kind !== "assistant")
      throw new Error("expected a streaming assistant entry");

    expect(last.text).toBe("Hello");
    expect(last.streaming).toBe(true);

    // Closing again changes nothing about the turn either.
    await act(async () => {
      const tabs = useChatStore.getState().panelTabs;
      useChatStore.getState().closePanelTab(tabs[0]!.id);
    });

    expect(useChatStore.getState().busy).toBe(true);

    await unmount();
  });

  it("renders the same token surfaces under light and dark themes", async () => {
    for (const theme of ["light", "dark"] as const) {
      document.documentElement.dataset.theme = theme;

      const { unmount } = await renderUI(<RightPanel />);
      await openTab("a1", "identity");

      const panel = screen.getByTestId("chat-right-panel");

      expect(panel.className).toContain("bg-surface");
      expect(panel.className).toContain("border-line");

      await unmount();
    }

    delete document.documentElement.dataset.theme;
  });

  it("previews a workspace file through the jailed reader", async () => {
    mockedClient.chat.workspaceFile.mockResolvedValueOnce("hello workspace");

    const { unmount } = await renderUI(<RightPanel />);
    await openFileTab("notes.md");

    await waitFor(() =>
      expect(screen.getByTestId("panel-preview-content").textContent).toBe(
        "hello workspace"
      )
    );
    expect(mockedClient.chat.workspaceFile).toHaveBeenCalledWith(
      "a1",
      "notes.md"
    );

    await unmount();
  });

  it("previews a subagent trajectory through the same file source", async () => {
    // Trajectory files are workspace-relative (.tasks/task-N.md), so they
    // reuse the file preview source (task 8.3).
    mockedClient.chat.workspaceFile.mockResolvedValueOnce(
      '{"step":"task","content":"Research"}\n'
    );

    const { unmount } = await renderUI(<RightPanel />);
    await openFileTab(".tasks/task-1.md");

    await waitFor(() =>
      expect(screen.getByTestId("panel-preview-content").textContent).toContain(
        "Research"
      )
    );
    expect(mockedClient.chat.workspaceFile).toHaveBeenCalledWith(
      "a1",
      ".tasks/task-1.md"
    );

    await unmount();
  });

  it("shows an empty preview for an empty workspace file", async () => {
    mockedClient.chat.workspaceFile.mockResolvedValueOnce("");

    const { unmount } = await renderUI(<RightPanel />);
    await openFileTab("notes.md");

    await waitFor(() =>
      expect(screen.getByTestId("panel-preview-empty").textContent).toBe(
        "Empty file"
      )
    );

    await unmount();
  });

  it("renders the refused outcome for a path the jail refuses", async () => {
    mockedClient.chat.workspaceFile.mockRejectedValueOnce(new Error(REFUSED));

    const { unmount } = await renderUI(<RightPanel />);
    await openFileTab("/etc/passwd");

    await waitFor(() =>
      expect(screen.getByTestId("panel-preview-refused")).toBeTruthy()
    );
    expect(screen.queryByTestId("panel-preview-content")).toBeNull();
    expect(screen.queryByTestId("panel-preview-error")).toBeNull();

    await unmount();
  });

  it("surfaces a non-refused workspace read failure as an error", async () => {
    mockedClient.chat.workspaceFile.mockRejectedValueOnce(
      new Error("not found: notes.md")
    );

    const { unmount } = await renderUI(<RightPanel />);
    await openFileTab("notes.md");

    await waitFor(() =>
      expect(screen.getByTestId("panel-preview-error").textContent).toBe(
        "not found: notes.md"
      )
    );
    expect(screen.queryByTestId("panel-preview-refused")).toBeNull();

    await unmount();
  });

  it("lists a directory and opens file entries as preview tabs", async () => {
    mockedClient.chat.listWorkspace.mockResolvedValueOnce([
      { name: ".tasks", dir: true, size: 0 },
      { name: "notes.md", dir: false, size: 3 },
    ]);

    const { unmount } = await renderUI(<RightPanel />);
    await openDirTab("");

    await waitFor(() =>
      expect(screen.getByTestId("panel-dir-listing")).toBeTruthy()
    );
    expect(screen.getByTestId("panel-dir-path").textContent).toBe("/");

    await act(async () => {
      fireEvent.click(screen.getByTestId("panel-dir-entry-notes.md"));
    });

    // The file entry opened its own preview tab through the store action.
    const tabs = useChatStore.getState().panelTabs;

    expect(tabs).toHaveLength(2);
    expect(tabs[1]).toMatchObject({
      kind: "workspace-file",
      data: { agentID: "a1", relPath: "notes.md" },
    });
    expect(mockedClient.chat.workspaceFile).toHaveBeenCalledWith(
      "a1",
      "notes.md"
    );

    await unmount();
  });

  it("navigates directory listings in place and back up", async () => {
    mockedClient.chat.listWorkspace.mockResolvedValueOnce([
      { name: "sub", dir: true, size: 0 },
    ]);

    const { unmount } = await renderUI(<RightPanel />);
    await openDirTab("");

    await waitFor(() =>
      expect(screen.getByTestId("panel-dir-entry-sub")).toBeTruthy()
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId("panel-dir-entry-sub"));
    });

    // The subdirectory navigated in place: no new tab, a nested list call,
    // and an Up affordance ("" is the root, which has none).
    expect(useChatStore.getState().panelTabs).toHaveLength(1);
    await waitFor(() =>
      expect(mockedClient.chat.listWorkspace).toHaveBeenCalledWith("a1", "sub")
    );
    expect(screen.getByTestId("panel-dir-path").textContent).toBe("sub");
    expect(screen.getByTestId("panel-dir-up")).toBeTruthy();

    mockedClient.chat.listWorkspace.mockResolvedValueOnce([
      { name: "notes.md", dir: false, size: 1 },
    ]);

    await act(async () => {
      fireEvent.click(screen.getByTestId("panel-dir-up"));
    });

    await waitFor(() =>
      expect(mockedClient.chat.listWorkspace).toHaveBeenCalledWith("a1", "")
    );
    expect(screen.getByTestId("panel-dir-path").textContent).toBe("/");

    await unmount();
  });

  it("shows empty and refused states for directory listings", async () => {
    mockedClient.chat.listWorkspace.mockResolvedValueOnce([]);

    const { unmount } = await renderUI(<RightPanel />);
    await openDirTab("");

    await waitFor(() =>
      expect(screen.getByTestId("panel-preview-empty").textContent).toBe(
        "Empty directory"
      )
    );

    await unmount();
    await clearTabs();

    mockedClient.chat.listWorkspace.mockRejectedValueOnce(new Error(REFUSED));

    const second = await renderUI(<RightPanel />);
    await openDirTab("../outside");

    await waitFor(() =>
      expect(screen.getByTestId("panel-preview-refused")).toBeTruthy()
    );
    expect(screen.queryByTestId("panel-dir-listing")).toBeNull();

    await second.unmount();
  });

  it("renders the workspace preview token surfaces under both themes", async () => {
    for (const theme of ["light", "dark"] as const) {
      document.documentElement.dataset.theme = theme;

      const { unmount } = await renderUI(<RightPanel />);
      await openFileTab("notes.md");

      const content = await waitFor(() =>
        screen.getByTestId("panel-preview-content")
      );

      expect(content.className).toContain("font-mono");
      expect(content.className).toContain("text-fg2");

      await unmount();
      await clearTabs();

      mockedClient.chat.workspaceFile.mockRejectedValueOnce(new Error(REFUSED));

      const refused = await renderUI(<RightPanel />);
      await openFileTab("/etc/passwd");

      await waitFor(() =>
        expect(screen.getByTestId("panel-preview-refused")).toBeTruthy()
      );
      expect(screen.getByText("Refused").className).toContain(
        "text-dangertext"
      );

      await refused.unmount();
    }

    delete document.documentElement.dataset.theme;
  });
});
