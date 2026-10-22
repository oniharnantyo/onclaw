/**
 * Select: the kit's native select (design D3). onclaw-v2 styles selects with
 * the same `inputCls` chain as inputs and keeps the NATIVE chrome (no
 * appearance-none, no custom chevron) — the picker follows the theme's
 * `color-scheme` (styles.css sets it per `data-theme`), which is the theming
 * spec's native-chrome parity requirement.
 *
 * State discipline mirrors Input: rest / hover / focus border-accent (global
 * outline suppressed locally per styles.css's input note), disabled native,
 * invalid danger border + aria-invalid. No readonly/loading concepts.
 */
import { forwardRef } from "react";
import type { ForwardedRef, ReactElement, SelectHTMLAttributes } from "react";

import { cn } from "../../lib/cn";

export type SelectProps = SelectHTMLAttributes<HTMLSelectElement> & {
  /** Validation failure: danger border + aria-invalid (message via FormField). */
  invalid?: boolean;
};

/** onclaw-v2 inputCls applied to <select> + hover/invalid states. */
const BASE_CLASSES =
  "h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] pl-3 pr-8 text-[14px] text-fg2 transition-colors hover:border-[color-mix(in_oklab,var(--fg)_25%,var(--border))] focus:border-accent focus:outline-none disabled:cursor-not-allowed disabled:opacity-50";

const INVALID_CLASSES = "border-danger hover:border-danger focus:border-danger";

export const Select = forwardRef<HTMLSelectElement, SelectProps>(
  function Select(
    props: SelectProps,
    ref: ForwardedRef<HTMLSelectElement>
  ): ReactElement {
    const { invalid = false, className, children, ...rest } = props;

    return (
      <select
        ref={ref}
        aria-invalid={invalid || undefined}
        className={cn(BASE_CLASSES, invalid && INVALID_CLASSES, className)}
        {...rest}
      >
        {children}
      </select>
    );
  }
);
