/**
 * RightBar tests (merge-right-bar-previews, designs D1–D2): ONE right bar
 * with two states. With no tabs open it renders the session identity (the
 * old right sidebar's agent + effective-model row; no tab strip), and the
 * model row reflects the session override with the effort appended. Opening
 * a preview swaps the SAME aside in place to the old right panel's tab
 * strip + content — exactly one bar in the document, identity gone — and
 * closing the last tab returns the identity. The store's session switch
 * resets the tabs (identity returns), collapsing hides the bar entirely in
 * either state, and the single width preference persists under
 * `buddies.chatSidebarWidth` (default 280, drag clamped to 220–560) through
 * the same CSS var in both states. Renderer internals (markdown modes,
 * refusal/empty states, trajectory) live with the renderers in
 * panelRenderers — this suite owns the container contract.
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import { Kind } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { ContextUsage } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/agents/models";
import type {
  Agent,
  Session,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { ProviderView } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client } from "../../../lib/client";
import { applyEvent } from "../entries";
import { RightBar } from "../components/RightBar";
import { workspaceFileTab } from "../components/panelRenderers";
import { TRANSCRIPT_PAGE_SIZE, useChatStore } from "../store";
import type { ChatStore } from "../types";
import { renderUI } from "./test-utils";

const agent: Agent = {
  ID: "a1",
  Name: "Scout",
  ProviderID: "prov",
  Model: "gpt-test",
  Effort: "",
  Slug: "scout",
  Description: "",
  Brief: "",
  Avatar: null,
  SubagentsEnabled: true,
  PromptsStatus: "ready",
  PromptsError: "",
  ContextWindow: 0,
  Temperature: 0.7,
  MaxTokens: 0,
  Tools: null,
  Skills: null,
  MCPServers: null,
  CreatedAt: "2026-01-01T00:00:00Z",
  UpdatedAt: "2026-01-01T00:00:00Z",
};

const session: Session = {
  ID: "s1",
  AgentID: "a1",
  Title: "Weekly notes",
  Model: "",
  Effort: "",
  CreatedAt: "2026-01-01T00:00:00Z",
  LastActivityAt: "2026-01-01T00:00:00Z",
};

const provider: ProviderView = {
  ID: "prov",
  Type: "openai",
  Name: "Prov Co",
  BaseURL: "",
  Enabled: true,
  KeySet: true,
  KeyHint: "sk-…",
  CreatedAt: "2026-01-01T00:00:00Z",
  UpdatedAt: "2026-01-01T00:00:00Z",
};

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  chat: {
    subscribe: MockFn;
    transcriptPage: MockFn;
    turnState: MockFn;
    contextUsage: MockFn;
    workspaceFile: MockFn;
  };
};

vi.mock("../../../lib/client", () => ({
  client: {
    chat: {
      subscribe: vi.fn(async (): Promise<() => void> => () => undefined),
      transcriptPage: vi.fn(async (): Promise<never[]> => []),
      turnState: vi.fn(
        async (): Promise<{ busy: boolean; turn_id: string }> => ({
          busy: false,
          turn_id: "",
        })
      ),
      contextUsage: vi.fn(async (): Promise<ContextUsage> => ({
        used: 0,
        limit: 0,
        last_input: 0,
        last_output: 0,
      })),
      // Deterministic per-path content: a tab re-mounting (the renderer keys
      // by tab id) always re-reads the same body.
      workspaceFile: vi.fn(
        async (_agentID: string, relPath: string): Promise<string> =>
          `content of ${relPath}`
      ),
    },
  },
}));

import { client } from "../../../lib/client";

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

// jsdom's localStorage would leak state between tests; every test stubs it
// against a Map instead (same fake as the old RightPanel.test.tsx).
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

/** Opens a workspace file preview tab through the store's public action. */
async function openFileTab(relPath: string): Promise<void> {
  await act(async () => {
    useChatStore.getState().openPanelTab(workspaceFileTab("a1", relPath));
  });
}

/** Clears open tabs between renders: the store outlives an unmount, so a
 * stale tab would re-mount and fetch in the next render. */
async function clearTabs(): Promise<void> {
  await act(async () => {
    useChatStore.setState({ panelTabs: [], activePanelTabID: null });
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
      agents: [agent],
      providers: [provider],
      sessions: [session],
      activeAgentID: "a1",
      activeSessionID: "s1",
      entries: [],
      panelTabs: [],
      activePanelTabID: null,
      sidebarCollapsed: false,
      loadingSession: false,
      error: null,
      busy: false,
    });
  });
});

