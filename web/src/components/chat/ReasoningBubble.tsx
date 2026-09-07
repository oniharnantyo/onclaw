import { useState } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";

/**
 * One reasoning segment of a turn, rendered as its own collapsible bubble.
 * A segment is "live" while it is the streaming tail of the turn (expanded,
 * "Thinking…"); once tool calls or text move the turn on it collapses to a
 * plain "Reasoning" row. Rendered from the ordered `parts` body (live and
 * hydrated turns alike) and from the legacy flat `reasoning` field.
 */
export function ReasoningBubble({ text, live, odId }: { text: string; live?: boolean; odId?: string }) {
  const [open, setOpen] = useState(false);
  const expanded = !!live || open;
  return (
    <div className="mb-2 overflow-hidden rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_3%,transparent)]" data-od-id={odId}>
      <button type="button" onClick={() => setOpen(!open)} aria-expanded={expanded}
        className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]">
        <Icon name="chevright" size={13} className={cx('text-muted transition-transform', expanded && 'rotate-90')}/>
        <span className="text-[11px] font-medium text-muted">{live ? 'Thinking…' : 'Reasoning'}</span>
      </button>
      {expanded && (
        <p className="od-scroll max-h-64 overflow-y-auto whitespace-pre-wrap border-t border-linesoft px-2.5 py-2 text-[13px] leading-relaxed text-muted">{text}</p>
      )}
    </div>
  );
}
