import { useState, useRef, useEffect } from 'react';
import { useAuth } from '../../store/auth';
import { useWorkspace } from '../../store';
import { Avatar } from '../ui/Avatar';
import { Icon } from '../ui/Icon';
import { Tooltip } from '../ui/Tooltip';
import { UserMemoryModal } from '../../modals/UserMemoryModal';
import { cx } from '../../lib/helpers';

interface UserMenuProps {
  onLogout?: () => void;
  expanded?: boolean;
}

export function UserMenu({ onLogout, expanded = false }: UserMenuProps) {
  const { user, logout } = useAuth();
  const [open, setOpen] = useState(false);
  const [memoryOpen, setMemoryOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const ws = useWorkspace();
  const wsSlug = ws?.sub || ws?.id || '';

  useEffect(() => {
    if (!open) return;
    const handleClickOutside = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };

    document.addEventListener('mousedown', handleClickOutside);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [open]);

  const handleLogout = async () => {
    setOpen(false);
    if (onLogout) {
      onLogout();
    } else {
      await logout();
    }
  };

  const displayName = user?.name ?? '';
  const displayEmail = user?.email || '';

  return (
    <div ref={menuRef} className={cx('relative mt-1', expanded && 'w-full')}>
      <Tooltip
        content={`User menu for ${displayName}`}
        placement="right"
        disabled={expanded}
      >
        <button
          type="button"
          onClick={() => setOpen(!open)}
          data-od-id="rail-user-chip"
          data-testid="rail-user-chip"
          aria-label={`User menu for ${displayName}`}
          aria-haspopup="menu"
          aria-expanded={open}
          className={cx(
            'flex items-center transition-transform hover:scale-[1.03] focus:outline-none focus:ring-2 focus:ring-accent',
            expanded
              ? 'h-9 w-[calc(100%-16px)] mx-2 px-1.5 gap-2 rounded-lg text-left hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)]'
              : 'h-9 w-9 justify-center rounded-full border border-line hover:scale-[1.05]'
          )}
        >
          <Avatar name={displayName} src={user?.avatar_url} kind="you" size={26} />
          {expanded && (
            <span className="min-w-0 flex-1 truncate text-[12px] font-medium text-fg">
              {displayName}
            </span>
          )}
        </button>
      </Tooltip>

      {open && (
        <div
          role="menu"
          data-od-id="user-menu-popover"
          className="od-pop absolute bottom-0 left-[calc(100%+10px)] z-50 min-w-[200px] max-w-[260px] rounded-lg border border-line bg-surface p-2 shadow-[var(--elev-raised)]"
        >
          <div className="px-2 py-1.5 border-b border-linesoft mb-1">
            <p className="text-[13px] font-semibold text-fg truncate">{displayName}</p>
            {displayEmail && (
              <p className="text-[11px] text-muted truncate mt-0.5">{displayEmail}</p>
            )}
          </div>

          {wsSlug && (
            <button
              type="button"
              role="menuitem"
              data-od-id="user-memory-btn"
              data-testid="user-memory-btn"
              onClick={() => {
                setOpen(false);
                setMemoryOpen(true);
              }}
              className="mb-0.5 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-[13px] text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
            >
              <Icon name="memory" size={15} />
              <span>My memory</span>
            </button>
          )}

          <button
            type="button"
            role="menuitem"
            data-od-id="logout-btn"
            onClick={handleLogout}
            className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-[13px] text-danger transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_8%,transparent)]"
          >
            <Icon name="logout" size={15} />
            <span>Log out</span>
          </button>
        </div>
      )}

      {memoryOpen && wsSlug && (
        <UserMemoryModal wsSlug={wsSlug} onClose={() => setMemoryOpen(false)} />
      )}
    </div>
  );
}
