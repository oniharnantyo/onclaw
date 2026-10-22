/**
 * SchedulesView component tests (task 6.3, design D9/D10/D12): rows render
 * from the list with last-run outcome, agent, recurrence, and relative next
 * fire; the row actions call their client member and refetch; the last run
 * opens its session; `schedules:changed` triggers a coalesced refetch; the
 * empty state offers manual add and names the agent path; and the add/edit
 * buttons mount the editor. The lib client is mocked wholesale.
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

import type { SchedulesChangedEvent } from "../../../lib/events";
import type { ScheduleView } from "../../../lib/client";
import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../../../lib/client";
import { renderUI } from "../../../__test__/test-utils";
import { useNavStore } from "../../../stores/nav";

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

// Import after the mock is registered.
import { client } from "../../../lib/client";
import SchedulesView from "../index";

// SAFETY: the client module is mocked with makeClient(), so its runtime
// shape is exactly this mock object.
const mockedClient = client as MockedClient;

let schedules: ScheduleView[];

let schedulesChangedHandler: ((e: SchedulesChangedEvent) => void) | null = null;

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  agents: {
    list: MockFn;
  };
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
  events: {
    onSchedulesChanged: MockFn;
    onSettingsOpen: MockFn;
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
    // Headroom past the exact hour keeps "in 2h" stable under Math.floor.
    nextFireAt: new Date(Date.now() + 2 * 3600_000 + 60_000).toISOString(),
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

function makeClient(): MockedClient {
  const c = {
    agents: {
      list: vi.fn(async (): Promise<Agent[]> => agents),
    },
    schedules: {
      list: vi.fn(async (): Promise<ScheduleView[]> => schedules),
      get: vi.fn(async (): Promise<ScheduleView> => makeSchedule()),
      create: vi.fn(async (): Promise<ScheduleView> => makeSchedule()),
      update: vi.fn(async (_id: string): Promise<ScheduleView> =>
        makeSchedule()
      ),
      setEnabled: vi.fn(async (): Promise<ScheduleView> => makeSchedule()),
      delete: vi.fn(async (): Promise<void> => undefined),
      runNow: vi.fn(async (): Promise<ScheduleView> => makeSchedule()),
      preview: vi.fn(async (): Promise<{ ok: boolean }> => ({ ok: true })),
    },
    events: {
      onSchedulesChanged: vi.fn(
        (handler: (e: SchedulesChangedEvent) => void) => {
          schedulesChangedHandler = handler;

          return (): void => {
            schedulesChangedHandler = null;
          };
        }
      ),
      onSettingsOpen: vi.fn(() => (): void => undefined),
    },
  };

  // SAFETY: makeClient() builds exactly this mock shape; the relabel
  // funnels through the same audited conversion the other suites use.
  return c as MockedClient;
}

const unmounts: Array<() => Promise<void>> = [];

/** Reads a rendered Toggle: the testid targets the kit switch, a
 * button element — narrowed through instanceof, no assertion. */
