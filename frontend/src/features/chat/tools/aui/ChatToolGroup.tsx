/**
 * The consecutive-tool-calls group slot (task 4.5, design D6 "Heavy-turn
 * folding"; flattened by `flatten-chat-transcript` design D2). Fewer than
 * four consecutive tool/task parts render INLINE — the group's children, one
 * line each, in the transcript flow. The vendored ghost group is gone: its
 * closed Radix content unmounted the cards, so the transcript showed a
 * "N tool calls" link where the work should have been. Four or more fold
 * behind the vendored
 * `ToolTimeline`: the summary row reads "N steps · M files changed" from the
 * run's completed edit/write calls, names the tool currently running while
 * the group streams, and reveals compact trace rows (icon, verb, target
 * chip) whose activation opens that call's full card beneath the row,
 * followed by the trailing per-file change statistics; the last row
 * shimmers while running (static under reduced motion). A pending approval
 * hard-splits the run so its card renders unfolded with the other calls
 * folded around it; a failed call tints the summary, reveals the rows, and
 * tints the failed row; repeated `todo_write` calls collapse to "plan
 * updated" lines except the last, which keeps its snapshot row. The export
 * name and prop shape are the contract between the transcript (wave 2A) and
 * the tool layer: keep both.
 */
import { createElement, useEffect, useMemo, useState } from "react";
import type { PropsWithChildren, ReactElement } from "react";
import { MessagePrimitive, useAuiState } from "@assistant-ui/react";
import type { AssistantState } from "@assistant-ui/react";
import type { ToolCallMessagePart } from "@assistant-ui/react";

import { cn } from "../../../../lib/cn";
import { ShimmerLabel } from "@/components/assistant-ui/elements/surfaces";
import {
  ToolTimeline,
  type TimelineStat,
  type TimelineStep,
} from "@/components/assistant-ui/elements/tool-timeline";

import { setOpen as setCollapseOpen } from "../collapse";
import { toolRenderers } from "./renderers";
import { changeStats, toolChip, toolIcon, toolVerb } from "./traceRows";

/** A run of consecutive tool-call parts as `MessagePrimitive.GroupedParts`
 * hands it to the group slot (`indices` into the message's parts, `status`). */
export type ToolGroupPart = MessagePrimitive.GroupedParts.GroupPart;

export type ChatToolGroupProps = PropsWithChildren<{ group: ToolGroupPart }>;

/** The narrow call view the fold derives its rows, counts, and stats from. */
interface RunCall {
  toolCallId: string;
  toolName: string;
  argsText: string;
  isError: boolean;
  completed: boolean;
  failed: boolean;
  approval: boolean;
  pendingApproval: boolean;
}

/** Runs below this many calls render as plain stacked cards. */
const FOLD_MIN_STEPS = 4;

/** The parts at one group's indices, narrowed to the call view (a group
 * contains only tool-call parts by construction of the grouping). */
function collectCalls(
  parts: readonly { type: string }[],
  indices: readonly number[]
): RunCall[] {
  const calls: RunCall[] = [];

  for (const index of indices) {
    // SAFETY: the group's indices address tool-call parts only (the
    // groupBy path "group-tool" coalesces exactly those); the narrowed read
    // below checks the type before trusting the fields.
    const part = parts[index] as ToolCallMessagePart | undefined;

    if (part?.type !== "tool-call") continue;

    const failed = part.isError === true;

    calls.push({
      toolCallId: part.toolCallId,
      toolName: part.toolName,
      argsText: part.argsText,
      isError: failed,
      completed: part.result !== undefined,
      failed,
      approval: part.approval !== undefined,
      pendingApproval:
        part.approval !== undefined &&
        part.approval.approved === undefined &&
        part.approval.resolution === undefined,
    });
  }

  return calls;
}

/** The components one part renders through when the fold opens its card on
 * demand (the same registry the transcript wires). */
const ROW_COMPONENTS = {
  tools: { by_name: toolRenderers, Fallback: toolRenderers.fallback },
};

/** One clickable trace row: icon, verb, target chip — activating it opens
 * the call's full card beneath (spec "Trace row opens its card"). */
