/**
 * Group transcript rendering tests (add-agent-groups task 5.2, design D7,
 * chat-view spec "Turn attribution"): the SHARED transcript renderer, fed
 * the group mapping's entries through the same runtime the session surface
 * uses, attributes each assistant turn to its own author — a member's row
 * opens with that member's avatar and name (a STATIC face: no live-avatar
 * state attributes under any motion setting), a coordinator row renders the
 * system-row label without agent attribution, and the user's own rows carry
 * no attribution stub at all. Task state never renders beneath any message
 * (align-group-task-lifecycle design D6, agent-groups spec "State stays off
 * messages"): even an entry stamped with a task id mounts no task-state row
 * under its attribution. Ordinary-session attribution is pinned by the
 * untouched session suites (entries.test.ts, App.test.tsx).
 */
import { screen } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  Agent,
  AgentGroupMessage,
  AgentGroupTask,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { mockedSurface } from "../../chat/__test__/mockedSurface";
import type { Client } from "../../../lib/client";
import { renderUI } from "../../chat/__test__/test-utils";
import { ChatRuntimeProvider } from "../../chat/runtime/ChatRuntimeProvider";
import { ChatThread } from "../../chat/thread/ChatThread";
import { useChatStore } from "../../chat/store";
import { mapGroupMessages } from "../groupEntries";

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

function row(overrides: Partial<AgentGroupMessage>): AgentGroupMessage {
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
    RequestMessageID: "m7",
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

function makeClient(): Client {
  // SAFETY: the thread's runtime adapter only reads the store at render
  // time; the client surface is called on user actions these cases never
  // drive, so the bare stub rides the shared bridge under the named Client
  // contract.
  return mockedSurface<Client, object>({});
}

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

type StorePatch = Partial<ReturnType<typeof useChatStore.getState>>;

function seedGroup(entries: ReturnType<typeof mapGroupMessages>): void {
  // SAFETY: the seed writes exactly the surface the thread reads; the cast
  // relabels the partial record for setState's Partial parameter.
  useChatStore.setState({
    agents: [scout],
    activeAgentID: null,
    activeSessionID: null,
    activeGroupID: "g1",
    entries,
    loadingSession: false,
    busy: false,
    dispatching: false,
    activeTurnID: null,
  } as StorePatch);
}

async function renderThread(): Promise<void> {
  await renderUI(
    <ChatRuntimeProvider>
      <ChatThread />
    </ChatRuntimeProvider>
  );
}

beforeEach(() => {
  // SAFETY: the reset writes exactly the surface the thread reads; the cast
  // relabels the partial record for setState's Partial parameter.
  useChatStore.setState({
    agents: [],
    activeAgentID: null,
    activeSessionID: null,
    activeGroupID: null,
    entries: [],
    busy: false,
    dispatching: false,
    activeTurnID: null,
  } as StorePatch);
});

describe("group attribution in the shared transcript", () => {
  it("opens a member's reply with that member's static avatar and name", async () => {
    seedGroup(
      mapGroupMessages([
        row({
          MessageID: "m1",
          Seq: 1,
          AuthorKind: "agent",
          AuthorAgentID: "a1",
          Payload: JSON.stringify({ text: "scout's answer" }),
        }),
      ])
    );

    await renderThread();

    const attribution = screen.getByTestId("group-attribution");

    expect(attribution.getAttribute("data-author-kind")).toBe("member");
    expect(attribution.textContent).toContain("Scout");
    // The completed body renders through the lazily loaded markdown path;
    // await it instead of asserting inside the first paint.
    expect((await screen.findByText("scout's answer")).textContent).toContain(
      "scout's answer"
    );

    // The face is the STATIC rendering path: no live avatar state rides it
    // under any motion setting — the right bar keeps the only live face.
    expect(attribution.querySelector("[data-avatar-state]")).toBeNull();
  });

  it("renders a coordinator assignment line as a system row without agent attribution", async () => {
    seedGroup(
      mapGroupMessages([
        row({
          MessageID: "m2",
          Seq: 2,
          Kind: "assigned",
          AuthorKind: "coordinator",
          Payload: JSON.stringify({ text: "Assigned to Scout" }),
        }),
      ])
    );

    await renderThread();

    const attribution = screen.getByTestId("group-attribution");

    expect(attribution.getAttribute("data-author-kind")).toBe("coordinator");
    // The system voice: a coordinator label, never an agent's avatar+name.
    expect(attribution.textContent).toContain("Coordinator");
    expect(attribution.textContent).not.toContain("Scout");
    // Same lazy completed-body path: await the text before asserting.
    expect(
      (await screen.findByText("Assigned to Scout")).textContent
    ).toContain("Assigned to Scout");
  });

  it("carries no attribution stub on the user's own rows", async () => {
    seedGroup(mapGroupMessages([row({ MessageID: "m3", Seq: 3 })]));

    await renderThread();

    expect(screen.getByTestId("entry-user")).not.toBeNull();
    expect(screen.queryByTestId("group-attribution")).toBeNull();
  });

  it("renders member and coordinator rows side by side in transcript order", async () => {
    seedGroup(
      mapGroupMessages([
        row({ MessageID: "m4", Seq: 4 }),
        row({
          MessageID: "m5",
          Seq: 5,
          Kind: "assigned",
          AuthorKind: "coordinator",
          Payload: JSON.stringify({ text: "Assigned to Scout" }),
        }),
        row({
          MessageID: "m6",
          Seq: 6,
          AuthorKind: "agent",
          AuthorAgentID: "a1",
          Payload: JSON.stringify({ text: "on it" }),
        }),
      ])
    );

    await renderThread();

    const attributions = screen.getAllByTestId("group-attribution");

    expect(attributions).toHaveLength(2);
    expect(attributions[0].getAttribute("data-author-kind")).toBe(
      "coordinator"
    );
    expect(attributions[1].getAttribute("data-author-kind")).toBe("member");
    expect(attributions[1].textContent).toContain("Scout");
  });

  it("renders no task-state row beneath an attribution even when the entry carries a task id", async () => {
    // SAFETY: the seed writes exactly the surface the (former) task rows
    // read; the cast relabels the partial record for setState's Partial
    // parameter.
    useChatStore.setState({
      groupTasks: [taskRow({})],
    } as StorePatch);

    seedGroup(
      mapGroupMessages([
        row({
          MessageID: "m7",
          Seq: 7,
          Kind: "assigned",
          AuthorKind: "coordinator",
          TaskID: "t1",
          Payload: JSON.stringify({ text: "Assigned to Scout" }),
        }),
      ])
    );

    await renderThread();

    // State stays off messages (align-group-task-lifecycle design D6):
    // live work shows on the activity line, stopped roots on the tail
    // notice — the attribution mounts no task row even for a linked entry.
    expect(screen.queryByTestId("group-task-row-t1")).toBeNull();
    expect(screen.queryByTestId(/group-task-row/)).toBeNull();
    expect(screen.queryByTestId("group-attribution")).not.toBeNull();
  });
});
