/**
 * ChatView integration tests (tasks 9.1–9.4): live streaming appends to the
 * in-flight assistant entry, the queued-message stack delivers pending sends
 * one at a time in order when the current turn completes, the context ring
 * refreshes on turn_completed, the nav store's chat target is consumed, and
 * switching sessions reloads that session's history. The lib client is mocked
 * wholesale; the live handler is captured from chat.subscribe.
 */
import { mockedSurface } from "./mockedSurface";
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import {
  Kind,
  type Event as SessionEvent,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type {
  Agent,
  Session,
  Skill,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../../../lib/client";
import { useNavStore } from "../../../stores/nav";
import ChatView from "../index";
import { SessionRail } from "../components/SessionRail";
import {
  TRANSCRIPT_PAGE_SIZE,
  useChatStore,
  type QueuedMessage,
} from "../store";
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
];

const sessionA: Session = {
  ID: "sA",
  AgentID: "a1",
  Title: "Older",
  Model: "",
  Effort: "",
  CreatedAt: "2026-01-01T00:00:00Z",
  LastActivityAt: "2026-01-01T00:00:00Z",
};

const sessionB: Session = {
  ID: "sB",
  AgentID: "a1",
  Title: "Newest",
  Model: "",
  Effort: "",
  CreatedAt: "2026-01-02T00:00:00Z",
  LastActivityAt: "2026-01-02T00:00:00Z",
};

let sessions: Session[];

interface TranscriptMap {
  [sessionID: string]: SessionEvent[];
}

let transcripts: TranscriptMap;

let usageResult = { used: 10000, limit: 200000 };

let liveHandler: ((e: SessionEvent) => void) | null = null;

function ev(
  kind: SessionEvent["kind"],
  extra: Partial<SessionEvent> = {}
): SessionEvent {
  return { kind, occurred_at: "2026-01-01T00:00:00Z", ...extra };
}

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

// Turn ids minted by the send mock; reset per test so emitted turn_completed
// events match the turn each test actually dispatched.
let turnSeq = 0;

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
    turnState: MockFn;
    contextUsage: MockFn;
  };
  settings: {
    providers: MockFn;
    skills: MockFn;
  };
  events: {
    onSchedulesChanged: MockFn;
  };
};

function makeClient(): MockedClient {
  const c = {
    agents: {
      list: vi.fn(async () => agents),
      get: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
    },
    chat: {
      sessions: vi.fn(async (agentID: string) =>
        agentID === "a1" ? sessions : []
      ),
      createSession: vi.fn(),
      send: vi.fn(async (_sessionID: string, _message: string) => {
        turnSeq += 1;

        return `turn-${turnSeq}`;
      }),
      subscribe: vi.fn(
        async (_sessionID: string, handler: (e: SessionEvent) => void) => {
          liveHandler = handler;

          return (): void => {
            if (liveHandler === handler) liveHandler = null;
          };
        }
      ),
      transcript: vi.fn(
        async (sessionID: string) => transcripts[sessionID] ?? []
      ),
      transcriptPage: vi.fn(
        async (sessionID: string) => transcripts[sessionID] ?? []
      ),
      unsubscribe: vi.fn(async () => undefined),
      turnState: vi.fn(async () => ({ busy: false, turn_id: "" })),
      contextUsage: vi.fn(async () => usageResult),
    },
    settings: {
      providers: vi.fn(async () => [
        { ID: "prov", Name: "Prov Co", Enabled: true },
      ]),
      skills: vi.fn(async () => [
        { Name: "search", Description: "Web search", Enabled: true },
        { Name: "hidden", Description: "", Enabled: false },
      ]),
    },
    // The view subscribes to schedules:changed on mount (design D10); the
    // suite never fires it.
    events: {
      onSchedulesChanged: vi.fn(
        (_handler: () => void) => (): void => undefined
      ),
    },
  };

  // SAFETY: the object literal above satisfies the Client surface with its
  // vi.fn members matching the MockedClient contract.
  // The literal mocks only the surface each suite exercises; the
  // Client-typed intersection defeats the structural check, so the
  // conversion funnels through the shared mockedSurface bridge.
  return mockedSurface<MockedClient, object>(c);
}

import { client } from "../../../lib/client";

// Typed alias exposing the vi.fn mocks behind the Client-shaped export.
// SAFETY: the client module is mocked with makeClient(), so its runtime shape
// is exactly this mock object.
const mockedClient = client as MockedClient;

async function renderChat(): Promise<void> {
  await renderUI(<ChatView />);
}

// The session layer now renders as the shell's Sessions island beside the
// chat view (redesign-shell-layout); cases that drive session switching or
// new-session through it mount both, like App does.
async function renderChatWithRail(): Promise<void> {
  await renderUI(
    <>
      <SessionRail />
      <ChatView />
    </>
  );
}

