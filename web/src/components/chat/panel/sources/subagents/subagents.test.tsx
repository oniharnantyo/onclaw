/**
 * @vitest-environment jsdom
 */
// Subagents panel source tests (add-subagent-runs-surface 6.1/6.2): the two
// views (list sections + trajectory detail), the D4 stop gating and cancel
// path, the terminal badge watcher, and the candidate matching that puts the
// panel on `agent`/`background_shell` cards. Registration is a module-load
// side effect of importing ./index, exactly like the other built-in sources —
// this suite imports it and never resets the registries.
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, fireEvent, render, waitFor } from '@testing-library/react';
import { matchPanelCandidate, renderPanelSource } from '../../../../../lib/panel/registry';
import { api } from '../../../../../lib/api';
import { useStore } from '../../../../../store';
import { applyTaskCompleted, applyTaskFrame, applyTaskList, type TaskEntry } from '../../../../../lib/tasks';
import { terminalBadgeDue } from './view';
import { resetFirstSeenForTests } from './SubagentsSource';
import { resetTaskBadgeWatcherForTests } from './watcher';
import './index';

const CHAT = 'a-atlas';

const listTab = (over: Record<string, unknown> = {}) => ({
  id: 't1', kind: 'subagents', title: 'Subagents', payload: { chatId: CHAT }, dedupKey: 'subagents::list', ...over,
});
const detailTab = (taskId: string) => ({
  id: 't2', kind: 'subagents', title: 'Subagents', payload: { chatId: CHAT, taskId }, dedupKey: 'subagents::' + taskId,
});

const ctx = { workspaceId: 'acme', agentSlug: 'ag-1' };

const entry = (over: Partial<TaskEntry> = {}): TaskEntry => ({
  taskId: 'task-run', kind: 'delegation', status: 'running', description: 'Research the vendor landscape', ...over,
});

const seedEntries = (list: TaskEntry[]) => applyTaskList(CHAT, list);

/** A resolvable session address for the replay/cancel endpoints. */
const seedSession = () =>
  useStore.setState((s: any) => ({
    db: {
      ...s.db,
      acme: {
        ...s.db.acme,
        threads: {
          ...(s.db.acme?.threads || {}),
          [CHAT]: { active: 's1', list: [{ id: 's1', sess: 'sess_123' }] },
        },
      },
    },
  }));

const renderSource = (tab: any, overCtx: Record<string, unknown> = {}) =>
  render(<div>{renderPanelSource('subagents', { tab, ctx: { ...ctx, ...overCtx } })}</div>);

const frame = (
  sequence: number,
  record: { type: 'text' | 'tool_call' | 'tool_result'; text?: string; name?: string; args?: string; result?: string }
) => ({
  task_id: 'task-run', kind: 'delegation', sequence, record,
});

beforeEach(() => {
  vi.restoreAllMocks();
  resetFirstSeenForTests();
  resetTaskBadgeWatcherForTests();
  useStore.setState({
    tasks: {},
    panel: { open: false, tabs: [], activeId: null, badge: false },
    pos: { ...useStore.getState().pos, tenantId: 'acme', chatId: CHAT },
  });
});

describe('panel sources/subagents — registration + candidate matching (6.1)', () => {
  it('is registered under the subagents kind', () => {
    expect(renderPanelSource('subagents', { tab: listTab(), ctx: {} })).not.toBeNull();
  });

  it('agent and background_shell cards match; other tools stay null', () => {
    const agent = matchPanelCandidate({ name: 'agent', args: '{"description":"Research X"}' });
    expect(agent).toMatchObject({ kind: 'subagents', title: 'Subagents', payload: {} });
    const shell = matchPanelCandidate({ name: 'background_shell', args: '{"command":"npm test"}' });
    expect(shell).toMatchObject({ kind: 'subagents', title: 'Subagents' });
    expect(matchPanelCandidate({ name: 'execute', args: '{"command":"ls"}' })).toBeNull();
    expect(matchPanelCandidate({ name: 'web.search', args: '{"query":"x"}' })).toBeNull();
  });
});

