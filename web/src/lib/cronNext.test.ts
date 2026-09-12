/**
 * @vitest-environment node
 */
// Pure cron math for the schedule editor (change integrate-scheduler, 7.5):
// the preset matrix (builder combos → expected expressions → expected next
// runs), Vixie DOM/DOW OR, and a DST-safe assertion using explicit instants
// in America/Los_Angeles.
import { describe, it, expect } from 'vitest';
import {
  parseCron,
  cronNext,
  cronNextRuns,
  describeCron,
  describeDays,
  buildPresetExpr,
  presetFromExpr,
  parseTimeInput,
  wallTimeToInstant,
} from './cronNext';

const UTC = 'UTC';
const LA = 'America/Los_Angeles';
const at = (iso: string) => new Date(iso);

describe('cronNext — parser (Vixie 5-field standard)', () => {
  it('accepts the standard field grammar', () => {
    expect(parseCron('0 9 * * 1-5')).not.toBeNull();
    expect(parseCron('*/30 * * * *')).not.toBeNull();
    expect(parseCron('0 9,18 1,15 */2 0,7')).not.toBeNull();
    expect(parseCron('30 17 * * fri')).not.toBeNull();
    expect(parseCron('0 9 1 jan *')).not.toBeNull();
    expect(parseCron('5/15 * * * *')).not.toBeNull();
  });

  it('rejects junk, wrong arity, and out-of-range values', () => {
    expect(parseCron('at nine')).toBeNull();
    expect(parseCron('0 9 * *')).toBeNull();
    expect(parseCron('0 9 * * * *')).toBeNull();
    expect(parseCron('60 * * * *')).toBeNull();
    expect(parseCron('* 24 * * *')).toBeNull();
    expect(parseCron('* * 0 * *')).toBeNull();
    expect(parseCron('* * * 13 *')).toBeNull();
    expect(parseCron('* * * * 8')).toBeNull();
    expect(parseCron('5-1 * * * *')).toBeNull();
    expect(parseCron('')).toBeNull();
  });

  it('maps dow 7 to Sunday and reads names case-insensitively', () => {
    expect(parseCron('* * * * 0')?.dow.values).toEqual(parseCron('* * * * 7')?.dow.values);
    expect(parseCron('* * * * SUN')?.dow.values.has(0)).toBe(true);
    expect(parseCron('* * * Jan *')?.month.values.has(1)).toBe(true);
  });

  it('treats a full span like */1 as unrestricted for the DOM/DOW rule', () => {
    expect(parseCron('0 9 * * */1')?.dow.restricted).toBe(false);
    expect(parseCron('0 9 * * *')?.dow.restricted).toBe(false);
    expect(parseCron('0 9 * * 1-5')?.dow.restricted).toBe(true);
  });
});

