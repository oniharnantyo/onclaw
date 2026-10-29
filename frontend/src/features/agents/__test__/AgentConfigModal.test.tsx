/**
 * Component tests for the config modal's Identity section (task 6.1): the
 * file-rail editors reflect the workspace documents, saving rewrites the
 * right file, "Enhance prompts" dispatches the regeneration with the
 * instruction and reloads documents plus the agent row, the failed banner
 * carries the reason, the editors lock while generating, and the old
 * SystemPrompt surface is gone. The lib client is mocked wholesale; the
 * modal mounts through react-dom/client with @testing-library/dom queries
 * because @testing-library/react is not a project dependency.
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

import type {
  AgentHookAssignment,
  ModelCapability,
  PromptDocuments,
  ProviderView,
  WorkspaceSkill,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type {
  Agent,
  Hook,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client, KnowledgeDocument } from "../../../lib/client";
import { dismissToast, getToasts, type Toast } from "../../../lib/toasts";

// The mutable payloads the mocked client hands out; tests re-point them to
// simulate a regeneration's reloaded documents and refreshed row.
let docs: PromptDocuments;

let agentRow: Agent;

/** Hook catalog and the agent's assignments, re-pointed per test. */
let hookDefs: Hook[] = [];

let hookAssignments: AgentHookAssignment[] = [];

// The provider's model capabilities, re-pointed per test (task 2.4's suites
// need a second model whose effort set is empty).
let models: ModelCapability[];

// The agent's own workspace skills, re-pointed per test (add-agent-created-
// skills task 4.2); promote/delete mocks mutate it to simulate the backend.
let workspaceSkills: WorkspaceSkill[];

const providerList: ProviderView[] = [
  {
    ID: "openai",
    Type: "acl/openai",
    Name: "OpenAI",
    BaseURL: "",
    Enabled: true,
    KeySet: true,
    KeyHint: "sk-…",
    CreatedAt: "2026-01-01T00:00:00Z",
    UpdatedAt: "2026-01-01T00:00:00Z",
  },
];

// SAFETY: vi.fn's generic pins the mock to the real prop signature, so
// mock.calls entries are Agents without a cast.
const onSaved = vi.fn<(agent: Agent) => void>();

// SAFETY: vi.fn's generic pins the mock to the real prop signature, so
// mock.calls entries are id strings without a cast.
const onDeleted = vi.fn<(id: string) => void>();

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

// Import after the mock is registered.
import { AgentConfigPane } from "../components/AgentConfigPane";
import { client } from "../../../lib/client";

