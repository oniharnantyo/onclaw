// @ts-nocheck
import { useRef, useEffect } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";

export function WorkspaceSwitcher({ open, db, currentId, onPick, onClose, onCreate }) {
  const ref = useRef(null);
  useEffect(() => {
    if (!open) return;
    const h = (e) => { if (ref.current && !ref.current.contains(e.target)) onClose(); };
    window.addEventListener('mousedown', h);
    return () => window.removeEventListener('mousedown', h);
  }, [open, onClose]);
  if (!open) return null;
  return (
    <div ref={ref} data-od-id="ws-switcher-popover"
      className="od-pop fixed left-[76px] top-[14px] z-40 w-64 overflow-hidden rounded-md border border-line bg-surface shadow-[var(--elev-raised)]">
      <div className="border-b border-linesoft px-3.5 pt-3 pb-2 text-[11px] font-semibold uppercase tracking-wider text-muted">Workspaces</div>
      <ul className="py-1.5">
        {Object.values(db).map((t) => (
          <li key={t.id}>
            <button type="button" onClick={() => onPick(t.id)} data-od-id={'ws-option-' + t.id}
              className={cx('flex w-full items-center gap-2.5 px-3.5 py-2 text-left transition-colors',
                t.id === currentId ? 'bg-[color-mix(in_oklab,var(--accent)_13%,transparent)]' : 'hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)]')}>
              <span className={cx('flex h-7 w-7 items-center justify-center rounded-md text-[11px] font-semibold',
                t.id === currentId ? 'bg-accent text-accenton' : 'bg-warm text-fg2')}>
                {t.name.split(' ').map((w) => w[0]).join('')}
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-[13px] font-medium text-fg">{t.name}</span>
                <span className="block font-mono text-[10px] text-muted">{t.agents.length} agents · {t.cron.filter((c) => c.enabled).length} schedules</span>
              </span>
              {t.id === currentId && <Icon name="check" size={14} className="shrink-0 text-accent"/>}
              <span className={cx('rounded-[5px] px-1.5 py-0.5 text-[10px] font-semibold',
                t.plan === 'Pro' ? 'bg-[color-mix(in_oklab,var(--accent)_18%,transparent)] text-accent' : 'bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted')}>
                {t.plan}
              </span>
            </button>
          </li>
        ))}
      </ul>
      <div className="border-t border-linesoft p-2">
        <button type="button" onClick={onCreate} data-od-id="ws-create"
          className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-[13px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg">
          <Icon name="plus" size={14}/> Create workspace
        </button>
      </div>
    </div>
  );
}

