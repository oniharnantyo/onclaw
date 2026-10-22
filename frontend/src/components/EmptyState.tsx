import type { ReactElement, ReactNode } from "react";

import type { EmptyStateProps, LoadingHintProps } from "./types";

/** `EmptyStateProps` plus the optional muted-icon slot (onclaw-v2's
 * circular tint well). Declared here — with the component — so the icon
 * stays optional and existing call sites compile unchanged. */
export type EmptyStateWithIconProps = EmptyStateProps & {
  /** Muted mark above the title, e.g. a kit `<Icon name="wrench" />`. */
  icon?: ReactNode;
};

/**
 * Nothing-here state (onclaw-v2's empty idiom: muted icon well, title,
 * max-w muted description, optional next-step action). `boxed` is a whole
 * dashed surface (agents grid, chat with no agents); `inline` is a quiet
 * line inside a settings pane. Task 4.8 consumes this for Skills/Hooks/MCP.
 */
export function EmptyState({
  title,
  description,
  action,
  icon,
  variant = "boxed",
  testId,
}: EmptyStateWithIconProps): ReactElement {
  if (variant === "inline") {
    return (
      <p className="text-[13px] text-muted" data-testid={testId}>
        {title}
      </p>
    );
  }

  return (
    <div
      className="rounded-lg border border-dashed border-line px-6 py-12 text-center"
      data-testid={testId}
    >
      {icon && (
        <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
          {icon}
        </div>
      )}
      <p className="text-[14px] font-medium text-fg">{title}</p>
      {description && (
        <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
          {description}
        </p>
      )}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

/** Muted "Loading…" line with the kit spinner as the leading mark. */
export function LoadingHint({
  label = "Loading…",
  testId,
}: LoadingHintProps): ReactElement {
  return (
    <div
      className="flex items-center gap-2 text-[13px] text-muted"
      data-testid={testId}
    >
      <span
        aria-hidden="true"
        className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-muted/40 border-t-muted"
      />
      <span>{label}</span>
    </div>
  );
}
