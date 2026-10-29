/**
 * Sessions rail tests (add-grouped-session-rail, tasks 4.1–4.5, design
 * D4/D5): the all-agent accordion — groups start collapsed with avatar,
 * name, session count, and a per-agent new-session control, except the
 * active agent's group, which is expanded; the header's flat new-session
 * button is gone. A collapsed row's "+" creates a session for ITS agent
 * without requiring it to be active, expanding a group changes nothing but
 * the expansion, a context move (activeAgentID) moves the expansion, the
 * group running dot rides the app-wide running set (never-opened sessions
 * included), load-more batches 4 → +8 → retire per group, and the search
 * spans every agent — matching groups expand and list every match, clearing
 * restores the collapse grammar without ever moving the active session.
 * The lib client is mocked wholesale like ChatView's suite (now including
 * the store's new allSessions / runningSessions / onTurnsChanged surface);
 * the store is seeded directly — real store actions are driven where the
 * flow owns them (cross-agent session clicks, create, delete), and the
 * new-session action is replaced with a spy where the rail's own contract
 * (passing the group's agent id) is what's under test. Row handles are the
 * kit SideRow's data-od-id seam (task 2.4: assertions target the
 * testid/od-id seam and the token grammar, not daisyUI class strings).
 * The refine-chat-surface-identity coverage rides the same grammar (tasks
 * 1.4 + 7.1/7.2): each row's relative-time meta line is read against a
 * reference-clock fixture (no fake timers), the group header reports
 * aria-expanded, the active row carries aria-current, the delete and
 * group-+ targets are 24px hit areas around their glyphs, the reveal binds
 * grid-template-rows 0fr↔1fr on the motion tokens with the chevron in
 * step, a collapsed group's rows stay mounted but frozen (inert +
 * aria-hidden — the close animates with real content height), and the
 * resize drag drops the width transition for 1:1 pointer tracking and
 * re-arms on release.
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

const agents: Agent[] = [
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
  {
    ID: "a2",
    Name: "Mule",
    ProviderID: "prov",
    Model: "gpt-test",
    Effort: "",
    Slug: "mule",
    Description: "",
    Brief: "",
    Avatar: null,
    SubagentsEnabled: false,
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
  {
    ID: "a3",
    Name: "Iris",
    ProviderID: "prov",
    Model: "gpt-test",
    Effort: "",
    Slug: "iris",
    Description: "",
    Brief: "",
    Avatar: null,
    SubagentsEnabled: false,
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

function makeSession(
  id: string,
  agentID: string,
  title: string,
  recency: number
): Session {
  return {
    ID: id,
    AgentID: agentID,
    Title: title,
    Model: "",
    Effort: "",
    CreatedAt: minutesAgo(recency),
    LastActivityAt: minutesAgo(recency),
  };
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
    // add-grouped-session-rail (task 3.1): the store's all-sessions model —
    // every agent's rows plus the app-wide running set the rail consumes.
    allSessions: MockFn;
    runningSessions: MockFn;
    createSession: MockFn;
    send: MockFn;
    subscribe: MockFn;
    transcript: MockFn;
    transcriptPage: MockFn;
    turnState: MockFn;
    unsubscribe: MockFn;
    contextUsage: MockFn;
    deleteSession: MockFn;
  };
  settings: {
    providers: MockFn;
    skills: MockFn;
  };
  events: {
    onTurnsChanged: MockFn;
  };
};

function makeClient(): MockedClient {
  const unsubscribe = (): void => undefined;

  const c = {
    agents: {
      list: vi.fn(async () => agents),
      get: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
    },
    chat: {
      // Until the store's all-sessions model lands, selectAgent still reads
      // the per-agent listing; both mocks hand out the same seeded rows, so
      // the real store actions run identically under either shape.
      sessions: vi.fn(async () => sessions),
      allSessions: vi.fn(async () => sessions),
      runningSessions: vi.fn(async (): Promise<string[]> => []),
      createSession: vi.fn(),
      send: vi.fn(async () => "turn-1"),
      subscribe: vi.fn(async () => unsubscribe),
      transcript: vi.fn(async () => []),
      transcriptPage: vi.fn(async () => []),
      turnState: vi.fn(async () => ({ busy: false, turn_id: "" })),
      unsubscribe: vi.fn(async () => undefined),
      contextUsage: vi.fn(async () => ({ used: 0, limit: 1 })),
      deleteSession: vi.fn(async () => undefined),
    },
    settings: {
      providers: vi.fn(async () => []),
      skills: vi.fn(async () => []),
    },
    events: {
      onTurnsChanged: vi.fn((): (() => void) => unsubscribe),
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

function group(id: string): HTMLElement {
  return screen.getByTestId(`agent-group-${id}`);
}

/** The collapsed-content freeze contract (refine-chat-surface-identity,
 * ui-conventions): a collapsed group's rows stay mounted so the
 * grid-template-rows 1fr→0fr close animates with real content height, but
 * the content wrapper carries aria-hidden + inert — neither announced nor
 * keyboard-focusable (display:none would kill the transition). */
