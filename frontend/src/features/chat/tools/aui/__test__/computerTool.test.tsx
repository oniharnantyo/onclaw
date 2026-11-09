/**
 * Computer-use trajectory card tests (add-computer-use task 6.2, spec
 * chat-tool-cards "Computer-use trajectory card"): a run of consecutive
 * computer-use calls renders as ONE trajectory card whose footer lists every
 * call in order (non-coordinate calls included, without cursor markers), the
 * running step highlights live and the finished trajectory persists, typed
 * text masks to a length-only placeholder, the backdrop loads through the
 * bound computerScreenshot read by the marker's file NAME only, and an
 * approval-gated call carries its approval state (chip on the step, the
 * actionable gate card beneath). Entries are built through the real reducer
 * and mounted through the runtime so the parts arrive exactly as the
 * transcript renders them.
 */
import { act } from "react";
import { screen, waitFor } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import {
  Kind,
  ToolStatus,
  type Event as SessionEvent,
} from "../../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { renderUI } from "../../../__test__/test-utils";
import { mockedSurface } from "../../../__test__/mockedSurface";
import { applyEvent, type Entry } from "../../../entries";
import { useChatStore } from "../../../store";
import type {
  BrowserScreenshotContent,
  Client,
} from "../../../../../lib/client";
import { partProps, renderWithEntries } from "./harness";
import { ComputerTool } from "../ComputerTool";
import { resolveToolRenderer } from "../renderers";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client surface this suite drives: the screenshot bytes read
 * and the catalog fetch some store paths trigger. */
type MockedClient = Client & {
  settings: { tools: MockFn };
  chat: { computerScreenshot: MockFn };
};

/** A tiny valid PNG (1×1 pixel, base64). */
const PNG_B64 =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

/** The client the hoisted mock factory installs. */
function makeClient(): MockedClient {
  // SAFETY: the literal covers the surface this suite drives; the Client
  // intersection defeats the structural check, so the conversion funnels
  // through the shared mockedSurface bridge (the audited conversion seam).
  return mockedSurface<MockedClient, object>({
    settings: { tools: vi.fn(async (): Promise<never[]> => []) },
    chat: {
      computerScreenshot: vi.fn(
        async (): Promise<BrowserScreenshotContent> => ({
          name: "1751895600000.png",
          width: 1440,
          height: 900,
          data: PNG_B64,
        })
      ),
    },
  });
}

