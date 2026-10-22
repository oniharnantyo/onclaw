/**
 * Unit tests for the relative-time formatter (task 2.1): the "just now"
 * floor, whole minute/hour/day buckets, and the short-date fallback past a
 * week. Deterministic via the injectable `now` — no fake timers — and the
 * date fallback is asserted by shape so the host timezone can't flake it.
 */
import { describe, expect, it } from "vite-plus/test";

import { relativeTime } from "../relativeTime";

const NOW = new Date("2026-10-16T12:00:00Z");

function minutesAgo(minutes: number): string {
  return new Date(NOW.getTime() - minutes * 60_000).toISOString();
}

describe("relativeTime", () => {
  it("reads as just now inside the first minute", () => {
    expect(relativeTime(minutesAgo(0.2), NOW)).toBe("just now");
  });

  it("formats whole minutes under an hour", () => {
    expect(relativeTime(minutesAgo(5), NOW)).toBe("5m");
  });

  it("formats whole hours under a day", () => {
    expect(relativeTime(minutesAgo(120), NOW)).toBe("2h");
  });

  it("formats whole days under a week", () => {
    expect(relativeTime(minutesAgo(60 * 72), NOW)).toBe("3d");
  });

  it("falls back to a short date a week out", () => {
    expect(relativeTime(minutesAgo(60 * 24 * 14), NOW)).toMatch(
      /^[A-Z][a-z]{2} \d{1,2}$/
    );
  });

  it("reads future timestamps as just now and blanks invalid ones", () => {
    expect(relativeTime(minutesAgo(-5), NOW)).toBe("just now");
    expect(relativeTime("not-a-date", NOW)).toBe("");
  });
});
