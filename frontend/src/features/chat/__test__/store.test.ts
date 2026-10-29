/**
 * Chat store tests (tasks 2.1, 3.3): the session-override surface —
 * hydration from the session row, persist-on-pick through
 * SetSessionOverrides (with the D2 effort-compat drop and the D9 sticky
 * default), seed-on-create for new sessions — and the cancellation terminal:
 * a turn_cancelled event finalizes streaming entries keeping their partial
 * text, returns the store to idle, and drains the queue.
 */
import { act } from "react";
import { mockedSurface } from "./mockedSurface";
import { waitFor } from "@testing-library/dom";
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
import type { ModelCapability } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { TurnsChangedEvent } from "../../../lib/events";
import type { Client } from "../../../lib/client";
import { connectRunningSessions, useChatStore } from "../store";
import type { Entry, PanelTab } from "../types";

const agents: Agent[] = [
  {
    ID: "a1",
    Name: "Scout",
    ProviderID: "prov",
    Model: "gpt-agent-default",
    Effort: "medium",
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
    Name: "Runner",
    ProviderID: "prov",
    Model: "claude-default",
    Effort: "",
    Slug: "runner",
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

const capabilities: ModelCapability[] = [
  {
    ID: "m-plain",
    ContextWindow: 8000,
    Efforts: [],
    ImageInput: false,
    PDFInput: false,
    VideoInput: false,
  },
  {
    ID: "m-reasoning",
    ContextWindow: 200000,
    Efforts: ["low", "high"],
    ImageInput: true,
    PDFInput: false,
    VideoInput: false,
  },
];

function row(id: string, model: string = "", effort: string = ""): Session {
  return {
    ID: id,
    AgentID: "a1",
    Title: "",
    Model: model,
    Effort: effort,
    CreatedAt: "2026-01-01T00:00:00Z",
    LastActivityAt: "2026-01-01T00:00:00Z",
  };
}

let sessions: Session[];

let liveHandler: ((e: SessionEvent) => void) | null = null;

const liveHandlers = new Map<string, (e: SessionEvent) => void>();

/** The turns:changed handler the client mock captured from the store's
 * connectRunningSessions wiring (add-grouped-session-rail D2). */
let turnsHandler: ((event: TurnsChangedEvent) => void) | null = null;

let turnSeq = 0;

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  agents: { list: MockFn };
  chat: {
    sessions: MockFn;
    allSessions: MockFn;
    runningSessions: MockFn;
    createSession: MockFn;
    send: MockFn;
    subscribe: MockFn;
    unsubscribe: MockFn;
    turnState: MockFn;
    transcript: MockFn;
    transcriptPage: MockFn;
    contextUsage: MockFn;
    setSessionOverrides: MockFn;
    cancel: MockFn;
    deleteSession: MockFn;
  };
  settings: {
    providers: MockFn;
    skills: MockFn;
    providerModels: MockFn;
  };
  events: {
    onTurnsChanged: MockFn;
  };
};

function makeClient(): MockedClient {
  const c = {
    agents: { list: vi.fn(async () => agents) },
    chat: {
      sessions: vi.fn(async (agentID: string) =>
        agentID === "a1" ? sessions : []
      ),
      allSessions: vi.fn(async (): Promise<Session[]> => sessions),
      runningSessions: vi.fn(async (): Promise<string[]> => []),
      createSession: vi.fn(),
      send: vi.fn(async () => {
        turnSeq += 1;

        return `turn-${turnSeq}`;
      }),
      subscribe: vi.fn(
        async (id: string, handler: (e: SessionEvent) => void) => {
          liveHandler = handler;
          liveHandlers.set(id, handler);

          return (): void => {
            if (liveHandler === handler) liveHandler = null;
            liveHandlers.delete(id);
          };
        }
      ),
      unsubscribe: vi.fn(async () => undefined),
      turnState: vi.fn(async () => ({ busy: false, turn_id: "" })),
      transcript: vi.fn(async () => []),
      transcriptPage: vi.fn(
        async (
          _sessionID: string,
          _beforeSeq: number,
          _limit: number
        ): Promise<SessionEvent[]> => []
      ),
      contextUsage: vi.fn(async () => ({ used: 0, limit: 200000 })),
      setSessionOverrides: vi.fn(async () => undefined),
      cancel: vi.fn(async () => undefined),
      deleteSession: vi.fn(async () => undefined),
    },
    settings: {
      providers: vi.fn(async () => []),
      skills: vi.fn(async (): Promise<Skill[]> => []),
      providerModels: vi.fn(async () => capabilities),
    },
    // The store's connectRunningSessions wiring subscribes to turns:changed
    // (add-grouped-session-rail D2); the mock captures the handler so tests
    // can fire it.
    events: {
      onTurnsChanged: vi.fn((handler: (event: TurnsChangedEvent) => void) => {
        turnsHandler = handler;

        return (): void => {
          if (turnsHandler === handler) turnsHandler = null;
        };
      }),
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

function emit(...events: SessionEvent[]): Promise<void> {
  for (const e of events) liveHandler?.(e);

  return Promise.resolve();
}

function resetStore(): void {
  useChatStore.setState({
    agents,
    providers: [],
    skills: [],
    activeAgentID: "a1",
    sessions,
    activeSessionID: null,
    models: [],
    modelOverride: "",
    effortOverride: "",
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
    panelTabs: [],
    activePanelTabID: null,
    runningSessions: new Set<string>(),
    lastTurnOutcome: null,
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  sessions = [row("s1", "m-reasoning", "high"), row("s2")];
  liveHandler = null;
  liveHandlers.clear();
  turnsHandler = null;
  turnSeq = 0;
  resetStore();
});

describe("chat store overrides (task 2.1)", () => {
  it("hydrates the model/effort overrides from the session row on select", async () => {
    await useChatStore.getState().selectSession("s1");
    expect(useChatStore.getState().modelOverride).toBe("m-reasoning");
    expect(useChatStore.getState().effortOverride).toBe("high");

    // An inherit row hydrates as empty on both.
    await useChatStore.getState().selectSession("s2");
    expect(useChatStore.getState().modelOverride).toBe("");
    expect(useChatStore.getState().effortOverride).toBe("");
  });

  it("persists a model pick through SetSessionOverrides and mirrors the row", async () => {
    await useChatStore.getState().selectSession("s1");
    await useChatStore.getState().setModelOverride("m-plain");

    expect(mockedClient.chat.setSessionOverrides).toHaveBeenCalledWith(
      "s1",
      "m-plain",
      ""
    );
    expect(useChatStore.getState().modelOverride).toBe("m-plain");
    expect(
      useChatStore.getState().sessions.find((s: Session) => s.ID === "s1")
        ?.Model
    ).toBe("m-plain");
  });

  it("drops an effort override the picked model cannot express (design D2)", async () => {
    await useChatStore.getState().selectSession("s1");
    await useChatStore.getState().setModelOverride("m-plain");

    // m-plain has no effort set; the inherited "high" from the old model
    // cannot survive the switch.
    expect(mockedClient.chat.setSessionOverrides).toHaveBeenCalledWith(
      "s1",
      "m-plain",
      ""
    );
    expect(useChatStore.getState().effortOverride).toBe("");
  });

  it("persists an effort pick alongside the current model override", async () => {
    await useChatStore.getState().selectSession("s1");
    await useChatStore.getState().setEffortOverride("low");

    expect(mockedClient.chat.setSessionOverrides).toHaveBeenCalledWith(
      "s1",
      "m-reasoning",
      "low"
    );
    expect(useChatStore.getState().effortOverride).toBe("low");
    expect(
      useChatStore.getState().sessions.find((s: Session) => s.ID === "s1")
        ?.Effort
    ).toBe("low");
  });

  it("records the sticky per-agent default on pick and clears it on inherit", async () => {
    await useChatStore.getState().selectSession("s1");
    await useChatStore.getState().setModelOverride("m-reasoning");
    await useChatStore.getState().setEffortOverride("low");

    expect(window.localStorage.getItem("buddies.chatDefaultModel.a1")).toBe(
      "m-reasoning"
    );
    expect(window.localStorage.getItem("buddies.chatDefaultEffort.a1")).toBe(
      "low"
    );

    await useChatStore.getState().setModelOverride("");
    expect(window.localStorage.getItem("buddies.chatDefaultModel.a1")).toBe("");
  });

  it("a failed persist leaves the override state untouched and surfaces the error", async () => {
    mockedClient.chat.setSessionOverrides.mockRejectedValueOnce(
      new Error("store busy")
    );
    await useChatStore.getState().selectSession("s1");
    await useChatStore.getState().setModelOverride("m-plain");

    expect(useChatStore.getState().error).toContain("store busy");
    expect(useChatStore.getState().modelOverride).toBe("m-reasoning");
  });

  it("a pick with no open session only records the sticky default", async () => {
    useChatStore.setState({ activeSessionID: null, sessions: [] });
    await useChatStore.getState().setModelOverride("m-reasoning");

    expect(mockedClient.chat.setSessionOverrides).not.toHaveBeenCalled();
    expect(useChatStore.getState().modelOverride).toBe("m-reasoning");
    expect(window.localStorage.getItem("buddies.chatDefaultModel.a1")).toBe(
      "m-reasoning"
    );
  });

  it("seeds a new session from the sticky per-agent default (design D9)", async () => {
    window.localStorage.setItem("buddies.chatDefaultModel.a1", "m-reasoning");
    window.localStorage.setItem("buddies.chatDefaultEffort.a1", "low");
    mockedClient.chat.createSession.mockResolvedValue(row("s-new"));
    // The opened row comes back through selectAgent's all-agents refetch
    // (design D6); the backend row carries the overrides that were just
    // persisted onto it.
    mockedClient.chat.allSessions.mockResolvedValueOnce([
      { ...row("s-new"), Model: "m-reasoning", Effort: "low" },
      ...sessions,
    ]);

    await useChatStore.getState().newSession();

    expect(mockedClient.chat.setSessionOverrides).toHaveBeenCalledWith(
      "s-new",
      "m-reasoning",
      "low"
    );

    const created = useChatStore
      .getState()
      .sessions.find((s: Session) => s.ID === "s-new");

    expect(created?.Model).toBe("m-reasoning");
    expect(created?.Effort).toBe("low");
    // The seeded session opens with its overrides hydrated.
    expect(useChatStore.getState().modelOverride).toBe("m-reasoning");
    expect(useChatStore.getState().effortOverride).toBe("low");
  });

  it("creates an unseeded session when no sticky default exists", async () => {
    mockedClient.chat.createSession.mockResolvedValue(row("s-new"));
    mockedClient.chat.allSessions.mockResolvedValueOnce([
      row("s-new"),
      ...sessions,
    ]);

    await useChatStore.getState().newSession();

    expect(mockedClient.chat.setSessionOverrides).not.toHaveBeenCalled();
    expect(
      useChatStore.getState().sessions.find((s: Session) => s.ID === "s-new")
        ?.Model
    ).toBe("");
  });

  it("creates and opens a session for a non-active agent through selectAgent (design D6)", async () => {
    const created = {
      ...row("s-a2"),
      AgentID: "a2",
      LastActivityAt: "2026-02-01T00:00:00Z",
    };

    mockedClient.chat.createSession.mockResolvedValue(created);
    mockedClient.chat.allSessions.mockResolvedValueOnce([created, ...sessions]);

    await useChatStore.getState().newSession("a2");

    expect(mockedClient.chat.createSession).toHaveBeenCalledWith("a2");
    // The context swaps: the created session's agent becomes active and the
    // session opens through the same selectAgent path as a rail click.
    expect(useChatStore.getState().activeAgentID).toBe("a2");
    expect(useChatStore.getState().activeSessionID).toBe("s-a2");
  });

  it("selectAgent populates the all-agents list (add-grouped-session-rail design D1)", async () => {
    const a2row = {
      ...row("s-a2"),
      AgentID: "a2",
      LastActivityAt: "2026-02-01T00:00:00Z",
    };

    mockedClient.chat.allSessions.mockResolvedValueOnce([row("s1"), a2row]);

    await useChatStore.getState().selectAgent("a1");

    expect(mockedClient.chat.allSessions).toHaveBeenCalled();
    // Rows from two agents, most recent activity first...
    expect(
      useChatStore.getState().sessions.map((s: Session): string => s.ID)
    ).toEqual(["s-a2", "s1"]);
    // ...and the opened session belongs to the selected agent, not to the
    // globally newest row.
    expect(useChatStore.getState().activeSessionID).toBe("s1");
  });

  it("loads the active agent's provider model capabilities on selectAgent", async () => {
    await useChatStore.getState().selectAgent("a1");

    expect(mockedClient.settings.providerModels).toHaveBeenCalledWith("prov");
    await waitFor(() =>
      expect(useChatStore.getState().models).toEqual(capabilities)
    );
  });
});

describe("chat store cancellation (task 3.3)", () => {
  it("stop calls Cancel for the open session; a failure surfaces", async () => {
    await useChatStore.getState().selectSession("s1");
    useChatStore.getState().stop();
    await waitFor(() =>
      expect(mockedClient.chat.cancel).toHaveBeenCalledWith("s1")
    );

    mockedClient.chat.cancel.mockRejectedValueOnce(new Error("gone"));
    useChatStore.getState().stop();
    await waitFor(() =>
      expect(useChatStore.getState().error).toContain("gone")
    );
  });

  it("a turn_cancelled event keeps the partial text, returns to idle, and drains the queue", async () => {
    await useChatStore.getState().selectSession("s1");
    await useChatStore.getState().send("first");
    expect(useChatStore.getState().activeTurnID).toBe("turn-1");

    await emit(
      ev(Kind.KindTextDelta, { text: "Hel", turn_id: "turn-1" }),
      ev(Kind.KindTextDelta, { text: "lo", turn_id: "turn-1" })
    );
    // Second send joins the queue while the turn is in flight.
    await useChatStore.getState().send("second");
    expect(useChatStore.getState().queue).toHaveLength(1);

    await emit(
      ev(Kind.KindTurnCancelled, {
        turn_id: "turn-1",
        id: "evt-cancel",
        text: "Hello (partial)",
      })
    );

    await waitFor(() =>
      expect(mockedClient.chat.send).toHaveBeenCalledTimes(2)
    );
    await waitFor(() =>
      expect(useChatStore.getState().activeTurnID).toBe("turn-2")
    );
    expect(
      mockedClient.chat.send.mock.calls.map((call: unknown[]) => call[1])
    ).toEqual(["first", "second"]);

    const entries = useChatStore.getState().entries;
    const assistant = entries.find((en: Entry) => en.kind === "assistant");

    expect(assistant).toMatchObject({ text: "Hello", streaming: false });
    expect(useChatStore.getState().queue).toHaveLength(0);
  });

  it("stop mid-stream keeps the partial text and returns the control to idle", async () => {
    await useChatStore.getState().selectSession("s1");
    await useChatStore.getState().send("first");

    await emit(
      ev(Kind.KindTextDelta, { text: "partial re", turn_id: "turn-1" })
    );
    expect(useChatStore.getState().busy).toBe(true);

    useChatStore.getState().stop();
    await waitFor(() =>
      expect(mockedClient.chat.cancel).toHaveBeenCalledWith("s1")
    );

    await emit(
      ev(Kind.KindTurnCancelled, {
        turn_id: "turn-1",
        id: "evt-cancel",
        text: "partial reply",
      })
    );

    await waitFor(() => expect(useChatStore.getState().busy).toBe(false));
    expect(useChatStore.getState().activeTurnID).toBeNull();

    const assistant = useChatStore
      .getState()
      .entries.find((en: Entry) => en.kind === "assistant");

    expect(assistant).toMatchObject({ text: "partial re", streaming: false });
  });
});

describe("chat store session delete (task 6.5)", () => {
  it("delete removes the session and its transcript, then moves to the next", async () => {
    await useChatStore.getState().selectSession("s1");
    await useChatStore.getState().deleteSession("s1");

    expect(mockedClient.chat.deleteSession).toHaveBeenCalledWith("s1");
    expect(useChatStore.getState().sessions.map((s: Session) => s.ID)).toEqual([
      "s2",
    ]);
    // The view lands on the agent's remaining session.
    expect(useChatStore.getState().activeSessionID).toBe("s2");
    expect(useChatStore.getState().runningSessions.has("s1")).toBe(false);
  });

  it("deleting the active session keeps the follow-up inside the same agent (add-grouped-session-rail design D1)", async () => {
    // The a2 session is globally newer than a1's remaining one — the old
    // plain-newest pick would cross the agent boundary.
    sessions = [
      { ...row("s-active"), LastActivityAt: "2026-03-01T00:00:00Z" },
      {
        ...row("s-a2"),
        AgentID: "a2",
        LastActivityAt: "2026-02-01T00:00:00Z",
      },
      { ...row("s-a1-old"), LastActivityAt: "2026-01-02T00:00:00Z" },
    ];
    resetStore();
    await useChatStore.getState().selectSession("s-active");

    await useChatStore.getState().deleteSession("s-active");

    expect(useChatStore.getState().activeSessionID).toBe("s-a1-old");
    expect(useChatStore.getState().activeAgentID).toBe("a1");
    expect(
      useChatStore.getState().sessions.map((s: Session): string => s.ID)
    ).toEqual(["s-a2", "s-a1-old"]);
  });

  it("deleting the agent's last session clears the view but keeps the agent", async () => {
    sessions = [
      { ...row("s-only"), LastActivityAt: "2026-01-01T00:00:00Z" },
      {
        ...row("s-a2"),
        AgentID: "a2",
        LastActivityAt: "2026-02-01T00:00:00Z",
      },
    ];
    resetStore();
    await useChatStore.getState().selectSession("s-only");

    await useChatStore.getState().deleteSession("s-only");

    // Nothing left for the agent: the "nothing left" clearing branch keeps
    // activeAgentID (design D1).
    expect(useChatStore.getState().activeSessionID).toBeNull();
    expect(useChatStore.getState().activeAgentID).toBe("a1");
  });

  it("a refused delete (turn in flight) surfaces the reason and keeps the session", async () => {
    await useChatStore.getState().selectSession("s1");
    mockedClient.chat.deleteSession.mockRejectedValueOnce(
      new Error("session has a turn in flight")
    );

    await useChatStore.getState().deleteSession("s1");

    expect(useChatStore.getState().error).toContain("turn in flight");
    // Nothing moved: the row and the open session stay as they were.
    expect(useChatStore.getState().sessions.map((s: Session) => s.ID)).toEqual([
      "s1",
      "s2",
    ]);
    expect(useChatStore.getState().activeSessionID).toBe("s1");
  });
});

describe("chat store per-session running state (task 6.5)", () => {
  it("live turn events flag a background session until a terminal retires it", async () => {
    await useChatStore.getState().selectSession("s1");
    // SAFETY: subscribe's mock captured the handler; s1's survives the
    // switch (subscriptions persist across session switches).
    const s1Handler = liveHandler;
    await useChatStore.getState().selectSession("s2");

    expect(useChatStore.getState().runningSessions.has("s1")).toBe(false);

    // A turn starts in the background session (a trigger-started one has no
    // send() behind it — the live events are the only signal).
    s1Handler?.(ev(Kind.KindUserMessage, { turn_id: "t-bg", id: "e-bg1" }));
    s1Handler?.(ev(Kind.KindTextDelta, { text: "hi", turn_id: "t-bg" }));
    expect(useChatStore.getState().runningSessions.has("s1")).toBe(true);

    // Background events never fold into the viewed transcript.
    expect(useChatStore.getState().entries).toHaveLength(0);

    s1Handler?.(ev(Kind.KindTurnCompleted, { turn_id: "t-bg", id: "e-bg2" }));
    expect(useChatStore.getState().runningSessions.has("s1")).toBe(false);
  });

  it("an approval-paused turn keeps the session flagged until its terminal", async () => {
    await useChatStore.getState().selectSession("s1");
    await useChatStore.getState().send("go");
    expect(useChatStore.getState().runningSessions.has("s1")).toBe(true);

    await emit(
      ev(Kind.KindApprovalRequired, {
        turn_id: "turn-1",
        id: "e-appr",
        approval: {
          interrupt_id: "int-1",
          command: "rm -rf /tmp/x",
          checkpoint_id: "adk_turn-1",
        },
      })
    );
    // Approval-paused is still a turn in flight.
    expect(useChatStore.getState().runningSessions.has("s1")).toBe(true);

    await emit(
      ev(Kind.KindApprovalResolved, {
        turn_id: "turn-1",
        id: "e-appr2",
        approval: { interrupt_id: "int-1", outcome: "approved" },
      })
    );
    expect(useChatStore.getState().runningSessions.has("s1")).toBe(true);

    await emit(ev(Kind.KindTurnCompleted, { turn_id: "turn-1", id: "e-done" }));
    expect(useChatStore.getState().runningSessions.has("s1")).toBe(false);
  });
});

describe("running dots backend sources (add-grouped-session-rail design D2)", () => {
  it("init seeds the running set from the backend's runningSessions", async () => {
    mockedClient.chat.runningSessions.mockResolvedValue(["s2", "s9"]);
    useChatStore.setState({
      activeAgentID: null,
      sessions: [],
      runningSessions: new Set<string>(),
    });

    await useChatStore.getState().init();

    await waitFor(() => {
      expect(useChatStore.getState().runningSessions.has("s9")).toBe(true);
    });
    expect(useChatStore.getState().runningSessions.has("s2")).toBe(true);
  });

  it("a failed seed leaves the running set as it was (keep-going)", async () => {
    mockedClient.chat.runningSessions.mockRejectedValueOnce(new Error("boom"));
    useChatStore.setState({
      activeAgentID: null,
      sessions: [],
      runningSessions: new Set<string>(["s-keep"]),
    });

    await useChatStore.getState().init();

    await waitFor(() => {
      expect(mockedClient.chat.runningSessions).toHaveBeenCalled();
    });
    expect(useChatStore.getState().runningSessions.has("s-keep")).toBe(true);
    expect(useChatStore.getState().error).toBeNull();
  });

  it("a turns:changed delivery flips the set through the captured handler", async () => {
    const off = connectRunningSessions();

    expect(mockedClient.events.onTurnsChanged).toHaveBeenCalled();
    expect(turnsHandler).not.toBeNull();

    turnsHandler?.({ sessionId: "s2", busy: true });
    expect(useChatStore.getState().runningSessions.has("s2")).toBe(true);

    turnsHandler?.({ sessionId: "s2", busy: false });
    expect(useChatStore.getState().runningSessions.has("s2")).toBe(false);

    off();
    expect(turnsHandler).toBeNull();
  });
});

function ev(
  kind: SessionEvent["kind"],
  extra: Partial<SessionEvent> = {}
): SessionEvent {
  return { kind, occurred_at: "2026-01-01T00:00:00Z", ...extra };
}

describe("refreshSessions (add-scheduler D10 + add-grouped-session-rail D1)", () => {
  it("refetches every agent's sessions on a background fire", async () => {
    // Distinct activity stamps so the assertion also pins sortSessions' order.
    sessions = [
      row("s1"),
      { ...row("s-new"), LastActivityAt: "2026-02-01T00:00:00Z" },
    ];

    await useChatStore.getState().refreshSessions("a1");

    expect(mockedClient.chat.allSessions).toHaveBeenCalled();
    expect(
      useChatStore.getState().sessions.map((s: Session): string => s.ID)
    ).toEqual(["s-new", "s1"]);
  });

  it("refetches even when the fire names a non-active agent (design D1)", async () => {
    // The argument only labels the fire: a background fire for ANY agent
    // must update that agent's group count in the rail.
    const a2row = {
      ...row("s-a2"),
      AgentID: "a2",
      LastActivityAt: "2026-02-01T00:00:00Z",
    };

    mockedClient.chat.allSessions.mockResolvedValueOnce([...sessions, a2row]);

    await useChatStore.getState().refreshSessions("a2");

    expect(mockedClient.chat.allSessions).toHaveBeenCalled();
    expect(
      useChatStore.getState().sessions.map((s: Session): string => s.ID)
    ).toEqual(["s-a2", "s1", "s2"]);
  });

  it("keeps the current list when the refetch fails", async () => {
    mockedClient.chat.allSessions.mockImplementationOnce(
      async (): Promise<Session[]> => {
        throw new Error("boom");
      }
    );

    await useChatStore.getState().refreshSessions("a1");

    expect(useChatStore.getState().sessions).toEqual(sessions);
    expect(useChatStore.getState().error).toBeNull();
  });

  it("does not write a superseded fetch after the agent switched", async () => {
    let resolveFetch: ((value: Session[]) => void) | null = null;
    mockedClient.chat.allSessions.mockImplementationOnce(
      (): Promise<Session[]> =>
        new Promise<Session[]>((resolve: (value: Session[]) => void) => {
          resolveFetch = resolve;
        })
    );

    const pending = useChatStore.getState().refreshSessions("a1");
    await useChatStore.getState().selectAgent("a2");

    await act(async () => {
      if (resolveFetch !== null) resolveFetch([row("s-stale")]);
    });
    await pending;

    expect(useChatStore.getState().activeAgentID).toBe("a2");
    // The stale write is dropped; the all-agents list the switch itself
    // fetched survives (the default mock lists both a1 rows).
    expect(
      useChatStore.getState().sessions.map((s: Session): string => s.ID)
    ).toEqual(["s1", "s2"]);
  });
});

describe("session subscription teardown and preservation (task 3.2)", () => {
  it("switching away and back preserves the active event feed without unsubscribing", async () => {
    // Select s1
    await useChatStore.getState().selectSession("s1");
    expect(mockedClient.chat.subscribe).toHaveBeenCalledWith(
      "s1",
      expect.any(Function)
    );

    // Switch to s2
    await useChatStore.getState().selectSession("s2");
    expect(mockedClient.chat.subscribe).toHaveBeenCalledWith(
      "s2",
      expect.any(Function)
    );
    expect(mockedClient.chat.unsubscribe).not.toHaveBeenCalled();

    // Switch back to s1
    await useChatStore.getState().selectSession("s1");
    // Verify client.chat.unsubscribe was never called during session switching
    expect(mockedClient.chat.unsubscribe).not.toHaveBeenCalled();

    // Emitting on s1 feeds the active transcript
    await emit(
      ev(Kind.KindTextDelta, {
        turn_id: "t-live",
        text: "live delta on s1",
      })
    );

    const entries = useChatStore.getState().entries;

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "assistant",
      turnID: "t-live",
      text: "live delta on s1",
    });
  });

  it("pruning sessions on agent switch calls client.chat.unsubscribe", async () => {
    await useChatStore.getState().selectSession("s1");
    expect(mockedClient.chat.subscribe).toHaveBeenCalledWith(
      "s1",
      expect.any(Function)
    );

    // Switch to agent a2 (which has no sessions in mock setup)
    await useChatStore.getState().selectAgent("a2");

    expect(mockedClient.chat.unsubscribe).toHaveBeenCalledWith("s1");
  });

  it("deleting a session calls client.chat.unsubscribe", async () => {
    await useChatStore.getState().selectSession("s1");
    expect(mockedClient.chat.subscribe).toHaveBeenCalledWith(
      "s1",
      expect.any(Function)
    );

    await useChatStore.getState().deleteSession("s1");

    expect(mockedClient.chat.deleteSession).toHaveBeenCalledWith("s1");
    expect(mockedClient.chat.unsubscribe).toHaveBeenCalledWith("s1");
  });
});

describe("initial load event buffering (task 4.1)", () => {
  it("live events arriving during initial page load survive the page fold", async () => {
    let resolveTranscriptPage: (
      events: SessionEvent[]
    ) => void = (): void => {};

    mockedClient.chat.transcriptPage.mockImplementationOnce(
      (): Promise<SessionEvent[]> =>
        new Promise<SessionEvent[]>(
          (resolve: (events: SessionEvent[]) => void): void => {
            resolveTranscriptPage = resolve;
          }
        )
    );

    const selectPromise = useChatStore.getState().selectSession("s1");

    // Wait until subscribe has completed and liveHandler is wired
    await waitFor(() => {
      expect(mockedClient.chat.subscribe).toHaveBeenCalledWith(
        "s1",
        expect.any(Function)
      );
    });

    // While transcriptPage is still resolving, live events arrive (e.g. catch-up snapshot delta)
    await emit(
      ev(Kind.KindTextDelta, {
        id: "snap-t1-partial",
        turn_id: "t1",
        text: "in-flight partial",
      })
    );

    // Now transcriptPage lands with historical user message
    resolveTranscriptPage([
      ev(Kind.KindUserMessage, {
        id: "e-user-1",
        turn_id: "t1",
        text: "hello world",
      }),
    ]);

    await selectPromise;

    // Both the durable page event and the buffered live delta survive
    const entries = useChatStore.getState().entries;

    expect(entries).toHaveLength(2);
    expect(entries[0]).toMatchObject({
      kind: "user",
      text: "hello world",
    });
    expect(entries[1]).toMatchObject({
      kind: "assistant",
      text: "in-flight partial",
      streaming: true,
    });
    expect(useChatStore.getState().loadingSession).toBe(false);
  });

  it("duplicate events between transcript page and buffer are deduped by event id", async () => {
    let resolveTranscriptPage: (
      events: SessionEvent[]
    ) => void = (): void => {};

    mockedClient.chat.transcriptPage.mockImplementationOnce(
      (): Promise<SessionEvent[]> =>
        new Promise<SessionEvent[]>(
          (resolve: (events: SessionEvent[]) => void): void => {
            resolveTranscriptPage = resolve;
          }
        )
    );

    const selectPromise = useChatStore.getState().selectSession("s1");

    await waitFor(() => {
      expect(mockedClient.chat.subscribe).toHaveBeenCalledWith(
        "s1",
        expect.any(Function)
      );
    });

    // Buffered event with an ID
    await emit(
      ev(Kind.KindUserMessage, {
        id: "e-shared-1",
        turn_id: "t1",
        text: "shared message",
      })
    );

    // Transcript page also contains the same event ID
    resolveTranscriptPage([
      ev(Kind.KindUserMessage, {
        id: "e-shared-1",
        turn_id: "t1",
        text: "shared message",
      }),
    ]);

    await selectPromise;

    const entries = useChatStore.getState().entries;

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "user",
      text: "shared message",
    });
  });

  it("superseded switch discards buffered events without writing state", async () => {
    let resolveFirstPage: (events: SessionEvent[]) => void = (): void => {};

    mockedClient.chat.transcriptPage.mockImplementationOnce(
      (): Promise<SessionEvent[]> =>
        new Promise<SessionEvent[]>(
          (resolve: (events: SessionEvent[]) => void): void => {
            resolveFirstPage = resolve;
          }
        )
    );

    const s1Promise = useChatStore.getState().selectSession("s1");

    await waitFor(() => {
      expect(mockedClient.chat.subscribe).toHaveBeenCalledWith(
        "s1",
        expect.any(Function)
      );
    });

    // Buffer event for s1
    const s1Handler = liveHandlers.get("s1");

    s1Handler?.(
      ev(Kind.KindTextDelta, {
        id: "snap-s1",
        turn_id: "t1",
        text: "buffered for s1",
      })
    );

    // Switch to s2 immediately
    await useChatStore.getState().selectSession("s2");

    // Late resolution of s1's transcriptPage
    resolveFirstPage([
      ev(Kind.KindUserMessage, { id: "e-s1", turn_id: "t1", text: "s1 msg" }),
    ]);
    await s1Promise;

    // Active session is s2 and s1's events did not overwrite state
    expect(useChatStore.getState().activeSessionID).toBe("s2");
  });
});

describe("re-adopt in-flight turn on re-entry (task 4.2)", () => {
  it("re-entering a busy session adopts activeTurnID and settles on turn_completed", async () => {
    mockedClient.chat.turnState.mockResolvedValueOnce({
      busy: true,
      turn_id: "t-in-flight",
    });

    await useChatStore.getState().selectSession("s1");

    expect(useChatStore.getState().activeTurnID).toBe("t-in-flight");
    expect(useChatStore.getState().busy).toBe(true);
    expect(useChatStore.getState().runningSessions.has("s1")).toBe(true);

    // Terminal completion event arrives
    await emit(
      ev(Kind.KindTurnCompleted, {
        turn_id: "t-in-flight",
        id: "e-completed",
      })
    );

    expect(useChatStore.getState().activeTurnID).toBeNull();
    expect(useChatStore.getState().busy).toBe(false);
    expect(useChatStore.getState().runningSessions.has("s1")).toBe(false);
  });

  it("re-entering a busy session settles on turn_cancelled", async () => {
    mockedClient.chat.turnState.mockResolvedValueOnce({
      busy: true,
      turn_id: "t-cancel-flight",
    });

    await useChatStore.getState().selectSession("s1");

    expect(useChatStore.getState().activeTurnID).toBe("t-cancel-flight");
    expect(useChatStore.getState().busy).toBe(true);
    expect(useChatStore.getState().runningSessions.has("s1")).toBe(true);

    await emit(
      ev(Kind.KindTurnCancelled, {
        turn_id: "t-cancel-flight",
        id: "e-cancelled",
      })
    );

    expect(useChatStore.getState().activeTurnID).toBeNull();
    expect(useChatStore.getState().busy).toBe(false);
    expect(useChatStore.getState().runningSessions.has("s1")).toBe(false);
  });

  it("re-entering an idle session after terminal event adopts nothing", async () => {
    mockedClient.chat.turnState.mockResolvedValueOnce({
      busy: false,
      turn_id: "",
    });

    await useChatStore.getState().selectSession("s1");

    expect(useChatStore.getState().activeTurnID).toBeNull();
    expect(useChatStore.getState().busy).toBe(false);
    expect(useChatStore.getState().runningSessions.has("s1")).toBe(false);
  });

  it("terminal event arriving during load completes turn and does not adopt stale busy", async () => {
    let resolveTranscriptPage: (
      events: SessionEvent[]
    ) => void = (): void => {};

    mockedClient.chat.transcriptPage.mockImplementationOnce(
      (): Promise<SessionEvent[]> =>
        new Promise<SessionEvent[]>(
          (resolve: (events: SessionEvent[]) => void): void => {
            resolveTranscriptPage = resolve;
          }
        )
    );

    mockedClient.chat.turnState.mockResolvedValueOnce({
      busy: true,
      turn_id: "t-ended-mid-load",
    });

    const selectPromise = useChatStore.getState().selectSession("s1");

    await waitFor(() => {
      expect(mockedClient.chat.subscribe).toHaveBeenCalledWith(
        "s1",
        expect.any(Function)
      );
    });

    // Terminal completion event arrives during load (buffered)
    await emit(
      ev(Kind.KindTurnCompleted, {
        turn_id: "t-ended-mid-load",
        id: "e-completed-mid-load",
      })
    );

    resolveTranscriptPage([]);
    await selectPromise;

    // Because the turn terminal was folded, finishedTurnIDs prevented adopting stale busy state
    expect(useChatStore.getState().activeTurnID).toBeNull();
    expect(useChatStore.getState().busy).toBe(false);
    expect(useChatStore.getState().runningSessions.has("s1")).toBe(false);
  });
});

describe("lastTurnOutcome (add-live-agent-avatar design D4)", () => {
  it("starts null before any live terminal arrives", () => {
    expect(useChatStore.getState().lastTurnOutcome).toBeNull();
  });

  it("a live turn_completed for the active session records the outcome", async () => {
    await useChatStore.getState().selectSession("s1");

    await emit(
      ev(Kind.KindTurnCompleted, {
        turn_id: "t-done",
        id: "e-done",
        occurred_at: "2026-02-01T12:00:00Z",
      })
    );

    expect(useChatStore.getState().lastTurnOutcome).toEqual({
      turnID: "t-done",
      kind: Kind.KindTurnCompleted,
      at: "2026-02-01T12:00:00Z",
    });
  });

  it("turn_error and turn_cancelled likewise record their kinds", async () => {
    await useChatStore.getState().selectSession("s1");

    await emit(ev(Kind.KindTurnError, { turn_id: "t-err", id: "e-err" }));
    expect(useChatStore.getState().lastTurnOutcome).toMatchObject({
      turnID: "t-err",
      kind: Kind.KindTurnError,
    });

    await emit(
      ev(Kind.KindTurnCancelled, { turn_id: "t-cancel", id: "e-cancel" })
    );
    expect(useChatStore.getState().lastTurnOutcome).toMatchObject({
      turnID: "t-cancel",
      kind: Kind.KindTurnCancelled,
    });
  });

  it("a non-terminal event leaves the last outcome unchanged", async () => {
    await useChatStore.getState().selectSession("s1");

    await emit(ev(Kind.KindTurnCompleted, { turn_id: "t-1", id: "e-done" }));
    const outcome = useChatStore.getState().lastTurnOutcome;

    expect(outcome).not.toBeNull();
    await emit(
      ev(Kind.KindTextDelta, { turn_id: "t-1", text: "streaming", id: "e-d1" })
    );

    expect(useChatStore.getState().lastTurnOutcome).toBe(outcome);
  });

  it("a terminal for a background session never moves the viewed outcome", async () => {
    await useChatStore.getState().selectSession("s1");
    // SAFETY: subscribe's mock captured the handler; s1's survives the
    // switch (subscriptions persist across session switches).
    const s1Handler = liveHandler;
    await useChatStore.getState().selectSession("s2");

    s1Handler?.(ev(Kind.KindTurnCompleted, { turn_id: "t-bg", id: "e-bg" }));

    expect(useChatStore.getState().lastTurnOutcome).toBeNull();
  });

  it("two consecutive completions mint two different outcome identities", async () => {
    await useChatStore.getState().selectSession("s1");

    await emit(ev(Kind.KindTurnCompleted, { turn_id: "t-1", id: "e-c1" }));
    const first = useChatStore.getState().lastTurnOutcome;

    await emit(ev(Kind.KindTurnCompleted, { turn_id: "t-2", id: "e-c2" }));
    const second = useChatStore.getState().lastTurnOutcome;

    expect(first).not.toBeNull();
    expect(second).not.toBeNull();
    expect(second).not.toBe(first);
  });

  it("selectSession resets the outcome to null for the session being opened", async () => {
    await useChatStore.getState().selectSession("s1");
    await emit(ev(Kind.KindTurnCompleted, { turn_id: "t-1", id: "e-done" }));
    expect(useChatStore.getState().lastTurnOutcome).not.toBeNull();

    await useChatStore.getState().selectSession("s2");
    expect(useChatStore.getState().lastTurnOutcome).toBeNull();
  });
});

describe("panel tabs — pinned identity tab (add-pinned-identity-tab)", () => {
  /** A workspace-file preview tab literal: the reducers only touch
   * id/identity, so the panelRenderers builder is not needed here. */
  function fileTab(relPath: string): Omit<PanelTab, "id"> {
    return {
      kind: "workspace-file",
      identity: `workspace-file:a1:${relPath}`,
      title: relPath,
      data: { agentID: "a1", relPath, name: relPath },
    };
  }

  function tabIDs(): string[] {
    return useChatStore.getState().panelTabs.map((t: PanelTab) => t.id);
  }

  it("activatePanelTab(null) keeps the open tabs and raises the identity", () => {
    useChatStore.getState().openPanelTab(fileTab("notes.md"));
    useChatStore.getState().openPanelTab(fileTab("plan.md"));
    const ids = tabIDs();

    useChatStore.getState().activatePanelTab(null);

    expect(tabIDs()).toEqual(ids);
    expect(useChatStore.getState().activePanelTabID).toBeNull();
  });

  it("closing a background tab while the identity is active keeps it", () => {
    useChatStore.getState().openPanelTab(fileTab("notes.md"));
    useChatStore.getState().openPanelTab(fileTab("plan.md"));
    const ids = tabIDs();
    useChatStore.getState().activatePanelTab(null);

    useChatStore.getState().closePanelTab(ids[0]);

    expect(tabIDs()).toEqual([ids[1]]);
    expect(useChatStore.getState().activePanelTabID).toBeNull();
  });

  it("a preview from the identity takes over; the last close returns null", () => {
    useChatStore.getState().openPanelTab(fileTab("notes.md"));
    useChatStore.getState().activatePanelTab(null);

    // Opening from the identity tab makes the new preview the active surface.
    useChatStore.getState().openPanelTab(fileTab("plan.md"));
    const openedID = tabIDs()[1];
    expect(useChatStore.getState().activePanelTabID).toBe(openedID);

    useChatStore.getState().closePanelTab(openedID);
    useChatStore.getState().closePanelTab(tabIDs()[0]);

    expect(useChatStore.getState().panelTabs).toEqual([]);
    expect(useChatStore.getState().activePanelTabID).toBeNull();
  });

  it("closing the active tab of two raises the last remaining one", () => {
    useChatStore.getState().openPanelTab(fileTab("notes.md"));
    useChatStore.getState().openPanelTab(fileTab("plan.md"));
    const ids = tabIDs();

    useChatStore.getState().closePanelTab(ids[1]);

    expect(useChatStore.getState().activePanelTabID).toBe(ids[0]);
    expect(tabIDs()).toEqual([ids[0]]);
  });
});
