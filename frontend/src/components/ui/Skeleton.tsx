/**
 * Skeleton: the kit's loading placeholder block (design D3). onclaw-v2's
 * idiom — `animate-pulse` over a hairline-bordered, 4%-fg block, sized per
 * call site — repeated by callers for lists. The global reduced-motion block
 * in styles.css collapses the pulse to opacity, so it degrades to a static
 * well.
 */
import type { ReactElement } from "react";

import { cn } from "../../lib/cn";

export type SkeletonProps = {
  /** Extra classes; size the block here (`h-16 w-full` default). */
  className?: string;
  testId?: string;
};

/** onclaw-v2 skeleton chain (GatewaysPane/ToolsPane loading rows). */
const BASE_CLASSES =
  "h-16 w-full animate-pulse rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]";

export function Skeleton({ className, testId }: SkeletonProps): ReactElement {
  return (
    <div
      aria-hidden="true"
      data-testid={testId}
      className={cn(BASE_CLASSES, className)}
    />
  );
}
