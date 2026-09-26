/**
 * @vitest-environment jsdom
 */
// ConnectionWebhooksDialog tests (add-connection-webhooks 4.3): the disabled
// state's recipe-default event checks with a gated Enable, the enable happy
// path (exact body shape + the reveal-once secret banner), diffed saves
// (events-only vs target-only), rotate's fresh banner, the two-step disable
// confirm, last_error rendering, the no-webhook recipe fallback, and inline
// failure handling that never closes the dialog.
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ConnectionWebhooksDialog } from './ConnectionWebhooksDialog';
import { type ApiConnection, type ApiIntegrationRecipe } from '../lib/connectionsApi';
import { api, ApiError } from '../lib/api';

// This environment's jsdom exposes no localStorage — install the stub BEFORE
// the token-carrying imports (same mode as src/lib/panel/filesApi.test.ts).
const backing = new Map<string, string>();
(globalThis as any).localStorage = {
  getItem: (k: string) => (backing.has(k) ? backing.get(k)! : null),
  setItem: (k: string, v: string) => void backing.set(k, String(v)),
  removeItem: (k: string) => void backing.delete(k),
  clear: () => void backing.clear(),
  key: (i: number) => Array.from(backing.keys())[i] ?? null,
  get length() { return backing.size; },
};

// The webhook namespace and listAgentSessions are hoisted mocks:
// listAgentSessions is a module-level function (vi.spyOn can't reach it), and
// the webhook namespace may not exist on the real connectionsApi yet — the
// factory layers it over importOriginal either way.
const mocks = vi.hoisted(() => ({
  webhookGet: vi.fn(),
  webhookEnable: vi.fn(),
  webhookDisable: vi.fn(),
  webhookRotate: vi.fn(),
  webhookUpdateTarget: vi.fn(),
  webhookUpdateEvents: vi.fn(),
  listAgentSessions: vi.fn(),
}));

vi.mock('../lib/connectionsApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/connectionsApi')>();
  return {
    ...actual,
    connectionsApi: {
      ...actual.connectionsApi,
      webhook: {
        get: mocks.webhookGet,
        enable: mocks.webhookEnable,
        disable: mocks.webhookDisable,
        rotate: mocks.webhookRotate,
        updateTarget: mocks.webhookUpdateTarget,
        updateEvents: mocks.webhookUpdateEvents,
      },
    },
  };
});

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>();
  return { ...actual, listAgentSessions: mocks.listAgentSessions };
});

const tenant = { id: 'acme', sub: 'acme' };

// `webhooks` is the sibling contract on ApiIntegrationRecipe — the cast keeps
// the fixture honest whether or not the type has landed yet.
const githubRecipe = (overrides: any = {}): ApiIntegrationRecipe =>
  ({
    id: 'github',
    service: 'GitHub',
    icon: 'github',
    auth_kind: 'pat',
    availability: 'available',
    transport: 'streamable_http',
    access_levels: ['read_only', 'read_write'],
    steps: [],
    scopes: [],
    probe: { tool: 'list-repositories' },
    webhooks: {
      events: ['push', 'pull_request', 'issue_comment'],
      default_events: ['push', 'issue_comment'],
      signature_scheme: 'hmac-sha256',
      templates: [],
      setup: {
        signature_header: 'X-Hub-Signature-256',
        event_type_header: 'X-Github-Event',
        delivery_id_header: 'X-Github-Delivery',
        url_path_shape: 'https://onclaw.example/w/:workspace/hooks/github/:connection',
        help: 'Add the payload URL and secret under your repo webhook settings.',
      },
    },
    ...overrides,
  }) as ApiIntegrationRecipe;

const connectionRow = (overrides: any = {}): ApiConnection =>
  ({
    id: 'conn-gh',
    workspace_id: 'acme',
    service: 'github',
    access_level: 'read_only',
    status: 'connected',
    status_error: null,
    token_hint: 'a1b2',
    server_id: 'srv-gh',
    server_enabled: true,
    tool_count: 24,
    attached_agents: ['Atlas'],
    origin: 'https://github.example.com',
    created_at: '',
    updated_at: '',
    ...overrides,
  }) as ApiConnection;

const enabledView = (overrides: any = {}) => ({
  enabled: true,
  secret_hint: '9f8e',
  ingest_url: 'https://onclaw.example/hooks/github/conn-gh',
  target: { agent_id: 'a1', target_kind: 'thread', target_id: 'sess-1' },
  events: ['push', 'issue_comment'],
  ...overrides,
});

