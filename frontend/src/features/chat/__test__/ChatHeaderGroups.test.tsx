/**
 * Chat header group tests (add-agent-groups design D9, task 5.2): with a
 * group open the header leads with the group's title-or-name (the same
 * convention the session side pins) and carries the member roster as a
 * stacked avatar row (round 8), green-ringing members with a task in
 * flight; with a session open the header is unchanged (pinned by the
 * untouched ChatHeader suite).
 */
import { screen } from "@testing-library/dom";
import { beforeEach, describe, expect, it } from "vite-plus/test";

import type {
  Agent,
  AgentGroup,
  AgentGroupTask,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { ChatHeader } from "../components/ChatHeader";
import { useChatStore } from "../store";
import { renderUI } from "./test-utils";

function groupRow(overrides: Partial<AgentGroup> = {}): AgentGroup {
  return {
    ID: "g1",
    Name: "research",
    Title: "Research",
    Instructions: "",
    LeadAgentID: null,
    Archived: false,
    Revision: 1,
    CreatedAt: "2026-01-01T00:00:00Z",
    UpdatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

beforeEach(() => {
  // SAFETY: the seed writes exactly the surface the header reads; the cast
  // relabels the partial record for setState's Partial parameter.
  useChatStore.setState({
    agents: [],
    sessions: [],
    groups: [groupRow()],
    activeAgentID: null,
    activeSessionID: null,
    activeGroupID: "g1",
    sidebarCollapsed: false,
    busy: false,
    entries: [],
  } as Partial<ReturnType<typeof useChatStore.getState>>);
});

describe("ChatHeader with a group open", () => {
  it("leads with the group's title", async () => {
    await renderUI(<ChatHeader />);
    expect(screen.getByTestId("chat-header-session-title").textContent).toBe(
      "Research"
    );
  });

  it("falls back to the group's name when untitled", async () => {
    useChatStore.setState({
      groups: [groupRow({ Title: "" })],
    });

    await renderUI(<ChatHeader />);
    expect(screen.getByTestId("chat-header-session-title").textContent).toBe(
      "research"
    );
  });

  it("no longer hosts a group settings opener — it lives in the right bar's group island (round 6)", async () => {
    await renderUI(<ChatHeader />);
    expect(screen.queryByTestId("btn-group-settings")).toBeNull();

    // Even with a group open the header stays title-only + sidebar toggle.
    expect(screen.queryByTestId("btn-group-settings")).toBeNull();
  });
});

describe("ChatHeader group avatar stack (round 8)", () => {
  function agentRow(id: string, name: string): Agent {
    return {
      ID: id,
      Name: name,
      ProviderID: "prov",
      Model: "m",
      Effort: "",
      Slug: id,
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
  }

  function taskRow(overrides: Partial<AgentGroupTask>): AgentGroupTask {
    // SAFETY: the literal covers every required AgentGroupTask field; the
    // spread applies only the row shapes the suite drives.
    return {
      TaskID: "t1",
      GroupID: "g1",
      ParentTaskID: null,
      RootTaskID: "",
      OwnerAgentID: "a1",
      RequestMessageID: "",
      Instruction: "",
      ExpectedResult: "",
      State: "running",
      StopReason: null,
      Revision: 1,
      AssignmentCount: 1,
      Error: null,
      ...overrides,
    } as AgentGroupTask;
  }

  function seedRoster(): void {
    useChatStore.setState({
      agents: [agentRow("a1", "Scout"), agentRow("a2", "Jono")],
      groupMembers: [
        { GroupID: "g1", AgentID: "a1", AddedAt: "2026-01-01T00:00:00Z" },
        { GroupID: "g1", AgentID: "a2", AddedAt: "2026-01-01T00:00:00Z" },
      ],
      groupTasks: [],
    });
  }

  it("carries the member roster as a stacked avatar row", async () => {
    seedRoster();

    await renderUI(<ChatHeader />);

    expect(screen.getByTestId("group-avatar-stack")).not.toBeNull();
    expect(screen.getByTestId("group-avatar-stack-item-a1")).not.toBeNull();
    expect(screen.getByTestId("group-avatar-stack-item-a2")).not.toBeNull();
  });

  it("rings a member green while their task is running", async () => {
    seedRoster();
    useChatStore.setState({
      groupTasks: [
        taskRow({ TaskID: "t1", State: "running", OwnerAgentID: "a1" }),
      ],
    });

    await renderUI(<ChatHeader />);

    const a1 = screen.getByTestId("group-avatar-stack-item-a1");

    expect(a1.dataset.running).toBe("true");
    expect(a1.className).toContain("ring-success");

    // The resting member keeps the surface ring.
    const a2 = screen.getByTestId("group-avatar-stack-item-a2");

    expect(a2.dataset.running).toBe("false");
    expect(a2.className).toContain("ring-surface");
  });

  it("keeps the ring surface-toned once the task reaches a resting state", async () => {
    seedRoster();
    useChatStore.setState({
      groupTasks: [
        taskRow({ TaskID: "t1", State: "completed", OwnerAgentID: "a1" }),
      ],
    });

    await renderUI(<ChatHeader />);

    expect(
      screen.getByTestId("group-avatar-stack-item-a1").className
    ).toContain("ring-surface");
    expect(
      screen.getByTestId("group-avatar-stack-item-a1").className
    ).not.toContain("ring-success");
  });

  it("renders no stack on the session surface", async () => {
    useChatStore.setState({
      agents: [agentRow("a1", "Scout")],
      activeGroupID: null,
      activeAgentID: "a1",
      groupMembers: [],
      groupTasks: [],
    });

    await renderUI(<ChatHeader />);

    expect(screen.queryByTestId("group-avatar-stack")).toBeNull();
  });
});
