import { useState, Fragment } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";
import { SideRow } from "../ui/SideRow";
import { SectionLabel } from "../ui/SectionLabel";
import { STATUS } from "../../lib/constants";

export function Sidebar({ view, tenant, chatId, onSelect, onDeploy, onNewSchedule, onEditCron, onOpenSwitcher, search, setSearch,
  activeIsAgent, session, sessions, onSwitchSession, onNewSession, onDeleteSession  }: any) {
  const [showAllSessions, setShowAllSessions] = useState(false);
  const [prevChatId, setPrevChatId] = useState(chatId);
  if (chatId !== prevChatId) { setPrevChatId(chatId); setShowAllSessions(false); }
  const q = search.trim().toLowerCase();
  const match = (s) => !q || s.toLowerCase().includes(q);
  const agents = tenant.agents.filter((a: any) => match(a.name) || match(a.role));
  const channels = tenant.channels.filter((c: any) => match(c.name) || match(c.purpose));
  const people = tenant.people.filter((p: any) => match(p.name));
  const nextUp = tenant.cron.filter((c: any) => c.enabled).slice(0, 4);
  const today = tenant.runs;
  const okCount = today.filter((r: any) => r.status === 'success').length;
  const failCount = today.filter((r: any) => r.status === 'failed').length;
  const agentRowIcon = (a) => (
    <span className="relative inline-flex shrink-0 items-center justify-center">
      <Avatar name={a.name} kind="agent" size={18}/>
      <span title={(STATUS as any)[a.status].label}
        className={cx('absolute -bottom-0.5 -right-0.5 h-2 w-2 rounded-full ring-2 ring-[var(--bg)]', (STATUS as any)[a.status].dot, a.status === 'running' && 'od-live')}/>
    </span>
  );

  return (
    <aside data-od-id="sidebar" aria-label="Conversation list"
      className="flex w-[264px] shrink-0 flex-col border-r border-linesoft bg-bg">
      <button type="button" onClick={onOpenSwitcher} data-od-id="ws-header" title="Switch workspace" aria-haspopup="menu"
        className="mx-3 mt-3 flex h-11 items-center gap-2.5 rounded-md px-1.5 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)]">
        <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[7px] bg-accent text-[11px] font-bold text-accenton">
          {tenant.name.split(' ').map((w: any) => w[0]).join('')}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[13px] font-semibold text-fg">{tenant.name}</span>
          <span className="block truncate font-mono text-[10px] text-muted">{tenant.plan} · {tenant.agents.length} agents</span>
        </span>
        <Icon name="chevdown" size={14} className="shrink-0 text-muted"/>
      </button>
      <div className="px-3 pt-2">
        <label className="relative block">
          <span className="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted"><Icon name="search" size={14}/></span>
          <input id="od-search" value={search} onChange={(e) => setSearch(e.target.value)} data-od-id="sidebar-search"
            placeholder="Search OnClaw  ⌘K" aria-label="Search agents, channels and people"
            className="h-8 w-full rounded-md border border-line bg-surface pl-8 pr-2.5 text-[13px] text-fg2 placeholder:text-muted focus:border-accent"/>
        </label>
      </div>

      {(view === 'chats' || view === 'agents') && (
        <div className="od-scroll mt-1 flex-1 overflow-y-auto pb-4">
          <SectionLabel action={
            <button type="button" onClick={onDeploy} data-od-id="sidebar-deploy" title="Deploy a new agent"
              className="flex h-5 w-5 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg"
              aria-label="Deploy a new agent">
              <Icon name="plus" size={13}/>
            </button>
          }>Agents</SectionLabel>
          <div className="px-1.5">
            {agents.map((a: any) => (
              <Fragment key={a.id}>
                <SideRow odId={'side-agent-' + a.id} active={chatId === a.id} onClick={() => onSelect(a.id)}
                  icon={agentRowIcon(a)} label={a.name}
                  sub={view === 'agents' ? <span className="font-mono text-[10px] text-muted">{a.model.replace('claude-', '').replace('llama-', '')}</span> : null}/>
                {view === 'chats' && activeIsAgent && chatId === a.id && (() => {
                  const list = sessions || [];
                  const activeIdx = list.findIndex((s: any) => session && s.id === session.id);
                  const capped = !showAllSessions && list.length > 4;
                  let visible = list;
                  if (capped) {
                    visible = activeIdx >= 4 ? list.slice(0, 3).concat([list[activeIdx]]) : list.slice(0, 4);
                  }
                  const olderCount = Math.max(0, list.length - 4);
                  return (
                    <div className="ml-[30px] mb-1 border-l border-line pl-1.5" data-od-id="agent-sessions">
                      <div className={cx(showAllSessions && list.length > 8 && 'od-scroll max-h-56 overflow-y-auto')}>
                        {visible.map((s: any) => {
                          const activeS = session && s.id === session.id;
                          return (
                            <div key={s.id} className="group relative">
                              <button type="button" onClick={() => onSwitchSession(s.id)} data-od-id={'sidebar-session-' + s.id}
                                title={s.title}
                                className={cx('flex h-[26px] w-full items-center rounded-md px-2 pr-7 text-left transition-colors',
                                  activeS ? 'bg-[color-mix(in_oklab,var(--accent)_12%,transparent)] text-fg' : 'text-muted hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] hover:text-fg2')}>
                                <span className="min-w-0 flex-1 truncate text-[12.5px] font-medium">{s.title}</span>
                              </button>
                              <button type="button" onClick={() => onDeleteSession(s.id)} data-od-id={'sidebar-session-del-' + s.id}
                                aria-label={'Delete ' + s.title} title="Delete session"
                                className="absolute right-1 top-1/2 flex h-5 w-5 -translate-y-1/2 items-center justify-center rounded-[5px] text-muted opacity-0 transition-opacity hover:bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] hover:text-danger focus-visible:opacity-100 group-hover:opacity-100">
                                <Icon name="x" size={11}/>
                              </button>
                            </div>
                          );
                        })}
                      </div>
                      {capped && olderCount > 0 && (
                        <button type="button" onClick={() => setShowAllSessions(true)} data-od-id="sidebar-sessions-expand"
                          className="flex h-[26px] w-full items-center gap-1.5 rounded-md px-2 text-[12px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] hover:text-fg2">
                          <Icon name="chevdown" size={11}/> Show {olderCount} older sessions
                        </button>
                      )}
                      {!capped && list.length > 4 && (
                        <button type="button" onClick={() => setShowAllSessions(false)} data-od-id="sidebar-sessions-collapse"
                          className="flex h-[26px] w-full items-center gap-1.5 rounded-md px-2 text-[12px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] hover:text-fg2">
                          <Icon name="up" size={11}/> Show recent only
                        </button>
                      )}
                      <button type="button" onClick={onNewSession} data-od-id="sidebar-new-session" title="Start a new chat"
                        className="flex h-[26px] w-full items-center gap-1.5 rounded-md px-2 text-[12px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] hover:text-fg2">
                        <Icon name="plus" size={11} sw={2.2}/> New chat
                      </button>
                    </div>
                  );
                })()}
              </Fragment>
            ))}
            {agents.length === 0 && (
              <p className="px-2.5 py-1.5 text-[12px] text-muted">{q ? 'No agents match “' + search + '”.' : 'No agents yet — deploy one from the Agents view.'}</p>
            )}
          </div>

          {view === 'chats' && (
            <>
              <SectionLabel>Channels</SectionLabel>
              <div className="px-1.5">
                {channels.map((c: any) => (
                  <SideRow key={c.id} odId={'side-channel-' + c.id} active={chatId === c.id} onClick={() => onSelect(c.id)}
                    icon={<Icon name="hash" size={13} className="text-muted"/>} label={c.name}
                    right={c.unread > 0 ? (
                      <span className="flex h-[17px] min-w-[17px] items-center justify-center rounded-full bg-accent px-1 text-[10px] font-bold text-accenton">{c.unread}</span>
                    ) : null}/>
                ))}
              </div>
              {channels.length === 0 && <p className="px-2.5 py-1.5 text-[12px] text-muted">No channels in this workspace yet.</p>}
              <SectionLabel>Team</SectionLabel>
              <div className="px-1.5">
                {people.map((p: any) => (
                  <SideRow key={p.id} odId={'side-person-' + p.id} active={chatId === p.id} onClick={() => onSelect(p.id)}
                    icon={(
                      <span className="relative inline-flex shrink-0 items-center justify-center">
                        <Avatar name={p.name} size={18}/>
                        <span title={p.presence === 'online' ? 'Online' : 'Away'}
                          className={cx('absolute -bottom-0.5 -right-0.5 h-2 w-2 rounded-full ring-2 ring-[var(--bg)]', p.presence === 'online' ? 'bg-success' : 'bg-muted')}/>
                      </span>
                    )}
                    label={p.name}/>
                ))}
                {people.length === 0 && <p className="px-2.5 py-1.5 text-[12px] text-muted">Invite teammates in Settings → Members.</p>}
              </div>
            </>
          )}
        </div>
      )}

      {view === 'cron' && (
        <div className="od-scroll flex-1 overflow-y-auto pb-4">
          <SectionLabel action={
            <button type="button" onClick={onNewSchedule} data-od-id="sidebar-new-schedule" title="New schedule"
              className="flex h-5 w-5 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg"
              aria-label="New schedule">
              <Icon name="plus" size={13}/>
            </button>
          }>Next up</SectionLabel>
          <div className="px-1.5">
            {nextUp.map((j: any) => (
              <SideRow key={j.id} odId={'side-cron-' + j.id} active={false} onClick={() => onEditCron(j)}
                icon={<Icon name="clock" size={13} className="text-muted"/>} label={j.name}
                sub={<span className="font-mono text-[10px] text-muted">{j.next}</span>}/>
            ))}
          </div>
          <SectionLabel>Schedules</SectionLabel>
          <p className="px-3.5 text-[12px] leading-5 text-muted">
            {tenant.cron.filter((c: any) => c.enabled).length} active · {tenant.cron.filter((c: any) => !c.enabled).length} paused.
            Open the Cron view to edit expressions, agents and history.
          </p>
        </div>
      )}

      {view === 'runs' && (
        <div className="od-scroll flex-1 overflow-y-auto pb-4">
          <SectionLabel>Latest window</SectionLabel>
          <div className="px-3.5">
            <div className="rounded-md border border-line bg-surface p-3">
              <div className="flex items-baseline justify-between">
                <span className="font-mono text-[22px] font-medium text-fg">{okCount + failCount}</span>
                <span className="font-mono text-[10px] uppercase tracking-wider text-muted">runs logged</span>
              </div>
              <div className="mt-2.5 flex h-1.5 overflow-hidden rounded-full bg-[color-mix(in_oklab,var(--fg)_10%,transparent)]">
                <div className="bg-success" style={{ width: (okCount / Math.max(1, okCount + failCount)) * 100 + '%' }}/>
                <div className="bg-danger" style={{ width: (failCount / Math.max(1, okCount + failCount)) * 100 + '%' }}/>
              </div>
              <p className="mt-2.5 text-[12px] leading-5 text-muted">
                <span className="text-fg2">{okCount} succeeded</span> · <span className="text-fg2">{failCount} failed</span>
              </p>
            </div>
          </div>
        </div>
      )}
    </aside>
  );
}

