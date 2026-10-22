/**
 * Component tests for the agents feature (tasks 6.1–6.3, spec agent-management master-detail).
 * The lib client is mocked wholesale; components mount through react-dom/client with
 * @testing-library/dom queries because @testing-library/react is not a project dependency.
 */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vite-plus/test";

import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  CreateAgentInput,
  ModelCapability,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client } from "../../../lib/client";
import { useNavStore } from "../../../stores/nav";

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

// Import after the mock is registered.
import AgentsView from "../index";
import { client } from "../../../lib/client";
import { avatarProps } from "../../../lib/avatar";

// SAFETY: the client module is mocked with makeClient(), so its runtime shape
// is exactly this mock object.
const mockedClient = client as MockedClient;

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    ID: "a1",
    Name: "Scout",
    Slug: "scout",
    Description: "",
    Brief: "A patient scout.",
    Avatar: null,
    SubagentsEnabled: true,
    PromptsStatus: "ready",
    PromptsError: "",
    ProviderID: "openai",
    Model: "gpt-test",
    Effort: "",
    Temperature: 0.7,
    MaxTokens: 0,
    ContextWindow: 0,
    Tools: null,
    Skills: null,
    MCPServers: null,
    CreatedAt: "2026-01-01T00:00:00Z",
    UpdatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

let agents: Agent[];

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  agents: {
    list: MockFn;
    get: MockFn;
    create: MockFn;
    update: MockFn;
    delete: MockFn;
    getPromptDocuments: MockFn;
    savePromptDocument: MockFn;
    regeneratePrompts: MockFn;
  };
  settings: {
    providers: MockFn;
    providerModels: MockFn;
    skills: MockFn;
    tools: MockFn;
    mcpServers: MockFn;
    hooks: MockFn;
    agentHooks: MockFn;
    assignHook: MockFn;
    unassignHook: MockFn;
  };
};

function makeClient(): MockedClient {
  const c = {
    agents: {
      list: vi.fn(async () => agents),
      get: vi.fn(async () => makeAgent()),
      create: vi.fn(async (input: CreateAgentInput) =>
        makeAgent({ ID: "a-new", Name: input.Name, Slug: input.Slug })
      ),
      update: vi.fn(async (a: Agent) => a),
      delete: vi.fn(async () => undefined),
      getPromptDocuments: vi.fn(async () => ({
        Identity: "",
        IdentityExists: false,
        Soul: "",
        SoulExists: false,
        Bootstrap: "",
        BootstrapExists: false,
      })),
      savePromptDocument: vi.fn(async () => undefined),
      regeneratePrompts: vi.fn(async () => undefined),
    },
    settings: {
      providers: vi.fn(async () => [
        { ID: "openai", Name: "OpenAI", Enabled: true },
        { ID: "anthropic", Name: "Anthropic", Enabled: true },
      ]),
      providerModels: vi.fn(async (): Promise<ModelCapability[]> => [
        {
          ID: "gpt-test",
          ContextWindow: 128000,
          Efforts: [],
          ImageInput: false,
          PDFInput: false,
          VideoInput: false,
        },
        {
          ID: "gpt-other",
          ContextWindow: 128000,
          Efforts: [],
          ImageInput: false,
          PDFInput: false,
          VideoInput: false,
        },
      ]),
      skills: vi.fn(async () => [
        { Name: "search", Description: "Web search", Enabled: true },
        { Name: "off-skill", Description: "", Enabled: false },
      ]),
      tools: vi.fn(async () => [
        { Name: "browser", Description: "Browse", Enabled: true },
        { Name: "shell", Description: "Run shell", Enabled: true },
      ]),
      mcpServers: vi.fn(async () => [
        { ID: "mcp1", Name: "Files", Enabled: true },
      ]),
      hooks: vi.fn(async () => []),
      agentHooks: vi.fn(async () => []),
      assignHook: vi.fn(async () => undefined),
      unassignHook: vi.fn(async () => undefined),
    },
  };

  // exposes the vi.fn members to the suite.
  // SAFETY: makeClient() builds exactly this mock shape; the relabel
  return c as MockedClient;
}

const roots: Root[] = [];

async function renderView(): Promise<void> {
  // boolean.
  // SAFETY: globalThis always exists and the flag is an optional
  (
    globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
  ).IS_REACT_ACT_ENVIRONMENT = true;
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  roots.push(root);
  await act(async () => {
    root.render(<AgentsView />);
  });
}

/**
 * Drives the create wizard from a fresh modal to the Capabilities step:
 * name "Radar" (slug auto-suggested), a brief, provider openai, model
 * gpt-test.
 */
