/**
 * Heavy-turn folding tests (task 4.5, spec chat-tool-cards "Heavy-turn
 * folding"): a run of four or more consecutive tool/task parts folds behind
 * the "N steps · M files changed" summary with trace rows (icon, verb,
 * target chip) and the trailing per-file stats row revealed on activation; a
 * trace row opens its call's full card beneath it; the running turn names
 * its current tool on a shimmer that reduced motion turns static; a pending
 * approval splits the run and stays actionable; a failed call tints the
 * summary, reveals the rows, and tints the failed row; and repeated
 * todo_write calls collapse to "plan updated" lines except the last. A run
 * below the fold threshold renders inline in the transcript flow with no
 * group disclosure standing in for it. Entries
 * are built through the real reducer and mounted through the runtime so the
 * parts arrive exactly as the transcript renders them.
 */
import { act } from "react";
import { fireEvent, screen } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import {
  Kind,
  ToolStatus,
  type Event as SessionEvent,
} from "../../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { applyEvent, type Entry } from "../../../entries";
import { useChatStore } from "../../../store";
import { renderWithEntries } from "./harness";

vi.mock("../../../../../lib/client", () => ({
  client: {
    settings: {
      tools: vi.fn(async (): Promise<never[]> => []),
    },
    chat: {
      browserScreenshot: vi.fn(async (): Promise<never> => {
        throw new Error("no screenshots in tests");
      }),
    },
  },
}));

// The cards' lazy Shiki loader stays out of these fold assertions.
vi.mock("../../primitives/shikiHighlight", () => ({
  highlightTokens: async (): Promise<null> => null,
  isKnownLanguage: (): boolean => false,
  tokenStyle: (): Record<string, string> => ({}),
}));

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

/** The spec's five-call run: two edits to distinct files. */
function fiveCallRun(): Entry[] {
  return fold(
    ev(Kind.KindUserMessage, { text: "do the thing", turn_id: "t1" }),
    toolEv("t2", {
      id: "c1",
      name: "read_file",
      args: '{"file_path":"src/a.ts"}',
      status: DONE,
      result: "contents",
    }),
    toolEv("t2", {
      id: "c2",
      name: "edit_file",
      args: '{"file_path":"src/a.ts","old_string":"x","new_string":"y"}',
      status: DONE,
      result: "ok",
    }),
    toolEv("t2", {
      id: "c3",
      name: "read_file",
      args: '{"file_path":"src/b.ts"}',
      status: DONE,
      result: "contents",
    }),
    toolEv("t2", {
      id: "c4",
      name: "write_file",
      args: '{"file_path":"src/gen/out.ts","content":"line"}',
      status: DONE,
      result: "ok",
    }),
    toolEv("t2", {
      id: "c5",
      name: "web_search",
      args: '{"query":"weather"}',
      status: DONE,
      result: "sunny",
    })
  );
}

/** The card id one call renders under: the mapping's part ids are entry ids
 * (design D2), not the session's call ids. */
function cardID(entries: Entry[], callID: string): string {
  const entry = entries.find(
    (candidate: Entry): boolean =>
      candidate.kind === "tool" && candidate.call?.call_id === callID
  );

  if (entry === undefined) throw new Error(`no tool entry for ${callID}`);

  return entry.id;
}

/** The timeline's summary trigger (the ToolTimeline disclosure control). */
function summaryTrigger(): HTMLElement {
  const trigger = document.querySelector(
    '[data-testid="run-summary"] [data-slot="collapsible-trigger"]'
  );

  if (trigger === null) throw new Error("run summary trigger missing");

  // SAFETY: the null check above proves the query matched an element, which
  // is the collapsible trigger's HTMLElement.
  return trigger as HTMLElement;
}

