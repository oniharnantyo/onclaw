/**
 * Session rail groups section tests (add-agent-groups task 5.1, design D9):
 * the groups section renders beside the agent session groups with the same
 * row grammar — the group's title-or-name, a busy dot while a member turn is
 * in flight, and the active row highlighted — and a row click swaps the
 * conversation surface through the store's openGroup. With no groups the
 * section stays hidden.
 */
import { act } from "react";
import { fireEvent, screen } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  Agent,
  AgentGroup,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../../../lib/client";
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

/** The openGroup spy the rows' click contract is asserted against. */
const openGroup = vi.fn(async (_groupID: string): Promise<void> => undefined);

function makeClient(): Client {
  // SAFETY: the rail renders from the store; the client surface is called
  // only by actions this suite replaces with spies, so the empty stub rides
  // the shared bridge under the named Client contract.
  return mockedSurface<Client, object>({});
}

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

async function renderRail(): Promise<void> {
  await renderUI(<SessionRail />);
}

beforeEach(() => {
  vi.clearAllMocks();
  // SAFETY: the seed writes exactly the surface the rail reads; the cast
  // relabels the partial record for setState's Partial parameter.
  useChatStore.setState({
    agents: [scout],
    sessions: [],
    activeAgentID: "a1",
    activeSessionID: null,
    groups: [groupRow()],
    activeGroupID: null,
    busyGroups: new Set<string>(),
    // The rail's contract is what's under test: the open action is a spy.
    openGroup,
  } as Partial<ReturnType<typeof useChatStore.getState>>);
});

describe("the rail's groups section (add-agent-groups design D9)", () => {
  it("lists the groups beside the agent session groups with the title-or-name voice", async () => {
    await renderRail();

    expect(screen.getByTestId("group-list")).not.toBeNull();
    // The section's rows read the group's Title, falling back to Name.
    expect(screen.getByText("Research")).not.toBeNull();
  });

  it("clicking a group row opens that group's conversation", async () => {
    await renderRail();

    // The rows ride the kit SideRow's data-od-id seam (the rail suites'
    // handle convention).
    // SAFETY: querySelector returns the closest element node or null; the
    // null assertion above establishes the row before the click.
    const row = document.querySelector<HTMLElement>(
      '[data-od-id="chat-group-g1"]'
    );

    expect(row).not.toBeNull();

    await act(async () => {
      // SAFETY: the null assertion above establishes the row, so the click
      // target is the element node the selector matched.
      fireEvent.click(row as HTMLElement);
    });

    expect(openGroup).toHaveBeenCalledWith("g1");
  });

  it("shows the busy dot only for a group with a member turn in flight", async () => {
    useChatStore.setState({
      busyGroups: new Set<string>(["g1"]),
    });

    await renderRail();

    expect(screen.getByTestId("group-busy-g1")).not.toBeNull();

    useChatStore.setState({ busyGroups: new Set<string>() });

    await renderRail();

    expect(screen.queryByTestId("group-busy-g1")).toBeNull();
  });

  it("falls back to the name when the group carries no title", async () => {
    useChatStore.setState({
      groups: [groupRow({ Title: "" })],
    });

    await renderRail();

    expect(screen.getByText("research")).not.toBeNull();
  });

  it("hides the section entirely when no groups exist", async () => {
    useChatStore.setState({ groups: [] });

    await renderRail();

    expect(screen.queryByTestId("group-list")).toBeNull();
  });
});