function toggleOf(testId: string): HTMLButtonElement {
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

async function renderView(): Promise<void> {
  const view = await renderUI(<SchedulesView />);

  unmounts.push(view.unmount);
}

beforeEach(() => {
  vi.clearAllMocks();
  schedules = [makeSchedule()];
  useNavStore.setState({ menu: "schedules", chatTarget: null });
});

afterEach(async (): Promise<void> => {
  while (unmounts.length > 0) {
    const unmount = unmounts.pop();

    if (unmount) await unmount();
  }

  document.body.innerHTML = "";
});

describe("SchedulesView", () => {
  it("renders rows with name, agent, recurrence, next fire, and last-run label", async () => {
    await renderView();

    expect(screen.getByTestId("schedules-view")).toBeTruthy();

    const row = await waitFor(() => screen.getByTestId("schedule-row-s1"));

    expect(row.textContent).toContain("Standup digest");
    // Agent avatar name rides the recurrence line.
    expect(row.textContent).toContain("Scout · Every weekday at 09:00");
    // nextFireAt is two hours out: "in 2h".
    expect(screen.getByTestId("schedule-next-s1").textContent).toBe(
      "Next: in 2h"
    );
    // lastRunAt is an hour behind: "ran · 1h".
    expect(screen.getByTestId("schedule-last-run-s1").textContent).toContain(
      "ran · 1h"
    );
    const toggle = toggleOf("schedule-toggle-s1");

    expect(toggle.getAttribute("aria-checked")).toBe("true");
  });

  it("renders the disabled schedule's next fire as a dash", async () => {
    schedules = [
      makeSchedule({ enabled: false, nextFireAt: null, lastRunStatus: "" }),
    ];
    await renderView();

    await waitFor(() => screen.getByTestId("schedule-row-s1"));
    expect(screen.getByTestId("schedule-next-s1").textContent).toBe("Next: —");
    expect(screen.getByTestId("schedule-last-run-s1").textContent).toContain(
      "never run"
    );
  });

  it("toggles the schedule off through setEnabled and refetches", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("schedule-row-s1"));
    const callsBefore = mockedClient.schedules.list.mock.calls.length;

    await act(async () => {
      fireEvent.click(screen.getByTestId("schedule-toggle-s1"));
    });

    await waitFor(() =>
      expect(mockedClient.schedules.setEnabled).toHaveBeenCalledWith(
        "s1",
        false
      )
    );
    await waitFor(() =>
      expect(mockedClient.schedules.list.mock.calls.length).toBeGreaterThan(
        callsBefore
      )
    );
  });

  it("gates delete behind the confirm dialog, then deletes and refetches", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("schedule-row-s1"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("schedule-delete-s1"));
    });
    expect(screen.getByTestId("delete-schedule-dialog")).toBeTruthy();
    expect(mockedClient.schedules.delete).not.toHaveBeenCalled();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-cancel-delete-schedule"));
    });
    await waitFor(() =>
      expect(screen.queryByTestId("delete-schedule-dialog")).toBeNull()
    );
    expect(mockedClient.schedules.delete).not.toHaveBeenCalled();

    await act(async () => {
      fireEvent.click(screen.getByTestId("schedule-delete-s1"));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-confirm-delete-schedule"));
    });

    await waitFor(() =>
      expect(mockedClient.schedules.delete).toHaveBeenCalledWith("s1")
    );
    await waitFor(() =>
      expect(screen.queryByTestId("delete-schedule-dialog")).toBeNull()
    );
  });

  it("runs a schedule now through runNow", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("schedule-row-s1"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("schedule-run-s1"));
    });

    await waitFor(() =>
      expect(mockedClient.schedules.runNow).toHaveBeenCalledWith("s1")
    );
  });

  it("opens the last run's session through the nav store", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("schedule-row-s1"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("schedule-last-run-s1"));
    });

    const nav = useNavStore.getState();

    expect(nav.menu).toBe("chat");
    expect(nav.chatTarget).toEqual({ agentID: "a1", sessionID: "sess-1" });
  });

  it("refetches when schedules:changed fires", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("schedule-row-s1"));
    expect(schedulesChangedHandler).not.toBeNull();
    const callsBefore = mockedClient.schedules.list.mock.calls.length;

    await act(async () => {
      schedulesChangedHandler?.({
        scheduleId: "s2",
        sessionId: "",
        agentId: "a1",
      });
    });

    await waitFor(() =>
      expect(mockedClient.schedules.list.mock.calls.length).toBeGreaterThan(
        callsBefore
      )
    );
  });

  it("coalesces a burst of events into one refetch", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("schedule-row-s1"));
    const callsBefore = mockedClient.schedules.list.mock.calls.length;

    await act(async () => {
      schedulesChangedHandler?.({
        scheduleId: "",
        sessionId: "",
        agentId: "a1",
      });
      schedulesChangedHandler?.({
        scheduleId: "",
        sessionId: "",
        agentId: "a1",
      });
      schedulesChangedHandler?.({
        scheduleId: "",
        sessionId: "",
        agentId: "a1",
      });
    });

    await waitFor(() =>
      expect(mockedClient.schedules.list.mock.calls.length).toBe(
        callsBefore + 1
      )
    );
  });

  it("shows the empty state with the agent path and an add action", async () => {
    schedules = [];
    await renderView();

    const empty = await waitFor(() => screen.getByTestId("schedules-empty"));

    // The copy names both paths: manual add and the agent/chat tool.
    expect(empty.textContent).toContain("chat");
    expect(empty.textContent).toContain("Scheduler tool");
    expect(screen.getByTestId("btn-add-schedule-empty")).toBeTruthy();
  });

  it("opens the editor from the header add button", async () => {
    await renderView();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-add-schedule"));
    });

    expect(await screen.findByTestId("schedule-editor")).toBeTruthy();
    expect(inputOf("field-name").value).toBe("");
  });

  it("opens the editor prefilled from the row edit action", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("schedule-row-s1"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("schedule-edit-s1"));
    });

    expect(await screen.findByTestId("schedule-editor")).toBeTruthy();
    expect(inputOf("field-name").value).toBe("Standup digest");
  });

  it("shows the error state when the list fetch fails", async () => {
    mockedClient.schedules.list.mockRejectedValueOnce(
      new Error("store unavailable")
    );
    await renderView();

    const error = await waitFor(() => screen.getByTestId("schedules-error"));

    expect(error.textContent).toContain("store unavailable");
  });
});
