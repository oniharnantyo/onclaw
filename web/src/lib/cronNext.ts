// Client-side 5-field cron math (change integrate-scheduler, design D2): a
// pure Vixie-semantics parser and next-runs computation powering the schedule
// editor's recurrence builder, custom-mode validation, and next-runs preview.
//
// The server (robfig/cron/v3, standard parser) is authoritative — the browser
// only mirrors its behavior for previews: standard 5-field subset (minute,
// hour, day-of-month, month, day-of-week), `*`, lists, ranges, steps, 3-letter
// month/day names, DOW 0–7 with 7≡0 (Sunday), and the Vixie DOM/DOW OR rule
// (when both fields are restricted, a day matching EITHER fires).
//
// All computation happens on wall-clock fields in a given IANA timezone (the
// workspace's), converted to explicit instants through Intl offsets — so
// previews stay wall-clock-correct across DST boundaries instead of drifting
// by a fixed UTC offset.

export const DOW_LABELS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'] as const;
export const DOW_ORDER = [1, 2, 3, 4, 5, 6, 0] as const; // Mon-first, chip order

const MONTH_NAMES = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'];
const DOW_NAMES = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'];

interface FieldSpec {
  values: Set<number>;
  /** Vixie "restricted": the field is not a bare star (or a full span like a star with step 1). */
  restricted: boolean;
}

export interface ParsedCron {
  minute: FieldSpec;
  hour: FieldSpec;
  dom: FieldSpec;
  month: FieldSpec;
  dow: FieldSpec;
}

interface Bounds {
  min: number;
  max: number;
  names?: string[];
  /** Normalize an accepted value (dow 7 ≡ 0). */
  wrap?: (v: number) => number;
}

const FIELDS: Array<Bounds> = [
  { min: 0, max: 59 }, // minute
  { min: 0, max: 23 }, // hour
  { min: 1, max: 31 }, // dom
  { min: 1, max: 12, names: MONTH_NAMES }, // month
  { min: 0, max: 6, names: DOW_NAMES, wrap: (v) => v % 7 }, // dow
];

function parseTerm(term: string, b: Bounds, out: Set<number>): boolean {
  let rest = term.toLowerCase();
  let step = 1;
  const slash = rest.indexOf('/');
  if (slash !== -1) {
    const stepPart = rest.slice(slash + 1);
    if (!/^\d+$/.test(stepPart) || Number(stepPart) === 0) return false;
    step = Number(stepPart);
    rest = rest.slice(0, slash);
  }
  let lo: number;
  let hi: number;
  if (rest === '*') {
    if (slash === -1) {
      // A bare `*` spans the field but stays "unrestricted" for Vixie DOM/DOW.
      for (let v = b.min; v <= b.max; v++) out.add(b.wrap ? b.wrap(v) : v);
      return true;
    }
    lo = b.min;
    hi = b.max;
  } else if (rest.includes('-')) {
    const [a, z] = rest.split('-');
    const pa = b.names ? b.names.indexOf(a) : -1;
    const pz = b.names ? b.names.indexOf(z) : -1;
    lo = /^\d+$/.test(a) ? Number(a) : pa;
    hi = /^\d+$/.test(z) ? Number(z) : pz;
    if (lo === -1 || hi === -1 || isNaN(lo) || isNaN(hi)) return false;
    // Dow accepts 0-7 on input (7 wraps to Sunday), so ranges may end at 7.
    if (lo < b.min || hi > b.max + (b.wrap ? 1 : 0) || lo > hi) return false;
  } else {
    const p = b.names ? b.names.indexOf(rest) : -1;
    // Names index from zero; values start at the field's minimum (months 1-12).
    // A digit stays as-is; the range check below rejects out-of-field values.
    lo = /^\d+$/.test(rest) ? Number(rest) : p + b.min;
    if (p === -1 && !/^\d+$/.test(rest)) return false;
    if (isNaN(lo)) return false;
    // Dow accepts 0-7 on input; 7 wraps to Sunday. Anything past 7 is junk.
    if (b.wrap && lo === b.max + 1) lo = b.min;
    if (lo < b.min || lo > b.max) return false;
    if (slash === -1) {
      out.add(lo);
      return true;
    }
    // `a/step` — Vixie reads it as a-max/step.
    hi = b.max;
  }
  for (let v = lo; v <= hi; v += step) out.add(b.wrap ? b.wrap(v) : v);
  return true;
}

