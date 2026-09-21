import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import type { QueuedMessage } from "../../store";

/** Label for one queued row: the message text, or the attachment names when
 * the entry queued as attachment-only (attachment-only sends are valid). */
const queuedLabel = (q: QueuedMessage): string =>
  q.text || (q.attachments || []).map((a) => a.name).join(', ') || 'Queued message';

/** The message queue stack (adopt-assistant-ui-elements 8.2, design D9):
 * rendered between the transcript and the composer while a run is active —
 * a running row naming the in-flight turn, then one cancelable row per
 * queued message showing its order. Present-only: nothing renders when
 * nothing is queued, and the running row appears only while the turn is
 * actually in flight. */
export function QueueStack({ items, running, agentName, onRemove }: {
  items: QueuedMessage[];
  running: boolean;
  agentName?: string;
  onRemove: (id: string) => void;
}) {
  if (!items || items.length === 0) return null;
  return (
    <div data-od-id="message-queue" aria-label="Message queue"
      className="mx-auto mb-2 w-full max-w-[44rem] px-4">
      <div className="overflow-hidden rounded-[12px] border border-line bg-surface">
        {running && (
          <div data-testid="queue-running-row" role="status"
            aria-label="Current run"
            className="flex items-center gap-2 border-b border-line px-3 py-1.5">
            <span aria-hidden className="h-1.5 w-1.5 animate-pulse rounded-full bg-accent"/>
            <span className="truncate text-[12px] font-medium text-fg2">
              {agentName ? agentName + ' is responding' : 'Responding'}
            </span>
          </div>
        )}
        {items.map((q, i) => (
          <div key={q.id} data-od-id="queue-item" data-queue-id={q.id}
            className={cx('flex items-center gap-2 px-3 py-1.5', i < items.length - 1 && 'border-b border-line')}>
            <span aria-hidden className="w-3 shrink-0 text-right text-[11px] tabular-nums text-muted">{i + 1}</span>
            <span className="min-w-0 flex-1 truncate text-[12px] leading-4 text-fg2" title={queuedLabel(q)}>
              {queuedLabel(q)}
            </span>
            <button type="button" data-testid="queue-remove" data-queue-remove={q.id}
              aria-label={'Remove queued message ' + (i + 1)} title="Remove from queue"
              onClick={() => onRemove(q.id)}
              className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg">
              <Icon name="x" size={11}/>
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
