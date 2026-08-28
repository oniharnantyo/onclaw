import { useState } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";

export function ToolCall({ t, running  }: any) {
  const [open, setOpen] = useState(false);
  const bad = !!t.error;
  return (
    <div className="mb-2 overflow-hidden rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]">
      <button type="button" onClick={() => setOpen(!open)} data-od-id={'tool-' + t.name}
        className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]">
        <Icon name="terminal" size={13} className={bad ? 'text-danger' : 'text-meta'}/>
        <span className={cx('font-mono text-[12px]', bad ? 'text-danger' : 'text-fg2')}>{t.name}</span>
        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-muted">{t.args}</span>
        {running ? (
          <span className="flex items-center gap-1.5 font-mono text-[10px] text-muted">
            <span className="od-dot"/><span className="od-dot"/><span className="od-dot"/>
          </span>
        ) : bad ? (
          <span className="font-mono text-[10px] text-danger">error · {t.ms}ms</span>
        ) : (
          <span className="font-mono text-[10px] text-muted">{t.ms}ms</span>
        )}
        <Icon name="chevright" size={13} className={cx('text-muted transition-transform', open && 'rotate-90')}/>
      </button>
      {open && (
        <div className="border-t border-linesoft px-2.5 py-2 font-mono text-[11px] leading-5">
          <p className="text-muted">args</p>
          <p className="mb-1.5 text-fg2">{t.args}</p>
          <p className="text-muted">{bad ? 'error' : 'result'}</p>
          <p className={bad ? 'text-danger' : 'text-fg2'}>{bad ? t.error : (t.res || ('ok — ' + (t.ms + 40) + 'ms, ' + Math.max(1, Math.round(t.ms / 90)) + ' rows'))}</p>
        </div>
      )}
    </div>
  );
}

