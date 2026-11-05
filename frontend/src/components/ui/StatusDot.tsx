/**
 * StatusDot: 8px status pill (onclaw-v2 parity, design D3). Colors ride the
 * ported tokens (`bg-success` / `bg-muted` / `bg-danger`) so both themes
 * re-skin without a re-render; `live` adds the `od-live` pulse primitive on
 * statuses that animate. The dot itself is decorative (`aria-hidden`); the
 * state rides a visually-hidden text label, and a live-flipping state
 * (running) gets `role="status"` so screen readers hear the change.
 */
import type { ReactElement } from "react";

import { cn } from "../../lib/cn";

export type StatusDotStatus = "running" | "idle" | "error" | "warn";

type StatusMeta = {
  dot: string;
  label: string;
  live?: boolean;
};

/** Status vocabulary (onclaw-v2 lib/constants STATUS, co-located for now). */
const STATUS: Record<StatusDotStatus, StatusMeta> = {
  running: { dot: "bg-success", label: "Running", live: true },
  idle: { dot: "bg-muted", label: "Idle" },
  error: { dot: "bg-danger", label: "Needs attention" },
  warn: { dot: "bg-warn", label: "Needs attention" },
};

export function StatusDot({
  status,
  live = false,
  className,
}: {
  status: StatusDotStatus;
  live?: boolean;
  className?: string;
}): ReactElement {
  const s: StatusMeta = STATUS[status] ?? STATUS.idle;

  return (
    <span
      className={cn("inline-flex shrink-0 items-center", className)}
      role={live && s.live ? "status" : undefined}
    >
      <span
        aria-hidden="true"
        className={cn(
          "inline-block h-2 w-2 rounded-full",
          s.dot,
          live && s.live && "od-live"
        )}
      />
      <span className="sr-only">{s.label}</span>
    </span>
  );
}
