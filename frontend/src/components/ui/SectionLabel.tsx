/**
 * SectionLabel: sidebar/modal group header (onclaw-v2 parity, design D3) —
 * the mono uppercase label with an optional right-aligned action node.
 */
import type { ReactElement, ReactNode } from "react";

export function SectionLabel({
  children,
  action,
}: {
  children: ReactNode;
  action?: ReactNode;
}): ReactElement {
  return (
    <div className="mt-4 mb-1 flex items-center justify-between px-2.5">
      <span className="font-mono text-[10px] font-medium uppercase tracking-[0.14em] text-muted">
        {children}
      </span>
      {action}
    </div>
  );
}
