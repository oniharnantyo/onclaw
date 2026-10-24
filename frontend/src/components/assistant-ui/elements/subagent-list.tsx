"use client";

import type { ComponentProps } from "react";
import { Ban, CheckIcon, Loader2Icon, XIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { mono, ShimmerLabel } from "./surfaces";

export type SubagentState = "running" | "completed" | "failed" | "stopped";

export interface SubagentItem {
  name: string;
  state: SubagentState;
  /** One muted line under a running row: the child's latest activity (with
   * its step count); the row draws none when absent. */
  detail?: string | undefined;
  /** One muted line under a failed row: the first line of its result. */
  error?: string | undefined;
}

function StateIcon({ state }: { state: SubagentState }) {
  if (state === "completed") {
    return (
      <CheckIcon
        aria-hidden
        className="text-emerald-600 dark:text-emerald-400 size-3.5 stroke-[2.5]"
      />
    );
  }
  if (state === "failed") {
    return (
      <XIcon
        aria-hidden
        className="text-destructive size-3.5 stroke-[2.5]"
      />
    );
  }
  if (state === "stopped") {
    return (
      <Ban aria-hidden className="text-muted-foreground size-3.5" />
    );
  }
  return (
    <Loader2Icon
      aria-hidden
      className="text-foreground/90 size-3.5 animate-spin motion-reduce:animate-none"
    />
  );
}

export function SubagentList({
  agents,
  onActivate,
  showSummary,
  summaryAgent,
  className,
  ...props
}: Omit<
  ComponentProps<"div">,
  "children" | "agents" | "showSummary" | "summaryAgent" | "onActivate"
> & {
  agents: readonly SubagentItem[];
  /** When set, each row activates through it with its index in `agents`. */
  onActivate?: ((index: number) => void) | undefined;
  showSummary: boolean;
  summaryAgent: SubagentItem;
}) {
  return (
    <div
      data-slot="subagent-list"
      className={cn("flex w-full max-w-xs flex-col gap-2", className)}

      {...props}
    >
      {agents.map((agent, index) => {
        const terminal =
          agent.state === "failed" || agent.state === "stopped";
        const line =
          agent.state === "running"
            ? agent.detail
            : agent.state === "failed"
              ? agent.error
              : undefined;

        const row = (
          <>
            <div className="flex items-center gap-2">
              <span className="flex size-4 shrink-0 items-center justify-center">
                <StateIcon state={agent.state} />
              </span>
              <span className="min-w-0 flex-1 truncate text-[13px] font-medium">
                {agent.name}
              </span>
              {terminal ? (
                <span
                  className={cn(
                    mono,
                    "text-[11px] px-1.5 py-0.5 rounded font-medium",
                    agent.state === "failed"
                      ? "bg-destructive/10 text-destructive"
                      : "text-muted-foreground"
                  )}
                >
                  {agent.state}
                </span>
              ) : null}
            </div>
            {line ? (
              <span
                className="text-muted-foreground truncate pl-6 text-xs"
                title={line}
              >
                {line}
              </span>
            ) : null}
          </>
        );

        return (
          <div key={agent.name} className="flex flex-col gap-0.5">
            {onActivate ? (
              <button
                type="button"
                data-testid={`run-agent-row-${index}`}
                aria-label={agent.name}
                onClick={(): void => onActivate(index)}
                className="flex w-full cursor-pointer flex-col gap-0.5 -mx-1 rounded-md px-1 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]"
              >
                {row}
              </button>
            ) : (
              row
            )}
          </div>
        );
      })}
      {showSummary && (
        <div className="flex flex-col gap-0.5">
          <div className="flex items-center gap-2">
            <span className="flex size-4 shrink-0 items-center justify-center">
              <Loader2Icon className="text-foreground/90 size-3.5 animate-spin motion-reduce:animate-none" />
            </span>
            <ShimmerLabel className="min-w-0 flex-1 truncate text-[13px] font-medium">
              {summaryAgent.name}
            </ShimmerLabel>
          </div>
          {summaryAgent.detail ? (
            <span className="text-muted-foreground truncate pl-6 text-xs">
              {summaryAgent.detail}
            </span>
          ) : null}
        </div>
      )}
    </div>
  );
}
