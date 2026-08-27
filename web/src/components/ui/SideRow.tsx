import { cx } from "../../lib/helpers";

export function SideRow({ active, icon, label, sub, right, onClick, odId, title  }: any) {
  return (
    <button type="button" onClick={onClick} data-od-id={odId} title={title || label}
      className={cx('flex h-[30px] w-full items-center gap-2 rounded-md px-2.5 text-left transition-colors',
        active
          ? 'bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-fg'
          : 'text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg')}>
      {icon}
      <span className="min-w-0 flex-1 truncate text-[13px]">{label}</span>
      {sub}
      {right}
    </button>
  );
}