function TraceRow({
  call,
  collapsedPlan,
  shimmer,
  tinted,
  active,
  onToggle,
}: {
  call: RunCall;
  collapsedPlan: boolean;
  shimmer: boolean;
  tinted: boolean;
  active: boolean;
  onToggle: () => void;
}): ReactElement {
  if (collapsedPlan) {
    return (
      <p
        className="text-muted-foreground px-1 py-0.5 text-xs"
        data-testid="run-plan-updated"
      >
        plan updated
      </p>
    );
  }

  const chip = toolChip(call.toolName, call.argsText);

  return (
    <button
      type="button"
      className={cn(
        "text-muted-foreground hover:bg-foreground/[0.04] hover:text-foreground flex w-full items-center gap-2 rounded-md px-1 py-0.5 text-left text-[13.5px]",
        tinted && "text-dangertext"
      )}
      data-testid="run-row"
      aria-expanded={active}
      onClick={onToggle}
    >
      {createElement(toolIcon(call.toolName), {
        "aria-hidden": true,
        className: "text-muted-foreground size-3.5 shrink-0",
      })}
      <ShimmerLabel
        active={shimmer}
        className="relative inline-block leading-none"
      >
        {toolVerb(call.toolName)}
      </ShimmerLabel>
      <span className="bg-foreground/[0.06] text-foreground/70 rounded-md px-1.5 py-0.5 font-mono text-[11px]">
        {tinted ? `${chip} · failed` : chip}
      </span>
    </button>
  );
}

/** One segment's fold: the ToolTimeline summary plus the revealed trace
 * rows with on-demand cards and the trailing stats row. */
