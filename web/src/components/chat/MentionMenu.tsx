import React from "react";
import { cx } from "../../lib/helpers";
import { Avatar } from "../ui/Avatar";

export function MentionMenu({ options, idx, onPick  }: any) {
  if (!options.length) return null;
  return (
    <div className="od-pop absolute bottom-full left-0 right-0 mb-2 overflow-hidden rounded-md border border-line bg-warm shadow-[var(--elev-raised)]" data-od-id="mention-menu">
      <div className="border-b border-linesoft px-3 py-1.5 font-mono text-[10px] uppercase tracking-wider text-muted">Mention — agents respond in thread</div>
      {options.map((m, i) => (
        <button key={m.id} type="button" onMouseDown={(e) => { e.preventDefault(); onPick(m); }}
          className={cx('flex w-full items-center gap-2.5 px-3 py-2 text-left',
            i === idx ? 'bg-[color-mix(in_oklab,var(--accent)_15%,transparent)]' : 'hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)]')}>
          <Avatar name={m.name} avatar={m.avatar || m.agent?.avatar} kind={m.kind === 'agent' ? 'agent' : 'other'} size={20}/>
          <span className="text-[13px] font-medium text-fg">{m.name}</span>
          <span className="ml-auto font-mono text-[10px] text-muted">{m.kind === 'agent' ? 'Agent' : 'Member'}</span>
        </button>
      ))}
    </div>
  );
}

