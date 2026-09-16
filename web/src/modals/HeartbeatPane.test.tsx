import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { HeartbeatPane, type HeartbeatPaneHandle } from './HeartbeatPane';
import { api, ApiError } from '../lib/api';
import { heartbeats, type ApiHeartbeat } from '../lib/heartbeats';

const hbRow = (overrides: Partial<ApiHeartbeat> = {}): ApiHeartbeat => ({
  id: 'hb-1',
  workspace_id: 'acme',
  agent_id: 'radar',
  prompt: 'Check the dashboards',
  expr: '*/30 * * * *',
  human_label: 'every 30 minutes',
  active_start: null,
  active_end: null,
  delivery: { type: 'creator_dm' },
  enabled: true,
  next_tick_at: '2026-09-15T15:30:00Z',
  last_tick: null,
  failure_streak: 0,
  created_at: '',
  updated_at: '',
  ...overrides,
});

/** Renders the pane with a captured imperative handle. */
function renderPane(props: Partial<Parameters<typeof HeartbeatPane>[0]> = {}) {
  const ref = { current: null as HeartbeatPaneHandle | null };
  const view = render(
    <HeartbeatPane ws="acme" agentId="radar" tz="UTC" canWrite ref={ref} {...props} />
  );
  return { ref, ...view };
}

const cadenceSelect = () => screen.getByTestId('heartbeat-cadence-select') as HTMLSelectElement;
const cadenceTime = () => screen.getByTestId('heartbeat-cadence-time') as HTMLInputElement;
const cronInput = () => screen.getByTestId('heartbeat-cron-input') as HTMLInputElement;

/** Waits for the initial read, enables the heartbeat, and opens Advanced. */
async function enableAndOpenAdvanced() {
  await waitFor(() => {
    expect((screen.getByTestId('heartbeat-prompt') as HTMLTextAreaElement).value).toBe('TEMPLATE');
  });
  fireEvent.click(screen.getByRole('switch', { name: 'Enable heartbeat' }));
  fireEvent.click(screen.getByTestId('heartbeat-advanced-toggle'));
  expect(screen.getByTestId('heartbeat-advanced')).not.toBeNull();
}

