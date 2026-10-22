/**
 * MicroLabel: 10px mono uppercase eyebrow label (onclaw-v2 parity, design
 * D3) — the small section header used inside forms and modal panes.
 */
import type { ReactElement, ReactNode } from "react";

export function MicroLabel({
  children,
}: {
  children: ReactNode;
}): ReactElement {
  return (
    <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-muted">
      {children}
    </p>
  );
}
