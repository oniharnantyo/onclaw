/**
 * Toggle: the kit's switch (onclaw-v2 parity, design D3) — ported verbatim
 * from onclaw-v2's components/ui/Toggle.tsx with `cn()` in place of its `cx`
 * helper and proper prop types. The track rides the accent token when on and
 * a 22% fg mix when off; the knob is bg-surface in both themes.
 *
 * State discipline: `role="switch"` + `aria-checked` carry the semantics,
 * `label` is the accessible name and native tooltip, `disabled` collapses
 * into the native attribute (panes hold it while an update is in flight),
 * and `testId` names the control — pane tests drive real clicks on it.
 */
import type { ReactElement } from "react";

import { cn } from "../../lib/cn";

export function Toggle({
  on,
  onChange,
  label,
  disabled = false,
  testId,
}: {
  /** Controlled state. */
  on: boolean;
  /** Called with the next state on click. */
  onChange: (next: boolean) => void;
  /** Accessible name and native tooltip text. */
  label: string;
  /** Blocks interaction while an update is in flight. */
  disabled?: boolean;
  /** Identifies the control in tests. */
  testId?: string;
}): ReactElement {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={label}
      title={label}
      data-testid={testId}
      disabled={disabled}
      onClick={() => onChange(!on)}
      className={cn(
        "relative h-5 w-9 shrink-0 rounded-full transition-colors duration-150",
        "disabled:cursor-default disabled:opacity-50",
        on ? "bg-accent" : "bg-[color-mix(in_oklab,var(--fg)_22%,transparent)]"
      )}
    >
      <span
        className={cn(
          "absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-surface transition-transform duration-150",
          on && "translate-x-4"
        )}
      />
    </button>
  );
}
