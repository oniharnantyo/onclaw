// Compaction divider (chat-compact-command, mockups C/D): a hairline-ruled
// transcript marker for a context compaction. Live `onclaw:context_compacted`
// events and hydrated `context_compacted` history entries render through this
// one component — no agent ack message is fabricated.
import { formatTokens } from '../../lib/helpers';

/** `154k → 9.2k` span: formatTokens per side (k for thousands, one decimal
 * when needed; values below 1000 stay raw). */
export function formatTokenSpan(before: number, after: number): string {
  return `${formatTokens(before)} → ${formatTokens(after)}`;
}

export function CompactionDivider({ m  }: any) {
  const c = m?.compaction || {};
  return (
    <div className="flex flex-col items-center gap-1 px-2 py-1" data-od-id={'msg-' + m.id} data-role="compaction">
      <div className="flex w-full items-center gap-3">
        <span className="h-px flex-1 bg-line"/>
        <span className="text-[12px] text-muted">Context compacted</span>
        <span className="h-px flex-1 bg-line"/>
      </div>
      <p className="font-mono text-[11px] text-muted">
        {formatTokenSpan(c.tokensBefore ?? 0, c.tokensAfter ?? 0)} tokens{m.summarySaved ? ' · summary saved to transcript' : ''}
      </p>
    </div>
  );
}
