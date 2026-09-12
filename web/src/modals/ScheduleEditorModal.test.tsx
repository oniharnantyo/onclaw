/**
 * @vitest-environment jsdom
 */
// Schedule editor modal (integrate-scheduler 7.5): the editor validation
// states from the spec — save gating on name/prompt, preset mode building the
// expression without showing cron notation, custom mode's inline error, and
// the once-mode future-instant rule.
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ScheduleEditorModal } from './ScheduleEditorModal';

vi.mock('../lib/schedulers', () => ({
  schedulers: {
    create: vi.fn().mockResolvedValue({ scheduler: {} }),
    update: vi.fn().mockResolvedValue({ scheduler: {} }),
    delete: vi.fn().mockResolvedValue(undefined),
  },
}));
// The modal toasts through the store; keep the real store but silence toasts.
vi.mock('../store', () => ({
  useStore: { getState: () => ({ toast: vi.fn(), updateTenant: vi.fn() }) },
}));
vi.mock('../lib/api', () => ({
  formatApiError: (e: unknown, m: string) => m,
  ApiError: class ApiError extends Error {},
}));

import { schedulers } from '../lib/schedulers';

const tenant = {
  id: 'acme',
  sub: 'acme',
  name: 'Acme Corp',
  tz: 'America/Los_Angeles',
  agents: [{ id: 'a-atlas', name: 'Atlas' }, { id: 'a-beacon', name: 'Beacon' }],
  channels: [{ id: 'ch-1', name: 'ops', slug: 'ops' }],
};

const draft = {
  id: 'sch-1', workspace_id: 'ws', agent_id: 'a-atlas', created_by: null,
  name: 'Morning ops digest', prompt: 'Digest the night',
  kind: 'recurring' as const, expr: '0 7 * * 1-5', run_at: null,
  delivery: { type: 'thread' as const }, enabled: true, next_run_at: null,
  last_run: null, human_label: '07:00 · Mon–Fri',
  created_at: '', updated_at: '',
};

const rendered = (props: any = {}) =>
  render(<ScheduleEditorModal draft={null} tenant={tenant} onClose={vi.fn()} onChanged={vi.fn()} {...props} />);

// Buttons carry the house data-od-id convention; inputs carry data-testid.
const saveBtn = () => document.querySelector('[data-od-id="btn-schedule-save"]') as HTMLButtonElement;
const deleteBtn = () => document.querySelector('[data-od-id="btn-schedule-delete"]') as HTMLButtonElement;

beforeEach(() => {
  vi.clearAllMocks();
});