// SAFETY: the client module is mocked with makeClient(), so its runtime shape
// is exactly this mock object.
const mockedClient = client as MockedClient;

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
    listWorkspaceSkills: MockFn;
    promoteWorkspaceSkill: MockFn;
    deleteWorkspaceSkill: MockFn;
  };
  knowledge: {
    list: MockFn;
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

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    ID: "a1",
    Name: "Scout",
    Slug: "scout",
    Description: "Watches the horizon.",
    Brief: "A patient scout that reports before acting.",
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

function makeDocs(overrides: Partial<PromptDocuments> = {}): PromptDocuments {
  return {
    Identity: "# Scout\nCurious and careful.",
    IdentityExists: true,
    Soul: "Warm, plain-spoken.",
    SoulExists: true,
    Bootstrap: "Start each session with a check-in.",
    BootstrapExists: true,
    ...overrides,
  };
}

function makeClient(): MockedClient {
  const c = {
    agents: {
      list: vi.fn(async (): Promise<Agent[]> => []),
      get: vi.fn(async (): Promise<Agent> => agentRow),
      create: vi.fn(),
      update: vi.fn(async (a: Agent): Promise<Agent> => a),
      delete: vi.fn(async (): Promise<void> => undefined),
      getPromptDocuments: vi.fn(async (): Promise<PromptDocuments> => docs),
      savePromptDocument: vi.fn(async (): Promise<void> => undefined),
      // Mimic the backend: regeneration settles the status to ready and
      // rewrites the documents, so a reload sees the new state.
      regeneratePrompts: vi.fn(async (): Promise<void> => {
        docs = makeDocs({
          Identity: "# Scout\nEnhanced by regeneration.",
          Soul: "Enhanced soul.",
        });
        agentRow = makeAgent({
          ...agentRow,
          PromptsStatus: "ready",
          PromptsError: "",
        });
      }),
      listWorkspaceSkills: vi.fn(
        async (): Promise<WorkspaceSkill[]> => workspaceSkills
      ),
      promoteWorkspaceSkill: vi.fn(async (): Promise<void> => undefined),
      deleteWorkspaceSkill: vi.fn(async (): Promise<void> => undefined),
    },
    knowledge: {
      list: vi.fn(async (): Promise<KnowledgeDocument[]> => []),
    },
    settings: {
      providers: vi.fn(async (): Promise<ProviderView[]> => providerList),
      providerModels: vi.fn(async (): Promise<ModelCapability[]> => models),
      skills: vi.fn(async (): Promise<object[]> => []),
      tools: vi.fn(async (): Promise<object[]> => []),
      mcpServers: vi.fn(async (): Promise<object[]> => []),
      hooks: vi.fn(async (): Promise<Hook[]> => hookDefs),
      agentHooks: vi.fn(
        async (): Promise<AgentHookAssignment[]> => hookAssignments
      ),
      assignHook: vi.fn(async (): Promise<void> => undefined),
      unassignHook: vi.fn(async (): Promise<void> => undefined),
    },
  };

  // SAFETY: the object literal above satisfies the Client surface with its
  // vi.fn members matching the MockedClient contract.
  return c as MockedClient;
}

// --- tiny mount helper over react-dom/client (@testing-library/react absent)
const roots: Root[] = [];

async function renderModal(agent: Agent): Promise<void> {
  // SAFETY: the flag is an optional boolean on the global object.
  (
    globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
  ).IS_REACT_ACT_ENVIRONMENT = true;

  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  roots.push(root);
  await act(async () => {
    root.render(
      <AgentConfigPane
        agent={agent}
        onClose={() => undefined}
        onSaved={onSaved}
        onDeleted={onDeleted}
      />
    );
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  docs = makeDocs();
  agentRow = makeAgent();
  hookDefs = [];
  hookAssignments = [];
  workspaceSkills = [];
  // Toasts live in a module store; drop leftovers so per-test assertions
  // only see this test's outcomes.
  getToasts().forEach((t: Toast): void => dismissToast(t.id));
  models = [
    {
      ID: "gpt-test",
      ContextWindow: 128000,
      Efforts: [],
      ImageInput: false,
      PDFInput: false,
      VideoInput: false,
      PDFParts: true,
      VideoParts: true,
    },
  ];
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

describe("AgentConfigModal Identity section", () => {
  it("loads the workspace documents into the rail editors", async () => {
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(mockedClient.agents.getPromptDocuments).toHaveBeenCalledWith("a1")
    );

    // SAFETY: this test id renders a textarea element.
    const identity = screen.getByTestId(
      "input-prompt-identity"
    ) as HTMLTextAreaElement;

    expect(identity.value).toBe("# Scout\nCurious and careful.");

    // The rail switches editors; each shows its own workspace file.
    await act(async () => {
      fireEvent.click(screen.getByTestId("prompt-file-soul"));
    });

    // SAFETY: this test id renders a textarea element.
    const soul = screen.getByTestId("input-prompt-soul") as HTMLTextAreaElement;

    expect(soul.value).toBe("Warm, plain-spoken.");

    await act(async () => {
      fireEvent.click(screen.getByTestId("prompt-file-bootstrap"));
    });

    // SAFETY: this test id renders a textarea element.
    const bootstrap = screen.getByTestId(
      "input-prompt-bootstrap"
    ) as HTMLTextAreaElement;

    expect(bootstrap.value).toBe("Start each session with a check-in.");
  });

  it("shows a not-generated hint for absent files", async () => {
    docs = makeDocs({
      Identity: "",
      IdentityExists: false,
      Soul: "",
      SoulExists: false,
      Bootstrap: "",
      BootstrapExists: false,
    });

    await renderModal(makeAgent());

    await waitFor(() =>
      expect(screen.getByTestId("prompt-not-generated-identity")).toBeTruthy()
    );

    // SAFETY: this test id renders a textarea element.
    const identity = screen.getByTestId(
      "input-prompt-identity"
    ) as HTMLTextAreaElement;

    expect(identity.value).toBe("");
  });

  it("saves an edited document under its workspace file name", async () => {
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(mockedClient.agents.getPromptDocuments).toHaveBeenCalledWith("a1")
    );

    // SAFETY: this test id renders a textarea element.
    const identity = screen.getByTestId(
      "input-prompt-identity"
    ) as HTMLTextAreaElement;

    await act(async () => {
      fireEvent.change(identity, {
        target: { value: "# Scout\nSharper now." },
      });
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-prompt-identity"));
    });

    await waitFor(() =>
      expect(mockedClient.agents.savePromptDocument).toHaveBeenCalledWith(
        "a1",
        "IDENTITY.md",
        "# Scout\nSharper now."
      )
    );
  });

  it("enhances with the instruction and reloads documents and the agent row", async () => {
    // Start failed: regeneration is the retry path out of the banner.
    agentRow = makeAgent({
      PromptsStatus: "failed",
      PromptsError: "provider unreachable",
    });
    await renderModal(agentRow);

    await waitFor(() =>
      expect(screen.getByTestId("prompts-failed-banner")).toBeTruthy()
    );

    expect(screen.getByTestId("prompts-failed-reason").textContent).toBe(
      "provider unreachable"
    );

    // SAFETY: this test id renders a textarea element.
    const instruction = screen.getByTestId(
      "input-regen-instruction"
    ) as HTMLTextAreaElement;

    await act(async () => {
      fireEvent.change(instruction, { target: { value: "make it sharper" } });
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-enhance-prompts"));
    });

    await waitFor(() =>
      expect(mockedClient.agents.regeneratePrompts).toHaveBeenCalledWith(
        "a1",
        "make it sharper"
      )
    );

    // The mock's regenerate swaps the docs payload; the section must reload it.
    await waitFor(() =>
      expect(mockedClient.agents.getPromptDocuments).toHaveBeenCalledTimes(2)
    );

    // SAFETY: this test id renders a textarea element.
    const identity = screen.getByTestId(
      "input-prompt-identity"
    ) as HTMLTextAreaElement;

    await waitFor(() =>
      expect(identity.value).toBe("# Scout\nEnhanced by regeneration.")
    );

    // The row is re-read and bubbled so the grid status follows.
    await waitFor(() =>
      expect(mockedClient.agents.get).toHaveBeenCalledWith("a1")
    );

    expect(onSaved).toHaveBeenCalledTimes(1);
    expect(onSaved.mock.calls[0][0].PromptsStatus).toBe("ready");

    // Success clears the failed banner and the instruction.
    await waitFor(() =>
      expect(screen.queryByTestId("prompts-failed-banner")).toBeNull()
    );

    // SAFETY: this test id renders a textarea element.
    const cleared = screen.getByTestId(
      "input-regen-instruction"
    ) as HTMLTextAreaElement;

    expect(cleared.value).toBe("");
  });

  it("renders the failed banner with the reason and keeps retry available", async () => {
    await renderModal(
      makeAgent({ PromptsStatus: "failed", PromptsError: "model timed out" })
    );

    await waitFor(() =>
      expect(screen.getByTestId("prompts-failed-banner")).toBeTruthy()
    );

    expect(screen.getByTestId("prompts-failed-reason").textContent).toBe(
      "model timed out"
    );

    // SAFETY: this test id renders a button element.
    const enhance = screen.getByTestId(
      "btn-enhance-prompts"
    ) as HTMLButtonElement;

    expect(enhance.disabled).toBe(false);
  });

  it("disables the editors while prompts are generating", async () => {
    await renderModal(makeAgent({ PromptsStatus: "generating" }));

    await waitFor(() =>
      expect(screen.getByTestId("prompts-generating")).toBeTruthy()
    );

    // SAFETY: this test id renders a textarea element.
    const identity = screen.getByTestId(
      "input-prompt-identity"
    ) as HTMLTextAreaElement;

    expect(identity.disabled).toBe(true);

    // SAFETY: this test id renders a button element.
    const save = screen.getByTestId(
      "btn-save-prompt-identity"
    ) as HTMLButtonElement;

    expect(save.disabled).toBe(true);

    // SAFETY: this test id renders a button element.
    const enhance = screen.getByTestId(
      "btn-enhance-prompts"
    ) as HTMLButtonElement;

    expect(enhance.disabled).toBe(true);
  });

  it("shows the slug and brief as read-only context, never inputs", async () => {
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(screen.getByTestId("config-readonly-slug")).toBeTruthy()
    );

    expect(screen.getByTestId("config-readonly-slug").textContent).toBe(
      "scout"
    );

    expect(screen.getByTestId("config-readonly-brief").textContent).toContain(
      "patient scout"
    );

    // Read-only context: rendered as text, not editable fields.
    expect(screen.getByTestId("config-readonly-slug").tagName).not.toBe(
      "INPUT"
    );
    expect(screen.getByTestId("config-readonly-brief").tagName).not.toBe(
      "TEXTAREA"
    );
  });

  it("drops the old system-prompt editing surface", async () => {
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(screen.getByTestId("config-section-identity")).toBeTruthy()
    );

    expect(screen.queryByTestId("config-section-prompt")).toBeNull();
    expect(screen.queryByTestId("input-config-system-prompt")).toBeNull();
  });
});

