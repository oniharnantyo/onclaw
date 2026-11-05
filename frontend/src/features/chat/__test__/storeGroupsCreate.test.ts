/**
 * Chat store createGroup tests (spec agent-groups "Group entity and
 * membership", scenario "Create a group with members"): the action calls the
 * groups create binding with the entered fields and the initial roster,
 * refreshes the rail's list, and opens the created group's conversation (it
 * opens as a conversation); a refused create records the store error and
 * resolves null without touching the surface.
 */
import { act } from "react";
import { mockedSurface } from "./mockedSurface";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  Agent,
  AgentGroup,
  AgentGroupMember,
  AgentGroupMessage,
  AgentGroupTask,
  Session,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../../../lib/client";
import { connectGroups, useChatStore } from "../store";

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

let createdRow: AgentGroup;

let groupsList: AgentGroup[];

function groupRow(overrides: Partial<AgentGroup> = {}): AgentGroup {
  return {
    ID: "g-new",
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
  return {
    GroupID: "g-new",
    AgentID: agentID,
    AddedAt: "2026-01-01T00:00:00Z",
  };
}

function messageRow(overrides: Partial<AgentGroupMessage>): AgentGroupMessage {
  return {
    GroupID: "g-new",
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

function taskRow(): AgentGroupTask {
  return {
    TaskID: "t1",
    GroupID: "g-new",
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

type MockFn = ReturnType<typeof vi.fn>;

type MockedClient = Client & {
  agents: { list: MockFn; listWorkspaceSkills: MockFn };
  chat: {
    allSessions: MockFn;
    runningSessions: MockFn;
    send: MockFn;
    subscribe: MockFn;
    unsubscribe: MockFn;
    turnState: MockFn;
    transcriptPage: MockFn;
    contextUsage: MockFn;
  };
  settings: { providers: MockFn; skills: MockFn; providerModels: MockFn };
  groups: {
    list: MockFn;
    get: MockFn;
    create: MockFn;
    update: MockFn;
    delete: MockFn;
    members: MockFn;
    addMember: MockFn;
    removeMember: MockFn;
    setLead: MockFn;
    transcriptPage: MockFn;
    tasks: MockFn;
    send: MockFn;
    onGroupEvent: MockFn;
  };
  events: { onTurnsChanged: MockFn; onGroupsChanged: MockFn };
};

function makeClient(): MockedClient {
  const c = {
    agents: {
      list: vi.fn(async (): Promise<Agent[]> => agents),
      listWorkspaceSkills: vi.fn(async (): Promise<never[]> => []),
    },
    chat: {
      allSessions: vi.fn(async (): Promise<Session[]> => sessions),
      runningSessions: vi.fn(async (): Promise<string[]> => []),
      send: vi.fn(async (): Promise<string> => "turn-1"),
      subscribe: vi.fn(async (): Promise<() => void> => (): void => undefined),
      unsubscribe: vi.fn(async (): Promise<void> => undefined),
      turnState: vi.fn(
        async (): Promise<{ busy: boolean; turn_id: string }> => ({
          busy: false,
          turn_id: "",
        })
      ),
      transcriptPage: vi.fn(async (): Promise<never[]> => []),
      contextUsage: vi.fn(
        async (): Promise<{ used: number; limit: number }> => ({
          used: 0,
          limit: 200000,
        })
      ),
    },
    settings: {
      providers: vi.fn(async (): Promise<never[]> => []),
      skills: vi.fn(async (): Promise<never[]> => []),
      providerModels: vi.fn(async (): Promise<never[]> => []),
    },
    groups: {
      list: vi.fn(async (): Promise<AgentGroup[]> => groupsList),
      get: vi.fn(async (id: string): Promise<AgentGroup> =>
        groupRow({ ID: id })
      ),
      create: vi.fn(
        async (
          _name: string,
          _title: string,
          _instructions: string,
          _memberAgentIDs: string[] | null
        ): Promise<AgentGroup> => createdRow
      ),
      update: vi.fn(async (): Promise<AgentGroup> => groupRow()),
      delete: vi.fn(async (): Promise<void> => undefined),
      members: vi.fn(async (): Promise<AgentGroupMember[]> => [
        memberRow("a1"),
        memberRow("a2"),
      ]),
      addMember: vi.fn(async (): Promise<void> => undefined),
      removeMember: vi.fn(async (): Promise<void> => undefined),
      setLead: vi.fn(async (): Promise<void> => undefined),
      transcriptPage: vi.fn(async (): Promise<AgentGroupMessage[]> => [
        messageRow({}),
      ]),
      tasks: vi.fn(async (): Promise<AgentGroupTask[]> => [taskRow()]),
      send: vi.fn(async (): Promise<void> => undefined),
      onGroupEvent: vi.fn((): (() => void) => (): void => undefined),
    },
    events: {
      onTurnsChanged: vi.fn((): (() => void) => (): void => undefined),
      onGroupsChanged: vi.fn((): (() => void) => (): void => undefined),
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

beforeEach(() => {
  vi.clearAllMocks();
  // Unwind the per-group subscriptions a previous test left behind (the
  // store module is file-level): subscribe-then-unsubscribe clears the map.
  connectGroups()();
  createdRow = groupRow();
  groupsList = [];
  useChatStore.setState({
    agents,
    providers: [],
    skills: [],
    activeAgentID: "a1",
    sessions,
    activeSessionID: "s1",
    groups: [],
    activeGroupID: null,
    groupMembers: [],
    groupMessages: [],
    groupTasks: [],
    busyGroups: new Set<string>(),
    groupRecipient: null,
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

describe("createGroup (spec: Create a group with members)", () => {
  it("calls the create binding with the fields and roster, then opens the created conversation", async () => {
    await act(async () => {
      await useChatStore
        .getState()
        .createGroup("research", "Research", "study things", ["a1", "a2"]);
    });

    expect(mockedClient.groups.create).toHaveBeenCalledWith(
      "research",
      "Research",
      "study things",
      ["a1", "a2"]
    );

    const s = useChatStore.getState();

    // The created group opens as a conversation (the spec scenario).
    expect(s.activeGroupID).toBe("g-new");
    expect(s.activeSessionID).toBeNull();
    expect(s.activeAgentID).toBeNull();
    // The rail's list gained the fresh row and the roster loaded.
    expect(s.groups.some((g: AgentGroup): boolean => g.ID === "g-new")).toBe(
      true
    );
    expect(
      s.groupMembers.map((m: AgentGroupMember): string => m.AgentID)
    ).toEqual(["a1", "a2"]);
  });

  it("passes null when the roster is empty", async () => {
    await act(async () => {
      await useChatStore.getState().createGroup("solo", "", "", null);
    });

    expect(mockedClient.groups.create).toHaveBeenCalledWith(
      "solo",
      "",
      "",
      null
    );
  });

  it("records a refused create through the store error and resolves null without opening anything", async () => {
    mockedClient.groups.create.mockRejectedValueOnce(new Error("name taken"));

    let resolvedNull = false;

    await act(async () => {
      resolvedNull =
        (await useChatStore
          .getState()
          .createGroup("research", "", "", ["a1"])) === null;
    });

    expect(resolvedNull).toBe(true);

    const s = useChatStore.getState();

    expect(s.error).toContain("name taken");
    // The surface is untouched: no conversation opened, no row appeared.
    expect(s.activeGroupID).toBeNull();
    expect(s.activeSessionID).toBe("s1");
    expect(s.groups).toHaveLength(0);
    expect(mockedClient.groups.get).not.toHaveBeenCalled();
  });
});