describe('modals/ScheduleEditorModal — save gating (spec: editor requirement)', () => {
  it('disables save and shows the prompt error while the task prompt is empty', () => {
    rendered();
    fireEvent.change(screen.getByTestId('input-schedule-name'), { target: { value: 'Nightly sweep' } });
    expect(saveBtn().disabled).toBe(true);
    expect(screen.getByTestId('error-schedule-prompt').textContent).toMatch(/required/i);

    fireEvent.change(screen.getByTestId('input-schedule-prompt'), { target: { value: 'Sweep the queues' } });
    expect(saveBtn().disabled).toBe(false);
  });

  it('disables save while the name is a single character', () => {
    rendered();
    fireEvent.change(screen.getByTestId('input-schedule-name'), { target: { value: 'N' } });
    fireEvent.change(screen.getByTestId('input-schedule-prompt'), { target: { value: 'Do the thing' } });
    expect(saveBtn().disabled).toBe(true);

    fireEvent.change(screen.getByTestId('input-schedule-name'), { target: { value: 'Nightly sweep' } });
    expect(saveBtn().disabled).toBe(false);
  });

  it('builds the daily 09:00 Mon–Fri recurrence from presets without showing cron notation', () => {
    rendered();
    // Default mode is the builder: no raw expression input anywhere.
    expect(screen.queryByTestId('input-schedule-expr')).toBeNull();
    fireEvent.change(screen.getByTestId('input-schedule-name'), { target: { value: 'Morning ops digest' } });
    fireEvent.change(screen.getByTestId('input-schedule-prompt'), { target: { value: 'Digest the night' } });
    // The summary is human-readable.
    expect(screen.getByTestId('schedule-summary').textContent).toBe('09:00 · Mon–Fri');

    fireEvent.click(saveBtn());
    expect(schedulers.create).toHaveBeenCalledWith('acme', expect.objectContaining({
      name: 'Morning ops digest',
      agent_id: 'a-atlas',
      kind: 'recurring',
      expr: '0 9 * * 1-5',
      delivery: { type: 'thread' },
    }));
  });

  it('generates a weekly expression from day chips (no cron notation in preset mode)', () => {
    rendered();
    fireEvent.change(screen.getByTestId('input-schedule-name'), { target: { value: 'Midweek check' } });
    fireEvent.change(screen.getByTestId('input-schedule-prompt'), { target: { value: 'Check the queue' } });
    fireEvent.change(screen.getByTestId('select-schedule-freq'), { target: { value: 'weekly' } });
    // Uncheck Mon and Fri — leaves Tue, Wed, Thu (a generated 2-4 range).
    fireEvent.click(screen.getByTestId('day-chip-mon'));
    fireEvent.click(screen.getByTestId('day-chip-fri'));
    expect(screen.getByTestId('schedule-summary').textContent).toBe('09:00 · Tue–Thu');

    fireEvent.click(saveBtn());
    expect(schedulers.create).toHaveBeenCalledWith('acme', expect.objectContaining({
      expr: '0 9 * * 2-4',
    }));
  });

  it('validates the custom expression inline: save disabled and no preview for junk', () => {
    rendered();
    fireEvent.click(screen.getByTestId('seg-mode-custom'));
    fireEvent.change(screen.getByTestId('input-schedule-name'), { target: { value: 'Custom sweep' } });
    fireEvent.change(screen.getByTestId('input-schedule-prompt'), { target: { value: 'Sweep' } });

    fireEvent.change(screen.getByTestId('input-schedule-expr'), { target: { value: 'at nine' } });
    expect(saveBtn().disabled).toBe(true);
    expect(screen.getByTestId('error-schedule-expr')).not.toBeNull();
    expect(screen.queryByTestId('schedule-next-runs')).toBeNull();

    fireEvent.change(screen.getByTestId('input-schedule-expr'), { target: { value: '30 17 * * 5' } });
    expect(saveBtn().disabled).toBe(false);
    expect(screen.queryByTestId('error-schedule-expr')).toBeNull();
    // The next-runs preview appears once the expression parses.
    expect(screen.getByTestId('schedule-next-runs')).not.toBeNull();

    fireEvent.click(saveBtn());
    expect(schedulers.create).toHaveBeenCalledWith('acme', expect.objectContaining({
      kind: 'recurring', expr: '30 17 * * 5',
    }));
  });

  it('requires a future instant for once schedules', () => {
    rendered();
    fireEvent.click(screen.getByTestId('seg-mode-once'));
    fireEvent.change(screen.getByTestId('input-schedule-name'), { target: { value: 'One-shot sweep' } });
    fireEvent.change(screen.getByTestId('input-schedule-prompt'), { target: { value: 'Sweep once' } });

    fireEvent.change(screen.getByTestId('input-schedule-runat'), { target: { value: '2020-01-01T09:00' } });
    expect(saveBtn().disabled).toBe(true);
    expect(screen.getByTestId('error-schedule-runat').textContent).toMatch(/future/i);

    fireEvent.change(screen.getByTestId('input-schedule-runat'), { target: { value: '2099-01-01T09:00' } });
    expect(saveBtn().disabled).toBe(false);
    fireEvent.click(saveBtn());
    expect(schedulers.create).toHaveBeenCalledWith('acme', expect.objectContaining({
      kind: 'once',
      expr: '',
      run_at: expect.any(String),
    }));
  });

  it('offers a channel delivery target from the loaded channels', () => {
    rendered();
    fireEvent.change(screen.getByTestId('input-schedule-name'), { target: { value: 'Channel digest' } });
    fireEvent.change(screen.getByTestId('input-schedule-prompt'), { target: { value: 'Digest' } });
    fireEvent.click(screen.getByTestId('seg-delivery-channel'));
    fireEvent.change(screen.getByTestId('select-schedule-channel'), { target: { value: 'ch-1' } });

    fireEvent.click(saveBtn());
    expect(schedulers.create).toHaveBeenCalledWith('acme', expect.objectContaining({
      delivery: { type: 'channel', channel_id: 'ch-1' },
    }));
  });

  it('updates the existing row and shows the workspace timezone read-only', () => {
    rendered({ draft });
    expect((screen.getByTestId('input-schedule-name') as HTMLInputElement).value).toBe('Morning ops digest');
    const tz = screen.getByTestId('input-schedule-name').parentElement?.parentElement?.querySelector('#sch-tz') as HTMLInputElement;
    expect(tz.disabled).toBe(true);
    expect(tz.value).toBe('America/Los_Angeles');

    fireEvent.click(saveBtn());
    expect(schedulers.update).toHaveBeenCalledWith('acme', 'sch-1', expect.objectContaining({
      name: 'Morning ops digest', kind: 'recurring', expr: '0 7 * * 1-5',
    }));
    expect(schedulers.create).not.toHaveBeenCalled();
  });

  it('deletes an existing scheduler with a two-step confirm', async () => {
    const onClose = vi.fn();
    const onChanged = vi.fn();
    rendered({ draft, onClose, onChanged });
    const del = deleteBtn();
    fireEvent.click(del);
    // First click only arms the confirm.
    expect(schedulers.delete).not.toHaveBeenCalled();
    fireEvent.click(del);
    await vi.waitFor(() => {
      expect(schedulers.delete).toHaveBeenCalledWith('acme', 'sch-1');
      expect(onChanged).toHaveBeenCalled();
      expect(onClose).toHaveBeenCalled();
    });
    expect(deleteBtn().textContent).toMatch(/confirm/i);
  });

  it('opens a once draft in once mode so editing never flips kind', () => {
    const onceDraft = { ...draft, kind: 'once' as const, expr: '', run_at: '2099-01-01T17:00:00Z' };
    rendered({ draft: onceDraft });
    // The once panel is the visible recurrence control; the preset builder is not.
    expect(screen.getByTestId('input-schedule-runat')).not.toBeNull();
    expect(screen.queryByTestId('input-schedule-time')).toBeNull();
    // Saving keeps kind 'once' with an expr of "".
    fireEvent.click(saveBtn());
    expect(schedulers.update).toHaveBeenCalledWith('acme', 'sch-1', expect.objectContaining({
      kind: 'once', expr: '', run_at: expect.any(String),
    }));
  });
});
