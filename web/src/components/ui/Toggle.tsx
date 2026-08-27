// @ts-nocheck
import { cx } from "../../lib/helpers";

export function Toggle({ on, onChange, label }) {
  return (
    <button type="button" role="switch" aria-checked={on} aria-label={label} title={label}
      onClick={() => onChange(!on)}
      className={cx('relative h-5 w-9 shrink-0 rounded-full transition-colors duration-150',
        on ? 'bg-accent' : 'bg-[color-mix(in_oklab,var(--fg)_22%,transparent)]')}>
      <span className={cx('absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-surface transition-transform duration-150', on && 'translate-x-4')}/>
    </button>
  );
}