describe('cronNext — preset matrix (builder combos → expressions → next runs)', () => {
  const base = { time: '09:00', days: [1, 2, 3, 4, 5], dayOfMonth: 1 };

  it('hourly generates a minute-only field that fires on the next hour', () => {
    const expr = buildPresetExpr({ ...base, freq: 'hourly' });
    expect(expr).toBe('0 * * * *');
    expect(cronNext(expr!, at('2026-09-10T15:07:30Z'), UTC)?.toISOString()).toBe('2026-09-10T16:00:00.000Z');
  });

  it('daily generates the minute+hour pair and fires once per day', () => {
    const expr = buildPresetExpr({ ...base, freq: 'daily', days: [] });
    expect(expr).toBe('0 9 * * *');
    const fires = cronNextRuns(expr!, at('2026-09-10T15:07:30Z'), UTC, 3);
    expect(fires.map((d) => d.toISOString())).toEqual([
      '2026-09-11T09:00:00.000Z',
      '2026-09-12T09:00:00.000Z',
      '2026-09-13T09:00:00.000Z',
    ]);
  });

  it('the spec scenario: daily preset + 09:00 + Mon–Fri chips → "09:00 · Mon–Fri"', () => {
    const expr = buildPresetExpr({ ...base, freq: 'daily', days: [1, 2, 3, 4, 5] });
    expect(expr).toBe('0 9 * * 1-5');
    expect(describeCron(expr!)).toBe('09:00 · Mon–Fri');
  });

  it('weekly with Mon–Fri chips emits the collapsed range and skips weekends', () => {
    const expr = buildPresetExpr({ ...base, freq: 'weekly', days: [1, 2, 3, 4, 5] });
    expect(expr).toBe('0 9 * * 1-5');
    // Thursday 2026-09-10 15:07Z → Fri, then Monday.
    const fires = cronNextRuns(expr!, at('2026-09-10T15:07:30Z'), UTC, 3);
    expect(fires.map((d) => d.toISOString())).toEqual([
      '2026-09-11T09:00:00.000Z',
      '2026-09-14T09:00:00.000Z',
      '2026-09-15T09:00:00.000Z',
    ]);
  });

  it('weekly with scattered chips emits a comma list', () => {
    expect(buildPresetExpr({ ...base, freq: 'weekly', days: [1, 3, 5] })).toBe('0 9 * * 1,3,5');
    expect(buildPresetExpr({ ...base, freq: 'weekly', days: [0, 6] })).toBe('0 9 * * 0,6');
    expect(buildPresetExpr({ ...base, freq: 'weekly', days: [3] })).toBe('0 9 * * 3');
  });

  it('monthly pins the day of month and lands on short months correctly', () => {
    const expr = buildPresetExpr({ ...base, freq: 'monthly', dayOfMonth: 1 });
    expect(expr).toBe('0 9 1 * *');
    const fires = cronNextRuns('0 9 1 * *', at('2026-01-31T00:00:00Z'), UTC, 4);
    expect(fires.map((d) => d.toISOString())).toEqual([
      '2026-02-01T09:00:00.000Z',
      '2026-03-01T09:00:00.000Z',
      '2026-04-01T09:00:00.000Z',
      '2026-05-01T09:00:00.000Z',
    ]);
  });

  it('rejects builder states without a valid time or days', () => {
    expect(buildPresetExpr({ ...base, freq: 'daily', time: '25:00' })).toBeNull();
    expect(buildPresetExpr({ ...base, freq: 'daily', time: 'nine' })).toBeNull();
    expect(buildPresetExpr({ ...base, freq: 'weekly', days: [] })).toBeNull();
  });

  it('round-trips preset expressions back into preset mode (and not custom ones)', () => {
    expect(presetFromExpr('0 9 * * *')).toMatchObject({ freq: 'daily', time: '09:00' });
    expect(presetFromExpr('30 17 * * 1-5')).toMatchObject({ freq: 'weekly', time: '17:30', days: [1, 2, 3, 4, 5] });
    expect(presetFromExpr('0 9 * * 1,3,5')).toMatchObject({ freq: 'weekly', days: [1, 3, 5] });
    expect(presetFromExpr('0 9 15 * *')).toMatchObject({ freq: 'monthly', dayOfMonth: 15 });
    // */30 in the minute field has 30 values — custom mode.
    expect(presetFromExpr('*/30 * * * *')).toBeNull();
    // DOW + DOM both restricted — not a preset shape.
    expect(presetFromExpr('0 9 1 * 1')).toBeNull();
  });

  it('parses time picker input', () => {
    expect(parseTimeInput('09:00')).toEqual([9, 0]);
    expect(parseTimeInput('9:05')).toEqual([9, 5]);
    expect(parseTimeInput('23:59')).toEqual([23, 59]);
    expect(parseTimeInput('24:00')).toBeNull();
    expect(parseTimeInput('9')).toBeNull();
  });
});

describe('cronNext — next-run computation (Vixie semantics)', () => {
  it('is strictly after the cursor: a fire exactly at "now" is skipped', () => {
    expect(cronNext('0 9 * * *', at('2026-09-11T09:00:00Z'), UTC)?.toISOString()).toBe('2026-09-12T09:00:00.000Z');
    expect(cronNext('0 9 * * *', at('2026-09-11T09:00:59Z'), UTC)?.toISOString()).toBe('2026-09-12T09:00:00.000Z');
  });

  it('implements the DOM/DOW OR rule when both fields are restricted', () => {
    // Fires on the 1st of the month OR any Monday: three Mondays in a row,
    // then the 1st of October — a Thursday reached through the DOM side.
    const fires = cronNextRuns('0 0 1 * 1', at('2026-09-10T00:00:00Z'), UTC, 4);
    expect(fires.map((d) => d.toISOString())).toEqual([
      '2026-09-14T00:00:00.000Z', // Monday
      '2026-09-21T00:00:00.000Z', // Monday
      '2026-09-28T00:00:00.000Z', // Monday
      '2026-10-01T00:00:00.000Z', // 1st (Thursday) — the OR's DOM arm
    ]);
  });

  it('skips impossible day-of-month values in short months', () => {
    expect(cronNext('0 0 31 * *', at('2026-02-01T00:00:00Z'), UTC)?.toISOString()).toBe('2026-03-31T00:00:00.000Z');
  });

  it('steps across hours and minutes', () => {
    const fires = cronNextRuns('*/15 3 * * *', at('2026-09-10T03:32:00Z'), UTC, 3);
    expect(fires.map((d) => d.toISOString())).toEqual([
      '2026-09-10T03:45:00.000Z',
      '2026-09-11T03:00:00.000Z',
      '2026-09-11T03:15:00.000Z',
    ]);
  });

  it('returns [] for expressions that never fire again', () => {
    expect(cronNextRuns('0 0 31 2 *', at('2026-02-01T00:00:00Z'), UTC, 3)).toEqual([]);
    expect(cronNextRuns('at nine', at('2026-02-01T00:00:00Z'), UTC, 3)).toEqual([]);
  });
});

