/**
 * Unit tests for the preset ↔ cron-expression mappers (task 7.1, design
 * D11): every preset shape round-trips both ways, star and stepped
 * (star-slash-N) collapse behaves, weekly ranges expand to day lists,
 * and anything that is not an exact preset shape — six fields,
 * out-of-range numbers, day names, `@daily`, empty input — decomposes
 * to Advanced. Nothing here throws.
 */
import { describe, expect, it } from "vite-plus/test";

import {
  composeCron,
  decomposeCron,
  defaultPreset,
  PRESET_KINDS,
  WEEKDAY_LABELS,
  type RecurrencePreset,
} from "../cronPresets";

const PRESETS: RecurrencePreset[] = [
  { kind: "minutely", everyMinutes: 5 },
  { kind: "minutely", everyMinutes: 1 },
  { kind: "hourly", everyHours: 1, minute: 0 },
  { kind: "hourly", everyHours: 6, minute: 30 },
  { kind: "daily", hour: 9, minute: 0 },
  { kind: "weekly", days: [1, 5], hour: 17, minute: 30 },
  { kind: "monthly", dayOfMonth: 15, hour: 9, minute: 0 },
  { kind: "advanced", expr: "*/15 9-17 * * 1-5" },
];

describe("composeCron", () => {
  it("emits unpadded canonical fields for every shape", () => {
    expect(composeCron({ kind: "minutely", everyMinutes: 5 })).toBe(
      "*/5 * * * *"
    );
    expect(composeCron({ kind: "hourly", everyHours: 6, minute: 30 })).toBe(
      "30 */6 * * *"
    );
    expect(composeCron({ kind: "daily", hour: 9, minute: 0 })).toBe(
      "0 9 * * *"
    );
    expect(
      composeCron({ kind: "weekly", days: [1, 5], hour: 17, minute: 30 })
    ).toBe("30 17 * * 1,5");
    expect(
      composeCron({ kind: "monthly", dayOfMonth: 15, hour: 9, minute: 0 })
    ).toBe("0 9 15 * *");
  });

  it("collapses every-1 steps to plain stars", () => {
    expect(composeCron({ kind: "minutely", everyMinutes: 1 })).toBe(
      "* * * * *"
    );
    expect(composeCron({ kind: "hourly", everyHours: 1, minute: 0 })).toBe(
      "0 * * * *"
    );
  });

  it("sorts and dedupes weekly days", () => {
    expect(
      composeCron({ kind: "weekly", days: [5, 1, 1], hour: 17, minute: 30 })
    ).toBe("30 17 * * 1,5");
  });

  it("emits advanced expressions trimmed verbatim", () => {
    expect(composeCron({ kind: "advanced", expr: "  0 12 */2 * 1  " })).toBe(
      "0 12 */2 * 1"
    );
  });
});

describe("decomposeCron", () => {
  it("reads minutely expressions, star and stepped", () => {
    expect(decomposeCron("* * * * *")).toEqual({
      kind: "minutely",
      everyMinutes: 1,
    });
    expect(decomposeCron("*/15 * * * *")).toEqual({
      kind: "minutely",
      everyMinutes: 15,
    });
  });

  it("reads hourly expressions", () => {
    expect(decomposeCron("0 * * * *")).toEqual({
      kind: "hourly",
      everyHours: 1,
      minute: 0,
    });
    expect(decomposeCron("30 */6 * * *")).toEqual({
      kind: "hourly",
      everyHours: 6,
      minute: 30,
    });
  });

  it("reads a daily expression", () => {
    expect(decomposeCron("0 9 * * *")).toEqual({
      kind: "daily",
      hour: 9,
      minute: 0,
    });
  });

  it("reads a weekly expression and accepts agent-style ranges", () => {
    expect(decomposeCron("30 17 * * 1,5")).toEqual({
      kind: "weekly",
      days: [1, 5],
      hour: 17,
      minute: 30,
    });
    // Range input opens Weekly with Mon–Fri selected; recomposing emits
    // the expanded list, which the Go describer treats as the same days.
    expect(decomposeCron("0 9 * * 1-5")).toEqual({
      kind: "weekly",
      days: [1, 2, 3, 4, 5],
      hour: 9,
      minute: 0,
    });
    expect(composeCron(decomposeCron("0 9 * * 1-5"))).toBe("0 9 * * 1,2,3,4,5");
  });

  it("reads a monthly expression", () => {
    expect(decomposeCron("0 9 15 * *")).toEqual({
      kind: "monthly",
      dayOfMonth: 15,
      hour: 9,
      minute: 0,
    });
  });

  it("falls back to Advanced for non-preset shapes", () => {
    const advanced = [
      "*/15 9-17 * * 1-5", // stepped hour and range day mix shapes
      "0 25 * * *", // hour out of range
      "60 * * * *", // minute out of range
      "@daily", // shorthand
      "0 0 9 * * *", // six fields
      "MON", // day names are not numbers
      "0 9 * * 7", // 7 is not a cron weekday here
      "*/0 * * * *", // zero step
      "1-3,5", // not five fields
      "", // empty
    ];

    for (const expr of advanced) {
      expect(decomposeCron(expr)).toEqual({
        kind: "advanced",
        expr: expr.trim(),
      });
    }
  });

  it("trims whitespace before matching", () => {
    expect(decomposeCron("  0 9 * * *  ")).toEqual({
      kind: "daily",
      hour: 9,
      minute: 0,
    });
  });
});

describe("preset ↔ expression round trips", () => {
  it("decompose the canonical expression of every preset back into it", () => {
    for (const preset of PRESETS) {
      expect(decomposeCron(composeCron(preset))).toEqual(preset);
    }
  });

  it("recompose the decomposition of every canonical preset expression", () => {
    const canonical = [
      "*/5 * * * *",
      "* * * * *",
      "0 * * * *",
      "30 */6 * * *",
      "0 9 * * *",
      "30 17 * * 1,5",
      "0 9 15 * *",
      "*/15 9-17 * * 1-5",
    ];

    for (const expr of canonical) {
      expect(composeCron(decomposeCron(expr))).toBe(expr);
    }
  });
});

describe("defaultPreset", () => {
  it("supplies the starting value for each preset kind", () => {
    expect(defaultPreset("minutely")).toEqual({
      kind: "minutely",
      everyMinutes: 5,
    });
    expect(defaultPreset("hourly")).toEqual({
      kind: "hourly",
      everyHours: 1,
      minute: 0,
    });
    expect(defaultPreset("daily")).toEqual({
      kind: "daily",
      hour: 9,
      minute: 0,
    });
    expect(defaultPreset("weekly")).toEqual({
      kind: "weekly",
      days: [1],
      hour: 9,
      minute: 0,
    });
    expect(defaultPreset("monthly")).toEqual({
      kind: "monthly",
      dayOfMonth: 1,
      hour: 9,
      minute: 0,
    });
    expect(defaultPreset("advanced")).toEqual({
      kind: "advanced",
      expr: "0 9 * * *",
    });
  });
});

describe("constants", () => {
  it("orders PRESET_KINDS for the Segmented strip", () => {
    expect([...PRESET_KINDS]).toEqual([
      "minutely",
      "hourly",
      "daily",
      "weekly",
      "monthly",
      "advanced",
    ]);
  });

  it("labels weekdays Sunday-first to match cron numbering", () => {
    expect([...WEEKDAY_LABELS]).toEqual([
      "Sun",
      "Mon",
      "Tue",
      "Wed",
      "Thu",
      "Fri",
      "Sat",
    ]);
  });
});
