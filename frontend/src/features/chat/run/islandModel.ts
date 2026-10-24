/**
 * The run island's pure model (spec chat-panels run card): the plan snapshot
 * grouped into the Progress section's rows, and the session's task entries
 * projected into the Agents section's rows. Both are pure derivations of the
 * store's `entries`, so live events and a replayed transcript rebuild
 * identical sections.
 */
import {
  ToolStatus,
  type PlanTask,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { Entry, TaskEntry } from "../types";

/** The Progress section: the snapshot's tasks in plan order, grouped by
 * status — pending tasks waiting on blockers split out of the free ones. */
export interface PlanGroups {
  total: number;
  completed: PlanTask[];
  inProgress: PlanTask[];
  /** Pending tasks whose blocked_by is empty — runnable now. */
  pending: PlanTask[];
  /** Pending tasks blocked on other tasks ("N waiting" group). */
  waiting: PlanTask[];
}

/** Groups one plan snapshot for the Progress section. Statuses outside the
 * plantask vocabulary ride with the pending tasks. */
export function planGroups(
  tasks: readonly PlanTask[] | null | undefined
): PlanGroups {
  const groups: PlanGroups = {
    total: 0,
    completed: [],
    inProgress: [],
    pending: [],
    waiting: [],
  };

  for (const task of tasks ?? []) {
    groups.total += 1;

    if (task.status === "completed") groups.completed.push(task);
    else if (task.status === "in_progress") groups.inProgress.push(task);
    else if ((task.blocked_by ?? []).length > 0) groups.waiting.push(task);
    else groups.pending.push(task);
  }

  return groups;
}

/** One Agents-section row: a delegated subagent task as the island shows it. */
export interface AgentRow {
  id: string;
  /** The task's description trimmed to the display cap (also the
   * trajectory tab's title). */
  name: string;
  state: "running" | "completed" | "failed" | "stopped";
  /** A running child's latest activity with its step count ("· 12 steps"),
   * or the "Starting…" placeholder before the first report; "" on terminal
   * rows. */
  detail: string;
  /** The first line of a failed task's result; "" otherwise. */
  error: string;
  /** The record's workspace-relative transcript path (".tasks/<tool-call-id>.output");
   * "" when the record carries none (legacy/persisted-before-layout rows). */
  transcriptPath: string;
}

/** The task entries of a session, in transcript order. */
export function taskEntries(entries: readonly Entry[]): TaskEntry[] {
  const tasks: TaskEntry[] = [];

  for (const entry of entries) {
    if (
      entry.kind === "task" &&
      entry.task !== undefined &&
      entry.task !== null
    ) {
      tasks.push(entry);
    }
  }

  return tasks;
}

/** The display cap for a delegated task's description (the parent model
 * writes it freely; the run card is a narrow island and the trajectory tab
 * title repeats it verbatim). */
const MAX_TASK_NAME = 36;

/** Trims a task's description to the display cap, cutting at a word
 * boundary when one sits past the cap's midpoint so the ellipsis never
 * lands mid-word. */
function trimTaskName(description: string): string {
  if (description.length <= MAX_TASK_NAME) return description;

  const cut = description.slice(0, MAX_TASK_NAME);
  const wordEnd = cut.lastIndexOf(" ");

  const trimmed = wordEnd > MAX_TASK_NAME / 2 ? cut.slice(0, wordEnd) : cut;

  return `${trimmed.trimEnd()}…`;
}

/** Projects the session's task entries into Agents rows, in transcript
 * order. A record without a status is legacy (persisted before statuses
 * existed) and terminal like completed. */
export function agentRows(entries: readonly Entry[]): AgentRow[] {
  const rows: AgentRow[] = [];

  for (const entry of taskEntries(entries)) {
    const task = entry.task;

    if (task === undefined || task === null) continue;

    const state = agentState(task.status);

    rows.push({
      id: task.task_id,
      name:
        task.description === undefined || task.description === ""
          ? "Background task"
          : trimTaskName(task.description),
      state,
      detail:
        state === "running" ? runningDetail(task.progress, task.steps) : "",
      error: state === "failed" ? firstLine(task.result) : "",
      transcriptPath: task.transcript_path ?? "",
    });
  }

  return rows;
}

/** The Agents rows' status tally — the pill's and the section header's
 * counts. */
export interface AgentCounts {
  running: number;
  completed: number;
  failed: number;
  stopped: number;
}

export function agentCounts(rows: readonly AgentRow[]): AgentCounts {
  const counts: AgentCounts = {
    running: 0,
    completed: 0,
    failed: 0,
    stopped: 0,
  };

  for (const row of rows) {
    counts[row.state] += 1;
  }

  return counts;
}

function agentState(status: ToolStatus | undefined): AgentRow["state"] {
  if (status === ToolStatus.ToolStatusRunning) return "running";

  if (status === ToolStatus.ToolStatusFailed) return "failed";

  if (status === ToolStatus.ToolStatusStopped) return "stopped";

  return "completed";
}

function runningDetail(
  progress: string | undefined,
  steps: number | undefined
): string {
  const line =
    progress === undefined || progress.trim() === "" ? "Starting…" : progress;

  return steps === undefined || steps <= 0 ? line : `${line} · ${steps} steps`;
}

function firstLine(text: string | undefined): string {
  if (text === undefined || text === null) return "";

  const line = text
    .split("\n")
    .map((l: string): string => l.trim())
    .find(Boolean);

  return line ?? "";
}
