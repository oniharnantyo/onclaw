/**
 * Right sidebar tests: the model row shows the session's EFFECTIVE model —
 * the override when set, else the agent's configured model — with the
 * effort appended when set. The context ring and its capability accounting
 * live on the composer's rail now (ContextMeter tests cover those), so the
 * sidebar's own contract is the identity/model row plus the minimized
 * behavior: collapsed renders nothing at all — no collapsed sliver island.
 * The docked column is drag-resizable (the preview panel's pattern): the
 * width starts from its persisted value, falls back when unreadable, and a
 * drag persists the clamped width.
 */
import { act } from "react";
import { fireEvent, screen } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  Agent,
  Session,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  ModelCapability,
  ProviderView,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client } from "../../../lib/client";
import { RightSidebar } from "../components/RightSidebar";
import { useChatStore } from "../store";
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

let capabilities: ModelCapability[];

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  settings: {
    providers: MockFn;
    providerModels: MockFn;
  };
};

function makeClient(): MockedClient {
  const c = {
    settings: {
      providers: vi.fn(async () => [
        { ID: "prov", Name: "Prov Co", Enabled: true },
      ]),
      providerModels: vi.fn(
        async (): Promise<ModelCapability[]> => capabilities
      ),
    },
  };

  // SAFETY: the literal above covers the client surface these tests touch.
  return c as MockedClient;
}

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../lib/client";

// Typed alias exposing the vi.fn mocks behind the Client-shaped export.
// SAFETY: the client module is mocked with makeClient(), so its runtime shape
// is exactly this mock object.
const mockedClient = client as MockedClient;

function seedStore(): void {
  useChatStore.setState({
    agents: [agent],
    providers: [provider],
    sessions: [session],
    activeAgentID: "a1",
    activeSessionID: "s1",
    usage: { used: 10_000, limit: 200_000, last_input: 0, last_output: 0 },
    sidebarCollapsed: false,
  });
}

// jsdom's localStorage would leak state between tests; the resize tests stub
// it against a Map instead (same fake as RightPanel.test.tsx).
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

beforeEach(() => {
  vi.clearAllMocks();
  capabilities = [];
  useChatStore.setState({
    agents: [],
    providers: [],
    sessions: [],
    activeAgentID: null,
    activeSessionID: null,
    usage: null,
    sidebarCollapsed: false,
  });
  seedStore();
});

describe("RightSidebar", () => {
  it("shows the agent's configured model when nothing is overridden", async () => {
    await renderUI(<RightSidebar />);
    expect(screen.getByTestId("sidebar-model").textContent).toBe(
      "Prov Co/gpt-test"
    );
    // The capability lookup moved to the composer's ContextMeter; the
    // sidebar never fetches and carries no usage readout of its own.
    expect(mockedClient.settings.providerModels).not.toHaveBeenCalled();
    expect(screen.queryByTestId("sidebar-usage")).toBeNull();
  });

  it("reflects the session override in the model row and the effort", async () => {
    useChatStore.setState({
      sessions: [{ ...session, Model: "big-model", Effort: "high" }],
    });

    await renderUI(<RightSidebar />);
    expect(screen.getByTestId("sidebar-model").textContent).toBe(
      "Prov Co/big-model · high"
    );
  });

  it("renders nothing at all while minimized — no collapsed sliver island", async () => {
    useChatStore.setState({ sidebarCollapsed: true });

    await renderUI(<RightSidebar />);
    expect(screen.queryByTestId("chat-sidebar")).toBeNull();
    expect(screen.queryByTestId("sidebar-agent-name")).toBeNull();
  });

  it("starts at the persisted width and falls back when it is unreadable", async () => {
    stubLocalStorage({ "buddies.chatSidebarWidth": "320" });

    const { unmount } = await renderUI(<RightSidebar />);

    expect(
      screen
        .getByTestId("chat-sidebar")
        .getAttribute("style")
        ?.replace(/\s+/g, "")
    ).toContain("--sidebar-width:320px");

    await unmount();

    stubLocalStorage({ "buddies.chatSidebarWidth": "not-a-number" });

    const second = await renderUI(<RightSidebar />);

    expect(
      second
        .getByTestId("chat-sidebar")
        .getAttribute("style")
        ?.replace(/\s+/g, "")
    ).toContain("--sidebar-width:240px");

    await second.unmount();
    vi.unstubAllGlobals();
  });

  it("persists the dragged width clamped to the minimum under buddies.chatSidebarWidth", async () => {
    stubLocalStorage();
    const { unmount } = await renderUI(<RightSidebar />);

    await act(async () => {
      fireEvent.pointerDown(screen.getByTestId("sidebar-resize"));
      // jsdom rects are all zero, so the panel's right edge sits at x=0:
      // dragging the pointer right of it drives the width below the clamp.
      window.dispatchEvent(new MouseEvent("pointermove", { clientX: 1000 }));
      window.dispatchEvent(new MouseEvent("pointerup"));
    });

    expect(localStorage.getItem("buddies.chatSidebarWidth")).toBe("200");

    await unmount();
    vi.unstubAllGlobals();
  });

  it("offers the handle only while the docked column exists (xl+)", async () => {
    stubLocalStorage();
    await renderUI(<RightSidebar />);

    const handle = screen.getByTestId("sidebar-resize");

    expect(handle.className).toContain("hidden");
    expect(handle.className).toContain("xl:block");

    vi.unstubAllGlobals();
  });
});
