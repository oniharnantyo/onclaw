/**
 * RecurrenceBuilder (task 7.2, design D11): the schedule editor's preset
 * strip and per-preset inputs. A controlled component over the canonical
 * five-field cron string — every edit recomposes with the pure mappers in
 * lib/cronPresets and emits through onChange; validity, the human-readable
 * description, and the next fires all come from the server's Preview call
 * the editor drives, so this component never validates beyond what the
 * mappers guarantee. The initial preset comes from decomposeCron(value), so
 * an edit of a preset-shaped expression reconstructs that preset and
 * anything else opens in Advanced showing the expression verbatim.
 */
import { useEffect, useState } from "react";
import type { ChangeEvent, ReactElement } from "react";

import {
  composeCron,
  decomposeCron,
  defaultPreset,
  PRESET_KINDS,
  WEEKDAY_LABELS,
  type PresetKind,
  type RecurrencePreset,
  type Weekday,
} from "../../../lib/cronPresets";
import { OptionChips } from "../../../components/ui/OptionChips";
import { Input } from "../../../components/ui/Input";
import { Segmented } from "../../../components/ui/Segmented";

export interface RecurrenceBuilderProps {
  /** The canonical cron expression this builder renders (controlled). */
  value: string;
  /** Emits the recomposed expression after every edit. */
  onChange: (cron: string) => void;
}

/** Display labels for the Segmented strip, in PRESET_KINDS order. */
const PRESET_LABELS: Record<PresetKind, string> = {
  minutely: "Minutely",
  hourly: "Hourly",
  daily: "Daily",
  weekly: "Weekly",
  monthly: "Monthly",
  advanced: "Advanced",
};

/** Two-digit field value for `<input type="time">` (it requires padding). */
function pad2(n: number): string {
  return String(n).padStart(2, "0");
}

/** The time input's value for an hour/minute pair. */
function timeValue(hour: number, minute: number): string {
  return `${pad2(hour)}:${pad2(minute)}`;
}

/** Parses "HH:MM" from a time input into an hour/minute pair, or null. */
function parseTimeValue(
  value: string
): { hour: number; minute: number } | null {
  const match = /^(\d{2}):(\d{2})$/.exec(value);

  if (!match) return null;

  const hour = Number(match[1]);
  const minute = Number(match[2]);

  if (hour > 23 || minute > 59) return null;

  return { hour, minute };
}

/** Parses a digit string, or null when the field is transiently empty. */
function parseIntValue(value: string): number | null {
  if (!/^\d+$/.test(value)) return null;

  return Number(value);
}