describe("AgentConfigModal identity editing", () => {
  it("edits the agent's name and description through update", async () => {
    await renderModal(makeAgent());

    await act(async () => {
      fireEvent.change(screen.getByTestId("input-config-name"), {
        target: { value: "Radar" },
      });
    });
    await act(async () => {
      fireEvent.change(screen.getByTestId("input-config-description"), {
        target: { value: "Sweeps the airspace." },
      });
    });

    // The pane header tracks the draft name.
    expect(screen.getByTestId("config-header-name").textContent).toBe("Radar");

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-agent"));
    });

    await waitFor(() => expect(mockedClient.agents.update).toHaveBeenCalled());

    // SAFETY: vi.fn() calls carry the updated Agent as the only argument.
    const saved = mockedClient.agents.update.mock.calls[0][0] as Agent;

    expect(saved.Name).toBe("Radar");
    expect(saved.Description).toBe("Sweeps the airspace.");
  });

  it("renders every MCP server on for an agent with no server list and saves an explicit list after a toggle", async () => {
    mockedClient.settings.mcpServers.mockResolvedValueOnce([
      { ID: "mcp1", Name: "Files", Enabled: true },
      { ID: "mcp2", Name: "Jira", Enabled: true },
    ]);
    await renderModal(makeAgent({ MCPServers: null }));

    await waitFor(() =>
      expect(screen.getByTestId("config-mcp-mcp1")).toBeTruthy()
    );
    expect(
      screen.getByTestId("config-mcp-mcp1").getAttribute("aria-checked")
    ).toBe("true");
    expect(
      screen.getByTestId("config-mcp-mcp2").getAttribute("aria-checked")
    ).toBe("true");

    // Turning Jira off materializes the remaining id as the stored list.
    await act(async () => {
      fireEvent.click(screen.getByTestId("config-mcp-mcp2"));
    });
    expect(
      screen.getByTestId("config-mcp-mcp2").getAttribute("aria-checked")
    ).toBe("false");

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-agent"));
    });
    await waitFor(() => expect(mockedClient.agents.update).toHaveBeenCalled());

    // SAFETY: vi.fn() calls carry the updated Agent as the only argument.
    const saved = mockedClient.agents.update.mock.calls[0][0] as Agent;
    expect(saved.MCPServers).toEqual(["mcp1"]);
  });

  it("renders only the listed servers on for a restricted agent", async () => {
    mockedClient.settings.mcpServers.mockResolvedValueOnce([
      { ID: "mcp1", Name: "Files", Enabled: true },
      { ID: "mcp2", Name: "Jira", Enabled: true },
    ]);
    await renderModal(makeAgent({ MCPServers: ["mcp2"] }));

    await waitFor(() =>
      expect(screen.getByTestId("config-mcp-mcp2")).toBeTruthy()
    );
    expect(
      screen.getByTestId("config-mcp-mcp1").getAttribute("aria-checked")
    ).toBe("false");
    expect(
      screen.getByTestId("config-mcp-mcp2").getAttribute("aria-checked")
    ).toBe("true");
    // The only server left on is pinned: an empty list would mean every
    // server, not none.
    // SAFETY: the Toggle renders a button element.
    expect(
      (screen.getByTestId("config-mcp-mcp2") as HTMLButtonElement).disabled
    ).toBe(true);
  });

  it("rejects an empty name before it saves", async () => {
    await renderModal(makeAgent());

    await act(async () => {
      fireEvent.change(screen.getByTestId("input-config-name"), {
        target: { value: "   " },
      });
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-agent"));
    });

    expect(mockedClient.agents.update).not.toHaveBeenCalled();
    expect(
      screen.getByTestId("field-error-config-agent-name").textContent
    ).toContain("required");
  });

  it("opens the type-the-name delete confirmation from the footer", async () => {
    await renderModal(makeAgent());

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-config-delete-agent"));
    });

    expect(screen.getByTestId("delete-agent-modal")).toBeTruthy();
    expect(mockedClient.agents.delete).not.toHaveBeenCalled();

    // Confirming without typing the exact name does nothing.
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-confirm-delete"));
    });
    expect(mockedClient.agents.delete).not.toHaveBeenCalled();

    // SAFETY: this test id renders an input element.
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
    await waitFor(() => expect(onDeleted).toHaveBeenCalledWith("a1"));
  });
});

