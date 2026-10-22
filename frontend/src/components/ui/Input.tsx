/**
 * Input: the kit's text input (design D3). onclaw-v2 inline-styles inputs at
 * call sites with a shared `inputCls` chain; buddies composes that exact
 * chain into this component instead of repeating it per screen. All colors
 * are token utilities, so both themes re-skin without a re-render.
 *
 * State discipline: rest / hover / focus (border turns accent — the global
 * `:focus-visible` outline is deliberately suppressed here, matching
 * styles.css's "inputs opt out locally with outline-none" note, so fields
 * don't get a second focus treatment — D9); disabled collapses into the
 * native attribute; readonly rides the rest chain (a locked field is not a
 * different affordance); invalid turns the border danger and sets
 * aria-invalid — per-field validation rides this in the wizard (D9
 * detached-validation fix). Loading is not a meaningful state for a bare
 * input. The two themes ride the tokens.
 */
import { forwardRef } from "react";
import type { ForwardedRef, InputHTMLAttributes, ReactElement } from "react";

import { cn } from "../../lib/cn";

export type InputProps = InputHTMLAttributes<HTMLInputElement> & {
  /** Validation failure: danger border + aria-invalid (message via FormField). */
  invalid?: boolean;
};

/** onclaw-v2 `inputCls` (components/ui/constants.ts) + hover/invalid states. */
const BASE_CLASSES =
  "h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-3 text-[14px] text-fg2 transition-colors placeholder:text-muted hover:border-[color-mix(in_oklab,var(--fg)_25%,var(--border))] focus:border-accent focus:outline-none disabled:cursor-not-allowed disabled:opacity-50";

const INVALID_CLASSES = "border-danger hover:border-danger focus:border-danger";

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  props: InputProps,
  ref: ForwardedRef<HTMLInputElement>
): ReactElement {
  const { invalid = false, className, ...rest } = props;

  return (
    <input
      ref={ref}
      aria-invalid={invalid || undefined}
      className={cn(BASE_CLASSES, invalid && INVALID_CLASSES, className)}
      {...rest}
    />
  );
});
