import { describe, it, expect, beforeEach, beforeAll, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { ChatRoute } from './ChatRoute';
import { useStore } from '../store';
import { useAuthStore } from '../store/auth';
import { seedDb } from '../data/seed';

vi.mock('@assistant-ui/react', () => ({
  useExternalStoreRuntime: vi.fn((opts) => opts),
  AssistantRuntimeProvider: ({ children }: any) => <div>{children}</div>,
}));

// Server transcript served by the native session-events endpoint for the
// bound session — the authority a second browser must converge to.
const SERVER_EVENTS = [
  { id: 'e1', kind: 'message_completed', occurred_at: '2026-09-06T10:00:00Z', turn_id: 't1',
    message: { role: 'user', content: 'Server-side user message' } },
  { id: 'e2', kind: 'tool_call_started', occurred_at: '2026-09-06T10:00:01Z', turn_id: 't2',
    tool_call: { call_id: 'call_1', name: 'grafana.query', arguments: 'q: uptime' } },
  { id: 'e3', kind: 'tool_call_finished', occurred_at: '2026-09-06T10:00:02Z', turn_id: 't2',
    tool_result: { call_id: 'call_1', name: 'grafana.query', result: '99.9%', latency: 7600000000 } },
  { id: 'e4', kind: 'message_completed', occurred_at: '2026-09-06T10:00:03Z', turn_id: 't2',
    message: { role: 'assistant', content: 'Server-side reply from history' } },
];

const { requestMock } = vi.hoisted(() => ({ requestMock: vi.fn() }));

vi.mock('../lib/api', () => {
  class ApiError extends Error {}
  const api = {
    request: requestMock,
    onUnauthorized: () => () => {},
    auth: {},
    agents: {
      listSkills: async () => ({ skills: [] }),
      sessionEvents: (ws: string, agent: string, sid: string) =>
        requestMock(`/workspaces/${ws}/agents/${agent}/sessions/${sid}/events`, { method: 'GET' }),
    },
    tools: { list: async () => ({ tools: [] }) },
  };
  return {
    api,
    apiKeys: {
      exchange: (ws: string) =>
        requestMock(`/workspaces/${encodeURIComponent(ws)}/api-keys/exchange`, { method: 'POST' }),
    },
    request: requestMock,
    ApiError,
    formatApiError: (_e: unknown, fallback: string) => fallback,
    getToken: () => 't',
    setToken: () => {},
    clearToken: () => {},
    pollAgentPromptsStatus: async () => ({}),
    API_ORIGIN: '',
  };
});

describe('ChatRoute component', () => {
  // Node's own disabled `localStorage` global shadows jsdom's on this Node
  // version; install a memory-backed stub so storage-backed behavior is
  // testable regardless (same failure mode as the pre-existing suite-wide
  // localStorage failures).
  beforeAll(() => {
    if (typeof localStorage === 'undefined' || !localStorage) {
      const mem = new Map<string, string>();
      const stub = {
        getItem: (k: string) => mem.get(k) ?? null,
        setItem: (k: string, v: string) => void mem.set(k, String(v)),
        removeItem: (k: string) => void mem.delete(k),
        clear: () => mem.clear(),
        key: (i: number) => Array.from(mem.keys())[i] ?? null,
        get length() {
          return mem.size;
        },
      };
      Object.defineProperty(globalThis, 'localStorage', { value: stub, configurable: true, writable: true });
    }
  });

  beforeEach(() => {
    localStorage.clear();
    requestMock.mockImplementation(async (endpoint: string) => {
      if (endpoint.includes('/api-keys/exchange')) return { key: 'oc_exchanged_key', api_key: {} };
      if (endpoint.includes('/sessions/sess_converge/events')) return { events: SERVER_EVENTS, next: '' };
      if (endpoint.includes('/skills')) return { skills: [] };
      return {};
    });
    useAuthStore.setState({
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [],
      status: 'authenticated',
      boot: vi.fn(),
    });
    useStore.setState({
      db: seedDb(),
      agentsLoaded: { acme: true },
      pos: { tenantId: 'acme', view: 'chats', chatId: 'a-atlas', showContext: false, railExpanded: false },
    });
    vi.restoreAllMocks();
  });

  it('renders not-found ErrorState for a well-formed but unknown chat ID', async () => {
    render(
      <MemoryRouter initialEntries={['/c/a-unknown-agent']}>
        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByText('Chat not found')).not.toBeNull();
      expect(screen.getByText(/the requested agent, channel, or person does not exist/i)).not.toBeNull();
      expect(screen.getByRole('button', { name: 'Back to chats' })).not.toBeNull();
      expect(screen.getByRole('button', { name: 'View agents' })).not.toBeNull();
    });
  });

  it('holds a loading state for an unknown chat ID while agents have not loaded yet', async () => {
    useStore.setState({ agentsLoaded: {} });

    render(
      <MemoryRouter initialEntries={['/c/a-unknown-agent']}>
        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
        </Routes>
      </MemoryRouter>
    );

    expect(screen.getByTestId('chat-loading')).not.toBeNull();
    expect(screen.queryByText('Chat not found')).toBeNull();

    // Once the workspace's agent list arrives, the id is genuinely missing.
    useStore.setState({ agentsLoaded: { acme: true } });

    await waitFor(() => {
      expect(screen.getByText('Chat not found')).not.toBeNull();
    });
  });

  it('renders not-found for a malformed chat ID without auto-opening an agent', async () => {
    render(
      <MemoryRouter initialEntries={['/c/@!invalid$$']}>
        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByText('Chat not found')).not.toBeNull();
    });
    expect(screen.queryByLabelText(/conversation with atlas/i)).toBeNull();
  });

  it('renders /c with no conversation opened, even though agents exist', async () => {
    render(
      <MemoryRouter initialEntries={['/c']}>
        <Routes>
          <Route path="/c" element={<ChatRoute />} />
          <Route path="/c/:chatId" element={<ChatRoute />} />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('chat-empty')).not.toBeNull();
      expect(screen.getByText('Select a conversation')).not.toBeNull();
    });
    // The workspace has agents (seeded acme) — none of them may auto-open.
    expect(screen.queryByLabelText(/conversation with atlas/i)).toBeNull();
  });

  it('renders active chat view for a valid agent ID', async () => {
    render(
      <MemoryRouter initialEntries={['/c/a-atlas']}>
        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByLabelText(/conversation with atlas/i)).not.toBeNull();
    });
  });

  it('provisions a chat key via the exchange endpoint on workspace entry', async () => {
    render(
      <MemoryRouter initialEntries={['/c/a-atlas']}>
        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(requestMock).toHaveBeenCalledWith(expect.stringContaining('/api-keys/exchange'), expect.anything());
    });
    expect(localStorage.getItem('onclaw.api_key.acme')).toBe('oc_exchanged_key');
  });

  it('hydrates a bound session from the server transcript (second-browser convergence)', async () => {
    const db: any = seedDb();
    db.acme.threads['a-atlas'] = {
      active: 'sess_converge',
      list: [
        {
          id: 'sess_converge',
          title: 'Converged',
          updated: '',
          messages: [{ id: 'm-stale', author: 'you', ts: '', text: 'stale local-only message' }],
        },
      ],
    };
    useStore.setState({ db, pos: { tenantId: 'acme', view: 'chats', chatId: 'a-atlas', showContext: false, railExpanded: false } });

    const { unmount } = render(
      <MemoryRouter initialEntries={['/c/a-atlas']}>
        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
        </Routes>
      </MemoryRouter>
    );

    // The server transcript replaces the local thread: server messages and
    // tool output render, the stale local-only message is gone.
    await waitFor(() => {
      expect(screen.getByText('Server-side reply from history')).not.toBeNull();
    });
    expect(screen.getByText('Server-side user message')).not.toBeNull();
    expect(screen.queryByText('stale local-only message')).toBeNull();

    // Both "browsers" load the same session id — a fresh mount (second
    // browser) with different local state converges to the identical server
    // transcript.
    const again: any = seedDb();
    again.acme.threads['a-atlas'] = {
      active: 'sess_converge',
      list: [
        {
          id: 'sess_converge',
          title: 'Converged',
          updated: '',
          messages: [{ id: 'm-stale-2', author: 'agent', ts: '', text: 'different local divergence' }],
        },
      ],
    };
    unmount();
    useStore.setState({ db: again });    render(
      <MemoryRouter initialEntries={['/c/a-atlas']}>
        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
        </Routes>
      </MemoryRouter>
    );await waitFor(() => {
      expect(screen.getByText('Server-side reply from history')).not.toBeNull();
    });
    expect(screen.queryByText('different local divergence')).toBeNull();
  });

  it('hydrates nothing for legacy unbound sessions', async () => {
    requestMock.mockClear();
    const db: any = seedDb();
    db.acme.threads['a-atlas'] = {
      active: 's1',
      list: [{ id: 's1', title: 'Legacy', updated: '', messages: [{ id: 'm1', author: 'you', ts: '', text: 'legacy local message' }] }],
    };
    useStore.setState({ db, pos: { tenantId: 'acme', view: 'chats', chatId: 'a-atlas', showContext: false, railExpanded: false } });

    render(
      <MemoryRouter initialEntries={['/c/a-atlas']}>
        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByText('legacy local message')).not.toBeNull();
    });
    expect(requestMock).not.toHaveBeenCalledWith(expect.stringContaining('/events'), expect.anything());
  });
});

