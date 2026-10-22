import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import userEvent from "@testing-library/user-event";
import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  ModelCapability,
  ProviderInput,
  ProviderModelInput,
  ProviderModelView,
  ProviderView,
  VerifyResult,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client } from "../../../lib/client";

import ProvidersPane from "../components/ProvidersPane";
import { renderUI } from "./test-utils";

/** A secret-free row fixture as the backend's ProviderView returns it. */
function makeProviderView(patch: Partial<ProviderView> = {}): ProviderView {
  return {
    ID: "p1",
    Type: "anthropic",
    Name: "anthropic",
    BaseURL: "",
    Enabled: true,
    KeySet: true,
    KeyHint: "-key",
    CreatedAt: "",
    UpdatedAt: "",
    ...patch,
  };
}

/** Stored model row fixture for provider models. */
function makeModelView(
  patch: Partial<ProviderModelView> = {}
): ProviderModelView {
  return {
    ID: 1,
    ProviderID: "p1",
    ModelID: "claude-sonnet-4-5",
    ContextWindow: 200000,
    ImageInput: true,
    PDFInput: true,
    VideoInput: false,
    Efforts: null,
    Enabled: true,
    CreatedAt: "",
    UpdatedAt: "",
    ...patch,
  };
}

const providers: ProviderView[] = [];

const models: ProviderModelView[] = [];

const agents: Agent[] = [];

vi.mock("../../../lib/client", () => ({
  client: {
    settings: {
      providers: vi.fn(async () => [...providers]),
      createProvider: vi.fn(async (p: ProviderInput): Promise<ProviderView> => {
        const view: ProviderView = {
          ID: `p${providers.length + 1}`,
          Type: p.Type,
          Name: p.Name,
          BaseURL: p.BaseURL,
          Enabled: p.Enabled,
          KeySet: p.APIKey !== "",
          KeyHint: p.APIKey.slice(-4),
          CreatedAt: "",
          UpdatedAt: "",
        };

        providers.push(view);

        return view;
      }),
      updateProvider: vi.fn(
        async (id: string, p: ProviderInput): Promise<ProviderView> => {
          const i = providers.findIndex((x: ProviderView) => x.ID === id);

          if (i >= 0) {
            const next: ProviderView = {
              ...providers[i],
              Type: p.Type,
              Name: p.Name,
              BaseURL: p.BaseURL,
              Enabled: p.Enabled,
            };

            // Blank key keeps the stored one (design D2).
            if (p.APIKey !== "") {
              next.KeySet = true;
              next.KeyHint = p.APIKey.slice(-4);
            }

            providers[i] = next;
          }

          return providers[i];
        }
      ),
      deleteProvider: vi.fn(async (id: string): Promise<void> => {
        const i = providers.findIndex((x: ProviderView) => x.ID === id);

        if (i >= 0) providers.splice(i, 1);
      }),
      providerModels: vi.fn(async (): Promise<ModelCapability[]> => [
        {
          ID: "claude-sonnet-4-5",
          ContextWindow: 200000,
          Efforts: [],
          ImageInput: true,
          PDFInput: true,
          VideoInput: true,
        },
      ]),
      listProviderModels: vi.fn(
        async (providerID: string): Promise<ProviderModelView[]> => [
          ...models.filter(
            (m: ProviderModelView): boolean => m.ProviderID === providerID
          ),
        ]
      ),
      createModel: vi.fn(
        async (
          providerID: string,
          input: ProviderModelInput
        ): Promise<ProviderModelView> => {
          if (
            models.some(
              (m: ProviderModelView): boolean =>
                m.ProviderID === providerID && m.ModelID === input.ModelID
            )
          ) {
            throw new Error(
              `model "${input.ModelID}" already exists on provider`
            );
          }

          const nextID =
            models.reduce(
              (max: number, m: ProviderModelView): number =>
                Math.max(max, m.ID),
              0
            ) + 1;

          const created: ProviderModelView = {
            ID: nextID,
            ProviderID: providerID,
            ModelID: input.ModelID,
            ContextWindow: input.ContextWindow,
            ImageInput: input.ImageInput,
            PDFInput: input.PDFInput,
            VideoInput: input.VideoInput,
            Efforts: input.Efforts,
            Enabled: input.Enabled,
            CreatedAt: "",
            UpdatedAt: "",
          };

          models.push(created);

          return created;
        }
      ),
      updateModel: vi.fn(
        async (
          id: number,
          input: ProviderModelInput
        ): Promise<ProviderModelView> => {
          const i = models.findIndex(
            (m: ProviderModelView): boolean => m.ID === id
          );

          if (i < 0) throw new Error("model not found");
          models[i] = {
            ...models[i],
            ModelID: input.ModelID,
            ContextWindow: input.ContextWindow,
            ImageInput: input.ImageInput,
            PDFInput: input.PDFInput,
            VideoInput: input.VideoInput,
            Efforts: input.Efforts,
            Enabled: input.Enabled,
          };

          return models[i];
        }
      ),
      deleteModel: vi.fn(async (id: number): Promise<void> => {
        const i = models.findIndex(
          (m: ProviderModelView): boolean => m.ID === id
        );

        if (i >= 0) models.splice(i, 1);
      }),
      toggleModel: vi.fn(
        async (id: number, enabled: boolean): Promise<ProviderModelView> => {
          const i = models.findIndex(
            (m: ProviderModelView): boolean => m.ID === id
          );

          if (i < 0) throw new Error("model not found");
          models[i] = {
            ...models[i],
            Enabled: enabled,
          };

          return models[i];
        }
      ),
      verifyProvider: vi.fn(async (): Promise<VerifyResult> => ({
        OK: true,
        Detail: "",
      })),
      verifyProviderDraft: vi.fn(async (): Promise<VerifyResult> => ({
        OK: true,
        Detail: "",
      })),
    },
    agents: {
      list: vi.fn(async () => [...agents]),
    },
  },
}));

