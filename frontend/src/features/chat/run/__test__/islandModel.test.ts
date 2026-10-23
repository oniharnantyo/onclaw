/**
 * Unit tests for the run island's pure model (spec chat-panels run card):
 * planGroups folds a snapshot into the Progress section's groups, and
 * agentRows / agentCounts project the session's task entries into the Agents
 * section's rows and tallies. Pure derivations, so the tests drive the
 * helpers directly with hand-built entries.
 */
import { describe, expect, it } from "vite-plus/test";

import {
  ToolStatus,
  type PlanTask,
  type TaskActivity,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { Entry, PlanEntry, TaskEntry } from "../../types";
import {
  agentCounts,
  agentRows,
  planGroups,
  taskEntries,
} from "../islandModel";
import type { AgentRow } from "../islandModel";

let nextID = 0;

function planEntryOf(tasks: PlanTask[] | null): PlanEntry {
  nextID += 1;

  return {
    kind: "plan",
    id: `entry-${nextID}`,
    plan: { tasks },
    at: "2026-01-01T00:00:00Z",
  };
}

function taskEntryOf(task: TaskActivity): TaskEntry {
  nextID += 1;

  return {
    kind: "task",
    id: `entry-${nextID}`,
    task,
    at: "2026-01-01T00:00:00Z",
  };
}

function userEntry(): Entry {
  nextID += 1;

  return {
    kind: "user",
    id: `entry-${nextID}`,
    text: "hello",
    at: "2026-01-01T00:00:00Z",
  };
}

describe("planGroups", () => {
  it("returns empty groups for a missing or empty snapshot", () => {
    for (const tasks of [null, undefined, []]) {
      const groups = planGroups(tasks);

      expect(groups).toEqual({
        total: 0,
        completed: [],
        inProgress: [],
        pending: [],
        waiting: [],
      });
    }
  });

  it("groups the snapshot's tasks by status, in plan order", () => {
    const tasks: PlanTask[] = [
      { id: "1", subject: "done one", status: "completed" },
      { id: "2", subject: "active one", status: "in_progress" },
      { id: "3", subject: "free one", status: "pending" },
      { id: "4", subject: "done two", status: "completed" },
    ];

    const groups = planGroups(tasks);

    expect(groups.total).toBe(4);
    expect(groups.completed.map((t: PlanTask): string => t.id)).toEqual([
      "1",
      "4",
    ]);
    expect(groups.inProgress.map((t: PlanTask): string => t.id)).toEqual(["2"]);
    expect(groups.pending.map((t: PlanTask): string => t.id)).toEqual(["3"]);
    expect(groups.waiting).toEqual([]);
  });

  it("splits blocked pending tasks into the waiting group", () => {
    const tasks: PlanTask[] = [
      { id: "3", subject: "free", status: "pending" },
      {
        id: "4",
        subject: "blocked",
        status: "pending",
        blocked_by: ["1", "2"],
      },
      { id: "5", subject: "empty blockers", status: "pending", blocked_by: [] },
      {
        id: "6",
        subject: "null blockers",
        status: "pending",
        blocked_by: null,
      },
    ];

    const groups = planGroups(tasks);

    expect(groups.waiting.map((t: PlanTask): string => t.id)).toEqual(["4"]);
    expect(groups.pending.map((t: PlanTask): string => t.id)).toEqual([
      "3",
      "5",
      "6",
    ]);
  });

  it("rides unknown statuses with the pending tasks", () => {
    const groups = planGroups([
      { id: "1", subject: "deleted?", status: "deleted" },
    ]);

    expect(groups.total).toBe(1);
    expect(groups.pending.map((t: PlanTask): string => t.id)).toEqual(["1"]);
  });
});

describe("taskEntries", () => {
  it("keeps task entries in transcript order, dropping other kinds", () => {
    const task = taskEntryOf({
      task_id: "task-1",
      description: "Research",
      status: ToolStatus.ToolStatusRunning,
    });

    const entries = [userEntry(), task];

    expect(taskEntries(entries)).toEqual([task]);
  });

  it("drops task entries without a payload", () => {
    const entry = taskEntryOf({
      task_id: "task-1",
      status: ToolStatus.ToolStatusRunning,
    });

    // TaskEntry.task admits undefined (a payload-less live form).
    expect(taskEntries([{ ...entry, task: undefined }])).toEqual([]);
  });
});

describe("agentRows", () => {
  it("projects a running task with its progress line and step count", () => {
    const rows = agentRows([
      taskEntryOf({
        task_id: "task-1",
        description: "Research",
        status: ToolStatus.ToolStatusRunning,
        progress: "call web_fetch https://example.com",
        steps: 12,
      }),
    ]);

    expect(rows).toEqual([
      {
        id: "task-1",
        name: "Research",
        state: "running",
        detail: "call web_fetch https://example.com · 12 steps",
        error: "",
      },
    ]);
  });

  it("falls back to the Starting placeholder without progress", () => {
    const rows = agentRows([
      taskEntryOf({
        task_id: "task-1",
        description: "Research",
        status: ToolStatus.ToolStatusRunning,
      }),
      taskEntryOf({
        task_id: "task-2",
        description: "Sweep",
        status: ToolStatus.ToolStatusRunning,
        progress: "   ",
        steps: 0,
      }),
    ]);

    expect(rows.map((r: AgentRow): string => r.detail)).toEqual([
      "Starting…",
      "Starting…",
    ]);
  });

  it("names a description-less task Background task", () => {
    const rows = agentRows([
      taskEntryOf({ task_id: "task-1", status: ToolStatus.ToolStatusRunning }),
    ]);

    expect(rows[0]?.name).toBe("Background task");
  });

  it("extracts the first non-empty line of a failed task's result", () => {
    const rows = agentRows([
      taskEntryOf({
        task_id: "task-1",
        description: "Audit",
        status: ToolStatus.ToolStatusFailed,
        result: "\n  provider unreachable  \n    at fetch",
      }),
    ]);

    expect(rows[0]?.state).toBe("failed");
    expect(rows[0]?.error).toBe("provider unreachable");
  });

  it("leaves terminal rows without detail or error", () => {
    const rows = agentRows([
      taskEntryOf({
        task_id: "task-1",
        description: "Done",
        status: ToolStatus.ToolStatusCompleted,
        result: "answer",
        progress: "stale line",
        steps: 4,
      }),
      taskEntryOf({
        task_id: "task-2",
        description: "Stopped",
        status: ToolStatus.ToolStatusStopped,
      }),
    ]);

    expect(rows).toEqual([
      { id: "task-1", name: "Done", state: "completed", detail: "", error: "" },
      {
        id: "task-2",
        name: "Stopped",
        state: "stopped",
        detail: "",
        error: "",
      },
    ]);
  });

  it("treats a status-less record as completed (legacy payload)", () => {
    const rows = agentRows([
      taskEntryOf({ task_id: "task-1", description: "Old" }),
    ]);

    expect(rows[0]?.state).toBe("completed");
  });

  it("skips task entries without a payload", () => {
    const entry = taskEntryOf({
      task_id: "task-1",
      status: ToolStatus.ToolStatusRunning,
    });

    // TaskEntry.task admits undefined (a payload-less live form).
    expect(agentRows([{ ...entry, task: undefined }])).toEqual([]);
  });
});

describe("agentCounts", () => {
  it("tallies the rows by state", () => {
    const rows = agentRows([
      taskEntryOf({
        task_id: "task-1",
        status: ToolStatus.ToolStatusRunning,
      }),
      taskEntryOf({
        task_id: "task-2",
        status: ToolStatus.ToolStatusCompleted,
      }),
      taskEntryOf({
        task_id: "task-3",
        status: ToolStatus.ToolStatusCompleted,
      }),
      taskEntryOf({ task_id: "task-4", status: ToolStatus.ToolStatusFailed }),
      taskEntryOf({ task_id: "task-5", status: ToolStatus.ToolStatusStopped }),
    ]);

    expect(agentCounts(rows)).toEqual({
      running: 1,
      completed: 2,
      failed: 1,
      stopped: 1,
    });
  });

  it("returns zeroed tallies for no rows", () => {
    expect(agentCounts([])).toEqual({
      running: 0,
      completed: 0,
      failed: 0,
      stopped: 0,
    });
  });
});

describe("planEntry handling", () => {
  it("planGroups ignores a plan entry folded among other entries", () => {
    // The island finds the plan entry itself; planGroups only sees its tasks.
    const entries: Entry[] = [
      userEntry(),
      planEntryOf([{ id: "1", subject: "only", status: "completed" }]),
    ];

    const plan = entries.find((e: Entry): e is PlanEntry => e.kind === "plan");

    expect(plan?.plan?.tasks).toEqual([
      { id: "1", subject: "only", status: "completed" },
    ]);
    expect(planGroups(plan?.plan?.tasks).total).toBe(1);
  });
});