async function walkWizardToCapabilities(): Promise<void> {
  // HTMLInputElement by construction.
  // SAFETY: the query targets the wizard's name field, an
  const nameInput = screen.getByTestId(
    "input-create-agent-name"
  ) as HTMLInputElement;

  await act(async () => {
    fireEvent.change(nameInput, { target: { value: "Radar" } });
  });
  await act(async () => {
    fireEvent.change(
      // HTMLInputElement by construction.
      // SAFETY: the query targets the wizard's brief field, an
      screen.getByTestId("input-create-agent-brief") as HTMLInputElement,
      { target: { value: "Sweeps the airspace." } }
    );
  });
  await act(async () => {
    fireEvent.click(screen.getByTestId("btn-wizard-next"));
  });

  // HTMLSelectElement by construction.
  // SAFETY: the query targets the wizard's provider select, an
  const providerSelect = screen.getByTestId(
    "select-create-provider"
  ) as HTMLSelectElement;

  await act(async () => {
    fireEvent.change(providerSelect, { target: { value: "openai" } });
  });
  await waitFor(() => {
    // HTMLSelectElement by construction.
    // SAFETY: the query targets the wizard's model select, an
    const modelSelect = screen.getByTestId(
      "select-create-model"
    ) as HTMLSelectElement;

    expect(
      Array.from(modelSelect.options).some(
        (o: HTMLOptionElement): boolean => o.value === "gpt-test"
      )
    ).toBe(true);
  });
  await act(async () => {
    fireEvent.change(
      // HTMLSelectElement by construction.
      // SAFETY: the query targets the wizard's model select, an
      screen.getByTestId("select-create-model") as HTMLSelectElement,
      { target: { value: "gpt-test" } }
    );
  });
  await act(async () => {
    fireEvent.click(screen.getByTestId("btn-wizard-next"));
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  agents = [makeAgent()];
  useNavStore.setState({ menu: "agents", chatTarget: null });
});

afterEach(() => {
  while (roots.length) {
    const root = roots.pop()!;
    act(() => {
      root.unmount();
    });
  }

  document.body.innerHTML = "";
});

describe("AgentsView", () => {
  it("renders each agent in the list with name, model line, and deterministic avatar", async () => {
    await renderView();
    const item = screen.getByTestId("agent-item-a1");
    expect(item).toBeTruthy();
    expect(item.textContent).toContain("Scout");
    expect(screen.getByTestId("agent-item-model-a1").textContent).toBe(
      "gpt-test"
    );
    expect(avatarProps("a1").seed).toBe("a1");
  });

  it("never renders the raw agent ID on the list entry", async () => {
    const hash =
      "b7f3a1c9e2d4f5061728394a5b6c7d8e9f0a1b2c3d4e5f60718293a4b5c6d7e";

    agents = [makeAgent({ ID: hash, Description: "" })];
    await renderView();

    const item = screen.getByTestId(`agent-item-${hash}`);
    expect(item.textContent).not.toContain(hash);
    expect(screen.getByTestId(`agent-item-model-${hash}`).textContent).toBe(
      "gpt-test"
    );
  });

  it("auto-picks the first agent on load and shows its configuration in the detail pane", async () => {
    await renderView();

    expect(screen.getByTestId("agent-config-pane")).toBeTruthy();
    // Detail pane header displays the selected agent
    expect(screen.getByTestId("config-readonly-slug").textContent).toBe(
      "scout"
    );
    // HTMLInputElement by construction.
    // SAFETY: the query targets the config pane's name field, an

    const nameInput = screen.getByTestId(
      "input-config-name"
    ) as HTMLInputElement;

    expect(nameInput.value).toBe("Scout");
  });

  it("clicking an agent in the list selects it and updates the detail pane", async () => {
    agents = [
      makeAgent({ ID: "a1", Name: "Scout", Slug: "scout" }),
      makeAgent({ ID: "a2", Name: "Sentinel", Slug: "sentinel" }),
    ];
    await renderView();

    // Initially Scout is auto-picked
    expect(screen.getByTestId("config-readonly-slug").textContent).toBe(
      "scout"
    );

    // Click Sentinel in the list
    await act(async () => {
      fireEvent.click(screen.getByTestId("agent-item-a2"));
    });

    // Detail pane now shows Sentinel
    expect(screen.getByTestId("config-readonly-slug").textContent).toBe(
      "sentinel"
    );
    // HTMLInputElement by construction.
    // SAFETY: the query targets the config pane's name field, an

    const nameInput = screen.getByTestId(
      "input-config-name"
    ) as HTMLInputElement;

    expect(nameInput.value).toBe("Sentinel");
  });

  it("renders empty state when no agents exist", async () => {
    agents = [];
    await renderView();
    expect(screen.getByTestId("agents-empty")).toBeTruthy();
    expect(screen.getByText("No agents yet")).toBeTruthy();
  });

  it("shows the failed indicator and dispatches retry on the entry", async () => {
    agents = [
      makeAgent({ PromptsStatus: "failed", PromptsError: "provider offline" }),
    ];
    let resolveRegen: (() => void) | null = null;
    mockedClient.agents.regeneratePrompts.mockImplementationOnce(
      (): Promise<void> =>
        new Promise<void>((resolve: () => void) => {
          resolveRegen = resolve;
        })
    );
    await renderView();

    const failed = screen.getByTestId("agent-item-failed-a1");
    expect(failed.textContent).toContain("provider offline");

    await act(async () => {
      fireEvent.click(screen.getByTestId("agent-retry-a1"));
    });
    expect(mockedClient.agents.regeneratePrompts).toHaveBeenCalledWith(
      "a1",
      ""
    );
    // Flips to generating state immediately
    expect(screen.getByTestId("agent-item-generating-a1")).toBeTruthy();
    expect(screen.queryByTestId("agent-item-failed-a1")).toBeNull();

    // Settled row replaces it when call resolves
    await act(async () => {
      if (resolveRegen) resolveRegen();
    });
    await waitFor(() =>
      expect(screen.queryByTestId("agent-item-generating-a1")).toBeNull()
    );
    expect(mockedClient.agents.get).toHaveBeenCalledWith("a1");
  });

  it("opens the agent chat via the chat affordance", async () => {
    await renderView();
    await act(async () => {
      fireEvent.click(screen.getByTestId("agent-chat-a1"));
    });
    const nav = useNavStore.getState();
    expect(nav.menu).toBe("chat");
    expect(nav.chatTarget).toEqual({ agentID: "a1" });
  });

  it("reflects a saved model change on the list entry in place", async () => {
    await renderView();

    // The detail pane is directly open for the selected agent
    expect(screen.getByTestId("agent-config-pane")).toBeTruthy();

    await act(async () => {
      fireEvent.click(screen.getByTestId("config-tab-model"));
    });

    // HTMLSelectElement by construction.
    // SAFETY: the query targets the config pane's provider select, an
    const providerSelect = screen.getByTestId(
      "select-config-provider"
    ) as HTMLSelectElement;

    await act(async () => {
      fireEvent.change(providerSelect, { target: { value: "anthropic" } });
    });
    await waitFor(() =>
      expect(mockedClient.settings.providerModels).toHaveBeenCalledWith(
        "anthropic"
      )
    );
    await waitFor(() =>
      expect(screen.getByTestId("select-config-model")).toBeTruthy()
    );

    // HTMLSelectElement by construction.
    // SAFETY: the query targets the config pane's model select, an
    const modelSelect = screen.getByTestId(
      "select-config-model"
    ) as HTMLSelectElement;

    await act(async () => {
      fireEvent.change(modelSelect, { target: { value: "gpt-other" } });
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-agent"));
    });

    // Detail pane remains mounted with updated agent
    expect(screen.getByTestId("agent-config-pane")).toBeTruthy();
    // List entry reflects the updated model
    expect(screen.getByTestId("agent-item-model-a1").textContent).toBe(
      "gpt-other"
    );
    expect(mockedClient.agents.update).toHaveBeenCalledTimes(1);
  });

  it("validates an empty name at the wizard's Identity step", async () => {
    await renderView();
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-new-agent"));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-wizard-next"));
    });
    expect(
      screen.getByTestId("field-error-create-agent-name").textContent
    ).toContain("required");
    expect(mockedClient.agents.create).not.toHaveBeenCalled();
  });

  it("creates an agent through the wizard which appears in the list", async () => {
    await renderView();
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-new-agent"));
    });

    await walkWizardToCapabilities();
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-create-agent"));
    });

    await waitFor(() =>
      expect(screen.queryByTestId("create-agent-modal")).toBeNull()
    );
    expect(screen.getByTestId("agent-item-a-new")).toBeTruthy();
    expect(screen.getByTestId("agent-item-model-a-new").textContent).toBe(
      "gpt-test"
    );
    expect(avatarProps("a-new").seed).toBe("a-new");
    expect(mockedClient.agents.create).toHaveBeenCalledWith(
      expect.objectContaining<Partial<CreateAgentInput>>({
        Name: "Radar",
        Slug: "radar",
        Brief: "Sweeps the airspace.",
        Avatar: expect.any(Object),
        ProviderID: "openai",
        Model: "gpt-test",
      })
    );
  });

  it("gates delete call behind confirmation and removes the agent", async () => {
    const c = mockedClient;
    await renderView();

    // Red destructive trigger in detail header
    // HTMLButtonElement by construction.
    // SAFETY: the query targets the detail header's delete control, an
    const del = screen.getByTestId(
      "btn-config-delete-agent"
    ) as HTMLButtonElement;

    expect(del.className).toContain("bg-danger");
    expect(del.className).toContain("text-accenton");

    await act(async () => {
      fireEvent.click(del);
    });
    expect(screen.getByTestId("delete-agent-modal")).toBeTruthy();
    expect(c.agents.delete).not.toHaveBeenCalled();

    // Cancel first
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-cancel-delete"));
    });
    await waitFor(() =>
      expect(screen.queryByTestId("delete-agent-modal")).toBeNull()
    );
    expect(c.agents.delete).not.toHaveBeenCalled();

    // Open again and confirm
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-config-delete-agent"));
    });

    // HTMLInputElement by construction.
    // SAFETY: the query targets the confirm dialog's name field, an
    const nameInput = screen.getByTestId(
      "delete-agent-name-input"
    ) as HTMLInputElement;

    await act(async () => {
      fireEvent.change(nameInput, { target: { value: "Scout" } });
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-confirm-delete"));
    });
    await waitFor(() => expect(c.agents.delete).toHaveBeenCalledWith("a1"));
    // The agent leaves the list and with no agents left, empty state shows
    await waitFor(() =>
      expect(screen.queryByTestId("agent-item-a1")).toBeNull()
    );
    expect(screen.getByTestId("agents-empty")).toBeTruthy();
  });

  it("selects the next available agent on delete when multiple agents exist", async () => {
    agents = [
      makeAgent({ ID: "a1", Name: "Scout", Slug: "scout" }),
      makeAgent({ ID: "a2", Name: "Sentinel", Slug: "sentinel" }),
    ];
    await renderView();

    expect(screen.getByTestId("agent-item-a1")).toBeTruthy();
    expect(screen.getByTestId("agent-item-a2")).toBeTruthy();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-config-delete-agent"));
    });

    // HTMLInputElement by construction.
    // SAFETY: the query targets the confirm dialog's name field, an
    const nameInput = screen.getByTestId(
      "delete-agent-name-input"
    ) as HTMLInputElement;

    await act(async () => {
      fireEvent.change(nameInput, { target: { value: "Scout" } });
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-confirm-delete"));
    });
    await waitFor(() =>
      expect(mockedClient.agents.delete).toHaveBeenCalledWith("a1")
    );

    // a1 is deleted, a2 is now selected in the detail pane
    await waitFor(() =>
      expect(screen.queryByTestId("agent-item-a1")).toBeNull()
    );
    expect(screen.getByTestId("agent-item-a2")).toBeTruthy();
    expect(screen.getByTestId("config-readonly-slug").textContent).toBe(
      "sentinel"
    );
  });
});