describe('cronNext — DST safety (explicit instants, America/Los_Angeles)', () => {
  it('keeps wall-clock 09:00 across the spring-forward transition', () => {
    // DST starts 2026-03-08 02:00 local. 09:00 stays 09:00 on the wall while
    // the UTC instant shifts an hour earlier (PST −08:00 → PDT −07:00).
    const fires = cronNextRuns('0 9 * * *', at('2026-03-06T15:00:00Z'), LA, 3);
    expect(fires.map((d) => d.toISOString())).toEqual([
      '2026-03-06T17:00:00.000Z', // 09:00 PST
      '2026-03-07T17:00:00.000Z', // 09:00 PST
      '2026-03-08T16:00:00.000Z', // 09:00 PDT — one wall-clock day later, one UTC hour earlier
    ]);
  });

  it('keeps wall-clock 12:00 across the fall-back transition', () => {
    // DST ends 2026-11-01 02:00 local: the UTC instant shifts an hour later.
    // (09:00 occurs twice that day, so noon makes the shift visible.)
    const fires = cronNextRuns('0 12 * * *', at('2026-10-30T17:00:00Z'), LA, 3);
    expect(fires.map((d) => d.toISOString())).toEqual([
      '2026-10-30T19:00:00.000Z', // 12:00 PDT
      '2026-10-31T19:00:00.000Z', // 12:00 PDT
      '2026-11-01T20:00:00.000Z', // 12:00 PST
    ]);
  });

  it('resolves an ambiguous fold time to the earlier occurrence', () => {
    // 01:30 occurs twice on 2026-11-01 in LA (01:30 PDT and 01:30 PST); the
    // earlier (PDT) instant wins.
    expect(wallTimeToInstant({ year: 2026, month: 11, day: 1, hour: 1, minute: 30 }, LA).toISOString())
      .toBe('2026-11-01T08:30:00.000Z');
  });

  it('normalizes a nonexistent wall time forward like Go time.Date', () => {
    // 02:30 does not exist on 2026-03-08 in LA (02:00 → 03:00); robfig fires
    // the normalized 03:30 PDT instant.
    expect(wallTimeToInstant({ year: 2026, month: 3, day: 8, hour: 2, minute: 30 }, LA).toISOString())
      .toBe('2026-03-08T10:30:00.000Z');
  });
});

describe('cronNext — human label (mirrors the server derivation)', () => {
  it('phrases the preset shapes without cron notation', () => {
    expect(describeCron('0 9 * * 1-5')).toBe('09:00 · Mon–Fri');
    expect(describeCron('0 7 * * 1-5')).toBe('07:00 · Mon–Fri');
    expect(describeCron('30 17 * * 5')).toBe('17:30 · Fri');
    expect(describeCron('0 9 * * *')).toBe('09:00 · Daily');
    expect(describeCron('*/30 * * * *')).toBe('Every 30 minutes');
    expect(describeCron('* * * * *')).toBe('Every minute');
    expect(describeCron('0 9 1 * *')).toBe('09:00 · Monthly on the 1st');
  });

  it('falls back to the raw expression for custom recurrences', () => {
    expect(describeCron('5,35 1-3/2 * * 0,6')).toBe('5,35 1-3/2 * * 0,6');
    expect(describeCron('at nine')).toBe('at nine');
  });

  it('describes day lists with ranges and lists', () => {
    expect(describeDays([1, 2, 3, 4, 5])).toBe('Mon–Fri');
    expect(describeDays([1, 3, 5])).toBe('Mon, Wed, Fri');
    expect(describeDays([0])).toBe('Sun');
    expect(describeDays([0, 1, 2, 3, 4, 5, 6])).toBe('Sun–Sat');
  });
});
