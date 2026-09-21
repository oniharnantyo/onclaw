// Timeline card (generative-ui spec, `$type: "timeline"`): a vertical axis of
// labeled events in the envelope's declared order — never re-sorted. Settled
// (past) events render a filled accent dot on normal text; future/reference
// events render hollow and dimmed. Events carry their dates for the
// timestamp line; detail lines render beneath when the envelope supplies
// them. An unusable event list falls back to the generic card (null).

import { cx } from "../helpers";

export interface TimelineEvent {
  label: string;
  /** ISO instant, or '' when the envelope supplied none/it was unparsable. */
  at: string;
  detail: string;
  /** Settled = past/done; future and reference events stay visually distinct. */
  settled: boolean;
}

const SETTLED_STATES = new Set(['done', 'past', 'settled']);
const REFERENCE_STATES = new Set(['upcoming', 'future', 'reference']);

/** Tolerant event parse. Explicit `state` decides settled-ness; without one,
 * a parsable `at` in the past is settled and an undated event is a reference
 * (never settled). Envelope order is preserved as declared. */
export function parseTimelineEvents(props: Record<string, unknown>): TimelineEvent[] | null {
  if (!Array.isArray(props.events)) return null;
  const now = Date.now();
  const events = props.events.map((raw: unknown): TimelineEvent => {
    const o = (raw !== null && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
    const label = typeof o.label === 'string' ? o.label : '';
    const at = typeof o.at === 'string' ? o.at : typeof o.date === 'string' ? o.date : '';
    const t = at ? Date.parse(at) : NaN;
    let settled: boolean;
    if (SETTLED_STATES.has(String(o.state))) settled = true;
    else if (REFERENCE_STATES.has(String(o.state))) settled = false;
    else settled = Number.isFinite(t) ? t <= now : false;
    return {
      label,
      at: Number.isFinite(t) ? at : '',
      detail: typeof o.detail === 'string' ? o.detail : '',
      settled,
    };
  });
  return events.length > 0 ? events : null;
}

/** Compact localized stamp for the timestamp line; falls back to the raw
 * string for exotic formats the parser accepted but Date cannot localize. */
function formatTimelineAt(iso: string): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return iso;
  return new Date(t).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

interface TimelineCardProps {
  title: string;
  events: TimelineEvent[];
  odId: string;
}

export function TimelineCard({ title, events, odId }: TimelineCardProps) {
  return (
    <div
      data-od-id={odId}
      className="mb-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-2.5 py-2"
    >
      {title && <p className="mb-1.5 text-[12px] text-fg2">{title}</p>}
      <ol className="space-y-2 border-s border-line ps-4">
        {events.map((event, i) => (
          <li key={i} className="relative">
            <span
              aria-hidden="true"
              className={cx(
                'absolute -left-[20.5px] top-[4px] h-2 w-2 rounded-full border',
                event.settled ? 'border-accent bg-accent' : 'border-muted bg-transparent'
              )}
            />
            <p className={cx('text-[12px] leading-4', event.settled ? 'text-fg2' : 'text-muted')}>
              {event.label || '(unlabeled)'}
            </p>
            {event.at && <p className="font-mono text-[10px] text-muted">{formatTimelineAt(event.at)}</p>}
            {event.detail && <p className="mt-0.5 text-[11px] leading-4 text-muted">{event.detail}</p>}
          </li>
        ))}
      </ol>
    </div>
  );
}