import { client } from "../../../lib/client";

type MockFn = ReturnType<typeof vi.fn>;

type MockedClient = Client & {
  settings: {
    providers: MockFn;
    createProvider: MockFn;
    updateProvider: MockFn;
    deleteProvider: MockFn;
    providerModels: MockFn;
    listProviderModels: MockFn;
    createModel: MockFn;
    updateModel: MockFn;
    deleteModel: MockFn;
    toggleModel: MockFn;
    verifyProvider: MockFn;
    verifyProviderDraft: MockFn;
  };
  agents: {
    list: MockFn;
  };
};

// relabel exposes the vi.fn members to the suite.
// SAFETY: the hoisted vi.mock builds exactly this Client shape; the
const mockedClient = client as MockedClient;

describe("ProvidersPane", () => {
  beforeEach(() => {
    providers.length = 0;
    models.length = 0;
    agents.length = 0;
    vi.clearAllMocks();
  });

  it("round-trips add: fill create form, createProvider called, list refreshes", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<ProvidersPane />);

    expect(await findByTestId("providers-empty")).toBeTruthy();

    await user.click(getByTestId("provider-add-empty"));
    await user.type(getByTestId("provider-name"), "openai");
    await user.type(getByTestId("provider-apikey"), "sk-openai-9999");
    await user.click(getByTestId("provider-save"));
    expect(mockedClient.settings.createProvider).toHaveBeenCalledWith(
      expect.objectContaining({ Name: "openai", APIKey: "sk-openai-9999" })
    );
    expect(await findByTestId("provider-list")).toBeTruthy();
  });

  it("edits a provider: blank write-only key on save, type locked", async () => {
    providers.push(makeProviderView());
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<ProvidersPane />);

    expect((await findByTestId("provider-key-p1")).textContent).toBe(
      "Key: ••••-key"
    );

    await user.click(getByTestId("provider-edit-p1"));
    // The form never shows the stored secret, only the non-secret hint.
    // HTMLInputElement by construction.
    // SAFETY: the query targets the pane's api-key field, an
    expect((getByTestId("provider-apikey") as HTMLInputElement).value).toBe("");
    // HTMLInputElement by construction.
    // SAFETY: the query targets the pane's api-key field, an
    expect(
      (getByTestId("provider-apikey") as HTMLInputElement).placeholder
    ).toContain("Leave blank to keep (••••-key)");
    // HTMLSelectElement by construction.
    // SAFETY: the query targets the pane's type select, an
    expect((getByTestId("provider-type") as HTMLSelectElement).disabled).toBe(
      true
    );

    await user.type(getByTestId("provider-name"), "-2");
    await user.click(getByTestId("provider-save"));

    // A blank key on edit preserves the stored secret (design D2).
    expect(mockedClient.settings.updateProvider).toHaveBeenCalledWith(
      "p1",
      expect.objectContaining({ Name: "anthropic-2", APIKey: "" })
    );
    expect(await findByTestId("provider-list")).toBeTruthy();
  });

  it("rejects a compatible type without a Base URL before any create call", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<ProvidersPane />);

    await user.click(await findByTestId("provider-add-empty"));
    await user.selectOptions(getByTestId("provider-type"), "openai-compatible");
    await user.type(getByTestId("provider-name"), "local proxy");
    await user.click(getByTestId("provider-save"));

    const message = getByTestId("provider-baseurl-error");
    expect(message.textContent).toContain(
      "Base URL is required for OpenAI-compatible"
    );
    expect(mockedClient.settings.createProvider).not.toHaveBeenCalled();
  });

  it("verifies the draft in the form without saving anything", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<ProvidersPane />);

    await user.click(await findByTestId("provider-add-empty"));
    await user.type(getByTestId("provider-apikey"), "sk-test-1234");
    await user.click(getByTestId("provider-verify-btn"));

    const result = await findByTestId("provider-verify-result");
    expect(result.textContent).toContain("Connection verified.");
    expect(mockedClient.settings.verifyProviderDraft).toHaveBeenCalledWith(
      expect.objectContaining({ Type: "anthropic", APIKey: "sk-test-1234" }),
      ""
    );
    expect(mockedClient.settings.createProvider).not.toHaveBeenCalled();
  });

  it("shows the failure detail when the draft verification fails", async () => {
    mockedClient.settings.verifyProviderDraft.mockResolvedValueOnce({
      OK: false,
      Detail: "401: invalid API key",
    } satisfies VerifyResult);
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<ProvidersPane />);

    await user.click(await findByTestId("provider-add-empty"));
    await user.click(getByTestId("provider-verify-btn"));

    expect((await findByTestId("provider-verify-result")).textContent).toBe(
      "401: invalid API key"
    );
  });

  it("toggles a provider off via updateProvider with a blank key", async () => {
    providers.push(makeProviderView());
    const user = userEvent.setup();
    const { findByTestId } = await renderUI(<ProvidersPane />);

    const toggle = await findByTestId("provider-toggle-p1");
    expect(toggle.getAttribute("role")).toBe("switch");
    expect(toggle.getAttribute("aria-checked")).toBe("true");
    expect(toggle.getAttribute("aria-label")).toBe("Disable anthropic");

    await user.click(toggle);

    expect(mockedClient.settings.updateProvider).toHaveBeenCalledWith(
      "p1",
      expect.objectContaining({ Enabled: false, APIKey: "" })
    );
    const row = await findByTestId("provider-p1");
    expect(row.className).toContain("opacity-70");
  });

  it("shows the row verification outcome without modifying the provider", async () => {
    providers.push(makeProviderView());
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<ProvidersPane />);

    await user.click(await findByTestId("provider-verify-btn-p1"));

    expect(await findByTestId("provider-verify-result-p1")).toBeTruthy();
    expect(mockedClient.settings.verifyProvider).toHaveBeenCalledWith("p1");
    expect(mockedClient.settings.updateProvider).not.toHaveBeenCalled();
    expect(getByTestId("provider-key-p1").textContent).toBe("Key: ••••-key");
  });

  it("shows the failure detail on the row when verification fails", async () => {
    providers.push(makeProviderView());
    mockedClient.settings.verifyProvider.mockResolvedValueOnce({
      OK: false,
      Detail: "401: invalid API key",
    } satisfies VerifyResult);
    const user = userEvent.setup();
    const { findByTestId } = await renderUI(<ProvidersPane />);

    await user.click(await findByTestId("provider-verify-btn-p1"));

    expect((await findByTestId("provider-verify-result-p1")).textContent).toBe(
      "401: invalid API key"
    );
  });

  it("renders key status from the secret-free view fields", async () => {
    providers.push(makeProviderView({ KeySet: false, KeyHint: "" }));
    const { findByTestId } = await renderUI(<ProvidersPane />);

    expect((await findByTestId("provider-key-p1")).textContent).toBe(
      "No key set"
    );
  });

  it("renders the onclaw card anatomy: sliders tile, type chip, mono URL", async () => {
    providers.push(makeProviderView({ BaseURL: "https://api.anthropic.com" }));

    const { findByTestId, getByText, getAllByText } = await renderUI(
      <ProvidersPane />
    );

    const row = await findByTestId("provider-p1");
    expect(row.querySelector("svg")).toBeTruthy();
    expect(getByText("https://api.anthropic.com").className).toContain(
      "font-mono"
    );
    expect(getAllByText("Anthropic").length).toBeGreaterThanOrEqual(1);
  });

  it("deletes an unused provider through the armed two-step confirm", async () => {
    providers.push(makeProviderView());
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<ProvidersPane />);

    await user.click(await findByTestId("provider-remove-p1"));
    const armed = getByTestId("provider-remove-p1");
    expect(armed.textContent).toBe("Click to confirm");
    expect(mockedClient.settings.deleteProvider).not.toHaveBeenCalled();

    await user.click(armed);

    expect(mockedClient.settings.deleteProvider).toHaveBeenCalledWith("p1");
  });

  it("warns before removing a provider an agent still uses", async () => {
    providers.push(makeProviderView());
    agents.push({
      ID: "a1",
      Name: "helper",
      ProviderID: "p1",
      Model: "",
      Effort: "",
      Slug: "scout",
      Description: "",
      Brief: "",
      Avatar: null,
      SubagentsEnabled: true,
      PromptsStatus: "ready",
      PromptsError: "",
      ContextWindow: 0,
      Temperature: 0,
      MaxTokens: 0,
      Tools: null,
      Skills: null,
      MCPServers: null,
      CreatedAt: "",
      UpdatedAt: "",
    });
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<ProvidersPane />);

    await user.click(await findByTestId("provider-remove-p1"));
    const warning = await findByTestId("provider-in-use-warning");
    expect(warning.textContent).toContain("helper");

    await user.click(getByTestId("provider-remove-confirm"));
    expect(mockedClient.settings.deleteProvider).toHaveBeenCalledWith("p1");
  });

  it("reads the credential line in the mono face", async () => {
    providers.push(makeProviderView());
    const { findByTestId } = await renderUI(<ProvidersPane />);

    const badge = await findByTestId("provider-key-p1");
    expect(badge.textContent).toBe("Key: ••••-key");
    expect(badge.querySelector(".font-mono")).toBeTruthy();
  });

  it("keeps a clear action hierarchy: Verify, toggle, edit pencil, quiet remove", async () => {
    providers.push(makeProviderView());

    const { container, findByTestId } = await renderUI(<ProvidersPane />);

    await findByTestId("provider-p1");

    // stays possible and the assertion below requires it.
    // SAFETY: the selector matches the pane's own edit control; null
    const edit = container.querySelector(
      '[data-testid="provider-edit-p1"]'
    ) as HTMLButtonElement | null;

    expect(edit?.getAttribute("aria-label")).toBe("Edit anthropic");

    // stays possible and the assertion below requires it.
    // SAFETY: the selector matches the pane's own remove control; null
    const remove = container.querySelector(
      '[data-testid="provider-remove-p1"]'
    ) as HTMLButtonElement | null;

    expect(remove?.getAttribute("aria-label")).toBe("Remove anthropic");
    expect(remove?.textContent).not.toContain("Remove");
  });

  it("offers Add your first provider as the empty state's next-step action", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<ProvidersPane />);

    expect(await findByTestId("providers-empty")).toBeTruthy();

    await user.click(getByTestId("provider-add-empty"));
    expect(getByTestId("provider-name")).toBeTruthy();
    expect(getByTestId("provider-cancel")).toBeTruthy();

    await user.click(getByTestId("provider-cancel"));
    expect(await findByTestId("providers-empty")).toBeTruthy();
  });

  // Task 5.1 & 5.2: Groups canonical and custom providers, and supports selecting rows
  it("groups canonical and custom providers and switches selection", async () => {
    providers.push(
      makeProviderView({ ID: "p1", Type: "anthropic", Name: "Anthropic Main" }),
      makeProviderView({ ID: "p2", Type: "openrouter", Name: "OpenRouter Hub" })
    );
    const user = userEvent.setup();

    const { getAllByText, getByText, getByTestId, findByTestId } =
      await renderUI(<ProvidersPane />);

    expect(await findByTestId("provider-list")).toBeTruthy();
    expect(getAllByText("Providers").length).toBeGreaterThanOrEqual(2);
    expect(getByText("Custom providers")).toBeTruthy();

    // Auto-selects first provider on load
    expect(getByTestId("provider-p1").getAttribute("aria-selected")).toBe(
      "true"
    );
    expect(getByTestId("provider-p2").getAttribute("aria-selected")).toBe(
      "false"
    );

    // Click second provider
    await user.click(getByTestId("provider-p2"));
    expect(getByTestId("provider-p2").getAttribute("aria-selected")).toBe(
      "true"
    );
    expect(getByTestId("provider-p1").getAttribute("aria-selected")).toBe(
      "false"
    );
  });

  // Task 5.4: Credential secrecy UI and reveal toggle
  it("toggles key reveal between password and text without revealing stored secret", async () => {
    providers.push(makeProviderView({ KeySet: true, KeyHint: "abcd" }));
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<ProvidersPane />);

    await findByTestId("provider-p1");
    // HTMLInputElement by construction.
    // SAFETY: the query targets the edit form's api-key field, an
    const keyInput = getByTestId("provider-apikey") as HTMLInputElement;

    // Initially blank with placeholder
    expect(keyInput.value).toBe("");
    expect(keyInput.type).toBe("password");
    expect(keyInput.placeholder).toContain("••••abcd");

    // Click reveal toggle
    const revealBtn = getByTestId("provider-apikey-reveal");
    await user.click(revealBtn);
    expect(keyInput.type).toBe("text");
    // Even when revealed, empty stored key is never displayed
    expect(keyInput.value).toBe("");

    // Type new draft text
    await user.type(keyInput, "my-new-secret");
    expect(keyInput.value).toBe("my-new-secret");

    // Toggle back to password
    await user.click(revealBtn);
    expect(keyInput.type).toBe("password");
  });

  // Task 5.1–5.3: Model list CRUD with input checkboxes, video badges, and
  // effort rows.
  it("loads, adds, edits, toggles, and deletes provider models", async () => {
    providers.push(makeProviderView({ ID: "p1" }));
    models.push(
      makeModelView({
        ID: 10,
        ProviderID: "p1",
        ModelID: "claude-3-5-sonnet",
        ContextWindow: 200000,
        ImageInput: true,
        PDFInput: true,
        VideoInput: true,
        Efforts: ["none", "high"],
        Enabled: true,
      }),
      makeModelView({
        ID: 11,
        ProviderID: "p1",
        ModelID: "gpt-4o-mini",
        ContextWindow: 128000,
        ImageInput: false,
        PDFInput: false,
        VideoInput: false,
        Efforts: null,
        Enabled: true,
      })
    );
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <ProvidersPane />
    );

    // Initial model rows render capability badges per declaration.
    expect(await findByTestId("model-row-10")).toBeTruthy();
    expect(getByTestId("model-id-10").textContent).toBe("claude-3-5-sonnet");
    expect(getByTestId("model-context-10").textContent).toBe("200k");
    expect(getByTestId("model-vision-10").textContent).toBe("Vision");
    expect(getByTestId("model-pdf-10").textContent).toBe("PDF");
    expect(getByTestId("model-video-10").textContent).toBe("Video");
    expect(queryByTestId("model-video-11")).toBeNull();

    // Toggle model
    await user.click(getByTestId("model-toggle-10"));
    expect(mockedClient.settings.toggleModel).toHaveBeenCalledWith(10, false);

    // Add model: image + video inputs and two declared efforts in order.
    await user.click(getByTestId("model-add-btn"));
    expect(getByTestId("model-form")).toBeTruthy();
    await user.type(getByTestId("model-form-id"), "claude-3-haiku");
    await user.clear(getByTestId("model-form-context"));
    await user.type(getByTestId("model-form-context"), "128000");
    await user.click(getByTestId("model-form-image"));
    await user.click(getByTestId("model-form-video"));
    await user.click(getByTestId("model-form-effort-add"));
    await user.type(getByTestId("model-form-effort-0"), "none");
    await user.click(getByTestId("model-form-effort-add"));
    await user.type(getByTestId("model-form-effort-1"), "high");
    await user.click(getByTestId("model-form-save"));

    expect(mockedClient.settings.createModel).toHaveBeenCalledWith(
      "p1",
      expect.objectContaining({
        ModelID: "claude-3-haiku",
        ContextWindow: 128000,
        ImageInput: true,
        PDFInput: false,
        VideoInput: true,
        Efforts: ["none", "high"],
      })
    );

    // A second add with zero effort rows saves undeclared.
    await user.click(getByTestId("model-add-btn"));
    await user.type(getByTestId("model-form-id"), "gpt-4o");
    await user.click(getByTestId("model-form-save"));

    expect(mockedClient.settings.createModel).toHaveBeenLastCalledWith(
      "p1",
      expect.objectContaining({
        ModelID: "gpt-4o",
        Efforts: [],
      })
    );

    // Edit hydrates the declared row's stored values.
    await user.click(getByTestId("model-edit-10"));
    expect(getByTestId("model-form")).toBeTruthy();
    // SAFETY: the query targets the pane's effort field, an HTMLInputElement
    // by construction.
    expect((getByTestId("model-form-effort-0") as HTMLInputElement).value).toBe(
      "none"
    );
    // SAFETY: the query targets the pane's effort field, an HTMLInputElement
    // by construction.
    expect((getByTestId("model-form-effort-1") as HTMLInputElement).value).toBe(
      "high"
    );
    // SAFETY: the query targets the pane's video checkbox, an HTMLInputElement
    // by construction.
    expect((getByTestId("model-form-video") as HTMLInputElement).checked).toBe(
      true
    );

    // Removing the first effort row leaves the second declaration.
    await user.click(getByTestId("model-form-effort-remove-0"));
    // SAFETY: the query targets the pane's effort field, an HTMLInputElement
    // by construction.
    expect((getByTestId("model-form-effort-0") as HTMLInputElement).value).toBe(
      "high"
    );
    await user.click(getByTestId("model-form-save"));

    expect(mockedClient.settings.updateModel).toHaveBeenCalledWith(
      10,
      expect.objectContaining({
        Efforts: ["high"],
      })
    );

    // Delete model
    await user.click(getByTestId("model-delete-10"));
    expect(mockedClient.settings.deleteModel).toHaveBeenCalledWith(10);
    expect(queryByTestId("model-row-10")).toBeNull();
  });

  // Task 5.3: Duplicate model rejection in form
  it("displays duplicate error when adding an existing model ID", async () => {
    providers.push(makeProviderView({ ID: "p1" }));
    models.push(
      makeModelView({
        ID: 10,
        ProviderID: "p1",
        ModelID: "duplicate-model",
      })
    );
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(<ProvidersPane />);

    await findByTestId("model-row-10");
    await user.click(getByTestId("model-add-btn"));
    await user.type(getByTestId("model-form-id"), "duplicate-model");
    await user.click(getByTestId("model-form-save"));

    const err = await findByTestId("model-form-error");
    expect(err.textContent).toContain("already exists");
  });

  // Task 5.1: with Refresh gone, the models error alert still reports the
  // row toggle (and delete) failure paths inline.
  it("reports a failed model toggle inline under models-error", async () => {
    providers.push(makeProviderView({ ID: "p1" }));
    models.push(makeModelView({ ID: 10, ProviderID: "p1" }));
    mockedClient.settings.toggleModel.mockRejectedValueOnce(
      new Error("toggle boom")
    );
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(<ProvidersPane />);

    await user.click(await findByTestId("model-toggle-10"));

    const err = await findByTestId("models-error");
    expect(err.textContent).toContain("toggle boom");
  });
});