function emit(e: SessionEvent): Promise<void> {
  return act(async () => {
    liveHandler?.(e);
  });
}

function resetStore(): void {
  useChatStore.setState({
    agents: [],
    providers: [],
    skills: [],
    activeAgentID: null,
    sessions: [],
    activeSessionID: null,
    entries: [],
    usage: null,
    loadingSession: false,
    error: null,
    busy: false,
    dispatching: false,
    activeTurnID: null,
    finishedTurnIDs: new Set<string>(),
    queue: [],
    sidebarCollapsed: false,
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  resetStore();
  turnSeq = 0;
  useNavStore.setState({ menu: "chat", chatTarget: null });
  sessions = [sessionA, sessionB];
  transcripts = {
    sA: [ev(Kind.KindUserMessage, { text: "hello A", id: "evt-a1" })],
    sB: [ev(Kind.KindUserMessage, { text: "hello B", id: "evt-b1" })],
  };
  usageResult = { used: 10000, limit: 200000 };
  liveHandler = null;
});

describe("ChatView", () => {
  it("streams text deltas into the transcript while the turn runs", async () => {
    await renderChat();
    const composer = await findByComposer();
    const user = userEvent.setup();

    await user.type(composer, "first");
    await user.keyboard("{Enter}");
    await waitFor(() =>
      expect(mockedClient.chat.send).toHaveBeenCalledWith("sB", "first")
    );
    expect(useChatStore.getState().busy).toBe(true);

    await emit(
      ev(Kind.KindUserMessage, {
        text: "first",
        turn_id: "turn-1",
        id: "evt-1",
      })
    );
    await emit(ev(Kind.KindTextDelta, { text: "Hel", turn_id: "turn-1" }));
    await emit(ev(Kind.KindTextDelta, { text: "lo", turn_id: "turn-1" }));

    const transcript = screen.getByTestId("transcript");
    expect(transcript.textContent).toContain("first");
    expect(transcript.textContent).toContain("Hello");
  });

  it("queues sends while busy and delivers them one at a time in order", async () => {
    await renderChat();
    const composer = await findByComposer();
    const user = userEvent.setup();

    await user.type(composer, "first");
    await user.keyboard("{Enter}");
    await waitFor(() => expect(mockedClient.chat.send).toHaveBeenCalledOnce());

    await user.type(composer, "second");
    await user.keyboard("{Enter}");
    expect(mockedClient.chat.send).toHaveBeenCalledOnce(); // still in flight, second queued

    const queue = screen.getByTestId("message-queue");
    expect(queue.textContent).toContain("second");
    expect(screen.getByTestId("queue-running-row").textContent).toContain(
      "Scout is responding"
    );

    await emit(
      ev(Kind.KindTurnCompleted, { turn_id: "turn-1", id: "evt-tc1" })
    );

    await waitFor(() =>
      expect(mockedClient.chat.send).toHaveBeenCalledTimes(2)
    );
    expect(
      mockedClient.chat.send.mock.calls.map((call: unknown[]) => call[1])
    ).toEqual(["first", "second"]);
    expect(useChatStore.getState().queue).toHaveLength(0);
    expect(screen.queryByTestId("message-queue")).toBeNull();
  });

  it("removes a single queued message without disturbing the rest", async () => {
    await renderChat();
    const composer = await findByComposer();
    const user = userEvent.setup();

    await user.type(composer, "first");
    await user.keyboard("{Enter}");
    await waitFor(() => expect(mockedClient.chat.send).toHaveBeenCalledOnce());
    await user.type(composer, "second");
    await user.keyboard("{Enter}");
    await user.type(composer, "third");
    await user.keyboard("{Enter}");
    expect(useChatStore.getState().queue).toHaveLength(2);

    const removeButtons = screen.getAllByTestId("queue-remove");
    await act(async () => {
      fireEvent.click(removeButtons[0]); // removes "second"
    });
    expect(
      useChatStore.getState().queue.map((q: QueuedMessage) => q.text)
    ).toEqual(["third"]);

    await emit(
      ev(Kind.KindTurnCompleted, { turn_id: "turn-1", id: "evt-tc1" })
    );
    await waitFor(() =>
      expect(mockedClient.chat.send).toHaveBeenCalledTimes(2)
    );
    expect(
      mockedClient.chat.send.mock.calls.map((call: unknown[]) => call[1])
    ).toEqual(["first", "third"]);
  });

  it("refreshes the context ring when a turn completes", async () => {
    await renderChat();
    // The ring lives on the composer's rail now (moved from the sidebar);
    // its hover title carries the used/limit pair.
    expect(
      await screen
        .findByTestId("context-ring")
        .then((el: HTMLElement) => el.getAttribute("title"))
    ).toBe("10k / 200k tokens");
    expect(screen.getByTestId("context-ring-pct").textContent).toBe("5%");

    usageResult = { used: 180000, limit: 200000 };
    await emit(
      ev(Kind.KindTurnCompleted, { turn_id: "turn-1", id: "evt-tc1" })
    );
    await waitFor(() =>
      expect(mockedClient.chat.contextUsage).toHaveBeenCalledTimes(2)
    );
    expect(screen.getByTestId("context-ring-pct").textContent).toBe("90%");
    expect(
      screen.getByTestId("context-ring-fill").getAttribute("class")
    ).toContain("stroke-danger");
  });

  it("resolves the provider name and model in the sidebar, loading enabled skills only", async () => {
    await renderChat();
    expect(
      await screen
        .findByTestId("sidebar-agent-name")
        .then((el: HTMLElement) => el.textContent)
    ).toBe("Scout");
    expect(screen.getByTestId("sidebar-model").textContent).toBe(
      "Prov Co/gpt-test"
    );
    await waitFor(() =>
      expect(useChatStore.getState().skills.map((s: Skill) => s.Name)).toEqual([
        "search",
      ])
    );
  });

  it("collapsing the sidebar hides the island entirely and keeps the turn state intact", async () => {
    await renderChat();
    await screen.findByTestId("chat-sidebar");
    // The toggle lives in the chat header now (the sidebar has no chrome of
    // its own to host it).
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-toggle-sidebar"));
    });
    // Minimized means GONE — not a collapsed sliver.
    expect(screen.queryByTestId("chat-sidebar")).toBeNull();
    // The live handler is still attached; a turn still lands in the transcript.
    await emit(
      ev(Kind.KindUserMessage, { text: "still live", id: "evt-live" })
    );
    expect(screen.getByTestId("transcript").textContent).toContain(
      "still live"
    );
  });

  it("opens the chat target from the agents menu and clears it", async () => {
    useNavStore.getState().openChat("a1", "sA");
    await renderChat();
    await waitFor(() => expect(useNavStore.getState().chatTarget).toBeNull());
    expect(useChatStore.getState().activeAgentID).toBe("a1");
    expect(useChatStore.getState().activeSessionID).toBe("sA");
    expect(screen.getByTestId("transcript").textContent).toContain("hello A");
  });

  it("switching sessions reloads that session's history", async () => {
    await renderChatWithRail();
    await screen.findByTestId("transcript");
    // Latest session (sB) auto-selected.
    expect(useChatStore.getState().activeSessionID).toBe("sB");
    expect(screen.getByTestId("transcript").textContent).toContain("hello B");

    await act(async () => {
      const railRow = screen
        .getByTestId("chat-session-rail")
        .querySelector('[data-od-id="chat-session-sA"]');

      // SAFETY: the rail renders a SideRow with this od-id seam for every
      // seeded session, so the sA row exists when the click fires.
      fireEvent.click(railRow as HTMLElement);
    });
    await waitFor(() =>
      // The transcript loads as the newest page (task 6.1, design D6).
      expect(mockedClient.chat.transcriptPage).toHaveBeenCalledWith(
        "sA",
        0,
        TRANSCRIPT_PAGE_SIZE
      )
    );
    expect(useChatStore.getState().activeSessionID).toBe("sA");
    expect(screen.getByTestId("transcript").textContent).toContain("hello A");
  });

  it("creates a new session from the rail and opens it empty", async () => {
    const fresh: Session = {
      ID: "sNEW",
      AgentID: "a1",
      Title: "",
      Model: "",
      Effort: "",
      CreatedAt: "2026-01-03T00:00:00Z",
      LastActivityAt: "2026-01-03T00:00:00Z",
    };

    mockedClient.chat.createSession.mockImplementation(async () => fresh);
    await renderChatWithRail();
    await screen.findByTestId("btn-new-session");
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-new-session"));
    });
    await waitFor(() =>
      expect(mockedClient.chat.createSession).toHaveBeenCalledWith("a1")
    );
    expect(useChatStore.getState().activeSessionID).toBe("sNEW");
    // The new session appears in the agent's session list...
    expect(
      screen
        .getByTestId("chat-session-rail")
        .querySelector('[data-od-id="chat-session-sNEW"]')
    ).toBeTruthy();
    // ...and the transcript opens empty for it.
    expect(screen.getByTestId("transcript").textContent).toContain(
      "No messages yet"
    );
  });

  it("shows the empty state when no agents exist", async () => {
    agents.length = 0;
    sessions = [];

    try {
      await renderChat();
      expect(screen.getByText("No agents yet")).toBeTruthy();
    } finally {
      agents.push({
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
      });
    }
  });
});

async function findByComposer(): Promise<HTMLTextAreaElement> {
  return waitFor(() => {
    // SAFETY: the chat composer renders as a textarea by contract.
    const el = screen.getByTestId("chat-composer") as HTMLTextAreaElement;
    expect(el).toBeTruthy();

    return el;
  });
}
