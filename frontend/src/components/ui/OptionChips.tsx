/**
 * OptionChips: the kit's multi-select chip row (onclaw-v2 parity, design D3),
 * ported from `web/src/components/ui/OptionChips.tsx` with the `cx`→`cn` and
 * typed-props adaptation. Chips toggle membership in a `value` array; a chip
 * whose `disabledOf` returns true renders locked (lock icon, no toggle) with
 * the disabled tooltip — call sites decide WHY an option is off-limits.
 *
 * Hallmark state discipline: rest/hover on the off chip, the accent-tinted
 * surface on the on chip, disabled collapsed into the native attribute;
 * focus-visible rides the global outline contract. Colors are token
 * utilities, so both themes re-skin without a re-render.
 */
import type { ReactElement } from "react";

import { cn } from "../../lib/cn";
import { Icon } from "./Icon";
import type { IconName } from "./Icon";

export interface OptionChipOption {
  id: string;
  label: string;
  /** Tooltip body for an enabled chip (defaults to the label). */
  detail?: string;
}

export interface OptionChipsProps {
  options: OptionChipOption[];
  /** Currently selected option ids (order-independent). */
  value: string[];
  onChange: (next: string[]) => void;
  /** Leading icon per enabled chip; omit for iconless chips. */
  iconOf?: (option: OptionChipOption) => IconName | undefined;
  /** Marks a chip locked (lock icon + tooltip, no toggle). */
  disabledOf?: (option: OptionChipOption) => boolean;
  /** Names each chip `<prefix>-<id>` so tests can drive one chip directly. */
  testIdPrefix?: string;
  /** Tooltip on a locked chip; defaults to the tools-settings copy. */
  disabledTitle?: string;
}

export function OptionChips({
  options,
  value,
  onChange,
  iconOf,
  disabledOf,
  testIdPrefix,
  disabledTitle = "Disabled in Settings → Tools",
}: OptionChipsProps): ReactElement {
  return (
    <div className="flex flex-wrap gap-2">
      {options.map((o: OptionChipOption) => {
        const on = value.includes(o.id);
        const disabled = disabledOf ? disabledOf(o) : false;
        const iconName: IconName | undefined = disabled ? "lock" : iconOf?.(o);

        return (
          <button
            key={o.id}
            type="button"
            aria-pressed={on}
            disabled={disabled}
            data-testid={testIdPrefix ? `${testIdPrefix}-${o.id}` : undefined}
            title={disabled ? disabledTitle : o.detail || o.label}
            onClick={() => {
              if (disabled) return;
              onChange(
                on ? value.filter((x: string) => x !== o.id) : [...value, o.id]
              );
            }}
            className={cn(
              "flex h-8 items-center gap-1.5 rounded-md border px-3 text-[12px] font-medium transition-colors",
              disabled
                ? "cursor-not-allowed border-line bg-surface text-muted opacity-60"
                : on
                  ? "border-accent bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg"
                  : "border-line text-muted hover:border-[color-mix(in_oklab,var(--fg)_26%,transparent)] hover:text-fg2"
            )}
          >
            {iconName && <Icon name={iconName} size={13} />}
            {o.label}
          </button>
        );
      })}
    </div>
  );
}
