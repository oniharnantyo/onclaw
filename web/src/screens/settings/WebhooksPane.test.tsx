import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import { WebhooksPane } from './WebhooksPane';
import { api, ApiError, type ApiAgent, type ApiChannel } from '../../lib/api';
import {
  workspaceWebhooksApi,
  type ApiStandaloneWebhookRow,
  type ApiWebhookDelivery,
  type ApiWebhookRow,
} from '../../lib/workspaceWebhooksApi';

const mockTenant = { id: 'acme', sub: 'acme', name: 'Acme Corp' };

const agents = (): ApiAgent[] =>
  [
    { id: 'a_atlas', slug: 'atlas', name: 'Atlas' },
    { id: 'a_beacon', slug: 'beacon', name: 'Beacon' },
  ].map((a) => ({
    id: a.id,
    workspace_id: 'acme',
    slug: a.slug,
    name: a.name,
    role: '',
    description: '',
    brief: '',
    identity: '',
    soul: '',
    provider_id: 'p1',
    model: 'm1',
    temperature: 1,
    autonomy: 'approval' as const,
    disabled_tools: [],
    enabled_mcps: [],
    avatar: {},
    prompts_status: 'ready' as const,
    created_at: '',
    updated_at: '',
  }));

const channels = (): ApiChannel[] => [
  {
    id: 'ch_ops',
    workspace_id: 'acme',
    name: 'Ops',
    slug: 'ops',
    purpose: '',
    conventions: '',
    created_by: null,
    created_at: '',
    updated_at: '',
  },
];

const standaloneRow = (overrides: Partial<NonNullable<unknown>> = {}): ApiStandaloneWebhookRow => ({
  source: 'standalone',
  webhook: {
    id: 'wh1',
    workspace_id: 'acme',
    name: 'gitlab-mr-review',
    scheme: 'plain_token',
    secret_hint: 'ab12',
    event_source: { kind: 'header', name: 'X-Gitlab-Event' },
    delivery_id_source: 'X-Gitlab-Delivery',
    filter_expression: 'event == "merge_request"',
    target_agent_id: 'a_atlas',
    target_kind: 'channel',
    target_id: 'ch_ops',
    enabled: true,
    created_at: '',
    updated_at: '',
    ...(overrides as any),
  },
  ingest_url: 'http://localhost:8080/api/ingest/hooks/acme/wh1',
});

const connectionRow = (): ApiWebhookRow => ({
  source: 'connection',
  connection_id: 'conn-gitlab',
  service: 'gitlab',
  webhook: {
    enabled: true,
    secret_hint: '9f04',
    ingest_url: 'http://localhost:8080/api/v1/ingest/conn-gitlab',
    target: { agent_id: 'a_atlas', target_kind: 'channel', target_id: 'ch_ops' },
    events: ['push', 'merge_request'],
  },
});

const deliveries = (): ApiWebhookDelivery[] => [
  {
    id: 'd1',
    webhook_id: 'wh1',
    event: 'merge_request',
    outcome: 'submitted',
    run_id: 'run_123',
    created_at: new Date().toISOString(),
  },
  {
    id: 'd2',
    webhook_id: 'wh1',
    event: 'push',
    outcome: 'filtered',
    detail: 'expression evaluated to false',
    created_at: new Date().toISOString(),
  },
  {
    id: 'd3',
    webhook_id: 'wh1',
    event: 'push',
    outcome: 'duplicate',
    created_at: new Date().toISOString(),
  },
];

function mockApis(rows: ApiWebhookRow[] = [standaloneRow()]) {
  vi.spyOn(workspaceWebhooksApi, 'list').mockResolvedValue({ webhooks: rows });
  vi.spyOn(api.agents, 'list').mockResolvedValue({ agents: agents() });
  vi.spyOn(api.channels, 'list').mockResolvedValue({ channels: channels() });
}

function renderPane(props: { canWrite?: boolean; onToast?: (t: string, k?: string) => void } = {}) {
  return render(
    <MemoryRouter>
      <WebhooksPane tenant={mockTenant} {...props} />
    </MemoryRouter>
  );
}

