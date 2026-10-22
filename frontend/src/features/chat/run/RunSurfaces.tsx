/**
 * Run surfaces (tasks 3.5, 9.1–9.2, design D8; spec chat-panels run card):
 * one floating card composing the vendored AgentPlan (the plan lane from
 * `latestPlanItems`) and SubagentList (the tasks lane from the session's
 * task entries) positioned top-right INSIDE the thread viewport's relative
 * wrapper — the wrapper, not the chat column, so the card can never reach
 * the composer, and it stays put while the transcript scrolls beneath it.
 * Both lanes are pure derivations of the store's `entries`, so live events
 * and a replayed transcript rebuild identical lanes, and the card renders
 * only while at least one lane has content.
 *
 * Element-state mapping (the elements carry simpler state models than the
 * lanes): AgentPlan marks everything before `activeIndex` done and shows the
 * item at it as in progress — the first not-done item is the active one,
 * and a failed item's state rides its label (the element has no failed
 * mark). SubagentList marks the first `completedCount` agents done, so
 * completed lanes sort first; a terminal failed/stopped task names its
 * state in the agent's model slot.
 */
import type { ReactElement } from "react";

import { AgentPlan } from "@/components/assistant-ui/elements/agent-plan";
import {
  SubagentList,
  type SubagentItem,
} from "@/components/assistant-ui/elements/subagent-list";

import { ToolStatus } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { latestPlanItems, type PlanItem } from "../entries";
import { useChatStore, type ExtendedChatStore } from "../store";
import type { Entry, TaskEntry } from "../types";

/** One AgentPlan row: a failed item's state rides its label. */
type PlanStep = { label: string };

/** The plan lane AgentPlan consumes: rows plus the first not-done index. */
type PlanLane = {
  steps: readonly PlanStep[];
  activeIndex: number;
};

/** The tasks lane SubagentList consumes: rows plus the completed prefix. */
type TaskLanes = {
  agents: readonly SubagentItem[];
  completedCount: number;
};

/** The plan lane onto the AgentPlan model: the first not-done item is the
 * active one (all done ⇒ past the end, the element's all-done state), and a
 * failed item carries its state — with the reducer's reason — in the label. */
function planSteps(plan: readonly PlanItem[]): PlanLane {
  const steps = plan.map((item: PlanItem): PlanStep => {
    if (item.status !== "failed") return { label: item.text };

    const reason = item.reason === undefined ? "" : `: ${item.reason}`;

    return { label: `${item.text} (failed${reason})` };
  });

  let activeIndex = steps.length;

  for (let i = 0; i < plan.length; i += 1) {
    const item = plan[i];

    if (item !== undefined && item.status !== "done") {
      activeIndex = i;

      break;
    }
  }

  return { steps, activeIndex };
}

/** The task entries of a session, in transcript order. */
function taskEntries(entries: readonly Entry[]): TaskEntry[] {
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

/** The tasks lane onto the SubagentList model: completed lanes sort first so
 * the element's completed-count prefix lands on the right rows; the model
 * slot names a terminal failed/stopped state the element cannot draw. */
function taskLanes(tasks: readonly TaskEntry[]): TaskLanes {
  const completed: SubagentItem[] = [];
  const active: SubagentItem[] = [];

  for (const entry of tasks) {
    const task = entry.task;

    if (task === undefined || task === null) continue;

    const name =
      task.description === undefined || task.description === ""
        ? "Background task"
        : task.description;

    if (task.status === ToolStatus.ToolStatusCompleted) {
      completed.push({ name, model: "" });

      continue;
    }

    if (task.status === ToolStatus.ToolStatusFailed) {
      active.push({ name, model: "failed" });

      continue;
    }

    if (task.status === ToolStatus.ToolStatusStopped) {
      active.push({ name, model: "stopped" });

      continue;
    }

    active.push({ name, model: "" });
  }

  return {
    agents: [...completed, ...active],
    completedCount: completed.length,
  };
}

/** The floating run card. */
export function RunSurfaces(): ReactElement | null {
  const entries = useChatStore((s: ExtendedChatStore) => s.entries);

  const tasks = taskEntries(entries);
  const plan = latestPlanItems(entries);

  if (tasks.length === 0 && plan.length === 0) return null;

  const { agents, completedCount } = taskLanes(tasks);
  const { steps, activeIndex } = planSteps(plan);

  // The element reads a per-index percentage; completed lanes are done,
  // everything else has no progress to show.
  const progress: number[] = [];

  for (let i = 0; i < agents.length; i += 1) {
    progress.push(i < completedCount ? 100 : 0);
  }

  return (
    <div
      className="absolute right-3 top-3 z-20 max-h-72 w-80 max-w-[calc(100%-1.5rem)] overflow-y-auto rounded-md border border-line bg-surface p-3 shadow-[var(--elev-raised)] lg:right-14"
      data-testid="run-card"
    >
      {tasks.length > 0 && (
        <div data-testid="run-card-tasks">
          <SubagentList
            agents={agents}
            completedCount={completedCount}
            progress={progress}
            showSummary={completedCount < agents.length}
            summaryAgent={{ name: "Working", model: "" }}
          />
        </div>
      )}
      {plan.length > 0 && (
        <div
          data-testid="run-card-plan"
          className={
            tasks.length > 0 ? "mt-3 border-t border-line pt-3" : undefined
          }
        >
          <AgentPlan steps={steps} activeIndex={activeIndex} title="Plan" />
        </div>
      )}
    </div>
  );
}
