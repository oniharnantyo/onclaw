/**
 * SideRow: the rail's navigation row (onclaw-v2 parity, design D3) — icon,
 * truncated label, optional sub/right nodes, active state tinted with the
 * accent at 15% (token `color-mix`, both themes). `title` falls back to the
 * label, exactly like onclaw-v2's `title || label`. State is semantic, not
 * just tinted: `active` emits `aria-current`, and an `expanded` value
 * (accordion headers) emits `aria-expanded`.
 */
import type { ReactElement, ReactNode } from "react";

import { cn } from "../../lib/cn";

export function SideRow({
  active = false,
  expanded,
  icon,
  label,
  sub,
  right,
  onClick,
  odId,
  testId,
  title,
  ariaCurrent,
}: {
  active?: boolean;
  /** Accordion headers pass the open state; plain rows omit it. */
  expanded?: boolean;
  icon?: ReactNode;
  label: string;
  sub?: ReactNode;
  right?: ReactNode;
  onClick?: () => void;
  odId?: string;
  /** Optional test hook; the attribute is absent when omitted. */
  testId?: string;
  title?: string;
  /** Emits `aria-current="page"` even on a non-`active` row. */
  ariaCurrent?: boolean;
}): ReactElement {
  return (
    <button
      type="button"
      onClick={onClick}
      data-od-id={odId}
      data-testid={testId}
      title={title || label}
      aria-current={ariaCurrent ? "page" : active || undefined}
      aria-expanded={expanded}
      className={cn(
        "flex h-[30px] w-full items-center gap-2 rounded-md px-2.5 text-left transition-colors",
        active
          ? "bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-fg"
          : "text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
      )}
    >
      {icon}
      <span className="min-w-0 flex-1 truncate text-[13px]">{label}</span>
      {sub}
      {right}
    </button>
  );
}
