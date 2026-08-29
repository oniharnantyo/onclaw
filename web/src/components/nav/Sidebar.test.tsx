import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { Sidebar } from './Sidebar';

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
    cron: [
      { id: 'cron1', name: 'Daily Digest', expr: '0 9 * * 1-5', next: 'Mon 09:00', enabled: true },
    ],
    runs: [
      { id: 'r1', status: 'success' },
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
    onEditCron: vi.fn(),
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
});
