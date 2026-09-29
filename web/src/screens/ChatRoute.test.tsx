import { describe, it, expect, beforeEach, afterEach, beforeAll, vi } from 'vitest';
import { StrictMode } from 'react';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
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

// --- Catch-up stream (live-run-reattach-and-catchup D4) ---------------------
// The stream consumer uses raw fetch (not the api module), so the SSE
// endpoint is stubbed on globalThis.

const encoder = new TextEncoder();

/** Minimal SSE Response — only what streamSessionEvents reads. */
const sseResponse = (chunks: string[]): Response =>
  ({
    ok: true,
    status: 200,
    headers: new Headers({ 'Content-Type': 'text/event-stream' }),
    body: new ReadableStream<Uint8Array>({
      start(controller) {
        for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
        controller.close();
      },
    }),
  }) as unknown as Response;

const sseFrame = (ev: any) => `data: ${JSON.stringify(ev)}\n\n`;

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

  let fetchMock: any;

  beforeEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
    requestMock.mockImplementation(async (endpoint: string) => {
      if (endpoint.includes('/api-keys/exchange')) return { key: 'oc_exchanged_key', api_key: {} };
      if (endpoint.includes('/sessions/sess_converge/events')) return { events: SERVER_EVENTS, next: '' };
      if (endpoint.includes('/skills')) return { skills: [] };
      return {};
    });
    // Default catch-up stream: the server answers an inactive session with an
    // immediate [DONE] (design D2 Phase 3) — attach is idempotent for every
    // bound-session test below. Stubbed after restoreAllMocks so it survives.
    fetchMock = vi.fn(async () => sseResponse(['data: [DONE]\n\n']));
    vi.stubGlobal('fetch', fetchMock);
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
  });

  afterEach(() => {
    vi.unstubAllGlobals();
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

  // --- In-flight re-attachment & catch-up stream (live-run-reattach D4) -----

  /** Binds a-atlas to the given session id with an empty local thread. */
  const bindSession = (sessionId: string) => {
    const db: any = seedDb();
    db.acme.threads['a-atlas'] = {
      active: sessionId,
      list: [{ id: sessionId, title: 'Live', updated: '', messages: [] }],
    };
    useStore.setState({
      db,
      pos: { tenantId: 'acme', view: 'chats', chatId: 'a-atlas', showContext: false, railExpanded: false },
    });
  };

  it('attaches the catch-up stream for an unfinished server-side turn and streams the reply into the thread', async () => {
    // Hydrate history ends with the user's message — the turn is still
    // running server-side; everything after cursor e1 arrives over SSE.
    requestMock.mockImplementation(async (endpoint: string) => {
      if (endpoint.includes('/api-keys/exchange')) return { key: 'oc_exchanged_key', api_key: {} };
      if (endpoint.includes('/sessions/sess_live/events'))
        return {
          events: [
            { id: 'e1', kind: 'message_completed', occurred_at: '2026-09-07T10:00:00Z', turn_id: 't1',
              message: { role: 'user', content: 'Run diagnostics' } },
          ],
          next: '',
        };
      if (endpoint.includes('/skills')) return { skills: [] };
      return {};
    });
    fetchMock.mockImplementation(async () =>
      sseResponse([
        sseFrame({ id: 'e2', kind: 'text_delta', occurred_at: '2026-09-07T10:00:01Z', turn_id: 't1', text_delta: 'Live catch-' }),
        sseFrame({ id: 'e3', kind: 'text_delta', occurred_at: '2026-09-07T10:00:02Z', turn_id: 't1', text_delta: 'up reply' }),
        sseFrame({ id: 'e4', kind: 'tool_call_started', occurred_at: '2026-09-07T10:00:03Z', turn_id: 't1',
          tool_call: { call_id: 'call_9', name: 'grafana.query', arguments: 'q: uptime' } }),
        sseFrame({ id: 'e5', kind: 'tool_call_finished', occurred_at: '2026-09-07T10:00:04Z', turn_id: 't1',
          tool_result: { call_id: 'call_9', result: '99.9%', latency: 7600000000 } }),
        sseFrame({ id: 'e6', kind: 'turn_completed', occurred_at: '2026-09-07T10:00:05Z', turn_id: 't1',
          usage: { input_tokens: 4000, output_tokens: 321, total_tokens: 4321, final_input_tokens: 4321 } }),
        'data: [DONE]\n\n',
      ])
    );
    bindSession('sess_live');

    render(
      <MemoryRouter initialEntries={['/c/a-atlas']}>
        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
        </Routes>
      </MemoryRouter>
    );

    // Deltas fold into ONE agent message; spinner clears on [DONE].
    await waitFor(() => {
      expect(screen.getByText('Live catch-up reply')).not.toBeNull();
    });
    const sess: any = useStore
      .getState()
      .db.acme.threads['a-atlas'].list.find((x: any) => x.id === 'sess_live');
    const agentMsg = sess.messages.find((m: any) => m.author === 'agent');
    expect(agentMsg.text).toBe('Live catch-up reply');
    expect(agentMsg.tools).toHaveLength(1);
    expect(agentMsg.tools[0].name).toBe('grafana.query');
    expect(agentMsg.tools[0].res).toBe('99.9%');
    // Usage from the streamed turn_completed drives the context meter.
    expect(sess.usage.finalInput).toBe(4321);
    expect(useStore.getState().ui.running).toBe(false);

    // The stream opened with the hydrate cursor: stream=true&after=e1, JWT auth.
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain('/workspaces/acme/agents/a-atlas/sessions/sess_live/events');
    expect(String(url)).toContain('stream=true');
    expect(String(url)).toContain('after=e1');
    expect(init.headers.Authorization).toBe('Bearer t');
  });

  it('aborts the catch-up stream on unmount', async () => {
    requestMock.mockImplementation(async (endpoint: string) => {
      if (endpoint.includes('/api-keys/exchange')) return { key: 'oc_exchanged_key', api_key: {} };
      if (endpoint.includes('/sessions/sess_live/events'))
        return {
          events: [
            { id: 'e1', kind: 'message_completed', occurred_at: '2026-09-07T10:00:00Z', turn_id: 't1',
              message: { role: 'user', content: 'Run diagnostics' } },
          ],
          next: '',
        };
      if (endpoint.includes('/skills')) return { skills: [] };
      return {};
    });
    // A run still in flight: keep the stream open past the unmount.
    fetchMock.mockImplementation(async () =>
      sseResponse([
        sseFrame({ id: 'e2', kind: 'text_delta', occurred_at: 'x', turn_id: 't1', text_delta: 'partial' }),
      ])
    );
    bindSession('sess_live');

    const { unmount } = render(
      <MemoryRouter initialEntries={['/c/a-atlas']}>
        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
        </Routes>
      </MemoryRouter>
    );
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(1);
    });
    unmount();
    expect(fetchMock.mock.calls[0][1].signal.aborted).toBe(true);
    // Teardown without onDone still clears the composer spinner.
    expect(useStore.getState().ui.running).toBe(false);
  });

  it('skips the catch-up stream when this page already has a live turn running', async () => {
    bindSession('sess_live');
    useStore.setState({ ui: { ...useStore.getState().ui, running: true } });

    render(
      <MemoryRouter initialEntries={['/c/a-atlas']}>
        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
        </Routes>
      </MemoryRouter>
    );

    // Hydration itself still runs; only the catch-up attach is suppressed.
    await waitFor(() => {
      expect(requestMock).toHaveBeenCalledWith(
        expect.stringContaining('/sessions/sess_live/events'),
        expect.anything()
      );
    });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(fetchMock).not.toHaveBeenCalled();
  });

  // Regression: reloading the page mid-run never attached the catch-up
  // stream. StrictMode's setup→cleanup→setup cycle aborted the first attach
  // attempt, and the sticky hydrated-ref guard made the second run bail — so
  // the reloaded transcript froze at the committed history and the next send
  // hit the server's run lock (409). The guard must clear on cleanup.
  it('attaches the catch-up stream under StrictMode double-effect (reload mid-run)', async () => {
    requestMock.mockImplementation(async (endpoint: string) => {
      if (endpoint.includes('/api-keys/exchange')) return { key: 'oc_exchanged_key', api_key: {} };
      if (endpoint.includes('/sessions/sess_live/events'))
        return {
          events: [
            { id: 'e1', kind: 'message_completed', occurred_at: '2026-09-07T10:00:00Z', turn_id: 't1',
              message: { role: 'user', content: 'Run diagnostics' } },
          ],
          next: '',
        };
      if (endpoint.includes('/skills')) return { skills: [] };
      return {};
    });
    fetchMock.mockImplementation(async () =>
      sseResponse([
        sseFrame({ kind: 'run_active', occurred_at: '2026-09-07T10:00:01Z' }),
        sseFrame({ id: 'e2', kind: 'text_delta', occurred_at: '2026-09-07T10:00:02Z', turn_id: 't1', text_delta: 'Streamed after reload' }),
        sseFrame({ id: 'e3', kind: 'turn_completed', occurred_at: '2026-09-07T10:00:03Z', turn_id: 't1' }),
        'data: [DONE]\n\n',
      ])
    );
    bindSession('sess_live');
    // The "skips when running" test leaves ui.running true on the shared
    // store — the reload-under-test boots with an idle composer.
    useStore.setState({ ui: { ...useStore.getState().ui, running: false } });

    render(
      <StrictMode>
        <MemoryRouter initialEntries={['/c/a-atlas']}>
          <Routes>
            <Route path="/c/:chatId" element={<ChatRoute />} />
          </Routes>
        </MemoryRouter>
      </StrictMode>
    );

    // The run's remainder streams into the thread despite the double-invoked
    // hydration effect.
    await waitFor(() => {
      expect(screen.getByText('Streamed after reload')).not.toBeNull();
    });
    // Exactly one stream opened: the aborted first attempt must not strand a
    // duplicate, and the surviving attempt must not have been skipped.
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(useStore.getState().ui.running).toBe(false);
  });

  // --- Mention bridge (rework-document-chat-surfaces 2.2/D3) -----------------
  // The panel's Documents listing inserts mentions into the composer through
  // the ChatRoute bridge: the composer registers its insertion callback, the
  // documents source's per-row Insert action calls it, and the composer adds
  // the markdown token (the visible pill) plus the identity chip.

  it('the panel documents listing Insert action inserts the mention into the composer (token + chip)', async () => {
    useStore.setState({ panel: { open: false, tabs: [], activeId: null, badge: false } } as any);
    requestMock.mockImplementation(async (endpoint: string) => {
      if (endpoint.includes('/api-keys/exchange')) return { key: 'oc_exchanged_key', api_key: {} };
      if (endpoint.includes('/documents')) {
        return {
          documents: [{
            id: 'doc-1', name: 'twilio-api.pdf', description: 'Twilio API manual.', mime: 'application/pdf',
            size: 2048, url: '/api/v1/workspaces/acme/documents/doc-1/file', indexStatus: 'ready',
            scope: 'workspace', pageCount: 31, agents: [], channels: [], createdAt: '2026-09-01T00:00:00Z',
          }],
        };
      }
      if (endpoint.includes('/skills')) return { skills: [] };
      return {};
    });

    const { container } = render(
      <MemoryRouter initialEntries={['/c/a-atlas']}>
        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByLabelText(/conversation with atlas/i)).not.toBeNull();
    });

    // The header's documents affordance opens the panel Documents listing
    // (2.1, 2026-09-28 user pivot: header toggle beside the panel toggle);
    // the listing fetches through the conversation lens.
    fireEvent.click(screen.getByTestId('btn-panel-documents'));
    // The panel chrome renders BOTH instances (docked + overlay) — queries
    // scope to the docked one, like RightPanel.test.
    const docked = () => container.querySelector('[data-od-id="right-panel-docked"]') as HTMLElement;
    await waitFor(() => {
      expect(docked().querySelector('[data-testid="panel-doc-twilio-api.pdf"]')).not.toBeNull();
    });

    fireEvent.click(docked().querySelector('[data-testid="panel-doc-insert-twilio-api.pdf"]') as HTMLElement);

    // The bridge invoked the composer's callback: the markdown token sits in
    // the input and the identity chip landed in the tray.
    const input = screen.getByLabelText('Message input') as HTMLTextAreaElement;
    expect(input.value).toBe('[📄 twilio-api.pdf](references/twilio-api.pdf) ');
    const chip = document.querySelector('[data-testid="document-chip"]');
    expect(chip).not.toBeNull();
    expect(chip?.getAttribute('data-document-id')).toBe('doc-1');
    // Design D3: the panel stays open — preview-then-send flow.
    expect(docked().querySelector('[data-testid="panel-doc-twilio-api.pdf"]')).not.toBeNull();
  });
});