const agentsFixture = () =>
  [
    { id: 'a1', slug: 'atlas', name: 'Atlas', enabled_mcps: [] },
    { id: 'a2', slug: 'beacon', name: 'Beacon', enabled_mcps: [] },
  ] as any[];

const channelsFixture = () => [{ id: 'ch-1', name: 'Ops', slug: 'ops' }] as any[];

const atlasSessions = [
  { id: 'row-1', session_id: 'sess-1', title: 'Morning triage', created_at: '', last_active_at: '', running: false },
];
const beaconSessions = [
  { id: 'row-2', session_id: 'sess-2', title: '', created_at: '', last_active_at: '', running: false },
];

function renderDialog(
  connection: ApiConnection,
  recipes: ApiIntegrationRecipe[],
  props: Partial<Parameters<typeof ConnectionWebhooksDialog>[0]> = {}
) {
  return render(
    <ConnectionWebhooksDialog
      tenant={tenant}
      connection={connection}
      recipes={recipes}
      onClose={vi.fn()}
      onToast={vi.fn()}
      onSaved={vi.fn()}
      {...props}
    />
  );
}

/** The agent/channel loads land async — interactions must wait for real
 * options, or fireEvent.change resolves to '' against an empty select. */
async function waitForSelectOptions(testId: string) {
  await waitFor(() => {
    expect((screen.getByTestId(testId) as HTMLSelectElement).options.length).toBeGreaterThan(1);
  });
}