beforeEach(async () => {
  vi.clearAllMocks();

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

describe("heavy-turn folding (task 4.5)", () => {
  it("five calls with two edited files fold; activating reveals the trace rows and the stats row", async () => {
    const entries = fiveCallRun();

    await renderWithEntries(entries);

    const folded = cardID(entries, "c1");

    const summary = screen.getByTestId("run-summary");

    expect(summary.textContent).toContain("5 steps");
    expect(summary.textContent).toContain("2 files changed");
    // The five cards are folded away.
    expect(screen.queryByTestId(`entry-tool-${folded}`)).toBeNull();

    await act(async () => {
      fireEvent.click(summaryTrigger());
    });

    const rows = screen.getAllByTestId("run-row");

    expect(rows).toHaveLength(5);
    // The trace rows carry the verb and target-chip grammar.
    expect(rows[0].textContent).toContain("Read");
    expect(rows[0].textContent).toContain("a.ts");
    expect(rows[1].textContent).toContain("Edit");

    // The trailing stats row names the two changed files.
    const stats = screen.getByTestId("run-stats");

    expect(stats.textContent).toContain("src/a.ts");
    expect(stats.textContent).toContain("+1");
    expect(stats.textContent).toContain("src/gen/out.ts");
    expect(stats.textContent).toContain("+1");

    // No cards yet — rows open cards on demand, not all at once.
    expect(screen.queryByTestId(`entry-tool-${folded}`)).toBeNull();
  });

  it("activating a trace row opens that call's full card beneath the row", async () => {
    await renderWithEntries(fiveCallRun());

    await act(async () => {
      fireEvent.click(summaryTrigger());
    });

    const rows = screen.getAllByTestId("run-row");

    await act(async () => {
      fireEvent.click(rows[1]);
    });

    // The second call's card opens beneath its row, with its arguments.
    const card = screen.getAllByTestId("run-row-card")[0];

    expect(card.textContent).toContain("edit_file");
    expect(card.textContent).toContain("a.ts");

    await act(async () => {
      fireEvent.click(rows[1]);
    });

    // A second activation closes it again.
    expect(screen.queryByTestId("run-row-card")).toBeNull();
  });

  it("a three-call run renders inline, in the flow, with no group disclosure", async () => {
    const entries = fold(
      ev(Kind.KindUserMessage, { text: "go", turn_id: "t1" }),
      toolEv("t2", {
        id: "c1",
        name: "read_file",
        status: DONE,
        result: "x",
      }),
      toolEv("t2", {
        id: "c2",
        name: "edit_file",
        args: '{"file_path":"src/a.ts","old_string":"x","new_string":"y"}',
        status: DONE,
        result: "ok",
      }),
      toolEv("t2", {
        id: "c3",
        name: "web_search",
        status: DONE,
        result: "s",
      })
    );

    await renderWithEntries(entries);

    expect(screen.queryByTestId("run-summary")).toBeNull();
    // The short run sits in the transcript flow…
    expect(screen.getByTestId("run-inline")).toBeTruthy();
    // …and each call is on the page, in message order.

    for (const callID of ["c1", "c2", "c3"]) {
      expect(
        screen.getByTestId(`entry-tool-${cardID(entries, callID)}`)
      ).toBeTruthy();
    }

    // No group disclosure stands in for them.
    expect(
      document.querySelector('[data-slot="tool-group-trigger"]')
    ).toBeNull();
  });

  it("the running turn names its current tool on the summary", async () => {
    await renderWithEntries(
      fold(
        ev(Kind.KindUserMessage, { text: "go", turn_id: "t1" }),
        toolEv("t2", {
          id: "c1",
          name: "read_file",
          status: DONE,
          result: "x",
        }),
        toolEv("t2", {
          id: "c2",
          name: "edit_file",
          status: DONE,
          result: "ok",
        }),
        toolEv("t2", {
          id: "c3",
          name: "web_search",
          status: DONE,
          result: "s",
        }),
        toolEv("t2", { id: "c4", name: "grep_file", status: RUNNING })
      ),
      true
    );

    const summary = screen.getByTestId("run-summary");

    expect(summary.textContent).toContain("running grep_file");
    // The active label shimmers while the run streams.
    expect(summary.querySelector(".shimmer")).toBeTruthy();
  });

  it("a pending approval splits the run and stays actionable", async () => {
    const entries = fold(
      ev(Kind.KindUserMessage, { text: "go", turn_id: "t1" }),
      toolEv("t2", { id: "c1", name: "read_file", status: DONE, result: "x" }),
      toolEv("t2", { id: "c2", name: "read_file", status: DONE, result: "x" }),
      toolEv("t2", { id: "c3", name: "read_file", status: DONE, result: "x" }),
      toolEv("t2", { id: "c4", name: "edit_file", status: DONE, result: "ok" }),
      ev(Kind.KindApprovalRequired, {
        turn_id: "t2",
        approval: { interrupt_id: "int1", command: "rm -rf build/" },
      }),
      toolEv("t2", { id: "c5", name: "read_file", status: DONE, result: "x" })
    );

    await renderWithEntries(entries, true);

    // The approval card renders unfolded with its decision actions.
    expect(screen.getByTestId("entry-approval").textContent).toContain(
      "rm -rf build/"
    );
    expect(
      document.querySelector('[data-slot="approval-card-allow-once"]')
    ).toBeTruthy();
    expect(
      document.querySelector('[data-slot="approval-card-deny"]')
    ).toBeTruthy();

    // The four calls before it fold; the one after renders plainly.
    expect(screen.getByTestId("run-summary").textContent).toContain("4 steps");
    expect(
      screen.queryByTestId(`entry-tool-${cardID(entries, "c1")}`)
    ).toBeNull();
    expect(
      screen.getByTestId(`entry-tool-${cardID(entries, "c5")}`)
    ).toBeTruthy();
  });

  it("a failed call tints the summary, reveals the rows, and tints the failed row", async () => {
    await renderWithEntries(
      fold(
        ev(Kind.KindUserMessage, { text: "go", turn_id: "t1" }),
        toolEv("t2", {
          id: "c1",
          name: "read_file",
          status: DONE,
          result: "x",
        }),
        toolEv("t2", {
          id: "c2",
          name: "read_file",
          status: DONE,
          result: "x",
        }),
        toolEv("t2", {
          id: "c3",
          name: "edit_file",
          status: ToolStatus.ToolStatusFailed,
          error: true,
          result: "permission denied",
        }),
        toolEv("t2", { id: "c4", name: "read_file", status: DONE, result: "x" })
      )
    );

    // Auto-revealed without a click, tinted with the failure grammar: the
    // summary line carries the danger treatment, not a bordered panel.
    expect(screen.getByTestId("run-summary").textContent).toContain("4 steps");
    expect(screen.getByTestId("run-failed-summary").className).toContain(
      "text-dangertext"
    );
    expect(screen.getByTestId("run-failed-summary").className).not.toContain(
      "border-danger"
    );
    expect(screen.getAllByTestId("run-row")).toHaveLength(4);

    // The failed row names the failure in its chip.
    const failedRow = screen
      .getAllByTestId("run-row")
      .find((row: HTMLElement): boolean => row.textContent.includes("failed"));

    expect(failedRow).toBeTruthy();

    // The failed call's error stays readable through its card.
    await act(async () => {
      fireEvent.click(failedRow!);
    });

    expect(screen.getByTestId("run-row-card").textContent).toContain(
      "permission denied"
    );
  });

  it("repeated todo_write calls collapse to plan updated except the last", async () => {
    await renderWithEntries(
      fold(
        ev(Kind.KindUserMessage, { text: "plan as you go", turn_id: "t1" }),
        toolEv("t2", {
          id: "c1",
          name: "todo_write",
          args: '{"items":[{"key":"k1","text":"first","status":"pending"}]}',
          status: DONE,
          result: '{"items":[{"key":"k1","text":"first","status":"pending"}]}',
        }),
        toolEv("t2", {
          id: "c2",
          name: "read_file",
          status: DONE,
          result: "x",
        }),
        toolEv("t2", {
          id: "c3",
          name: "todo_write",
          args: '{"items":[{"key":"k1","text":"first","status":"done"}]}',
          status: DONE,
          result: '{"items":[{"key":"k1","text":"first","status":"done"}]}',
        }),
        toolEv("t2", {
          id: "c4",
          name: "edit_file",
          status: DONE,
          result: "ok",
        })
      )
    );

    await act(async () => {
      fireEvent.click(summaryTrigger());
    });

    // All but the last todo_write became plan-updated lines.
    expect(screen.getAllByTestId("run-plan-updated")).toHaveLength(1);

    const rows = screen.getAllByTestId("run-row");

    // The last todo_write keeps its row (opening its plan snapshot), and the
    // read/edit calls keep theirs.
    expect(rows).toHaveLength(3);

    await act(async () => {
      fireEvent.click(rows[1]);
    });

    expect(screen.getByTestId("run-row-card").textContent).toContain(
      "todo_write"
    );
  });
});
