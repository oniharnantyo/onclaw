/**
 * Component tests for the three-step create wizard (tasks 5.1–5.2): per-step
 * validation rendered per field (task 4.5: error text + danger border on the
 * offending input — slug shape, slug uniqueness, required brief, required
 * provider/model), the slug auto-suggestion from the name, the advanced
 * section's catalog auto-fill of the context window (touch-freeze + Reset to
 * auto), the custom model-ID entry for models outside the provider list, the
 * onboarding loader
 * as the single create-phase spinner (task 4.6), and both create outcomes —
 * success, and the failed-generation handover that keeps the persisted row
 * (spec: Failed generation keeps the wizard's agent). The lib client is
 * mocked wholesale; the modal mounts through react-dom/client with
 * @testing-library/dom queries because @testing-library/react is not a
 * project dependency.
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

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

// Import after the mock is registered.
import { CreateAgentModal } from "../components/CreateAgentModal";
import { client } from "../../../lib/client";

// SAFETY: the client module is mocked with makeClient(), so its runtime shape
// is exactly this mock object.
const mockedClient = client as MockedClient;

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    ID: "a1",
    Name: "Scout",
    Slug: "scout",
    Description: "",
    Brief: "Watches the perimeter.",
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

let existingAgents: Agent[];

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
  };
};

function makeClient(): MockedClient {
  const c = {
    agents: {
      list: vi.fn(async () => existingAgents),
      get: vi.fn(),
      create: vi.fn(async (input: CreateAgentInput) =>
        makeAgent({
          ID: "a-new",
          Name: input.Name,
          Slug: input.Slug,
          Description: input.Description,
          Brief: input.Brief,
          Avatar: input.Avatar,
          ProviderID: input.ProviderID,
          Model: input.Model,
        })
      ),
      update: vi.fn(async (a: Agent) => a),
      delete: vi.fn(async () => undefined),
      getPromptDocuments: vi.fn(),
      savePromptDocument: vi.fn(),
      regeneratePrompts: vi.fn(async () => undefined),
    },
    settings: {
      providers: vi.fn(async () => [
        { ID: "openai", Name: "OpenAI", Type: "openai", Enabled: true },
        {
          ID: "anthropic",
          Name: "Anthropic",
          Type: "anthropic",
          Enabled: true,
        },
      ]),
      providerModels: vi.fn(async (): Promise<ModelCapability[]> => [
        {
          ID: "gpt-5",
          ContextWindow: 400000,
          Efforts: [],
          ImageInput: false,
          PDFInput: false,
          VideoInput: false,
        },
        {
          ID: "gpt-4o",
          ContextWindow: 128000,
          Efforts: [],
          ImageInput: false,
          PDFInput: false,
          VideoInput: false,
        },
        {
          ID: "custom-model",
          ContextWindow: 200000,
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
    },
  };

  // SAFETY: the object literal above satisfies the Client surface with its
  // vi.fn members matching the MockedClient contract.
  return c as MockedClient;
}

// --- tiny mount helper over react-dom/client (@testing-library/react absent)
const roots: Root[] = [];

const onClose = vi.fn<() => void>();

const onCreated = vi.fn<(agent: Agent) => void>();

async function renderModal(): Promise<void> {
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
      <CreateAgentModal
        agents={existingAgents}
        onClose={onClose}
        onCreated={onCreated}
      />
    );
  });
}

/** Fills a field by test id and flushes the change. */
async function type(testId: string, value: string): Promise<void> {
  // SAFETY: the wizard fields are inputs or textareas by construction.
  const el = screen.getByTestId(testId) as HTMLInputElement;
  await act(async () => {
    fireEvent.change(el, { target: { value } });
  });
}

async function click(testId: string): Promise<void> {
  await act(async () => {
    fireEvent.click(screen.getByTestId(testId));
  });
}

/** The wizard's provider/model pickers, looked up by test id. */
function selectEl(testId: string): HTMLSelectElement {
  // SAFETY: the wizard's picker test ids render select elements.
  return screen.getByTestId(testId) as HTMLSelectElement;
}

