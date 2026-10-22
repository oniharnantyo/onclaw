/**
 * ScheduleEditorModal component tests (task 7.2, design D11/D12/D13): the
 * debounced server preview drives the summary and next-three-fires block,
 * invalid expressions show the server's inline recurrence error and block
 * Save, the add form opens without inline errors (they appear once a field
 * is touched), edits reconstruct presets (or open Advanced), Save sends
 * exactly an Input-shaped payload (D13), and backend validation errors land
 * on their field. The lib client is mocked wholesale.
 */
import { act } from "react";
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
import type { ScheduleView } from "../../../lib/client";
import type { Client } from "../../../lib/client";
import { renderUI } from "../../../__test__/test-utils";

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

// Import after the mock is registered.
import { client } from "../../../lib/client";
import { ScheduleEditorModal } from "../components/ScheduleEditorModal";

// SAFETY: the client module is mocked with makeClient(), so its runtime
// shape is exactly this mock object.
const mockedClient = client as MockedClient;

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  schedules: {
    list: MockFn;
    get: MockFn;
    create: MockFn;
    update: MockFn;
    setEnabled: MockFn;
    delete: MockFn;
    runNow: MockFn;
    preview: MockFn;
  };
};

function makeSchedule(overrides: Partial<ScheduleView> = {}): ScheduleView {
  return {
    id: "s1",
    name: "Standup digest",
    agentId: "a1",
    prompt: "Digest the standups",
    cron: "0 9 * * 1-5",
    description: "Every weekday at 09:00",
    enabled: true,
    nextFireAt: new Date(Date.now() + 2 * 3600_000).toISOString(),
    lastRunAt: new Date(Date.now() - 3600_000).toISOString(),
    lastRunStatus: "ran",
    lastRunError: "",
    lastRunSessionId: "sess-1",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

const agents: Agent[] = [
  {
    ID: "a1",
    Name: "Scout",
    Slug: "scout",
    Description: "",
    Brief: "",
    Avatar: null,
    SubagentsEnabled: true,
    PromptsStatus: "ready",
    PromptsError: "",
    ProviderID: "prov",
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
  },
  {
    ID: "a2",
    Name: "Sentinel",
    Slug: "sentinel",
    Description: "",
    Brief: "",
    Avatar: null,
    SubagentsEnabled: true,
    PromptsStatus: "ready",
    PromptsError: "",
    ProviderID: "prov",
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
  },
];

/** Preview results keyed by the expression they answer. */
let previews: Record<string, ScheduleEditorPreview>;

/** When set, the preview call never resolves (the pending block test);
 * reset per test so the stall cannot leak across cases. */
let previewHangs = false;

interface ScheduleEditorPreview {
  ok: boolean;
  error?: string;
  description?: string;
  next?: string[] | null;
}

function makeClient(): MockedClient {
  const c = {
    schedules: {
      list: vi.fn(async (): Promise<ScheduleView[]> => []),
      get: vi.fn(async (): Promise<ScheduleView> => makeSchedule()),
      create: vi.fn(async (): Promise<ScheduleView> => makeSchedule()),
      update: vi.fn(async (_id: string): Promise<ScheduleView> =>
        makeSchedule()
      ),
      setEnabled: vi.fn(async (): Promise<ScheduleView> => makeSchedule()),
      delete: vi.fn(async (): Promise<void> => undefined),
      runNow: vi.fn(async (): Promise<ScheduleView> => makeSchedule()),
      preview: vi.fn(async (expr: string): Promise<ScheduleEditorPreview> => {
        if (previewHangs)
          return new Promise<ScheduleEditorPreview>(() => undefined);

        return (
          previews[expr] ?? {
            ok: true,
            description: "Every day at 09:00",
            next: [
              "2026-10-22T09:00:00",
              "2026-10-23T09:00:00",
              "2026-10-24T09:00:00",
            ],
          }
        );
      }),
    },
  };

  // SAFETY: makeClient() builds exactly this mock shape; the relabel
  // funnels through the same audited conversion the other suites use.
  return c as MockedClient;
}

interface RenderModalOptions {
  schedule?: ScheduleView;
  onSaved?: () => void;
  onClose?: () => void;
}

const unmounts: Array<() => Promise<void>> = [];

/** Reads a rendered kit Button: an HTMLButtonElement by construction —
 * narrowed through instanceof, no assertion. */
function buttonOf(testId: string): HTMLButtonElement {
  const el = screen.getByTestId(testId);

  if (!(el instanceof HTMLButtonElement))
    throw new Error(`${testId} is not a button`);

  return el;
}

/** Reads a rendered kit Input: an HTMLInputElement by construction. */
function inputOf(testId: string): HTMLInputElement {
  const el = screen.getByTestId(testId);

  if (!(el instanceof HTMLInputElement))
    throw new Error(`${testId} is not an input`);

  return el;
}

async function renderModal(options: RenderModalOptions = {}): Promise<void> {
  const view = await renderUI(
    <ScheduleEditorModal
      open
      schedule={options.schedule}
      agents={agents}
      onClose={options.onClose ?? vi.fn()}
      onSaved={options.onSaved ?? vi.fn()}
    />
  );

  unmounts.push(view.unmount);
}

beforeEach(() => {
  vi.clearAllMocks();
  previews = {};
  previewHangs = false;
});

afterEach(async (): Promise<void> => {
  while (unmounts.length > 0) {
    const unmount = unmounts.pop();

    if (unmount) await unmount();
  }

  document.body.innerHTML = "";
});

/** Types into a field inside an act batch. */
async function type(testId: string, value: string): Promise<void> {
  await act(async () => {
    fireEvent.change(screen.getByTestId(testId), {
      target: { value },
    });
  });
}

describe("ScheduleEditorModal", () => {
  it("previews the daily default and shows the next three fires", async () => {
    previews["0 9 * * *"] = {
      ok: true,
      description: "Daily at 09:00",
      next: [
        "2026-10-22T09:00:00",
        "2026-10-23T09:00:00",
        "2026-10-24T09:00:00",
      ],
    };
    await renderModal();

    await waitFor(() =>
      expect(mockedClient.schedules.preview).toHaveBeenCalledWith("0 9 * * *")
    );
    await waitFor(() =>
      expect(screen.getByTestId("recurrence-fire-0")).toBeTruthy()
    );
    expect(screen.getByTestId("recurrence-fire-1")).toBeTruthy();
    expect(screen.getByTestId("recurrence-fire-2")).toBeTruthy();
    expect(screen.getByTestId("recurrence-preview").textContent).toContain(
      "Daily at 09:00"
    );
    // The fixed local-time note names the host zone (design D5).
    expect(screen.getByTestId("recurrence-preview").textContent).toContain(
      `Times are local (${
        Intl.DateTimeFormat().resolvedOptions().timeZone ?? "local time"
      })`
    );
  });

  it("composes weekly Mon+Fri 17:30 into `30 17 * * 1,5` for the preview", async () => {
    await renderModal();

    await act(async () => {
      fireEvent.click(screen.getByTestId("recurrence-preset-weekly"));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("recurrence-weekday-5"));
    });
    await type("recurrence-time", "17:30");

    await waitFor(() =>
      expect(mockedClient.schedules.preview).toHaveBeenCalledWith(
        "30 17 * * 1,5"
      )
    );
  });

  it("reports an invalid advanced expression inline and disables Save", async () => {
    previews["0 25 * * *"] = {
      ok: false,
      error: "recurrence must be five fields: minute hour dom month dow",
    };
    await renderModal();

    await act(async () => {
      fireEvent.click(screen.getByTestId("recurrence-preset-advanced"));
    });
    await type("recurrence-advanced-input", "0 25 * * *");

    const error = await waitFor(() => screen.getByTestId("recurrence-error"));

    expect(error.textContent).toContain(
      "recurrence must be five fields: minute hour dom month dow"
    );
    // The preview block empties: no stale fires remain.
    expect(screen.queryByTestId("recurrence-fire-0")).toBeNull();
    const save = buttonOf("btn-save-schedule");

    expect(save.disabled).toBe(true);
  });

  it("keeps Save disabled while the preview is pending", async () => {
    previewHangs = true;
    await renderModal();

    const save = buttonOf("btn-save-schedule");

    expect(save.disabled).toBe(true);

    // Filling the text fields does not lift the block while pending.
    await type("field-name", "Digest");
    await type("field-prompt", "Do the thing");
    expect(save.disabled).toBe(true);
  });

  it("opens quiet with Save blocked and shows required errors only after interaction", async () => {
    await renderModal();

    // A fresh add form shows no inline errors before any interaction.
    expect(screen.queryByTestId("field-error-name")).toBeNull();
    expect(screen.queryByTestId("field-error-prompt")).toBeNull();
    expect(buttonOf("btn-save-schedule").disabled).toBe(true);
    expect(mockedClient.schedules.create).not.toHaveBeenCalled();

    // Touching a field and leaving it empty surfaces that field's error
    // alone (type then clear).
    await type("field-name", "D");
    await type("field-name", "");
    expect(screen.getByTestId("field-error-name").textContent).toContain(
      "required"
    );
    expect(screen.queryByTestId("field-error-prompt")).toBeNull();

    // Filling both fields clears the error and lifts the Save block.
    await type("field-name", "Digest");
    await type("field-prompt", "Do the thing");

    await waitFor(() =>
      expect(buttonOf("btn-save-schedule").disabled).toBe(false)
    );
    expect(screen.queryByTestId("field-error-name")).toBeNull();
  });

  it("sends exactly an Input-shaped payload to create and closes via onSaved", async () => {
    const onSaved = vi.fn();
    const onClose = vi.fn();
    await renderModal({ onSaved, onClose });

    await type("field-name", "Digest");
    await type("field-prompt", "Do the thing");
    await waitFor(() =>
      expect(buttonOf("btn-save-schedule").disabled).toBe(false)
    );
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-schedule"));
    });

    await waitFor(() =>
      expect(mockedClient.schedules.create).toHaveBeenCalledTimes(1)
    );
    // D13: exactly the five input fields, no timestamps.
    expect(mockedClient.schedules.create).toHaveBeenCalledWith({
      name: "Digest",
      agentId: "a1",
      prompt: "Do the thing",
      cron: "0 9 * * *",
      enabled: true,
    });
    expect(onSaved).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
  });

  it("routes an edit save through update with the schedule's id", async () => {
    const onSaved = vi.fn();
    await renderModal({ schedule: makeSchedule(), onSaved });

    await waitFor(() =>
      expect(buttonOf("btn-save-schedule").disabled).toBe(false)
    );
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-schedule"));
    });

    await waitFor(() =>
      expect(mockedClient.schedules.update).toHaveBeenCalledTimes(1)
    );
    expect(mockedClient.schedules.update).toHaveBeenCalledWith(
      "s1",
      expect.objectContaining({ name: "Standup digest", cron: "0 9 * * 1-5" })
    );
    expect(onSaved).toHaveBeenCalledTimes(1);
  });

  it("lands a backend name-validation error on the name field", async () => {
    mockedClient.schedules.create.mockRejectedValueOnce(
      new Error('name: a schedule named "Digest" already exists (id abc)')
    );
    await renderModal();

    await type("field-name", "Digest");
    await type("field-prompt", "Do the thing");
    await waitFor(() =>
      expect(buttonOf("btn-save-schedule").disabled).toBe(false)
    );
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-schedule"));
    });

    const fieldError = await waitFor(() =>
      screen.getByTestId("field-error-name")
    );

    expect(fieldError.textContent).toContain(
      'a schedule named "Digest" already exists (id abc)'
    );
  });

  it("surfaces an unknown backend error shape at the form level", async () => {
    mockedClient.schedules.create.mockRejectedValueOnce(
      new Error("store unavailable")
    );
    await renderModal();

    await type("field-name", "Digest");
    await type("field-prompt", "Do the thing");
    await waitFor(() =>
      expect(buttonOf("btn-save-schedule").disabled).toBe(false)
    );
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-save-schedule"));
    });

    const formError = await waitFor(() =>
      screen.getByTestId("schedule-editor-error")
    );

    expect(formError.textContent).toContain("store unavailable");
  });

  it("reconstructs the weekly preset from a stored expression", async () => {
    await renderModal({
      schedule: makeSchedule({ cron: "30 17 * * 1,5" }),
    });

    expect(
      screen
        .getByTestId("recurrence-preset-weekly")
        .getAttribute("aria-checked")
    ).toBe("true");
    expect(
      screen.getByTestId("recurrence-weekday-1").getAttribute("aria-pressed")
    ).toBe("true");
    expect(
      screen.getByTestId("recurrence-weekday-5").getAttribute("aria-pressed")
    ).toBe("true");
    expect(inputOf("recurrence-time").value).toBe("17:30");
    expect(inputOf("field-name").value).toBe("Standup digest");
  });

  it("opens Advanced for a stored non-preset expression", async () => {
    await renderModal({
      schedule: makeSchedule({ cron: "*/15 9-17 * * 1-5" }),
    });

    expect(
      screen
        .getByTestId("recurrence-preset-advanced")
        .getAttribute("aria-checked")
    ).toBe("true");
    expect(inputOf("recurrence-advanced-input").value).toBe(
      "*/15 9-17 * * 1-5"
    );
  });

  it("renders nothing when open is false", async () => {
    const view = await renderUI(
      <ScheduleEditorModal
        open={false}
        agents={agents}
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />
    );

    expect(screen.queryByTestId("schedule-editor")).toBeNull();
    await view.unmount();
  });
});
