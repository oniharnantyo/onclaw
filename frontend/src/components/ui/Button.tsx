/**
 * Button: the kit's action button (design D3). onclaw-v2 inline-styles buttons
 * at call sites; buddies composes those exact utility chains into this
 * component instead of repeating them per screen. All colors are token
 * utilities, so both themes re-skin without a re-render.
 *
 * Hallmark state discipline (8 states): rest / hover / active come from the
 * variant chain; focus-visible is the global `:focus-visible` outline
 * contract — this component deliberately adds NO second ring (the D9
 * double-ring fix); disabled and loading collapse into the `disabled`
 * attribute (native semantics keep receiving pointer events); the two themes
 * ride the tokens.
 */
import { forwardRef } from "react";
import type { ButtonHTMLAttributes, ForwardedRef, ReactElement } from "react";

import { cn } from "../../lib/cn";

export type ButtonVariant = "primary" | "ghost" | "destructive";

export type ButtonSize = "sm" | "md";

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant;
  size?: ButtonSize;
  /** Shows the inline spinner and disables the button while true. */
  loading?: boolean;
};

/** Fill + state chains lifted from onclaw-v2 call-site buttons. */
const VARIANT_CLASSES: Record<ButtonVariant, string> = {
  primary:
    "bg-accent text-accenton hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]",
  ghost:
    "border border-line bg-transparent text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg active:bg-[color-mix(in_oklab,var(--fg)_10%,transparent)]",
  destructive: "bg-danger text-accenton hover:opacity-90 active:opacity-80",
};

const VARIANT_WEIGHT: Record<ButtonVariant, string> = {
  primary: "font-semibold",
  ghost: "font-medium",
  destructive: "font-semibold",
};

const SIZE_CLASSES: Record<ButtonSize, string> = {
  sm: "h-8 gap-1.5 px-3 text-[12px]",
  md: "h-9 gap-2 px-3.5 text-[13px]",
};

/** onclaw-v2 spinner: track at 30% label color, head at full label color. */
const SPINNER_CLASSES: Record<ButtonVariant, string> = {
  primary: "border-accenton/30 border-t-accenton",
  ghost: "border-fg2/30 border-t-fg2",
  destructive: "border-accenton/30 border-t-accenton",
};

const SPINNER_SIZE: Record<ButtonSize, string> = {
  sm: "h-3.5 w-3.5",
  md: "h-4 w-4",
};

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  function Button(
    props: ButtonProps,
    ref: ForwardedRef<HTMLButtonElement>
  ): ReactElement {
    const {
      variant = "primary",
      size = "md",
      loading = false,
      disabled = false,
      type = "button",
      className,
      children,
      ...rest
    } = props;

    return (
      <button
        ref={ref}
        type={type}
        disabled={disabled || loading}
        aria-busy={loading || undefined}
        className={cn(
          "inline-flex items-center justify-center rounded-md transition-colors",
          "disabled:cursor-default disabled:opacity-50",
          VARIANT_CLASSES[variant],
          VARIANT_WEIGHT[variant],
          SIZE_CLASSES[size],
          className
        )}
        {...rest}
      >
        {loading && (
          <span
            aria-hidden="true"
            className={cn(
              "animate-spin rounded-full border-2",
              SPINNER_CLASSES[variant],
              SPINNER_SIZE[size]
            )}
          />
        )}
        {children}
      </button>
    );
  }
);