vi.mock("../../../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../../../lib/client";

// SAFETY: the client module is mocked with makeClient() above, so its
// runtime shape is exactly the mock object this suite drives.
const mockScreenshot = (client as MockedClient).chat.computerScreenshot;

function ev(
  kind: SessionEvent["kind"],
  extra: Partial<SessionEvent> = {}
): SessionEvent {
  return { kind, occurred_at: "2026-01-01T00:00:00Z", ...extra };
}

const DONE = ToolStatus.ToolStatusCompleted;

const RUNNING = ToolStatus.ToolStatusRunning;

function toolEv(
  turn: string,
  call: {
    id: string;
    name: string;
    args?: string;
    status?: ToolStatus;
    result?: string;
    error?: boolean;
  }
): SessionEvent {
  return {
    kind: Kind.KindToolActivity,
    occurred_at: "2026-01-01T00:00:00Z",
    turn_id: turn,
    tool: {
      call_id: call.id,
      name: call.name,
      arguments: call.args ?? "{}",
      status: call.status,
      result: call.result,
      is_error: call.error,
    },
  };
}

function fold(...events: SessionEvent[]): Entry[] {
  return events.reduce<Entry[]>(
    (entries: Entry[], e: SessionEvent) => applyEvent(entries, e),
    []
  );
}

/** The observation result one observe call delivers (observe.go's
 * ObservationResult): the marker names the capture the bound read serves. */
function observeResult(
  name: string,
  width: number = 1440,
  height: number = 900
): string {
  return JSON.stringify({
    tree: "window " + name,
    capture: "1",
    screenshotPath: `/tmp/workspace/sessions/s1/computer/${name}`,
    width,
    height,
    marker: `[screenshot: sessions/s1/computer/${name}]`,
  });
}

/** The spec's run: observe, click, type, click again — one card, four
 * steps. */
function trajectoryRun(): Entry[] {
  return fold(
    ev(Kind.KindUserMessage, { text: "open the settings", turn_id: "t1" }),
    toolEv("t2", {
      id: "cu1",
      name: "mcp__computer-use__observe_window",
      args: '{"app":"Safari"}',
      status: DONE,
      result: observeResult("1751895600000.png"),
    }),
    toolEv("t2", {
      id: "cu2",
      name: "mcp__computer-use__click",
      args: '{"x":720,"y":450,"capture":"1"}',
      status: DONE,
      result: '{"ok":true}',
    }),
    toolEv("t2", {
      id: "cu3",
      name: "mcp__computer-use__type",
      args: '{"element":"e12","text":"hunter2"}',
      status: DONE,
      result: '{"ok":true}',
    }),
    toolEv("t2", {
      id: "cu4",
      name: "mcp__computer-use__click",
      args: '{"x":360,"y":225,"capture":"1"}',
      status: DONE,
      result: '{"ok":true}',
    })
  );
}

beforeEach(async () => {
  vi.clearAllMocks();

  Object.defineProperty(URL, "createObjectURL", {
    value: vi.fn((): string => "blob:mock-url"),
    configurable: true,
  });

  Object.defineProperty(URL, "revokeObjectURL", {
    value: vi.fn(),
    configurable: true,
  });

  await act(async () => {
    useChatStore.setState({
      activeAgentID: "a1",
      activeSessionID: "s1",
      entries: [],
      busy: false,
      panelTabs: [],
    });
  });
});

describe("computer-use trajectory card (add-computer-use task 6.2)", () => {
  it("consecutive calls group into one card with every step in the footer", async () => {
    await renderWithEntries(trajectoryRun());

    const cards = screen.getAllByTestId("computer-trajectory");

    expect(cards).toHaveLength(1);

    const steps = screen.getAllByTestId("computer-step");

    expect(steps).toHaveLength(4);
    expect(
      steps.map((step: HTMLElement): string => step.dataset.step ?? "")
    ).toEqual(["observe_window", "click", "type", "click"]);

    // The finished trajectory persists: nothing reads live.
    expect(cards[0].dataset.live).toBe("false");
    expect(
      steps.every(
        (step: HTMLElement): boolean => step.dataset.running === "false"
      )
    ).toBe(true);
  });

  it("the backdrop loads through the bound read, addressed by the marker's file name only", async () => {
    await renderWithEntries(trajectoryRun());

    await waitFor((): void => {
      expect(screen.getByTestId("computer-backdrop-image")).toBeTruthy();
    });

    // The read received the viewing session plus the basename — never a path.
    expect(mockScreenshot).toHaveBeenCalledTimes(1);
    expect(mockScreenshot).toHaveBeenCalledWith("s1", "1751895600000.png");

    // The title names the observed app.
    expect(screen.getByTestId("computer-trajectory").textContent).toContain(
      "Safari"
    );
  });

  it("typed text masks to a length-only placeholder, never the verbatim text", async () => {
    await renderWithEntries(trajectoryRun());

    const typeStep = screen
      .getAllByTestId("computer-step")
      .find((step: HTMLElement): boolean => step.dataset.step === "type");

    expect(typeStep).toBeTruthy();

    const chip = typeStep?.querySelector('[data-testid="computer-step-chip"]');

    expect(chip?.textContent).toContain("••••");
    expect(chip?.textContent).toContain("(7 chars)");
    expect(chip?.textContent).toContain("e12");

    // The verbatim text never reaches the DOM.
    expect(screen.getByTestId("computer-trajectory").textContent).not.toContain(
      "hunter2"
    );
  });

  it("the running step highlights live and the completed ones do not", async () => {
    await renderWithEntries(
      fold(
        ev(Kind.KindUserMessage, { text: "go", turn_id: "t1" }),
        toolEv("t2", {
          id: "cu1",
          name: "mcp__computer-use__observe_window",
          args: '{"app":"Safari"}',
          status: DONE,
          result: observeResult("1751895600000.png"),
        }),
        toolEv("t2", {
          id: "cu2",
          name: "mcp__computer-use__click",
          args: '{"x":720,"y":450,"capture":"1"}',
          status: RUNNING,
        })
      ),
      true
    );

    const steps = screen.getAllByTestId("computer-step");

    expect(steps).toHaveLength(2);
    expect(steps[0].dataset.running).toBe("false");
    expect(steps[1].dataset.running).toBe("true");
    expect(screen.getByTestId("computer-trajectory").dataset.live).toBe("true");
  });

  it("non-coordinate calls render as footer steps without cursor markers", async () => {
    await renderWithEntries(
      fold(
        ev(Kind.KindUserMessage, { text: "go", turn_id: "t1" }),
        toolEv("t2", {
          id: "cu1",
          name: "mcp__computer-use__observe_window",
          args: '{"app":"Safari"}',
          status: DONE,
          result: observeResult("1751895600000.png"),
        }),
        toolEv("t2", {
          id: "cu2",
          name: "mcp__computer-use__key",
          args: '{"key":"Enter"}',
          status: DONE,
          result: '{"ok":true}',
        }),
        toolEv("t2", {
          id: "cu3",
          name: "mcp__computer-use__read_clipboard",
          status: DONE,
          result: '{"text":"clipboard body"}',
        })
      )
    );

    const steps = screen.getAllByTestId("computer-step");

    expect(
      steps.map((step: HTMLElement): string => step.dataset.step ?? "")
    ).toEqual(["observe_window", "key", "read_clipboard"]);

    // The keyboard and clipboard steps carry no cursor markers: the element's
    // backdrop renders no trail dots (its only styled spans) and no cursor.
    const frame = document.querySelector('[data-slot="computer-use"]');

    expect(frame?.querySelectorAll("span[style]")).toHaveLength(0);
    expect(frame?.querySelectorAll("svg")).toHaveLength(0);

    // The clipboard read's content rides the result payload only — the chip
    // names the clipboard, never its text.
    expect(screen.getByTestId("computer-trajectory").textContent).not.toContain(
      "clipboard body"
    );
  });

  it("coordinate actions mark the backdrop with cursor markers at converted percentages", async () => {
    await renderWithEntries(trajectoryRun());

    await waitFor((): void => {
      expect(screen.getByTestId("computer-backdrop-image")).toBeTruthy();
    });

    // One trail dot per coordinate action (720,450 → 50%/50%; 360,225 →
    // 25%/25% of the 1440×900 backdrop), plus the cursor.
    const frame = document.querySelector('[data-slot="computer-use"]');

    const dots = frame?.querySelectorAll("span[style]") ?? [];

    expect(dots).toHaveLength(2);
    expect(dots[0].getAttribute("style")).toContain("left: 50%");
    expect(dots[0].getAttribute("style")).toContain("top: 50%");
    expect(dots[1].getAttribute("style")).toContain("left: 25%");
    expect(dots[1].getAttribute("style")).toContain("top: 25%");
    expect(frame?.querySelectorAll("svg")).toHaveLength(1);
  });

  it("an approval-gated call carries its approval state chip and the actionable gate card", async () => {
    await renderWithEntries(
      fold(
        ev(Kind.KindUserMessage, { text: "go", turn_id: "t1" }),
        toolEv("t2", {
          id: "cu1",
          name: "mcp__computer-use__observe_window",
          args: '{"app":"Safari"}',
          status: DONE,
          result: observeResult("1751895600000.png"),
        }),
        // The gate parks the write: the approval part (the "execute"-named
        // tool-call the mapping mints, payload naming tool + target) joins
        // the run, and the resolved decision rides it (replay order).
        ev(Kind.KindApprovalRequired, {
          turn_id: "t2",
          approval: {
            interrupt_id: "int-cu",
            command: "write_clipboard clipboard",
          },
        }),
        ev(Kind.KindApprovalResolved, {
          turn_id: "t2",
          approval: { interrupt_id: "int-cu", outcome: "approved" },
        }),
        toolEv("t2", {
          id: "cu2",
          name: "mcp__computer-use__write_clipboard",
          args: '{"text":"sensitive body"}',
          status: DONE,
          result: '{"ok":true}',
        })
      )
    );

    const steps = screen.getAllByTestId("computer-step");

    expect(steps).toHaveLength(2);

    const gated = steps.find(
      (step: HTMLElement): boolean => step.dataset.step === "write_clipboard"
    );

    expect(
      gated
        ?.querySelector('[data-testid="computer-step-approval"]')
        ?.getAttribute("data-approval-state")
    ).toBe("approved");

    // The gate card renders beneath the card with the recorded receipt.
    expect(screen.getByTestId("entry-approval").textContent).toContain(
      "Approved"
    );
    expect(screen.getByTestId("entry-approval").textContent).toContain(
      "write_clipboard"
    );

    // The clipboard text masks like typed text.
    expect(screen.getByTestId("computer-trajectory").textContent).not.toContain(
      "sensitive body"
    );
  });

  it("a pending approval renders the actionable controls on the gate card", async () => {
    await renderWithEntries(
      fold(
        ev(Kind.KindUserMessage, { text: "go", turn_id: "t1" }),
        toolEv("t2", {
          id: "cu1",
          name: "mcp__computer-use__observe_window",
          args: '{"app":"Safari"}',
          status: DONE,
          result: observeResult("1751895600000.png"),
        }),
        ev(Kind.KindApprovalRequired, {
          turn_id: "t2",
          approval: { interrupt_id: "int-cu", command: "kill_app Safari" },
        })
      ),
      true
    );

    const steps = screen.getAllByTestId("computer-step");

    expect(steps).toHaveLength(1);

    const gated = steps.find(
      (step: HTMLElement): boolean => step.dataset.step === "observe_window"
    );

    // The kill_app approval joins the run but names no transcript step (the
    // gated call itself has no part yet) — the chip rides only a step.
    expect(
      gated?.querySelector('[data-testid="computer-step-approval"]')
    ).toBeNull();

    // The pending gate card stays actionable beneath the trajectory.
    expect(screen.getByTestId("entry-approval").textContent).toContain(
      "kill_app"
    );
    expect(
      document.querySelector('[data-slot="approval-card-allow-once"]')
    ).toBeTruthy();
    expect(
      document.querySelector('[data-slot="approval-card-deny"]')
    ).toBeTruthy();
  });

  it("a refused screenshot read degrades to the unavailable state", async () => {
    mockScreenshot.mockRejectedValueOnce(
      new Error("computer screenshot: refused name")
    );

    await renderWithEntries(
      fold(
        ev(Kind.KindUserMessage, { text: "go", turn_id: "t1" }),
        toolEv("t2", {
          id: "cu1",
          name: "mcp__computer-use__observe_window",
          args: '{"app":"Safari"}',
          status: DONE,
          result: observeResult("1751895600000.png"),
        })
      )
    );

    await waitFor((): void => {
      expect(screen.getByTestId("computer-unavailable")).toBeTruthy();
    });

    expect(screen.queryByTestId("computer-backdrop-image")).toBeNull();
  });

  it("a single isolated call renders the same card with its one step", async () => {
    const Computer = resolveToolRenderer("mcp__computer-use__click");

    expect(Computer).toBe(ComputerTool);

    await renderWithEntries(
      fold(
        ev(Kind.KindUserMessage, { text: "go", turn_id: "t1" }),
        toolEv("t2", {
          id: "cu1",
          name: "mcp__computer-use__click",
          args: '{"x":720,"y":450,"capture":"1"}',
          status: DONE,
          result: '{"ok":true}',
        })
      )
    );

    // The isolated call still renders the trajectory card — one step, and
    // the cursor markers stand down with no observation to convert against.
    // The desktop frame renders only when an observation image exists
    // (user feedback): no capture in the run, no frame at all — the step
    // rows stand alone.
    expect(screen.getAllByTestId("computer-trajectory")).toHaveLength(1);
    expect(screen.getAllByTestId("computer-step")).toHaveLength(1);
    expect(screen.queryByTestId("computer-backdrop-empty")).toBeNull();
    expect(
      document.querySelector('[data-slot="computer-use"]')
    ).toBeNull();
  });

  it("mounts standalone for an isolated call outside any run", async () => {
    const { unmount } = await renderUI(
      <ComputerTool
        {...partProps({
          toolCallId: "cu-standalone",
          toolName: "mcp__computer-use__type",
          argsText: '{"element":"e9","text":"hello world"}',
          result: '{"ok":true}',
        })}
      />
    );

    expect(screen.getAllByTestId("computer-trajectory")).toHaveLength(1);
    expect(screen.getAllByTestId("computer-step")).toHaveLength(1);
    expect(screen.getByTestId("computer-trajectory").textContent).not.toContain(
      "hello world"
    );
    // No observation in this run either — the desktop frame stays off.
    expect(
      document.querySelector('[data-slot="computer-use"]')
    ).toBeNull();

    await unmount();
  });
});