describe("AgentConfigModal avatar section", () => {
  it("hydrates the stored avatar and sends it back on save", async () => {
    const stored = { sex: "woman", hairStyle: "mohawk" };

    await renderModal(makeAgent({ Avatar: stored }));

    expect(screen.getByTestId("config-section-avatar")).toBeTruthy();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-agent"));
    });

    await waitFor(() => expect(mockedClient.agents.update).toHaveBeenCalled());

    // SAFETY: vi.fn() calls carry the updated Agent as the only argument.
    const saved = mockedClient.agents.update.mock.calls[0][0] as Agent;

    expect(saved.Avatar).toEqual(stored);
  });

  it("rolls a fresh random config when the agent has no stored avatar", async () => {
    await renderModal(makeAgent());

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-agent"));
    });

    await waitFor(() => expect(mockedClient.agents.update).toHaveBeenCalled());

    // SAFETY: vi.fn() calls carry the updated Agent as the only argument.
    const saved = mockedClient.agents.update.mock.calls[0][0] as Agent;

    expect(Object.keys(saved.Avatar ?? {}).length).toBeGreaterThan(5);
  });
});

describe("AgentConfigModal hooks section", () => {
  function makeHook(patch: Partial<Hook> = {}): Hook {
    return {
      ID: "h1",
      Name: "notify",
      Event: "pre_tool_use",
      Matcher: "",
      HandlerType: "command",
      Config: '{"command": "echo done"}',
      TimeoutMS: 0,
      OnFailure: "",
      Enabled: true,
      Position: 0,
      Status: "",
      StatusError: "",
      CreatedAt: "",
      UpdatedAt: "",
      ...patch,
    };
  }

  function assignment(
    patch: Partial<AgentHookAssignment>
  ): AgentHookAssignment {
    return { HookID: "h1", HookName: "notify", Position: 0, ...patch };
  }

  /** Row titles in render order — the list the dispatcher evaluates top-down. */
  function assignedTitles(): string[] {
    return Array.from(
      screen.getByTestId("config-hook-list").querySelectorAll("li")
    ).map((li: Element): string => li.textContent ?? "");
  }

  it("shows the empty state when nothing is assigned", async () => {
    hookDefs = [makeHook()];
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(screen.getByTestId("config-hooks-empty")).toBeTruthy()
    );

    expect(screen.queryByTestId("config-hook-list")).toBeNull();
    // The catalog picker stays available for the first assignment.
    expect(screen.getByTestId("config-hook-add-select")).toBeTruthy();
  });

  it("says so when no hooks exist in Settings at all", async () => {
    hookDefs = [];
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(screen.getByTestId("config-hooks-none")).toBeTruthy()
    );
    expect(screen.queryByTestId("config-hook-add-select")).toBeNull();
  });

  it("renders the assigned hooks in evaluation order with their events", async () => {
    hookDefs = [
      makeHook(),
      makeHook({ ID: "h2", Name: "guard", Event: "post_tool_use" }),
    ];
    hookAssignments = [
      assignment({ HookID: "h1", HookName: "notify", Position: 0 }),
      assignment({ HookID: "h2", HookName: "guard", Position: 1 }),
    ];
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(screen.getByTestId("config-hook-list")).toBeTruthy()
    );

    const titles = assignedTitles();
    expect(titles[0]).toContain("notify");
    expect(titles[0]).toContain("pre_tool_use");
    expect(titles[1]).toContain("guard");
    expect(titles[1]).toContain("post_tool_use");

    // The end rows cannot move further out.
    // SAFETY: this test id renders a button element.
    expect(
      (screen.getByTestId("config-hook-up-h1") as HTMLButtonElement).disabled
    ).toBe(true);
    // SAFETY: this test id renders a button element.
    expect(
      (screen.getByTestId("config-hook-down-h2") as HTMLButtonElement).disabled
    ).toBe(true);
  });

  it("persists the reordered set through AssignHook on save", async () => {
    hookDefs = [makeHook(), makeHook({ ID: "h2", Name: "guard" })];
    hookAssignments = [
      assignment({ HookID: "h1", HookName: "notify", Position: 0 }),
      assignment({ HookID: "h2", HookName: "guard", Position: 1 }),
    ];
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(screen.getByTestId("config-hook-up-h2")).toBeTruthy()
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId("config-hook-up-h2"));
    });
    expect(assignedTitles()[0]).toContain("guard");

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-agent"));
    });

    await waitFor(() =>
      expect(mockedClient.settings.assignHook).toHaveBeenCalledTimes(2)
    );
    expect(mockedClient.settings.assignHook).toHaveBeenNthCalledWith(
      1,
      "a1",
      "h2",
      0
    );
    expect(mockedClient.settings.assignHook).toHaveBeenNthCalledWith(
      2,
      "a1",
      "h1",
      1
    );
    expect(mockedClient.settings.unassignHook).not.toHaveBeenCalled();
  });

  it("persists a removal through UnassignHook on save", async () => {
    hookDefs = [makeHook(), makeHook({ ID: "h2", Name: "guard" })];
    hookAssignments = [
      assignment({ HookID: "h1", HookName: "notify", Position: 0 }),
      assignment({ HookID: "h2", HookName: "guard", Position: 1 }),
    ];
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(screen.getByTestId("config-hook-remove-h1")).toBeTruthy()
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId("config-hook-remove-h1"));
    });
    expect(assignedTitles().length).toBe(1);

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-agent"));
    });

    await waitFor(() =>
      expect(mockedClient.settings.unassignHook).toHaveBeenCalledWith(
        "a1",
        "h1"
      )
    );
    expect(mockedClient.settings.assignHook).toHaveBeenCalledWith(
      "a1",
      "h2",
      0
    );
  });

  it("picks a hook from the full list and appends it to the order", async () => {
    hookDefs = [
      makeHook(),
      makeHook({ ID: "h2", Name: "guard", Event: "run_finished" }),
    ];
    hookAssignments = [assignment({ HookID: "h1", HookName: "notify" })];
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(screen.getByTestId("config-hook-add-select")).toBeTruthy()
    );

    // SAFETY: this test id renders a select element.
    const picker = screen.getByTestId(
      "config-hook-add-select"
    ) as HTMLSelectElement;

    await act(async () => {
      fireEvent.change(picker, { target: { value: "h2" } });
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("config-hook-add"));
    });

    expect(assignedTitles()[1]).toContain("guard");

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-agent"));
    });

    await waitFor(() =>
      expect(mockedClient.settings.assignHook).toHaveBeenCalledWith(
        "a1",
        "h2",
        1
      )
    );
  });
});

