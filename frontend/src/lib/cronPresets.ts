/**
 * Pure preset ↔ five-field cron-expression mappers for the schedule
 * editor (task 7.1, design D11). TS keeps only these mappers — validity,
 * the human-readable description, and next runs all come from the Go
 * `Preview` service, so anything that is not an exact preset shape
 * decomposes to Advanced and this module never throws. composeCron
 * assumes the preset's numeric fields are in range and a weekly preset
 * has at least one day (the editor enforces that before Save; the
 * server's Preview reports anything else). Numbers compose unpadded:
 * `0 9 * * 1,5`.
 */

/** Cron day-of-week, 0 = Sunday … 6 = Saturday. */
export type Weekday = 0 | 1 | 2 | 3 | 4 | 5 | 6;

export type RecurrencePreset =
  | { kind: "minutely"; everyMinutes: number } // "*/N * * * *" (N === 1 → "* * * * *")
  | { kind: "hourly"; everyHours: number; minute: number } // "M */N * * *" (N === 1 → "M * * * *")
  | { kind: "daily"; hour: number; minute: number } // "M H * * *"
  | { kind: "weekly"; days: Weekday[]; hour: number; minute: number } // "M H * * d,d" (days sorted ascending, deduped)
  | { kind: "monthly"; dayOfMonth: number; hour: number; minute: number } // "M H D * *"
  | { kind: "advanced"; expr: string }; // anything else, verbatim (trimmed)

export type PresetKind = RecurrencePreset["kind"];

/** Display order for the editor's Segmented strip. */
export const PRESET_KINDS: readonly PresetKind[] = [
  "minutely",
  "hourly",
  "daily",
  "weekly",
  "monthly",
  "advanced",
];

export const WEEKDAY_LABELS: readonly string[] = [
  "Sun",
  "Mon",
  "Tue",
  "Wed",
  "Thu",
  "Fri",
  "Sat",
];

/** Sensible starting values when the user switches preset. */
export function defaultPreset(kind: PresetKind): RecurrencePreset {
  switch (kind) {
    case "minutely":
      return { kind: "minutely", everyMinutes: 5 };
    case "hourly":
      return { kind: "hourly", everyHours: 1, minute: 0 };
    case "daily":
      return { kind: "daily", hour: 9, minute: 0 };
    case "weekly":
      return { kind: "weekly", days: [1], hour: 9, minute: 0 };
    case "monthly":
      return { kind: "monthly", dayOfMonth: 1, hour: 9, minute: 0 };
    case "advanced":
      return { kind: "advanced", expr: "0 9 * * *" };
  }
}

/** Star-or-stepped field ("star-slash-N"): N === 1 collapses to a bare star. */
function stepField(n: number): string {
  return n === 1 ? "*" : `*/${n}`;
}

/** Preset → canonical five-field expression. */
export function composeCron(preset: RecurrencePreset): string {
  switch (preset.kind) {
    case "minutely":
      return `${stepField(preset.everyMinutes)} * * * *`;

    case "hourly":
      return `${preset.minute} ${stepField(preset.everyHours)} * * *`;

    case "daily":
      return `${preset.minute} ${preset.hour} * * *`;

    case "weekly": {
      const days = [...new Set(preset.days)]
        .sort((a: number, b: number) => a - b)
        .join(",");

      return `${preset.minute} ${preset.hour} * * ${days}`;
    }

    case "monthly":
      return `${preset.minute} ${preset.hour} ${preset.dayOfMonth} * *`;

    case "advanced":
      return preset.expr.trim();
  }
}

const INT = /^\d+$/; // digit-only, so `09` is accepted but `+5`/`1e2` are not

const STEP = /^\*\/(\d+)$/;

/** Bare star → 1, star-slash-N → N with N ≥ 1, anything else → null. */
function parseStep(field: string): number | null {
  if (field === "*") return 1;

  const match = STEP.exec(field);

  if (!match) return null;

  const n = Number(match[1]);

  return n >= 1 ? n : null;
}

