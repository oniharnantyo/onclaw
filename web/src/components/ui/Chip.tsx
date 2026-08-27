// @ts-nocheck
import { cx } from "../../lib/helpers";

export function Chip({ children, className = '', mono = false }) {
  return (
    <span className={cx('inline-flex items-center gap-1 rounded-[6px] border border-line bg-[color-mix(in_oklab,var(--fg),5%,transparent)] px-1.5 py-0.5 text-[11px] leading-4 text-fg2', mono && 'font-mono', className)}>
      {children}
    </span>
  );
}

