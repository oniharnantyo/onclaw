// @ts-nocheck
import { cx } from "../../lib/helpers";

export function Avatar({ name, kind = 'other', size = 28 }) {
  const words = String(name || '?').trim().split(/\s+/);
  const init = (words.length > 1 ? words[0][0] + words[1][0] : words[0].slice(0, 2)).toUpperCase();
  const tone = kind === 'agent'
    ? 'bg-[color-mix(in_oklab,var(--accent)_16%,transparent)] text-accent'
    : kind === 'you'
      ? 'bg-[color-mix(in_oklab,var(--fg)_88%,transparent)] text-[var(--accent-on)]'
      : 'bg-[color-mix(in_oklab,var(--fg)_9%,transparent)] text-fg';
  return (
    <div className={cx('flex shrink-0 items-center justify-center rounded-md font-semibold', tone)}
      style={{ width: size, height: size, fontSize: size <= 24 ? 10 : 11 }}>
      {init}
    </div>
  );
}

