/**
 * Run surfaces tests (tasks 3.5, 9.1–9.2, design D8; spec chat-panels run
 * card): the island hides with no content, starts as the minimized pill whose
 * summary covers both sections ("1/4 · 1 running"), the Progress section
 * groups the plan snapshot (collapsed completed/waiting groups, the
 * in-progress row's active_form, free pending rows), the Agents section
 * draws each delegated task's state (spinner + progress line, failed chip +
 * error detail, completed check), both sections are pure derivations of the
 * store's entries — a second snapshot or terminal record replaces in place —
 * and the island anchors to the thread viewport's relative wrapper, never
 * the chat column, so it cannot reach the composer. Activating an Agents
 * row opens its trajectory as a tab in the right panel (design D3,
 * spec merge-right-bar-previews); rows without a transcript path are inert.
 */
import { act } from "react";
import { fireEvent, screen } from "@testing-library/dom";
import { beforeEach, describe, expect, it } from "vite-plus/test";

import {
  Kind,
  ToolStatus,
  type Event as SessionEvent,
  type PlanTask,
  type TaskActivity,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { applyEvent, type Entry } from "../../entries";
import { useChatStore } from "../../store";
import { renderThread } from "../../thread/__test__/harness";
import { RunSurfaces } from "../RunSurfaces";
import { renderUI } from "../../__test__/test-utils";

function taskEvent(task: TaskActivity): SessionEvent {
  return {
    kind: Kind.KindTaskActivity,
    occurred_at: "2026-01-01T00:00:00Z",
    task,
  };
}

function planEvent(tasks: PlanTask[] | null): SessionEvent {
  return {
    kind: Kind.KindPlanActivity,
    occurred_at: "2026-01-01T00:00:00Z",
    plan: { tasks },
  };
}

/** One plan snapshot: a done item, an active one with an active_form, a
 * runnable pending one, and a pending one blocked on the active task. */
const planTasks: PlanTask[] = [
  { id: "1", subject: "Create path: driver port", status: "completed" },
  {
    id: "2",
    subject: "Write the driver port",
    status: "in_progress",
    active_form: "Creating path: driver port",
  },
  { id: "3", subject: "HTTP: creatorResponse", status: "pending" },
  {
    id: "4",
    subject: "Docs: openapi.yaml",
    status: "pending",
    blocked_by: ["2"],
  },
];

const runningTask = taskEvent({
  task_id: "task-1",
  description: "Research the provider catalog",
  status: ToolStatus.ToolStatusRunning,
  progress: "call web_fetch https://docs.example.com",
  steps: 12,
  background: true,
});

const completedTask = taskEvent({
  task_id: "task-1",
  description: "Research the provider catalog",
  status: ToolStatus.ToolStatusCompleted,
  result: "Two providers fit.",
  background: true,
});

function foldOf(...events: SessionEvent[]): Entry[] {
  return events.reduce<Entry[]>(
    (entries: Entry[], e: SessionEvent) => applyEvent(entries, e),
    []
  );
}

/** Expands the minimized pill into the full card. */
function expandCard(): void {
  act(() => {
    fireEvent.click(screen.getByTestId("run-card-pill"));
  });
}

/** Opens a collapsed plan group by its toggle. */
function expandGroup(testId: string): void {
  act(() => {
    fireEvent.click(screen.getByTestId(`${testId}-toggle`));
  });
}

beforeEach(() => {
  useChatStore.setState({
    entries: [],
    activeAgentID: null,
    panelTabs: [],
  });
});

describe("RunSurfaces (task 3.5)", () => {
  it("renders nothing when neither section has content", async () => {
    await renderUI(<RunSurfaces />);

    expect(screen.queryByTestId("run-card")).toBeNull();
  });

  it("renders nothing when the plan snapshot holds no tasks", async () => {
    await act(async () => {
      useChatStore.setState({ entries: foldOf(planEvent([])) });
    });
    const { unmount } = await renderUI(<RunSurfaces />);

    expect(screen.queryByTestId("run-card")).toBeNull();

    await unmount();

    await act(async () => {
      useChatStore.setState({ entries: foldOf(planEvent(null)) });
    });
    const { unmount: unmount2 } = await renderUI(<RunSurfaces />);

    expect(screen.queryByTestId("run-card")).toBeNull();

    await unmount2();
  });

  it("starts minimized: the pill summarizes both sections, the sections stay hidden", async () => {
    await act(async () => {
      useChatStore.setState({
        entries: foldOf(planEvent(planTasks), runningTask),
      });
    });
    const { unmount } = await renderUI(<RunSurfaces />);

    const pill = screen.getByTestId("run-card-pill");

    expect(pill.textContent).toContain("Progress");
    expect(pill.textContent).toContain("1/4");
    expect(pill.textContent).toContain("Agents");
    expect(pill.textContent).toContain("1 running");
    expect(screen.queryByTestId("run-card-plan")).toBeNull();
    expect(screen.queryByTestId("run-card-tasks")).toBeNull();

    expandCard();

    expect(screen.getByTestId("run-card-plan")).not.toBeNull();
    expect(screen.getByTestId("run-card-tasks")).not.toBeNull();

    // Activating the pill again collapses back to it.
    expandCard();

    expect(screen.queryByTestId("run-card-plan")).toBeNull();
    expect(screen.queryByTestId("run-card-tasks")).toBeNull();

    await unmount();
  });

  it("renders the Progress section: counts, the active form, pending rows, collapsed groups", async () => {
    await act(async () => {
      useChatStore.setState({ entries: foldOf(planEvent(planTasks)) });
    });
    const { unmount } = await renderUI(<RunSurfaces />);
    expandCard();

    const plan = screen.getByTestId("run-card-plan");

    expect(plan.textContent).toContain("Progress");
    expect(plan.textContent).toContain("1/4");
    // The in-progress row shows the task's active_form, not its subject.
    expect(plan.textContent).toContain("Creating path: driver port");
    expect(plan.textContent).not.toContain("Write the driver port");
    // Free pending rows render expanded; the blocked one stays folded.
    expect(plan.textContent).toContain("HTTP: creatorResponse");
    expect(plan.textContent).not.toContain("Docs: openapi.yaml");
    expect(screen.queryByTestId("plan-completed-items")).toBeNull();
    expect(screen.queryByTestId("plan-waiting-items")).toBeNull();

    // The completed group expands to its checked rows.
    expandGroup("plan-completed");

    expect(screen.getByTestId("plan-completed-items").textContent).toContain(
      "Create path: driver port"
    );
    expect(
      screen.getByTestId("plan-completed-items").querySelector(".lucide-check")
    ).not.toBeNull();
    // Completed rows strike through; the section's live rows do not.
    expect(
      screen.getByTestId("plan-completed-items").querySelector(".line-through")
    ).not.toBeNull();
    expect(plan.querySelectorAll(".line-through").length).toBe(1);

    // The waiting group expands naming its blockers.
    expandGroup("plan-waiting");

    const waiting = screen.getByTestId("plan-waiting-items");

    expect(waiting.textContent).toContain("Docs: openapi.yaml");
    expect(waiting.textContent).toContain("blocked by #2");

    await unmount();
  });

  it("hides the Progress section when no plan entry exists", async () => {
    await act(async () => {
      useChatStore.setState({ entries: foldOf(runningTask) });
    });
    const { unmount } = await renderUI(<RunSurfaces />);

    expect(screen.getByTestId("run-card-pill").textContent).not.toContain(
      "Progress"
    );

    expandCard();

    expect(screen.queryByTestId("run-card-plan")).toBeNull();
    expect(screen.getByTestId("run-card-tasks")).not.toBeNull();

    await unmount();
  });

  it("hides the Agents section when the session has no task entries", async () => {
    await act(async () => {
      useChatStore.setState({ entries: foldOf(planEvent(planTasks)) });
    });
    const { unmount } = await renderUI(<RunSurfaces />);

    expect(screen.getByTestId("run-card-pill").textContent).not.toContain(
      "Agents"
    );

    expandCard();

    expect(screen.getByTestId("run-card-plan")).not.toBeNull();
    expect(screen.queryByTestId("run-card-tasks")).toBeNull();

    await unmount();
  });

  it("shows a running agent's spinner and progress line", async () => {
    await act(async () => {
      useChatStore.setState({ entries: foldOf(runningTask) });
    });
    const { unmount } = await renderUI(<RunSurfaces />);
    expandCard();

    const tasks = screen.getByTestId("run-card-tasks");

    expect(tasks.textContent).toContain("Research the provider catalog");
    expect(tasks.textContent).toContain(
      "call web_fetch https://docs.example.com"
    );
    expect(tasks.textContent).toContain("· 12 steps");
    expect(tasks.querySelector(".animate-spin")).not.toBeNull();

    await unmount();
  });

  it("falls back to the Starting placeholder before the first progress report", async () => {
    await act(async () => {
      useChatStore.setState({
        entries: foldOf(
          taskEvent({
            task_id: "task-1",
            description: "Research",
            status: ToolStatus.ToolStatusRunning,
            background: true,
          })
        ),
      });
    });
    const { unmount } = await renderUI(<RunSurfaces />);
    expandCard();

    expect(screen.getByTestId("run-card-tasks").textContent).toContain(
      "Starting…"
    );

    await unmount();
  });

  it("shows a failed agent's chip and the first line of its result", async () => {
    await act(async () => {
      useChatStore.setState({
        entries: foldOf(
          taskEvent({
            task_id: "task-2",
            description: "Audit",
            status: ToolStatus.ToolStatusFailed,
            result: "provider unreachable\n    at fetch (index.js:1)",
          })
        ),
      });
    });
    const { unmount } = await renderUI(<RunSurfaces />);
    expandCard();

    const tasks = screen.getByTestId("run-card-tasks");

    expect(tasks.textContent).toContain("failed");
    expect(tasks.textContent).toContain("provider unreachable");
    expect(tasks.textContent).not.toContain("at fetch (index.js:1)");

    await unmount();
  });

  it("clips a failed row's long error to one line (truncate needs a block box)", async () => {
    // The row renders inside the activating <button>; a bare span there stays
    // inline, where overflow/text-overflow do not apply and the error line
    // overflows the island sideways instead of clipping.
    await act(async () => {
      useChatStore.setState({
        entries: foldOf(
          taskEvent({
            task_id: "task-2",
            description: "Audit",
            status: ToolStatus.ToolStatusFailed,
            result:
              "[NodeRunError] failed to stream tool call chatcmpl-tx9: connection reset while streaming the summary it generated",
            transcript_path: ".tasks/call-2.output",
          })
        ),
        activeAgentID: "agent-7",
      });
    });
    const { unmount } = await renderUI(<RunSurfaces />);
    expandCard();

    const row = screen.getByTestId("run-agent-row-0");

    expect(row.className).toContain("flex-col");

    const errorSpan = Array.from(row.querySelectorAll("span")).find(
      (span: Element): boolean =>
        span.textContent?.startsWith("[NodeRunError]") === true
    );

    expect(errorSpan).toBeDefined();
    expect(errorSpan?.className).toContain("truncate");
    // The span blockifies as a flex child of the row button.
    expect(errorSpan?.parentElement).toBe(row);

    await unmount();
  });

  it("marks a completed agent with a check and a stopped one with its chip", async () => {
    await act(async () => {
      useChatStore.setState({
        entries: foldOf(
          completedTask,
          taskEvent({
            task_id: "task-3",
            description: "Sweep",
            status: ToolStatus.ToolStatusStopped,
          })
        ),
      });
    });
    const { unmount } = await renderUI(<RunSurfaces />);

    expect(screen.getByTestId("run-card-pill").textContent).toContain("1 done");
    expect(screen.getByTestId("run-card-pill").textContent).toContain(
      "1 stopped"
    );

    expandCard();

    const tasks = screen.getByTestId("run-card-tasks");

    expect(tasks.querySelector(".lucide-check")).not.toBeNull();
    expect(tasks.textContent).toContain("stopped");

    await unmount();
  });

  it("a terminal record replaces the running row in place", async () => {
    await act(async () => {
      useChatStore.setState({ entries: foldOf(runningTask) });
    });
    const { unmount } = await renderUI(<RunSurfaces />);
    expandCard();

    expect(
      screen.getByTestId("run-card-tasks").querySelector(".animate-spin")
    ).not.toBeNull();

    await act(async () => {
      useChatStore.setState({
        entries: foldOf(runningTask, completedTask),
      });
    });

    const tasks = screen.getByTestId("run-card-tasks");

    expect(tasks.querySelector(".lucide-check")).not.toBeNull();
    expect(tasks.querySelector(".animate-spin")).toBeNull();
    expect(screen.getByTestId("run-card-pill").textContent).toContain("1 done");

    await unmount();
  });

  it("a second plan snapshot replaces the first in place", async () => {
    await act(async () => {
      useChatStore.setState({ entries: foldOf(planEvent(planTasks)) });
    });
    const { unmount } = await renderUI(<RunSurfaces />);
    expandCard();

    expect(screen.getByTestId("run-card-plan").textContent).toContain("1/4");

    await act(async () => {
      useChatStore.setState({
        entries: foldOf(
          planEvent(planTasks),
          planEvent([
            {
              id: "1",
              subject: "Create path: driver port",
              status: "completed",
            },
            { id: "2", subject: "Write the driver port", status: "completed" },
            {
              id: "3",
              subject: "HTTP: creatorResponse",
              status: "in_progress",
            },
            {
              id: "4",
              subject: "Docs: openapi.yaml",
              status: "pending",
              blocked_by: ["3"],
            },
            { id: "5", subject: "Ship it", status: "pending" },
          ])
        ),
      });
    });

    expect(screen.getByTestId("run-card-plan").textContent).toContain("2/5");

    await unmount();
  });

  it("activating an Agents row opens its trajectory tab (design D3)", async () => {
    await act(async () => {
      useChatStore.setState({
        entries: foldOf(
          taskEvent({
            task_id: "task-9",
            description: "Research the provider catalog",
            status: ToolStatus.ToolStatusCompleted,
            result: "Two providers fit.",
            transcript_path: ".tasks/call-9.output",
          })
        ),
        activeAgentID: "agent-7",
      });
    });
    const { unmount } = await renderUI(<RunSurfaces />);
    expandCard();

    await act(async () => {
      fireEvent.click(screen.getByTestId("run-agent-row-0"));
    });

    const tabs = useChatStore.getState().panelTabs;

    expect(tabs).toHaveLength(1);
    expect(tabs[0]?.identity).toBe("trajectory:agent-7:task-9");
    expect(tabs[0]?.kind).toBe("trajectory");
    expect(tabs[0]?.title).toBe("Research the provider catalog");

    // Re-activating the same row dedupes on the identity, not a new tab.
    await act(async () => {
      fireEvent.click(screen.getByTestId("run-agent-row-0"));
    });

    expect(useChatStore.getState().panelTabs).toHaveLength(1);

    await unmount();
  });

  it("a row without a recorded transcript path stays inert", async () => {
    // completedTask carries no transcript_path (legacy-shaped record).
    await act(async () => {
      useChatStore.setState({
        entries: foldOf(completedTask),
        activeAgentID: "agent-7",
      });
    });
    const { unmount } = await renderUI(<RunSurfaces />);
    expandCard();

    expect(screen.getByTestId("run-agent-row-0")).not.toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByTestId("run-agent-row-0"));
    });

    expect(useChatStore.getState().panelTabs).toEqual([]);

    await unmount();
  });

  it("anchors to the viewport's relative wrapper, not the chat column", async () => {
    const entries = foldOf(
      {
        kind: Kind.KindUserMessage,
        occurred_at: "2026-01-01T00:00:00Z",
        text: "hello",
        turn_id: "t1",
      },
      runningTask
    );

    const h = await renderThread(entries);

    const card = screen.getByTestId("run-card");

    // The card is absolutely positioned (it floats) and its parent is the
    // viewport's relative wrapper — not the chat column and not the
    // scrolling content.
    expect(card.className).toContain("absolute");
    expect(card.className).toContain("top-3");

    const wrapper = card.parentElement;

    expect(wrapper?.className).toContain("relative");
    expect(wrapper?.className).toContain("flex-1");
    expect(wrapper?.querySelector('[data-testid="transcript"]')).not.toBeNull();

    await h.unmount();
  });
});