function FoldRun({
  calls,
  indices,
  streaming,
}: {
  calls: RunCall[];
  indices: readonly number[];
  streaming: boolean;
}): ReactElement {
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState<number | null>(null);

  const failed = useMemo(
    (): boolean => calls.some((call: RunCall): boolean => call.failed),
    [calls]
  );

  // A failed call expands the run once (also on a replay that mounts failed);
  // afterwards the reader's explicit close stays — the effect only re-runs
  // when the run's failure state itself flips.
  useEffect((): void => {
    if (failed) setOpen(true);
  }, [failed]);

  const stats = useMemo(
    (): TimelineStat[] =>
      changeStats(
        calls.map((call: RunCall) => ({
          toolName: call.toolName,
          argsText: call.argsText,
          completed: call.completed,
          failed: call.failed,
        }))
      ),
    [calls]
  );

  // Repeated plan writes collapse to a "plan updated" line except the last.
  const lastPlanIndex = useMemo((): number => {
    let last = -1;

    for (let i = 0; i < calls.length; i += 1) {
      if (calls[i].toolName === "todo_write") last = i;
    }

    return last;
  }, [calls]);

  const runningTool = useMemo((): string | null => {
    let name: string | null = null;

    for (const call of calls) {
      if (!call.completed) name = call.toolName;
    }

    return name;
  }, [calls]);

  // A call still open keeps this slice's run live even when the message as a
  // whole reads complete (a tool can be the turn's last entry, with no text
  // or reasoning part streaming beside it).
  const live = streaming || runningTool !== null;

  const stepCount = calls.length;
  const filesChanged = stats.length;

  const restingLabel =
    filesChanged > 0
      ? `${stepCount} ${stepCount === 1 ? "step" : "steps"} · ${filesChanged} ${
          filesChanged === 1 ? "file" : "files"
        } changed`
      : `${stepCount} ${stepCount === 1 ? "step" : "steps"}`;

  const steps: TimelineStep[] = calls.map(
    (call: RunCall, i: number): TimelineStep => ({
      id: call.toolCallId,
      verb: toolVerb(call.toolName),
      chip:
        call.toolName === "todo_write" && i !== lastPlanIndex
          ? "plan updated"
          : toolChip(call.toolName, call.argsText),
      icon: toolIcon(call.toolName),
    })
  );

  return (
    <div className="w-full" data-testid="run-summary">
      <div
        className={cn(
          failed && "[&_[data-slot=collapsible-trigger]]:text-dangertext"
        )}
        data-testid={failed ? "run-failed-summary" : undefined}
      >
        <ToolTimeline
          steps={steps}
          // The trace rows render below, where each row is a button that
          // opens its call's card; the element carries the summary trigger
          // and its resting/active label swap.
          visibleSteps={0}
          streaming={live}
          open={open}
          onOpenChange={setOpen}
          restingLabel={restingLabel}
          activeLabel={
            runningTool === null ? restingLabel : `running ${runningTool}`
          }
          stats={[]}
          className="w-full max-w-none"
        />
        {failed ? (
          <span className="sr-only" data-testid="run-failed">
            failed
          </span>
        ) : null}
      </div>
      {open ? (
        <div
          className="border-border/60 mt-2 flex flex-col gap-2 border-l ps-3"
          data-testid="run-rows"
        >
          {calls.map((call: RunCall, i: number): ReactElement => {
            const collapsedPlan =
              call.toolName === "todo_write" && i !== lastPlanIndex;

            return (
              <div key={call.toolCallId}>
                <TraceRow
                  call={call}
                  collapsedPlan={collapsedPlan}
                  shimmer={live && i === calls.length - 1 && !call.completed}
                  tinted={call.failed}
                  active={active === i}
                  onToggle={(): void => {
                    // Activating a row opens that call's full card (spec
                    // "Trace row opens its card") — the card's own collapse
                    // state seeds open so the arguments and result show;
                    // an explicit close afterwards still wins.
                    setCollapseOpen(call.toolCallId, true);
                    setActive((current: number | null): number | null =>
                      current === i ? null : i
                    );
                  }}
                />
                {active === i && !collapsedPlan ? (
                  <div className="mt-2 ps-3" data-testid="run-row-card">
                    <MessagePrimitive.PartByIndex
                      index={indices[i]}
                      components={ROW_COMPONENTS}
                    />
                  </div>
                ) : null}
              </div>
            );
          })}
          {stats.length > 0 ? (
            <div
              className="flex flex-wrap gap-1.5 pt-1"
              data-testid="run-stats"
            >
              {stats.map((stat: TimelineStat): ReactElement => (
                <span
                  key={stat.file}
                  className="bg-foreground/[0.06] text-foreground/70 inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 font-mono text-[11px]"
                >
                  <span>{stat.file}</span>
                  {stat.added !== undefined ? (
                    <span className="text-emerald-600 dark:text-emerald-400">
                      +{stat.added}
                    </span>
                  ) : null}
                  {stat.removed !== undefined ? (
                    <span className="text-red-600 dark:text-red-400">
                      −{stat.removed}
                    </span>
                  ) : null}
                </span>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

/** A segment of calls that renders unfolded: one full card per call. */
function PlainSegment({
  indices,
}: {
  indices: readonly number[];
}): ReactElement {
  return (
    <div className="flex flex-col gap-0.5">
      {indices.map((index: number): ReactElement => (
        <MessagePrimitive.PartByIndex
          key={index}
          index={index}
          components={ROW_COMPONENTS}
        />
      ))}
    </div>
  );
}

export function ChatToolGroup({
  group,
  children,
}: ChatToolGroupProps): ReactElement {
  const parts = useAuiState((s: AssistantState) => s.message.content);

  const calls = useMemo(
    (): RunCall[] => collectCalls(parts, group.indices),
    [group.indices, parts]
  );

  const firstApproval = useMemo(
    (): number => calls.findIndex((call: RunCall): boolean => call.approval),
    [calls]
  );

  // A run shorter than the fold threshold renders its calls inline, one line
  // each, in the transcript flow (design D2). The ghost group would have
  // hidden them behind a "N tool calls" disclosure whose content Radix
  // unmounts while closed, leaving the reader a bare link where the work
  // should be.
  if (firstApproval === -1 && calls.length < FOLD_MIN_STEPS) {
    return (
      // The run's margin wraps the whole block; the rows' own margins are
      // zeroed so the lines stay tight with each other (the reference's
      // list of lines) while the block keeps its air around it.
      <div
        className="my-2 flex flex-col gap-0.5 [&>*]:my-0"
        data-testid="run-inline"
      >
        {children}
      </div>
    );
  }

  if (calls.length === 0) {
    return <div className="flex flex-col gap-0.5">{children}</div>;
  }

  // An approval hard-splits the run at its position: a pending one stays
  // unfolded and actionable (spec "Pending approval stays visible"), a
  // resolved one keeps its receipt visible beside the folded calls.
  const segments: { start: number; end: number }[] = [];
  let cursor = 0;

  for (let i = 0; i < calls.length; i += 1) {
    if (calls[i].approval) {
      if (i > cursor) segments.push({ start: cursor, end: i - 1 });

      segments.push({ start: i, end: i });
      cursor = i + 1;
    }
  }

  if (cursor < calls.length) {
    segments.push({ start: cursor, end: calls.length - 1 });
  }

  const streaming = group.status.type === "running";

  return (
    <div className="my-2 flex flex-col gap-2" data-testid="run-split">
      {segments.map((segment: { start: number; end: number }): ReactElement => {
        const length = segment.end - segment.start + 1;
        const slice = calls.slice(segment.start, segment.end + 1);

        const sliceIndices = group.indices.slice(
          segment.start,
          segment.end + 1
        );

        if (length === 1 && slice[0].approval) {
          return <PlainSegment key={segment.start} indices={sliceIndices} />;
        }

        if (length < FOLD_MIN_STEPS) {
          return <PlainSegment key={segment.start} indices={sliceIndices} />;
        }

        return (
          <FoldRun
            key={segment.start}
            calls={slice}
            indices={sliceIndices}
            streaming={streaming}
          />
        );
      })}
    </div>
  );
}
