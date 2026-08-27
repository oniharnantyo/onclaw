// @ts-nocheck
import React from "react";
import { cx } from "../lib/helpers";
import { Icon } from "../components/ui/Icon";
import { Avatar } from "../components/ui/Avatar";
import { StatusDot } from "../components/ui/StatusDot";
import { Chip } from "../components/ui/Chip";

export function AgentCard({ a, onChat, onConfigure }) {
  return (
    <article data-od-id={'agent-card-' + a.id}
      className="flex flex-col rounded-lg border border-line bg-[color-mix(in_oklab,var(--bg)_35%,var(--surface))] p-4 transition-colors hover:border-[color-mix(in_oklab,var(--fg)_24%,transparent)]">
      <div className="flex items-start gap-3">
        <Avatar name={a.name} kind="agent" size={36}/>
        <div className="min-w-0 flex-1">
          <h3 className="flex items-center gap-2 text-[15px] font-semibold text-fg">
            <span className="truncate">{a.name}</span>
            <StatusDot status={a.status}/>
          </h3>
          <p className="mt-0.5 font-mono text-[11px] text-muted">{a.model} · temp {a.temp.toFixed(1)}</p>
        </div>
      </div>
      <p className="mt-3 min-h-10 flex-1 text-[13px] leading-5 text-fg2">{a.role}</p>
      {a.status === 'error' && (
        <p className="mb-3 flex items-center gap-1.5 text-[12px] text-danger">
          <Icon name="alert" size={13}/> Needs attention — see latest run
        </p>
      )}
      <div className="flex flex-wrap items-center gap-1.5">
        {a.tools.slice(0, 4).map((t) => <Chip key={t} mono>{t}</Chip>)}
      </div>
      <div className="mt-4 flex items-center gap-2">
        <button type="button" onClick={onChat} data-od-id={'agent-chat-' + a.id}
          className="flex h-8 flex-1 items-center justify-center gap-1.5 rounded-md border border-line text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg">
          <Icon name="chat" size={14}/> Open chat
        </button>
        <button type="button" onClick={onConfigure} data-od-id={'agent-configure-' + a.id}
          className="flex h-8 items-center justify-center gap-1.5 rounded-md px-3 text-[13px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2">
          <Icon name="sliders" size={14}/> Configure
        </button>
      </div>
      <p className="mt-3 font-mono text-[10px] text-muted">last active {a.lastActive}</p>
    </article>
  );
}

