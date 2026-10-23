/**
 * Run surfaces (tasks 3.5, 9.1–9.2, design D8; spec chat-panels run card):
 * one floating island of two sections positioned top-right INSIDE the thread
 * viewport's relative wrapper — the wrapper, not the chat column, so the
 * island can never reach the composer, and it stays put while the transcript
 * scrolls beneath it. Both sections are pure derivations of the store's
 * `entries` (run/islandModel.ts), so live events and a replayed transcript
 * rebuild identical sections, and the island renders only while at least one
 * section has content.
 *
 * Progress renders the agent's task plan (the newest plan_activity snapshot,
 * one entry per session) as: a collapsed "N completed" group, the in-progress
 * row (its active_form when set), the runnable pending rows, and a collapsed
 * "N waiting" group for the pending tasks whose blocked_by is non-empty.
 * Agents renders the session's delegated task entries: a running row carries
 * the child's latest activity as a muted progress line, a failed row its
 * result's first line, and terminal rows name their state in a chip.
 *
 * The island starts MINIMIZED: a small pill (spinner, the sections' summary
 * — "3/7 · 1 running" — with done/failed/stopped tallies) like a toolbar
 * badge; activating it expands the full card, activating again collapses it
 * back. The expansion is view state only — the sections stay store-derived
 * either way.
 */
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";
import {
  ArrowRightIcon,
  CheckIcon,
  ChevronRightIcon,
  ListTodoIcon,
  Loader2Icon,
} from "lucide-react";

import {
  SubagentList,
  type SubagentItem,
} from "@/components/assistant-ui/elements/subagent-list";

import { cn } from "../../../lib/cn";
import type { PlanTask } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import {
  agentCounts,
  agentRows,
  planGroups,
  type AgentRow,
} from "./islandModel";
import { useChatStore, type ExtendedChatStore } from "../store";
import type { Entry, PlanEntry } from "../types";

/** One "N running" segment of the pill; zero tallies render nothing. */
function CountSegment({
  count,
  label,
  className,
}: {
  count: number;
  label: string;
  className?: string;
}): ReactElement | null {
  if (count === 0) return null;

  return (
    <span className={cn("font-mono text-[11.5px]", className)}>
      {count} {label}
    </span>
  );
}

/** The marker + subject row of one plan task. The in-progress row shows the
 * task's active_form when the agent set one, and a waiting row names its
 * blockers; both subject and detail lines truncate in CSS. */
function PlanTaskRow({
  task,
  variant,
  blockedBy = [],
}: {
  task: PlanTask;
  variant: "done" | "active" | "pending";
  blockedBy?: readonly string[];
}): ReactElement {
  const label =
    variant === "active" && task.active_form ? task.active_form : task.subject;

  return (
    <div className="flex items-center gap-2 text-[13px]">
      <span className="flex size-4 shrink-0 items-center justify-center">
        {variant === "done" ? (
          <CheckIcon
            aria-hidden={true}
            className="text-muted-foreground size-3.5"
          />
        ) : variant === "active" ? (
          <ArrowRightIcon
            aria-hidden={true}
            className="text-foreground/90 size-3.5"
          />
        ) : (
          <span
            aria-hidden={true}
            className="bg-foreground/15 inset-ring-border size-1.5 rounded-full inset-ring"
          />
        )}
      </span>
      <span
        title={label}
        className={cn(
          "min-w-0 flex-1 truncate",
          variant === "active" ? "text-foreground/90" : "text-muted-foreground"
        )}
      >
        {label}
      </span>
      {blockedBy.length > 0 ? (
        <span className="text-muted-foreground shrink-0 font-mono text-[11px]">
          blocked by{" "}
          {blockedBy.map((id: string): string => `#${id}`).join(", ")}
        </span>
      ) : null}
    </div>
  );
}

/** A collapsed-by-default plan group ("3 completed" / "1 waiting"): the
 * chevron row toggles the group's rows; the state is local view state. */
function PlanGroup({
  label,
  count,
  testId,
  children,
}: {
  label: string;
  count: number;
  testId: string;
  children: ReactNode;
}): ReactElement {
  const [open, setOpen] = useState(false);

  return (
    <div>
      <button
        type="button"
        data-testid={`${testId}-toggle`}
        aria-expanded={open}
        aria-label={`${count} ${label}`}
        onClick={(): void => setOpen((current: boolean): boolean => !current)}
        className="text-muted-foreground hover:text-foreground -ml-0.5 flex items-center gap-1 py-0.5 text-[12.5px] transition-colors"
      >
        <ChevronRightIcon
          aria-hidden={true}
          className={cn(
            "size-3 shrink-0 transition-transform duration-200 motion-reduce:transition-none",
            open && "rotate-90"
          )}
        />
        <span className="font-medium">
          {count} {label}
        </span>
      </button>
      {open ? (
        <div
          data-testid={`${testId}-items`}
          className="mt-1 flex flex-col gap-1.5"
        >
          {children}
        </div>
      ) : null}
    </div>
  );
}

/** The floating run island: a minimized pill by default, the full two-section
 * card once expanded. Progress reads the plan snapshot, Agents the delegated
 * task entries. */
