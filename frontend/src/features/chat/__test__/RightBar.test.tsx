/**
 * RightBar tests (merge-right-bar-previews, designs D1–D2): ONE right bar
 * with two states. With no tabs open it renders the session identity (the
 * old right sidebar's agent + effective-model row; no tab strip), and the
 * model row reflects the session override with the effort appended. Opening
 * a preview swaps the SAME aside in place to the old right panel's tab
 * strip + content — exactly one bar in the document, identity gone — and
 * closing the last tab returns the identity. The store's session switch
 * resets the tabs (identity returns), collapsing (refine-chat-surface-
 * identity D8) folds the bar to zero inline size while it stays mounted —
 * nothing paints, the transition stands down only while a drag is live —
 * and the restore animates from the same nodes, and the single width
 * preference persists under `buddies.chatSidebarWidth` (default 280, drag
 * clamped to 220–560) through the same CSS var in both states. The model
 * line distills to provider + family (D4) with the full id on hover. The
 * knowledge browsing state
 * (add-knowledge-corpus) rides the container too: the identity view's opener
 * swaps in the panel, a document activation opens its preview tab, and
 * closing the last tab returns the identity — never the browsing state.
 * Renderer internals (markdown modes, refusal/empty states, trajectory) live
 * with the renderers in panelRenderers — this suite owns the container
 * contract.
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
import type { Client, KnowledgeDocument } from "../../../lib/client";
import { applyEvent } from "../entries";
import { RightBar } from "../components/RightBar";
import { workspaceFileTab } from "../components/panelRenderers";
import { type ChatStore } from "../types";
import { TRANSCRIPT_PAGE_SIZE, useChatStore } from "../store";
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

const knowledgeDoc: KnowledgeDocument = {
  id: "d1",
  filename: "runbook.md",
  mime: "text/plain",
  size: 20,
  scope: "agent",
  agentId: "a1",
  agentSlug: "scout",
  description: "the runbook",
  source: "upload",
  createdAt: "2026-01-01T00:00:00Z",
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
  knowledge: {
    list: MockFn;
    content: MockFn;
  };
  events: {
    onKnowledgeChanged: MockFn;
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
    knowledge: {
      list: vi.fn(async (): Promise<never[]> => []),
      content: vi.fn(async (): Promise<string> => btoa("knowledge body of d1")),
    },
    events: {
      onKnowledgeChanged: vi.fn(
        (_handler: () => void): (() => void) =>
          () =>
            undefined
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

  it("shows provider + model family in the line with the full id on hover (D4)", async () => {
    useChatStore.setState({
      sessions: [{ ...session, Model: "openai/gpt-4o-mini", Effort: "high" }],
    });

    await renderUI(<RightBar />);

    const line = screen.getByTestId("sidebar-model");

    // Family label — the last `/`-delimited segment — next to the provider
    // display name; the full id moves to the hover title.
    expect(line.textContent).toBe("Prov Co/gpt-4o-mini · high");
    expect(line.getAttribute("title")).toBe("openai/gpt-4o-mini");
  });

  it("truncates the model line instead of breaking it (D4)", async () => {
    useChatStore.setState({
      sessions: [{ ...session, Model: "openai/gpt-4o-mini" }],
    });

    await renderUI(<RightBar />);

    const line = screen.getByTestId("sidebar-model");

    expect(line.className).toContain("truncate");
    expect(line.className).not.toContain("break-all");
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

  it("folds keep-mounted to zero size while collapsed and restores in place", async () => {
    const { unmount } = await renderUI(<RightBar />);
    const bar = screen.getByTestId("chat-right-bar");

    await act(async () => {
      useChatStore.setState({ sidebarCollapsed: true });
    });

    // Keep-mounted fold (refine-chat-surface-identity D8): the SAME aside
    // stays in the document at zero inline size — painting nothing,
    // intercepting no pointers, transition armed for the restore — and the
    // identity content survives inside it.
    expect(screen.getByTestId("chat-right-bar")).toBe(bar);
    expect(bar.style.width).toBe("0px");
    expect(bar.style.pointerEvents).toBe("none");
    expect(bar.style.overflow).toBe("hidden");
    expect(bar.style.transition).toContain("var(--motion-base)");
    expect(screen.getByTestId("sidebar-agent-name").textContent).toBe("Scout");

    await act(async () => {
      useChatStore.setState({ sidebarCollapsed: false });
    });

    // Restored from the same nodes — not a remount.
    expect(screen.getByTestId("chat-right-bar")).toBe(bar);
    expect(screen.getByTestId("sidebar-agent-name").textContent).toBe("Scout");
    expect(bar.style.width).toBe("");

    await unmount();

    // Even with a preview open, the fold swallows the whole bar.
    await openFileTab("notes.md");

    await act(async () => {
      useChatStore.setState({ sidebarCollapsed: true });
    });

    const second = await renderUI(<RightBar />);
    const tabBar = second.getByTestId("chat-right-bar");

    expect(tabBar.style.width).toBe("0px");
    expect(tabBar.style.pointerEvents).toBe("none");
    expect(second.getByTestId("panel-tabs")).toBeTruthy();
    expect(second.queryByTestId("sidebar-agent-name")).toBeNull();

    await second.unmount();
    await clearTabs();
  });

  it("makes the collapsed bar inert and drops it on restore (D8)", async () => {
    const { unmount } = await renderUI(<RightBar />);
    const bar = screen.getByTestId("chat-right-bar");

    // Extended: focusable while visible.
    expect(bar.hasAttribute("inert")).toBe(false);

    await act(async () => {
      useChatStore.setState({ sidebarCollapsed: true });
    });

    // Keep-mounted fold (refine-chat-surface-identity D8): the same aside
    // stays mounted, but `inert` removes its whole subtree from the
    // interaction tree — nothing focusable in the invisible fold.
    expect(screen.getByTestId("chat-right-bar")).toBe(bar);
    expect(bar.hasAttribute("inert")).toBe(true);

    await act(async () => {
      useChatStore.setState({ sidebarCollapsed: false });
    });

    // Restored from the same nodes — inert drops, and the controls are
    // focusable again.
    expect(screen.getByTestId("chat-right-bar")).toBe(bar);
    expect(bar.hasAttribute("inert")).toBe(false);

    await unmount();
  });

  it("stands the width transition down while dragging and re-arms on release (D8)", async () => {
    const { unmount } = await renderUI(<RightBar />);
    const bar = screen.getByTestId("chat-right-bar");

    expect(bar.style.transition).toContain("var(--motion-base)");

    await act(async () => {
      fireEvent.pointerDown(screen.getByTestId("sidebar-resize"));
      window.dispatchEvent(new MouseEvent("pointermove", { clientX: 400 }));
    });

    // 1:1 pointer tracking: no transition during the drag.
    expect(bar.style.transition).toBe("none");

    await act(async () => {
      window.dispatchEvent(new MouseEvent("pointerup"));
    });

    expect(bar.style.transition).toContain("var(--motion-base)");

    await unmount();
  });

  it("re-arms the width transition when the drag is cancelled (D8)", async () => {
    const { unmount } = await renderUI(<RightBar />);
    const bar = screen.getByTestId("chat-right-bar");

    await act(async () => {
      fireEvent.pointerDown(screen.getByTestId("sidebar-resize"));
      window.dispatchEvent(new MouseEvent("pointermove", { clientX: 400 }));
    });

    // 1:1 pointer tracking: no transition during the drag.
    expect(bar.style.transition).toBe("none");

    await act(async () => {
      window.dispatchEvent(new MouseEvent("pointercancel"));
    });

    // Cancelled — the transition re-arms (same observable as the release
    // test above; SessionRail's drag contract mirrored).
    expect(bar.style.transition).toContain("var(--motion-base)");

    await unmount();
  });

  it("replays the od-fade on the shared motion grammar when the surface swaps (D8)", async () => {
    const { unmount } = await renderUI(<RightBar />);
    await openFileTab("notes.md");

    const preview = screen.getByTestId("panel-surface");

    expect(preview.className).toContain("od-fade");
    // One motion grammar (refine-chat-surface-identity ui-conventions): the
    // class carries the base duration + standard ease — no per-instance
    // fast-duration override on the node.
    expect(preview.style.animationDuration).toBe("");

    // Preview → identity (the pinned tab) swaps the surface node, so the
    // fade replays on every identity↔preview swap.
    await act(async () => {
      fireEvent.click(screen.getByTestId("panel-identity-tab"));
    });

    const identity = screen.getByTestId("panel-surface");

    expect(identity).not.toBe(preview);
    expect(identity.className).toContain("od-fade");
    expect(identity.style.animationDuration).toBe("");

    await unmount();
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

describe("RightBar — knowledge panel (add-knowledge-corpus)", () => {
  it("swaps the identity island for the knowledge panel on the opener", async () => {
    const { unmount } = await renderUI(<RightBar />);

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-opener"));
    });

    // Still one bar: the panel replaced the identity in place.
    expect(screen.getAllByTestId("chat-right-bar")).toHaveLength(1);
    expect(screen.getByTestId("knowledge-panel")).toBeTruthy();
    expect(screen.queryByTestId("sidebar-agent-name")).toBeNull();
    expect(mockedClient.knowledge.list).toHaveBeenCalled();

    await unmount();
    await clearTabs();
  });

  it("returns to the identity view with Back", async () => {
    const { unmount } = await renderUI(<RightBar />);

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-opener"));
    });

    expect(screen.getByTestId("knowledge-panel")).toBeTruthy();

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-back"));
    });

    expect(screen.getByTestId("sidebar-agent-name")).toBeTruthy();
    expect(screen.queryByTestId("knowledge-panel")).toBeNull();

    await unmount();
    await clearTabs();
  });

  it("activating a knowledge document opens its preview tab", async () => {
    mockedClient.knowledge.list.mockImplementation(
      async (): Promise<KnowledgeDocument[]> => [knowledgeDoc]
    );

    const { unmount } = await renderUI(<RightBar />);

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-opener"));
    });

    await waitFor(() => screen.getByTestId("knowledge-doc-d1"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-doc-d1"));
    });

    // The browsing state yields to the tabs view; the store holds the
    // knowledge-doc tab built from the catalog row.
    expect(screen.getByTestId("panel-tabs")).toBeTruthy();
    expect(screen.queryByTestId("knowledge-panel")).toBeNull();
    expect(useChatStore.getState().panelTabs).toHaveLength(1);
    expect(useChatStore.getState().panelTabs[0]?.kind).toBe("knowledge-doc");
    expect(useChatStore.getState().panelTabs[0]?.identity).toBe(
      "knowledge-doc:d1"
    );

    // The renderer decoded the document's bytes into the tab body.
    await waitFor(() =>
      expect(screen.getByTestId("panel-content").textContent).toContain(
        "knowledge body of d1"
      )
    );

    await unmount();
    await clearTabs();
  });

  it("an externally opened tab leaves the browsing state, and closing returns the identity view", async () => {
    const { unmount } = await renderUI(<RightBar />);

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-opener"));
    });

    expect(screen.getByTestId("knowledge-panel")).toBeTruthy();

    // A tab opened from outside the panel (a tool-card link) yields the bar
    // to the tabs view.
    await openFileTab("notes.md");

    expect(screen.getByTestId("panel-tabs")).toBeTruthy();
    expect(screen.queryByTestId("knowledge-panel")).toBeNull();

    // Closing the last tab returns the IDENTITY view — the opener, never
    // the browsing state (it does not persist across that boundary).
    await act(async () => {
      fireEvent.click(closeButton(0));
    });

    expect(screen.getByTestId("sidebar-agent-name")).toBeTruthy();
    expect(screen.queryByTestId("knowledge-panel")).toBeNull();

    await unmount();
    await clearTabs();
  });
});

describe("RightBar — pinned identity tab (add-pinned-identity-tab)", () => {
  it("leads the strip with an uncloseable pinned tab, absent at zero tabs", async () => {
    const { unmount } = await renderUI(<RightBar />);

    // No strip, no pinned tab.
    expect(screen.queryByTestId("panel-identity-tab")).toBeNull();

    await openFileTab("notes.md");

    const pinned = screen.getByTestId("panel-identity-tab");

    // First entry of the strip, named for the agent, no close affordance —
    // the strip's close buttons all belong to preview tabs.
    expect(screen.getByTestId("panel-tabs").firstElementChild).toBe(pinned);
    expect(pinned.getAttribute("aria-label")).toBe("Scout");
    expect(
      pinned.querySelectorAll("[data-testid^='panel-tab-close-']")
    ).toHaveLength(0);
    expect(openTabCount()).toBe(1);

    // The preview is still the active surface; the pinned tab reads inactive.
    await waitFor(() =>
      expect(screen.getByTestId("panel-content").textContent).toContain(
        "content of notes.md"
      )
    );
    expect(pinned.getAttribute("aria-current")).toBeNull();

    await unmount();
    await clearTabs();
  });

  it("activating the pinned tab returns the identity view, tabs still open", async () => {
    const { unmount } = await renderUI(<RightBar />);
    await openFileTab("notes.md");

    await act(async () => {
      fireEvent.click(screen.getByTestId("panel-identity-tab"));
    });

    // The identity content renders inside the tab shell — the name and the
    // live 64 px face (the strip's label avatar carries no state attribute).
    expect(screen.getByTestId("sidebar-agent-name").textContent).toBe("Scout");
    expect(
      screen.getByTestId("panel-content").querySelector("[data-avatar-state]")
    ).toBeTruthy();
    expect(
      screen
        .getByTestId("panel-identity-tab")
        .querySelector("[data-avatar-state]")
    ).toBeNull();

    // Every open tab stays listed; the preview text is gone from the body.
    expect(openTabCount()).toBe(1);
    expect(screen.getByTestId("panel-content").textContent).not.toContain(
      "content of notes.md"
    );

    // The pinned tab reads active; the preview tab's wrapper reads inactive.
    const pinned = screen.getByTestId("panel-identity-tab");

    expect(pinned.getAttribute("aria-current")).toBe("true");
    expect(pinned.className).toContain("border-line");
    expect(tabButton(0).closest("div")?.className).toContain(
      "border-transparent"
    );

    await unmount();
    await clearTabs();
  });

  it("a preview opened or re-activated from the identity tab takes over", async () => {
    const { unmount } = await renderUI(<RightBar />);
    await openFileTab("notes.md");

    await act(async () => {
      fireEvent.click(screen.getByTestId("panel-identity-tab"));
    });

    // Opening a new file from the identity tab activates it in place.
    await openFileTab("todo.md");

    await waitFor(() =>
      expect(screen.getByTestId("panel-content").textContent).toContain(
        "content of todo.md"
      )
    );
    expect(screen.getByTestId("panel-content").textContent).not.toContain(
      "content of notes.md"
    );

    // Back to the identity, then re-activate the FIRST preview by its tab.
    await act(async () => {
      fireEvent.click(screen.getByTestId("panel-identity-tab"));
    });
    await act(async () => {
      fireEvent.click(tabButton(0));
    });

    await waitFor(() =>
      expect(screen.getByTestId("panel-content").textContent).toContain(
        "content of notes.md"
      )
    );

    await unmount();
    await clearTabs();
  });

  it("closing a background tab while the identity is active keeps the identity", async () => {
    const { unmount } = await renderUI(<RightBar />);
    await openFileTab("notes.md");
    await openFileTab("todo.md");

    await act(async () => {
      fireEvent.click(screen.getByTestId("panel-identity-tab"));
    });

    expect(screen.getByTestId("sidebar-agent-name")).toBeTruthy();

    // The closed tab was not the active surface, so the identity stays.
    await act(async () => {
      fireEvent.click(closeButton(0));
    });

    expect(screen.getByTestId("sidebar-agent-name")).toBeTruthy();
    expect(openTabCount()).toBe(1);

    const pinned = screen.getByTestId("panel-identity-tab");

    expect(pinned.getAttribute("aria-current")).toBe("true");

    await unmount();
    await clearTabs();
  });

  it("closing the last tab from the identity tab returns the island", async () => {
    const { unmount } = await renderUI(<RightBar />);
    await openFileTab("notes.md");

    await act(async () => {
      fireEvent.click(screen.getByTestId("panel-identity-tab"));
    });

    await act(async () => {
      fireEvent.click(closeButton(0));
    });

    expect(screen.queryByTestId("panel-tabs")).toBeNull();
    expect(screen.getByTestId("sidebar-agent-name").textContent).toBe("Scout");
    expect(screen.queryByTestId("panel-identity-tab")).toBeNull();

    await unmount();
    await clearTabs();
  });

  it("opens knowledge from the identity tab and resets it when a doc opens", async () => {
    mockedClient.knowledge.list.mockImplementation(
      async (): Promise<KnowledgeDocument[]> => [knowledgeDoc]
    );

    const { unmount } = await renderUI(<RightBar />);
    await openFileTab("notes.md");

    await act(async () => {
      fireEvent.click(screen.getByTestId("panel-identity-tab"));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-opener"));
    });

    // The browser renders inside the tab shell's content area.
    expect(
      screen
        .getByTestId("panel-content")
        .contains(screen.getByTestId("knowledge-panel"))
    ).toBe(true);

    // Activating a document opens its preview tab — the browsing state ends.
    await waitFor(() => screen.getByTestId("knowledge-doc-d1"));
    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-doc-d1"));
    });

    await waitFor(() =>
      expect(screen.getByTestId("panel-content").textContent).toContain(
        "knowledge body of d1"
      )
    );
    expect(useChatStore.getState().panelTabs).toHaveLength(2);
    expect(useChatStore.getState().panelTabs[1]?.kind).toBe("knowledge-doc");

    // Returning via the pinned tab shows the identity content, never the
    // browser (the browsing state does not survive leaving the surface).
    await act(async () => {
      fireEvent.click(screen.getByTestId("panel-identity-tab"));
    });

    expect(screen.getByTestId("sidebar-agent-name")).toBeTruthy();
    expect(screen.queryByTestId("knowledge-panel")).toBeNull();

    await unmount();
    await clearTabs();
  });

  it("renders the pinned tab's inactive and active tokens under both themes", async () => {
    for (const theme of ["light", "dark"] as const) {
      document.documentElement.dataset.theme = theme;

      const { unmount } = await renderUI(<RightBar />);
      await openFileTab("notes.md");

      // Inactive: muted and borderless, no aria-current.
      const pinned = screen.getByTestId("panel-identity-tab");

      expect(pinned.className).toContain("border-transparent");
      expect(pinned.className).toContain("text-muted");
      expect(pinned.getAttribute("aria-current")).toBeNull();

      await act(async () => {
        fireEvent.click(pinned);
      });

      // Active: the strip's active tokens plus the aria-current flag.
      const active = screen.getByTestId("panel-identity-tab");

      expect(active.className).toContain("border-line");
      expect(active.className).toContain("text-fg");
      expect(active.getAttribute("aria-current")).toBe("true");

      await unmount();
      await clearTabs();
    }

    delete document.documentElement.dataset.theme;
  });
});
