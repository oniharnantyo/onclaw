/**
 * Component tests for the Hooks pane (tasks 4.5, 4.7, 11.4): the form offers
 * only the five lifecycle events and the two handler types, surfaces the
 * service's validation error, renders the new dry-run result contract
 * (decision/reason/console/error), and lists the execution audit trail.
 * The lib client and the generated TestHook binding are mocked wholesale.
 */
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import userEvent from "@testing-library/user-event";
import type {
  Hook,
  HookExecution,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../../../lib/client";

import HooksPane from "../components/HooksPane";
import type { SettingsPaneConfig } from "../types";
import { renderUI, setInput } from "./test-utils";

/** Header config passed to the pane; mirrors the static nav entry. */
const pane: SettingsPaneConfig = {
  key: "hooks",
  label: "Hooks",
  icon: "activity",
  description: "Lifecycle hooks that run commands on gateway events.",
};

const hooks: Hook[] = [];

let executions: HookExecution[] = [];

vi.mock("../../../lib/client", () => ({
  client: {
    settings: {
      hooks: vi.fn(async () => [...hooks]),
      hookExecutions: vi.fn(async () => [...executions]),
      createHook: vi.fn(async (hook: Hook) => {
        hooks.push({ ...hook, ID: `h${hooks.length + 1}` });

        return hook;
      }),
      updateHook: vi.fn(async (hook: Hook) => {
        const i = hooks.findIndex((h: Hook) => h.ID === hook.ID);

        if (i >= 0) hooks[i] = hook;

        return hook;
      }),
      deleteHook: vi.fn(async (id: string) => {
        const i = hooks.findIndex((h: Hook) => h.ID === id);

        if (i >= 0) hooks.splice(i, 1);
      }),
    },
  },
}));

vi.mock(
  "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/settingsservice",
  () => ({
    TestHook: vi.fn(),
  })
);

import { client } from "../../../lib/client";
import { TestHook } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/settingsservice";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  settings: {
    hooks: MockFn;
    hookExecutions: MockFn;
    createHook: MockFn;
    updateHook: MockFn;
    deleteHook: MockFn;
  };
};

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

const mockTestHook = vi.mocked(TestHook);

