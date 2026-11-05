/**
 * Group activity line tests (round 6): the transcript-tail "is my chat
 * working" indicator for the group surface. While the open group has a
 * running task the line names its owner ("Scout is working…"); queued work
 * (the 2-active budget's backlog) reads "Waiting for Jono…"; with nothing in
 * flight — or on the session surface — it renders nothing.
 */
import { screen } from "@testing-library/dom";
import { beforeEach, describe, expect, it } from "vite-plus/test";

import type {
  Agent,
  AgentGroupTask,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { renderUI } from "../../chat/__test__/test-utils";
import { useChatStore } from "../../chat/store";
import { GroupActivity } from "../GroupActivity";

function task(overrides: Partial<AgentGroupTask>): AgentGroupTask {
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

function agent(id: string, name: string): Agent {
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
    PromptsStatus: "ready" as const,
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

beforeEach(() => {
  // SAFETY: the seed writes exactly the surface the line reads; the cast
  // relabels the partial record for setState's Partial parameter.
  useChatStore.setState({
    agents: [agent("a1", "Scout"), agent("a2", "Jono")],
    activeGroupID: "g1",
    groupTasks: [],
  } as Partial<ReturnType<typeof useChatStore.getState>>);
});

describe("GroupActivity (round 6)", () => {
  it("names the running owner while their turn is in flight", async () => {
    useChatStore.setState({
      groupTasks: [task({ TaskID: "t1", State: "running" })],
    });

    await renderUI(<GroupActivity />);

    const line = screen.getByTestId("group-activity-line");

    expect(line.textContent).toContain("Scout is working…");
  });

  it("reads 'Waiting for' while work sits in the 2-active queue", async () => {
    useChatStore.setState({
      groupTasks: [task({ TaskID: "t2", State: "queued" })],
    });

    await renderUI(<GroupActivity />);

    expect(screen.getByTestId("group-activity-line").textContent).toContain(
      "Waiting for Scout…"
    );
  });

  it("joins a running and a queued clause when both exist", async () => {
    useChatStore.setState({
      groupTasks: [
        task({ TaskID: "t1", State: "running", OwnerAgentID: "a1" }),
        task({ TaskID: "t2", State: "queued", OwnerAgentID: "a2" }),
      ],
    });

    await renderUI(<GroupActivity />);

    const text = screen.getByTestId("group-activity-line").textContent;

    expect(text).toContain("Scout is working…");
    expect(text).toContain("Waiting for Jono…");
  });

  it("falls back to 'a member' when the owner left the roster", async () => {
    useChatStore.setState({
      groupTasks: [
        task({ TaskID: "t1", State: "running", OwnerAgentID: "gone" }),
      ],
    });

    await renderUI(<GroupActivity />);

    expect(screen.getByTestId("group-activity-line").textContent).toContain(
      "a member is working…"
    );
  });

  it("renders nothing once every task reached a resting state", async () => {
    useChatStore.setState({
      groupTasks: [
        task({ TaskID: "t1", State: "waiting" }),
        task({ TaskID: "t2", State: "completed" }),
        task({ TaskID: "t3", State: "paused" }),
      ],
    });

    await renderUI(<GroupActivity />);

    expect(screen.queryByTestId("group-activity-line")).toBeNull();
  });

  it("renders nothing on the session surface even with tasks in flight", async () => {
    useChatStore.setState({
      activeGroupID: null,
      groupTasks: [task({ TaskID: "t1", State: "running" })],
    });

    await renderUI(<GroupActivity />);

    expect(screen.queryByTestId("group-activity-line")).toBeNull();
  });
});
