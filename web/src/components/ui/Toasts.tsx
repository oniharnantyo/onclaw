// @ts-nocheck
import { cx } from "../../lib/helpers";
import { Icon } from "./Icon";

export function Toasts({ toasts }) {
  return (
    <div className="pointer-events-none fixed bottom-5 right-5 z-[70] flex w-80 flex-col gap-2" role="status" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className="od-pop pointer-events-auto flex items-start gap-2.5 rounded-md border border-line bg-warm px-3.5 py-2.5 shadow-[var(--elev-raised)]">
          <span className={cx('mt-0.5', t.kind === 'danger' ? 'text-danger' : 'text-accent')}>
            <Icon name={t.kind === 'danger' ? 'alert' : 'check'} size={15}/>
          </span>
          <p className="text-[13px] leading-5 text-fg2">{t.text}</p>
        </div>
      ))}
    </div>
  );
}

