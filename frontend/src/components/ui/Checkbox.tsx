/**
 * Checkbox: the kit's checkbox (design D5). Same state discipline as the
 * Toggle — controlled `checked`, `onChange` reports the next state — but the
 * semantics ride a real `<input type="checkbox">`, visually hidden with
 * `sr-only`, so keyboard focus, Space toggling, and form semantics come from
 * the browser instead of a role="switch" shim. The styled 16px box mirrors
 * it: accent fill + the kit's `check` glyph when on, surface with the muted
 * border when off; the focus ring is `peer-focus-visible:` off the hidden
 * input (the global `:focus-visible` outline contract, relocated to the
 * visible box). All colors are token utilities, so both themes re-skin
 * without a re-render.
 */
import type { ChangeEvent, ReactElement, ReactNode } from "react";

import { cn } from "../../lib/cn";
import { Icon } from "./Icon";

export function Checkbox({
  checked,
  onChange,
  label,
  disabled = false,
  testId,
  id,
}: {
  /** Controlled state. */
  checked: boolean;
  /** Called with the next state. */
  onChange: (next: boolean) => void;
  /** Visible label; also the accessible name. */
  label: ReactNode;
  /** Blocks interaction while an update is in flight. */
  disabled?: boolean;
  /** Identifies the native input in tests. */
  testId?: string;
  id?: string;
}): ReactElement {
  return (
    <label
      className={cn(
        "inline-flex cursor-pointer items-center gap-2 text-sm text-fg2",
        disabled && "cursor-default opacity-50"
      )}
    >
      <input
        type="checkbox"
        id={id}
        data-testid={testId}
        checked={checked}
        disabled={disabled}
        onChange={(e: ChangeEvent<HTMLInputElement>) =>
          onChange(e.target.checked)
        }
        className="peer sr-only"
      />
      <span
        aria-hidden="true"
        className={cn(
          "flex h-4 w-4 shrink-0 items-center justify-center rounded border transition-colors",
          "peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-accent",
          checked
            ? "border-accent bg-accent text-accenton"
            : "border-line bg-surface text-transparent"
        )}
      >
        {checked && <Icon name="check" size={12} />}
      </span>
      <span>{label}</span>
    </label>
  );
}
