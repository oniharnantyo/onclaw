/**
 * Chat store group tests (add-agent-groups tasks 5.1/5.3, design D9):
 * openGroup loads the group, roster, newest transcript page, and tasks and
 * switches the surface (activeSessionID clears so the composer targets the
 * group); an agent-session selection closes the group back; a group send
 * routes through the groups binding with the mention picker's recipient;
 * the live group:<id>:event channel appends a member's reply and refreshes
 * task rows without any manual refetch; and refreshGroups feeds the rail's
 * busy dots from the per-group task states.
 */
import { act } from "react";
import { mockedSurface } from "./mockedSurface";
import { waitFor } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  Agent,
  AgentGroup,
  AgentGroupMember,
  AgentGroupMessage,
  AgentGroupTask,
  Session,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { GroupEvent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client } from "../../../lib/client";
import type { TurnsChangedEvent } from "../../../lib/events";
import { useNavStore } from "../../../stores/nav";
import { connectGroups, useChatStore } from "../store";
import type { Entry } from "../types";

const agents: Agent[] = [
  {
    ID: "a1",
    Name: "Scout",
    ProviderID: "prov",
    Model: "m",
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
    Name: "Runner",
    ProviderID: "prov",
    Model: "m",
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

function groupRow(overrides: Partial<AgentGroup> = {}): AgentGroup {
  return {
    ID: "g1",
    Name: "research",
    Title: "Research",
    Instructions: "study things",
    LeadAgentID: "a1",
    Archived: false,
    Revision: 1,
    CreatedAt: "2026-01-01T00:00:00Z",
    UpdatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function memberRow(agentID: string): AgentGroupMember {
  return { GroupID: "g1", AgentID: agentID, AddedAt: "2026-01-01T00:00:00Z" };
}

function messageRow(overrides: Partial<AgentGroupMessage>): AgentGroupMessage {
  return {
    GroupID: "g1",
    MessageID: "m1",
    Seq: 1,
    Kind: "message",
    AuthorKind: "user",
    AuthorAgentID: null,
    TaskID: null,
    Payload: JSON.stringify({ text: "hello" }),
    OccurredAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function taskRow(overrides: Partial<AgentGroupTask>): AgentGroupTask {
  return {
    TaskID: "t1",
    GroupID: "g1",
    ParentTaskID: null,
    RootTaskID: "t1",
    OwnerAgentID: "a1",
    RequestMessageID: "m1",
    Instruction: "do it",
    ExpectedResult: "",
    State: "queued",
    StopReason: null,
    Revision: 1,
    AssignmentCount: 0,
    Error: null,
    ...overrides,
  };
}

const sessions: Session[] = [
  {
    ID: "s1",
    AgentID: "a1",
    Title: "",
    Model: "",
    Effort: "",
    CreatedAt: "2026-01-01T00:00:00Z",
    LastActivityAt: "2026-01-01T00:00:00Z",
  },
];

let groupsList: AgentGroup[];

let groupPage: AgentGroupMessage[];

let groupTasks: AgentGroupTask[];

/** The per-group live handlers the client mock captured (design D9). */
const groupHandlers = new Map<string, (event: GroupEvent) => void>();

let turnsHandler: ((event: TurnsChangedEvent) => void) | null = null;

type MockFn = ReturnType<typeof vi.fn>;

type MockedClient = Client & {
  agents: { list: MockFn; listWorkspaceSkills: MockFn };
  chat: {
    allSessions: MockFn;
    runningSessions: MockFn;
    createSession: MockFn;
    send: MockFn;
    subscribe: MockFn;
    unsubscribe: MockFn;
    turnState: MockFn;
    transcriptPage: MockFn;
    contextUsage: MockFn;
    deleteSession: MockFn;
  };
  settings: { providers: MockFn; skills: MockFn; providerModels: MockFn };
  groups: {
    list: MockFn;
    get: MockFn;
    update: MockFn;
    delete: MockFn;
    members: MockFn;
    addMember: MockFn;
    removeMember: MockFn;
    setLead: MockFn;
    transcriptPage: MockFn;
    tasks: MockFn;
    send: MockFn;
    stopTask: MockFn;
    continueTask: MockFn;
    reassignTask: MockFn;
    cancelTask: MockFn;
    onGroupEvent: MockFn;
  };
  events: { onTurnsChanged: MockFn; onGroupsChanged: MockFn };
};

function makeClient(): MockedClient {
  const c = {
    agents: {
      list: vi.fn(async () => agents),
      listWorkspaceSkills: vi.fn(async () => []),
    },
    chat: {
      allSessions: vi.fn(async (): Promise<Session[]> => sessions),
      runningSessions: vi.fn(async (): Promise<string[]> => []),
      createSession: vi.fn(),
      send: vi.fn(async () => "turn-1"),
      subscribe: vi.fn(async () => (): void => undefined),
      unsubscribe: vi.fn(async () => undefined),
      turnState: vi.fn(async () => ({ busy: false, turn_id: "" })),
      transcriptPage: vi.fn(async () => []),
      contextUsage: vi.fn(async () => ({ used: 0, limit: 200000 })),
      deleteSession: vi.fn(async () => undefined),
    },
    settings: {
      providers: vi.fn(async () => []),
      skills: vi.fn(async () => []),
      providerModels: vi.fn(async () => []),
    },
    groups: {
      list: vi.fn(async (): Promise<AgentGroup[]> => groupsList),
      get: vi.fn(async (id: string): Promise<AgentGroup> =>
        groupRow({ ID: id })
      ),
      update: vi.fn(async () => groupRow()),
      delete: vi.fn(async () => undefined),
      members: vi.fn(async (): Promise<AgentGroupMember[]> => [
        memberRow("a1"),
        memberRow("a2"),
      ]),
      addMember: vi.fn(async () => undefined),
      removeMember: vi.fn(async () => undefined),
      setLead: vi.fn(async () => undefined),
      transcriptPage: vi.fn(
        async (
          _groupID: string,
          _beforeSeq: number,
          _limit: number
        ): Promise<AgentGroupMessage[]> => groupPage
      ),
      tasks: vi.fn(
        async (_groupID: string): Promise<AgentGroupTask[]> => groupTasks
      ),
      send: vi.fn(async () => undefined),
      stopTask: vi.fn(async () => undefined),
      continueTask: vi.fn(async () => undefined),
      reassignTask: vi.fn(async () => undefined),
      cancelTask: vi.fn(async () => undefined),
      onGroupEvent: vi.fn(
        (groupID: string, handler: (event: GroupEvent) => void) => {
          groupHandlers.set(groupID, handler);

          return (): void => {
            groupHandlers.delete(groupID);
          };
        }
      ),
    },
    events: {
      onTurnsChanged: vi.fn((handler: (event: TurnsChangedEvent) => void) => {
        turnsHandler = handler;

        return (): void => {
          if (turnsHandler === handler) turnsHandler = null;
        };
      }),
      onGroupsChanged: vi.fn(() => (): void => undefined),
    },
  };

  // SAFETY: the object literal above satisfies the Client surface with its
  // vi.fn members matching the MockedClient contract; the conversion
  // funnels through the shared mockedSurface bridge.
  return mockedSurface<MockedClient, object>(c);
}

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../lib/client";

// Typed alias exposing the vi.fn mocks behind the Client-shaped export.
// SAFETY: the client module is mocked with makeClient(), so its runtime
// shape is exactly this mock object.
const mockedClient = client as MockedClient;

/** Fires the captured live handler for one group. */
function emitGroupEvent(groupID: string, event: GroupEvent): Promise<void> {
  return act(async () => {
    groupHandlers.get(groupID)?.(event);
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  groupHandlers.clear();
  turnsHandler = null;
  // Unwind the per-group subscriptions a previous test left behind (the
  // store module is file-level): subscribe-then-unsubscribe clears the map.
  connectGroups()();
  groupsList = [groupRow()];
  groupPage = [
    messageRow({}),
    messageRow({
      MessageID: "m2",
      Seq: 2,
      Kind: "message",
      AuthorKind: "agent",
      AuthorAgentID: "a1",
      Payload: JSON.stringify({ text: "scout's reply" }),
    }),
  ];
  groupTasks = [taskRow({})];
  useNavStore.setState({ menu: "chat", chatTarget: null });
  useChatStore.setState({
    agents,
    providers: [],
    skills: [],
    activeAgentID: null,
    sessions,
    activeSessionID: null,
    groups: [],
    activeGroupID: null,
    groupMembers: [],
    groupMessages: [],
    groupTasks: [],
    busyGroups: new Set<string>(),
    groupRecipient: null,
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
    panelTabs: [],
    activePanelTabID: null,
  });
});

describe("openGroup (add-agent-groups task 5.1)", () => {
  it("loads the group, roster, transcript page, and tasks, and switches the surface", async () => {
    await act(async () => {
      await useChatStore.getState().openGroup("g1");
    });

    const s = useChatStore.getState();

    expect(s.activeGroupID).toBe("g1");
    expect(s.activeSessionID).toBeNull();
    expect(s.activeAgentID).toBeNull();
    expect(
      s.groupMembers.map((m: AgentGroupMember): string => m.AgentID)
    ).toEqual(["a1", "a2"]);
    expect(s.groupMessages).toHaveLength(2);
    expect(s.groupTasks).toHaveLength(1);
    expect(s.loadingSession).toBe(false);
    // The transcript maps into the shared entries for the one renderer.
    expect(s.entries.map((e: Entry): string => e.kind)).toEqual([
      "user",
      "assistant",
    ]);
    // The rail's list gains the fresh row.
    expect(s.groups.some((g: AgentGroup): boolean => g.ID === "g1")).toBe(true);
  });

  it("selecting an agent session closes the group surface", async () => {
    await act(async () => {
      await useChatStore.getState().openGroup("g1");
    });

    expect(useChatStore.getState().activeGroupID).toBe("g1");

    await act(async () => {
      await useChatStore.getState().selectAgent("a1", "s1");
    });

    const s = useChatStore.getState();

    expect(s.activeGroupID).toBeNull();
    expect(s.activeSessionID).toBe("s1");
    expect(s.groupMessages).toHaveLength(0);
    expect(s.groupMembers).toHaveLength(0);
  });

  it("supersedes a stale group load (the loadSeq guard)", async () => {
    // A slow first load loses to a fast second one.
    let resolveFirst: ((rows: AgentGroupMessage[]) => void) | null = null;

    mockedClient.groups.transcriptPage.mockImplementationOnce(
      (): Promise<AgentGroupMessage[]> =>
        new Promise((resolve: (rows: AgentGroupMessage[]) => void): void => {
          resolveFirst = resolve;
        })
    );

    const first = useChatStore.getState().openGroup("g1");

    const second = useChatStore.getState().openGroup("g1");

    await act(async () => {
      resolveFirst?.([]);
      await first;
      await second;
    });

    expect(useChatStore.getState().activeGroupID).toBe("g1");
    // The stale continuation wrote nothing after the newer load landed.
    expect(useChatStore.getState().loadingSession).toBe(false);
  });

  it("records a failed load and keeps the surface honest", async () => {
    mockedClient.groups.get.mockRejectedValueOnce(new Error("gone"));

    await act(async () => {
      await useChatStore.getState().openGroup("gX");
    });

    const s = useChatStore.getState();

    expect(s.error).toContain("gone");
    expect(s.loadingSession).toBe(false);
  });
});

describe("the group send path (add-agent-groups design D4)", () => {
  it("routes a send through the groups binding with the picker's recipient", async () => {
    await act(async () => {
      await useChatStore.getState().openGroup("g1");
    });

    await act(async () => {
      useChatStore.getState().setGroupRecipient("a2");
    });

    await act(async () => {
      await useChatStore.getState().send("summarize the corpus");
    });

    expect(mockedClient.groups.send).toHaveBeenCalledOnce();

    const req = mockedClient.groups.send.mock.calls[0][0];

    expect(req.GroupID).toBe("g1");
    expect(req.Text).toBe("summarize the corpus");
    expect(req.RecipientAgentID).toBe("a2");
    expect(req.ReplyToMessageID).toBe("");
    // The recipient rides exactly one send.
    expect(useChatStore.getState().groupRecipient).toBeNull();
  });

  it("routes a send with no recipient untargeted", async () => {
    await act(async () => {
      await useChatStore.getState().openGroup("g1");
    });

    await act(async () => {
      await useChatStore.getState().send("@everyone give your view");
    });

    const req = mockedClient.groups.send.mock.calls[0][0];

    expect(req.Text).toBe("@everyone give your view");
    expect(req.RecipientAgentID).toBe("");
  });

  it("surfaces a refused send through the store error", async () => {
    await act(async () => {
      await useChatStore.getState().openGroup("g1");
    });

    mockedClient.groups.send.mockRejectedValueOnce(new Error("lane busy"));

    await act(async () => {
      await useChatStore.getState().send("hello");
    });

    expect(useChatStore.getState().error).toContain("lane busy");
  });
});

describe("the live group channel (add-agent-groups design D9, task 5.3)", () => {
  it("appends a member's reply from a message-appended event without a refetch", async () => {
    await act(async () => {
      await useChatStore.getState().openGroup("g1");
    });

    expect(useChatStore.getState().entries).toHaveLength(2);

    const before = mockedClient.groups.transcriptPage.mock.calls.length;

    await emitGroupEvent("g1", {
      kind: "message-appended",
      messageId: "m3",
      seq: 3,
      message: messageRow({
        MessageID: "m3",
        Seq: 3,
        Kind: "message",
        AuthorKind: "agent",
        AuthorAgentID: "a2",
        Payload: JSON.stringify({ text: "runner's reply" }),
      }),
    });

    const s = useChatStore.getState();

    expect(s.groupMessages).toHaveLength(3);
    expect(s.entries).toHaveLength(3);
    expect(s.entries[2]).toMatchObject({
      kind: "assistant",
      text: "runner's reply",
      author: { kind: "member", agentID: "a2" },
    });
    // No refetch: the push carried the row.
    expect(mockedClient.groups.transcriptPage.mock.calls.length).toBe(before);
  });

  it("replaces a copied-back row regenerated in place (same MessageID)", async () => {
    await act(async () => {
      await useChatStore.getState().openGroup("g1");
    });

    await emitGroupEvent("g1", {
      kind: "message-appended",
      messageId: "m2",
      seq: 2,
      message: messageRow({
        MessageID: "m2",
        Seq: 2,
        Kind: "message",
        AuthorKind: "agent",
        AuthorAgentID: "a1",
        Payload: JSON.stringify({ text: "scout's REGENERATED reply" }),
      }),
    });

    const s = useChatStore.getState();

    expect(s.groupMessages).toHaveLength(2);
    expect(s.entries).toHaveLength(2);
    expect(s.entries[1]).toMatchObject({
      text: "scout's REGENERATED reply",
    });
  });

  it("ignores transcript pushes for a group that is not open", async () => {
    await act(async () => {
      await useChatStore.getState().openGroup("g1");
    });

    await act(async () => {
      await useChatStore.getState().selectAgent("a1", "s1");
    });

    await emitGroupEvent("g1", {
      kind: "message-appended",
      messageId: "m9",
      seq: 9,
      message: messageRow({ MessageID: "m9", Seq: 9 }),
    });

    expect(useChatStore.getState().groupMessages).toHaveLength(0);
  });

  it("refreshes the task rows on a task-updated event and flags the busy dot", async () => {
    await act(async () => {
      await useChatStore.getState().openGroup("g1");
    });

    expect(useChatStore.getState().busyGroups.has("g1")).toBe(false);

    groupTasks = [taskRow({ State: "running" })];

    await emitGroupEvent("g1", { kind: "task-updated", taskId: "t1" });

    await waitFor(() =>
      expect(useChatStore.getState().groupTasks[0]?.State).toBe("running")
    );
    expect(useChatStore.getState().busyGroups.has("g1")).toBe(true);

    groupTasks = [taskRow({ State: "completed" })];

    await emitGroupEvent("g1", { kind: "task-updated", taskId: "t1" });

    await waitFor(() =>
      expect(useChatStore.getState().busyGroups.has("g1")).toBe(false)
    );
  });
});

describe("refreshGroups (the rail's groups section)", () => {
  it("feeds the busy dots from the per-group task states", async () => {
    groupsList = [groupRow(), groupRow({ ID: "g2", Name: "ops", Title: "" })];
    groupTasks = [taskRow({ State: "running" })];

    // Only g1 reports tasks in this pass; the per-group reads follow the
    // list order.
    mockedClient.groups.tasks.mockImplementation(
      async (groupID: string): Promise<AgentGroupTask[]> =>
        groupID === "g1" ? groupTasks : []
    );

    await act(async () => {
      await useChatStore.getState().refreshGroups();
    });

    const s = useChatStore.getState();

    expect(s.groups).toHaveLength(2);
    expect(s.busyGroups.has("g1")).toBe(true);
    expect(s.busyGroups.has("g2")).toBe(false);
  });

  it("aligns the per-group live subscriptions with the list", async () => {
    await act(async () => {
      await useChatStore.getState().refreshGroups();
    });

    expect(mockedClient.groups.onGroupEvent).toHaveBeenCalledWith(
      "g1",
      expect.any(Function)
    );

    // A removed group's channel unsubscribes; a new one's subscribes.
    groupsList = [groupRow({ ID: "g2", Name: "ops", Title: "" })];

    await act(async () => {
      await useChatStore.getState().refreshGroups();
    });

    expect(mockedClient.groups.onGroupEvent).toHaveBeenCalledWith(
      "g2",
      expect.any(Function)
    );
  });

  it("keeps the last known list when the fetch fails", async () => {
    await act(async () => {
      await useChatStore.getState().refreshGroups();
    });

    expect(useChatStore.getState().groups).toHaveLength(1);

    mockedClient.groups.list.mockRejectedValueOnce(new Error("down"));

    await act(async () => {
      await useChatStore.getState().refreshGroups();
    });

    expect(useChatStore.getState().groups).toHaveLength(1);
  });

  it("connectGroups refetches on groups:changed and unwinds cleanly", async () => {
    const offChanged = vi.fn();

    mockedClient.events.onGroupsChanged.mockImplementation(
      (handler: () => void) => {
        void handler;

        return offChanged;
      }
    );

    await act(async () => {
      const off = connectGroups();

      off();
    });

    expect(offChanged).toHaveBeenCalledOnce();
  });
});

describe("the nav chat target (add-agent-groups design D9)", () => {
  it("init consumes a group target through openGroup", async () => {
    useNavStore.setState({ menu: "chat", chatTarget: { groupID: "g1" } });

    await act(async () => {
      await useChatStore.getState().init();
    });

    expect(useChatStore.getState().activeGroupID).toBe("g1");
    expect(useNavStore.getState().chatTarget).toBeNull();
  });

  it("init still opens an agent target when the target carries one", async () => {
    useNavStore.setState({
      menu: "chat",
      chatTarget: { agentID: "a1", sessionID: "s1" },
    });

    await act(async () => {
      await useChatStore.getState().init();
    });

    expect(useChatStore.getState().activeGroupID).toBeNull();
    expect(useChatStore.getState().activeAgentID).toBe("a1");
  });
});

describe("the group transcript pages backward", () => {
  it("loadEarlier prepends the older page through the same mapping", async () => {
    groupPage = Array.from(
      { length: 60 },
      (_row: undefined, i: number): AgentGroupMessage =>
        messageRow({
          MessageID: `m${i + 1}`,
          Seq: i + 1,
          Payload: JSON.stringify({ text: `row ${i + 1}` }),
        })
    );

    await act(async () => {
      await useChatStore.getState().openGroup("g1");
    });

    // A full page leaves the windowing cursor set.
    expect(useChatStore.getState().oldestSeq).toBe(1);

    const olderPage = [
      messageRow({
        MessageID: "m0",
        Seq: 0,
        Payload: JSON.stringify({ text: "oldest" }),
      }),
    ];

    mockedClient.groups.transcriptPage.mockResolvedValueOnce(olderPage);

    await act(async () => {
      await useChatStore.getState().loadEarlier();
    });

    const s = useChatStore.getState();

    expect(s.groupMessages[0]?.MessageID).toBe("m0");
    expect(s.entries[0]).toMatchObject({ kind: "user", text: "oldest" });
    // A short older page means the group's beginning is reached: the window
    // cursor retires (the same contract the session path pins).
    expect(s.oldestSeq).toBeNull();
  });
});
