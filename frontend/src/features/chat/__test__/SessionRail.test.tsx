/**
 * Sessions rail tests (tasks 2.1–2.2): the header ("Sessions" + search +
 * new-session) and entries render with the rail testids, the title filter
 * matches / no-matches / clears without ever moving the store's active
 * session, the active entry carries the kit SideRow's accent-tint active
 * state, the new-session button gates on the active agent, entries carry a
 * relative-time meta line, and no agent entries remain (the shell rail owns
 * navigation). The lib client is mocked wholesale like ChatView's suite; the
 * store is seeded directly, so no init traffic runs. Row handles are the kit
 * SideRow's data-od-id seam (task 2.4: assertions target the testid/od-id
 * seam and the token grammar, not daisyUI class strings).
 */
import { mockedSurface } from "./mockedSurface";
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import userEvent from "@testing-library/user-event";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vite-plus/test";

import type {
  Agent,
  Session,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../../../lib/client";
import { getToasts, type Toast } from "../../../lib/toasts";
import { useNavStore } from "../../../stores/nav";
import { SessionRail } from "../components/SessionRail";
import { useChatStore } from "../store";
import { renderUI } from "./test-utils";

const agent: Agent[] = [
  {
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
  },
];

let sessions: Session[];

function minutesAgo(minutes: number): string {
  return new Date(Date.now() - minutes * 60_000).toISOString();
}

// jsdom has no matchMedia; the reduced-motion stub follows the pattern from
// OnboardingLoader's suite (restored after each test that stubs it).
const savedMatchMedia = window.matchMedia;

/** The piece of MediaQueryList the rail reads. */
interface MediaMatches {
  matches: boolean;
}

function stubMatchMedia(matches: boolean): void {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: (query: string): MediaMatches => ({
      matches: matches && query === "(prefers-reduced-motion: reduce)",
    }),
  });
}

afterEach(() => {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: savedMatchMedia,
  });
});

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  agents: {
    list: MockFn;
    get: MockFn;
    create: MockFn;
    update: MockFn;
    delete: MockFn;
  };
  chat: {
    sessions: MockFn;
    createSession: MockFn;
    send: MockFn;
    subscribe: MockFn;
    transcript: MockFn;
    unsubscribe: MockFn;
    contextUsage: MockFn;
    deleteSession: MockFn;
  };
  settings: {
    providers: MockFn;
    skills: MockFn;
  };
};

function makeClient(): MockedClient {
  const c = {
    agents: {
      list: vi.fn(async () => agent),
      get: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
    },
    chat: {
      sessions: vi.fn(async () => sessions),
      createSession: vi.fn(),
      send: vi.fn(async () => "turn-1"),
      subscribe: vi.fn(async () => (): void => undefined),
      transcript: vi.fn(async () => []),
      transcriptPage: vi.fn(async () => []),
      unsubscribe: vi.fn(async () => undefined),
      contextUsage: vi.fn(async () => ({ used: 0, limit: 1 })),
      deleteSession: vi.fn(async () => undefined),
    },
    settings: {
      providers: vi.fn(async () => []),
      skills: vi.fn(async () => []),
    },
  };

  // SAFETY: the object literal above satisfies the Client surface with its
  // vi.fn members matching the MockedClient contract.
  // The literal mocks only the surface each suite exercises; the
  // Client-typed intersection defeats the structural check, so the
  // conversion funnels through the shared mockedSurface bridge.
  return mockedSurface<MockedClient, object>(c);
}

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../lib/client";

// Typed alias exposing the vi.fn mocks behind the Client-shaped export.
// SAFETY: the client module is mocked with makeClient(), so its runtime shape
// is exactly this mock object.
const mockedClient = client as MockedClient;

async function renderIsland(): Promise<void> {
  await renderUI(<SessionRail />);
}

/** SideRow's test seam (design D3): each row carries data-od-id. */
function row(id: string): HTMLElement | null {
  return screen
    .getByTestId("chat-session-rail")
    .querySelector(`[data-od-id="chat-session-${id}"]`);
}

/** SideRow's active fill: a 15% accent tint over the surface. */
const ACTIVE_TINT = "bg-[color-mix(in_oklab,var(--accent)_15%,transparent)]";