describe("AgentConfigModal subagents toggle", () => {
  it("renders on by default for an agent with subagents enabled", async () => {
    await renderModal(makeAgent());

    // SAFETY: the toggle renders a button carrying role="switch".
    const toggle = screen.getByTestId(
      "config-subagents-toggle"
    ) as HTMLButtonElement;

    expect(toggle.getAttribute("aria-checked")).toBe("true");
  });

  it("hydrates the stored flag and persists a flip through update", async () => {
    await renderModal(makeAgent({ SubagentsEnabled: false }));

    // SAFETY: the toggle renders a button carrying role="switch".
    const toggle = screen.getByTestId(
      "config-subagents-toggle"
    ) as HTMLButtonElement;

    expect(toggle.getAttribute("aria-checked")).toBe("false");

    await act(async () => {
      fireEvent.click(toggle);
    });
    expect(toggle.getAttribute("aria-checked")).toBe("true");

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-agent"));
    });

    await waitFor(() => expect(mockedClient.agents.update).toHaveBeenCalled());

    // SAFETY: vi.fn() calls carry the updated Agent as the only argument.
    const saved = mockedClient.agents.update.mock.calls[0][0] as Agent;

    expect(saved.SubagentsEnabled).toBe(true);
  });

  it("persists a switch-off through update", async () => {
    await renderModal(makeAgent({ SubagentsEnabled: true }));

    // SAFETY: the toggle renders a button carrying role="switch".
    const toggle = screen.getByTestId(
      "config-subagents-toggle"
    ) as HTMLButtonElement;

    await act(async () => {
      fireEvent.click(toggle);
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-agent"));
    });

    await waitFor(() => expect(mockedClient.agents.update).toHaveBeenCalled());

    // SAFETY: vi.fn() calls carry the updated Agent as the only argument.
    const saved = mockedClient.agents.update.mock.calls[0][0] as Agent;

    expect(saved.SubagentsEnabled).toBe(false);
  });
});