describe('panel sources/subagents — list view (6.1)', () => {
  it('sections Running / Ended with name, kind badge, elapsed, outcome, relative time, snippet', () => {
    seedEntries([
      entry(),
      entry({ taskId: 'task-done', kind: 'shell', status: 'completed', description: 'npm test -- --coverage', summary: 'All 42 tests passed' }),
    ]);
    const { container } = renderSource(listTab());
    expect(container.textContent).toContain('Running · 1');
    expect(container.textContent).toContain('Ended · 1');
    // Running delegation row: honest 'Agent' name, kind badge, elapsed.
    const run = container.querySelector('[data-testid="panel-subagent-task-run"]')!;
    expect(run.textContent).toContain('Agent');
    expect(run.textContent).toContain('delegation');
    expect(container.querySelector('[data-testid="panel-subagent-elapsed"]')!.textContent).toMatch(/^\d/);
    expect(run.textContent).toContain('Research the vendor landscape');
    // Ended shell row: command-excerpt name, outcome chip, first-seen age,
    // two-line snippet from the summary.
    const done = container.querySelector('[data-testid="panel-subagent-task-done"]')!;
    expect(done.textContent).toContain('npm test -- --coverage');
    expect(done.textContent).toContain('shell');
    expect(container.querySelector('[data-testid="panel-subagent-status-completed"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="panel-subagent-seen"]')!.textContent).toContain('just now');
    expect(done.textContent).toContain('All 42 tests passed');
  });

  it('an empty session renders the present-only empty state', () => {
    const { container } = renderSource(listTab());
    expect(container.querySelector('[data-testid="panel-subagents-empty"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="panel-subagents-list"]')).toBeNull();
  });

  it('running rows carry a live preview strip fed by the progress frames (tail window)', () => {
    seedEntries([entry()]);
    const { container } = renderSource(listTab());
    expect(container.querySelector('[data-testid="panel-subagent-preview"]')!.textContent).toContain('Waiting for progress…');
    act(() => {
      applyTaskFrame(CHAT, frame(1, { type: 'text', text: 'Step one: reading sources' }));
      applyTaskFrame(CHAT, frame(2, { type: 'tool_call', name: 'web.search' }));
      applyTaskFrame(CHAT, frame(3, { type: 'tool_result', result: '10 results, first is onclaw.dev' }));
      applyTaskFrame(CHAT, frame(4, { type: 'text', text: 'Step two: comparing vendors' }));
    });
    const lines = [...container.querySelectorAll('[data-testid^="panel-subagent-preview-line-"]')];
    expect(lines).toHaveLength(3); // tail window ~3 records — record 1 slid out
    expect(lines[0].textContent).toContain('web.search');
    expect(lines[1].textContent).toContain('10 results');
    expect(lines[2].textContent).toContain('Step two: comparing vendors');
  });

  it('a row click swaps to the detail, back returns to the list', () => {
    seedEntries([entry(), entry({ taskId: 'task-done', status: 'failed' })]);
    const { container } = renderSource(listTab());
    fireEvent.click(container.querySelector('[data-testid="panel-subagent-task-run"]')!);
    expect(container.querySelector('[data-testid="panel-subagent-detail"]')).not.toBeNull();
    fireEvent.click(container.querySelector('[data-testid="panel-subagents-back"]')!);
    expect(container.querySelector('[data-testid="panel-subagents-list"]')).not.toBeNull();
  });

  it('a payload taskId opens the detail directly; a different tab resets the view to its own state', () => {
    seedEntries([entry()]);
    const { container, rerender } = renderSource(detailTab('task-run'));
    expect(container.querySelector('[data-testid="panel-subagent-detail"]')).not.toBeNull();
    // Back still returns to the list inside this tab.
    fireEvent.click(container.querySelector('[data-testid="panel-subagents-back"]')!);
    expect(container.querySelector('[data-testid="panel-subagents-list"]')).not.toBeNull();
    // A NEW tab identity remounts the views: same payload family, fresh state.
    rerender(<div>{renderPanelSource('subagents', { tab: { ...listTab(), id: 't9' }, ctx })}</div>);
    expect(container.querySelector('[data-testid="panel-subagents-list"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="panel-subagent-detail"]')).toBeNull();
  });
});