function seedStore(): void {
  useChatStore.setState({
    agents: agent,
    activeAgentID: "a1",
    sessions,
    activeSessionID: "sB",
    busy: false,
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  useChatStore.setState({
    agents: [],
    activeAgentID: null,
    sessions: [],
    activeSessionID: null,
    busy: false,
  });
  sessions = [
    {
      ID: "sA",
      AgentID: "a1",
      Title: "Retro planning",
      Model: "",
      Effort: "",
      CreatedAt: minutesAgo(60),
      LastActivityAt: minutesAgo(50),
    },
    {
      ID: "sB",
      AgentID: "a1",
      Title: "Weekly notes",
      Model: "",
      Effort: "",
      CreatedAt: minutesAgo(3),
      LastActivityAt: minutesAgo(2),
    },
  ];
  seedStore();
});

describe("SessionRail", () => {
  it("renders the rail header, search pill, and session entries", async () => {
    await renderIsland();
    expect(screen.getByTestId("chat-session-rail")).toBeTruthy();
    expect(screen.getByText("Sessions")).toBeTruthy();
    expect(screen.getByTestId("session-search")).toBeTruthy();
    expect(screen.getByTestId("btn-new-session")).toBeTruthy();
    expect(row("sA")).toBeTruthy();
    expect(row("sB")).toBeTruthy();
  });

  it("shows the store-empty text when no sessions exist", async () => {
    useChatStore.setState({ sessions: [], activeSessionID: null });
    await renderIsland();
    expect(screen.getByText("No sessions yet")).toBeTruthy();
  });

  it("filters entries by title and restores the full list on clear", async () => {
    await renderIsland();
    const user = userEvent.setup();
    // SAFETY: the search pill renders an input by contract.
    const search = screen.getByTestId("session-search") as HTMLInputElement;

    await user.type(search, "weekly");
    expect(row("sA")).toBeNull();
    expect(row("sB")).toBeTruthy();

    await user.type(search, "zzz");
    expect(screen.getByText("No matching sessions")).toBeTruthy();
    expect(row("sB")).toBeNull();

    await user.clear(search);
    expect(row("sA")).toBeTruthy();
    expect(row("sB")).toBeTruthy();
    // Filtering never moved the active session.
    expect(useChatStore.getState().activeSessionID).toBe("sB");
    expect(row("sB")?.className).toContain(ACTIVE_TINT);
  });

  it("highlights the active session with the SideRow accent tint", async () => {
    await renderIsland();
    expect(row("sB")?.className).toContain(ACTIVE_TINT);

    const inactive = row("sA");
    expect(inactive?.className).not.toContain(ACTIVE_TINT);
  });

  it("disables new-session while no agent is active", async () => {
    useChatStore.setState({ activeAgentID: null });
    await renderIsland();
    // SAFETY: the new-session control renders a button by contract.
    const button = screen.getByTestId("btn-new-session") as HTMLButtonElement;
    expect(button.disabled).toBe(true);
  });

  it("creates a session for the active agent from the island header", async () => {
    const fresh: Session = {
      ID: "sNEW",
      AgentID: "a1",
      Title: "",
      Model: "",
      Effort: "",
      CreatedAt: minutesAgo(0),
      LastActivityAt: minutesAgo(0),
    };

    mockedClient.chat.createSession.mockImplementation(async () => fresh);

    await renderIsland();
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-new-session"));
    });
    await waitFor(() =>
      expect(mockedClient.chat.createSession).toHaveBeenCalledWith("a1")
    );
    expect(useChatStore.getState().activeSessionID).toBe("sNEW");
    expect(row("sNEW")).toBeTruthy();
  });

  it("renders title-only 26px rows without the relative-time meta line", async () => {
    await renderIsland();
    // The meta line is dropped (task 7.1): no relative time in any row.
    expect(row("sA")?.textContent).not.toContain("50m");
    expect(row("sB")?.textContent).not.toContain("2m");
    // The compact grammar shrinks the kit SideRow's 30px button from the
    // row wrapper.
    expect(row("sA")?.parentElement?.className).toContain(
      "[&>button]:h-[26px]"
    );
  });

  it("runs the inline spinner before a pulsing title while motion is allowed", async () => {
    stubMatchMedia(false);
    useChatStore.setState({ busy: true, runningSessions: new Set(["sB"]) });
    await renderIsland();

    const spinner = row("sB")?.querySelector(
      '[data-testid="session-running-sB"]'
    );

    const label = row("sB")?.querySelector(".truncate");
    expect(spinner).toBeTruthy();
    expect(label).toBeTruthy();
    // The spinner rides the row button's icon slot — before the title.
    expect(
      label!.compareDocumentPosition(spinner!) &
        Node.DOCUMENT_POSITION_PRECEDING
    ).toBeTruthy();
    expect(spinner?.className).toContain("animate-spin");
    expect(row("sB")?.parentElement?.className).toContain(
      "[&_.truncate]:animate-pulse"
    );
  });

  it("keeps the running indicator but renders it static under reduced motion", async () => {
    stubMatchMedia(true);
    useChatStore.setState({ busy: true, runningSessions: new Set(["sB"]) });
    await renderIsland();

    const spinner = row("sB")?.querySelector(
      '[data-testid="session-running-sB"]'
    );

    expect(spinner).toBeTruthy();
    expect(spinner?.className).not.toContain("animate-spin");
    expect(row("sB")?.parentElement?.className).not.toContain(
      "[&_.truncate]:animate-pulse"
    );
  });

  it("flags the in-flight session's entry with a running indicator", async () => {
    useChatStore.setState({ busy: true, runningSessions: new Set(["sB"]) });
    await renderIsland();
    expect(
      row("sB")?.querySelector('[data-testid="session-running-sB"]')
    ).toBeTruthy();
    // Only the session with the turn in flight carries it.
    expect(
      row("sA")?.querySelector('[data-testid^="session-running"]')
    ).toBeNull();
  });

  it("flags a background session's turn from the per-session running set", async () => {
    // The turn runs in a NON-open session: busy is false (the open session
    // is idle) but the rail still flags the entry (spec chat-view session
    // rows: per-session running state).
    useChatStore.setState({ busy: false, runningSessions: new Set(["sA"]) });
    await renderIsland();
    expect(
      row("sA")?.querySelector('[data-testid="session-running-sA"]')
    ).toBeTruthy();
    expect(
      row("sB")?.querySelector('[data-testid^="session-running"]')
    ).toBeNull();
  });

  it("drops the running indicator when the turn reaches a terminal outcome", async () => {
    useChatStore.setState({ busy: true, runningSessions: new Set(["sB"]) });
    await renderIsland();
    expect(
      row("sB")?.querySelector('[data-testid="session-running-sB"]')
    ).toBeTruthy();

    await act(async () => {
      useChatStore.setState({ runningSessions: new Set<string>() });
    });
    expect(
      row("sB")?.querySelector('[data-testid="session-running-sB"]')
    ).toBeNull();
  });

  it("leaves navigation to the shell rail: no agent entries remain", async () => {
    await renderIsland();
    expect(screen.queryByText("Agents")).toBeNull();
    expect(screen.queryByTestId("chat-agent-a1")).toBeNull();
  });

  // When the nav rail is collapsed its 80px shell is reserved for the
  // macOS traffic lights, so the rail's expand control lives here — in a
  // row above the SESSIONS label; clicking it flips the nav store (and
  // persists) and the row disappears with the rail expanded again.
  it("hosts the rail expand control above the label while the nav rail is collapsed", async () => {
    useNavStore.setState({ railCollapsed: true });
    await renderIsland();
    const button = screen.getByTestId("btn-toggle-rail");

    expect(button.getAttribute("aria-label")).toBe("Expand navigation");
    // The control precedes the SESSIONS label in the column (above it).
    const label = screen.getByText("Sessions");
    expect(
      label.compareDocumentPosition(button) & Node.DOCUMENT_POSITION_PRECEDING
    ).toBeTruthy();

    await act(async () => {
      fireEvent.click(button);
    });
    expect(useNavStore.getState().railCollapsed).toBe(false);
    expect(window.localStorage.getItem("buddies.railCollapsed")).toBe("0");
    expect(screen.queryByTestId("btn-toggle-rail")).toBeNull();
  });

  it("renders no expand control while the nav rail is expanded", async () => {
    await renderIsland();
    expect(screen.queryByTestId("btn-toggle-rail")).toBeNull();
  });
});

