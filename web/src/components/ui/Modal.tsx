// @ts-nocheck
import { useEffect } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "./Icon";

export function Modal({ title, onClose, children, footer, wide, odId }) {
  useEffect(() => {
    const h = (e) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', h);
    return () => window.removeEventListener('keydown', h);
  }, [onClose]);
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-0 md:p-6" role="dialog" aria-modal="true" aria-label={title} data-od-id={odId}>
      <div className="od-fade absolute inset-0 bg-[color-mix(in_oklab,var(--fg)_32%,transparent)]" onClick={onClose}/>
      <div className={cx('od-pop relative flex h-[100dvh] md:h-auto max-h-[100dvh] md:max-h-[86vh] w-full flex-col overflow-hidden rounded-none md:rounded-lg border border-line bg-surface shadow-[var(--elev-raised)]',
        wide ? 'max-w-4xl' : 'max-w-lg')}>
        <div className="flex h-14 shrink-0 items-center justify-between border-b border-linesoft px-5">
          <h2 className="text-[17px] font-semibold text-fg">{title}</h2>
          <button type="button" onClick={onClose} aria-label="Close dialog"
            className="flex h-8 w-8 items-center justify-center rounded-md text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg">
            <Icon name="x" size={16}/>
          </button>
        </div>
        <div className="od-scroll flex-1 overflow-y-auto">{children}</div>
        {footer && <div className="flex shrink-0 items-center justify-end gap-2.5 border-t border-linesoft bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] px-5 py-3.5">{footer}</div>}
      </div>
    </div>
  );
}

