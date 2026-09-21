// Shared activity status label (fix-tool-timeline-fold 1.2): shimmering
// activity text with an optional elapsed suffix. The shimmer span is keyed
// on the label text so the sweep animation replays on every flip.
import type React from 'react';
import { formatLatency } from '../../lib/toolDisplay';

export function ActivityLabel({ label, elapsedMs, className }: {
  label: string;
  elapsedMs?: number | null;   // present-only: null/undefined renders no elapsed suffix
  className?: string;          // extra classes on the outer span
}): React.JSX.Element {
  return (
    <span aria-live="polite" className={className ?? 'font-mono text-[11px] text-muted'}>
      <span key={label} className="od-shimmer">{label}</span>
      {typeof elapsedMs === 'number' && Number.isFinite(elapsedMs) && elapsedMs > 0 && (
        <span className="text-muted"> · {formatLatency(elapsedMs)}</span>
      )}
    </span>
  );
}