describe('panel sources/subagents — trajectory detail (6.1)', () => {
  it('a running task renders its live records: text blocks, tool calls, tool results, count', () => {
    seedEntries([entry()]);
    applyTaskFrame(CHAT, frame(1, { type: 'text', text: 'Starting the vendor scan' }));
    applyTaskFrame(CHAT, frame(2, { type: 'tool_call', name: 'web.search', args: '{"query":"vendors"}' }));
    applyTaskFrame(CHAT, frame(3, { type: 'tool_result', result: '10 results' }));
    const { container } = renderSource(detailTab('task-run'));
    expect(container.querySelector('[data-testid="panel-subagent-detail"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="panel-subagent-text"]')!.textContent).toContain('Starting the vendor scan');
    expect(container.querySelectorAll('[data-testid="panel-subagent-tool-call"]')).toHaveLength(1);
    expect(container.querySelector('[data-testid="panel-subagent-tool-call"]')!.textContent).toContain('web.search');
    expect(container.querySelector('[data-testid="panel-subagent-tool-result"]')!.textContent).toContain('10 results');
    expect(container.querySelector('[data-testid="panel-subagent-tool-count"]')!.textContent).toContain('1 tool call');
  });

  it('an ended task replays from the trajectory endpoint', async () => {
    seedSession();
    seedEntries([entry({ taskId: 'task-done', status: 'completed' })]);
    const out = vi.spyOn(api.agents, 'taskOutput').mockResolvedValue({
      task_id: 'task-done',
      records: [
        { sequence: 1, type: 'text', text: 'Replayed child text' },
        { sequence: 2, type: 'tool_call', name: 'files.write', args: '{"path":"x.md"}' },
        { sequence: 3, type: 'tool_result', result: 'wrote 12 bytes' },
        { sequence: 4, type: 'tool_call', name: 'web.search' },
      ],
      terminal: { outcome: 'completed' },
    });
    const { container } = renderSource(detailTab('task-done'));
    await waitFor(() => {
      expect(container.querySelector('[data-testid="panel-subagent-trajectory"]')).not.toBeNull();
    });
    expect(out).toHaveBeenCalledWith('acme', 'ag-1', 'sess_123', 'task-done');
    expect(container.querySelector('[data-testid="panel-subagent-text"]')!.textContent).toContain('Replayed child text');
    expect(container.querySelectorAll('[data-testid="panel-subagent-tool-call"]')).toHaveLength(2);
    expect(container.querySelector('[data-testid="panel-subagent-tool-count"]')!.textContent).toContain('2 tool calls');
  });

  it('pruned output (404) renders the clear not-found state', async () => {
    seedSession();
    seedEntries([entry({ taskId: 'task-gone', status: 'completed' })]);
    vi.spyOn(api.agents, 'taskOutput').mockRejectedValue(Object.assign(new Error('not found'), { status: 404 }));
    const { container } = renderSource(detailTab('task-gone'));
    await waitFor(() => {
      expect(container.querySelector('[data-testid="panel-subagent-replay-missing"]')).not.toBeNull();
    });
    expect(container.textContent).toContain('pruned');
  });

  it('the list fold repopulates ended tasks after a reload and the detail stays replayable', async () => {
    // Post-reload shape: entries arrive ONLY through the REST list fold.
    seedSession();
    vi.spyOn(api.agents, 'listTasks').mockResolvedValue({
      tasks: [{ task_id: 'task-done', kind: 'delegation', status: 'completed', description: 'Research the vendor landscape' }],
    });
    const out = vi.spyOn(api.agents, 'taskOutput').mockResolvedValue({
      task_id: 'task-done',
      records: [{ sequence: 1, type: 'text', text: 'Reloaded replay' }],
      terminal: { outcome: 'completed' },
    });
    renderSource(detailTab('task-done'));
    await waitFor(() => {
      expect(out).toHaveBeenCalled();
    });
  });
});