describe('modals/ConnectionWebhooksDialog', () => {
  beforeEach(() => {
    // restoreAllMocks resets the api spies; clearAllMocks drops the hoisted
    // vi.fn()s' call history (restore doesn't touch plain mocks).
    vi.restoreAllMocks();
    vi.clearAllMocks();
    vi.spyOn(api.agents, 'list').mockResolvedValue({ agents: agentsFixture() });
    vi.spyOn(api.channels, 'list').mockResolvedValue({ channels: channelsFixture() } as any);
    mocks.webhookGet.mockResolvedValue({ webhook: enabledView() });
    mocks.webhookEnable.mockResolvedValue({
      secret: 'whsec_new1',
      webhook: enabledView({ secret_hint: 'new1' }),
    });
    mocks.webhookDisable.mockResolvedValue({ webhook: enabledView({ enabled: false }) });
    mocks.webhookRotate.mockResolvedValue({
      secret: 'whsec_rot1',
      webhook: enabledView({ secret_hint: 'rot1' }),
    });
    mocks.webhookUpdateTarget.mockImplementation((_ws: any, _id: any, body: any) =>
      Promise.resolve({ webhook: enabledView({ target: body }) })
    );
    mocks.webhookUpdateEvents.mockImplementation((_ws: any, _id: any, body: any) =>
      Promise.resolve({ webhook: enabledView({ events: body.events }) })
    );
    mocks.listAgentSessions.mockImplementation((_ws: any, slug: any) =>
      Promise.resolve({ sessions: slug === 'atlas' ? atlasSessions : beaconSessions })
    );
  });

  it('renders the disabled state with recipe-default events checked and holds Enable until agent+target are chosen', async () => {
    mocks.webhookGet.mockResolvedValue({ webhook: { enabled: false } });
    renderDialog(connectionRow(), [githubRecipe()]);

    await waitFor(() => {
      expect(mocks.webhookGet).toHaveBeenCalledWith('acme', 'conn-gh');
    });
    expect(screen.getByTestId('webhooks-status').textContent).toContain(
      'Disabled — GitHub events are not ingested.'
    );
    // Defaults pre-checked; the rest of the catalog unchecked.
    expect((screen.getByTestId('webhook-event-push') as HTMLInputElement).checked).toBe(true);
    expect((screen.getByTestId('webhook-event-issue_comment') as HTMLInputElement).checked).toBe(true);
    expect((screen.getByTestId('webhook-event-pull_request') as HTMLInputElement).checked).toBe(false);
    expect((screen.getByTestId('btn-webhook-enable') as HTMLButtonElement).disabled).toBe(true);

    await waitForSelectOptions('select-webhook-agent');
    fireEvent.change(screen.getByTestId('select-webhook-agent'), { target: { value: 'a1' } });
    fireEvent.click(screen.getByTestId('radio-webhook-thread'));
    await waitForSelectOptions('select-webhook-thread');
    // Agent picked, thread list loaded — but no thread chosen yet.
    expect((screen.getByTestId('btn-webhook-enable') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByTestId('select-webhook-thread'), { target: { value: 'sess-1' } });
    expect((screen.getByTestId('btn-webhook-enable') as HTMLButtonElement).disabled).toBe(false);
  });

  it('enables with the exact body shape and shows the reveal-once secret banner', async () => {
    mocks.webhookGet.mockResolvedValue({ webhook: { enabled: false } });
    const onSaved = vi.fn();
    renderDialog(connectionRow(), [githubRecipe()], { onSaved });

    await waitForSelectOptions('select-webhook-agent');
    fireEvent.change(screen.getByTestId('select-webhook-agent'), { target: { value: 'a1' } });
    fireEvent.click(screen.getByTestId('radio-webhook-thread'));
    await waitForSelectOptions('select-webhook-thread');
    fireEvent.change(screen.getByTestId('select-webhook-thread'), { target: { value: 'sess-1' } });
    fireEvent.click(screen.getByTestId('btn-webhook-enable'));

    await waitFor(() => {
      expect(mocks.webhookEnable).toHaveBeenCalledWith('acme', 'conn-gh', {
        agent_id: 'a1',
        target_kind: 'thread',
        target_id: 'sess-1',
        events: ['push', 'issue_comment'],
      });
    });
    // The thread target binds session_id (the agent-facing handle), not row id.
    await waitFor(() => {
      expect(screen.getByTestId('webhooks-secret-value').textContent).toBe('whsec_new1');
    });
    expect(screen.getByTestId('webhooks-secret-reveal').textContent).toContain('SHOWN ONLY ONCE');
    expect(screen.getByTestId('webhooks-status').textContent).toContain(
      'Enabled — GitHub events become turns for Atlas in Morning triage.'
    );
    expect(onSaved).toHaveBeenCalled();
  });

  it('shows the enabled setup surface and saves only the events diff when only events changed', async () => {
    const onToast = vi.fn();
    const onSaved = vi.fn();
    renderDialog(connectionRow({ webhook: enabledView() }), [githubRecipe()], { onToast, onSaved });

    await waitFor(() => {
      expect(screen.getByTestId('webhooks-status').textContent).toContain(
        'Enabled — GitHub events become turns for Atlas in Morning triage.'
      );
    });
    // Provider setup: server-declared ingest URL + hint-only secret.
    expect(screen.getByTestId('webhooks-setup').textContent).toContain(
      'https://onclaw.example/hooks/github/conn-gh'
    );
    expect(screen.getByTestId('webhooks-setup').textContent).toContain('····9f8e');
    // Unchanged — save stays disabled.
    expect((screen.getByTestId('btn-webhook-save') as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(screen.getByTestId('webhook-event-pull_request'));
    expect((screen.getByTestId('btn-webhook-save') as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByTestId('btn-webhook-save'));

    await waitFor(() => {
      expect(mocks.webhookUpdateEvents).toHaveBeenCalledWith('acme', 'conn-gh', {
        events: ['push', 'issue_comment', 'pull_request'],
      });
    });
    expect(mocks.webhookUpdateTarget).not.toHaveBeenCalled();
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('GitHub webhooks updated');
      expect(onSaved).toHaveBeenCalled();
    });
  });

  it('saves only the target diff when the agent changes', async () => {
    renderDialog(connectionRow({ webhook: enabledView() }), [githubRecipe()]);

    await waitFor(() => {
      expect(mocks.listAgentSessions).toHaveBeenCalledWith('acme', 'atlas');
    });
    fireEvent.change(screen.getByTestId('select-webhook-agent'), { target: { value: 'a2' } });
    await waitFor(() => {
      expect(mocks.listAgentSessions).toHaveBeenCalledWith('acme', 'beacon');
    });
    await waitFor(() => {
      // Beacon's untitled session renders under its session_id.
      expect(screen.getByTestId('select-webhook-thread').textContent).toContain('sess-2');
    });
    fireEvent.change(screen.getByTestId('select-webhook-thread'), { target: { value: 'sess-2' } });
    fireEvent.click(screen.getByTestId('btn-webhook-save'));

    await waitFor(() => {
      expect(mocks.webhookUpdateTarget).toHaveBeenCalledWith('acme', 'conn-gh', {
        agent_id: 'a2',
        target_kind: 'thread',
        target_id: 'sess-2',
      });
    });
    expect(mocks.webhookUpdateEvents).not.toHaveBeenCalled();
  });

  it('rotate shows a fresh reveal-once banner with the new secret', async () => {
    const onSaved = vi.fn();
    renderDialog(connectionRow({ webhook: enabledView() }), [githubRecipe()], { onSaved });

    await waitFor(() => {
      expect(screen.getByTestId('btn-webhook-rotate')).not.toBeNull();
    });
    expect(screen.queryByTestId('webhooks-secret-reveal')).toBeNull();

    fireEvent.click(screen.getByTestId('btn-webhook-rotate'));
    await waitFor(() => {
      expect(mocks.webhookRotate).toHaveBeenCalledWith('acme', 'conn-gh');
    });
    await waitFor(() => {
      expect(screen.getByTestId('webhooks-secret-value').textContent).toBe('whsec_rot1');
    });
    expect(onSaved).toHaveBeenCalled();
  });

  it('disable requires a two-step confirm and flips the status line', async () => {
    const onToast = vi.fn();
    const onSaved = vi.fn();
    renderDialog(connectionRow({ webhook: enabledView() }), [githubRecipe()], { onToast, onSaved });

    await waitFor(() => {
      expect(screen.getByTestId('btn-webhook-disable')).not.toBeNull();
    });
    // First click only arms the confirm.
    fireEvent.click(screen.getByTestId('btn-webhook-disable'));
    expect(screen.getByTestId('btn-webhook-disable').textContent).toBe('Confirm disable');
    expect(mocks.webhookDisable).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId('btn-webhook-disable'));
    await waitFor(() => {
      expect(mocks.webhookDisable).toHaveBeenCalledWith('acme', 'conn-gh');
    });
    await waitFor(() => {
      expect(screen.getByTestId('webhooks-status').textContent).toContain(
        'Disabled — GitHub events are not ingested.'
      );
    });
    expect(onToast).toHaveBeenCalledWith('GitHub webhooks disabled');
    expect(onSaved).toHaveBeenCalled();
  });

  it('renders the webhook last error when present', async () => {
    const withError = enabledView({
      last_error: { event: 'push', error: 'signature mismatch', at: '2026-09-25T10:00:00Z' },
    });
    // The fresh read is authoritative — it must carry the error, not just the seed.
    mocks.webhookGet.mockResolvedValue({ webhook: withError });
    renderDialog(connectionRow({ webhook: withError }), [githubRecipe()]);

    await waitFor(() => {
      expect(screen.getByTestId('webhooks-last-error').textContent).toContain('signature mismatch');
    });
    const line = screen.getByTestId('webhooks-last-error').textContent;
    expect(line).toContain('push');
    expect(line).toContain('2026-09-25T10:00:00Z');
  });

  it('renders the no-webhook-support fallback when the recipe declares none', () => {
    renderDialog(connectionRow(), [githubRecipe({ webhooks: undefined })]);

    expect(screen.getByTestId('modal-connection-webhooks').textContent).toContain(
      "This service doesn't declare webhook support."
    );
    expect(screen.queryByTestId('btn-webhook-enable')).toBeNull();
    expect(mocks.webhookGet).not.toHaveBeenCalled();
  });

  it('surfaces an enable failure inline and never closes the dialog', async () => {
    mocks.webhookGet.mockResolvedValue({ webhook: { enabled: false } });
    mocks.webhookEnable.mockRejectedValue(new ApiError(400, 'invalid_request', 'pick at least one event'));
    const onClose = vi.fn();
    renderDialog(connectionRow(), [githubRecipe()], { onClose });

    await waitForSelectOptions('select-webhook-agent');
    fireEvent.change(screen.getByTestId('select-webhook-agent'), { target: { value: 'a1' } });
    fireEvent.click(screen.getByTestId('radio-webhook-thread'));
    await waitForSelectOptions('select-webhook-thread');
    fireEvent.change(screen.getByTestId('select-webhook-thread'), { target: { value: 'sess-1' } });
    fireEvent.click(screen.getByTestId('btn-webhook-enable'));

    await waitFor(() => {
      expect(screen.getByTestId('webhooks-error').textContent).toContain('pick at least one event');
    });
    expect(onClose).not.toHaveBeenCalled();
  });
});
