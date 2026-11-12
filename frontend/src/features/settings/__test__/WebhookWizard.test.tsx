import { act } from "react";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import userEvent from "@testing-library/user-event";
import type {
  Agent,
  AgentGroup,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  WebhookEndpointInput,
  WebhookEndpointView,
  WebhookPresetView,
  WebhookVerifierView,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client } from "../../../lib/client";

import { WebhookWizard } from "../components/webhooks/WebhookWizard";
import { renderUI, setInput } from "./test-utils";

let agents: Agent[];

let groups: AgentGroup[];

let presets: WebhookPresetView[];

let createError: Error | null = null;

/** The stdweb row the GitLab preset seeds. */
const GITLAB_ROW: WebhookVerifierView = {
  id: "p1",
  position: 0,
  kind: "stdweb",
  preset: "gitlab",
  headerName: "",
  timestampHeader: "",
  algo: "",
  encoding: "",
  prefix: "",
  scheme: "",
  secretSet: false,
  secretHint: "",
};

/** The raw-hmac row the GitHub preset seeds. */
const GITHUB_ROW: WebhookVerifierView = {
  id: "p2",
  position: 0,
  kind: "raw-hmac",
  preset: "github",
  headerName: "X-Hub-Signature-256",
  timestampHeader: "",
  algo: "sha256",
  encoding: "hex",
  prefix: "sha256=",
  scheme: "",
  secretSet: false,
  secretHint: "",
};

vi.mock("../../../lib/client", () => ({
  client: {
    agents: {
      list: vi.fn(async (): Promise<Agent[]> => [...agents]),
    },
    groups: {
      list: vi.fn(async (): Promise<AgentGroup[]> => [...groups]),
    },
    webhooks: {
      presets: vi.fn(async (): Promise<WebhookPresetView[]> => [...presets]),
      create: vi.fn(
        async (input: WebhookEndpointInput): Promise<WebhookEndpointView> => {
          if (createError !== null) throw createError;

          return makeView({ name: input.name, prompt: input.prompt });
        }
      ),
      update: vi.fn(
        async (
          _id: string,
          input: WebhookEndpointInput
        ): Promise<WebhookEndpointView> =>
          makeView({ name: input.name, prompt: input.prompt })
      ),
    },
  },
}));

import { client } from "../../../lib/client";

type MockFn = ReturnType<typeof vi.fn>;

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as Client & {
  webhooks: { presets: MockFn; create: MockFn; update: MockFn };
};

function makeAgent(patch: Partial<Agent> = {}): Agent {
  return {
    ID: "a1",
    Name: "Scout",
    Slug: "scout",
    Description: "",
    Brief: "",
    Avatar: null,
    PromptsStatus: "",
    PromptsError: "",
    ProviderID: "p1",
    Model: "m1",
    Effort: "",
    Temperature: 0,
    MaxTokens: 0,
    ContextWindow: 0,
    Tools: null,
    Skills: null,
    MCPServers: null,
    SubagentsEnabled: true,
    CreatedAt: "",
    UpdatedAt: "",
    ...patch,
  };
}

function makeGroup(patch: Partial<AgentGroup> = {}): AgentGroup {
  return {
    ID: "g1",
    Name: "ops",
    Title: "Ops Room",
    Instructions: "",
    LeadAgentID: null,
    Archived: false,
    Revision: 0,
    CreatedAt: "",
    UpdatedAt: "",
    ...patch,
  };
}

function makeView(
  patch: Partial<WebhookEndpointView> = {}
): WebhookEndpointView {
  return {
    id: "w-new",
    name: "GitLab deploys",
    agentId: "a1",
    groupId: "",
    targetName: "Scout",
    prompt: "Triage this build:\n<payload>",
    enabled: true,
    verifiers: null,
    url: "http://127.0.0.1:8787/webhooks/w-new/token123",
    lastOutcome: "",
    lastStatus: 0,
    lastAttemptedAt: "",
    createdAt: "",
    updatedAt: "",
    ...patch,
  };
}

/** Clipboard stub: jsdom has none; the wizard's CopyRow writes through it. */
const writeText = vi.fn(async (): Promise<void> => undefined);

/**
 * user-event's setup() REPLACES navigator.clipboard with its own stub, so
 * ours is installed after it — any earlier stub is invisible to the copy
 * handlers.
 */
function makeUser(): ReturnType<typeof userEvent.setup> {
  const user = userEvent.setup();

  Object.defineProperty(navigator, "clipboard", {
    value: { writeText },
    configurable: true,
  });

  return user;
}

async function renderWizard(endpoint: WebhookEndpointView | null = null) {
  const result = await renderUI(
    <WebhookWizard
      endpoint={endpoint}
      onClose={(): void => undefined}
      onSaved={(): void => undefined}
    />
  );

  return result;
}