describe('panel sources/subagents — stop control (6.2)', () => {
  it('enabled for a running background task; cancel flips the status everywhere without reload', async () => {
    seedSession();
    seedEntries([entry()]);
    const cancel = vi.spyOn(api.agents, 'cancelTask').mockResolvedValue({ cancelled: true });
    const { container } = renderSource(detailTab('task-run'));
    const stop = container.querySelector('[data-testid="panel-subagent-stop"]') as HTMLButtonElement;
    expect(stop.disabled).toBe(false);
    fireEvent.click(stop);
    await waitFor(() => {
      expect(cancel).toHaveBeenCalledWith('acme', 'ag-1', 'sess_123', 'task-run');
    });
    // Optimistic fold, no reload: the store entry itself flipped.
    await waitFor(() => {
      const stored = useStore.getState().tasks[CHAT].entries.find((e: TaskEntry) => e.taskId === 'task-run');
      expect(stored?.status).toBe('canceled');
    });
    expect(container.querySelector('[data-testid="panel-subagent-status-canceled"]')).not.toBeNull();
  });

  it('disabled for an ended task and never calls the endpoint', () => {
    seedSession();
    seedEntries([entry({ taskId: 'task-done', status: 'completed' })]);
    const cancel = vi.spyOn(api.agents, 'cancelTask').mockResolvedValue({ cancelled: true });
    const { container } = renderSource(detailTab('task-done'));
    const stop = container.querySelector('[data-testid="panel-subagent-stop"]') as HTMLButtonElement;
    expect(stop.disabled).toBe(true);
    fireEvent.click(stop);
    expect(cancel).not.toHaveBeenCalled();
  });

  it('a foreground delegation (no joinable entry) shows the disabled stop with the background-only explanation', () => {
    seedSession();
    seedEntries([entry()]); // some OTHER task exists — the addressed one does not
    const { container } = renderSource(detailTab('task-foreground'));
    expect(container.querySelector('[data-testid="panel-subagent-unknown"]')).not.toBeNull();
    const stop = container.querySelector('[data-testid="panel-subagent-stop"]') as HTMLButtonElement;
    expect(stop.disabled).toBe(true);
    expect(container.querySelector('[data-testid="panel-subagent-stop-note"]')!.textContent).toContain(
      'Only background tasks can be stopped'
    );
  });

  it('a 409 surfaces the non-fatal notice and folds a fresh list', async () => {
    seedSession();
    seedEntries([entry()]);
    vi.spyOn(api.agents, 'cancelTask').mockRejectedValue(Object.assign(new Error('conflict'), { status: 409 }));
    const list = vi.spyOn(api.agents, 'listTasks').mockResolvedValue({
      tasks: [{ task_id: 'task-run', kind: 'delegation', status: 'completed', description: 'Research the vendor landscape' }],
    });
    const { container } = renderSource(detailTab('task-run'));
    fireEvent.click(container.querySelector('[data-testid="panel-subagent-stop"]')!);
    await waitFor(() => {
      expect(container.querySelector('[data-testid="panel-subagent-notice"]')!.textContent).toContain('already ended');
    });
    await waitFor(() => {
      expect(list).toHaveBeenCalledWith('acme', 'ag-1', 'sess_123');
    });
    // The fresh list folded: the row truth moved from the stale running state.
    await waitFor(() => {
      const stored = useStore.getState().tasks[CHAT].entries.find((e: TaskEntry) => e.taskId === 'task-run');
      expect(stored?.status).toBe('completed');
    });
  });
});

describe('panel sources/subagents — terminal badge watcher (6.2)', () => {
  it('pure core: only an observed running→terminal transition is due', () => {
    const running = [entry()];
    const ended = [entry({ status: 'canceled' })];
    const created = [entry({ taskId: 't2', status: 'created' })];
    expect(terminalBadgeDue(running, ended)).toBe(true);
    expect(terminalBadgeDue(undefined, ended)).toBe(false); // history, not a finish
    expect(terminalBadgeDue(created, [entry({ taskId: 't2', status: 'failed' })])).toBe(false);
    expect(terminalBadgeDue(running, running)).toBe(false);
  });

  it('a task ending while the panel is closed sets the header badge', () => {
    seedEntries([entry()]); // baseline: watcher records the running state
    expect(useStore.getState().panel.badge).toBe(false);
    applyTaskCompleted(CHAT, { taskId: 'task-run', kind: 'delegation', outcome: 'canceled' });
    expect(useStore.getState().panel.badge).toBe(true);
  });

  it('a task ending while the Subagents source is actively viewed stays badge-free', () => {
    useStore.setState({
      panel: {
        open: true,
        activeId: 'view',
        tabs: [{ id: 'view', kind: 'subagents', title: 'Subagents', payload: { chatId: CHAT }, dedupKey: 'k' }],
        badge: false,
      },
    } as any);
    seedEntries([entry()]);
    applyTaskCompleted(CHAT, { taskId: 'task-run', kind: 'delegation', outcome: 'failed' });
    expect(useStore.getState().panel.badge).toBe(false);
  });

  it('ended tasks appearing without a prior running sighting never badge (reload/list-fold history)', () => {
    seedEntries([entry({ status: 'completed' })]);
    expect(useStore.getState().panel.badge).toBe(false);
  });
});