export function RecurrenceBuilder({
  value,
  onChange,
}: RecurrenceBuilderProps): ReactElement {
  const [preset, setPreset] = useState<RecurrencePreset>(() =>
    decomposeCron(value)
  );

  // Follows value changes the builder did not produce itself (a parent
  // reset): recompose only when the incoming expression diverges from what
  // the current preset would emit, so user typing is never clobbered.
  useEffect((): void => {
    setPreset((prev: RecurrencePreset): RecurrencePreset =>
      value === composeCron(prev) ? prev : decomposeCron(value)
    );
  }, [value]);

  /** Applies a new preset and emits its canonical expression. */
  const apply = (next: RecurrencePreset): void => {
    setPreset(next);
    onChange(composeCron(next));
  };

  /** Switching presets seeds that preset's defaults (design D11). */
  const switchPreset = (raw: string): void => {
    const kind = PRESET_KINDS.find((k: PresetKind): boolean => k === raw);

    if (kind) apply(defaultPreset(kind));
  };

  const updateNumeric = (
    patch: (current: RecurrencePreset) => RecurrencePreset
  ): void => {
    apply(patch(preset));
  };

  /** A time input edit lands on whichever preset carries hour/minute. */
  const handleTimeChange = (e: ChangeEvent<HTMLInputElement>): void => {
    const parsed = parseTimeValue(e.target.value);

    if (!parsed) return;

    if (preset.kind === "daily") {
      apply({ kind: "daily", hour: parsed.hour, minute: parsed.minute });
    } else if (preset.kind === "weekly") {
      apply({ ...preset, hour: parsed.hour, minute: parsed.minute });
    } else if (preset.kind === "monthly") {
      apply({ ...preset, hour: parsed.hour, minute: parsed.minute });
    }
  };

  const timePresetHour =
    preset.kind === "daily"
      ? preset.hour
      : preset.kind === "weekly"
        ? preset.hour
        : preset.kind === "monthly"
          ? preset.hour
          : 0;

  const timePresetMinute =
    preset.kind === "daily"
      ? preset.minute
      : preset.kind === "weekly"
        ? preset.minute
        : preset.kind === "monthly"
          ? preset.minute
          : 0;

  return (
    <div className="flex flex-col gap-3" data-testid="recurrence-builder">
      <Segmented
        value={preset.kind}
        options={PRESET_KINDS.map((kind: PresetKind) => ({
          id: kind,
          label: PRESET_LABELS[kind],
          testid: `recurrence-preset-${kind}`,
        }))}
        onChange={switchPreset}
      />

      {preset.kind === "minutely" && (
        <label className="flex items-center gap-2 text-[12px] font-medium text-fg2">
          Every
          <Input
            type="number"
            min={1}
            max={59}
            data-testid="recurrence-minutely-every"
            className="w-20"
            value={String(preset.everyMinutes)}
            onChange={(e: ChangeEvent<HTMLInputElement>) => {
              const n = parseIntValue(e.target.value);

              if (n !== null && n >= 1 && n <= 59)
                updateNumeric(() => ({
                  kind: "minutely",
                  everyMinutes: n,
                }));
            }}
          />
          minutes
        </label>
      )}

      {preset.kind === "hourly" && (
        <div className="flex items-center gap-2 text-[12px] font-medium text-fg2">
          Every
          <Input
            type="number"
            min={1}
            max={23}
            data-testid="recurrence-hourly-every"
            className="w-20"
            value={String(preset.everyHours)}
            onChange={(e: ChangeEvent<HTMLInputElement>) => {
              const n = parseIntValue(e.target.value);

              if (n !== null && n >= 1 && n <= 23)
                updateNumeric((current: RecurrencePreset) =>
                  current.kind === "hourly"
                    ? { kind: "hourly", everyHours: n, minute: current.minute }
                    : current
                );
            }}
          />
          hours at minute
          <Input
            type="number"
            min={0}
            max={59}
            data-testid="recurrence-hourly-minute"
            className="w-20"
            value={String(preset.minute)}
            onChange={(e: ChangeEvent<HTMLInputElement>) => {
              const n = parseIntValue(e.target.value);

              if (n !== null && n >= 0 && n <= 59)
                updateNumeric((current: RecurrencePreset) =>
                  current.kind === "hourly"
                    ? {
                        kind: "hourly",
                        everyHours: current.everyHours,
                        minute: n,
                      }
                    : current
                );
            }}
          />
        </div>
      )}

      {(preset.kind === "daily" ||
        preset.kind === "weekly" ||
        preset.kind === "monthly") && (
        <label className="flex items-center gap-2 text-[12px] font-medium text-fg2">
          {preset.kind === "daily" ? "At" : "at"}
          <input
            type="time"
            data-testid="recurrence-time"
            value={timeValue(timePresetHour, timePresetMinute)}
            onChange={handleTimeChange}
            className="h-9 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2 text-[14px] text-fg2 focus:border-accent focus:outline-none"
          />
        </label>
      )}

      {preset.kind === "weekly" && (
        <div className="flex flex-col gap-1.5">
          <span className="text-[12px] font-medium text-fg2">
            On days (at least one)
          </span>
          <OptionChips
            testIdPrefix="recurrence-weekday"
            options={WEEKDAY_LABELS.map((label: string, index: number) => ({
              id: String(index),
              label,
            }))}
            value={preset.days.map((d: Weekday): string => String(d))}
            onChange={(next: string[]): void =>
              apply({
                kind: "weekly",
                // SAFETY: the chip ids are the weekday indexes 0–6 this
                // component minted above.
                days: next.map(Number) as Weekday[],
                hour: preset.hour,
                minute: preset.minute,
              })
            }
          />
        </div>
      )}

      {preset.kind === "monthly" && (
        <label className="flex items-center gap-2 text-[12px] font-medium text-fg2">
          On day of month
          <Input
            type="number"
            min={1}
            max={31}
            data-testid="recurrence-monthly-day"
            className="w-20"
            value={String(preset.dayOfMonth)}
            onChange={(e: ChangeEvent<HTMLInputElement>) => {
              const n = parseIntValue(e.target.value);

              if (n !== null && n >= 1 && n <= 31)
                updateNumeric((current: RecurrencePreset) =>
                  current.kind === "monthly"
                    ? {
                        kind: "monthly",
                        dayOfMonth: n,
                        hour: current.hour,
                        minute: current.minute,
                      }
                    : current
                );
            }}
          />
        </label>
      )}

      {preset.kind === "advanced" && (
        <div className="flex flex-col gap-1.5">
          <span className="text-[12px] font-medium text-fg2">
            Cron expression (five fields, local time)
          </span>
          <Input
            className="font-mono"
            data-testid="recurrence-advanced-input"
            value={preset.expr}
            placeholder="e.g. */15 9-17 * * 1-5"
            onChange={(e: ChangeEvent<HTMLInputElement>) =>
              apply({ kind: "advanced", expr: e.target.value })
            }
          />
        </div>
      )}
    </div>
  );
}
