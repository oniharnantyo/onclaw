/**
 * Session rail create-group tests (spec agent-groups "Group entity and
 * membership", scenario "Create a group with members"): the Groups section
 * header carries a "+" affordance (the per-agent new-session grammar's
 * section-header twin) that stays reachable with zero groups — the section
 * only hides with zero agents, where there is nothing to roster — and opens
 * the settings pane's create mode in a dialog. Filling the dialog and
 * creating calls the groups create binding with the entered fields and
 * roster; on success the store selects the created group (activeGroupID),
 * the rail lists it, and the dialog closes. Name gates the Create button; a
 * refused create toasts the failure and keeps the form.
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
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
import { dismissToast, getToasts } from "../../../lib/toasts";
import { mockedSurface } from "./mockedSurface";
import { SessionRail } from "../components/SessionRail";
import { useChatStore } from "../store";
import { renderUI } from "./test-utils";

const scout: Agent = {
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
};

const runner: Agent = {
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
};

function groupRow(overrides: Partial<AgentGroup> = {}): AgentGroup {
  return {
    ID: "g-new",
    Name: "research",
    Title: "",
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

function messageRow(): AgentGroupMessage {
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

const sessions: Session[] = [];

type MockFn = ReturnType<typeof vi.fn>;

type MockedClient = Client & {
  groups: {
    list: MockFn;
    get: MockFn;
    create: MockFn;
    members: MockFn;
    transcriptPage: MockFn;
    tasks: MockFn;
    onGroupEvent: MockFn;
  };
};

function makeClient(): Client {
  // The create flow runs the store's real createGroup action, so the mock
  // covers the whole pass: the binding, the refreshGroups refetch, and
  // openGroup's roster/page/tasks reads plus the live-channel subscribe.
  const c = {
    groups: {
      list: vi.fn(async (): Promise<AgentGroup[]> => []),
      get: vi.fn(async (id: string): Promise<AgentGroup> =>
        groupRow({ ID: id })
      ),
      create: vi.fn(async (): Promise<AgentGroup> => groupRow()),
      members: vi.fn(async (): Promise<AgentGroupMember[]> => [
        memberRow("a1"),
        memberRow("a2"),
      ]),
      transcriptPage: vi.fn(async (): Promise<AgentGroupMessage[]> => [
        messageRow(),
      ]),
      tasks: vi.fn(async (): Promise<AgentGroupTask[]> => [taskRow()]),
      onGroupEvent: vi.fn((): (() => void) => (): void => undefined),
    },
  };

  // SAFETY: the rail renders from the store; the client surface is called
  // only by the create flow under test, so the partial stub rides the
  // shared bridge under the named Client contract.
  return mockedSurface<Client, object>(c);
}

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../lib/client";

// SAFETY: the client module is mocked with makeClient(), so its runtime
// shape is exactly this mock object.
const mockedClient = client as MockedClient;

async function renderRail(): Promise<void> {
  await renderUI(<SessionRail />);
}

beforeEach(() => {
  vi.clearAllMocks();
  // Leftover toasts would leak into the failure-path assertions.
  getToasts().forEach((t: { id: string }): void => dismissToast(t.id));
  // SAFETY: the seed writes exactly the surface the rail and the create
  // flow read; the cast relabels the partial record for setState's Partial
  // parameter.
  useChatStore.setState({
    agents: [scout, runner],
    sessions,
    activeAgentID: "a1",
    activeSessionID: null,
    groups: [],
    activeGroupID: null,
    busyGroups: new Set<string>(),
    error: null,
  } as Partial<ReturnType<typeof useChatStore.getState>>);
});

describe("the rail's create-group affordance (spec: Create a group with members)", () => {
  it("shows the Groups header with its + affordance when zero groups exist", async () => {
    await renderRail();

    expect(screen.queryByTestId("group-list")).toBeNull();
    expect(screen.getByTestId("btn-new-group")).not.toBeNull();
  });

  it("hides the Groups section entirely when there are no agents to roster", async () => {
    useChatStore.setState({
      agents: [],
      activeAgentID: null,
    });

    await renderRail();

    expect(screen.queryByTestId("btn-new-group")).toBeNull();
  });

  it("the + opens the create dialog with empty fields and every agent offered", async () => {
    await renderRail();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-new-group"));
    });

    expect(screen.getByTestId("group-create-modal")).not.toBeNull();
    expect(screen.getByTestId("group-create-pane")).not.toBeNull();
    // SAFETY: the pane's fields are the kit Input/Textarea by contract.
    expect(
      (screen.getByTestId("group-name-input") as HTMLInputElement).value
    ).toBe("");
    expect(
      // SAFETY: the title field is the kit Input by contract.
      (screen.getByTestId("group-title-input") as HTMLInputElement).value
    ).toBe("");
    expect(
      // SAFETY: the instructions field is the kit Textarea by contract.
      (screen.getByTestId("group-instructions-input") as HTMLTextAreaElement)
        .value
    ).toBe("");
    // Every agent is offered as an initial-member toggle.
    expect(screen.getByTestId("group-create-option-a1")).not.toBeNull();
    expect(screen.getByTestId("group-create-option-a2")).not.toBeNull();
    // The dialog ends in a footer action row: Cancel and the (gated) Create.
    expect(screen.getByTestId("btn-cancel-group")).not.toBeNull();
    expect(screen.getByTestId("btn-create-group")).not.toBeNull();
  });

  it("Create stays disabled until the name is non-empty", async () => {
    await renderRail();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-new-group"));
    });

    // SAFETY: the confirm control is the kit Button's button by contract.
    expect(
      (screen.getByTestId("btn-create-group") as HTMLButtonElement).disabled
    ).toBe(true);

    await act(async () => {
      fireEvent.change(screen.getByTestId("group-name-input"), {
        target: { value: "research" },
      });
    });

    expect(
      // SAFETY: the confirm control is the kit Button's button by contract.
      (screen.getByTestId("btn-create-group") as HTMLButtonElement).disabled
    ).toBe(false);
  });

  it("creating calls the binding with the fields and roster, opens the new conversation, and lists the row", async () => {
    await renderRail();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-new-group"));
    });
    await act(async () => {
      fireEvent.change(screen.getByTestId("group-name-input"), {
        target: { value: "research" },
      });
      fireEvent.change(screen.getByTestId("group-title-input"), {
        target: { value: "Research" },
      });
      fireEvent.change(screen.getByTestId("group-instructions-input"), {
        target: { value: "study things" },
      });
      fireEvent.click(screen.getByTestId("group-member-toggle-a1"));
      fireEvent.click(screen.getByTestId("group-member-toggle-a2"));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-create-group"));
    });

    await waitFor(() =>
      expect(mockedClient.groups.create).toHaveBeenCalledWith(
        "research",
        "Research",
        "study things",
        ["a1", "a2"]
      )
    );

    // The store selected the created group and the dialog closed.
    await waitFor(() =>
      expect(useChatStore.getState().activeGroupID).toBe("g-new")
    );
    await waitFor(() =>
      expect(screen.queryByTestId("group-create-modal")).toBeNull()
    );

    // The rail lists the created group (untitled, so the name voice).
    await waitFor(() => expect(screen.getByText("research")).not.toBeNull());
    expect(screen.getByTestId("group-list")).not.toBeNull();
  });

  it("a refused create toasts the failure and keeps the form", async () => {
    mockedClient.groups.create.mockRejectedValueOnce(new Error("name taken"));

    await renderRail();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-new-group"));
    });
    await act(async () => {
      fireEvent.change(screen.getByTestId("group-name-input"), {
        target: { value: "research" },
      });
      fireEvent.click(screen.getByTestId("group-member-toggle-a1"));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-create-group"));
    });

    await waitFor(() =>
      expect(
        getToasts().some(
          (t: { kind: string; text: string }): boolean =>
            t.kind === "danger" && t.text.includes("name taken")
        )
      ).toBe(true)
    );

    // The dialog stayed up with the form intact (nothing cleared), and no
    // conversation opened.
    expect(screen.getByTestId("group-create-modal")).not.toBeNull();
    expect(
      // SAFETY: the pane's fields are the kit Input by contract.
      (screen.getByTestId("group-name-input") as HTMLInputElement).value
    ).toBe("research");
    expect(useChatStore.getState().activeGroupID).toBeNull();
  });
});
