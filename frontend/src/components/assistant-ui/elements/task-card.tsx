"use client";

import { Children, type ComponentProps, type ReactNode, useState } from "react";
import {
  Ban,
  CheckIcon,
  ChevronRightIcon,
  Loader2Icon,
  XIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { mono, paper } from "./surfaces";

export type TaskCardState =
  | "working"
  | "waiting"
  | "done"
  | "failed"
  | "cancelled";

const isRenderable = (node: ReactNode) =>
  node !== undefined && node !== null && node !== false && node !== true;

export function TaskStateIcon({
  state,
  className,
}: {
  state: TaskCardState;
  className?: string;
}) {
  if (state === "done") {
    return (
      <span
        aria-hidden
        className={cn(
          "flex size-5 shrink-0 items-center justify-center rounded-md border border-emerald-500/25 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
          className
        )}
      >
        <CheckIcon aria-hidden className="size-3 stroke-[2.5]" />
      </span>
    );
  }
  if (state === "failed") {
    return (
      <span
        aria-hidden
        className={cn(
          "flex size-5 shrink-0 items-center justify-center rounded-md border border-destructive/25 bg-destructive/10 text-destructive",
          className
        )}
      >
        <XIcon aria-hidden className="size-3 stroke-[2.5]" />
      </span>
    );
  }
  if (state === "cancelled") {
    return (
      <span
        aria-hidden
        className={cn(
          "flex size-5 shrink-0 items-center justify-center rounded-md border border-border bg-muted/60 text-muted-foreground",
          className
        )}
      >
        <Ban aria-hidden className="size-3" />
      </span>
    );
  }
  if (state === "working") {
    return (
      <span
        aria-hidden
        className={cn(
          "flex size-5 shrink-0 items-center justify-center rounded-md border border-primary/20 bg-primary/10 text-primary",
          className
        )}
      >
        <Loader2Icon
          aria-hidden
          className="size-3 animate-spin motion-reduce:animate-none"
        />
      </span>
    );
  }
  return (
    <span
      aria-hidden
      className={cn(
        "flex size-5 shrink-0 items-center justify-center rounded-md border border-border/60 bg-muted/40 text-muted-foreground",
        className
      )}
    >
      <span className="size-1.5 rounded-full bg-muted-foreground/60" />
    </span>
  );
}

export function TaskCard({
  label,
  meta,
  state,
  elapsed,
  actions,
  result,
  open,
  onOpenChange,
  children,
  className,
  ...props
}: Omit<
  ComponentProps<"div">,
  "children" | "label" | "state" | "result" | "open" | "onOpenChange"
> & {
  label: string;
  meta?: string | undefined;
  state: TaskCardState;
  elapsed?: string | undefined;
  actions?: ReactNode | undefined;
  result?: ReactNode | undefined;
  open?: boolean | undefined;
  onOpenChange?: ((open: boolean) => void) | undefined;
  children?: ReactNode | undefined;
}) {
  const hasTranscript = Children.toArray(children).length > 0;
  const inert = open !== undefined && onOpenChange === undefined;
  const [uncontrolledOpen, setUncontrolledOpen] = useState(false);
  const isOpen = open ?? uncontrolledOpen;
  const toggle = () => {
    const next = !isOpen;
    if (open === undefined) setUncontrolledOpen(next);
    onOpenChange?.(next);
  };

  return (
    <div
      data-slot="task-card"
      data-state={state}
      className={cn(
        paper,
        "flex w-full max-w-sm flex-col overflow-hidden rounded-xl transition-colors shadow-xs",
        className
      )}
      {...props}
    >
      <button
        type="button"
        aria-expanded={hasTranscript ? isOpen : undefined}
        disabled={!hasTranscript || inert}
        onClick={toggle}
        className="hover:enabled:bg-foreground/[0.03] flex items-center gap-2.5 px-3 py-2 text-start transition-colors disabled:cursor-default"
      >
        <TaskStateIcon state={state} />
        <span className="sr-only">{state}</span>
        <span className="shrink-0 rounded px-1.5 py-0.5 font-mono text-[10px] font-medium uppercase tracking-wider bg-foreground/[0.05] text-muted-foreground">
          Subagent
        </span>
        <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-foreground">
          {label}
        </span>
        <span className="shrink-0 font-mono text-[11px]">
          {state === "done" ? (
            <span className="text-emerald-600 dark:text-emerald-400 font-medium">
              done
            </span>
          ) : state === "working" ? (
            <span className="text-primary font-medium animate-pulse">
              running
            </span>
          ) : state === "failed" ? (
            <span className="text-destructive font-medium">failed</span>
          ) : state === "cancelled" ? (
            <span className="text-muted-foreground">cancelled</span>
          ) : null}
        </span>
        {meta !== undefined && (
          <span
            className={cn(
              mono,
              "text-muted-foreground/75 max-w-24 shrink-0 truncate text-[11px]"
            )}
          >
            {meta}
          </span>
        )}
        {elapsed !== undefined && (
          <span
            className={cn(
              mono,
              "text-muted-foreground shrink-0 tabular-nums text-[11px]"
            )}
          >
            {elapsed}
          </span>
        )}
        {hasTranscript && (
          <ChevronRightIcon
            aria-hidden
            className={cn(
              "text-muted-foreground size-3 shrink-0 transition-transform duration-200 motion-reduce:transition-none",
              isOpen && "rotate-90"
            )}
          />
        )}
      </button>
      {isRenderable(actions) && (
        <div
          data-slot="task-card-actions"
          className="border-border/50 border-t px-3 py-2"
        >
          {actions}
        </div>
      )}
      {hasTranscript && isOpen && (
        <div
          data-slot="task-card-transcript"
          className="border-border/50 flex flex-col gap-2 border-t px-3 py-2"
        >
          {children}
        </div>
      )}
      {isRenderable(result) && (
        <div
          data-slot="task-card-result"
          className="border-border/50 text-foreground/80 border-t bg-foreground/[0.015] px-3 py-2 text-xs leading-relaxed"
        >
          {result}
        </div>
      )}
    </div>
  );
}
