import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { Sidebar } from './Sidebar';
import { useStore } from '../../store';

describe('components/nav/Sidebar', () => {
  const mockTenant = {
    id: 'acme',
    name: 'Acme Corp',
    plan: 'Pro',
    agents: [
      { id: 'a1', name: 'Atlas', status: 'idle', model: 'claude-3-5-sonnet' },
      { id: 'a2', name: 'Beacon', status: 'running', model: 'llama-3-70b' },
    ],
    channels: [
      { id: 'c1', name: 'general', unread: 2, purpose: 'All team discussion' },
      { id: 'c2', name: 'dev', unread: 0, purpose: 'Dev chat' },
    ],
    people: [
      { id: 'p1', name: 'Alice Smith', presence: 'online' },
      { id: 'p2', name: 'Bob Jones', presence: 'offline' },
    ],
    schedules: [
      { id: 'sched1', name: 'Daily Digest', expr: '0 9 * * 1-5', next_run_at: '2026-09-14T16:00:00Z', enabled: true },
    ],
    runs: [
      { id: 'r1', status: 'completed' },
      { id: 'r2', status: 'failed' },
    ],
  };

  const defaultProps = {
    view: 'chats',
    tenant: mockTenant,
    chatId: 'a1',
    onSelect: vi.fn(),
    onDeploy: vi.fn(),
    onNewSchedule: vi.fn(),
    onEditSchedule: vi.fn(),
    onOpenSwitcher: vi.fn(),
    search: '',
    setSearch: vi.fn(),
    activeIsAgent: true,
    session: null,
    sessions: [],
    onSwitchSession: vi.fn(),
    onNewSession: vi.fn(),
    onDeleteSession: vi.fn(),
  };

  it('renders agents, channels, and people in chats view', () => {
    render(<Sidebar {...defaultProps} />);

    expect(screen.getByText('Atlas')).not.toBeNull();
    expect(screen.getByText('Beacon')).not.toBeNull();
    expect(screen.getByText('general')).not.toBeNull();
    expect(screen.getByText('dev')).not.toBeNull();
    expect(screen.getByText('Alice Smith')).not.toBeNull();
    expect(screen.getByText('Bob Jones')).not.toBeNull();
  });

  it('does NOT render any Instance Admin button in the sidebar footer', () => {
    render(<Sidebar {...defaultProps} />);

    expect(screen.queryByText(/instance admin/i)).toBeNull();
    expect(screen.queryByTestId('sidebar-admin')).toBeNull();
  });

  it('filters agents and channels based on search input', () => {
    const setSearch = vi.fn();
    render(<Sidebar {...defaultProps} search="Atlas" setSearch={setSearch} />);

    expect(screen.getByText('Atlas')).not.toBeNull();
    expect(screen.queryByText('Beacon')).toBeNull();
  });

  it('calls onSelect when an agent or channel is clicked', () => {
    const onSelect = vi.fn();
    render(<Sidebar {...defaultProps} onSelect={onSelect} />);

    fireEvent.click(screen.getByText('Beacon'));
    expect(onSelect).toHaveBeenCalledWith('a2');

    fireEvent.click(screen.getByText('general'));
    expect(onSelect).toHaveBeenCalledWith('c1');
  });

  it('renders agent avatar when configured and initials fallback when empty', () => {
    const tenantWithAvatars = {
      ...mockTenant,
      agents: [
        {
          id: 'a1',
          name: 'Atlas',
          status: 'idle',
          model: 'claude-3-5-sonnet',
          avatar: { sex: 'man', faceColor: '#F9C9B6', earSize: 'small' },
        },
        {
          id: 'a2',
          name: 'Beacon',
          status: 'running',
          model: 'llama-3-70b',
          avatar: {},
        },
      ],
    };

    const { container } = render(<Sidebar {...defaultProps} tenant={tenantWithAvatars} />);

    // Atlas has avatar configured -> renders NiceAvatar container inside its SideRow
    const atlasRow = container.querySelector('[data-od-id="side-agent-a1"]');
    expect(atlasRow).not.toBeNull();
    expect(atlasRow?.querySelector('svg, div[style*="border-radius"]')).not.toBeNull();

    // Beacon has empty avatar -> renders initials fallback "BE"
    const beaconRow = container.querySelector('[data-od-id="side-agent-a2"]');
    expect(beaconRow).not.toBeNull();
    expect(beaconRow?.textContent).toContain('BE');
  });
});

