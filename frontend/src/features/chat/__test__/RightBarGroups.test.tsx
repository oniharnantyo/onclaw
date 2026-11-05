/**
 * Right bar group island tests (round 6): with a group open the right bar
 * swaps its identity for the group's — title-or-name, member count, the
 * roster's avatars at a glance, and the group settings opener (the entry the
 * header no longer hosts). The opener mounts the same GroupSettingsPane
 * dialog. With a session open the bar is unchanged (the agent identity
 * island, pinned by the untouched RightBar suites).
 */
import { act } from "react";
import { fireEvent, screen } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  Agent,
  AgentGroup,
  AgentGroupTask,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { mockedSurface } from "./mockedSurface";
import type { Client } from "../../../lib/client";
import { renderUI } from "./test-utils";
import { useChatStore } from "../store";
import { RightBar } from "../components/RightBar";

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

type MockFn = ReturnType<typeof vi.fn>;

type MockedClient = Client & {
  groups: {
    list: MockFn;
    members: MockFn;
    tasks: MockFn;
    update: MockFn;
    addMember: MockFn;
    removeMember: MockFn;
    setLead: MockFn;
    delete: MockFn;
  };
};

function makeClient(): MockedClient {
  const c = {
    groups: {
      list: vi.fn(async (): Promise<AgentGroup[]> => [groupRow()]),
      members: vi.fn(async () => []),
      tasks: vi.fn(async (): Promise<AgentGroupTask[]> => []),
      update: vi.fn(async (): Promise<AgentGroup> => groupRow()),
      addMember: vi.fn(async () => undefined),
      removeMember: vi.fn(async () => undefined),
      setLead: vi.fn(async () => undefined),
      delete: vi.fn(async () => undefined),
    },
  };

  // SAFETY: the literal covers exactly the surface the suite drives; this
  // bridge only relabels the object for the compiler.
  return mockedSurface<MockedClient, object>(c);
}

vi.mock("../../../lib/client", () => ({
  client: mockedSurface<MockedClient, object>(makeClient()),
}));

beforeEach(() => {
  vi.clearAllMocks();
  // SAFETY: the seed writes exactly the surface the bar reads; the cast
  // relabels the partial record for setState's Partial parameter.
  useChatStore.setState({
    agents,
    sessions: [],
    groups: [groupRow()],
    groupMembers: [
      { GroupID: "g1", AgentID: "a1", AddedAt: "2026-01-01T00:00:00Z" },
      { GroupID: "g1", AgentID: "a2", AddedAt: "2026-01-01T00:00:00Z" },
    ],
    activeAgentID: null,
    activeSessionID: null,
    activeGroupID: "g1",
    sidebarCollapsed: false,
    panelTabs: [],
  } as Partial<ReturnType<typeof useChatStore.getState>>);
});

describe("RightBar with a group open (round 6)", () => {
  it("swaps the identity island for the group's: name, member count, roster avatars", async () => {
    await renderUI(<RightBar />);

    expect(screen.getByTestId("chat-right-bar")).not.toBeNull();
    expect(screen.getByTestId("sidebar-group-name").textContent).toBe(
      "Research"
    );
    expect(screen.getByTestId("sidebar-group-members").textContent).toBe(
      "2 members"
    );
  });

  it("hosts the group settings opener; activating it swaps the island for the in-place panel (round 7 — never a modal)", async () => {
    await renderUI(<RightBar />);

    expect(screen.queryByTestId("btn-group-settings")).toBeNull(); // the header's old gear is gone
    expect(screen.getByTestId("group-settings-opener")).not.toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByTestId("group-settings-opener"));
    });

    // The form renders INSIDE the bar (no ModalShell) — a modal wider than
    // the sidebar clipped against the window edge.
    expect(screen.getByTestId("group-settings-panel")).not.toBeNull();
    expect(screen.queryByTestId("group-settings-modal")).toBeNull();
    expect(screen.getByTestId("group-name-input")).not.toBeNull();
    // The island's roundel is gone while the panel is up.
    expect(screen.queryByTestId("sidebar-group-name")).toBeNull();

    // Back returns the island.
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-group-settings-back"));
    });

    expect(screen.queryByTestId("group-settings-panel")).toBeNull();
    expect(screen.getByTestId("sidebar-group-name")).not.toBeNull();
  });

  it("falls back to the group's name when untitled", async () => {
    useChatStore.setState({
      groups: [groupRow({ Title: "" })],
    });

    await renderUI(<RightBar />);

    expect(screen.getByTestId("sidebar-group-name").textContent).toBe(
      "research"
    );
  });

  it("renders nothing (no agent island) when the open group is missing from the list", async () => {
    useChatStore.setState({ groups: [] });

    await renderUI(<RightBar />);

    expect(screen.queryByTestId("chat-right-bar")).toBeNull();
  });
});