describe("AgentConfigPane sections", () => {
  it("renders identity, model, skills, tools, MCP, and hooks sections", async () => {
    await renderView();

    for (const id of [
      "config-section-identity",
      "config-section-model",
      "config-section-params",
      "config-section-skills",
      "config-section-tools",
      "config-section-mcp",
      "config-section-hooks",
    ]) {
      expect(screen.getByTestId(id), `missing section ${id}`).toBeTruthy();
    }

    // HTMLButtonElement by construction.
    // SAFETY: the query targets the skill row's toggle control, an
    const off = screen.getByTestId(
      "config-skill-off-skill"
    ) as HTMLButtonElement;

    expect(off.disabled).toBe(true);
  });

  it("writes an explicit tool list on first deselect (all-on default)", async () => {
    const c = mockedClient;
    await renderView();

    await act(async () => {
      fireEvent.click(screen.getByTestId("config-tab-capabilities"));
    });

    // HTMLButtonElement by construction.
    // SAFETY: the query targets the tool row's toggle control, an
    const shell = screen.getByTestId("config-tool-shell") as HTMLButtonElement;
    expect(shell.getAttribute("aria-pressed")).toBe("true");

    await act(async () => {
      fireEvent.click(shell);
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-agent"));
    });
    await waitFor(() => expect(c.agents.update).toHaveBeenCalled());
    // the pane builds.
    // SAFETY: the suite's update mock is only ever called with the Agent
    const saved = c.agents.update.mock.calls[0][0] as Agent;
    expect(saved.Tools).toEqual(["browser"]);
    expect(saved.Skills).toEqual([]);
    expect(saved.MCPServers).toEqual([]);
  });
});