function parseField(raw: string, b: Bounds): FieldSpec | null {
  if (!raw) return null;
  const values = new Set<number>();
  let restricted = raw.trim() !== '*';
  for (const term of raw.split(',')) {
    if (!parseTerm(term.trim(), b, values)) return null;
  }
  // `*/1` (or an equivalent full span) selects every value — the field may as
  // well be `*`, so it does not restrict the Vixie DOM/DOW decision.
  if (values.size === b.max - b.min + 1) restricted = false;
  return { values, restricted };
}

/** Parses a standard 5-field expression; returns null when it is not a valid
 * Vixie expression (the editor's custom-mode inline error). */
export function parseCron(expr: string): ParsedCron | null {
  const parts = (expr || '').trim().split(/\s+/);
  if (parts.length !== 5) return null;
  const specs: Array<FieldSpec | null> = parts.map((p, i) => parseField(p, FIELDS[i]));
  if (specs.some((s) => s === null)) return null;
  return {
    minute: specs[0] as FieldSpec,
    hour: specs[1] as FieldSpec,
    dom: specs[2] as FieldSpec,
    month: specs[3] as FieldSpec,
    dow: specs[4] as FieldSpec,
  };
}

const sorted = (s: FieldSpec): number[] => Array.from(s.values).sort((a, b) => a - b);

/** Vixie DOM/DOW OR: unrestricted fields are ignored; two restricted fields
 * OR together; two unrestricted fields mean every day. */
function dayMatches(p: ParsedCron, dom: number, dow: number): boolean {
  const domR = p.dom.restricted;
  const dowR = p.dow.restricted;
  if (!domR && !dowR) return true;
  const domOk = domR && p.dom.values.has(dom);
  const dowOk = dowR && p.dow.values.has(dow);
  return domR && dowR ? domOk || dowOk : Boolean(domOk || dowOk);
}

function daysInMonth(year: number, month: number): number {
  return new Date(Date.UTC(year, month, 0)).getUTCDate();
}

/** Day-of-week (0=Sun) for a calendar date, computed purely from the calendar. */
function dowOf(year: number, month: number, day: number): number {
  return new Date(Date.UTC(year, month - 1, day)).getUTCDay();
}

export interface WallTime {
  year: number;
  month: number; // 1-12
  day: number;
  hour: number;
  minute: number;
}

/** Next matching wall-clock time strictly after `after`, or null when none
 * exists within four years (e.g. Feb 30th). */
export function nextWallTime(p: ParsedCron, after: WallTime): WallTime | null {
  const minutes = sorted(p.minute);
  const hours = sorted(p.hour);
  const months = sorted(p.month);
  const start = { ...after };

  const LIMIT_YEAR = start.year + 4;
  let year = start.year;
  let monthIdx = months.findIndex((m) => m >= start.month);
  for (;;) {
    // -1 on entry means "nothing left this year — advance to next year";
    // the same normalization handles running past the last matched month.
    if (monthIdx < 0 || monthIdx >= months.length) {
      year += 1;
      if (year > LIMIT_YEAR) return null;
      monthIdx = 0;
    }
    const month = months[monthIdx];
    const sameMonth = year === start.year && month === start.month;
    const total = daysInMonth(year, month);
    for (let day = sameMonth ? start.day : 1; day <= total; day++) {
      if (!dayMatches(p, day, dowOf(year, month, day))) continue;
      const sameDay = sameMonth && day === start.day;
      for (const hour of hours) {
        if (sameDay && hour < start.hour) continue;
        for (const minute of minutes) {
          if (sameDay && hour === start.hour && minute <= start.minute) continue;
          return { year, month, day, hour, minute };
        }
      }
    }
    monthIdx += 1;
  }
  return null;
}

// ---------------------------------------------------------------------------
// Timezone conversion (wall clock ⇄ instant) via Intl offsets
// ---------------------------------------------------------------------------

interface TzParts {
  year: number;
  month: number;
  day: number;
  hour: number;
  minute: number;
}

