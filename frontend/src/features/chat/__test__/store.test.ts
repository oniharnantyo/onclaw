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
import type { Client } from "../../../lib/client";
import { useChatStore } from "../store";
import type { Entry } from "../types";

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

let turnSeq = 0;

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  agents: { list: MockFn };
  chat: {
    sessions: MockFn;
    createSession: MockFn;
    send: MockFn;
    subscribe: MockFn;
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
};

function makeClient(): MockedClient {
  const c = {
    agents: { list: vi.fn(async () => agents) },
    chat: {
      sessions: vi.fn(async (agentID: string) =>
        agentID === "a1" ? sessions : []
      ),
      createSession: vi.fn(),
      send: vi.fn(async () => {
        turnSeq += 1;

        return `turn-${turnSeq}`;
      }),
      subscribe: vi.fn(
        async (_id: string, handler: (e: SessionEvent) => void) => {
          liveHandler = handler;

          return (): void => {
            liveHandler = null;
          };
        }
      ),
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
    runningSessions: new Set<string>(),
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  sessions = [row("s1", "m-reasoning", "high"), row("s2")];
  liveHandler = null;
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

    await useChatStore.getState().newSession();

    expect(mockedClient.chat.setSessionOverrides).not.toHaveBeenCalled();
    expect(
      useChatStore.getState().sessions.find((s: Session) => s.ID === "s-new")
        ?.Model
    ).toBe("");
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

function ev(
  kind: SessionEvent["kind"],
  extra: Partial<SessionEvent> = {}
): SessionEvent {
  return { kind, occurred_at: "2026-01-01T00:00:00Z", ...extra };
}

describe("refreshSessions (add-scheduler design D10)", () => {
  it("refetches the active agent's sessions when the agent matches", async () => {
    // Distinct activity stamps so the assertion also pins sortSessions' order.
    sessions = [
      row("s1"),
      { ...row("s-new"), LastActivityAt: "2026-02-01T00:00:00Z" },
    ];

    await useChatStore.getState().refreshSessions("a1");

    expect(mockedClient.chat.sessions).toHaveBeenCalledWith("a1");
    expect(
      useChatStore.getState().sessions.map((s: Session): string => s.ID)
    ).toEqual(["s-new", "s1"]);
  });

  it("ignores an event for a non-active agent", async () => {
    await useChatStore.getState().refreshSessions("a2");

    expect(mockedClient.chat.sessions).not.toHaveBeenCalled();
  });

  it("keeps the current list when the refetch fails", async () => {
    mockedClient.chat.sessions.mockImplementationOnce(
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
    mockedClient.chat.sessions.mockImplementationOnce(
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
    // The stale a1 write is dropped; a2's own (empty) list survives.
    expect(useChatStore.getState().sessions).toEqual([]);
  });
});