describe('components/nav/Sidebar session running indicator', () => {
  const sessionList = [
    { id: 's1', title: 'First session' },
    { id: 's2', title: 'Second session' },
  ];

  const title = (container: HTMLElement, id: string): Element | null =>
    container.querySelector('[data-od-id="sidebar-session-title-' + id + '"]');

  const spinner = (container: HTMLElement, id: string): Element | null =>
    container.querySelector('[data-od-id="sidebar-session-spinner-' + id + '"]');

  const defaultProps = {
    view: 'chats',
    tenant: {
      id: 'acme',
      name: 'Acme Corp',
      agents: [{ id: 'a1', name: 'Atlas', status: 'idle', model: 'claude-3-5-sonnet' }],
      channels: [],
      people: [],
      schedules: [],
      runs: [],
    },
    chatId: 'a1',
    onSelect: vi.fn(),
    onDeploy: vi.fn(),
    onNewSchedule: vi.fn(),
    onEditSchedule: vi.fn(),
    onOpenSwitcher: vi.fn(),
    search: '',
    setSearch: vi.fn(),
    activeIsAgent: true,
    session: null,
    sessions: [],
    onSwitchSession: vi.fn(),
    onNewSession: vi.fn(),
    onDeleteSession: vi.fn(),
  };

  it('renders idle session rows with no running marker at all', () => {
    const { container } = render(
      <Sidebar {...defaultProps} sessions={sessionList} session={sessionList[0]} />
    );

    for (const s of sessionList) {
      const t = title(container, s.id);
      expect(t).not.toBeNull();
      expect(t?.className).not.toContain('animate-pulse');
    }
    // The dot-based indicator is gone — no dot or spinner elements anywhere.
    expect(container.querySelector('[data-od-id^="sidebar-session-dot-"]')).toBeNull();
    expect(container.querySelector('[data-od-id^="sidebar-session-spinner-"]')).toBeNull();
  });

  it('pulses the title when an entry carries running: true', () => {
    const list = [
      { id: 's1', title: 'First session', running: true },
      { id: 's2', title: 'Second session' },
    ];
    const { container } = render(
      <Sidebar {...defaultProps} sessions={list} session={null} uiRunning={false} />
    );

    // Foreign run (server flag) pulses even though this tab never started it.
    expect(title(container, 's1')?.className).toContain('animate-pulse');
    expect(title(container, 's2')?.className).not.toContain('animate-pulse');
  });

  it('shows a leading spinner when an entry carries running: true', () => {
    const list = [
      { id: 's1', title: 'First session', running: true },
      { id: 's2', title: 'Second session' },
    ];
    const { container } = render(
      <Sidebar {...defaultProps} sessions={list} session={null} uiRunning={false} />
    );

    // Foreign run (server flag) spins even though this tab never started it.
    expect(spinner(container, 's1')?.className).toContain('animate-spin');
    expect(spinner(container, 's2')).toBeNull();
  });

  it('pulses the active session title instantly from the local run flag', () => {
    const { container } = render(
      <Sidebar {...defaultProps} sessions={sessionList} session={sessionList[0]} uiRunning={true} />
    );

    expect(title(container, 's1')?.className).toContain('animate-pulse');
    // The local flag also spins the active session immediately.
    expect(spinner(container, 's1')?.className).toContain('animate-spin');
    // Other rows in the same chat stay idle.
    expect(title(container, 's2')?.className).not.toContain('animate-pulse');
    expect(spinner(container, 's2')).toBeNull();
  });

  it('shows exactly one pulsing title when both sources agree on the same session', () => {
    const list = [
      { id: 's1', title: 'First session', running: true },
      { id: 's2', title: 'Second session' },
    ];
    const { container } = render(
      <Sidebar {...defaultProps} sessions={list} session={list[0]} uiRunning={true} />
    );

    const pulsing = container.querySelectorAll(
      '[data-od-id^="sidebar-session-title-"].animate-pulse'
    );
    expect(pulsing.length).toBe(1);
    expect(pulsing[0].getAttribute('data-od-id')).toBe('sidebar-session-title-s1');
  });

  it('expands older sessions and triggers refetchAgentSessions with tenant and chat ids', () => {
    const list = [
      { id: 's1', title: 'One' },
      { id: 's2', title: 'Two' },
      { id: 's3', title: 'Three' },
      { id: 's4', title: 'Four' },
      { id: 's5', title: 'Five' },
      { id: 's6', title: 'Six' },
    ];
    const refetch = vi.fn();
    const store = useStore.getState() as any;
    const original = store.refetchAgentSessions;
    useStore.setState({ refetchAgentSessions: refetch } as any);
    try {
      const { container } = render(
        <Sidebar {...defaultProps} sessions={list} session={list[0]} />
      );

      // Capped to the four newest before expanding.
      expect(title(container, 's5')).toBeNull();
      const expand = screen.getByText(/Show 2 older sessions/).closest('button');
      expect(expand).not.toBeNull();

      fireEvent.click(expand as Element);

      // Expand still works and the refetch fires with the component's ids.
      expect(title(container, 's5')).not.toBeNull();
      expect(title(container, 's6')).not.toBeNull();
      expect(refetch).toHaveBeenCalledTimes(1);
      expect(refetch).toHaveBeenCalledWith('acme', 'a1');
    } finally {
      useStore.setState({ refetchAgentSessions: original } as any);
    }
  });

  it('still expands older sessions when the store refetch action is absent', () => {
    const list = [
      { id: 's1', title: 'One' },
      { id: 's2', title: 'Two' },
      { id: 's3', title: 'Three' },
      { id: 's4', title: 'Four' },
      { id: 's5', title: 'Five' },
    ];
    const original = (useStore.getState() as any).refetchAgentSessions;
    useStore.setState({ refetchAgentSessions: undefined } as any);
    try {
      const { container } = render(
        <Sidebar {...defaultProps} sessions={list} session={list[0]} />
      );

      fireEvent.click(screen.getByText(/Show 1 older sessions/).closest('button') as Element);

      // No store action yet — expansion must still work without throwing.
      expect(title(container, 's5')).not.toBeNull();
    } finally {
      useStore.setState({ refetchAgentSessions: original } as any);
    }
  });
});
