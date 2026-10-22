import type { ReactElement, ReactNode } from "react";

import { cn } from "../lib/cn";
import type { FormFieldProps } from "./types";

/**
 * Label + control + message. Settings forms repeat this shape on every
 * field, so the label/control pairing (onclaw-v2's `labelCls` chain) and the
 * inline error tone are defined once. The message is the per-field
 * validation slot — pair it with `<Input invalid>` so the error lands on the
 * offending control, not detached below the form (D9).
 */
export function FormField({
  id,
  label,
  children,
  message,
  invalid,
  messageTestId,
}: FormFieldProps): ReactElement {
  return (
    <div className="flex flex-col gap-1.5">
      <label className="text-[12px] font-medium text-fg2" htmlFor={id}>
        {label}
      </label>
      {children}
      {message && (
        <p
          className={cn("text-xs", invalid ? "text-dangertext" : "text-muted")}
          role={invalid ? "alert" : undefined}
          data-testid={messageTestId ?? `field-error-${id}`}
        >
          {message}
        </p>
      )}
    </div>
  );
}

export interface FormGridProps {
  /** Two-column on sm+, stacked below. */
  columns?: 1 | 2;
  children: ReactNode;
  className?: string;
  testId?: string;
}

/** Evenly spaced stack of `FormField`s, optionally in two columns. */
export function FormGrid({
  columns = 1,
  children,
  className = "",
  testId,
}: FormGridProps): ReactElement {
  const layout =
    columns === 2 ? "grid gap-3 sm:grid-cols-2" : "flex flex-col gap-3";

  return (
    <div className={`${layout} ${className}`} data-testid={testId}>
      {children}
    </div>
  );
}