describe("SessionRail delete (task 6.5)", () => {
  it("deletes only after explicit confirmation; the entry disappears", async () => {
    await renderIsland();
    await act(async () => {
      fireEvent.click(screen.getByTestId("session-delete-sA"));
    });
    // The dialog guards the delete: nothing has happened yet.
    expect(screen.getByTestId("confirm-delete-session")).toBeTruthy();
    expect(mockedClient.chat.deleteSession).not.toHaveBeenCalled();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-confirm-delete-session"));
    });
    await waitFor(() =>
      expect(mockedClient.chat.deleteSession).toHaveBeenCalledWith("sA")
    );
    await waitFor(() => expect(row("sA")).toBeNull());
    // The agent's other sessions are untouched.
    expect(row("sB")).toBeTruthy();
  });

  it("cancelling the confirmation is a no-op", async () => {
    await renderIsland();
    await act(async () => {
      fireEvent.click(screen.getByTestId("session-delete-sA"));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-cancel-delete-session"));
    });

    expect(mockedClient.chat.deleteSession).not.toHaveBeenCalled();
    expect(screen.queryByTestId("confirm-delete-session")).toBeNull();
    expect(row("sA")).toBeTruthy();
  });

  it("a refused delete (turn in flight) toasts the refusal and keeps the row", async () => {
    mockedClient.chat.deleteSession.mockRejectedValueOnce(
      new Error("session has a turn in flight")
    );
    await renderIsland();
    await act(async () => {
      fireEvent.click(screen.getByTestId("session-delete-sA"));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-confirm-delete-session"));
    });
    await waitFor(() =>
      expect(
        getToasts().some(
          (t: Toast): boolean =>
            t.kind === "danger" && t.text.includes("turn in flight")
        )
      ).toBe(true)
    );
    // The refusal leaves the entry in place.
    expect(row("sA")).toBeTruthy();
  });
});

