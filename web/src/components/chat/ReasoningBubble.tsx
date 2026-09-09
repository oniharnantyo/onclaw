import { useEffect, useRef, useState } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { formatLatency } from "../../lib/toolDisplay";

/**
 * One reasoning segment of a turn, rendered as its own collapsible bubble.
 * A segment is "live" while it is the streaming tail of the turn (expanded,
 * "Thinking"); once tool calls or text move the turn on it collapses to a
 * "Thought" row with the wall-clock time the segment took. Rendered from the
 * ordered `parts` body (live and hydrated turns alike) and from the legacy
 * flat `reasoning` field.
 */
export function ReasoningBubble({ text, live, odId }: { text: string; live?: boolean; odId?: string }) {
  const [open, setOpen] = useState(false);
  const expanded = !!live || open;
  // Segment duration, measured client-side: transcript events carry no
  // reasoning timing, so the clock starts when a live segment first renders
  // and freezes when the turn moves on. Hydrated segments mount already
  // finished and stay duration-less (present-only, like the tool card's
  // Started row) — the duration is honest wall time of this session only.
  const startRef = useRef<number | null>(live ? Date.now() : null);
  const [ms, setMs] = useState<number | null>(null);
  useEffect(() => {
    if (live) {
      if (startRef.current === null) startRef.current = Date.now();
      return;
    }
    if (startRef.current !== null) {
      setMs(Date.now() - startRef.current);
      startRef.current = null;
    }
  }, [live]);
  return (
    <div className="mb-2 overflow-hidden rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_3%,transparent)]" data-od-id={odId}>
      <button type="button" onClick={() => setOpen(!open)} aria-expanded={expanded}
        className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]">
        <Icon name="chevright" size={13} className={cx('text-muted transition-transform', expanded && 'rotate-90')}/>
        <span aria-live="polite" className="text-[11px] font-medium text-muted">
          {live ? 'Thinking' : ms !== null ? `Thought · ${formatLatency(ms)}` : 'Thought'}
        </span>
      </button>
      {expanded && (
        <p className="od-scroll max-h-64 overflow-y-auto whitespace-pre-wrap border-t border-linesoft px-2.5 py-2 text-[13px] leading-relaxed text-muted">{text}</p>
      )}
    </div>
  );
}
