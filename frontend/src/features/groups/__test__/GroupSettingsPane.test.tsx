/**
 * Group settings pane tests (add-agent-groups task 5.5, spec agent-groups
 * "Group surfaces in the chat menu"): the pane edits the group's name, title,
 * and instructions through UpdateGroup; lists the members with add, remove,
 * and the crown lead control (exactly one lead — the current lead's crown is
 * inert and its row carries the Lead badge); and deletes the group behind the
 * confirm dialog. Member and lead mutations hit the groups bindings and
 * refresh the store's group state. The create mode keeps the roster in
 * submission order — the crown marks the prospective lead (the first member,
 * which the backend leads with) and moving it reorders the submitted roster —
 * behind a name search, with the actions in a Cancel + Create footer.
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  Agent,
  AgentGroup,
  AgentGroupTask,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { mockedSurface } from "../../chat/__test__/mockedSurface";
import type { Client } from "../../../lib/client";
import { renderUI } from "../../chat/__test__/test-utils";
import { useChatStore } from "../../chat/store";
import { GroupSettingsPane } from "../GroupSettingsPane";

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
  {
    ID: "a3",
    Name: "Iris",
    ProviderID: "prov",
    Model: "m",
    Effort: "",
    Slug: "iris",
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

import { client } from "../../../lib/client";

// SAFETY: the client module is mocked with makeClient(), so its runtime
// shape is exactly this mock object.
const mockedClient = client as MockedClient;

async function renderPane(
  group: AgentGroup = groupRow()
): Promise<{ onClosed: MockFn }> {
  const onClosed = vi.fn();

  await renderUI(<GroupSettingsPane group={group} onClosed={onClosed} />);

  return { onClosed };
}

async function renderCreatePane(): Promise<{ onClosed: MockFn }> {
  const onClosed = vi.fn();

  await renderUI(<GroupSettingsPane create onClosed={onClosed} />);

  return { onClosed };
}

beforeEach(() => {
  vi.clearAllMocks();
  useChatStore.setState({
    agents,
    activeGroupID: "g1",
    activeSessionID: null,
    groups: [groupRow()],
    groupMembers: [
      { GroupID: "g1", AgentID: "a1", AddedAt: "2026-01-01T00:00:00Z" },
      { GroupID: "g1", AgentID: "a2", AddedAt: "2026-01-01T00:00:00Z" },
    ],
    groupTasks: [],
    busyGroups: new Set<string>(),
  });
});

describe("GroupSettingsPane (add-agent-groups task 5.5)", () => {
  it("panel variant (round 7): renders the form in place — no modal, back affordance, Save still pushes", async () => {
    const onClosed = vi.fn();

    await renderUI(
      <GroupSettingsPane
        group={groupRow()}
        onClosed={onClosed}
        variant="panel"
      />
    );

    // In place: the panel shell, no ModalShell chrome.
    expect(screen.getByTestId("group-settings-panel")).not.toBeNull();
    expect(screen.queryByTestId("group-settings-modal")).toBeNull();
    // SAFETY: the pane's fields are the kit Input controls by contract.
    expect(
      (screen.getByTestId("group-name-input") as HTMLInputElement).value
    ).toBe("research");

    // The back affordance closes the hosting surface.
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-group-settings-back"));
    });
    expect(onClosed).toHaveBeenCalled();
  });

  it("hydrates the editable fields from the open row", async () => {
    await renderPane();

    const nameInput = screen.getByTestId("group-name-input");
    const titleInput = screen.getByTestId("group-title-input");
    const instructionsInput = screen.getByTestId("group-instructions-input");

    // SAFETY: the pane's fields are the kit Input controls by contract.
    expect((nameInput as HTMLInputElement).value).toBe("research");
    // SAFETY: as above — the title field is the kit Input.
    expect((titleInput as HTMLInputElement).value).toBe("Research");
    // SAFETY: the instructions field is the kit Textarea by contract.
    expect((instructionsInput as HTMLTextAreaElement).value).toBe(
      "study things"
    );
  });

  it("Save pushes the fields through UpdateGroup and refreshes the group state", async () => {
    await renderPane();

    await act(async () => {
      fireEvent.change(screen.getByTestId("group-title-input"), {
        target: { value: "Research v2" },
      });
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-group"));
    });

    await waitFor(() =>
      expect(mockedClient.groups.update).toHaveBeenCalledWith(
        "g1",
        "research",
        "Research v2",
        "study things"
      )
    );
    // The refresh pass reads the list (and the open group's roster).
    await waitFor(() => expect(mockedClient.groups.list).toHaveBeenCalled());
  });

  it("a blank name refuses to save locally", async () => {
    await renderPane();

    await act(async () => {
      fireEvent.change(screen.getByTestId("group-name-input"), {
        target: { value: "" },
      });
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-group"));
    });

    expect(mockedClient.groups.update).not.toHaveBeenCalled();
    expect(screen.getByText("Name is required")).not.toBeNull();
  });

  it("the crown assigns the lead and the current lead's crown is inert", async () => {
    await renderPane();

    const leadCrown = screen.getByTestId("group-lead-a1");

    // SAFETY: the crown is the kit IconButton's button by contract.
    expect((leadCrown as HTMLButtonElement).disabled).toBe(true);

    await act(async () => {
      fireEvent.click(screen.getByTestId("group-lead-a2"));
    });

    await waitFor(() =>
      expect(mockedClient.groups.setLead).toHaveBeenCalledWith("g1", "a2")
    );
  });

  it("the current lead's row carries the persistent lead badge", async () => {
    await renderPane();

    expect(screen.getByTestId("group-lead-badge-a1")).not.toBeNull();
    expect(screen.queryByTestId("group-lead-badge-a2")).toBeNull();
  });

  it("removes a member through the binding", async () => {
    await renderPane();

    await act(async () => {
      fireEvent.click(screen.getByTestId("group-member-remove-a2"));
    });

    await waitFor(() =>
      expect(mockedClient.groups.removeMember).toHaveBeenCalledWith("g1", "a2")
    );
  });

  it("offers the not-yet-member agents and adds one", async () => {
    await renderPane();

    // Iris is not a member; the add list offers her, not Runner.
    expect(screen.getByTestId("group-add-option-a3")).not.toBeNull();
    expect(screen.queryByTestId("group-add-option-a2")).toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByTestId("group-member-add-a3"));
    });

    await waitFor(() =>
      expect(mockedClient.groups.addMember).toHaveBeenCalledWith("g1", "a3")
    );
  });

  it("deletes the group behind the confirm dialog and closes the pane", async () => {
    const { onClosed } = await renderPane();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-delete-group"));
    });

    expect(mockedClient.groups.delete).not.toHaveBeenCalled();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-confirm-delete-group"));
    });

    await waitFor(() =>
      expect(mockedClient.groups.delete).toHaveBeenCalledWith("g1")
    );
    expect(onClosed).toHaveBeenCalled();
  });
});

describe("GroupSettingsPane create mode (spec: Create a group with members)", () => {
  it("ends in a footer with Cancel and a name-gated Create", async () => {
    const { onClosed } = await renderCreatePane();

    // SAFETY: the footer actions are the kit Button's buttons by contract.
    expect(
      (screen.getByTestId("btn-create-group") as HTMLButtonElement).disabled
    ).toBe(true);
    expect(screen.getByTestId("btn-cancel-group")).not.toBeNull();

    await act(async () => {
      fireEvent.change(screen.getByTestId("group-name-input"), {
        target: { value: "research" },
      });
    });
    // SAFETY: the create button is the kit Button's button by contract.
    expect(
      (screen.getByTestId("btn-create-group") as HTMLButtonElement).disabled
    ).toBe(false);

    // Cancel is the create flow's dismiss exit — it closes the dialog.
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-cancel-group"));
    });
    expect(onClosed).toHaveBeenCalled();
  });

  it("crowns the first-selected member and moving the crown reorders the submitted roster", async () => {
    // The pane drives the store's createGroup; swap in a recorder so the
    // submitted memberAgentIDs order is assertable (the backend makes the
    // FIRST id the lead).
    const createGroup = vi.fn(async (): Promise<AgentGroup | null> =>
      groupRow()
    );

    useChatStore.setState({ createGroup });

    await renderCreatePane();

    await act(async () => {
      fireEvent.change(screen.getByTestId("group-name-input"), {
        target: { value: "research" },
      });
      fireEvent.click(screen.getByTestId("group-member-toggle-a2"));
      fireEvent.click(screen.getByTestId("group-member-toggle-a1"));
    });

    // Runner was picked first, so the crown marks Runner and is inert;
    // Scout's crown is the make-lead affordance.
    // SAFETY: the crown is the kit IconButton's button by contract.
    expect(
      (screen.getByTestId("group-create-lead-a2") as HTMLButtonElement).disabled
    ).toBe(true);
    // SAFETY: as above — Scout's crown is the kit IconButton's button.
    expect(
      (screen.getByTestId("group-create-lead-a1") as HTMLButtonElement).disabled
    ).toBe(false);

    await act(async () => {
      fireEvent.click(screen.getByTestId("group-create-lead-a1"));
    });

    // The marker moved with the reorder.
    // SAFETY: the crown is the kit IconButton's button by contract.
    expect(
      (screen.getByTestId("group-create-lead-a1") as HTMLButtonElement).disabled
    ).toBe(true);
    // SAFETY: as above — Runner's crown is the kit IconButton's button.
    expect(
      (screen.getByTestId("group-create-lead-a2") as HTMLButtonElement).disabled
    ).toBe(false);

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-create-group"));
    });

    await waitFor(() =>
      expect(createGroup).toHaveBeenCalledWith("research", "", "", ["a1", "a2"])
    );
  });

  it("the search filters agent rows, shows a no-match state, and survives the toggle interplay", async () => {
    await renderCreatePane();

    await act(async () => {
      fireEvent.change(screen.getByTestId("group-member-search"), {
        target: { value: "run" },
      });
    });
    expect(screen.getByTestId("group-create-option-a2")).not.toBeNull();
    expect(screen.queryByTestId("group-create-option-a1")).toBeNull();
    expect(screen.queryByTestId("group-create-option-a3")).toBeNull();

    await act(async () => {
      fireEvent.change(screen.getByTestId("group-member-search"), {
        target: { value: "zzz" },
      });
    });
    expect(screen.getByTestId("group-member-empty").textContent).toContain(
      "zzz"
    );

    // Clearing restores every agent, and picking through a search still
    // crowns the first selection.
    await act(async () => {
      fireEvent.change(screen.getByTestId("group-member-search"), {
        target: { value: "iris" },
      });
      fireEvent.click(screen.getByTestId("group-member-toggle-a3"));
      fireEvent.change(screen.getByTestId("group-member-search"), {
        target: { value: "" },
      });
    });
    expect(screen.getByTestId("group-create-option-a1")).not.toBeNull();
    // SAFETY: the crown is the kit IconButton's button by contract.
    expect(
      (screen.getByTestId("group-create-lead-a3") as HTMLButtonElement).disabled
    ).toBe(true);
  });
});