function expectCollapsedRows(id: string): void {
  const wrap = screen.getByTestId(`group-sessions-${id}`);

  expect(wrap.getAttribute("aria-hidden")).toBe("true");
  expect(wrap.hasAttribute("inert")).toBe(true);
}

/** Activates a group row (the SideRow button — the wrapper's first button,
 * ahead of the per-agent "+"): expansion without a context move. */
function expandGroup(id: string): HTMLElement {
  const button = group(id).querySelector("button");

  // SAFETY: the group row renders its SideRow button by contract.
  fireEvent.click(button!);

  return button!;
}

/** SideRow's active fill: a 15% accent tint over the surface. */
const ACTIVE_TINT = "bg-[color-mix(in_oklab,var(--accent)_15%,transparent)]";

function seedStore(): void {
  useChatStore.setState({
    agents,
    activeAgentID: "a1",
    sessions,
    activeSessionID: "sB",
    busy: false,
    runningSessions: new Set<string>(),
  });
}

// The store is module-level, so the new-session spy one test installs must
// not leak into the next: the pristine action is captured once and restored
// before every test.
const realNewSession = useChatStore.getState().newSession;

beforeEach(() => {
  vi.clearAllMocks();
  useChatStore.setState({
    agents: [],
    activeAgentID: null,
    sessions: [],
    activeSessionID: null,
    busy: false,
    runningSessions: new Set<string>(),
    newSession: realNewSession,
  });
  // Two agents with sessions and one without (design D5: session-less
  // agents still render a collapsed row); a1 is active with sB open.
  sessions = [
    makeSession("sB", "a1", "Weekly notes", 2),
    makeSession("sC", "a2", "Mule briefing", 10),
    makeSession("sD", "a3", "Iris review", 20),
    makeSession("sA", "a1", "Retro planning", 50),
  ];
  seedStore();
});

