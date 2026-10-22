/**
 * Textarea: the kit's multi-line input (design D3). Same field grammar as
 * Input — onclaw-v2's bordered textarea chain, lifted from its config-payload
 * textareas, with `resize-y` and a body-font default (callers add
 * `font-mono` where the payload needs it). Token utilities throughout.
 *
 * State discipline mirrors Input: rest / hover / focus border-accent (global
 * outline suppressed locally per styles.css's input note), disabled native,
 * readonly rides the rest chain, invalid danger border + aria-invalid.
 */
import { forwardRef } from "react";
import type { ForwardedRef, ReactElement, TextareaHTMLAttributes } from "react";

import { cn } from "../../lib/cn";

export type TextareaProps = TextareaHTMLAttributes<HTMLTextAreaElement> & {
  /** Validation failure: danger border + aria-invalid (message via FormField). */
  invalid?: boolean;
};

/** onclaw-v2 textarea chain + hover/invalid states. */
const BASE_CLASSES =
  "min-h-20 w-full resize-y rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] p-3 text-[13px] leading-relaxed text-fg2 transition-colors placeholder:text-muted hover:border-[color-mix(in_oklab,var(--fg)_25%,var(--border))] focus:border-accent focus:outline-none disabled:cursor-not-allowed disabled:opacity-50";

const INVALID_CLASSES = "border-danger hover:border-danger focus:border-danger";

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(
  function Textarea(
    props: TextareaProps,
    ref: ForwardedRef<HTMLTextAreaElement>
  ): ReactElement {
    const { invalid = false, className, ...rest } = props;

    return (
      <textarea
        ref={ref}
        aria-invalid={invalid || undefined}
        className={cn(BASE_CLASSES, invalid && INVALID_CLASSES, className)}
        {...rest}
      />
    );
  }
);