/** Fills the Basics step and advances to Credentials. */
async function fillBasics(
  findByTestId: (id: string) => Promise<HTMLElement>
): Promise<void> {
  const user = makeUser();

  await setInput(await findByTestId("field-webhook-name"), "GitLab deploys");
  await user.selectOptions(
    await findByTestId("field-webhook-target"),
    "agent:a1"
  );
  await setInput(
    await findByTestId("field-webhook-prompt"),
    "Triage this build:\n<payload>"
  );
  await user.click(await findByTestId("btn-wizard-next"));
}

describe("WebhookWizard", () => {
  beforeEach(() => {
    agents = [makeAgent()];
    groups = [];
    presets = [
      {
        name: "gitlab",
        title: "GitLab",
        hint: "GitLab: project or group > Settings > Webhooks > edit the webhook's Secret token",
        verifiers: [GITLAB_ROW],
      },
      {
        name: "github",
        title: "GitHub",
        hint: "GitHub: repo Settings > Webhooks > Add webhook > Secret",
        verifiers: [GITHUB_ROW],
      },
    ];
    createError = null;
    vi.clearAllMocks();
    writeText.mockClear();
  });

  it("renders each gallery card with its where-to-find-it hint", async () => {
    const { findByTestId } = await renderWizard();

    // The gallery lives on the Credentials step.
    await fillBasics(findByTestId);

    expect((await findByTestId("preset-hint-gitlab")).textContent).toContain(
      "Settings > Webhooks"
    );
    expect((await findByTestId("preset-hint-github")).textContent).toContain(
      "Add webhook > Secret"
    );
    expect(await findByTestId("preset-card-custom")).toBeTruthy();
  });

  it("selecting a preset seeds the verifier set the save carries", async () => {
    const user = makeUser();
    const { findByTestId } = await renderWizard();

    await fillBasics(findByTestId);

    await user.click(await findByTestId("preset-card-select-gitlab"));
    await setInput(
      await findByTestId("preset-secret-gitlab"),
      "whsec_gitlab_1"
    );
    await user.click(await findByTestId("btn-save-webhook"));

    expect(mockedClient.webhooks.create).toHaveBeenCalledTimes(1);
    const input = mockedClient.webhooks.create.mock
      .calls[0][0] as WebhookEndpointInput;

    expect(input.name).toBe("GitLab deploys");
    expect(input.agentId).toBe("a1");
    expect(input.groupId).toBe("");
    expect(input.verifiers).toHaveLength(1);
    expect(input.verifiers![0].kind).toBe("stdweb");
    expect(input.verifiers![0].preset).toBe("gitlab");
    expect(input.verifiers![0].secret).toBe("whsec_gitlab_1");
  });

  it("targets a group: the save names the group, not an agent", async () => {
    groups = [makeGroup()];
    const user = makeUser();
    const { findByTestId } = await renderWizard();

    await setInput(await findByTestId("field-webhook-name"), "Ops intake");
    await user.selectOptions(
      await findByTestId("field-webhook-target"),
      "group:g1"
    );
    await setInput(
      await findByTestId("field-webhook-prompt"),
      "@Lead a webhook came in:\n<payload>"
    );
    await user.click(await findByTestId("btn-wizard-next"));
    await user.click(await findByTestId("preset-card-select-gitlab"));
    await user.click(await findByTestId("btn-save-webhook"));

    const input = mockedClient.webhooks.create.mock
      .calls[0][0] as WebhookEndpointInput;

    expect(input.groupId).toBe("g1");
    expect(input.agentId).toBe("");
  });

  it("shows the generated URL with a copy affordance after the save", async () => {
    const user = makeUser();
    const { findByTestId } = await renderWizard();

    await fillBasics(findByTestId);
    await user.click(await findByTestId("preset-card-select-gitlab"));
    await user.click(await findByTestId("btn-save-webhook"));

    expect((await findByTestId("webhook-url")).textContent).toBe(
      "http://127.0.0.1:8787/webhooks/w-new/token123"
    );

    await user.click(await findByTestId("webhook-url-copy"));

    // The copy rides the clipboard stub; the async resolve lands in act.
    await act(async (): Promise<void> => undefined);

    expect(writeText).toHaveBeenCalledWith(
      "http://127.0.0.1:8787/webhooks/w-new/token123"
    );
  });

  it("shows a session-entered secret exactly once with the note", async () => {
    const user = makeUser();
    const { findByTestId } = await renderWizard();

    await fillBasics(findByTestId);
    await user.click(await findByTestId("preset-card-select-github"));
    await setInput(await findByTestId("preset-secret-github"), "gh_secret_9");
    await user.click(await findByTestId("btn-save-webhook"));

    const shown = await findByTestId("shown-secret-0");

    expect(shown.textContent).toBe("gh_secret_9");
    expect((await findByTestId("shown-secret-copy-0")).textContent).toContain(
      "Copy"
    );

    // The "won't be shown again" note rides the block.
    expect(await findByTestId("webhook-step-finish")).toBeTruthy();
    expect((await findByTestId("webhook-step-finish")).textContent).toContain(
      "Won't be shown again"
    );
  });

  it("generate produces a fresh random secret that the save carries", async () => {
    const user = makeUser();
    const { findByTestId } = await renderWizard();

    await fillBasics(findByTestId);
    await user.click(await findByTestId("preset-card-select-gitlab"));
    await user.click(await findByTestId("preset-generate-gitlab"));
    await user.click(await findByTestId("btn-save-webhook"));

    const input = mockedClient.webhooks.create.mock
      .calls[0][0] as WebhookEndpointInput;

    expect(input.verifiers![0].secret).toMatch(/^[A-Za-z0-9_-]{40,}$/);
  });

  it("blocks the save without any verifier", async () => {
    const user = makeUser();
    const { findByTestId } = await renderWizard();

    await fillBasics(findByTestId);

    // No card selected: zero verifier rows.
    await user.click(await findByTestId("btn-save-webhook"));

    expect((await findByTestId("field-error-verifiers")).textContent).toContain(
      "At least one verifier is required"
    );
    expect(mockedClient.webhooks.create).not.toHaveBeenCalled();
  });

  it("round-trips an edit: preset identity preserved, rotated secret carried, blank keeps stored", async () => {
    const endpoint = makeView({
      id: "w1",
      name: "GitHub pulls",
      verifiers: [{ ...GITHUB_ROW, secretSet: true, secretHint: "9f8e" }],
    });
    const user = makeUser();
    const { findByTestId } = await renderWizard(endpoint);

    // Basics seed from the row; advance straight to Credentials.
    await user.click(await findByTestId("btn-wizard-next"));

    // The GitHub card is the current preset, with the stored-secret hint.
    expect(await findByTestId("preset-card-current-github")).toBeTruthy();

    // SAFETY: the testid names the card's kit Input, an HTMLInputElement.
    const secret = (await findByTestId(
      "preset-secret-github"
    )) as HTMLInputElement;

    expect(secret.value).toBe("");

    // Rotate: type a fresh secret and save.
    await setInput(await findByTestId("preset-secret-github"), "rotated_1");
    await user.click(await findByTestId("btn-save-webhook"));

    expect(mockedClient.webhooks.update).toHaveBeenCalledWith(
      "w1",
      expect.objectContaining({
        name: "GitHub pulls",
        verifiers: [
          expect.objectContaining({
            kind: "raw-hmac",
            preset: "github",
            headerName: "X-Hub-Signature-256",
            algo: "sha256",
            encoding: "hex",
            prefix: "sha256=",
            secret: "rotated_1",
          }),
        ],
      })
    );

    // The rotated secret is shown once.
    expect((await findByTestId("shown-secret-0")).textContent).toBe(
      "rotated_1"
    );
  });

  it("round-trips an edit saving the secret field blank (keeps stored)", async () => {
    const endpoint = makeView({
      id: "w1",
      verifiers: [{ ...GITLAB_ROW, secretSet: true, secretHint: "abcd" }],
    });
    const user = makeUser();
    const { findByTestId } = await renderWizard(endpoint);

    await user.click(await findByTestId("btn-wizard-next"));

    // Nothing typed: the blank secret rides the save, and no secret is
    // shown at the finish step.
    await user.click(await findByTestId("btn-save-webhook"));

    const input = mockedClient.webhooks.update.mock
      .calls[0][1] as WebhookEndpointInput;

    expect(input.verifiers![0].secret).toBe("");
    expect(await findByTestId("webhook-step-finish")).toBeTruthy();
  });

  it("surfaces a backend save rejection as the form error", async () => {
    createError = new Error(
      'save webhook endpoint: the name "GitLab deploys" is already in use'
    );
    const user = makeUser();
    const { findByTestId } = await renderWizard();

    await fillBasics(findByTestId);
    await user.click(await findByTestId("preset-card-select-gitlab"));
    await user.click(await findByTestId("btn-save-webhook"));

    expect((await findByTestId("webhook-wizard-error")).textContent).toContain(
      "already in use"
    );

    // The wizard stays on the credentials step; nothing was shown once.
    expect(await findByTestId("webhook-step-credentials")).toBeTruthy();
  });

  it("opens the manual builder from the Custom card", async () => {
    const user = makeUser();
    const { findByTestId } = await renderWizard();

    await fillBasics(findByTestId);
    await user.click(await findByTestId("preset-card-select-custom"));

    // One blank manual row of the default kind, editable in place.
    expect(await findByTestId("verifier-editor")).toBeTruthy();
    expect(await findByTestId("verifier-editor-row-0")).toBeTruthy();
    expect(await findByTestId("verifier-editor-add")).toBeTruthy();
  });
});