describe("SessionRail resize", () => {
  // jsdom's localStorage would leak state between tests; every test here
  // stubs it against a Map instead (same fake as RightPanel.test.tsx).
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

  it("starts at the persisted width and falls back when it is unreadable", async () => {
    stubLocalStorage({ "buddies.sessionRailWidth": "320" });

    const { unmount } = await renderUI(<SessionRail />);

    expect(
      screen
        .getByTestId("chat-session-rail")
        .getAttribute("style")
        ?.replace(/\s+/g, "")
    ).toContain("--rail-width:320px");

    await unmount();

    stubLocalStorage({ "buddies.sessionRailWidth": "not-a-number" });

    const second = await renderUI(<SessionRail />);

    expect(
      second
        .getByTestId("chat-session-rail")
        .getAttribute("style")
        ?.replace(/\s+/g, "")
    ).toContain("--rail-width:224px");

    await second.unmount();
  });

  it("persists the dragged width under buddies.sessionRailWidth", async () => {
    stubLocalStorage();
    const { unmount } = await renderUI(<SessionRail />);

    await act(async () => {
      fireEvent.pointerDown(screen.getByTestId("session-rail-resize"));
      // jsdom rects are all zero, so the rail's left edge sits at x=0 and
      // the width follows the pointer; 300 stays inside the clamp.
      window.dispatchEvent(new MouseEvent("pointermove", { clientX: 300 }));
      window.dispatchEvent(new MouseEvent("pointerup"));
    });

    expect(localStorage.getItem("buddies.sessionRailWidth")).toBe("300");

    await unmount();
  });

  it("clamps a drag that would squeeze the rail below its minimum", async () => {
    stubLocalStorage();
    const { unmount } = await renderUI(<SessionRail />);

    await act(async () => {
      fireEvent.pointerDown(screen.getByTestId("session-rail-resize"));
      window.dispatchEvent(new MouseEvent("pointermove", { clientX: 40 }));
      window.dispatchEvent(new MouseEvent("pointerup"));
    });

    expect(localStorage.getItem("buddies.sessionRailWidth")).toBe("176");

    await unmount();
    vi.unstubAllGlobals();
  });

  it("offers the handle only while the rail itself is visible (md+)", async () => {
    stubLocalStorage();
    await renderIsland();

    const handle = screen.getByTestId("session-rail-resize");

    expect(handle.className).toContain("hidden");
    expect(handle.className).toContain("md:block");

    vi.unstubAllGlobals();
  });
});

