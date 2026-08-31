import { useState } from 'react';
import { useConnectionStore } from '../store/connection';
import { cx } from '../lib/helpers';

export interface ConnectionBannerProps {
  message?: string;
  onRetry?: () => void | Promise<void>;
  degraded?: boolean;
  className?: string;
}

export function ConnectionBanner({
  message = 'Connection lost. Reconnecting to OnClaw…',
  onRetry,
  degraded: propDegraded,
  className,
}: ConnectionBannerProps) {
  const storeDegraded = useConnectionStore((s) => s.degraded);
  const degraded = propDegraded !== undefined ? propDegraded : storeDegraded;
  const [retrying, setRetrying] = useState(false);

  if (!degraded) {
    return null;
  }

  const handleRetry = async () => {
    setRetrying(true);
    try {
      if (onRetry) {
        await onRetry();
      }
    } finally {
      setRetrying(false);
    }
  };

  return (
    <div
      role="status"
      aria-live="polite"
      data-testid="connection-banner"
      className={cx(
        'sticky top-0 z-50 flex w-full items-center justify-between gap-3 border-b border-accent/40 bg-[color-mix(in_oklab,var(--surface)_94%,var(--accent))] px-4 py-2 text-[13px] text-fg shadow-sm od-fade',
        className
      )}
    >
      <div className="flex items-center gap-2 font-medium">
        <span className="flex h-2 w-2 rounded-full bg-warn animate-pulse shrink-0" />
        <span>{message}</span>
      </div>
      <button
        type="button"
        onClick={handleRetry}
        disabled={retrying}
        className="inline-flex h-7 items-center justify-center rounded bg-accent px-3 text-[12px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] cursor-pointer disabled:opacity-50 shrink-0"
      >
        {retrying ? (
          <span className="flex items-center gap-1.5">
            <span className="inline-block h-3 w-3 animate-spin rounded-full border border-current border-t-transparent" />
            <span>Retrying…</span>
          </span>
        ) : (
          'Retry'
        )}
      </button>
    </div>
  );
}

export default ConnectionBanner;
