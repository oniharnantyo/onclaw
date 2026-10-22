import type { ReactElement } from "react";

import { cn } from "../lib/cn";
import type { IconButtonProps, PageHeaderProps } from "./types";

/**
 * Icon-only action button. Token ghost chain (the Button kit's ghost
 * variant, collapsed to a circle) — the daisyUI-era `btn-icon-glass` class
 * this replaces had no backing CSS. An accessible name is mandatory here:
 * these controls carry no visible text.
 *
 * State discipline: rest / hover / active from the chain; focus-visible is
 * the global outline contract (no second ring — D9); disabled collapses into
 * the native attribute; the two themes ride the tokens.
 */
export function IconButton({
  label,
  onClick,
  disabled,
  icon,
  size = "md",
  testId,
  className = "",
}: IconButtonProps): ReactElement {
  const box = size === "sm" ? "size-8" : "size-9";

  return (
    <button
      type="button"
      className={cn(
        "inline-flex items-center justify-center rounded-full border border-line bg-transparent p-0 text-fg2 transition-colors",
        "hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg",
        "active:bg-[color-mix(in_oklab,var(--fg)_10%,transparent)]",
        "disabled:cursor-default disabled:opacity-50",
        box,
        className
      )}
      data-testid={testId}
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
    >
      {icon}
    </button>
  );
}

/**
 * Menu/page title row with an optional trailing action. Used by the agents
 * grid and any future pane with a title + primary action.
 */
export function PageHeader({
  title,
  action,
  className = "",
}: PageHeaderProps): ReactElement {
  return (
    <div className={`flex items-center justify-between ${className}`}>
      <h1 className="font-display text-2xl font-bold tracking-tight">
        {title}
      </h1>
      {action}
    </div>
  );
}
