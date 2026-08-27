// @ts-nocheck
import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";

export function Rail({ view, onNav, tenant, unread, onOpenSwitcher, onSettings }) {
  const items = [
    { id: 'chats', icon: 'chat', label: 'Chats', badge: unread },
    { id: 'agents', icon: 'bot', label: 'Agents' },
    { id: 'cron', icon: 'clock', label: 'Cron' },
    { id: 'runs', icon: 'activity', label: 'Runs' }
  ];
  return (
    <nav data-od-id="rail" aria-label="Primary"
      className="relative z-30 flex w-[68px] shrink-0 flex-col items-center gap-1 border-r border-linesoft bg-[color-mix(in_oklab,var(--fg)_6%,var(--bg))] py-3">
      <button type="button" onClick={onOpenSwitcher} data-od-id="ws-switcher" aria-label={'Switch workspace, current: ' + tenant.name}
        title={'Switch workspace — ' + tenant.name}
        className="mb-2 flex h-11 w-11 items-center justify-center rounded-[12px] bg-accent text-[13px] font-bold text-accenton transition-transform hover:scale-[1.04]">
        {tenant.name.split(' ').map((w) => w[0]).join('')}
      </button>
      {items.map((it) => (
        <button key={it.id} type="button" onClick={() => onNav(it.id)} data-od-id={'rail-' + it.id} title={it.label} aria-label={it.label}
          className={cx('relative flex h-11 w-11 items-center justify-center rounded-[12px] transition-colors',
            view === it.id
              ? 'bg-[color-mix(in_oklab,var(--accent)_16%,transparent)] text-accent'
              : 'text-muted hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2')}>
          <Icon name={it.icon} size={19} sw={1.7}/>
          {it.badge > 0 && (
            <span className="absolute right-1.5 top-1.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-accent px-1 text-[9px] font-bold text-accenton">{it.badge}</span>
          )}
        </button>
      ))}
      <div className="flex-1"/>
      <button type="button" onClick={onSettings} data-od-id="rail-settings" title="Workspace settings" aria-label="Workspace settings"
        className="flex h-11 w-11 items-center justify-center rounded-[12px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2">
        <Icon name="sliders" size={19} sw={1.7}/>
      </button>
      <div className="mt-1 flex h-9 w-9 items-center justify-center rounded-full border border-line" title="You">
        <Avatar name="You" kind="you" size={26}/>
      </div>
    </nav>
  );
}

