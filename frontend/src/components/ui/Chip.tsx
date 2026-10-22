/**
 * Chip: small inline tag (onclaw-v2 parity, design D3) — bordered pill for
 * kinds, versions, and other short metadata. `mono` renders values like IDs
 * and version strings in the mono face; `className` merges caller overrides.
 */
import type { ReactElement, ReactNode } from "react";

import { cn } from "../../lib/cn";

export function Chip({
  children,
  className = "",
  mono = false,
  testId,
  "data-testid": dataTestId,
}: {
  children: ReactNode;
  className?: string;
  mono?: boolean;
  testId?: string;
  "data-testid"?: string;
}): ReactElement {
  return (
    <span
      data-testid={testId ?? dataTestId}
      className={cn(
        "inline-flex items-center gap-1 rounded-[6px] border border-line bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] px-1.5 py-0.5 text-[11px] leading-4 text-fg2",
        mono && "font-mono",
        className
      )}
    >
      {children}
    </span>
  );
}
