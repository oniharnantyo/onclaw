// Day separators + hover timestamps (adopt-assistant-ui-elements 9.2, design
// D11): a full-width hairline divider renders wherever the calendar day
// changes between consecutive DATED transcript entries — today and yesterday
// by name, older days by date. Entries carry their parseable instant in `at`
// (live mints) or a RFC3339 `ts` (hydrated history); legacy display strings
// ("9:14 AM") do not parse and contribute neither a divider nor a timestamp.
// Hairline styling matches the compaction divider's transcript idiom.

const DAY_MS = 24 * 60 * 60 * 1000;

const DAY_KEY_FMT = { month: 'short', day: 'numeric', year: 'numeric' } as const;
const STAMP_FMT = { ...DAY_KEY_FMT, hour: 'numeric', minute: '2-digit' } as const;

/** The entry's parseable instant, or null. `at` (live ISO) wins; hydrated
 * history carries RFC3339 in `ts`; anything unparseable stays undated. */
export function parseEntryDate(m: any): Date | null {
  for (const raw of [m?.at, m?.ts]) {
    if (typeof raw !== 'string' || !raw) continue;
    const d = new Date(raw);
    if (!Number.isNaN(d.getTime())) return d;
  }
  return null;
}

/** Local calendar-day key — dividers follow the viewer's clock, like the
 * labels do. */
export function dayKeyOf(d: Date): string {
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
}

/** Divider label: today and yesterday by name, older days by date. */
export function dayLabel(d: Date): string {
  const today = new Date();
  const days = Math.round((startOfDay(today) - startOfDay(d)) / DAY_MS);
  if (days === 0) return 'Today';
  if (days === 1) return 'Yesterday';
  return d.toLocaleDateString(undefined, DAY_KEY_FMT);
}

/** Full date+time for an entry's hover title ("Sep 18, 2026, 5:00 PM"). */
export function fullStamp(d: Date): string {
  return d.toLocaleString(undefined, STAMP_FMT);
}

const startOfDay = (d: Date): number =>
  new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();

export function DayDivider({ date }: { date: Date }) {
  const label = dayLabel(date);
  return (
    <div className="flex items-center gap-3 px-2" data-od-id="day-divider"
      role="separator" aria-label={label}>
      <span className="h-px flex-1 bg-line"/>
      <span className="text-[12px] text-muted">{label}</span>
      <span className="h-px flex-1 bg-line"/>
    </div>
  );
}
