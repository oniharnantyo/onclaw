// @ts-nocheck
import { cx } from "../../lib/helpers";

export function Segmented({ value, options, onChange }) {
  return (
    <div className="flex rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] p-0.5" role="radiogroup">
      {options.map((o) => (
        <button key={o.id} type="button" role="radio" aria-checked={value === o.id} onClick={() => onChange(o.id)}
          className={cx('flex-1 rounded-[9px] px-2 py-1.5 text-[12px] font-medium transition-colors',
            value === o.id ? 'bg-surface text-fg shadow-[var(--elev-raised)]' : 'text-muted hover:text-fg')}>
          {o.label}
        </button>
      ))}
    </div>
  );
}

