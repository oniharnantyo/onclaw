import { cx } from "../../lib/helpers";
import { Icon } from "./Icon";

export function OptionChips({ options, value, onChange, iconOf  }: any) {
  return (
    <div className="flex flex-wrap gap-2">
      {options.map((o: any) => {
        const on = value.includes(o.id);
        return (
          <button key={o.id} type="button" aria-pressed={on} title={o.detail || o.label}
            onClick={() => onChange(on ? value.filter((x: any) => x !== o.id) : [...value, o.id])}
            className={cx('flex h-8 items-center gap-1.5 rounded-md border px-3 text-[12px] font-medium transition-colors',
              on ? 'border-accent bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg' : 'border-line text-muted hover:border-[color-mix(in_oklab,var(--fg)_26%,transparent)] hover:text-fg2')}>
            {iconOf && <Icon name={iconOf(o)} size={13}/>}
            {o.label}
          </button>
        );
      })}
    </div>
  );
}

