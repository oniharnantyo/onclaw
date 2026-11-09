"use client";

/* BUDDIES RETARGET (add-computer-use task 6.2, design D8): vendored from the
 * assistant-ui registry item `elements-computer-use` (r.assistant-ui.com),
 * restyled to the desktop frame at vendor time — the browser-chrome URL pill
 * is replaced by a plain title showing the observed app's name (no URL bar;
 * the desktop drives windows, not pages), and the cursor trail/pointer are
 * re-based onto the accent tokens (status-palette contract, no blue-*).
 * Everything else is the registry's own markup: the traffic lights, the
 * backdrop slot (children), the fading cursor trail, and the current-action
 * line. Re-vendoring (`assistant-ui add -o elements-computer-use`)
 * regenerates the URL bar and MUST re-apply this retarget (VENDORED.md
 * divergence 10). */

import type { ComponentProps } from "react";
import { MousePointer2Icon } from "lucide-react";
import { cn } from "@/lib/utils";
import { mono, paper } from "./surfaces";
import { at, indexIn } from "../utils/range";

export interface ComputerStep {
  id: string;
  action: string;
  target: string;
  x: number;
  y: number;
}

export function ComputerUse({
  title,
  steps,
  activeIndex,
  children,
  className,
  ...props
}: Omit<
  ComponentProps<"div">,
  "title" | "steps" | "activeIndex" | "children"
> & {
  title: string;
  steps: readonly ComputerStep[];
  activeIndex: number;
  children: React.ReactNode;
}) {
  const index = indexIn(steps, activeIndex);
  const active = at(steps, index);
  const trail = steps.slice(Math.max(0, index - 2), index + 1);

  return (
    <div
      data-slot="computer-use"
      className={cn(
        paper,
        "flex w-full max-w-md flex-col overflow-hidden rounded-2xl",
        className,
      )}
      {...props}
    >
      <div className="flex items-center gap-2 px-3 py-2">
        <span className="flex shrink-0 gap-1">
          {["bg-red-500/50", "bg-amber-500/50", "bg-emerald-500/50"].map(
            (tint) => (
              <span
                key={tint}
                aria-hidden
                className={cn("size-2 rounded-full", tint)}
              />
            ),
          )}
        </span>
        <span className="text-foreground/80 min-w-0 flex-1 truncate text-[13px] font-medium">
          {title}
        </span>
      </div>

      <div className="border-foreground/[0.07] relative min-h-[8.5rem] overflow-hidden border-t">
        {children}

        {trail.map((step, i) => (
          <span
            key={step.id}
            aria-hidden
            className="pointer-events-none absolute size-2 rounded-full bg-accent transition-opacity duration-300"
            style={{
              left: `${step.x}%`,
              top: `${step.y}%`,
              opacity: 0.18 * (i + 1),
            }}
          />
        ))}

        {active && (
          <MousePointer2Icon
            aria-hidden
            className="pointer-events-none absolute size-4 fill-accent text-accent transition-[left,top] duration-500 ease-out motion-reduce:transition-none"
            style={{ left: `${active.x}%`, top: `${active.y}%` }}
          />
        )}
      </div>

      {active && (
        <div className="border-foreground/[0.07] flex items-center gap-2 border-t px-3.5 py-2">
          <span className={cn(mono, "text-muted-foreground shrink-0")}>
            {active.action}
          </span>
          <span className="text-foreground/80 min-w-0 flex-1 truncate text-[13px]">
            {active.target}
          </span>
          <span
            className={cn(mono, "text-muted-foreground shrink-0 tabular-nums")}
          >
            {index + 1}/{steps.length}
          </span>
        </div>
      )}
    </div>
  );
}