describe("AgentConfigModal default effort (task 2.4)", () => {
  /** The two-model capability payload the effort suites drive. */
  function useEffortModels(): void {
    models = [
      {
        ID: "gpt-test",
        ContextWindow: 128000,
        Efforts: ["low", "medium", "high"],
        ImageInput: false,
        PDFInput: false,
        VideoInput: false,
        PDFParts: true,
        VideoParts: true,
      },
      {
        ID: "plain-model",
        ContextWindow: 64000,
        Efforts: [],
        ImageInput: false,
        PDFInput: false,
        VideoInput: false,
        PDFParts: true,
        VideoParts: true,
      },
    ];
  }

  async function openModelTab(): Promise<void> {
    await act(async () => {
      fireEvent.click(screen.getByTestId("config-tab-model"));
    });
    await waitFor(() =>
      expect(mockedClient.settings.providerModels).toHaveBeenCalledWith(
        "openai"
      )
    );
  }

  it("offers only the selected model's effort set and hydrates the stored pick", async () => {
    useEffortModels();
    await renderModal(makeAgent({ Model: "gpt-test", Effort: "low" }));
    await openModelTab();

    // SAFETY: the effort picker renders a native select by contract.
    const effort = screen.getByTestId(
      "select-config-effort"
    ) as HTMLSelectElement;

    expect(effort.value).toBe("low");

    const options = Array.from(effort.options).map(
      (o: HTMLOptionElement): string => o.value
    );

    // "Provider default" (inherit) plus exactly the model's set — nothing else.
    expect(options).toEqual(["", "low", "medium", "high"]);
  });

  it("hides the control and resets the choice for a model without efforts", async () => {
    useEffortModels();
    await renderModal(makeAgent({ Model: "gpt-test", Effort: "high" }));
    await openModelTab();
    expect(screen.getByTestId("select-config-effort")).toBeTruthy();

    // Switch to the effort-less model through the model picker's native
    // select seam (the Combobox mirrors its value there for tests).
    await act(async () => {
      fireEvent.change(screen.getByTestId("select-config-model"), {
        target: { value: "plain-model" },
      });
    });

    // The control is gone; saving records the reset (empty = inherit).
    expect(screen.queryByTestId("select-config-effort")).toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-agent"));
    });
    await waitFor(() => expect(mockedClient.agents.update).toHaveBeenCalled());

    // SAFETY: vi.fn() calls carry the updated Agent as the only argument.
    const saved = mockedClient.agents.update.mock.calls[0][0] as Agent;

    expect(saved.Model).toBe("plain-model");
    expect(saved.Effort).toBe("");
  });

  it("round-trips the picked effort through save", async () => {
    useEffortModels();
    await renderModal(makeAgent({ Model: "gpt-test" }));
    await openModelTab();

    await act(async () => {
      fireEvent.change(screen.getByTestId("select-config-effort"), {
        target: { value: "high" },
      });
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-agent"));
    });
    await waitFor(() => expect(mockedClient.agents.update).toHaveBeenCalled());

    // SAFETY: vi.fn() calls carry the updated Agent as the only argument.
    const saved = mockedClient.agents.update.mock.calls[0][0] as Agent;

    expect(saved.Model).toBe("gpt-test");
    expect(saved.Effort).toBe("high");
  });
});

