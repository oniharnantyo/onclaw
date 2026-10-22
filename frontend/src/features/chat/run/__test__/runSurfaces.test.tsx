/**
 * Run surfaces tests (task 3.5, spec chat-panels run card): the card hides
 * with no lane content, the plan lane is absent without plan data, both
 * lanes rebuild from replayed events (they are pure derivations of the
 * store's entries), and the card anchors to the thread viewport's relative
 * wrapper — never the chat column — so it cannot reach the composer.
 */
import { act } from "react";
import { screen } from "@testing-library/dom";
import { beforeEach, describe, expect, it } from "vite-plus/test";

import {
  Kind,
  ToolStatus,
  type Event as SessionEvent,
  type TaskActivity,
  type ToolActivity,
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

function todoWriteEvent(tool: Partial<ToolActivity>): SessionEvent {
  return {
    kind: Kind.KindToolActivity,
    occurred_at: "2026-01-01T00:00:00Z",
    tool: { call_id: "call-1", name: "todo_write", ...tool },
  };
}

/** Plan payload exactly as todo_write persists it ({items, revision}). */
function planJSON(
  items: Array<{
    key: string;
    text: string;
    status: string;
    reason?: string;
  }>
): string {
  return JSON.stringify({ items, revision: 1 });
}

const runningTask = taskEvent({
  task_id: "task-1",
  description: "Research the provider catalog",
  status: ToolStatus.ToolStatusRunning,
  background: true,
});

const completedTask = taskEvent({
  task_id: "task-1",
  description: "Research the provider catalog",
  status: ToolStatus.ToolStatusCompleted,
  result: "Two providers fit.",
  background: true,
});

const fourItemPlan = planJSON([
  { key: "a", text: "Read the config", status: "done" },
  { key: "b", text: "Sweep the workspace", status: "active" },
  { key: "c", text: "Draft the report", status: "pending" },
  { key: "d", text: "Call the provider", status: "failed", reason: "offline" },
]);

function foldOf(...events: SessionEvent[]): Entry[] {
  return events.reduce<Entry[]>(
    (entries: Entry[], e: SessionEvent) => applyEvent(entries, e),
    []
  );
}

beforeEach(() => {
  useChatStore.setState({ entries: [] });
});

describe("RunSurfaces (task 3.5)", () => {
  it("renders nothing when no lane has content", async () => {
    await renderUI(<RunSurfaces />);

    expect(screen.queryByTestId("run-card")).toBeNull();
  });

  it("shows a running task until the terminal record lands, in place", async () => {
    await act(async () => {
      useChatStore.setState({ entries: foldOf(runningTask) });
    });
    const { unmount } = await renderUI(<RunSurfaces />);

    const tasks = screen.getByTestId("run-card-tasks");

    expect(tasks.textContent).toContain("Research the provider catalog");
    expect(tasks.querySelector(".animate-spin")).not.toBeNull();

    // The terminal record replaces the running row in place (the completed
    // lane sorts first, so the element's done-mark lands on it).
    await act(async () => {
      useChatStore.setState({ entries: foldOf(runningTask, completedTask) });
    });

    expect(
      screen.getByTestId("run-card-tasks").querySelector(".lucide-check")
    ).not.toBeNull();
    expect(screen.getByTestId("run-card-tasks").textContent).not.toContain(
      "Working"
    );

    await unmount();
  });

  it("rebuilds the terminal lane from a replayed transcript alone", async () => {
    await act(async () => {
      useChatStore.setState({ entries: foldOf(completedTask) });
    });
    const { unmount } = await renderUI(<RunSurfaces />);

    expect(screen.getByTestId("run-card-tasks").textContent).toContain(
      "Research the provider catalog"
    );

    await unmount();
  });

  it("names a terminal failed or stopped state in the lane", async () => {
    await act(async () => {
      useChatStore.setState({
        entries: foldOf(
          taskEvent({
            task_id: "task-2",
            description: "Audit",
            status: ToolStatus.ToolStatusFailed,
            result: "provider unreachable",
          }),
          taskEvent({
            task_id: "task-3",
            description: "Sweep",
            status: ToolStatus.ToolStatusStopped,
          })
        ),
      });
    });
    const { unmount } = await renderUI(<RunSurfaces />);

    const tasks = screen.getByTestId("run-card-tasks");

    expect(tasks.textContent).toContain("failed");
    expect(tasks.textContent).toContain("stopped");

    await unmount();
  });

  it("shows the plan lane with the failed item's state in its label", async () => {
    await act(async () => {
      useChatStore.setState({
        entries: foldOf(
          todoWriteEvent({
            status: ToolStatus.ToolStatusCompleted,
            result: fourItemPlan,
          })
        ),
      });
    });
    const { unmount } = await renderUI(<RunSurfaces />);

    const plan = screen.getByTestId("run-card-plan");

    expect(plan.textContent).toContain("Read the config");
    // The element cannot draw a failed mark: the state rides the label.
    expect(plan.textContent).toContain("Call the provider (failed: offline)");
    expect(plan.textContent).toContain("1 of 4");

    await unmount();
  });

  it("updates the checklist live mid-turn, latest write wins", async () => {
    const runningWrite = todoWriteEvent({
      call_id: "call-1",
      status: ToolStatus.ToolStatusRunning,
      arguments: planJSON([
        { key: "a", text: "Old step", status: "pending" },
        { key: "b", text: "Other step", status: "active" },
      ]),
    });

    await act(async () => {
      useChatStore.setState({ entries: foldOf(runningWrite) });
    });
    const { unmount } = await renderUI(<RunSurfaces />);

    expect(screen.getByTestId("run-card-plan").textContent).toContain(
      "Old step"
    );

    await act(async () => {
      useChatStore.setState({
        entries: foldOf(
          runningWrite,
          todoWriteEvent({
            call_id: "call-1",
            status: ToolStatus.ToolStatusCompleted,
            result: fourItemPlan,
          })
        ),
      });
    });

    expect(screen.getByTestId("run-card-plan").textContent).toContain(
      "Read the config"
    );
    expect(screen.queryByTestId("run-card-tasks")).toBeNull();

    await unmount();
  });

  it("hides entirely when the plan clears or the latest write failed", async () => {
    await act(async () => {
      useChatStore.setState({
        entries: foldOf(
          todoWriteEvent({
            status: ToolStatus.ToolStatusCompleted,
            result: planJSON([]),
          })
        ),
      });
    });
    const { unmount } = await renderUI(<RunSurfaces />);

    expect(screen.queryByTestId("run-card")).toBeNull();

    await unmount();

    await act(async () => {
      useChatStore.setState({
        entries: foldOf(
          todoWriteEvent({
            status: ToolStatus.ToolStatusFailed,
            result: "todo_write: item 1: key is required",
          })
        ),
      });
    });
    const { unmount: unmount2 } = await renderUI(<RunSurfaces />);

    expect(screen.queryByTestId("run-card")).toBeNull();

    await unmount2();
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