describe("RightBar", () => {
  it("shows the session identity with no tabs open and no tab strip", async () => {
    await renderUI(<RightBar />);

    expect(screen.getByTestId("chat-right-bar").className).toContain(
      "glass-panel"
    );
    // Below xl the identity state is hidden outright; at xl it docks.
    expect(screen.getByTestId("chat-right-bar").className).toContain("hidden");
    expect(screen.getByTestId("chat-right-bar").className).toContain("xl:flex");
    expect(screen.getByTestId("sidebar-agent-name").textContent).toBe("Scout");
    expect(screen.getByTestId("sidebar-model").textContent).toBe(
      "Prov Co/gpt-test"
    );
    // Identity only: no tab strip, and the bar never fetches capabilities
    // (that readout lives on the composer's ContextMeter).
    expect(screen.queryByTestId("panel-tabs")).toBeNull();
    expect(mockedClient.chat.workspaceFile).not.toHaveBeenCalled();
  });

  it("reflects the session override and effort in the model row", async () => {
    useChatStore.setState({
      sessions: [{ ...session, Model: "big-model", Effort: "high" }],
    });

    await renderUI(<RightBar />);
    expect(screen.getByTestId("sidebar-model").textContent).toBe(
      "Prov Co/big-model · high"
    );
  });

  it("falls back to 'No model selected' when nothing configures a model", async () => {
    useChatStore.setState({
      agents: [{ ...agent, ProviderID: "", Model: "" }],
      providers: [],
    });

    await renderUI(<RightBar />);
    expect(screen.getByTestId("sidebar-model").textContent).toBe(
      "No model selected"
    );
  });

  it("renders nothing when no agent matches the active agent", async () => {
    useChatStore.setState({ activeAgentID: "ghost" });

    await renderUI(<RightBar />);
    expect(screen.queryByTestId("chat-right-bar")).toBeNull();
  });

  it("swaps the SAME aside to tabs + content on open — no second column", async () => {
    const { unmount } = await renderUI(<RightBar />);
    await openFileTab("notes.md");

    // Exactly one bar in the document: the previews replaced the identity
    // in place, never a second column (design D1).
    expect(screen.getAllByTestId("chat-right-bar")).toHaveLength(1);
    expect(screen.queryByTestId("sidebar-agent-name")).toBeNull();
    expect(openTabCount()).toBe(1);

    const bar = screen.getByTestId("chat-right-bar");

    // Overlay sheet below xl (full-height, right-anchored, above the view),
    // docked column at xl — the old panel's responsive classes.
    expect(bar.className).toContain("absolute");
    expect(bar.className).toContain("inset-y-0");
    expect(bar.className).toContain("z-30");
    expect(bar.className).toContain("xl:relative");
    expect(bar.className).toContain("xl:w-[var(--sidebar-width)]");
    // One width preference (design D2): the same CSS var drives both states.
    expect(bar.getAttribute("style")?.replace(/\s+/g, "")).toContain(
      "--sidebar-width:280px"
    );

    // Markdown files grow the renderer's Preview|Raw toggle (design D6), so
    // the container asserts the body mounts, not the raw text alone.
    await waitFor(() =>
      expect(screen.getByTestId("panel-content").textContent).toContain(
        "content of notes.md"
      )
    );
    expect(mockedClient.chat.workspaceFile).toHaveBeenCalledWith(
      "a1",
      "notes.md"
    );

    await unmount();
    await clearTabs();
  });

  it("closing the last tab returns the identity view", async () => {
    const { unmount } = await renderUI(<RightBar />);
    await openFileTab("notes.md");

    expect(screen.getByTestId("panel-tabs")).toBeTruthy();

    await act(async () => {
      fireEvent.click(closeButton(0));
    });

    expect(screen.getByTestId("chat-right-bar")).toBeTruthy();
    expect(screen.queryByTestId("panel-tabs")).toBeNull();
    expect(screen.getByTestId("sidebar-agent-name").textContent).toBe("Scout");

    await unmount();
    await clearTabs();
  });

  it("closes tabs per tab and keeps the active tab when another closes", async () => {
    const { unmount } = await renderUI(<RightBar />);
    await openFileTab("notes.md");
    await openFileTab("todo.md");

    expect(openTabCount()).toBe(2);

    // Focus the first tab, close the second: the first stays active.
    await act(async () => {
      fireEvent.click(tabButton(0));
    });
    await act(async () => {
      fireEvent.click(closeButton(1));
    });

    expect(openTabCount()).toBe(1);
    await waitFor(() =>
      expect(screen.getByTestId("panel-content").textContent).toContain(
        "content of notes.md"
      )
    );

    await unmount();
    await clearTabs();
  });

  it("deduplicates re-opens of the same file", async () => {
    const { unmount } = await renderUI(<RightBar />);
    await openFileTab("notes.md");
    await openFileTab("notes.md");

    expect(openTabCount()).toBe(1);

    await unmount();
    await clearTabs();
  });

  it("resets to the identity view when the session switches", async () => {
    const { unmount } = await renderUI(<RightBar />);
    await openFileTab("notes.md");

    expect(screen.getByTestId("panel-tabs")).toBeTruthy();

    await act(async () => {
      await useChatStore.getState().selectSession("s2");
    });

    // The store cleared the tabs (spec chat-panels: switching sessions
    // resets the panel), so the bar is back to the identity state.
    expect(screen.queryByTestId("panel-tabs")).toBeNull();
    expect(screen.getByTestId("sidebar-agent-name")).toBeTruthy();
    expect(mockedClient.chat.transcriptPage).toHaveBeenCalledWith(
      "s2",
      0,
      TRANSCRIPT_PAGE_SIZE
    );

    await unmount();
    await clearTabs();
  });

  it("renders nothing at all while collapsed — with tabs or without", async () => {
    useChatStore.setState({ sidebarCollapsed: true });

    const { unmount } = await renderUI(<RightBar />);

    expect(screen.queryByTestId("chat-right-bar")).toBeNull();
    expect(screen.queryByTestId("sidebar-agent-name")).toBeNull();

    await unmount();

    // Even with a preview open, minimizing hides the whole bar.
    await openFileTab("notes.md");

    const second = await renderUI(<RightBar />);

    expect(second.queryByTestId("chat-right-bar")).toBeNull();
    expect(second.queryByTestId("panel-tabs")).toBeNull();

    await second.unmount();
    await clearTabs();
  });

  it("starts at the persisted width and falls back to the default when unreadable", async () => {
    stubLocalStorage({ "buddies.chatSidebarWidth": "480" });

    const { unmount } = await renderUI(<RightBar />);

    expect(
      screen
        .getByTestId("chat-right-bar")
        .getAttribute("style")
        ?.replace(/\s+/g, "")
    ).toContain("--sidebar-width:480px");

    await unmount();

    stubLocalStorage({ "buddies.chatSidebarWidth": "not-a-number" });

    const second = await renderUI(<RightBar />);

    expect(
      second
        .getByTestId("chat-right-bar")
        .getAttribute("style")
        ?.replace(/\s+/g, "")
    ).toContain("--sidebar-width:280px");

    await second.unmount();
  });

  it("persists the dragged width clamped to the minimum under buddies.chatSidebarWidth", async () => {
    const { unmount } = await renderUI(<RightBar />);

    await act(async () => {
      fireEvent.pointerDown(screen.getByTestId("sidebar-resize"));
      // jsdom rects are all zero, so the bar's right edge sits at x=0:
      // dragging the pointer right of it drives the width below the clamp.
      window.dispatchEvent(new MouseEvent("pointermove", { clientX: 1000 }));
      window.dispatchEvent(new MouseEvent("pointerup"));
    });

    expect(localStorage.getItem("buddies.chatSidebarWidth")).toBe("220");

    await unmount();
  });

  it("offers the resize handle only while docked (xl+), in both states", async () => {
    stubLocalStorage();
    const { unmount } = await renderUI(<RightBar />);

    const handle = screen.getByTestId("sidebar-resize");

    expect(handle.className).toContain("hidden");
    expect(handle.className).toContain("xl:block");

    await unmount();

    // The handle rides the tab state's aside too (one width pref, D2).
    await openFileTab("notes.md");

    const second = await renderUI(<RightBar />);
    const tabHandle = second.getByTestId("sidebar-resize");

    expect(tabHandle.className).toContain("hidden");
    expect(tabHandle.className).toContain("xl:block");

    await second.unmount();
    await clearTabs();
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

    const { unmount } = await renderUI(<RightBar />);
    await openFileTab("notes.md");

    expect(screen.getByTestId("chat-right-bar")).toBeTruthy();

    // A delta lands while the bar shows a preview: the turn keeps folding.
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
    await clearTabs();
  });

  it("raises an already-open tab when its identity is re-opened", async () => {
    // The run-card row click is an activation gesture: clicking agent A,
    // then B, then A again must show A's content again — the dedupe on the
    // tab identity may not swallow the raise (user-reported regression).
    const { unmount } = await renderUI(<RightBar />);

    await openFileTab("docs/a.md");
    await openFileTab("docs/b.md");

    await waitFor(() =>
      expect(screen.getByTestId("panel-content").textContent).toContain(
        "content of docs/b.md"
      )
    );

    await openFileTab("docs/a.md");

    await waitFor(() =>
      expect(screen.getByTestId("panel-content").textContent).toContain(
        "content of docs/a.md"
      )
    );
    // Raised, not duplicated.
    expect(useChatStore.getState().panelTabs).toHaveLength(2);
    expect(useChatStore.getState().activePanelTabID).toBe(
      useChatStore.getState().panelTabs[0]?.id
    );

    await unmount();
    await clearTabs();
  });

  it("renders both states' token surfaces under light and dark themes", async () => {
    for (const theme of ["light", "dark"] as const) {
      document.documentElement.dataset.theme = theme;

      const { unmount } = await renderUI(<RightBar />);

      expect(screen.getByTestId("chat-right-bar").className).toContain(
        "bg-surface"
      );
      expect(screen.getByTestId("chat-right-bar").className).toContain(
        "border-line"
      );

      await unmount();

      await openFileTab("notes.md");

      const second = await renderUI(<RightBar />);
      const bar = second.getByTestId("chat-right-bar");

      expect(bar.className).toContain("bg-surface");
      expect(bar.className).toContain("border-line");

      await second.unmount();
      await clearTabs();
    }

    delete document.documentElement.dataset.theme;
  });
});
