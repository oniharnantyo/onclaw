import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { Rail } from './Rail';
import { THEME_STORAGE_KEY } from '../../lib/theme';

// ThemeCycleButton (mounted in the rail) reads localStorage and applyTheme
// touches window.matchMedia on click — the vitest jsdom env has neither, so
// the theme-control tests stub both (unstubbed in afterEach).
function stubLocalStorage(initial: Record<string, string> = {}) {
  const map = new Map(Object.entries(initial));
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => (map.has(key) ? map.get(key)! : null),
    setItem: (key: string, value: string) => {
      map.set(key, String(value));
    },
    removeItem: (key: string) => {
      map.delete(key);
    },
  });
}

const stubMatchMedia = (matches: boolean) =>
  vi.stubGlobal('matchMedia', () => ({
    matches,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
  }));

describe('components/nav/Rail', () => {
  const mockTenant = {
    id: 'acme',
    name: 'Acme Corp',
    plan: 'Pro',
    agents: [{ id: 'a1', name: 'Atlas' }],
    channels: [{ id: 'general', name: 'general', unread: 3 }],
    people: [],
    schedules: [],
    runs: [],
  };

  const defaultProps = {
    view: 'chats',
    onNav: vi.fn(),
    tenant: mockTenant,
    unread: 3,
    onOpenSwitcher: vi.fn(),
    onSettings: vi.fn(),
    onMenuToggle: vi.fn(),
    onLogout: vi.fn(),
    showAdmin: false,
    expanded: false,
    onToggleExpand: vi.fn(),
  };

  it('renders primary nav items (Chats, Agents, Schedules, Runs) without native title attrs', () => {
    render(<Rail {...defaultProps} />);

    const chatsBtn = screen.getByRole('button', { name: 'Chats' });
    const agentsBtn = screen.getByRole('button', { name: 'Agents' });
    const schedulesBtn = screen.getByRole('button', { name: 'Schedules' });
    const runsBtn = screen.getByRole('button', { name: 'Runs' });

    expect(chatsBtn).not.toBeNull();
    expect(agentsBtn).not.toBeNull();
    expect(schedulesBtn).not.toBeNull();
    expect(runsBtn).not.toBeNull();

    // Ensure native title attribute is removed to avoid duplicate tooltips
    expect(chatsBtn.getAttribute('title')).toBeNull();
    expect(agentsBtn.getAttribute('title')).toBeNull();
    expect(schedulesBtn.getAttribute('title')).toBeNull();
    expect(runsBtn.getAttribute('title')).toBeNull();

    // Switcher and Settings also have no native title
    expect(screen.getByTestId('ws-switcher').getAttribute('title')).toBeNull();
    expect(screen.getByTestId('rail-settings').getAttribute('title')).toBeNull();
  });

  it('hides Workspaces and Accounts items when showAdmin is false', () => {
    render(<Rail {...defaultProps} showAdmin={false} />);

    expect(screen.queryByRole('button', { name: 'Workspaces' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Accounts' })).toBeNull();
  });

  it('renders Workspaces and Accounts items when showAdmin is true', () => {
    render(<Rail {...defaultProps} showAdmin={true} />);

    expect(screen.getByRole('button', { name: 'Workspaces' })).not.toBeNull();
    expect(screen.getByRole('button', { name: 'Accounts' })).not.toBeNull();
  });

  it('calls onNav with item id on click', () => {
    const onNav = vi.fn();
    render(<Rail {...defaultProps} onNav={onNav} showAdmin={true} />);

    fireEvent.click(screen.getByRole('button', { name: 'Agents' }));
    expect(onNav).toHaveBeenCalledWith('agents');

    fireEvent.click(screen.getByRole('button', { name: 'Workspaces' }));
    expect(onNav).toHaveBeenCalledWith('admin-workspaces');

    fireEvent.click(screen.getByRole('button', { name: 'Accounts' }));
    expect(onNav).toHaveBeenCalledWith('admin-accounts');
  });

  it('displays unread badge in collapsed mode', () => {
    render(<Rail {...defaultProps} unread={5} />);
    expect(screen.getByText('5')).not.toBeNull();
  });

  it('highlights nothing in the rail when view is empty (e.g. /welcome route)', () => {
    const { container } = render(<Rail {...defaultProps} view="" showAdmin={true} />);

    // None of the nav buttons should have the active background color
    const navButtons = container.querySelectorAll('button[data-od-id^="rail-"]');
    navButtons.forEach((btn) => {
      expect(btn.className).not.toContain('text-accent font-semibold');
    });
  });

  it('highlights specific admin items according to view', () => {
    const { rerender } = render(<Rail {...defaultProps} view="admin-workspaces" showAdmin={true} />);
    expect(screen.getByTestId('rail-admin-workspaces').className).toContain('text-accent');
    expect(screen.getByTestId('rail-admin-accounts').className).not.toContain('text-accent font-semibold');

    rerender(<Rail {...defaultProps} view="admin-accounts" showAdmin={true} />);
    expect(screen.getByTestId('rail-admin-accounts').className).toContain('text-accent');
    expect(screen.getByTestId('rail-admin-workspaces').className).not.toContain('text-accent font-semibold');
  });

  it('shows tooltip on hover in collapsed mode', () => {
    vi.useFakeTimers();
    render(<Rail {...defaultProps} />);

    const agentsBtn = screen.getByRole('button', { name: 'Agents' });
    fireEvent.mouseEnter(agentsBtn);

    act(() => {
      vi.advanceTimersByTime(200);
    });

    const tooltip = screen.getByRole('tooltip');
    expect(tooltip).not.toBeNull();
    expect(tooltip.textContent).toBe('Agents');

    fireEvent.mouseLeave(agentsBtn);
    expect(screen.queryByRole('tooltip')).toBeNull();
    vi.useRealTimers();
  });

  it('renders expanded rail with visible labels, tenant name, and inline badge', () => {
    render(<Rail {...defaultProps} expanded={true} showAdmin={true} unread={4} />);

    // Rail container should be w-[200px]
    const nav = screen.getByRole('navigation', { name: /primary/i });
    expect(nav.className).toContain('w-[200px]');

    // Workspace name is visible
    expect(screen.getByText('Acme Corp')).not.toBeNull();

    // All labels are rendered as visible text
    expect(screen.getByText('Chats')).not.toBeNull();
    expect(screen.getByText('Agents')).not.toBeNull();
    expect(screen.getByText('Schedules')).not.toBeNull();
    expect(screen.getByText('Runs')).not.toBeNull();
    expect(screen.getByText('Workspaces')).not.toBeNull();
    expect(screen.getByText('Accounts')).not.toBeNull();
    expect(screen.getByText('Settings')).not.toBeNull();
    expect(screen.getByRole('button', { name: 'Collapse rail' })).not.toBeNull();

    // Unread badge is present inline
    const badge = screen.getByText('4');
    expect(badge.className).toContain('ml-auto');
  });

  it('renders expand/collapse toggle hidden on mobile (hidden md:flex)', () => {
    const onToggleExpand = vi.fn();
    render(<Rail {...defaultProps} onToggleExpand={onToggleExpand} />);

    const toggle = screen.getByRole('button', { name: 'Expand rail' });
    expect(toggle.className).toContain('hidden');
    expect(toggle.className).toContain('md:flex');

    fireEvent.click(toggle);
    expect(onToggleExpand).toHaveBeenCalledTimes(1);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders the theme cycle control icon-only in the collapsed rail', () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: 'light' });
    stubMatchMedia(false);
    render(<Rail {...defaultProps} />);

    const themeBtn = screen.getByTestId('rail-theme');
    expect(themeBtn.getAttribute('aria-label')).toBe('Theme: light (click for dark)');

    // Icon-only: no visible label text in the collapsed rail
    expect(screen.queryByText('Theme: light')).toBeNull();

    // Settings-row shape, not the expanded row shape
    expect(themeBtn.className).toContain('w-11');
    expect(themeBtn.className).not.toContain('w-[calc(100%-16px)]');
  });

  it('renders the theme cycle control as a labeled row in the expanded rail, above Settings', () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: 'system' });
    stubMatchMedia(true);
    render(<Rail {...defaultProps} expanded={true} />);

    const themeBtn = screen.getByTestId('rail-theme');
    const settingsBtn = screen.getByTestId('rail-settings');

    // Visible `Theme: <mode>` label in the Settings-row shape
    expect(screen.getByText('Theme: system')).not.toBeNull();
    expect(themeBtn.className).toContain('w-[calc(100%-16px)]');
    expect(themeBtn.className).toContain('text-left');

    // Positioned directly above the Settings row
    expect(
      themeBtn.compareDocumentPosition(settingsBtn) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
  });

  it('cycles the theme preference when the rail control is clicked', () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: 'light' });
    stubMatchMedia(false);
    render(<Rail {...defaultProps} />);

    const themeBtn = screen.getByTestId('rail-theme');
    fireEvent.click(themeBtn);

    expect(themeBtn.getAttribute('aria-label')).toBe('Theme: dark (click for system)');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
    expect(document.documentElement.dataset.theme).toBe('dark');
  });
});