export function RunSurfaces(): ReactElement | null {
  const entries = useChatStore((s: ExtendedChatStore) => s.entries);
  const [expanded, setExpanded] = useState(false);

  const rows = agentRows(entries);
  const counts = agentCounts(rows);

  const planEntry = entries.find(
    (e: Entry): e is PlanEntry => e.kind === "plan"
  );

  const groups = planGroups(planEntry?.plan?.tasks);

  const planVisible = groups.total > 0;
  const tasksVisible = rows.length > 0;

  if (!planVisible && !tasksVisible) return null;

  const isRunning = counts.running > 0;
  const allTasksDone = tasksVisible && !isRunning;

  const renderStatusIcon = (): ReactElement => {
    if (isRunning) {
      return (
        <Loader2Icon
          aria-hidden={true}
          className="text-muted-foreground size-3.5 shrink-0 animate-spin motion-reduce:animate-none"
        />
      );
    }

    if (allTasksDone) {
      return (
        <CheckIcon
          aria-hidden={true}
          className="size-3.5 shrink-0 text-emerald-500"
        />
      );
    }

    return <ListTodoIcon aria-hidden={true} className="size-3.5 shrink-0" />;
  };

  return (
    <div
      className={cn(
        "absolute right-3 top-3 z-20",
        expanded &&
          "max-h-[30rem] w-84 max-w-[calc(100%-1.5rem)] overflow-y-auto rounded-xl border border-line/80 bg-surface/95 backdrop-blur-xl p-3.5 shadow-xl transition-all"
      )}
      data-testid="run-card"
    >
      <button
        type="button"
        data-testid="run-card-pill"
        aria-expanded={expanded}
        title="Run activity"
        onClick={(): void =>
          setExpanded((current: boolean): boolean => !current)
        }
        className={cn(
          "border-line bg-surface text-muted-foreground hover:text-foreground flex items-center gap-2 rounded-full border px-3 py-1.5 text-left shadow-[var(--elev-raised)] transition-colors",
          expanded && "mb-3"
        )}
      >
        {renderStatusIcon()}
        {planVisible ? (
          <>
            <span className="text-foreground text-[13px] font-medium">
              Progress
            </span>
            <span className="text-muted-foreground font-mono text-[11.5px]">
              {groups.completed.length}/{groups.total}
            </span>
          </>
        ) : null}
        {planVisible && tasksVisible ? (
          <span className="text-line mx-0.5">•</span>
        ) : null}
        {tasksVisible ? (
          <>
            <span className="text-foreground text-[13px] font-medium">
              Agents
            </span>
            <CountSegment
              count={counts.running}
              label="running"
              className="text-foreground/70"
            />
            <CountSegment
              count={counts.completed}
              label="done"
              className="text-emerald-600 dark:text-emerald-400"
            />
            <CountSegment
              count={counts.failed}
              label="failed"
              className="text-red-600 dark:text-red-400"
            />
            <CountSegment
              count={counts.stopped}
              label="stopped"
              className="text-muted-foreground"
            />
          </>
        ) : null}
      </button>
      {expanded ? (
        <>
          {planVisible ? (
            <div
              data-testid="run-card-plan"
              className={cn(
                tasksVisible && "mb-3.5 pb-3.5 border-b border-line/70"
              )}
            >
              <div className="flex flex-col gap-1.5">
                <div className="flex items-center justify-between px-0.5">
                  <span className="text-[13px] font-medium text-foreground">
                    Progress
                  </span>
                  <span className="font-mono text-[11.5px] text-muted-foreground">
                    {groups.completed.length}/{groups.total}
                  </span>
                </div>
                {groups.completed.length > 0 ? (
                  <PlanGroup
                    testId="plan-completed"
                    label="completed"
                    count={groups.completed.length}
                  >
                    {groups.completed.map((task: PlanTask) => (
                      <PlanTaskRow key={task.id} task={task} variant="done" />
                    ))}
                  </PlanGroup>
                ) : null}
                {groups.inProgress.map((task: PlanTask) => (
                  <PlanTaskRow key={task.id} task={task} variant="active" />
                ))}
                {groups.pending.map((task: PlanTask) => (
                  <PlanTaskRow key={task.id} task={task} variant="pending" />
                ))}
                {groups.waiting.length > 0 ? (
                  <PlanGroup
                    testId="plan-waiting"
                    label="waiting"
                    count={groups.waiting.length}
                  >
                    {groups.waiting.map((task: PlanTask) => (
                      <PlanTaskRow
                        key={task.id}
                        task={task}
                        variant="pending"
                        blockedBy={task.blocked_by ?? []}
                      />
                    ))}
                  </PlanGroup>
                ) : null}
              </div>
            </div>
          ) : null}
          {tasksVisible ? (
            <div data-testid="run-card-tasks" className="flex flex-col gap-2">
              <div className="flex items-center justify-between px-0.5">
                <span className="text-[13px] font-medium text-foreground">
                  Agents
                </span>
                <span className="font-mono text-[11.5px] text-muted-foreground">
                  {isRunning
                    ? `${counts.running} running`
                    : `${counts.completed} completed`}
                </span>
              </div>
              <SubagentList
                agents={rows.map((row: AgentRow): SubagentItem => ({
                  name: row.name,
                  state: row.state,
                  detail: row.detail === "" ? undefined : row.detail,
                  error: row.error === "" ? undefined : row.error,
                }))}
                showSummary={false}
                summaryAgent={{ name: "Working", state: "running" }}
              />
            </div>
          ) : null}
        </>
      ) : null}
    </div>
  );
}
