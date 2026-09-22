import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";
import { Chip } from "../ui/Chip";
import { STATUS } from "../../lib/constants";

// The context meter moved to the composer's left rail (adopt-assistant-ui-
// elements D1) — the header renders no meter; the ring + breakdown popover
// live in components/chat/ContextRing.tsx.
//
// The top-right slot belongs to the right-panel toggle (add-right-panel D1).
// In direct agent chats it REPLACED the configure button — agent
// configuration stays reachable from the Agents screen, never here. In
// channels the members avatar stack opens the panel's members tab; the dot
// badge on the toggle marks a panel-able tool that finished while the panel
// was closed.

export function ChatHeader({ target, agent, channelMembers, langfuseUrl, onOpenMembers, panelOpen, panelBadge, onTogglePanel  }: any) {
  const t = target;
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
        {/* Observability deep link (integrate-langfuse-tracing D6): rendered
            only when the opened run carries a langfuse_url — untraced runs
            (tracing unconfigured or pre-dating export) see no action. */}
        {langfuseUrl && (
          <a href={langfuseUrl} target="_blank" rel="noopener" data-od-id="btn-open-langfuse"
            title="Open in Langfuse" aria-label="Open in Langfuse"
            className="flex h-8 w-8 items-center justify-center rounded-md text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg2">
            <Icon name="external-link" size={16}/>
          </a>
        )}
        {channelMembers && channelMembers.length > 0 && (
          <button type="button" onClick={onOpenMembers} data-od-id="btn-channel-members"
            title="Open members panel"
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
        <button type="button" onClick={onTogglePanel} data-od-id="btn-panel-toggle"
          title={panelOpen ? 'Hide panel' : 'Show panel'} aria-pressed={Boolean(panelOpen)}
          className="relative flex h-8 w-8 items-center justify-center rounded-md text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg2">
          <Icon name={panelOpen ? 'panelclose' : 'panelopen'} size={16}/>
          {panelBadge && !panelOpen && (
            <span data-od-id="panel-badge" title="New result in the panel"
              className="absolute right-1 top-1 h-2 w-2 rounded-full bg-[var(--accent)] ring-2 ring-[var(--bg)]"/>
          )}
        </button>
      </div>
    </header>
  );
}