function makeHook(patch: Partial<Hook> = {}): Hook {
  return {
    ID: "h1",
    Name: "notify",
    Event: "run_finished",
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

function makeExecution(patch: Partial<HookExecution> = {}): HookExecution {
  return {
    ID: "e1",
    HookID: "h1",
    AgentID: "a1",
    SessionID: "s1",
    TurnID: "t1",
    Event: "pre_tool_use",
    Outcome: "allowed",
    Error: "",
    DurationMS: 12,
    OccurredAt: new Date().toISOString(),
    ...patch,
  };
}

/** Fills the add form: name, event (a Select now), and a valid config. */
async function fillAddForm(
  user: ReturnType<typeof userEvent.setup>,
  queries: {
    getByTestId: (id: string) => HTMLElement;
  },
  patch: {
    name?: string;
    event?: string;
    handlerType?: string;
    config?: string;
  } = {}
): Promise<void> {
  await user.type(queries.getByTestId("hook-name"), patch.name ?? "notify");
  await user.selectOptions(
    queries.getByTestId("hook-event"),
    patch.event ?? "run_finished"
  );

  if (patch.handlerType) {
    await user.selectOptions(
      queries.getByTestId("hook-handlertype"),
      patch.handlerType
    );
  }

  await setInput(
    queries.getByTestId("hook-config"),
    patch.config ?? '{"command": "echo done"}'
  );
}

describe("HooksPane", () => {
  beforeEach(() => {
    hooks.length = 0;
    executions = [];
    vi.clearAllMocks();
  });

  it("round-trips add: dialog fill, createHook called, list refreshes", async () => {
    const user = userEvent.setup();

    const { getByTestId, queryByTestId, findByTestId } = await renderUI(
      <HooksPane pane={pane} />
    );

    expect(await findByTestId("hooks-empty")).toBeTruthy();

    await user.click(getByTestId("hook-add"));
    await fillAddForm(user, { getByTestId }, { name: "notify" });
    await user.click(getByTestId("hook-save"));

    expect(mockedClient.settings.createHook).toHaveBeenCalledWith(
      expect.objectContaining({
        Name: "notify",
        Event: "run_finished",
        HandlerType: "command",
      })
    );
    expect(await findByTestId("hook-list")).toBeTruthy();
    expect(queryByTestId("hook-dialog")).toBeNull();
  });

  it("offers only the five lifecycle events and both handler types", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <HooksPane pane={pane} />
    );

    await user.click(await findByTestId("hook-add"));

    const events = Array.from(
      // SAFETY: this test id renders a select element.
      (getByTestId("hook-event") as HTMLSelectElement).options
    ).map((o: HTMLOptionElement): string => o.value);

    const handlers = Array.from(
      // SAFETY: this test id renders a select element.
      (getByTestId("hook-handlertype") as HTMLSelectElement).options
    ).map((o: HTMLOptionElement): string => o.value);

    expect(events).toEqual([
      "run_started",
      "user_prompt_submit",
      "pre_tool_use",
      "post_tool_use",
      "run_finished",
    ]);
    expect(handlers).toEqual(["command", "script"]);
  });

  it("saves a script handler with the script config placeholder shape", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <HooksPane pane={pane} />
    );

    await user.click(await findByTestId("hook-add"));
    await fillAddForm(
      user,
      { getByTestId },
      {
        name: "guard",
        event: "pre_tool_use",
        handlerType: "script",
        config:
          '{"script": "(function(input){ return {decision: \\"block\\"} })"}',
      }
    );
    await user.click(getByTestId("hook-save"));

    expect(mockedClient.settings.createHook).toHaveBeenCalledWith(
      expect.objectContaining({
        Event: "pre_tool_use",
        HandlerType: "script",
      })
    );
  });

  it("round-trips the dialog Enabled switch from the empty state's action", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <HooksPane pane={pane} />
    );

    // Empty state carries a next-step action (task 4.8).
    expect(await findByTestId("hooks-empty")).toBeTruthy();

    await user.click(getByTestId("hook-add-empty"));
    await fillAddForm(user, { getByTestId }, { name: "off-hook" });
    // The kit Toggle renders as a role=switch button.
    const enabled = getByTestId("hook-enabled");
    expect(enabled.getAttribute("role")).toBe("switch");
    expect(enabled.getAttribute("aria-checked")).toBe("true");
    await user.click(enabled);
    await user.click(getByTestId("hook-save"));

    expect(mockedClient.settings.createHook).toHaveBeenCalledWith(
      expect.objectContaining({ Name: "off-hook", Enabled: false })
    );
    expect(queryByTestId("hook-dialog")).toBeNull();
  });

  it("blocks save on invalid config JSON", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <HooksPane pane={pane} />
    );

    await user.click(await findByTestId("hook-add"));
    await user.type(getByTestId("hook-name"), "bad");
    await user.selectOptions(getByTestId("hook-event"), "run_finished");
    await setInput(getByTestId("hook-config"), "{{not json");

    // SAFETY: this test id renders the field message element.
    expect((getByTestId("hook-config-error") as HTMLElement).textContent).toBe(
      "Invalid JSON"
    );
    // SAFETY: this test id renders a button element.
    expect((getByTestId("hook-save") as HTMLButtonElement).disabled).toBe(true);
    expect(mockedClient.settings.createHook).not.toHaveBeenCalled();
  });

  it("shows the service validation error from a rejected save", async () => {
    mockedClient.settings.createHook.mockRejectedValueOnce(
      new Error(
        "hook: event must be one of run_started, user_prompt_submit, pre_tool_use, post_tool_use, run_finished"
      )
    );
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <HooksPane pane={pane} />
    );

    await user.click(await findByTestId("hook-add"));
    await fillAddForm(user, { getByTestId });
    await user.click(getByTestId("hook-save"));

    // The dialog stays open with the backend's explanation.
    const alert = await findByTestId("hook-save-error");
    expect(alert.textContent).toContain("event must be one of");
    expect(getByTestId("hook-dialog")).toBeTruthy();
  });

  it("edits and removes hooks", async () => {
    hooks.push(makeHook());
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <HooksPane pane={pane} />
    );

    await user.click(await findByTestId("hook-edit-h1"));
    await user.type(getByTestId("hook-name"), "-v2");
    await user.click(getByTestId("hook-save"));
    expect(mockedClient.settings.updateHook).toHaveBeenCalledWith(
      expect.objectContaining({ ID: "h1", Name: "notify-v2" })
    );

    await user.click(await findByTestId("hook-remove-h1"));
    await user.click(getByTestId("hook-remove-confirm"));
    expect(mockedClient.settings.deleteHook).toHaveBeenCalledWith("h1");
  });

  it("renders an allowing dry run with its reason and output", async () => {
    hooks.push(makeHook());
    mockTestHook.mockResolvedValue({
      Success: true,
      Decision: "allow",
      Reason: "",
      Output: "done\n",
      Err: "",
      ConsoleLines: null,
      DurationMS: 8,
    });
    const user = userEvent.setup();
    const { findByTestId } = await renderUI(<HooksPane pane={pane} />);

    await user.click(await findByTestId("hook-test-btn-h1"));
    const result = await findByTestId("hook-test-result-h1");
    expect(result.textContent).toContain("Decision: allow");
    expect((await findByTestId("hook-test-output-h1")).textContent).toContain(
      "done"
    );
  });

  it("renders a blocking dry run with its reason", async () => {
    hooks.push(makeHook());
    mockTestHook.mockResolvedValue({
      Success: true,
      Decision: "block",
      Reason: "shell commands are disabled for this agent",
      Output: "",
      Err: "",
      ConsoleLines: null,
      DurationMS: 4,
    });
    const user = userEvent.setup();
    const { findByTestId } = await renderUI(<HooksPane pane={pane} />);

    await user.click(await findByTestId("hook-test-btn-h1"));
    expect((await findByTestId("hook-test-result-h1")).textContent).toContain(
      "Decision: block"
    );
    expect((await findByTestId("hook-test-reason-h1")).textContent).toContain(
      "shell commands are disabled"
    );
  });

  it("renders a script dry run with its console lines", async () => {
    hooks.push(makeHook({ HandlerType: "script" }));
    mockTestHook.mockResolvedValue({
      Success: false,
      Decision: "failure",
      Reason: "",
      Output: "",
      Err: "script failed: timeout after 5000ms",
      ConsoleLines: ["sample pre_tool_use", "no decision"],
      DurationMS: 5000,
    });
    const user = userEvent.setup();
    const { findByTestId } = await renderUI(<HooksPane pane={pane} />);

    await user.click(await findByTestId("hook-test-btn-h1"));
    expect((await findByTestId("hook-test-result-h1")).textContent).toContain(
      "Decision: failure"
    );
    expect((await findByTestId("hook-test-console-h1")).textContent).toContain(
      "sample pre_tool_use"
    );
    expect((await findByTestId("hook-test-error-h1")).textContent).toContain(
      "timeout"
    );
  });

  describe("execution audit", () => {
    it("lists recent executions with hook name, event, and outcome", async () => {
      hooks.push(makeHook());
      executions.push(
        makeExecution({ ID: "e1", Outcome: "allowed", DurationMS: 12 }),
        makeExecution({
          ID: "e2",
          Outcome: "blocked",
          Error: "shell commands are disabled",
          DurationMS: 4,
          OccurredAt: new Date().toISOString(),
        }),
        makeExecution({
          ID: "e3",
          Outcome: "error",
          Error: "exit status 3",
          DurationMS: 31,
          OccurredAt: new Date().toISOString(),
        })
      );
      const { findByTestId } = await renderUI(<HooksPane pane={pane} />);

      const list = await findByTestId("hooks-audit-list");
      expect(list.textContent).toContain("notify");
      expect(list.textContent).toContain("pre_tool_use");
      expect(list.textContent).toContain("allowed");
      expect(list.textContent).toContain("blocked");
      expect(list.textContent).toContain("error");
      // Durations ride the row detail line.
      expect(list.textContent).toContain("12 ms");
      // The failure reason renders on its own row.
      expect(
        (await findByTestId("hook-execution-error-e3")).textContent
      ).toContain("exit status 3");
    });

    it("falls back to the hook id for a deleted hook and shows an empty state", async () => {
      executions.push(
        makeExecution({ ID: "e1", HookID: "gone", OccurredAt: "" })
      );

      const { findByTestId, unmount } = await renderUI(
        <HooksPane pane={pane} />
      );

      expect((await findByTestId("hooks-audit-list")).textContent).toContain(
        "gone"
      );
      await unmount();

      executions = [];
      const empty = await renderUI(<HooksPane pane={pane} />);

      expect(await empty.findByTestId("hooks-audit-empty")).toBeTruthy();
      expect(empty.queryByTestId("hooks-audit-list")).toBeNull();
    });
  });
});