describe('modals/HeartbeatPane', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api.channels, 'list').mockResolvedValue({ channels: [] });
  });

  it('A1 never-created: off state with the checklist seeded from default_prompt and fields disabled', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: null, default_prompt: 'TEMPLATE' });
    renderPane();

    await waitFor(() => {
      expect((screen.getByTestId('heartbeat-prompt') as HTMLTextAreaElement).value).toBe('TEMPLATE');
    });

    // Off: the toggle starts unchecked and the form is dimmed but present.
    expect(
      screen.getByRole('switch', { name: 'Enable heartbeat' }).getAttribute('aria-checked')
    ).toBe('false');
    expect(screen.getByTestId('heartbeat-off-hint').textContent).toContain('Heartbeat is off');

    // Fields visible but disabled; Advanced collapsed.
    expect(cadenceSelect().disabled).toBe(true);
    expect((screen.getByTestId('heartbeat-prompt') as HTMLTextAreaElement).disabled).toBe(true);
    expect(screen.queryByTestId('heartbeat-advanced')).toBeNull();
    expect((screen.getByTestId('heartbeat-advanced-toggle') as HTMLButtonElement).disabled).toBe(true);

    // Enabling unlocks the form, Advanced included.
    fireEvent.click(screen.getByRole('switch', { name: 'Enable heartbeat' }));
    expect(cadenceSelect().disabled).toBe(false);
    expect((screen.getByTestId('heartbeat-prompt') as HTMLTextAreaElement).disabled).toBe(false);
    fireEvent.click(screen.getByTestId('heartbeat-advanced-toggle'));
    expect(screen.getByTestId('heartbeat-advanced')).not.toBeNull();
    expect((screen.getByTestId('heartbeat-active-start') as HTMLInputElement).disabled).toBe(false);
    expect((screen.getByTestId('heartbeat-active-end') as HTMLInputElement).disabled).toBe(false);
  });

  it('A2 on healthy: shows the cadence line and tick status for a saved heartbeat', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({
      heartbeat: hbRow({
        last_tick: {
          status: 'completed',
          trigger: 'tick',
          started_at: '2026-09-15T14:00:00Z',
          duration_ms: 42000,
          tokens_used: 18200,
          session_id: 'hb_radar',
          delivery_status: 'delivered',
        },
      }),
      default_prompt: 'TEMPLATE',
    });
    renderPane();

    await waitFor(() => {
      const line = screen.getByTestId('heartbeat-cadence-line');
      expect(line.textContent).toContain('every 30 minutes');
      expect(line.textContent).toContain('Next tick');
    });
    expect(screen.getByTestId('heartbeat-cadence-line').textContent).toContain('(');

    const status = screen.getByTestId('heartbeat-status');
    expect(status.textContent).toContain('Last tick');
    expect(status.textContent).toContain('completed');
    expect(status.textContent).toContain('delivered');
    expect(status.textContent).toContain('18.2k tok');
    expect(status.textContent).toContain('42s');
    expect(status.textContent).not.toContain('Failures in a row');
  });

  it('A2 reports "No ticks yet" and the failure streak when a tick has not run', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({
      heartbeat: hbRow({ last_tick: null, failure_streak: 2 }),
      default_prompt: 'TEMPLATE',
    });
    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('heartbeat-status').textContent).toContain('No ticks yet');
    });
    expect(screen.getByTestId('heartbeat-failures').textContent).toBe('Failures in a row: 2');
  });

  it('A4 cadence dropdown: renders the friendly frequency options and defaults to Every 30 minutes', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: null, default_prompt: 'TEMPLATE' });
    renderPane();

    await waitFor(() => {
      expect((screen.getByTestId('heartbeat-prompt') as HTMLTextAreaElement).value).toBe('TEMPLATE');
    });

    const options = Array.from(cadenceSelect().options).map((o) => o.textContent);
    expect(options).toEqual([
      'Every 5 minutes',
      'Every 15 minutes',
      'Every 30 minutes',
      'Every hour',
      'Every 2 hours',
      'Every 4 hours',
      'Every 6 hours',
      'Every 12 hours',
      'Daily at…',
      'Weekly on…',
    ]);
    expect(cadenceSelect().value).toBe('every30');
    // Custom (cron) is a synthetic display-only entry that only exists while
    // a raw Advanced override is active — never listed in the pristine state.
    expect(options).not.toContain('Custom (cron)');
  });

  it('A4 Daily at…: reveals the time input and saves the composed `M H * * *` expression', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: null, default_prompt: 'TEMPLATE' });
    const putSpy = vi.spyOn(heartbeats, 'update').mockResolvedValue({ heartbeat: hbRow() });
    const { ref } = renderPane();

    fireEvent.click(screen.getByRole('switch', { name: 'Enable heartbeat' }));
    fireEvent.change(cadenceSelect(), { target: { value: 'daily' } });
    expect(cadenceTime().value).toBe('09:00');

    fireEvent.change(cadenceTime(), { target: { value: '09:30' } });
    let ok = false;
    await act(async () => {
      ok = await ref.current!.save();
    });
    expect(ok).toBe(true);
    expect(putSpy).toHaveBeenCalledWith('acme', 'radar', expect.objectContaining({
      enabled: true,
      expr: '30 9 * * *',
    }));
  });

  it('A4 Weekly on…: reveals time + day chips (Mon–Fri pre-checked), requires a day, and saves cron day numbers', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: null, default_prompt: 'TEMPLATE' });
    const putSpy = vi.spyOn(heartbeats, 'update').mockResolvedValue({ heartbeat: hbRow() });
    const { ref } = renderPane();

    fireEvent.click(screen.getByRole('switch', { name: 'Enable heartbeat' }));
    fireEvent.change(cadenceSelect(), { target: { value: 'weekly' } });

    // Fresh switch: the time input and all seven chips render, Mon–Fri on.
    expect(cadenceTime().value).toBe('09:00');
    for (const day of ['mon', 'tue', 'wed', 'thu', 'fri']) {
      expect(screen.getByTestId('heartbeat-day-' + day).getAttribute('aria-pressed')).toBe('true');
    }
    expect(screen.getByTestId('heartbeat-day-sat').getAttribute('aria-pressed')).toBe('false');
    expect(screen.getByTestId('heartbeat-day-sun').getAttribute('aria-pressed')).toBe('false');
    expect(screen.queryByTestId('heartbeat-cadence-error')).toBeNull();

    // Unchecking every day blocks the save with the inline requirement.
    for (const day of ['mon', 'tue', 'wed', 'thu', 'fri']) {
      fireEvent.click(screen.getByTestId('heartbeat-day-' + day));
    }
    expect(screen.getByTestId('heartbeat-cadence-error').textContent).toBe('Pick at least one day.');
    let ok = true;
    await act(async () => {
      ok = await ref.current!.save();
    });
    expect(ok).toBe(false);
    expect(putSpy).not.toHaveBeenCalled();

    // Switching away and back re-pre-checks the fresh Mon–Fri set.
    fireEvent.change(cadenceSelect(), { target: { value: 'daily' } });
    fireEvent.change(cadenceSelect(), { target: { value: 'weekly' } });
    expect(screen.queryByTestId('heartbeat-cadence-error')).toBeNull();
    expect(screen.getByTestId('heartbeat-day-mon').getAttribute('aria-pressed')).toBe('true');

    // Leaving only Tuesday checked PUTs the cron day number (Tue=2).
    for (const day of ['mon', 'wed', 'thu', 'fri']) {
      fireEvent.click(screen.getByTestId('heartbeat-day-' + day));
    }
    await act(async () => {
      ok = await ref.current!.save();
    });
    expect(ok).toBe(true);
    expect(putSpy).toHaveBeenCalledWith('acme', 'radar', expect.objectContaining({
      expr: '0 9 * * 2',
    }));
  });

  it('A4 hydration: `*/30 * * * *` maps to Every 30 minutes', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({
      heartbeat: hbRow({ expr: '*/30 * * * *' }),
      default_prompt: 'TEMPLATE',
    });
    renderPane();

    await waitFor(() => {
      expect(cadenceSelect().value).toBe('every30');
    });
  });

  it('A4 hydration: `0 9 * * 1-5` maps to Weekly on… Mon–Fri at 09:00', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({
      heartbeat: hbRow({ expr: '0 9 * * 1-5' }),
      default_prompt: 'TEMPLATE',
    });
    renderPane();

    await waitFor(() => {
      expect(cadenceSelect().value).toBe('weekly');
    });
    expect(cadenceTime().value).toBe('09:00');
    for (const day of ['mon', 'tue', 'wed', 'thu', 'fri']) {
      expect(screen.getByTestId('heartbeat-day-' + day).getAttribute('aria-pressed')).toBe('true');
    }
    expect(screen.getByTestId('heartbeat-day-sun').getAttribute('aria-pressed')).toBe('false');
  });

  it('A4 hydration: single-time daily exprs map to Daily at… (`30 14 * * *` → 14:30, `37 5 * * *` → 05:37)', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({
      heartbeat: hbRow({ expr: '30 14 * * *' }),
      default_prompt: 'TEMPLATE',
    });
    const { unmount } = renderPane();
    await waitFor(() => {
      expect(cadenceSelect().value).toBe('daily');
    });
    expect(cadenceTime().value).toBe('14:30');
    unmount();

    vi.spyOn(heartbeats, 'get').mockResolvedValue({
      heartbeat: hbRow({ expr: '37 5 * * *' }),
      default_prompt: 'TEMPLATE',
    });
    renderPane();
    await waitFor(() => {
      expect(cadenceSelect().value).toBe('daily');
    });
    expect(cadenceTime().value).toBe('05:37');
  });

  it('A4 hydration: a non-representable expr selects Custom (cron), opens Advanced, and surfaces the raw expr', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({
      heartbeat: hbRow({ expr: '30 * * * *' }), // hourly at :30 — no dropdown option
      default_prompt: 'TEMPLATE',
    });
    renderPane();

    await waitFor(() => {
      expect(cadenceSelect().value).toBe('custom');
    });
    expect(screen.getByTestId('heartbeat-advanced-toggle').getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByTestId('heartbeat-advanced')).not.toBeNull();
    expect(cronInput().value).toBe('30 * * * *');
    expect(screen.queryByTestId('heartbeat-expr-error')).toBeNull();
  });

  it('A4 advanced is collapsed by default and auto-opens when the saved heartbeat has active hours', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({
      heartbeat: hbRow({ active_start: '09:00', active_end: '18:00' }),
      default_prompt: 'TEMPLATE',
    });
    const { unmount } = renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('heartbeat-prompt') as HTMLTextAreaElement);
    });
    expect(screen.getByTestId('heartbeat-advanced')).not.toBeNull();
    expect((screen.getByTestId('heartbeat-active-start') as HTMLInputElement).value).toBe('09:00');
    unmount();

    // Without saved advanced state the section stays collapsed behind the toggle.
    vi.spyOn(heartbeats, 'get').mockResolvedValue({
      heartbeat: hbRow(),
      default_prompt: 'TEMPLATE',
    });
    renderPane();
    await waitFor(() => {
      expect(cadenceSelect().value).toBe('every30');
    });
    expect(screen.queryByTestId('heartbeat-advanced')).toBeNull();
    expect(screen.getByTestId('heartbeat-advanced-toggle').textContent).toContain('▸ Advanced settings');

    fireEvent.click(screen.getByTestId('heartbeat-advanced-toggle'));
    expect(screen.getByTestId('heartbeat-advanced')).not.toBeNull();
    expect(screen.getByTestId('heartbeat-advanced-toggle').textContent).toContain('▾ Advanced settings');
  });

  it('A4 advanced cron override: floor violation shows the inline error, selects Custom (cron), and blocks the save', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: null, default_prompt: 'TEMPLATE' });
    const putSpy = vi.spyOn(heartbeats, 'update').mockResolvedValue({ heartbeat: hbRow() });
    const { ref } = renderPane();
    await enableAndOpenAdvanced();

    fireEvent.change(cronInput(), { target: { value: '*/2 * * * *' } });

    expect(cadenceSelect().value).toBe('custom');
    expect(screen.getByTestId('heartbeat-expr-error').textContent).toBe(
      'Minimum cadence is every 5 minutes.'
    );
    expect(screen.queryByTestId('heartbeat-next-runs')).toBeNull();

    let ok = true;
    await act(async () => {
      ok = await ref.current!.save();
    });
    expect(ok).toBe(false);
    expect(putSpy).not.toHaveBeenCalled();
  });

  it('A4 advanced cron override: a valid expression renders the three-run preview and wins on save', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: null, default_prompt: 'TEMPLATE' });
    const putSpy = vi.spyOn(heartbeats, 'update').mockResolvedValue({ heartbeat: hbRow() });
    const { ref } = renderPane();
    await enableAndOpenAdvanced();

    fireEvent.change(cronInput(), { target: { value: '0 */6 * * *' } });
    await waitFor(() => {
      expect(screen.getByTestId('heartbeat-next-runs')).not.toBeNull();
    });
    const items = screen.getByTestId('heartbeat-next-runs').querySelectorAll('li');
    expect(items.length).toBe(3);
    expect(screen.queryByTestId('heartbeat-expr-error')).toBeNull();

    let ok = false;
    await act(async () => {
      ok = await ref.current!.save();
    });
    expect(ok).toBe(true);
    expect(putSpy).toHaveBeenCalledWith('acme', 'radar', expect.objectContaining({
      expr: '0 */6 * * *',
    }));
  });

  it('A4 advanced cron override: picking a dropdown option clears the override and the dropdown wins', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: null, default_prompt: 'TEMPLATE' });
    const putSpy = vi.spyOn(heartbeats, 'update').mockResolvedValue({ heartbeat: hbRow() });
    const { ref } = renderPane();
    await enableAndOpenAdvanced();

    // The override wins while it holds a valid expression.
    fireEvent.change(cronInput(), { target: { value: '0 9 * * 1-5' } });
    expect(cadenceSelect().value).toBe('custom');
    let ok = false;
    await act(async () => {
      ok = await ref.current!.save();
    });
    expect(ok).toBe(true);
    expect(putSpy).toHaveBeenNthCalledWith(1, 'acme', 'radar', expect.objectContaining({
      expr: '0 9 * * 1-5',
    }));

    // A dropdown pick clears the override and takes over the payload.
    fireEvent.change(cadenceSelect(), { target: { value: 'every15' } });
    expect(cadenceSelect().value).toBe('every15');
    expect(cronInput().value).toBe('');
    await act(async () => {
      ok = await ref.current!.save();
    });
    expect(ok).toBe(true);
    expect(putSpy).toHaveBeenNthCalledWith(2, 'acme', 'radar', expect.objectContaining({
      expr: '*/15 * * * *',
    }));
  });

  it('A4 dropdown picks PUT their canonical expressions (Every 15 minutes → */15 * * * *)', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: null, default_prompt: 'TEMPLATE' });
    const putSpy = vi.spyOn(heartbeats, 'update').mockResolvedValue({ heartbeat: hbRow() });
    const { ref } = renderPane();

    await waitFor(() => {
      expect((screen.getByTestId('heartbeat-prompt') as HTMLTextAreaElement).value).toBe('TEMPLATE');
    });
    fireEvent.click(screen.getByRole('switch', { name: 'Enable heartbeat' }));
    fireEvent.change(cadenceSelect(), { target: { value: 'every15' } });

    let ok = false;
    await act(async () => {
      ok = await ref.current!.save();
    });
    expect(ok).toBe(true);
    expect(putSpy).toHaveBeenCalledWith('acme', 'radar', {
      enabled: true,
      expr: '*/15 * * * *',
      active_start: null,
      active_end: null,
      delivery: { type: 'creator_dm' },
      prompt: 'TEMPLATE',
    });
  });

  it('save is a no-op for a pristine pane (no accidental row creation) and PUTs once once touched', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: null, default_prompt: 'TEMPLATE' });
    const putSpy = vi.spyOn(heartbeats, 'update').mockResolvedValue({ heartbeat: hbRow() });
    const { ref } = renderPane();

    await waitFor(() => {
      expect((screen.getByTestId('heartbeat-prompt') as HTMLTextAreaElement).value).toBe('TEMPLATE');
    });

    // Pristine: save resolves true without a PUT.
    let ok = false;
    await act(async () => {
      ok = await ref.current!.save();
    });
    expect(ok).toBe(true);
    expect(putSpy).not.toHaveBeenCalled();

    // Touching (enable toggle) makes the next save PUT the exact payload.
    fireEvent.click(screen.getByRole('switch', { name: 'Enable heartbeat' }));
    await act(async () => {
      ok = await ref.current!.save();
    });
    expect(ok).toBe(true);
    expect(putSpy).toHaveBeenCalledTimes(1);
    expect(putSpy).toHaveBeenCalledWith('acme', 'radar', {
      enabled: true,
      expr: '*/30 * * * *',
      active_start: null,
      active_end: null,
      delivery: { type: 'creator_dm' },
      prompt: 'TEMPLATE',
    });
  });

  it('A3 delivery channel: picker renders from the channels API and a missing pick blocks the save', async () => {
    vi.spyOn(api.channels, 'list').mockResolvedValue({
      channels: [
        { id: 'ch-ops', workspace_id: 'acme', name: 'Ops', slug: 'ops', purpose: '', conventions: '', created_at: '', updated_at: '' },
      ],
    });
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: null, default_prompt: 'TEMPLATE' });
    const putSpy = vi.spyOn(heartbeats, 'update').mockResolvedValue({ heartbeat: hbRow() });
    const { ref } = renderPane();

    await waitFor(() => {
      expect((screen.getByTestId('heartbeat-prompt') as HTMLTextAreaElement).value).toBe('TEMPLATE');
    });
    fireEvent.click(screen.getByRole('switch', { name: 'Enable heartbeat' }));
    fireEvent.click(screen.getByTestId('heartbeat-delivery-channel'));

    const picker = screen.getByTestId('heartbeat-channel-select') as HTMLSelectElement;
    expect(picker).not.toBeNull();

    // Save without a pick: client-side hint, no PUT.
    let ok = true;
    await act(async () => {
      ok = await ref.current!.save();
    });
    expect(ok).toBe(false);
    expect(putSpy).not.toHaveBeenCalled();
    expect(screen.getByTestId('heartbeat-channel-error').textContent).toContain('Pick a channel');

    // Picking a channel unblocks the save with the exact delivery payload.
    fireEvent.change(picker, { target: { value: 'ch-ops' } });
    await act(async () => {
      ok = await ref.current!.save();
    });
    expect(ok).toBe(true);
    expect(putSpy).toHaveBeenCalledWith('acme', 'radar', expect.objectContaining({
      delivery: { type: 'channel', channel_id: 'ch-ops' },
    }));
  });

  it('maps server 422 field details next to the offending control', async () => {
    vi.spyOn(api.channels, 'list').mockResolvedValue({
      channels: [
        { id: 'ch-ops', workspace_id: 'acme', name: 'Ops', slug: 'ops', purpose: '', conventions: '', created_at: '', updated_at: '' },
      ],
    });
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: null, default_prompt: 'TEMPLATE' });
    vi.spyOn(heartbeats, 'update').mockRejectedValue(
      new ApiError(422, 'invalid_request', 'invalid request', [
        { field: 'delivery.channel_id', message: 'channel is required for channel delivery' },
      ])
    );
    const { ref } = renderPane();

    await waitFor(() => {
      expect((screen.getByTestId('heartbeat-prompt') as HTMLTextAreaElement).value).toBe('TEMPLATE');
    });
    fireEvent.click(screen.getByRole('switch', { name: 'Enable heartbeat' }));
    fireEvent.click(screen.getByTestId('heartbeat-delivery-channel'));
    fireEvent.change(screen.getByTestId('heartbeat-channel-select'), { target: { value: 'ch-ops' } });

    let ok = true;
    await act(async () => {
      ok = await ref.current!.save();
    });
    expect(ok).toBe(false);
    expect(screen.getByTestId('heartbeat-channel-error').textContent).toContain(
      'channel is required for channel delivery'
    );
  });

  it('A5 auto-paused: banner renders and Resume POSTs immediately then re-GETs', async () => {
    const getSpy = vi.spyOn(heartbeats, 'get')
      // Initial read: auto-paused. The post-resume refresh reads the
      // re-enabled row back from the server.
      .mockResolvedValueOnce({
        heartbeat: hbRow({ enabled: false, failure_streak: 5, next_tick_at: null }),
        default_prompt: 'TEMPLATE',
      })
      .mockResolvedValue({
        heartbeat: hbRow({ enabled: true, failure_streak: 5, next_tick_at: '2026-09-15T16:00:00Z' }),
        default_prompt: 'TEMPLATE',
      });
    vi.spyOn(heartbeats, 'resume').mockResolvedValue({
      heartbeat: hbRow({ enabled: true, next_tick_at: '2026-09-15T16:00:00Z' }),
    });
    renderPane();

    await waitFor(() => {
      const banner = screen.queryByTestId('heartbeat-paused-banner');
      expect(banner).not.toBeNull();
      expect(banner!.textContent).toContain('Auto-paused — 5 ticks failed in a row');
    });
    expect(screen.getByTestId('heartbeat-resume')).not.toBeNull();

    fireEvent.click(screen.getByTestId('heartbeat-resume'));

    await waitFor(() => {
      expect(heartbeats.resume).toHaveBeenCalledWith('acme', 'radar');
      expect(screen.queryByTestId('heartbeat-paused-banner')).toBeNull();
    });
    // Resume acts immediately, then the pane re-reads the heartbeat.
    await waitFor(() => {
      expect(getSpy).toHaveBeenCalledTimes(2);
    });
  });

  it('A6 run now: disabled with "Running…" while pending, refreshes after success', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: hbRow(), default_prompt: 'TEMPLATE' });
    let resolveRun: (v: unknown) => void = () => {};
    vi.spyOn(heartbeats, 'runNow').mockImplementation(
      () => new Promise((resolve) => { resolveRun = resolve; })
    );
    const getSpy = vi.spyOn(heartbeats, 'get');
    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('heartbeat-run-now')).not.toBeNull();
    });
    const initialGets = getSpy.mock.calls.length;

    fireEvent.click(screen.getByTestId('heartbeat-run-now'));
    expect((screen.getByTestId('heartbeat-run-now') as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByTestId('heartbeat-run-now').textContent).toBe('Running…');

    await act(async () => {
      resolveRun({ run: {} });
    });
    await waitFor(() => {
      expect((screen.getByTestId('heartbeat-run-now') as HTMLButtonElement).disabled).toBe(false);
    });
    expect(heartbeats.runNow).toHaveBeenCalledWith('acme', 'radar');
    expect(getSpy.mock.calls.length).toBeGreaterThan(initialGets);
  });

  it('A6 run now: a 409 while a tick is in flight shows the inline notice', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: hbRow(), default_prompt: 'TEMPLATE' });
    vi.spyOn(heartbeats, 'runNow').mockRejectedValue(
      new ApiError(409, 'conflict', 'a tick is already in flight')
    );
    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('heartbeat-run-now')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('heartbeat-run-now'));

    await waitFor(() => {
      expect(screen.getByTestId('heartbeat-run-notice').textContent).toBe('A tick is already running.');
    });
  });

  it('A7 read-only: summary lines and checklist preview render, no controls do', async () => {
    vi.spyOn(api.channels, 'list').mockResolvedValue({
      channels: [
        { id: 'ch-ops', workspace_id: 'acme', name: 'Ops', slug: 'ops', purpose: '', conventions: '', created_at: '', updated_at: '' },
      ],
    });
    vi.spyOn(heartbeats, 'get').mockResolvedValue({
      heartbeat: hbRow({
        active_start: '09:00',
        active_end: '18:00',
        delivery: { type: 'channel', channel_id: 'ch-ops' },
        failure_streak: 5,
      }),
      default_prompt: 'TEMPLATE',
    });
    renderPane({ canWrite: false });

    await waitFor(() => {
      expect(screen.getByTestId('heartbeat-summary')).not.toBeNull();
    });

    const summary = screen.getByTestId('heartbeat-summary').textContent;
    expect(summary).toContain('every 30 minutes');
    expect(summary).toContain('Next tick');
    expect(summary).toContain('#ops');
    expect(summary).toContain('09:00–18:00 (UTC)');
    expect(screen.getByTestId('heartbeat-status').textContent).toContain('Failures in a row: 5');

    // Checklist renders as a plain preview, not an editable textarea.
    expect(screen.getByTestId('heartbeat-prompt-preview').textContent).toContain('Check the dashboards');
    expect(screen.queryByTestId('heartbeat-prompt')).toBeNull();

    // Every write affordance is absent — including the cadence select and the
    // advanced collapsible.
    expect(screen.queryByTestId('heartbeat-toggle')).toBeNull();
    expect(screen.queryByTestId('heartbeat-cadence-select')).toBeNull();
    expect(screen.queryByTestId('heartbeat-advanced-toggle')).toBeNull();
    expect(screen.queryByRole('switch', { name: 'Enable heartbeat' })).toBeNull();
    expect(screen.queryByTestId('heartbeat-run-now')).toBeNull();
    expect(screen.queryByTestId('heartbeat-run-now-banner')).toBeNull();
    expect(screen.queryByTestId('heartbeat-resume')).toBeNull();
    expect(screen.queryByTestId('heartbeat-reset-prompt')).toBeNull();
    expect(screen.getByTestId('heartbeat-readonly-note').textContent).toContain(
      'You can view this heartbeat but not edit it.'
    );
  });

  it('A7 read-only with no heartbeat: empty note only', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: null, default_prompt: 'TEMPLATE' });
    renderPane({ canWrite: false });

    await waitFor(() => {
      expect(screen.getByTestId('heartbeat-none')).not.toBeNull();
    });
    expect(screen.queryByTestId('heartbeat-toggle')).toBeNull();
    expect(screen.queryByTestId('heartbeat-run-now')).toBeNull();
  });

  it('active hours: an equal start/end pair is rejected and blocks the save', async () => {
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: null, default_prompt: 'TEMPLATE' });
    const putSpy = vi.spyOn(heartbeats, 'update').mockResolvedValue({ heartbeat: hbRow() });
    const { ref } = renderPane();
    await enableAndOpenAdvanced();

    fireEvent.change(screen.getByTestId('heartbeat-active-start'), { target: { value: '09:00' } });
    // Only one bound set: the pair rule warns live.
    expect(screen.getByTestId('heartbeat-active-error').textContent).toContain('both a start and an end');

    fireEvent.change(screen.getByTestId('heartbeat-active-end'), { target: { value: '09:00' } });
    expect(screen.getByTestId('heartbeat-active-error').textContent).toContain('must not be equal');

    let ok = true;
    await act(async () => {
      ok = await ref.current!.save();
    });
    expect(ok).toBe(false);
    expect(putSpy).not.toHaveBeenCalled();
  });
});