/** The wizard's text fields; inputs and textareas both carry .value. */
function fieldEl(testId: string): HTMLInputElement {
  // SAFETY: the wizard's text-field test ids render input/textarea elements.
  return screen.getByTestId(testId) as HTMLInputElement;
}

/**
 * Walks Identity → Model with valid values: name "Research Scout" (slug
 * auto-suggested to research-scout), a brief, provider openai, model gpt-5.
 */
async function advanceToCapabilities(): Promise<void> {
  await type("input-create-agent-name", "Research Scout");
  await type("input-create-agent-brief", "Finds and summarizes things.");
  await click("btn-wizard-next");

  const providerSelect = selectEl("select-create-provider");

  await act(async () => {
    fireEvent.change(providerSelect, { target: { value: "openai" } });
  });
  await waitFor(() => {
    const modelSelect = selectEl("select-create-model");
    expect(
      Array.from(modelSelect.options).some(
        (o: HTMLOptionElement): boolean => o.value === "gpt-5"
      )
    ).toBe(true);
  });

  await act(async () => {
    fireEvent.change(selectEl("select-create-model"), {
      target: { value: "gpt-5" },
    });
  });
  await click("btn-wizard-next");
}

beforeEach(() => {
  vi.clearAllMocks();
  existingAgents = [];
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

describe("CreateAgentModal wizard", () => {
  it("auto-suggests the slug from the name and stops once the slug is edited", async () => {
    await renderModal();

    await type("input-create-agent-name", "Research Scout");
    expect(fieldEl("input-create-agent-slug").value).toBe("research-scout");

    // A manual edit freezes the suggestion.
    await type("input-create-agent-slug", "my-own");
    await type("input-create-agent-name", "Renamed Agent");
    expect(fieldEl("input-create-agent-slug").value).toBe("my-own");
  });

  it("rejects an empty name on the Name field itself (per-field validation)", async () => {
    await renderModal();
    await click("btn-wizard-next");

    // Task 4.5: the message lands on the offending control — error text via
    // the field's message slot, danger border via aria-invalid — with no
    // detached message under the stepper.
    expect(
      screen.getByTestId("field-error-create-agent-name").textContent
    ).toContain("required");
    expect(
      fieldEl("input-create-agent-name").getAttribute("aria-invalid")
    ).toBe("true");
    expect(screen.queryByTestId("create-agent-step-error")).toBeNull();
    expect(mockedClient.agents.create).not.toHaveBeenCalled();
  });

  it("rejects an invalid slug at the Identity step", async () => {
    await renderModal();
    await type("input-create-agent-name", "Scout");
    await type("input-create-agent-slug", "Bad Slug!");
    await click("btn-wizard-next");

    expect(
      screen.getByTestId("field-error-create-agent-slug").textContent
    ).toContain("lowercase letters");
    expect(
      fieldEl("input-create-agent-slug").getAttribute("aria-invalid")
    ).toBe("true");
    expect(mockedClient.agents.create).not.toHaveBeenCalled();
  });

  it("rejects a duplicate slug at the Identity step", async () => {
    existingAgents = [makeAgent()];
    await renderModal();

    // The suggestion for "Scout" is the taken slug.
    await type("input-create-agent-name", "Scout");
    await type("input-create-agent-brief", "Anything.");
    await click("btn-wizard-next");

    expect(
      screen.getByTestId("field-error-create-agent-slug").textContent
    ).toContain("unique");
    expect(mockedClient.agents.create).not.toHaveBeenCalled();
  });

  it("requires the brief and explains that it drives prompt generation", async () => {
    await renderModal();
    await type("input-create-agent-name", "Scout");
    await click("btn-wizard-next");

    expect(
      screen.getByTestId("field-error-create-agent-brief").textContent
    ).toContain("drives prompt generation");
    expect(mockedClient.agents.create).not.toHaveBeenCalled();
  });

  it("requires a provider and model at the Model step", async () => {
    await renderModal();
    await type("input-create-agent-name", "Scout");
    await type("input-create-agent-brief", "Anything.");
    await click("btn-wizard-next");

    await click("btn-wizard-next");
    expect(
      screen.getByTestId("field-error-create-provider").textContent
    ).toContain("Select a provider");

    const providerSelect = selectEl("select-create-provider");

    await act(async () => {
      fireEvent.change(providerSelect, { target: { value: "openai" } });
    });
    await click("btn-wizard-next");
    expect(screen.queryByTestId("field-error-create-provider")).toBeNull();
    expect(
      screen.getByTestId("field-error-create-model").textContent
    ).toContain("Select a model");
    // The picker's closed control takes the danger chain with the message.
    expect(
      screen.getByTestId("select-create-model-trigger").className
    ).toContain("border-danger");
  });

  it("auto-fills the context window from the catalog and blanks it for unknown models", async () => {
    await renderModal();
    await type("input-create-agent-name", "Scout");
    await type("input-create-agent-brief", "Anything.");
    await click("btn-wizard-next");

    const providerSelect = selectEl("select-create-provider");

    await act(async () => {
      fireEvent.change(providerSelect, { target: { value: "openai" } });
    });
    await waitFor(() =>
      expect(
        Array.from(selectEl("select-create-model").options).some(
          (o: HTMLOptionElement): boolean => o.value === "gpt-5"
        )
      ).toBe(true)
    );

    await click("btn-create-advanced");
    await act(async () => {
      fireEvent.change(selectEl("select-create-model"), {
        target: { value: "gpt-5" },
      });
    });
    expect(fieldEl("input-create-context-window").value).toBe("400000");

    // An unknown model leaves the override blank (catalog default).
    await act(async () => {
      fireEvent.change(selectEl("select-create-model"), {
        target: { value: "custom-model" },
      });
    });
    expect(fieldEl("input-create-context-window").value).toBe("");
  });

  it("keeps a hand-edited context window over later model picks until Reset to auto", async () => {
    await renderModal();
    await type("input-create-agent-name", "Scout");
    await type("input-create-agent-brief", "Anything.");
    await click("btn-wizard-next");

    const providerSelect = selectEl("select-create-provider");

    await act(async () => {
      fireEvent.change(providerSelect, { target: { value: "openai" } });
    });
    await waitFor(() =>
      expect(
        Array.from(selectEl("select-create-model").options).some(
          (o: HTMLOptionElement): boolean => o.value === "gpt-5"
        )
      ).toBe(true)
    );

    await click("btn-create-advanced");
    await act(async () => {
      fireEvent.change(selectEl("select-create-model"), {
        target: { value: "gpt-5" },
      });
    });
    expect(fieldEl("input-create-context-window").value).toBe("400000");

    // A manual edit freezes the auto-fill…
    await type("input-create-context-window", "123456");
    await act(async () => {
      fireEvent.change(selectEl("select-create-model"), {
        target: { value: "gpt-4o" },
      });
    });
    expect(fieldEl("input-create-context-window").value).toBe("123456");

    // …and Reset to auto releases it back to the current model's catalog
    // value (gpt-4o, not the earlier gpt-5 pick).
    await click("btn-reset-context-window");
    expect(fieldEl("input-create-context-window").value).toBe("128000");
  });

  it("reaches models outside the provider list through the custom model entry", async () => {
    await renderModal();
    await type("input-create-agent-name", "Scout");
    await type("input-create-agent-brief", "Anything.");
    await click("btn-wizard-next");

    const providerSelect = selectEl("select-create-provider");

    await act(async () => {
      fireEvent.change(providerSelect, { target: { value: "openai" } });
    });
    await waitFor(() =>
      expect(
        Array.from(selectEl("select-create-model").options).some(
          (o: HTMLOptionElement): boolean => o.value === "__custom__"
        )
      ).toBe(true)
    );

    // Picking the sentinel swaps in the free-text field.
    await act(async () => {
      fireEvent.change(selectEl("select-create-model"), {
        target: { value: "__custom__" },
      });
    });

    await type("input-create-custom-model", "my-fine-tune");
    expect(fieldEl("input-create-custom-model").value).toBe("my-fine-tune");

    // The custom ID submits as the model, with the context override blank
    // (the catalog does not know it).
    await click("btn-wizard-next");
    await click("btn-create-agent");

    await waitFor(() => expect(onCreated).toHaveBeenCalled());
    expect(mockedClient.agents.create).toHaveBeenCalledWith(
      expect.objectContaining({
        Model: "my-fine-tune",
        ContextWindow: 0,
      })
    );
  });

  it("submits Create with the collected fields and closes on success", async () => {
    await renderModal();

    await type("input-create-agent-name", "Research Scout");
    await type("input-create-agent-description", "Finds things");
    await type("input-create-agent-brief", "Finds and summarizes things.");
    await click("btn-wizard-next");

    const providerSelect = selectEl("select-create-provider");

    await act(async () => {
      fireEvent.change(providerSelect, { target: { value: "openai" } });
    });
    await waitFor(() =>
      expect(
        Array.from(selectEl("select-create-model").options).some(
          (o: HTMLOptionElement): boolean => o.value === "gpt-5"
        )
      ).toBe(true)
    );
    await act(async () => {
      fireEvent.change(selectEl("select-create-model"), {
        target: { value: "gpt-5" },
      });
    });
    await click("btn-wizard-next");

    // Capabilities: opt into a skill; deselecting a tool materializes the
    // explicit list (all-on default), mirroring the config modal semantics.
    await click("create-skill-search");
    await click("create-tool-browser");
    await click("btn-create-agent");

    await waitFor(() => expect(onCreated).toHaveBeenCalled());
    expect(mockedClient.agents.create).toHaveBeenCalledWith({
      Name: "Research Scout",
      Slug: "research-scout",
      Description: "Finds things",
      Brief: "Finds and summarizes things.",
      // The picker's config rides along (random full config — any object).
      Avatar: expect.any(Object),
      ProviderID: "openai",
      Model: "gpt-5",
      // No wizard step sets a default effort yet; empty is the inherit state.
      Effort: "",
      Temperature: 1,
      MaxTokens: 0,
      ContextWindow: 400000,
      Tools: ["shell"],
      Skills: ["search"],
      MCPServers: [],
      // Nil means the column default (on) — the wizard has no subagents step.
      SubagentsEnabled: null,
    } satisfies CreateAgentInput);
    expect(onClose).toHaveBeenCalled();
  });

  it("submits the customized avatar config with the create call", async () => {
    await renderModal();

    // Expand the customize grid and pin one trait through its select (the
    // grid's first control is Sex).
    await click("btn-avatar-customize");

    // SAFETY: the customize grid's trait selects render selects by
    // construction; the first one is Sex.
    const sexSelect = document.querySelector(
      "[data-testid='create-agent-avatar-preview'] select"
    ) as HTMLSelectElement;

    await act(async () => {
      fireEvent.change(sexSelect, { target: { value: "woman" } });
    });

    await advanceToCapabilities();
    await click("btn-create-agent");

    await waitFor(() => expect(onCreated).toHaveBeenCalled());

    // SAFETY: vi.fn() calls carry the CreateAgentInput as the only argument.
    const input = mockedClient.agents.create.mock
      .calls[0][0] as CreateAgentInput;

    expect(input.Avatar).toEqual(expect.objectContaining({ sex: "woman" }));
  });

  it("offers every MCP server by default and pins the last one on", async () => {
    await renderModal();
    await advanceToCapabilities();

    // The fixture registers one server: on from the empty (every-server)
    // default and — because an empty list cannot mean "none" — pinned rather
    // than switchable off.
    // SAFETY: the Toggle renders a button element.
    const toggle = screen.getByTestId("create-mcp-mcp1") as HTMLButtonElement;
    expect(toggle.getAttribute("aria-checked")).toBe("true");
    expect(toggle.disabled).toBe(true);
    expect(screen.getByText("always on")).toBeTruthy();

    await click("btn-create-agent");
    await waitFor(() => expect(onCreated).toHaveBeenCalled());

    // SAFETY: vi.fn() calls carry the CreateAgentInput as the only argument.
    const input = mockedClient.agents.create.mock
      .calls[0][0] as CreateAgentInput;

    expect(input.MCPServers).toEqual([]);
  });

  it("materializes an explicit server list when one of several is turned off", async () => {
    mockedClient.settings.mcpServers.mockResolvedValueOnce([
      { ID: "mcp1", Name: "Files", Enabled: true },
      { ID: "mcp2", Name: "Jira", Enabled: true },
    ]);
    await renderModal();
    await advanceToCapabilities();

    const files = (): HTMLButtonElement =>
      // SAFETY: the Toggle renders a button element.
      screen.getByTestId("create-mcp-mcp1") as HTMLButtonElement;

    const jira = (): HTMLButtonElement =>
      // SAFETY: the Toggle renders a button element.
      screen.getByTestId("create-mcp-mcp2") as HTMLButtonElement;

    expect(files().getAttribute("aria-checked")).toBe("true");
    expect(jira().getAttribute("aria-checked")).toBe("true");
    expect(files().disabled).toBe(false);

    // Turning Files off leaves Jira as the explicit list — and pins it.
    await click("create-mcp-mcp1");
    expect(files().getAttribute("aria-checked")).toBe("false");
    expect(jira().getAttribute("aria-checked")).toBe("true");
    expect(jira().disabled).toBe(true);

    await click("btn-create-agent");
    await waitFor(() => expect(onCreated).toHaveBeenCalled());

    // SAFETY: vi.fn() calls carry the CreateAgentInput as the only argument.
    const input = mockedClient.agents.create.mock
      .calls[0][0] as CreateAgentInput;

    expect(input.MCPServers).toEqual(["mcp2"]);
  });

  it("shows the onboarding loader and the busy footer while create is in flight", async () => {
    mockedClient.agents.create.mockImplementationOnce(
      (): Promise<Agent> => new Promise<Agent>(() => undefined)
    );
    await renderModal();
    await advanceToCapabilities();

    await click("btn-create-agent");

    expect(screen.getByTestId("onboarding-loader")).toBeTruthy();
    expect(screen.getByTestId("onboarding-label").textContent).toBe(
      "Onboarding Research Scout…"
    );
    expect(screen.getByTestId("onboarding-line").textContent).toBe(
      "Reading the brief twice…"
    );

    // Busy footer: the submit button is disabled while in flight, the step
    // form is gone, and there is no Cancel to abandon the run.
    // SAFETY: the busy footer renders a button element.
    const busy = screen.getByTestId("btn-create-busy") as HTMLButtonElement;
    expect(busy.disabled).toBe(true);
    expect(screen.queryByTestId("wizard-step-identity")).toBeNull();
    expect(screen.queryByText("Cancel")).toBeNull();

    // Task 4.6: exactly ONE loading indicator during create — the
    // OnboardingLoader's spinner. The busy control reports in words only.
    expect(busy.textContent).toBe("Creating…");
    expect(busy.querySelector(".animate-spin")).toBeNull();
    expect(document.querySelectorAll(".animate-spin")).toHaveLength(1);
  });

  it("hands over the persisted failed row when create rejects with the slug present", async () => {
    mockedClient.agents.create.mockRejectedValueOnce(
      new Error("generation failed")
    );
    existingAgents = [];
    await renderModal();
    await advanceToCapabilities();

    // The backend keeps the row on a generation failure (design D4): the
    // list refresh after the rejection finds it by slug.
    mockedClient.agents.list.mockResolvedValueOnce([
      makeAgent({
        ID: "a-new",
        Name: "Research Scout",
        Slug: "research-scout",
        PromptsStatus: "failed",
        PromptsError: "boom",
      }),
    ]);

    await click("btn-create-agent");

    await waitFor(() => expect(onCreated).toHaveBeenCalled());
    // SAFETY: onCreated receives the failed Agent row.
    const handed = onCreated.mock.calls[0][0] as Agent;
    expect(handed.Slug).toBe("research-scout");
    expect(handed.PromptsStatus).toBe("failed");
    expect(onClose).toHaveBeenCalled();
  });

  it("keeps the wizard open with the error when no row was persisted", async () => {
    mockedClient.agents.create.mockRejectedValueOnce(
      new Error("generation failed")
    );
    await renderModal();
    await advanceToCapabilities();

    mockedClient.agents.list.mockResolvedValueOnce([]);
    await click("btn-create-agent");

    await waitFor(() =>
      expect(screen.getByTestId("create-agent-error").textContent).toContain(
        "generation failed"
      )
    );
    expect(onCreated).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
  });
});
