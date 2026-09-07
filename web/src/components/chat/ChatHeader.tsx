import React, { useEffect, useRef, useState } from "react";
import { cx, formatTokens } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";
import { Chip } from "../ui/Chip";
import { STATUS } from "../../lib/constants";

// Below 10% the label keeps one decimal: at the 200k default window a short
// turn moves the fill by well under a point, and integer rounding would leave
// the header reading the same percentage for many turns.
const pctLabel = (pct: number) => (pct > 0 && pct < 10 ? String(Math.round(pct * 10) / 10) : String(Math.round(pct)));
const exact = (n: number) => n.toLocaleString('en-US');
const warnText = 'text-[color-mix(in_oklab,var(--warn),black_38%)]';

function ContextMeter({ usage, effective, trigger }: { usage: any; effective: number; trigger: number }) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLSpanElement>(null);
  const pct = Math.min(100, Math.round((usage.finalInput / effective) * 1000) / 10);
  const warn = trigger > 0 && usage.finalInput >= trigger;
  const triggerPct = trigger > 0 ? Math.min(100, Math.round((trigger / effective) * 1000) / 10) : 0;

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: PointerEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  return (
    <span ref={rootRef} className="relative flex items-center">
      <button type="button" data-od-id="context-meter" aria-expanded={open} aria-haspopup="dialog"
        title={formatTokens(usage.finalInput) + ' / ' + formatTokens(effective)}
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-2 rounded-md transition-opacity hover:opacity-80">
        <span className="h-1 w-14 overflow-hidden rounded-full bg-linesoft">
          <span data-od-id="context-meter-fill" className={cx('block h-full rounded-full', warn ? 'bg-warn' : 'bg-accent')} style={{ width: pct + '%' }}/>
        </span>
        <span data-od-id="context-meter-pct" className={cx('font-mono text-[10px]', warn ? warnText : 'text-muted')}>
          {pctLabel(pct)}%
        </span>
      </button>
      {open && (
        <span role="dialog" aria-label="Context window usage" data-od-id="context-meter-details"
          className="absolute right-0 top-full z-30 mt-2 block w-60 rounded-lg border border-linesoft bg-surface p-3 shadow-lg">
          <span className="block text-[10px] font-semibold uppercase tracking-wide text-muted">Context</span>
          <span data-od-id="context-meter-used" className="mt-1.5 block font-mono text-[13px] text-fg">
            {exact(usage.finalInput)} <span className="text-muted">/ {exact(effective)}</span>
          </span>
          <span className="relative mt-1.5 block h-1.5 overflow-hidden rounded-full bg-linesoft">
            <span className={cx('block h-full rounded-full', warn ? 'bg-warn' : 'bg-accent')} style={{ width: pct + '%' }}/>
            {trigger > 0 && (
              <span data-od-id="context-meter-trigger-tick"
                className="absolute top-0 h-full w-px bg-[color-mix(in_oklab,var(--warn),black_30%)]"
                style={{ left: triggerPct + '%' }}/>
            )}
          </span>
          <span className={cx('mt-1.5 block font-mono text-[10px]', warn ? warnText : 'text-muted')}>
            {pctLabel(pct)}% of the context window
          </span>
        </span>
      )}
    </span>
  );
}

export function ChatHeader({ target, agent, channelMembers, usage, onToggleMembers, onConfigure  }: any) {
  const t = target;
  const meterVisible = t.kind === 'agent' && usage && usage.finalInput > 0 && agent.effective_context_window > 0;
  return (
    <header data-od-id="chat-header"
      className="flex h-14 shrink-0 items-center gap-3 border-b border-linesoft bg-bg px-5">
      {t.kind === 'channel' && <Icon name="hash" size={17} className="text-muted"/>}
      {t.kind === 'person' && <Icon name="at" size={16} className="text-muted"/>}
      {t.kind === 'agent' && (
        <span className="relative inline-flex shrink-0">
          <Avatar name={agent.name} avatar={agent.avatar} kind="agent" size={26}/>
          <span title={STATUS[agent.status].label}
            className={cx('absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full ring-2 ring-[var(--bg)]', STATUS[agent.status].dot, agent.status === 'running' && 'od-live')}/>
        </span>
      )}
      <div className="min-w-0">
        <h1 className="flex items-center gap-2 truncate text-[16px] font-semibold text-fg">
          {t.kind === 'channel' ? '#' + t.obj.name : t.obj.name}
          {t.kind === 'agent' && <Chip mono>{agent.model}</Chip>}
        </h1>
        <p className="truncate text-[12px] text-muted">
          {t.kind === 'agent' && (STATUS[agent.status].label + ' · last active ' + agent.lastActive)}
          {t.kind === 'channel' && (t.obj.purpose + ' · ' + (channelMembers && channelMembers.length ? channelMembers.length + ' members' : 'no members yet'))}
          {t.kind === 'person' && (t.obj.presence === 'online' ? 'Online' : 'Away') + ' · direct message'}
        </p>
      </div>
      <div className="ml-auto flex items-center gap-3">
        {meterVisible && <ContextMeter usage={usage} effective={agent.effective_context_window} trigger={agent.summarization_trigger_tokens || 0}/>}
        {channelMembers && channelMembers.length > 0 && (
          <button type="button" onClick={onToggleMembers} data-od-id="btn-channel-members"
            title="Show members"
            className="flex items-center -space-x-1.5 rounded-md transition-transform hover:scale-[1.04]">
            {channelMembers.slice(0, 4).map((m: any) => (
              <span key={m.id} className="rounded-md ring-2 ring-[var(--bg)]">
                <Avatar name={m.name} avatar={m.avatar || m.agent?.avatar} kind={m.kind === 'agent' ? 'agent' : 'other'} size={22}/>
              </span>
            ))}
            {channelMembers.length > 4 && (
              <span className="flex h-[22px] items-center rounded-md bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] px-1.5 font-mono text-[10px] font-semibold text-fg2 ring-2 ring-[var(--bg)]">
                +{channelMembers.length - 4}
              </span>
            )}
          </button>
        )}
        {t.kind === 'agent' && (
          <button type="button" onClick={onConfigure} data-od-id="btn-configure-agent" title={'Configure ' + agent.name}
            className="flex h-8 w-8 items-center justify-center rounded-md text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg2">
            <Icon name="sliders" size={16}/>
          </button>
        )}
      </div>
    </header>
  );
}
