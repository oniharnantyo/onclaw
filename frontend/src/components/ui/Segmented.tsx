/**
 * Segmented: the kit's segmented control (onclaw-v2 parity, design D3) — a
 * radiogroup of equal-width options used by settings panes and the wizard.
 * The active option rides the raised-surface chain; colors are token
 * utilities so both themes re-skin without a re-render.
 */
import type { ReactElement } from "react";

import { cn } from "../../lib/cn";

export type SegmentedOption = {
  id: string;
  label: string;
  testid?: string;
};

export function Segmented({
  value,
  options,
  onChange,
}: {
  value: string;
  options: SegmentedOption[];
  onChange: (id: string) => void;
}): ReactElement {
  return (
    <div
      className="flex rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] p-0.5"
      role="radiogroup"
    >
      {options.map((o: SegmentedOption) => (
        <button
          key={o.id}
          type="button"
          role="radio"
          aria-checked={value === o.id}
          onClick={() => onChange(o.id)}
          data-testid={o.testid}
          className={cn(
            "flex-1 rounded-[9px] px-2 py-1.5 text-[12px] font-medium transition-colors",
            value === o.id
              ? "bg-surface text-fg shadow-[var(--elev-raised)]"
              : "text-muted hover:text-fg"
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
