import { useState, useRef, useEffect } from "react";
import { cx, memberHandle } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";
import { StatusDot } from "../ui/StatusDot";
import { MentionText } from "../ui/MentionText";
import { Chip } from "../ui/Chip";
import { STATUS, COMMANDS } from "../../lib/constants";

export function ContextPanel({ channelMembers, candidates, primaryAgentId, onAddMember, onRemoveMember, onClose, onOpenMember  }: any) {
  const [adding, setAdding] = useState(false);
  return (
    <aside data-od-id="context-panel" aria-label="Channel members"
      className="od-scroll w-[300px] shrink-0 overflow-y-auto border-l border-linesoft bg-bg">
      <div className="flex h-14 items-center justify-between border-b border-linesoft px-4">
        <h2 className="text-[13px] font-semibold text-fg">Members</h2>
        <button type="button" onClick={onClose} aria-label="Close members panel"
          className="flex h-7 w-7 items-center justify-center rounded-md text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg">
          <Icon name="x" size={14}/>
        </button>
      </div>
      <div className="p-4">
        <div className="space-y-1">
          {(channelMembers || []).map((m: any) => (
            <div key={m.id} className="group relative">
              <button type="button" onClick={() => onOpenMember(m.id)} data-od-id={'drawer-member-' + m.id}
                title={'Open chat with ' + m.name}
                className="flex w-full items-center gap-2.5 rounded-md px-2 py-2 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]">
                <Avatar name={m.name} kind={m.kind === 'agent' ? 'agent' : 'other'} size={28}/>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] font-medium text-fg">{m.name}</span>
                  <span className="block font-mono text-[10px] text-muted">
                    {m.kind === 'agent' ? 'Agent' : 'Member · ' + (m.presence === 'online' ? 'online' : 'away')}
                  </span>
                </span>
                {m.kind === 'agent' && m.agent && <StatusDot status={m.agent.status}/>}
              </button>
              {m.id !== primaryAgentId && (
                <button type="button" onClick={() => onRemoveMember(m.id)} data-od-id={'drawer-member-remove-' + m.id}
                  aria-label={'Remove ' + m.name + ' from channel'} title="Remove from channel"
                  className="absolute right-1.5 top-1/2 flex h-5 w-5 -translate-y-1/2 items-center justify-center rounded-[5px] text-muted opacity-0 transition-opacity hover:bg-[color-mix(in_oklab,var(--danger),12%,transparent)] hover:text-danger focus-visible:opacity-100 group-hover:opacity-100">
                  <Icon name="x" size={11}/>
                </button>
              )}
            </div>
          ))}
          {(channelMembers || []).length === 0 && <p className="px-2 py-1 text-[12px] text-muted">No members yet.</p>}
        </div>

        {adding ? (
          <div className="mt-3 rounded-md border border-line bg-surface p-2" data-od-id="add-member-list">
            <p className="px-1 pb-1.5 pt-0.5 font-mono text-[9px] font-semibold uppercase tracking-[0.14em] text-muted">Add to channel</p>
            <div className="od-scroll max-h-56 overflow-y-auto">
              {(candidates || []).map((m: any) => (
                <button key={m.id} type="button" onClick={() => onAddMember(m.id)} data-od-id={'add-member-' + m.id}
                  className="flex w-full items-center gap-2.5 rounded-md px-1.5 py-1.5 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]">
                  <Avatar name={m.name} kind={m.kind === 'agent' ? 'agent' : 'other'} size={22}/>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[12.5px] font-medium text-fg">{m.name}</span>
                    <span className="block font-mono text-[9px] text-muted">{m.kind === 'agent' ? 'Agent' : 'Member'}</span>
                  </span>
                  <Icon name="plus" size={12} className="text-muted"/>
                </button>
              ))}
              {(candidates || []).length === 0 && <p className="px-1.5 py-2 text-[12px] text-muted">Everyone here is already a member.</p>}
            </div>
            <button type="button" onClick={() => setAdding(false)}
              className="mt-1.5 flex h-7 w-full items-center justify-center rounded-md text-[12px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] hover:text-fg2">
              Done
            </button>
          </div>
        ) : (
          <button type="button" onClick={() => setAdding(true)} data-od-id="btn-add-member"
            className="mt-3 flex h-8 w-full items-center justify-center gap-1.5 rounded-md border border-dashed border-line text-[12px] font-medium text-muted transition-colors hover:border-accent hover:text-fg">
            <Icon name="plus" size={12} sw={2.2}/> Add member
          </button>
        )}

        <p className="mt-4 text-[12px] leading-5 text-muted">Type @ in the composer to mention anyone here — mentioned agents respond in the thread.</p>
      </div>
    </aside>
  );
}

