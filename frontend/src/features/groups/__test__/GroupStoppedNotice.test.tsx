/**
 * Group stopped notice tests (align-group-task-lifecycle design D6, spec
 * agent-groups "State stays off messages"): the transcript tail renders one
 * notice per stopped ROOT run — a paused or failed task whose RootTaskID is
 * its own — with the stop reason (the failure's error for a failed run) and
 * the controls the state admits: Continue on every stopped root (paused or
 * failed), Reassign on a paused root only (reassignment needs an open
 * task), Cancel behind the confirm dialog on both. A non-root stopped task, a
 * group with no stopped roots, and the session surface render nothing.
 * Every control calls the corresponding groups binding and refreshes the
 * open group's tasks; a refusal surfaces as a danger toast.
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  Agent,
  AgentGroupMember,
  AgentGroupTask,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { mockedSurface } from "../../chat/__test__/mockedSurface";
import type { Client } from "../../../lib/client";
import { renderUI } from "../../chat/__test__/test-utils";
import { getToasts, type Toast } from "../../../lib/toasts";
import { useChatStore } from "../../chat/store";
import { GroupStoppedNotice } from "../GroupStoppedNotice";

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

const runner: Agent = { ...scout, ID: "a2", Name: "Runner", Slug: "runner" };

const members: AgentGroupMember[] = [
  { GroupID: "g1", AgentID: "a1", AddedAt: "2026-01-01T00:00:00Z" },
  { GroupID: "g1", AgentID: "a2", AddedAt: "2026-01-01T00:00:00Z" },
];

function taskRow(overrides: Partial<AgentGroupTask>): AgentGroupTask {
  return {
    TaskID: "t1",
    GroupID: "g1",
    ParentTaskID: null,
    RootTaskID: "t1",
    OwnerAgentID: "a1",
    RequestMessageID: "m1",
    Instruction: "summarize",
    ExpectedResult: "",
    State: "running",
    StopReason: null,
    Revision: 1,
    AssignmentCount: 0,
    Error: null,
    ...overrides,
  };
}

type MockFn = ReturnType<typeof vi.fn>;

type MockedClient = Client & {
  groups: {
    tasks: MockFn;
    stopTask: MockFn;
    continueTask: MockFn;
    reassignTask: MockFn;
    cancelTask: MockFn;
  };
};

function makeClient(): MockedClient {
  const c = {
    groups: {
      tasks: vi.fn(async (): Promise<AgentGroupTask[]> => []),
      stopTask: vi.fn(async () => undefined),
      continueTask: vi.fn(async () => undefined),
      reassignTask: vi.fn(async () => undefined),
      cancelTask: vi.fn(async () => undefined),
    },
  };

  // SAFETY: the literal covers exactly the surface the suite drives; this
  // bridge only relabels the object for the compiler.
  return mockedSurface<MockedClient, object>(c);
}

vi.mock("../../../lib/client", () => ({
  client: mockedSurface<MockedClient, object>(makeClient()),
}));

import { client } from "../../../lib/client";

// SAFETY: the client module is mocked with makeClient(), so its runtime
// shape is exactly this mock object.
const mockedClient = client as MockedClient;

function seed(tasks: AgentGroupTask[]): void {
  useChatStore.setState({
    agents: [scout, runner],
    activeGroupID: "g1",
    activeSessionID: null,
    groupMembers: members,
    groupTasks: tasks,
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  useChatStore.setState({
    agents: [],
    activeGroupID: null,
    activeSessionID: null,
    groupMembers: [],
    groupTasks: [],
  });
});

describe("GroupStoppedNotice (the transcript tail's stopped runs)", () => {
  it("renders exactly one notice per stopped root with its reason and controls", async () => {
    seed([taskRow({ State: "paused", StopReason: "waiting on credentials" })]);
    await renderUI(<GroupStoppedNotice />);

    const notices = screen.getAllByTestId("group-stopped-notice-t1");

    expect(notices).toHaveLength(1);
    expect(notices[0].textContent).toContain("Paused");
    expect(
      screen.getByTestId("group-task-stop-reason-t1").textContent
    ).toContain("waiting on credentials");
    expect(screen.getByTestId("group-task-continue-t1")).not.toBeNull();
    expect(screen.getByTestId("group-task-reassign-t1")).not.toBeNull();
    expect(screen.getByTestId("group-task-cancel-t1")).not.toBeNull();
  });

  it("renders a failed root with its error, Continue, and Cancel but no Reassign", async () => {
    seed([taskRow({ State: "failed", Error: "provider quota exhausted" })]);
    await renderUI(<GroupStoppedNotice />);

    const notice = screen.getByTestId("group-stopped-notice-t1");

    expect(notice.textContent).toContain("Failed");
    expect(
      screen.getByTestId("group-task-stop-reason-t1").textContent
    ).toContain("provider quota exhausted");
    // A failed task is stopped, so the spec's continue covers it too;
    // Reassign stays paused-only (reassignment needs an open task).
    expect(screen.getByTestId("group-task-continue-t1")).not.toBeNull();
    expect(screen.queryByTestId("group-task-reassign-t1")).toBeNull();
    expect(screen.getByTestId("group-task-cancel-t1")).not.toBeNull();
  });

  it("renders nothing for a stopped NON-ROOT task", async () => {
    seed([
      taskRow({
        TaskID: "t2",
        RootTaskID: "t1",
        ParentTaskID: "t1",
        State: "paused",
        StopReason: "waiting on credentials",
      }),
    ]);
    await renderUI(<GroupStoppedNotice />);

    expect(screen.queryByTestId("group-stopped-notice-t2")).toBeNull();
  });

  it("renders nothing while the open group has no stopped roots", async () => {
    seed([
      taskRow({ State: "running" }),
      taskRow({ TaskID: "t3", RootTaskID: "t3", State: "completed" }),
    ]);
    await renderUI(<GroupStoppedNotice />);

    expect(screen.queryByTestId("group-stopped-notice-t1")).toBeNull();
    expect(screen.queryByTestId("group-stopped-notice-t3")).toBeNull();
  });

  it("renders nothing on the session surface", async () => {
    useChatStore.setState({
      activeGroupID: null,
      groupTasks: [taskRow({ State: "paused", StopReason: "paused" })],
    });
    await renderUI(<GroupStoppedNotice />);

    expect(screen.queryByTestId("group-stopped-notice-t1")).toBeNull();
  });

  it("Continue re-dispatches the paused root with the continue message", async () => {
    seed([taskRow({ State: "paused", StopReason: "waiting on credentials" })]);
    await renderUI(<GroupStoppedNotice />);

    await act(async () => {
      fireEvent.click(screen.getByTestId("group-task-continue-t1"));
    });

    await waitFor(() =>
      expect(mockedClient.groups.continueTask).toHaveBeenCalledWith(
        "t1",
        "Continue"
      )
    );
    await waitFor(() => expect(mockedClient.groups.tasks).toHaveBeenCalled());
  });

  it("Continue re-dispatches a failed root with the continue message", async () => {
    seed([taskRow({ State: "failed", Error: "provider quota exhausted" })]);
    await renderUI(<GroupStoppedNotice />);

    await act(async () => {
      fireEvent.click(screen.getByTestId("group-task-continue-t1"));
    });

    await waitFor(() =>
      expect(mockedClient.groups.continueTask).toHaveBeenCalledWith(
        "t1",
        "Continue"
      )
    );
    await waitFor(() => expect(mockedClient.groups.tasks).toHaveBeenCalled());
  });

  it("Cancel ends the root chain behind the confirm dialog", async () => {
    seed([taskRow({ State: "paused", StopReason: "waiting on credentials" })]);
    await renderUI(<GroupStoppedNotice />);

    await act(async () => {
      fireEvent.click(screen.getByTestId("group-task-cancel-t1"));
    });

    // The dialog guards the action (the codebase's confirm precedent).
    expect(mockedClient.groups.cancelTask).not.toHaveBeenCalled();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-confirm-cancel-task-t1"));
    });

    await waitFor(() =>
      expect(mockedClient.groups.cancelTask).toHaveBeenCalledWith("t1")
    );
  });

  it("Reassign opens the member picker and moves the task to the chosen member", async () => {
    seed([taskRow({ State: "paused", StopReason: "waiting on credentials" })]);
    await renderUI(<GroupStoppedNotice />);

    await act(async () => {
      fireEvent.click(screen.getByTestId("group-task-reassign-t1"));
    });

    const modal = screen.getByTestId("group-task-reassign-modal");

    expect(modal.textContent).toContain("Scout");
    expect(modal.textContent).toContain("Runner");

    // The current owner is inert.
    const ownerRow = screen.getByTestId("group-task-reassign-to-a1");

    // SAFETY: the picker rows are buttons by contract.
    expect((ownerRow as HTMLButtonElement).disabled).toBe(true);

    await act(async () => {
      fireEvent.click(screen.getByTestId("group-task-reassign-to-a2"));
    });

    await waitFor(() =>
      expect(mockedClient.groups.reassignTask).toHaveBeenCalledWith("t1", "a2")
    );
  });

  it("toasts a refused control instead of dying", async () => {
    seed([taskRow({ State: "paused", StopReason: "waiting on credentials" })]);
    await renderUI(<GroupStoppedNotice />);

    mockedClient.groups.cancelTask.mockRejectedValueOnce(
      new Error("only a stopped task cancels")
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId("group-task-cancel-t1"));
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-confirm-cancel-task-t1"));
    });

    // The refusal lands in the toast store (the rail's delete-refusal
    // idiom — the toast DOM mounts at the app root, not here).
    await waitFor(() =>
      expect(
        getToasts().some((t: Toast): boolean =>
          t.text.includes("only a stopped task cancels")
        )
      ).toBe(true)
    );
  });
});