describe("AgentConfigModal workspace skills section", () => {
  function wsRow(name: string, description: string): WorkspaceSkill {
    return { Name: name, Description: description };
  }

  /** True when a toast with this kind carrying this text is on the stack. */
  function hasToast(kind: Toast["kind"], needle: string): boolean {
    return getToasts().some(
      (t: Toast): boolean => t.kind === kind && t.text.includes(needle)
    );
  }

  it("lists the agent's workspace skills with name and description", async () => {
    workspaceSkills = [
      wsRow("deploy-note", "Posts a deployment note to the log."),
      wsRow("triage", "Triages incoming reports."),
    ];
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(screen.getByTestId("config-section-workspace-skills")).toBeTruthy()
    );

    const first = screen.getByTestId(
      "config-workspace-skill-deploy-note"
    ).textContent;

    expect(first).toContain("deploy-note");
    expect(first).toContain("Posts a deployment note to the log.");

    const second = screen.getByTestId(
      "config-workspace-skill-triage"
    ).textContent;

    expect(second).toContain("triage");
    expect(second).toContain("Triages incoming reports.");

    // Each row carries its Promote and Delete actions.
    expect(
      screen.getByTestId("config-workspace-skill-promote-deploy-note")
    ).toBeTruthy();
    expect(
      screen.getByTestId("config-workspace-skill-delete-deploy-note")
    ).toBeTruthy();
    expect(
      screen.getByTestId("config-workspace-skill-promote-triage")
    ).toBeTruthy();
    expect(
      screen.getByTestId("config-workspace-skill-delete-triage")
    ).toBeTruthy();
  });

  it("shows the empty state when the workspace has no skills", async () => {
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(screen.getByTestId("config-workspace-skills-empty")).toBeTruthy()
    );

    expect(screen.queryByTestId("config-workspace-skill-list")).toBeNull();
  });

  it("promotes, fires the success toast, and re-fetches the list", async () => {
    workspaceSkills = [wsRow("deploy-note", "Posts a deployment note.")];
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(
        screen.getByTestId("config-workspace-skill-promote-deploy-note")
      ).toBeTruthy()
    );
    expect(mockedClient.agents.listWorkspaceSkills).toHaveBeenCalledTimes(1);

    await act(async () => {
      fireEvent.click(
        screen.getByTestId("config-workspace-skill-promote-deploy-note")
      );
    });

    await waitFor(() =>
      expect(mockedClient.agents.promoteWorkspaceSkill).toHaveBeenCalledWith(
        "a1",
        "deploy-note"
      )
    );
    // The list refreshes after every successful action (load + refresh).
    await waitFor(() =>
      expect(mockedClient.agents.listWorkspaceSkills).toHaveBeenCalledTimes(2)
    );
    await waitFor(() =>
      expect(hasToast("info", "promoted to the global library")).toBe(true)
    );
  });

  it("surfaces the collision rejection and changes nothing", async () => {
    workspaceSkills = [wsRow("deploy-note", "Posts a deployment note.")];
    mockedClient.agents.promoteWorkspaceSkill.mockRejectedValueOnce(
      new Error('a skill named "deploy-note" already exists in the library')
    );
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(
        screen.getByTestId("config-workspace-skill-promote-deploy-note")
      ).toBeTruthy()
    );

    await act(async () => {
      fireEvent.click(
        screen.getByTestId("config-workspace-skill-promote-deploy-note")
      );
    });

    // The backend's rejection message — it names the colliding skill — is
    // what surfaces.
    await waitFor(() =>
      expect(hasToast("danger", "already exists in the library")).toBe(true)
    );
    // A rejection does not refresh the list.
    expect(mockedClient.agents.listWorkspaceSkills).toHaveBeenCalledTimes(1);
    expect(
      screen.getByTestId("config-workspace-skill-deploy-note")
    ).toBeTruthy();
  });

  it("deletes the workspace skill and refreshes the list", async () => {
    workspaceSkills = [
      wsRow("deploy-note", "Posts a deployment note."),
      wsRow("triage", "Triages incoming reports."),
    ];
    // Mimic the backend: the directory is gone when the refresh reads it.
    mockedClient.agents.deleteWorkspaceSkill.mockImplementation(
      async (): Promise<void> => {
        workspaceSkills = workspaceSkills.filter(
          (s: WorkspaceSkill): boolean => s.Name !== "deploy-note"
        );
      }
    );
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(
        screen.getByTestId("config-workspace-skill-deploy-note")
      ).toBeTruthy()
    );

    await act(async () => {
      fireEvent.click(
        screen.getByTestId("config-workspace-skill-delete-deploy-note")
      );
    });

    await waitFor(() =>
      expect(mockedClient.agents.deleteWorkspaceSkill).toHaveBeenCalledWith(
        "a1",
        "deploy-note"
      )
    );
    await waitFor(() =>
      expect(mockedClient.agents.listWorkspaceSkills).toHaveBeenCalledTimes(2)
    );
    await waitFor(() =>
      expect(
        screen.queryByTestId("config-workspace-skill-deploy-note")
      ).toBeNull()
    );
    // The untouched row stays.
    expect(screen.getByTestId("config-workspace-skill-triage")).toBeTruthy();
  });

  it("keeps the row actions inert while a request is in flight", async () => {
    workspaceSkills = [wsRow("deploy-note", "Posts a deployment note.")];
    mockedClient.agents.promoteWorkspaceSkill.mockReturnValueOnce(
      new Promise<void>(() => undefined)
    );
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(
        screen.getByTestId("config-workspace-skill-promote-deploy-note")
      ).toBeTruthy()
    );

    await act(async () => {
      fireEvent.click(
        screen.getByTestId("config-workspace-skill-promote-deploy-note")
      );
    });

    // SAFETY: these test ids render button elements.
    expect(
      (
        screen.getByTestId(
          "config-workspace-skill-promote-deploy-note"
        ) as HTMLButtonElement
      ).disabled
    ).toBe(true);
    // SAFETY: this test id renders the row's IconButton, a button element.
    expect(
      (
        screen.getByTestId(
          "config-workspace-skill-delete-deploy-note"
        ) as HTMLButtonElement
      ).disabled
    ).toBe(true);
  });

  it("degrades a rejected initial load to an inline error line", async () => {
    mockedClient.agents.listWorkspaceSkills.mockRejectedValueOnce(
      new Error("workspace unreadable")
    );
    await renderModal(makeAgent());

    await waitFor(() =>
      expect(
        screen.getByTestId("config-section-workspace-skills").textContent
      ).toContain("Couldn't load this agent's skills.")
    );

    // No rows and no crash.
    expect(screen.queryByTestId("config-workspace-skill-list")).toBeNull();
  });
});