function tzParts(instant: Date, tz: string): TzParts {
  const dtf = new Intl.DateTimeFormat('en-US', {
    timeZone: tz,
    hour12: false,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
  const parts: Record<string, number> = {};
  for (const p of dtf.formatToParts(instant)) {
    if (p.type !== 'literal') parts[p.type] = Number(p.value);
  }
  return {
    year: parts.year,
    month: parts.month,
    day: parts.day,
    // Some ICU builds render midnight as hour 24 under h12:false.
    hour: parts.hour % 24,
    minute: parts.minute,
  };
}

function offsetMs(instant: Date, tz: string): number {
  const p = tzParts(instant, tz);
  const asUtc = Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute);
  return asUtc - instant.getTime();
}

/** Converts wall-clock fields in `tz` to the explicit UTC instant. Transition
 * handling mirrors Go's time.Date normalization (what robfig/cron/v3 sits on):
 * ambiguous fold times resolve to the EARLIER occurrence; nonexistent gap
 * times normalize FORWARD (02:30 on a spring-forward night fires 03:30). */
export function wallTimeToInstant(w: WallTime, tz: string): Date {
  const asUtc = Date.UTC(w.year, w.month - 1, w.day, w.hour, w.minute);
  const same = (ts: number): boolean => {
    const rb = instantToWallTime(new Date(ts), tz);
    return rb.year === w.year && rb.month === w.month && rb.day === w.day &&
      rb.hour === w.hour && rb.minute === w.minute;
  };
  // Start from the offset a day earlier — the pre-transition side, which is
  // the correct mapping for every normal time and the earlier occurrence of
  // a fold.
  const ts1 = asUtc - offsetMs(new Date(asUtc - 86400000), tz);
  const ts2 = asUtc - offsetMs(new Date(asUtc + 86400000), tz);
  const ok1 = same(ts1);
  const ok2 = same(ts2);
  // Both map exactly: the wall time is ambiguous (fold) — earlier wins.
  if (ok1 && ok2) return new Date(Math.min(ts1, ts2));
  // One mapping: that's the answer (the other probe read a different time).
  if (ok2) return new Date(ts2);
  // Neither maps exactly: the wall time fell into a gap — the pre-transition
  // probe is the forward-normalized instant Go produces.
  return new Date(ts1);
}

/** The wall-clock fields `instant` reads as in `tz`. */
export function instantToWallTime(instant: Date, tz: string): WallTime {
  const p = tzParts(instant, tz);
  return { year: p.year, month: p.month, day: p.day, hour: p.hour, minute: p.minute };
}

/** Next n fires strictly after `after` (explicit instant), evaluated on the
 * wall clock of `tz`. Fewer than n entries when the expression never fires
 * again (invalid or impossible dates). */
export function cronNextRuns(expr: string, after: Date, tz: string, n = 3): Date[] {
  const p = parseCron(expr);
  if (!p) return [];
  const out: Date[] = [];
  let cursor = instantToWallTime(after, tz);
  for (let i = 0; i < n; i++) {
    const next = nextWallTime(p, cursor);
    if (!next) break;
    out.push(wallTimeToInstant(next, tz));
    cursor = next;
  }
  return out;
}

/** Single-fire convenience over cronNextRuns. */
export function cronNext(expr: string, after: Date, tz: string): Date | null {
  return cronNextRuns(expr, after, tz, 1)[0] ?? null;
}

// ---------------------------------------------------------------------------
// Human label (mirrors the server's derivation, design D2)
// ---------------------------------------------------------------------------

const pad2 = (v: number) => String(v).padStart(2, '0');
export const formatWallClock = (h: number, m: number): string => `${pad2(h)}:${pad2(m)}`;

/** Collapses a sorted day list into compact ranges over the dow numbers:
 * [1,2,3,4,5] → "Mon–Fri"; [1,3,5] → "Mon, Wed, Fri"; [0] → "Sun". */
export function describeDays(days: number[]): string {
  const sorted0 = Array.from(new Set(days)).sort((a, b) => a - b);
  if (sorted0.length === 0) return '';
  const parts: string[] = [];
  let start = 0;
  for (let i = 1; i <= sorted0.length; i++) {
    const broke = i === sorted0.length || sorted0[i] !== sorted0[i - 1] + 1;
    if (broke) {
      const first = DOW_LABELS[sorted0[start] % 7];
      const last = DOW_LABELS[sorted0[i - 1] % 7];
      if (i - start === 1) parts.push(first);
      else parts.push(first + '–' + last);
      start = i;
    }
  }
  return parts.join(', ');
}

/** The human-readable recurrence label mirroring the server ("09:00 ·
 * Mon–Fri"). Returns the raw expression for custom recurrences it cannot
 * phrase — the same rule the server applies. */
export function describeCron(expr: string): string {
  const p = parseCron(expr);
  if (!p) return expr;
  const minutes = sorted(p.minute);
  const hours = sorted(p.hour);
  const minuteRaw = expr.trim().split(/\s+/)[0];
  const minuteStep = /^.\/(\d+)$/.exec(minuteRaw);

  // Every minute: the plain all-stars expression.
  if (!p.minute.restricted && !p.hour.restricted && !p.dom.restricted &&
      !p.month.restricted && !p.dow.restricted) {
    return 'Every minute';
  }

  // Every N minutes: a stepped minute field with hours left open.
  if (!p.hour.restricted && minuteStep) {
    const n = Number(minuteStep[1]);
    return n === 1 ? 'Every minute' : `Every ${n} minutes`;
  }

  // Hourly at a fixed minute: "30 * * * *".
  if (!p.hour.restricted && minutes.length === 1) {
    return `Hourly · :${pad2(minutes[0])}`;
  }

  // Single wall-clock time (one minute, one hour).
  if (minutes.length === 1 && hours.length === 1) {
    const time = formatWallClock(hours[0], minutes[0]);
    const days = sorted(p.dow);
    const doms = sorted(p.dom);
    if (p.dow.restricted) {
      if (days.length === 7) return `${time} · Daily`;
      return `${time} · ${describeDays(days)}`;
    }
    if (p.dom.values.size === 1) {
      return `${time} · Monthly on the ${ordinal(doms[0])}`;
    }
    if (!p.dom.restricted) {
      return `${time} · Daily`;
    }
  }

  return expr;
}

function ordinal(n: number): string {
  const s = ['th', 'st', 'nd', 'rd'];
  const v = n % 100;
  return n + (s[(v - 20) % 10] || s[v] || s[0]);
}

// ---------------------------------------------------------------------------
// Preset builder (editor recurrence builder → canonical 5-field expression)
// ---------------------------------------------------------------------------

export type RecurrenceFreq = 'hourly' | 'daily' | 'weekly' | 'monthly';

export interface PresetState {
  freq: RecurrenceFreq;
  /** "HH:MM" 24h wall-clock time from the editor's time picker. */
  time: string;
  /** Selected day-of-week chips, 0=Sun…6=Sat (weekly only). */
  days: number[];
  /** Day of month, 1–28 (monthly only). */
  dayOfMonth: number;
}

/** Parses "HH:MM" (or "H:MM") into [hour, minute], or null. */
export function parseTimeInput(time: string): [number, number] | null {
  const m = /^(\d{1,2}):(\d{2})$/.exec((time || '').trim());
  if (!m) return null;
  const h = Number(m[1]);
  const min = Number(m[2]);
  if (h > 23 || min > 59) return null;
  return [h, min];
}

/** Emits a comma list with ≥3-runs collapsed to a dash range (1-5). */
function emitDowList(days: number[]): string {
  const sorted0 = Array.from(new Set(days)).sort((a, b) => a - b);
  const parts: string[] = [];
  let start = 0;
  for (let i = 1; i <= sorted0.length; i++) {
    const broke = i === sorted0.length || sorted0[i] !== sorted0[i - 1] + 1;
    if (broke) {
      if (i - start === 1) parts.push(String(sorted0[start]));
      else parts.push(`${sorted0[start]}-${sorted0[i - 1]}`);
      start = i;
    }
  }
  return parts.join(',');
}

/** Generates the canonical 5-field expression from the preset builder state.
 * Returns null when the time input is not a valid wall-clock time. */
export function buildPresetExpr(state: PresetState): string | null {
  const t = parseTimeInput(state.time);
  if (!t) return null;
  const [h, m] = t;
  switch (state.freq) {
    case 'hourly':
      return `${m} * * * *`;
    case 'daily':
      // Day chips narrow the daily preset (spec: daily + Mon–Fri chips →
      // "09:00 · Mon–Fri"); every day or no selection stays plain daily.
      if (state.days.length > 0 && state.days.length < 7) {
        return `${m} ${h} * * ${emitDowList(state.days)}`;
      }
      return `${m} ${h} * * *`;
    case 'weekly': {
      if (state.days.length === 0) return null;
      return `${m} ${h} * * ${emitDowList(state.days)}`;
    }
    case 'monthly':
      return `${m} ${h} ${state.dayOfMonth} * *`;
  }
}

/** Recognizes a preset-shaped expression so the editor reopens in preset
 * mode; anything else (or an unparseable expression) opens in custom mode. */
export function presetFromExpr(expr: string): PresetState | null {
  const p = parseCron(expr);
  if (!p) return null;
  if (p.minute.values.size !== 1 || p.hour.values.size !== 1) return null;
  const [minute] = sorted(p.minute);
  const [hour] = sorted(p.hour);
  const time = formatWallClock(hour, minute);
  const base = { time, days: [] as number[], dayOfMonth: 1 };
  if (p.dow.restricted && !p.dom.restricted) {
    const days = sorted(p.dow);
    if (days.length === 7) return { ...base, freq: 'daily', days: [] };
    return { ...base, freq: 'weekly', days };
  }
  if (p.dom.restricted && !p.dow.restricted && p.dom.values.size === 1) {
    return { ...base, freq: 'monthly', dayOfMonth: sorted(p.dom)[0] };
  }
  if (!p.dow.restricted && !p.dom.restricted) {
    return { ...base, freq: 'daily', days: [] };
  }
  return null;
}