describe("SessionRail list cap (tasks 7.2, design D11)", () => {
  function makeSession(id: string, title: string, recency: number): Session {
    return {
      ID: id,
      AgentID: "a1",
      Title: title,
      Model: "",
      Effort: "",
      CreatedAt: minutesAgo(recency),
      LastActivityAt: minutesAgo(recency),
    };
  }

  /** `n` sessions, most recent first (the store's order): s1 is newest. */
  function seedSessions(n: number): Session[] {
    const list: Session[] = [];

    for (let i = 0; i < n; i += 1) {
      list.push(makeSession(`s${i + 1}`, `S${i + 1}`, i + 1));
    }

    return list;
  }

  function seed(n: number, activeID: string): void {
    useChatStore.setState({
      sessions: seedSessions(n),
      activeSessionID: activeID,
    });
  }

  it("caps the unfiltered list at the four most recent sessions", async () => {
    seed(6, "s2");
    await renderIsland();

    for (const id of ["s1", "s2", "s3", "s4"]) {
      expect(row(id)).toBeTruthy();
    }

    expect(row("s5")).toBeNull();
    expect(row("s6")).toBeNull();
    expect(screen.getByTestId("btn-show-older").textContent).toBe(
      "Show 2 older sessions"
    );
  });

  it("keeps the active session visible when it is older than the cap", async () => {
    seed(6, "s6");
    await renderIsland();

    // The four most recent plus the active (oldest) one.
    for (const id of ["s1", "s2", "s3", "s4", "s6"]) {
      expect(row(id)).toBeTruthy();
    }

    expect(row("s5")).toBeNull();
    expect(screen.getByTestId("btn-show-older").textContent).toBe(
      "Show 1 older session"
    );
  });

  it("reveals the older sessions and collapses back to recent only", async () => {
    seed(6, "s2");
    await renderIsland();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-show-older"));
    });

    for (const id of ["s1", "s2", "s3", "s4", "s5", "s6"]) {
      expect(row(id)).toBeTruthy();
    }

    expect(screen.getByTestId("btn-show-recent").textContent).toBe(
      "Show recent only"
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-show-recent"));
    });
    expect(row("s5")).toBeNull();
    expect(row("s6")).toBeNull();
    expect(screen.getByTestId("btn-show-older")).toBeTruthy();
  });

  it("offers no controls while the list fits the cap", async () => {
    seed(4, "s1");
    await renderIsland();

    expect(screen.queryByTestId("btn-show-older")).toBeNull();
    expect(screen.queryByTestId("btn-show-recent")).toBeNull();
  });

  it("lifts the cap while a non-empty search lists every match", async () => {
    seed(6, "s2");
    await renderIsland();
    // SAFETY: the search pill renders an input by contract.
    const search = screen.getByTestId("session-search") as HTMLInputElement;
    const user = userEvent.setup();

    await user.type(search, "S");

    for (const id of ["s1", "s2", "s3", "s4", "s5", "s6"]) {
      expect(row(id)).toBeTruthy();
    }

    expect(screen.queryByTestId("btn-show-older")).toBeNull();
    expect(screen.queryByTestId("btn-show-recent")).toBeNull();

    // Clearing the search restores the cap.
    await user.clear(search);
    expect(row("s5")).toBeNull();
    expect(row("s6")).toBeNull();
  });

  it("scrolls within a max height only once more than eight rows are visible", async () => {
    seed(10, "s2");
    await renderIsland();

    const list = screen.getByTestId("session-list");
    // Collapsed: four rows, no scroll cap needed.
    expect(list.className).not.toContain("max-h-");

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-show-older"));
    });
    expect(list.className).toContain("max-h-[228px]");
    expect(list.className).toContain("min-h-0");
    expect(list.className).toContain("overflow-y-auto");

    // A search listing ten matches scrolls too.
    // SAFETY: the search pill renders an input by contract.
    const search = screen.getByTestId("session-search") as HTMLInputElement;
    await userEvent.setup().type(search, "S");
    expect(list.className).toContain("max-h-[228px]");
  });
});