/** Digit-only integer within [min, max]. */
function parseIntIn(field: string, min: number, max: number): number | null {
  if (!INT.test(field)) return null;

  const n = Number(field);

  return n >= min && n <= max ? n : null;
}

function isStar(...fields: string[]): boolean {
  return fields.every((f: string) => f === "*");
}

/**
 * Day-of-week field: a comma list of 0–6, or one simple range `a-b`
 * (a ≤ b, both 0–6) the way agents write it. A range expands to the
 * list, so the friendly `0 9 * * 1-5` opens Weekly with Mon–Fri
 * selected; recomposing emits the expanded list `1,2,3,4,5`, which the
 * Go describer treats as the same weekdays. Day names (`MON`), `7`,
 * steps (`1-5/2`), and mixed ranges+lists (`1-3,5`) are not presets.
 */
function parseDays(field: string): Weekday[] | null {
  if (field.includes("-")) {
    const range = /^(\d+)-(\d+)$/.exec(field);

    if (!range) return null;

    const from = Number(range[1]);
    const to = Number(range[2]);

    if (from > to || from < 0 || to > 6) return null;

    const days: Weekday[] = [];

    for (let d = from; d <= to; d++) {
      // SAFETY: the guard above pins from/to to 0–6, so d is always a Weekday.
      days.push(d as Weekday);
    }

    return days;
  }

  const parts = field.split(",");

  if (!parts.every((p: string) => INT.test(p))) return null;

  const days = parts.map(Number);

  if (days.some((d: number) => d < 0 || d > 6)) return null;

  // SAFETY: every element passed the 0–6 range check immediately above.
  return [...new Set(days)].sort((a: number, b: number) => a - b) as Weekday[];
}

/** Expression → preset when it matches one of the five shapes exactly; otherwise Advanced. */
export function decomposeCron(expr: string): RecurrencePreset {
  const trimmed = expr.trim();
  const fields = trimmed.split(/\s+/);

  if (fields.length !== 5) return { kind: "advanced", expr: trimmed };

  const [minute, hour, dom, month, dow] = fields;
  const minuteOfHour = parseIntIn(minute, 0, 59);
  const hourOfDay = parseIntIn(hour, 0, 23);

  // Minutely: bare star or star-slash-N minutes, stars everywhere else.
  const everyMinutes = parseStep(minute);

  if (everyMinutes !== null && isStar(hour, dom, month, dow)) {
    return { kind: "minutely", everyMinutes };
  }

  // Hourly: fixed minute-of-hour, star or stepped hours.
  const everyHours = parseStep(hour);

  if (minuteOfHour !== null && everyHours !== null && isStar(dom, month, dow)) {
    return { kind: "hourly", everyHours, minute: minuteOfHour };
  }

  // Daily: fixed minute and hour, stars everywhere else.
  if (minuteOfHour !== null && hourOfDay !== null && isStar(dom, month, dow)) {
    return { kind: "daily", hour: hourOfDay, minute: minuteOfHour };
  }

  // Weekly: fixed minute and hour, stars for dom/month, day list or range.
  if (minuteOfHour !== null && hourOfDay !== null && isStar(dom, month)) {
    const days = parseDays(dow);

    if (days !== null) {
      return { kind: "weekly", days, hour: hourOfDay, minute: minuteOfHour };
    }
  }

  // Monthly: fixed minute, hour, and day-of-month 1–31, stars elsewhere.
  if (minuteOfHour !== null && hourOfDay !== null && isStar(month, dow)) {
    const dayOfMonth = parseIntIn(dom, 1, 31);

    if (dayOfMonth !== null) {
      return {
        kind: "monthly",
        dayOfMonth,
        hour: hourOfDay,
        minute: minuteOfHour,
      };
    }
  }

  return { kind: "advanced", expr: trimmed };
}
