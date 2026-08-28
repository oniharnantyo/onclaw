import React from "react";
import { cx } from "../../lib/helpers";
import { COMMANDS } from "../../lib/constants";

export function SlashMenu({ q, idx, onPick  }: any) {
  const list = COMMANDS.filter((c) => c.cmd.startsWith(q.toLowerCase()));
  if (!list.length || q === '') return null;
  return (
    <div className="od-pop absolute bottom-full left-0 right-0 mb-2 overflow-hidden rounded-md border border-line bg-warm shadow-[var(--elev-raised)]" data-od-id="slash-menu">
      <div className="border-b border-linesoft px-3 py-1.5 font-mono text-[10px] uppercase tracking-wider text-muted">Slash commands</div>
      {list.map((c, i) => (
        <button key={c.cmd} type="button" onMouseDown={(e) => { e.preventDefault(); onPick(c.cmd); }}
          className={cx('flex w-full items-center gap-3 px-3 py-2 text-left',
            i === idx ? 'bg-[color-mix(in_oklab,var(--accent)_15%,transparent)]' : 'hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)]')}>
          <span className="font-mono text-[13px] text-accent">{c.cmd}</span>
          <span className="text-[12px] text-muted">{c.desc}</span>
        </button>
      ))}
    </div>
  );
}

