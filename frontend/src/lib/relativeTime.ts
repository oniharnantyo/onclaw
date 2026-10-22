/**
 * Compact relative timestamps for the Sessions island meta line (task 2.1):
 * "just now" inside the first minute, then whole minutes, hours, and days,
 * and a short locale date once the activity is a week old or more — past
 * that, relative labels stop carrying information. `now` is injectable so
 * tests pin the clock without fake timers.
 */

const MINUTE_MS = 60_000;

const HOUR_MS = 60 * MINUTE_MS;

const DAY_MS = 24 * HOUR_MS;

/** Cutoff where "Nd" stops being useful and a calendar date takes over. */
const WEEK_MS = 7 * DAY_MS;

const shortDate = new Intl.DateTimeFormat(undefined, {
  day: "numeric",
  month: "short",
});

export function relativeTime(
  timestamp: string,
  now: Date = new Date()
): string {
  const then = new Date(timestamp).getTime();

  if (Number.isNaN(then)) return "";

  const elapsed = now.getTime() - then;

  if (elapsed < MINUTE_MS) return "just now";

  if (elapsed < HOUR_MS) return `${Math.floor(elapsed / MINUTE_MS)}m`;

  if (elapsed < DAY_MS) return `${Math.floor(elapsed / HOUR_MS)}h`;

  if (elapsed < WEEK_MS) return `${Math.floor(elapsed / DAY_MS)}d`;

  return shortDate.format(then);
}