describe('screens/settings/WebhooksPane', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
  });

  it('renders standalone rows with scheme chip, target, hint, and ingest URL', async () => {
    mockApis();

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('webhook-name-wh1').textContent).toBe('gitlab-mr-review');
    });
    expect(screen.getByTestId('webhook-scheme-wh1').textContent).toBe('Plain token');
    expect(screen.getByTestId('webhook-target-wh1').textContent).toContain('Atlas');
    expect(screen.getByTestId('webhook-target-wh1').textContent).toContain('#Ops');
    expect(screen.getByTestId('webhook-hint-wh1').textContent).toContain('····ab12');
    expect(screen.getByTestId('webhook-ingest-wh1').textContent).toContain('/api/ingest/hooks/acme/wh1');
    expect(screen.getByTestId('webhook-state-wh1').textContent).toBe('Active');
    // Write-only: no plaintext secret in the DOM
    expect(screen.queryByText(/whsec_/)).toBeNull();
  });

  it('flags connection webhooks read-in and links to Integrations instead of management actions', async () => {
    mockApis([standaloneRow(), connectionRow()]);

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('webhook-connection-row-conn-gitlab')).not.toBeNull();
    });
    expect(screen.getByTestId('webhook-connection-flag-conn-gitlab').textContent).toBe('Connection webhook');
    expect(screen.getByTestId('webhook-connection-service-conn-gitlab').textContent).toBe('gitlab');
    const link = screen.getByTestId('link-manage-in-integrations-conn-gitlab');
    expect(link.textContent).toContain('Manage in Integrations');
    expect(link.getAttribute('href')).toBe('/settings/integrations');
    // Read-in rows carry no management affordances
    expect(screen.queryByTestId('btn-rotate-conn-gitlab')).toBeNull();
    expect(screen.queryByTestId('btn-edit-conn-gitlab')).toBeNull();
  });

  it('renders the empty state explaining webhooks with a create CTA', async () => {
    mockApis([]);

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('webhooks-empty')).not.toBeNull();
    });
    expect(screen.getByTestId('webhooks-empty').textContent).toContain('agent runs');
    expect(screen.getByTestId('btn-webhook-empty-create')).not.toBeNull();
  });

  it('renders loading skeletons without layout shift while the list resolves', async () => {
    vi.spyOn(workspaceWebhooksApi, 'list').mockImplementation(() => new Promise(() => {}));
    vi.spyOn(api.agents, 'list').mockResolvedValue({ agents: [] });
    vi.spyOn(api.channels, 'list').mockResolvedValue({ channels: [] });

    renderPane();

    expect(screen.getByTestId('webhooks-loading')).not.toBeNull();
    expect(screen.queryByTestId('webhooks-empty')).toBeNull();
  });

  it('renders the error state with retry on load failure', async () => {
    const listSpy = vi
      .spyOn(workspaceWebhooksApi, 'list')
      .mockRejectedValue(new ApiError(500, 'internal', 'boom'));
    vi.spyOn(api.agents, 'list').mockResolvedValue({ agents: [] });
    vi.spyOn(api.channels, 'list').mockResolvedValue({ channels: [] });

    renderPane();

    await waitFor(() => {
      expect(screen.getByText("Couldn't load the webhooks")).not.toBeNull();
    });
    expect(listSpy).toHaveBeenCalledTimes(1);

    listSpy.mockResolvedValue({ webhooks: [standaloneRow()] });
    fireEvent.click(screen.getByRole('button', { name: /retry/i }));

    await waitFor(() => {
      expect(listSpy).toHaveBeenCalledTimes(2);
      expect(screen.getByTestId('webhook-name-wh1')).not.toBeNull();
    });
  });

  it('creates a webhook with a generated secret and reveals it exactly once', async () => {
    mockApis([]);
    const createSpy = vi.spyOn(workspaceWebhooksApi, 'create').mockResolvedValue({
      secret: 'whsec_generated_value_1',
      webhook: {
        id: 'wh_new',
        workspace_id: 'acme',
        name: 'gitlab-mr-review',
        scheme: 'plain_token',
        secret_hint: 'e_1',
        event_source: { kind: 'header', name: 'X-Gitlab-Event' },
        delivery_id_source: 'X-Gitlab-Delivery',
        target_agent_id: 'a_atlas',
        target_kind: 'channel',
        target_id: 'ch_ops',
        enabled: true,
      },
      ingest_url: 'http://localhost:8080/api/ingest/hooks/acme/wh_new',
    });
    const onToast = vi.fn();

    renderPane({ onToast });

    await waitFor(() => {
      expect(screen.getByTestId('btn-webhook-add')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-webhook-add'));

    await waitFor(() => {
      expect(screen.getByTestId('modal-webhook')).not.toBeNull();
    });

    fireEvent.change(screen.getByTestId('input-webhook-name'), { target: { value: 'gitlab-mr-review' } });
    fireEvent.change(screen.getByTestId('input-webhook-event-header'), { target: { value: 'X-Gitlab-Event' } });
    fireEvent.change(screen.getByTestId('input-webhook-delivery-id'), { target: { value: 'X-Gitlab-Delivery' } });
    fireEvent.change(screen.getByTestId('select-webhook-agent'), { target: { value: 'a_atlas' } });
    fireEvent.click(screen.getByTestId('radio-webhook-channel'));
    fireEvent.change(screen.getByTestId('select-webhook-channel'), { target: { value: 'ch_ops' } });

    // Secret left empty — generated server-side
    fireEvent.click(screen.getByTestId('btn-webhook-create-confirm'));

    await waitFor(() => {
      expect(createSpy).toHaveBeenCalledWith('acme', {
        name: 'gitlab-mr-review',
        scheme: 'plain_token',
        event_source: { kind: 'header', name: 'X-Gitlab-Event' },
        delivery_id_source: 'X-Gitlab-Delivery',
        target_agent_id: 'a_atlas',
        target_kind: 'channel',
        target_id: 'ch_ops',
        enabled: true,
      });
      expect(onToast).toHaveBeenCalledWith('Webhook created — copy the secret now, it won\'t be shown again');
    });

    // Reveal-once banner: exactly one instance, then gone after Done
    await waitFor(() => {
      expect(screen.getByTestId('webhooks-secret-reveal')).not.toBeNull();
    });
    expect(screen.getAllByTestId('webhooks-secret-value').length).toBe(1);
    expect(screen.getByTestId('webhooks-secret-value').textContent).toBe('whsec_generated_value_1');

    fireEvent.click(screen.getByTestId('btn-copy-secret'));
    await waitFor(() => {
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('whsec_generated_value_1');
    });

    fireEvent.click(screen.getByTestId('btn-webhook-reveal-done'));
    expect(screen.queryByTestId('webhooks-secret-reveal')).toBeNull();
    expect(screen.queryByTestId('modal-webhook-secret')).toBeNull();
  });

  it('creates a webhook with a bring-your-own secret and shows no reveal', async () => {
    mockApis([]);
    const createSpy = vi.spyOn(workspaceWebhooksApi, 'create').mockResolvedValue({
      webhook: {
        id: 'wh_byo',
        workspace_id: 'acme',
        name: 'byo-hook',
        scheme: 'hmac_sha256',
        secret_hint: 'yb77',
        event_source: { kind: 'fixed', value: 'push' },
        delivery_id_source: 'X-Gitlab-Delivery',
        target_agent_id: 'a_atlas',
        target_kind: 'channel',
        target_id: 'ch_ops',
        enabled: true,
      },
      ingest_url: 'http://localhost:8080/api/ingest/hooks/acme/wh_byo',
    } as any);
    const onToast = vi.fn();

    renderPane({ onToast });

    await waitFor(() => {
      expect(screen.getByTestId('btn-webhook-add')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-webhook-add'));

    await waitFor(() => {
      expect(screen.getByTestId('modal-webhook')).not.toBeNull();
    });

    fireEvent.change(screen.getByTestId('input-webhook-name'), { target: { value: 'byo-hook' } });
    fireEvent.change(screen.getByTestId('select-webhook-scheme'), { target: { value: 'hmac_sha256' } });
    fireEvent.change(screen.getByTestId('select-webhook-event-source-kind'), { target: { value: 'fixed' } });
    fireEvent.change(screen.getByTestId('input-webhook-event-value'), { target: { value: 'push' } });
    fireEvent.change(screen.getByTestId('input-webhook-delivery-id'), { target: { value: 'X-Gitlab-Delivery' } });
    fireEvent.change(screen.getByTestId('input-webhook-secret'), { target: { value: 'my-own-secret' } });
    fireEvent.click(screen.getByTestId('radio-webhook-channel'));
    fireEvent.change(screen.getByTestId('select-webhook-agent'), { target: { value: 'a_atlas' } });
    fireEvent.change(screen.getByTestId('select-webhook-channel'), { target: { value: 'ch_ops' } });

    fireEvent.click(screen.getByTestId('btn-webhook-create-confirm'));

    await waitFor(() => {
      expect(createSpy).toHaveBeenCalledWith(
        'acme',
        expect.objectContaining({ name: 'byo-hook', scheme: 'hmac_sha256', secret: 'my-own-secret' })
      );
      expect(onToast).toHaveBeenCalledWith('Webhook byo-hook created');
    });
    // BYO: the server returns no secret — no reveal moment
    expect(screen.queryByTestId('modal-webhook-secret')).toBeNull();
    expect(screen.queryByTestId('webhooks-secret-reveal')).toBeNull();
  });

  it('blocks create with inline validation errors before calling the API', async () => {
    mockApis([]);
    const createSpy = vi.spyOn(workspaceWebhooksApi, 'create').mockResolvedValue({
      webhook: standaloneRow().webhook as any,
      ingest_url: 'x',
      secret: 's',
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('btn-webhook-add')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-webhook-add'));
    await waitFor(() => {
      expect(screen.getByTestId('modal-webhook')).not.toBeNull();
    });

    fireEvent.click(screen.getByTestId('btn-webhook-create-confirm'));

    expect(createSpy).not.toHaveBeenCalled();
    expect(screen.getByTestId('webhook-name-error').textContent).toContain('required');
    expect(screen.getByTestId('webhook-agent-error').textContent).toContain('Choose');
  });

  it('edits a webhook and renders a 400 filter parse error inline with the parser message verbatim', async () => {
    mockApis();
    const parserMessage = "unexpected token '\"' — strings are double-quoted";
    const updateSpy = vi
      .spyOn(workspaceWebhooksApi, 'update')
      .mockRejectedValue(new ApiError(400, 'invalid_request', parserMessage));

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('btn-edit-wh1')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-edit-wh1'));

    await waitFor(() => {
      expect(screen.getByTestId('modal-webhook')).not.toBeNull();
    });

    // The secret is immutable in edit — hint only, no input
    expect(screen.getByTestId('webhook-edit-hint').textContent).toContain('····ab12');
    expect(screen.queryByTestId('input-webhook-secret')).toBeNull();

    fireEvent.change(screen.getByTestId('input-webhook-filter'), {
      target: { value: "event == 'merge_request'" },
    });
    fireEvent.click(screen.getByTestId('btn-webhook-save'));

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith(
        'acme',
        'wh1',
        expect.objectContaining({
          name: 'gitlab-mr-review',
          filter_expression: "event == 'merge_request'",
          target_kind: 'channel',
        })
      );
      expect(screen.getByTestId('webhook-filter-error').textContent).toBe(parserMessage);
    });
    // The modal stays open — nothing closed on error
    expect(screen.getByTestId('modal-webhook')).not.toBeNull();
  });

  it('rotates the secret, revealing the new value once and updating the list hint', async () => {
    mockApis();
    vi.spyOn(workspaceWebhooksApi, 'rotate').mockResolvedValue({
      secret: 'whsec_rotated_value_2',
      webhook: { ...standaloneRow().webhook, secret_hint: 'cd34' } as any,
      ingest_url: standaloneRow().ingest_url,
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('btn-rotate-wh1')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-rotate-wh1'));

    await waitFor(() => {
      expect(screen.getByTestId('webhooks-secret-reveal')).not.toBeNull();
    });
    expect(screen.getByTestId('webhooks-secret-value').textContent).toBe('whsec_rotated_value_2');
    // The row hint reflects the rotated secret
    expect(screen.getByTestId('webhook-hint-wh1').textContent).toContain('····cd34');

    fireEvent.click(screen.getByTestId('btn-webhook-reveal-done'));
    expect(screen.queryByTestId('webhooks-secret-reveal')).toBeNull();
  });

  it('pauses and resumes a webhook through the dedicated actions and toggle', async () => {
    mockApis();
    const disableSpy = vi
      .spyOn(workspaceWebhooksApi, 'disable')
      .mockResolvedValue({
        webhook: { ...standaloneRow().webhook, enabled: false } as any,
        ingest_url: standaloneRow().ingest_url,
      });
    const enableSpy = vi.spyOn(workspaceWebhooksApi, 'enable').mockResolvedValue({
      webhook: standaloneRow().webhook as any,
      ingest_url: standaloneRow().ingest_url,
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('btn-pause-wh1')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-pause-wh1'));

    await waitFor(() => {
      expect(disableSpy).toHaveBeenCalledWith('acme', 'wh1');
      expect(screen.getByTestId('webhook-state-wh1').textContent).toBe('Paused');
      expect(screen.getByTestId('btn-resume-wh1')).not.toBeNull();
    });

    fireEvent.click(screen.getByTestId('btn-resume-wh1'));

    await waitFor(() => {
      expect(enableSpy).toHaveBeenCalledWith('acme', 'wh1');
      expect(screen.getByTestId('webhook-state-wh1').textContent).toBe('Active');
    });
  });

  it('deletes a webhook behind a two-click confirmation', async () => {
    mockApis();
    const removeSpy = vi.spyOn(workspaceWebhooksApi, 'remove').mockResolvedValue({ ok: true });
    const onToast = vi.fn();

    renderPane({ onToast });

    await waitFor(() => {
      expect(screen.getByTestId('btn-delete-wh1')).not.toBeNull();
    });

    // First click arms the confirmation — nothing deleted yet
    fireEvent.click(screen.getByTestId('btn-delete-wh1'));
    expect(removeSpy).not.toHaveBeenCalled();
    expect(screen.getByTestId('btn-delete-confirm-wh1')).not.toBeNull();

    fireEvent.click(screen.getByTestId('btn-delete-confirm-wh1'));

    await waitFor(() => {
      expect(removeSpy).toHaveBeenCalledWith('acme', 'wh1');
      expect(onToast).toHaveBeenCalledWith('gitlab-mr-review deleted');
      expect(screen.queryByTestId('webhook-row-wh1')).toBeNull();
    });
  });

  it('runs the dry-run tester and renders match, ignored, and error verdicts', async () => {
    mockApis();
    const dryRunSpy = vi
      .spyOn(workspaceWebhooksApi, 'dryRun')
      .mockResolvedValueOnce({ verdict: { outcome: 'match' } })
      .mockResolvedValueOnce({ verdict: { outcome: 'ignored' } })
      .mockResolvedValueOnce({ verdict: { outcome: 'error', message: 'unknown field: artefact' } });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('btn-deliveries-wh1')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-deliveries-wh1'));

    await waitFor(() => {
      expect(screen.getByTestId('input-dryrun-payload-wh1')).not.toBeNull();
    });

    fireEvent.change(screen.getByTestId('input-dryrun-payload-wh1'), {
      target: { value: '{"event": "merge_request"}' },
    });

    // Invalid JSON never reaches the API
    fireEvent.change(screen.getByTestId('input-dryrun-payload-wh1'), { target: { value: 'not json' } });
    fireEvent.click(screen.getByTestId('btn-dryrun-wh1'));
    await waitFor(() => {
      expect(screen.getByTestId('webhook-verdict-wh1').textContent).toContain('Payload must be valid JSON');
    });
    expect(dryRunSpy).not.toHaveBeenCalled();

    // Match
    fireEvent.change(screen.getByTestId('input-dryrun-payload-wh1'), {
      target: { value: '{"event": "merge_request"}' },
    });
    fireEvent.click(screen.getByTestId('btn-dryrun-wh1'));
    await waitFor(() => {
      expect(screen.getByTestId('webhook-verdict-wh1').textContent).toBe('Match');
    });

    // Ignored
    fireEvent.click(screen.getByTestId('btn-dryrun-wh1'));
    await waitFor(() => {
      expect(screen.getByTestId('webhook-verdict-wh1').textContent).toBe('Ignored');
    });

    // Error with the server message
    fireEvent.click(screen.getByTestId('btn-dryrun-wh1'));
    await waitFor(() => {
      expect(screen.getByTestId('webhook-verdict-wh1').textContent).toBe('Error — unknown field: artefact');
    });
    expect(dryRunSpy).toHaveBeenCalledTimes(3);
    expect(dryRunSpy).toHaveBeenCalledWith('acme', 'wh1', { event: 'merge_request' });
  });

  it('renders the delivery log with outcome badges, detail lines, and run links', async () => {
    mockApis();
    vi.spyOn(workspaceWebhooksApi, 'deliveries').mockResolvedValue({ entries: deliveries() });

    // The submitted row's run link opens the run's transcript (under the
    // endpoint's agent chat) — capture the landing location for the handoff
    // state assertion.
    let openedLocation: any = null;
    const RunTranscriptProbe = () => {
      openedLocation = useLocation();
      return <div data-testid="run-transcript-probe" />;
    };

    render(
      <MemoryRouter initialEntries={['/settings/webhooks']}>
        <Routes>
          <Route path="/settings/webhooks" element={<WebhooksPane tenant={mockTenant} />} />
          <Route path="/c/:chatId" element={<RunTranscriptProbe />} />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('btn-deliveries-wh1')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-deliveries-wh1'));

    await waitFor(() => {
      expect(screen.getByTestId('delivery-d1')).not.toBeNull();
    });
    expect(screen.getByTestId('delivery-outcome-d1').textContent).toBe('Submitted');
    expect(screen.getByTestId('delivery-outcome-d2').textContent).toBe('Filtered');
    expect(screen.getByTestId('delivery-outcome-d3').textContent).toBe('Duplicate');
    expect(screen.getByText('expression evaluated to false')).not.toBeNull();

    // The submitted delivery links to the run's transcript (endpoint agent chat)
    const runLink = screen.getByTestId('delivery-run-d1');
    expect(runLink.getAttribute('href')).toBe('/c/a_atlas');
    expect(runLink.textContent).toBe('run_123');
    // Filtered/duplicate entries carry no run link
    expect(screen.queryByTestId('delivery-run-d2')).toBeNull();

    fireEvent.click(runLink);
    await waitFor(() => {
      expect(screen.getByTestId('run-transcript-probe')).not.toBeNull();
    });
    // Same openRun handoff the channel room's activity strip uses
    expect(openedLocation.pathname).toBe('/c/a_atlas');
    expect(openedLocation.state).toEqual({
      openRun: { sessionId: 'run_123', schedulerName: 'gitlab-mr-review', originKind: 'webhook' },
    });
  });

  it('hides management actions for readers while keeping the list and log visible', async () => {
    mockApis();
    vi.spyOn(workspaceWebhooksApi, 'deliveries').mockResolvedValue({ entries: deliveries() });

    renderPane({ canWrite: false });

    await waitFor(() => {
      expect(screen.getByTestId('webhook-name-wh1')).not.toBeNull();
    });
    // No create affordance, no rotate/pause/edit/delete, no toggle
    expect(screen.queryByTestId('btn-webhook-add')).toBeNull();
    expect(screen.queryByTestId('btn-rotate-wh1')).toBeNull();
    expect(screen.queryByTestId('btn-pause-wh1')).toBeNull();
    expect(screen.queryByTestId('btn-edit-wh1')).toBeNull();
    expect(screen.queryByTestId('btn-delete-wh1')).toBeNull();
    expect(screen.queryByRole('switch')).toBeNull();

    // Reads still work: the log expands, the writer-only dry-run stays hidden
    fireEvent.click(screen.getByTestId('btn-deliveries-wh1'));
    await waitFor(() => {
      expect(screen.getByTestId('delivery-d1')).not.toBeNull();
    });
    expect(screen.queryByTestId('btn-dryrun-wh1')).toBeNull();
  });
});