describe("SessionRail", () => {
  it("renders the rail header, search pill, and one row per agent", async () => {
    await renderIsland();
    expect(screen.getByTestId("chat-session-rail")).toBeTruthy();
    expect(screen.getByText("Sessions")).toBeTruthy();
    expect(screen.getByTestId("session-search")).toBeTruthy();
    expect(group("a1")).toBeTruthy();
    expect(group("a2")).toBeTruthy();
    // The flat header "+" is gone (task 4.2): each group row carries the
    // per-agent control instead.
    expect(screen.queryByTestId("btn-new-session")).toBeNull();
    expect(screen.getByTestId("group-new-session-a1")).toBeTruthy();
    expect(screen.getByTestId("group-new-session-a2")).toBeTruthy();
  });

  it("keeps the per-agent + always visible in the group row's corner", async () => {
    await renderIsland();
    const plus = screen.getByTestId("group-new-session-a2");
    // No hover gating: the primary action is discoverable at rest.
    expect(plus.className).not.toContain("opacity-0");
    expect(plus.className).not.toContain("group-hover:opacity-100");
    // The row reserves the corner for it with trailing padding, and the
    // dot/count/chevron metadata sits beside it without a cross-fade.
    expect(group("a2").className).toContain("[&>button:first-child]:pe-9");
    expect(
      screen.getByTestId("group-count-a2").parentElement?.className
    ).not.toContain("group-hover:opacity-0");
  });

  it("wraps the delete and group-+ glyphs in 24px hit areas", async () => {
    await renderIsland();
    // Design D6: where the glyph is smaller, the clickable target extends
    // beyond it — 24px hit areas around the 12px/16px glyphs.
    const del = screen.getByTestId("session-delete-sA");
    expect(del.className).toContain("h-6 w-6");
    expect(del.className).not.toContain("h-5");
    expect(del.querySelector("svg")?.getAttribute("class")).toContain("size-3");

    const plus = screen.getByTestId("group-new-session-a1");
    expect(plus.className).toContain("h-6 w-6");
    expect(plus.querySelector("svg")?.getAttribute("class")).toContain(
      "size-4"
    );
  });

  it("starts collapsed except the active agent's group", async () => {
    await renderIsland();
    // The active agent's sessions list under its expanded group…
    expect(row("sA")).toBeTruthy();
    expect(row("sB")).toBeTruthy();
    // …while the other agents' groups render collapsed rows — mounted but
    // frozen (neither focusable nor announced).
    expectCollapsedRows("a2");
    expectCollapsedRows("a3");
    // Rows show the agent's name and session count.
    expect(group("a2").textContent).toContain("Mule");
    expect(screen.getByTestId("group-count-a2").textContent).toBe("1");
    expect(screen.getByTestId("group-count-a3").textContent).toBe("1");
    expect(screen.getByTestId("group-count-a1").textContent).toBe("2");
    // Group order (design D5): the active agent first, then agents with
    // sessions by their most recent activity (a2 at 10m beats a3 at 20m).
    expect(
      group("a1").compareDocumentPosition(group("a2")) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
    expect(
      group("a2").compareDocumentPosition(group("a3")) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
  });

  it("renders a session-less agent as a normal collapsed row after the rest", async () => {
    useChatStore.setState({
      sessions: sessions.filter((s: Session): boolean => s.AgentID !== "a3"),
    });
    await renderIsland();
    expect(group("a3")).toBeTruthy();
    expect(screen.getByTestId("group-count-a3").textContent).toBe("0");
    expect(screen.getByTestId("group-new-session-a3")).toBeTruthy();
    // Session-less agents sort last (design D5).
    expect(
      group("a2").compareDocumentPosition(group("a3")) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
  });

  it("creates a session for a NON-active agent from its collapsed row's +", async () => {
    const newSession = vi.fn(async (): Promise<void> => undefined);

    useChatStore.setState({ newSession });
    await renderIsland();

    await act(async () => {
      fireEvent.click(screen.getByTestId("group-new-session-a2"));
    });

    // The group's agent id rides the call, active or not (design D6).
    expect(newSession).toHaveBeenCalledWith("a2");
    // The click is the control's alone: no context move, no expansion.
    expect(useChatStore.getState().activeAgentID).toBe("a1");
    expect(useChatStore.getState().activeSessionID).toBe("sB");
    // a2's group stays collapsed — its rows frozen, not listed.
    expectCollapsedRows("a2");
  });

  it("creates a session for the active agent from its expanded row's +", async () => {
    const fresh = makeSession("sNEW", "a1", "", 0);

    // The backend would list the new row immediately; seeding the mock's
    // backing list keeps the store's open path (selectAgent refetch under
    // the all-sessions model) able to find it.
    mockedClient.chat.createSession.mockImplementation(async () => {
      sessions = [fresh, ...sessions];

      return fresh;
    });

    await renderIsland();
    await act(async () => {
      fireEvent.click(screen.getByTestId("group-new-session-a1"));
    });
    await waitFor(() =>
      expect(mockedClient.chat.createSession).toHaveBeenCalledWith("a1")
    );
    await waitFor(() =>
      expect(useChatStore.getState().activeSessionID).toBe("sNEW")
    );
    await waitFor(() => expect(row("sNEW")).toBeTruthy());
  });

  it("shows the group running dot for a never-opened session of a collapsed agent", async () => {
    // The turn runs in a session that was never opened here: the app-wide
    // running set (backend-fed, design D2) lights the collapsed group row.
    useChatStore.setState({ busy: false, runningSessions: new Set(["sD"]) });
    await renderIsland();
    expect(screen.getByTestId("group-running-a3")).toBeTruthy();
    // Agents without in-flight turns carry no dot.
    expect(screen.queryByTestId("group-running-a1")).toBeNull();
    expect(screen.queryByTestId("group-running-a2")).toBeNull();
  });

  it("expands a collapsed group without touching the conversation", async () => {
    await renderIsland();

    await act(async () => {
      expandGroup("a2");
    });

    // a2's sessions are listed now…
    expect(row("sC")).toBeTruthy();
    // …the previously expanded group collapsed (its rows remain mounted
    // but frozen)…
    expectCollapsedRows("a1");
    // …and nothing else moved (spec: expanding a group changes nothing else).
    expect(useChatStore.getState().activeAgentID).toBe("a1");
    expect(useChatStore.getState().activeSessionID).toBe("sB");
  });

  it("reports the accordion state as aria-expanded on the group header", async () => {
    await renderIsland();

    // The active agent's group starts open, the rest closed (design D4) —
    // semantic state on the header button, not just the chevron (design D6).
    const a1 = group("a1").querySelector("button");
    const a2 = group("a2").querySelector("button");

    // SAFETY: each group row renders its SideRow button by contract.
    expect(a1?.getAttribute("aria-expanded")).toBe("true");
    expect(a2?.getAttribute("aria-expanded")).toBe("false");

    await act(async () => {
      expandGroup("a2");
    });

    expect(a2?.getAttribute("aria-expanded")).toBe("true");
    expect(a1?.getAttribute("aria-expanded")).toBe("false");
  });

  it("binds the group's reveal to the grid-rows accordion transition", async () => {
    await renderIsland();

    // Design D8: the reveal rides grid-template-rows 0fr ↔ 1fr on the
    // shared motion grammar — no raw height animation.
    const open = group("a1").nextElementSibling;
    expect(open?.className).toContain("grid");
    expect(open?.className).toContain("transition-[grid-template-rows]");
    expect(open?.className).toContain("duration-[var(--motion-base)]");
    expect(open?.className).toContain("ease-[var(--ease-standard)]");
    expect(open?.getAttribute("style")?.replace(/\s+/g, "")).toContain(
      "grid-template-rows:1fr"
    );

    // A collapsed group holds the same binding at 0fr.
    const closed = group("a3").nextElementSibling;
    expect(closed?.getAttribute("style")?.replace(/\s+/g, "")).toContain(
      "grid-template-rows:0fr"
    );

    // The header chevron rotates on the same tokens, in step with the
    // reveal: the open group carries no rotation, the collapsed one points
    // right again. It is the only svg in the dot/count/chevron metadata
    // span.
    const chevron = screen
      .getByTestId("group-count-a1")
      .parentElement?.querySelector("svg");

    expect(chevron?.getAttribute("class")).toContain(
      "duration-[var(--motion-base)]"
    );
    expect(chevron?.getAttribute("class")).toContain(
      "ease-[var(--ease-standard)]"
    );
    expect(chevron?.getAttribute("class")).not.toContain("-rotate-90");

    const collapsedChevron = screen
      .getByTestId("group-count-a3")
      .parentElement?.querySelector("svg");

    expect(collapsedChevron?.getAttribute("class")).toContain("-rotate-90");
  });

  it("keeps a collapsed group's rows mounted but frozen until it expands", async () => {
    await renderIsland();

    // The rows remain in the DOM while the group sits in its 0fr track…
    expect(row("sC")).toBeTruthy();
    expectCollapsedRows("a2");

    // …and expanding that group unfreezes exactly it: the track opens to
    // 1fr and the rows become focusable and announced again.
    await act(async () => {
      expandGroup("a2");
    });

    const track = group("a2").nextElementSibling;
    expect(track?.getAttribute("style")?.replace(/\s+/g, "")).toContain(
      "grid-template-rows:1fr"
    );

    const wrap = screen.getByTestId("group-sessions-a2");
    expect(wrap.getAttribute("aria-hidden")).toBeNull();
    expect(wrap.hasAttribute("inert")).toBe(false);
  });

  it("moves the expansion when the context moves to another agent", async () => {
    await renderIsland();

    // A context move (here: the store's own state after a selectAgent —
    // same path a session click or create-and-open rides, design D4).
    await act(async () => {
      useChatStore.setState({ activeAgentID: "a2", activeSessionID: "sC" });
    });

    expect(row("sC")).toBeTruthy();
    expectCollapsedRows("a1");
  });

  it("switches the whole context on a cross-agent session click", async () => {
    await renderIsland();

    await act(async () => {
      expandGroup("a2");
    });
    await act(async () => {
      fireEvent.click(row("sC")!);
    });

    await waitFor(() =>
      expect(useChatStore.getState().activeAgentID).toBe("a2")
    );
    expect(useChatStore.getState().activeSessionID).toBe("sC");
  });

  it("highlights the active session with the SideRow accent tint", async () => {
    await renderIsland();
    expect(row("sB")?.className).toContain(ACTIVE_TINT);

    const inactive = row("sA");
    expect(inactive?.className).not.toContain(ACTIVE_TINT);
  });

  it("marks the active session row with aria-current and leaves the rest unset", async () => {
    await renderIsland();
    // Semantic state beside the tint (design D6): assistive tech reads the
    // current row without relying on the color-mix fill.
    expect(row("sB")?.getAttribute("aria-current")).toBe("true");
    expect(row("sA")?.getAttribute("aria-current")).toBeNull();
  });

  it("renders compact 26px rows with the relative-time meta line", async () => {
    await renderIsland();
    // The critique wave restored the meta line: dozens of indistinguishable
    // title-only rows lost their recency ordering.
    expect(row("sA")?.textContent).toContain("50m");
    expect(row("sB")?.textContent).toContain("2m");
    // The compact grammar shrinks the kit SideRow's 30px button from the
    // row wrapper.
    expect(row("sA")?.parentElement?.className).toContain(
      "[&>button]:h-[26px]"
    );
  });

  it("renders each row's meta line across the relativeTime tiers", async () => {
    // Fixtures hang off one reading of the clock captured at test start —
    // the rail renders relativeTime against the live now, so the reference
    // pins the expectations without fake timers.
    const reference = Date.now();

    const ago = (ms: number): string => new Date(reference - ms).toISOString();

    const month = 30 * 24 * 60 * 60_000;
    const oldest = new Date(reference - month);
    useChatStore.setState({
      sessions: [
        makeSession("tMin", "a1", "Minutes", 0),
        makeSession("tHour", "a1", "Hours", 0),
        makeSession("tDay", "a1", "Days", 0),
        makeSession("tDate", "a1", "Calendar", 0),
      ].map((s: Session, i: number): Session => ({
        ...s,
        LastActivityAt: ago(
          [2 * 60_000, 3 * 60 * 60_000, 2 * 24 * 60 * 60_000, month][i]
        ),
      })),
    });
    await renderIsland();

    expect(screen.getByTestId("session-meta-tMin").textContent).toBe("2m");
    expect(screen.getByTestId("session-meta-tHour").textContent).toBe("3h");
    expect(screen.getByTestId("session-meta-tDay").textContent).toBe("2d");
    // A week or more old: a short calendar date takes over.
    expect(screen.getByTestId("session-meta-tDate").textContent).toBe(
      new Intl.DateTimeFormat(undefined, {
        day: "numeric",
        month: "short",
      }).format(oldest)
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
    useChatStore.setState({
      busy: true,
      runningSessions: new Set(["sB", "sD"]),
    });
    await renderIsland();

    const spinner = row("sB")?.querySelector(
      '[data-testid="session-running-sB"]'
    );

    expect(spinner).toBeTruthy();
    expect(spinner?.className).not.toContain("animate-spin");
    expect(row("sB")?.parentElement?.className).not.toContain(
      "[&_.truncate]:animate-pulse"
    );
    // The group dot renders but its live pulse drops with the setting.
    expect(screen.getByTestId("group-running-a3")).toBeTruthy();
    expect(
      screen.getByTestId("group-running-a3").querySelector(".od-live")
    ).toBeNull();
  });

  it("drops the running indicators when the turn reaches a terminal outcome", async () => {
    useChatStore.setState({
      busy: true,
      runningSessions: new Set(["sB", "sD"]),
    });
    await renderIsland();
    expect(
      row("sB")?.querySelector('[data-testid="session-running-sB"]')
    ).toBeTruthy();
    expect(screen.getByTestId("group-running-a3")).toBeTruthy();

    await act(async () => {
      useChatStore.setState({ runningSessions: new Set<string>() });
    });
    expect(
      row("sB")?.querySelector('[data-testid="session-running-sB"]')
    ).toBeNull();
    expect(screen.queryByTestId("group-running-a3")).toBeNull();
  });

  it("shows the store-empty text when no agent has any session", async () => {
    useChatStore.setState({ sessions: [], activeSessionID: null });
    await renderIsland();
    expect(screen.getByText("No sessions yet")).toBeTruthy();
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

describe("SessionRail search (spec: Session search filter)", () => {
  // SAFETY: the search pill renders an input by contract.
  async function typeSearch(text: string): Promise<HTMLInputElement> {
    // SAFETY: the search pill renders an input by contract.
    const search = screen.getByTestId("session-search") as HTMLInputElement;
    const user = userEvent.setup();

    await user.clear(search);

    if (text) await user.type(search, text);

    return search;
  }

  it("expands exactly the matching groups and lists every match", async () => {
    await renderIsland();
    // "ing" matches a1's "Retro planning" and a2's "Mule briefing" — but
    // not a1's "Weekly notes" nor a3's "Iris review".
    await typeSearch("ing");

    // Both matching groups expand and list only their matches…
    expect(row("sA")).toBeTruthy();
    expect(row("sB")).toBeNull();
    expect(row("sC")).toBeTruthy();
    // …while the group without a match stays a collapsed row (frozen).
    expectCollapsedRows("a3");
    expect(group("a3")).toBeTruthy();
  });

  it("lists matches at natural height with the column scrolling as one", async () => {
    await renderIsland();
    await typeSearch("ing");

    // A search expands every matching group — none pins a private scroll
    // region; the column scrolls as a whole.
    const scroll = screen.getByTestId("group-sessions-a1");
    expect(scroll.className).toContain("shrink-0");
    expect(scroll.className).not.toContain("overflow-y-auto");
  });

  it("lists every match regardless of the load-more state", async () => {
    const batch: Session[] = [];

    for (let i = 0; i < 6; i += 1) {
      batch.push(
        makeSession(`x${i + 1}`, "a1", `Batched plan ${i + 1}`, 60 + i)
      );
    }

    useChatStore.setState({ sessions: [...sessions, ...batch] });
    await renderIsland();

    // Unfiltered, a1's group caps at the four most recent.
    expect(row("x1")).toBeTruthy();
    expect(row("x6")).toBeNull();

    await typeSearch("batched");

    // All six matches are listed in the group (design D4: the cap lifts).
    for (const id of ["x1", "x2", "x3", "x4", "x5", "x6"]) {
      expect(row(id)).toBeTruthy();
    }

    expect(screen.queryByTestId("group-load-more-a1")).toBeNull();
  });

  it("hides the no-match text only while nothing matches anywhere", async () => {
    await renderIsland();

    await typeSearch("zzz");
    expect(screen.getByText("No matching sessions")).toBeTruthy();

    await typeSearch("mule");
    expect(screen.queryByText("No matching sessions")).toBeNull();
  });

  it("restores the collapse grammar on clear without moving the active session", async () => {
    await renderIsland();
    const search = await typeSearch("ing");
    expectCollapsedRows("a3");

    await userEvent.setup().clear(search);

    // The active agent's group is expanded again, the rest collapsed
    // (frozen, still out of the announced tree)…
    expect(row("sB")).toBeTruthy();
    expectCollapsedRows("a2");
    expectCollapsedRows("a3");
    // …with the previously active session still selected.
    expect(useChatStore.getState().activeSessionID).toBe("sB");
    expect(row("sB")?.className).toContain(ACTIVE_TINT);
  });
});

describe("SessionRail load more (design D4)", () => {
  function seed(n: number, activeID: string): void {
    const list: Session[] = [];

    for (let i = 0; i < n; i += 1) {
      // s1 is newest (the store's order).
      list.push(makeSession(`s${i + 1}`, "a1", `S${i + 1}`, i + 1));
    }

    useChatStore.setState({
      sessions: [
        ...list,
        ...sessions.filter((s: Session): boolean => s.AgentID !== "a1"),
      ],
      activeSessionID: activeID,
    });
  }

  it("caps the expanded group at the four most recent sessions", async () => {
    seed(6, "s2");
    await renderIsland();

    for (const id of ["s1", "s2", "s3", "s4"]) {
      expect(row(id)).toBeTruthy();
    }

    expect(row("s5")).toBeNull();
    expect(row("s6")).toBeNull();
    expect(screen.getByTestId("group-load-more-a1")).toBeTruthy();
  });

  it("keeps the active session visible when it is older than the cap", async () => {
    seed(6, "s6");
    await renderIsland();

    // The four most recent plus the active (oldest) one.
    for (const id of ["s1", "s2", "s3", "s4", "s6"]) {
      expect(row(id)).toBeTruthy();
    }

    expect(row("s5")).toBeNull();
  });

  it("appends the next batch of eight per activation and retires on exhaustion", async () => {
    seed(14, "s1");
    await renderIsland();

    await act(async () => {
      fireEvent.click(screen.getByTestId("group-load-more-a1"));
    });

    // 4 + 8: s1–s12 listed, s13–s14 still behind the control.
    for (const id of ["s1", "s12"]) {
      expect(row(id)).toBeTruthy();
    }

    expect(row("s13")).toBeNull();
    expect(screen.getByTestId("group-load-more-a1")).toBeTruthy();

    await act(async () => {
      fireEvent.click(screen.getByTestId("group-load-more-a1"));
    });

    // Everything is listed; the control retires.
    expect(row("s14")).toBeTruthy();
    expect(screen.queryByTestId("group-load-more-a1")).toBeNull();
  });

  it("hides the control while a search lists every match", async () => {
    seed(6, "s2");
    await renderIsland();
    expect(screen.getByTestId("group-load-more-a1")).toBeTruthy();

    // SAFETY: the search pill renders an input by contract.
    const search = screen.getByTestId("session-search") as HTMLInputElement;
    await userEvent.setup().type(search, "S");

    for (const id of ["s1", "s6"]) {
      expect(row(id)).toBeTruthy();
    }

    expect(screen.queryByTestId("group-load-more-a1")).toBeNull();

    // Clearing restores the cap with the counts reset (design D4).
    await userEvent.setup().clear(search);
    expect(row("s5")).toBeNull();
    expect(row("s6")).toBeNull();
  });

  it("tracks each agent's window independently", async () => {
    seed(6, "s1");
    await renderIsland();

    await act(async () => {
      fireEvent.click(screen.getByTestId("group-load-more-a1"));
    });
    expect(row("s5")).toBeTruthy();

    // a2's own single-session group is unaffected by a1's window.
    await act(async () => {
      expandGroup("a2");
    });
    expect(row("sC")).toBeTruthy();
    expectCollapsedRows("a1");

    // Re-expanding a1 keeps its revealed window (design D4: counts survive).
    await act(async () => {
      expandGroup("a1");
    });
    expect(row("s5")).toBeTruthy();
  });

  it("scrolls an agent's long history beneath its pinned group row", async () => {
    seed(14, "s1");
    await renderIsland();
    await act(async () => {
      fireEvent.click(screen.getByTestId("group-load-more-a1"));
    });

    // The open group's rows live in a dedicated scroll region…
    const scroll = screen.getByTestId("group-sessions-a1");
    expect(scroll.className).toContain("overflow-y-auto");
    expect(scroll.className).toContain("flex-1");
    // …holding the session rows and the load-more control but never the
    // agent row: scrolling the history cannot scroll the agent row away.
    expect(
      scroll.querySelector('[data-od-id="chat-agent-group-a1"]')
    ).toBeNull();
    expect(scroll.querySelector('[data-od-id="chat-session-s1"]')).toBeTruthy();
    expect(
      scroll.querySelector('[data-testid="group-load-more-a1"]')
    ).toBeTruthy();
    // Pinned row and scroll region live in the same group block — the
    // accordion reveal grid (design D8) sits between them.
    expect(scroll.parentElement?.parentElement?.parentElement).toBe(
      group("a1").parentElement
    );
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
    // And so are the other agents' groups.
    expect(screen.getByTestId("group-count-a1").textContent).toBe("1");
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
  // stubs it against a Map instead (same fake as RightBar.test.tsx).
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

  it("animates width on the motion grammar but drops the transition while dragging", async () => {
    stubLocalStorage();
    const { unmount } = await renderUI(<SessionRail />);
    const rail = screen.getByTestId("chat-session-rail");

    // Design D8: the column's inline width animates on the shared grammar…
    expect(rail.className).toContain("transition-[width]");
    expect(rail.className).toContain("duration-[var(--motion-base)]");
    expect(rail.className).toContain("ease-[var(--ease-standard)]");

    await act(async () => {
      fireEvent.pointerDown(screen.getByTestId("session-rail-resize"));
    });
    // …but a live drag must track the pointer 1:1, so the transition drops.
    expect(rail.className).toContain("transition-none");
    expect(rail.className).not.toContain("transition-[width]");

    await act(async () => {
      window.dispatchEvent(new MouseEvent("pointerup"));
    });
    // Released — the transition re-arms.
    expect(rail.className).not.toContain("transition-none");
    expect(rail.className).toContain("transition-[width]");

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
