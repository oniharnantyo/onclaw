import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { useStore, useWorkspace, useThread } from './index';
import { useAuthStore } from './auth';
import { seedDb } from '../data/seed';

function WorkspaceProbe() {
  const ws = useWorkspace();
  return <div data-testid="ws">{ws.name}</div>;
}

function ThreadProbe({ chatId }: { chatId: string }) {
  const th = useThread(chatId);
  return <div data-testid="th">{th.list.length}</div>;
}

const posFor = (tenantId: string) => ({ tenantId, view: 'chats', chatId: '', showContext: false });

describe('derived store selectors', () => {
  beforeEach(() => {
    localStorage.clear();
    useStore.setState({ db: seedDb(), pos: posFor('acme') });
    useAuthStore.setState({ memberships: [] });
  });

  it('useWorkspace resolves a membership-only workspace without looping', () => {
    // Regression: the old selector built a fresh blankTenant per call for ids
    // missing from db, which re-rendered the component forever under
    // useSyncExternalStore ("Maximum update depth exceeded").
    useAuthStore.setState({
      memberships: [{ workspace_id: 'ghost-ws', workspace_name: 'Ghost WS' } as any],
    });
    useStore.setState({ pos: posFor('ghost-ws') });

    render(<WorkspaceProbe />);

    expect(screen.getByTestId('ws').textContent).toBe('Ghost WS');
  });

  it('useWorkspace falls back to a stable default when the id is unknown everywhere', () => {
    useStore.setState({ pos: posFor('nowhere') });

    render(<WorkspaceProbe />);

    expect(screen.getByTestId('ws').textContent).toBe('Acme Corp');
  });

  it('useThread wraps legacy array-shaped threads into a session', () => {
    useStore.setState({
      db: { acme: { ...seedDb().acme, threads: { 'legacy-agent': [{ id: 'm1', author: 'you', ts: '9:00 AM', text: 'hello' }] } } },
      pos: posFor('acme'),
    });

    render(<ThreadProbe chatId="legacy-agent" />);

    expect(screen.getByTestId('th').textContent).toBe('1');
  });

  it('useThread returns the shared empty state for missing threads', () => {
    render(<ThreadProbe chatId="does-not-exist" />);

    expect(screen.getByTestId('th').textContent).toBe('0');
  });
});
