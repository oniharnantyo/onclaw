import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Tooltip } from "../ui/Tooltip";
import { ThemeCycleButton } from "../ui/ThemeCycleButton";
import { UserMenu } from "./UserMenu";

export function Rail({
  view,
  onNav,
  tenant,
  unread,
  onOpenSwitcher,
  onSettings,
  onMenuToggle,
  onLogout,
  showAdmin,
  expanded = false,
  onToggleExpand,
}: any) {
  const items = [
    { id: 'chats', icon: 'chat', label: 'Chats', badge: unread },
    { id: 'agents', icon: 'bot', label: 'Agents' },
    { id: 'schedules', icon: 'clock', label: 'Schedules' },
    { id: 'runs', icon: 'activity', label: 'Runs' },
    ...(showAdmin
      ? [
          { id: 'admin-workspaces', icon: 'globe', label: 'Workspaces' },
          { id: 'admin-accounts', icon: 'users', label: 'Accounts' },
        ]
      : []),
  ];

  const tenantInitials = (tenant?.name || '')
    .split(' ')
    .map((w: string) => w[0])
    .join('');

  return (
    <nav
      data-od-id="rail"
      aria-label="Primary"
      className={cx(
        'relative z-30 flex shrink-0 flex-col items-center gap-1 border-r border-linesoft bg-[color-mix(in_oklab,var(--fg)_6%,var(--bg))] py-3 transition-[width] duration-200 ease-[var(--ease-standard)]',
        expanded ? 'w-[200px]' : 'w-[68px]'
      )}
    >
      <Tooltip
        content={expanded ? 'Collapse rail' : 'Expand rail'}
        placement="right"
        disabled={expanded}
      >
        <button
          type="button"
          onClick={onToggleExpand}
          data-od-id="rail-toggle"
          data-testid="rail-toggle"
          aria-label={expanded ? 'Collapse rail' : 'Expand rail'}
          className="hidden md:flex h-11 w-11 self-start ml-3 mb-0.5 items-center justify-center rounded-[12px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
        >
          <Icon name={expanded ? 'panelclose' : 'panelopen'} size={19} sw={1.7} />
        </button>
      </Tooltip>

      <Tooltip
        content={'Switch workspace — ' + (tenant?.name || '')}
        placement="right"
        disabled={expanded}
      >
        <button
          type="button"
          onClick={onOpenSwitcher}
          data-od-id="ws-switcher"
          data-testid="ws-switcher"
          aria-label={'Switch workspace, current: ' + (tenant?.name || '')}
          className={cx(
            'mb-2 flex items-center rounded-[12px] bg-accent text-accenton transition-all hover:scale-[1.02]',
            expanded
              ? 'h-11 w-[calc(100%-16px)] mx-2 px-2.5 gap-2.5 text-left'
              : 'h-11 w-11 justify-center text-[13px] font-bold hover:scale-[1.04]'
          )}
        >
          <span
            className={cx(
              'flex shrink-0 items-center justify-center font-bold',
              expanded
                ? 'h-7 w-7 rounded-[7px] bg-[color-mix(in_oklab,var(--fg)_15%,transparent)] text-[11px]'
                : 'text-[13px]'
            )}
          >
            {tenantInitials}
          </span>
          {expanded && (
            <span className="min-w-0 flex-1 truncate text-[13px] font-semibold">
              {tenant?.name}
            </span>
          )}
        </button>
      </Tooltip>

      {items.map((it: any) => {
        const isActive = view === it.id;
        return (
          <Tooltip
            key={it.id}
            content={it.label}
            placement="right"
            disabled={expanded}
          >
            <button
              type="button"
              onClick={() => onNav(it.id)}
              data-od-id={'rail-' + it.id}
              data-testid={'rail-' + it.id}
              aria-label={it.label}
              className={cx(
                'relative flex h-11 items-center rounded-[12px] transition-colors',
                expanded
                  ? 'w-[calc(100%-16px)] mx-2 px-3 gap-3 text-left'
                  : 'w-11 justify-center',
                isActive
                  ? 'bg-[color-mix(in_oklab,var(--accent)_16%,transparent)] text-accenttext font-semibold'
                  : 'text-muted hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2'
              )}
            >
              <Icon name={it.icon} size={19} sw={1.7} className="shrink-0" />
              {expanded && (
                <span className="min-w-0 flex-1 truncate text-[13px]">
                  {it.label}
                </span>
              )}
              {it.badge > 0 && (
                <span
                  className={cx(
                    'flex h-4 min-w-4 items-center justify-center rounded-full bg-accent px-1 text-[9px] font-bold text-accenton',
                    expanded ? 'ml-auto' : 'absolute right-1.5 top-1.5'
                  )}
                >
                  {it.badge}
                </span>
              )}
            </button>
          </Tooltip>
        );
      })}

      <div className="flex-1" />

      <Tooltip content="Menu" placement="right" disabled={expanded}>
        <button
          type="button"
          onClick={onMenuToggle}
          aria-label="Menu"
          className="mb-1 flex h-11 w-11 items-center justify-center rounded-[12px] md:hidden text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
        >
          <Icon name="menu" size={20} />
        </button>
      </Tooltip>

      <ThemeCycleButton variant="rail" expanded={expanded} testId="rail-theme" odId="rail-theme" />

      <Tooltip content="Workspace settings" placement="right" disabled={expanded}>
        <button
          type="button"
          onClick={onSettings}
          data-od-id="rail-settings"
          data-testid="rail-settings"
          aria-label="Workspace settings"
          className={cx(
            'flex h-11 items-center rounded-[12px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2',
            expanded
              ? 'w-[calc(100%-16px)] mx-2 px-3 gap-3 text-left'
              : 'w-11 justify-center'
          )}
        >
          <Icon name="sliders" size={19} sw={1.7} className="shrink-0" />
          {expanded && (
            <span className="min-w-0 flex-1 truncate text-[13px] font-medium">
              Settings
            </span>
          )}
        </button>
      </Tooltip>

      <UserMenu onLogout={onLogout} expanded={expanded} />
    </nav>
  );
}


