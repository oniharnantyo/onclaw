/**
 * @vitest-environment jsdom
 */
// AgentMessage tool-call-list tests (adopt-assistant-ui-elements 6.2): earlier
// same-turn todo_write calls collapse to one-line "plan updated" summaries and
// only the newest renders the full checklist; a turn without todo_write
// renders nothing todo-related (present-only).
// Also covers tasks 10.1–10.3 (tool group timeline collapse) and 11.1–11.2
// (lazy remark-math + rehype-katex rendering in MarkdownBody), plus change
// fix-tool-timeline-fold: the fold owns reasoning rows too, and the streaming
// header shows the activity label with the client-measured turn elapsed.
import { describe, it, expect, vi } from 'vitest';
import { act, fireEvent, render, waitFor } from '@testing-library/react';
import { AgentMessage, containsMathDelimiters } from './AgentMessage';
import { changedFileCount } from './ToolTimelineHeader';
import { recordTurnTiming } from '../../chat/turnTiming';
import { useStore } from '../../store';

const todoWrite = (revision: number) => ({
  name: 'todo_write',
  args: JSON.stringify({
    revision,
    items: [
      { key: 'a', text: 'First', status: 'done' },
      { key: 'b', text: 'Second', status: 'pending' },
    ],
  }),
  res: JSON.stringify({ revision, items: [] }),
  ms: 3,
});

const read = (path: string) => ({
  name: 'read_file',
  args: JSON.stringify({ file_path: path }),
  res: 'content',
  ms: 2,
});
const edit = (path: string, error?: boolean) => ({
  name: 'edit_file',
  args: JSON.stringify({ file_path: path, old_string: 'a', new_string: 'b' }),
  res: 'ok',
  ms: 4,
  ...(error ? { error: true } : {}),
});
const write = (path: string) => ({
  name: 'write_file',
  args: JSON.stringify({ file_path: path, content: 'x' }),
  res: 'ok',
  ms: 2,
});
const search = (query: string) => ({
  name: 'web.search',
  args: JSON.stringify({ query }),
  res: 'no envelope',
  ms: 5,
});

// busy/isLast overrides (fix-tool-timeline-fold): streaming-turn tests pass
// { busy: true, isLast: true } without duplicating the AgentMessage markup —
// rerender with messageElement(m, otherOpts) for the stream→rest swap.
function messageElement(m: any, opts?: { busy?: boolean; isLast?: boolean }) {
  return (
    <AgentMessage
      m={m}
      agent={{ name: 'Atlas' }}
      inChannel={false}
      busy={opts?.busy ?? false}
      isLast={opts?.isLast ?? false}
      onCopy={vi.fn()}
      onRefresh={vi.fn()}
      onBranch={vi.fn()}
      members={[]}
    />
  );
}

function renderMessage(m: any, opts?: { busy?: boolean; isLast?: boolean }) {
  return render(messageElement(m, opts));
}

function renderTurn(tools: any[], parts?: any[]) {
  return renderMessage({ id: 'm1', text: '', tools, ...(parts ? { parts } : {}) });
}

const toolIds = (container: HTMLElement): string[] =>
  Array.from(container.querySelectorAll('[data-od-id]'))
    .map((el) => el.getAttribute('data-od-id') || '')
    .filter((id) => id.startsWith('tool-'));

describe('components/chat/AgentMessage — todo rewrite collapse (6.2)', () => {
  it('collapses earlier rewrites to summaries; only the newest renders the checklist', () => {
    const { container } = renderTurn([todoWrite(1), todoWrite(2), todoWrite(3)]);
    // First two: one-line "plan updated" summaries with their revision.
    expect(container.querySelector('[data-od-id="todo-updated-0"]')).not.toBeNull();
    expect(container.querySelector('[data-od-id="todo-updated-1"]')).not.toBeNull();
    expect(container.querySelector('[data-od-id="todo-updated-2"]')).toBeNull();
    const summaries = container.querySelectorAll('[data-od-id^="todo-updated-"]');
    expect(summaries).toHaveLength(2);
    expect(summaries[0].textContent).toContain('Plan updated');
    expect(summaries[0].textContent).toContain('rev 1');
    expect(summaries[1].textContent).toContain('rev 2');
    // Summaries never stack full checklists.
    summaries.forEach((s) => expect(s.querySelectorAll('li')).toHaveLength(0));
    // Only the newest renders the full checklist.
    const cards = container.querySelectorAll('[data-od-id="tool-todo_write"]');
    expect(cards).toHaveLength(1);
    expect(cards[0].querySelectorAll('li')).toHaveLength(2);
  });

  it('collapses through the ordered parts path too', () => {
    const { container } = renderTurn([todoWrite(1), todoWrite(2)], [
      { k: 'tool', i: 0 },
      { k: 'tool', i: 1 },
    ]);
    expect(container.querySelector('[data-od-id="todo-updated-0"]')).not.toBeNull();
    expect(container.querySelectorAll('[data-od-id="tool-todo_write"]')).toHaveLength(1);
  });

  it('a turn with no todo_write renders nothing todo-related (present-only)', () => {
    const { container } = renderTurn([
      { name: 'read_file', args: '{"file_path":"main.go"}', res: 'package main', ms: 5 },
    ]);
    expect(container.querySelector('[data-od-id="tool-todo_write"]')).toBeNull();
    expect(container.querySelector('[data-od-id^="todo-updated-"]')).toBeNull();
    expect(container.textContent).not.toContain('Plan updated');
  });

  it('a single todo_write renders the full checklist with no summary', () => {
    const { container } = renderTurn([todoWrite(1)]);
    expect(container.querySelector('[data-od-id^="todo-updated-"]')).toBeNull();
    expect(container.querySelectorAll('[data-od-id="tool-todo_write"]')).toHaveLength(1);
  });
});

describe('components/chat/AgentMessage — tool group timeline collapse (10.1–10.3)', () => {
  it('renders three calls inline with no collapse header (light-turn passthrough)', () => {
    const { container } = renderTurn([read('a.go'), read('b.go'), read('c.go')]);
    expect(container.querySelector('[data-od-id="tool-group-m1"]')).toBeNull();
    // Cards render inline exactly as before — none muted or hidden.
    expect(container.querySelectorAll('[data-od-id="tool-read_file"]')).toHaveLength(3);
    expect(toolIds(container)).toEqual(['tool-read_file', 'tool-read_file', 'tool-read_file']);
  });

  it('collapses a four-call turn behind the header by default', () => {
    const { container } = renderTurn([read('a.go'), read('b.go'), read('c.go'), read('d.go')]);
    const header = container.querySelector('[data-od-id="tool-group-m1"]');
    expect(header).not.toBeNull();
    expect(header!.getAttribute('aria-expanded')).toBe('false');
    expect(header!.textContent).toContain('4 steps');
    // No "files changed" clause when nothing was edited (present-only).
    expect(header!.textContent).not.toContain('files changed');
    expect(container.querySelectorAll('[data-od-id="tool-read_file"]')).toHaveLength(0);
  });

  it('counts changed files once per file in the resting summary (spec scenario)', () => {
    const { container } = renderTurn([
      read('a.go'),
      edit('src/x.go'),
      edit('src/x.go'), // same file twice — counted once
      edit('src/y.go'),
      write('src/z.go'),
      read('b.go'),
    ]);
    const header = container.querySelector('[data-od-id="tool-group-m1"]');
    expect(header!.textContent).toContain('6 steps · 3 files changed');
  });

  it('skips errored edits in the churn — a failed call changed nothing', () => {
    const { container } = renderTurn([
      read('a.go'),
      edit('src/x.go'),
      edit('src/boom.go', true),
      read('b.go'),
    ]);
    const header = container.querySelector('[data-od-id="tool-group-m1"]');
    expect(header!.textContent).toContain('4 steps · 1 file changed');
  });

  it('expands to the inline cards in stream order and collapses again', () => {
    const { container } = renderTurn([
      read('a.go'),
      edit('x.go'),
      search('onclaw'),
      write('z.go'),
    ]);
    expect(container.querySelectorAll('[data-od-id="tool-read_file"]')).toHaveLength(0);
    fireEvent.click(container.querySelector('[data-od-id="tool-group-m1"]')!);
    expect(toolIds(container)).toEqual([
      'tool-group-m1',
      'tool-read_file',
      'tool-edit_file',
      'tool-web.search',
      'tool-write_file',
    ]);
    // Toggling back re-hides the stack; state stays user-controlled.
    fireEvent.click(container.querySelector('[data-od-id="tool-group-m1"]')!);
    expect(container.querySelectorAll('[data-od-id="tool-read_file"]')).toHaveLength(0);
    expect(container.querySelectorAll('[data-od-id="tool-write_file"]')).toHaveLength(0);
  });

  it('keeps the todo rewrite collapse working inside the expanded body', () => {
    const { container } = renderTurn([todoWrite(1), todoWrite(2), read('a.go'), read('b.go')]);
    // Collapsed by default: no summaries, no cards.
    expect(container.querySelector('[data-od-id="todo-updated-0"]')).toBeNull();
    fireEvent.click(container.querySelector('[data-od-id="tool-group-m1"]')!);
    // The familiar body: earlier rewrites as summaries, newest as checklist.
    expect(container.querySelector('[data-od-id="todo-updated-0"]')).not.toBeNull();
    expect(container.querySelector('[data-od-id="todo-updated-1"]')).toBeNull();
    expect(container.querySelectorAll('[data-od-id="tool-todo_write"]')).toHaveLength(1);
  });

  it('drives the ordered parts path identically', () => {
    const { container } = renderTurn(
      [read('a.go'), read('b.go'), read('c.go'), read('d.go')],
      [{ k: 'tool', i: 0 }, { k: 'tool', i: 1 }, { k: 'tool', i: 2 }, { k: 'tool', i: 3 }]
    );
    expect(container.querySelector('[data-od-id="tool-group-m1"]')).not.toBeNull();
    expect(container.querySelectorAll('[data-od-id="tool-read_file"]')).toHaveLength(0);
  });

  it('nests the expanded body under the header behind the left rail, light turns flush (D8)', () => {
    const { container } = renderTurn([read('a.go'), read('b.go'), read('c.go'), read('d.go')]);
    const railSel = '.ms-3.border-s.border-linesoft.ps-3';
    // Collapsed: header only, no rail, no cards.
    expect(container.querySelector(railSel)).toBeNull();
    fireEvent.click(container.querySelector('[data-od-id="tool-group-m1"]')!);
    // Expanded: the cards live inside the rail container — the header's children.
    const rail = container.querySelector(railSel);
    expect(rail).not.toBeNull();
    expect(rail!.querySelectorAll('[data-od-id^="tool-"]').length).toBeGreaterThan(0);
    // Light turn: inline cards with no rail anywhere.
    const light = renderTurn([read('a.go'), read('b.go')]);
    expect(light.container.querySelector(railSel)).toBeNull();
    expect(light.container.querySelectorAll('[data-od-id^="tool-"]').length).toBe(2);
  });

  it('derives churn directly from raw card items', () => {
    expect(changedFileCount(undefined)).toBe(0);
    expect(changedFileCount([])).toBe(0);
    expect(changedFileCount([read('a.go'), read('b.go')])).toBe(0);
    expect(changedFileCount([edit('x.go'), edit('x.go'), edit('y.go')])).toBe(2);
    expect(changedFileCount([edit('x.go', true), write('x.go')])).toBe(1);
    expect(changedFileCount([{ name: 'edit_file', args: '{bad json' }])).toBe(0);
    expect(changedFileCount([edit('  spaced.go  ')])).toBe(1);
  });
});

describe('components/chat/AgentMessage — fold owns reasoning + streaming header (fix-tool-timeline-fold)', () => {
  const pendingExecute = { name: 'execute', args: JSON.stringify({ command: 'ls -la' }) };
  // Activity-stream ids in DOM order: the timeline header, interleaved tool
  // cards, and reasoning rows — everything the fold owns.
  const activityIds = (container: HTMLElement): string[] =>
    Array.from(container.querySelectorAll('[data-od-id]'))
      .map((el) => el.getAttribute('data-od-id') || '')
      .filter((id) => id.startsWith('tool-') || id.startsWith('msg-reasoning-'));

  it('a collapsed heavy turn hides reasoning rows along with the cards', () => {
    const { container } = renderTurn(
      [read('a.go'), edit('x.go'), read('b.go'), read('c.go')],
      [
        { k: 'reasoning', text: 'Looking at the workspace first.' },
        { k: 'tool', i: 0 },
        { k: 'reasoning', text: 'Now the edits.' },
        { k: 'tool', i: 1 },
        { k: 'tool', i: 2 },
        { k: 'tool', i: 3 },
      ]
    );
    // Header + text only: no orphaned Thought rows above a collapsed fold.
    expect(container.querySelector('[data-od-id="tool-group-m1"]')).not.toBeNull();
    expect(activityIds(container)).toEqual(['tool-group-m1']);
    expect(container.querySelector('[data-od-id^="msg-reasoning-"]')).toBeNull();
  });

  it('expanding reveals cards AND Thought rows interleaved in stream order', () => {
    const { container } = renderTurn(
      [read('a.go'), edit('x.go'), read('b.go'), read('c.go')],
      [
        { k: 'reasoning', text: 'Looking at the workspace first.' },
        { k: 'tool', i: 0 },
        { k: 'reasoning', text: 'Now the edits.' },
        { k: 'tool', i: 1 },
        { k: 'tool', i: 2 },
        { k: 'tool', i: 3 },
      ]
    );
    fireEvent.click(container.querySelector('[data-od-id="tool-group-m1"]')!);
    expect(activityIds(container)).toEqual([
      'tool-group-m1',
      'msg-reasoning-m1-0',
      'tool-read_file',
      'msg-reasoning-m1-2',
      'tool-edit_file',
      'tool-read_file',
      'tool-read_file',
    ]);
    // The revealed rows are the familiar collapsed Thought bubbles — one per
    // reasoning segment (their bodies stay behind each row's own toggle).
    expect(container.textContent).toContain('Thought');
    expect(container.textContent.match(/Thought/g)).toHaveLength(2);
  });

  it('a light turn is unchanged: cards + thoughts inline, no header', () => {
    const { container } = renderTurn([read('a.go'), edit('x.go')], [
      { k: 'reasoning', text: 'Quick look.' },
      { k: 'tool', i: 0 },
      { k: 'tool', i: 1 },
    ]);
    expect(container.querySelector('[data-od-id="tool-group-m1"]')).toBeNull();
    expect(activityIds(container)).toEqual(['msg-reasoning-m1-0', 'tool-read_file', 'tool-edit_file']);
  });

  it('legacy flat-reasoning heavy turn: thought hidden collapsed, revealed expanded', () => {
    const m = { id: 'm1', text: '', tools: [read('a.go'), read('b.go'), read('c.go'), read('d.go')], reasoning: 'Deep deliberation.' };
    const view = renderMessage(m);
    expect(view.container.querySelector('[data-od-id="msg-reasoning-m1"]')).toBeNull();
    expect(view.container.textContent).not.toContain('Deep deliberation.');
    fireEvent.click(view.container.querySelector('[data-od-id="tool-group-m1"]')!);
    // Revealed as the familiar collapsed Thought row (its body stays behind
    // the row's own toggle) — the point is the fold no longer hides it.
    expect(view.container.querySelector('[data-od-id="msg-reasoning-m1"]')).not.toBeNull();
    expect(view.container.textContent).toContain('Thought');
  });

  it('a streaming heavy turn names the last pending call and ticks the turn elapsed', () => {
    vi.useFakeTimers();
    try {
      // The execute call is still pending (no res/error) and sits BEFORE the
      // resolved reads — the LAST pending scan must still land on it, and the
      // label reads the catalog display name ("Shell"), never the raw id.
      const { container } = renderMessage(
        { id: 'm1', text: '', tools: [pendingExecute, read('a.go'), read('b.go'), read('c.go')] },
        { busy: true, isLast: true }
      );
      const header = container.querySelector('[data-od-id="tool-group-m1"]')!;
      expect(header).not.toBeNull();
      expect(header.textContent).toContain('Running Shell');
      expect(header.textContent).not.toContain('execute');
      expect(header.querySelector('.od-shimmer')).not.toBeNull();
      // Present-only elapsed: nothing before the first tick…
      expect(header.textContent).not.toContain('·');
      // …then the 1 s clock shows up as a suffix.
      act(() => { vi.advanceTimersByTime(1000); });
      expect(header.textContent).toContain('· 1 s');
      // The fold stays collapsed mid-stream — the header is still the only
      // activity element.
      expect(activityIds(container)).toEqual(['tool-group-m1']);
    } finally {
      vi.useRealTimers();
    }
  });

  it('a streaming heavy turn with no pending call reads "Thinking"', () => {
    vi.useFakeTimers();
    try {
      const { container } = renderMessage(
        { id: 'm1', text: '', tools: [read('a.go'), read('b.go'), read('c.go'), read('d.go')] },
        { busy: true, isLast: true }
      );
      const header = container.querySelector('[data-od-id="tool-group-m1"]')!;
      expect(header.textContent).toContain('Thinking');
      expect(header.querySelector('.od-shimmer')).not.toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it('completing the turn swaps the activity label for the resting summary', () => {
    vi.useFakeTimers();
    try {
      const m = { id: 'm1', text: '', tools: [pendingExecute, read('a.go'), read('b.go'), read('c.go')] };
      const view = renderMessage(m, { busy: true, isLast: true });
      act(() => { vi.advanceTimersByTime(1000); });
      expect(view.container.querySelector('[data-od-id="tool-group-m1"]')!.textContent).toContain('Running Shell');
      view.rerender(messageElement(m));
      const header = view.container.querySelector('[data-od-id="tool-group-m1"]')!;
      // Resting summary exactly as before the change — no shimmer, no
      // leftover elapsed digits.
      expect(header.textContent).toContain('4 steps');
      expect(header.textContent).not.toContain('·');
      expect(header.querySelector('.od-shimmer')).toBeNull();
      expect(view.container.querySelector('.od-shimmer')).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it('a hydrated turn shows the resting summary with no elapsed digits anywhere', () => {
    const { container } = renderTurn([read('a.go'), read('b.go'), read('c.go'), read('d.go')]);
    const header = container.querySelector('[data-od-id="tool-group-m1"]')!;
    expect(header.textContent).toContain('4 steps');
    expect(header.textContent).not.toContain('·');
    expect(header.querySelector('.od-shimmer')).toBeNull();
  });
});

describe('components/chat/AgentMessage — message timing (9.1)', () => {
  it('a completed live turn reveals client-measured figures on hover of the action row', () => {
    recordTurnTiming('mt-timed', { firstMs: 800, totalMs: 6200, tps: 42 });
    const { container } = renderMessage({ id: 'mt-timed', text: 'All done.', ts: '' });
    const line = container.querySelector('[data-od-id="msg-timing-mt-timed"]') as HTMLElement | null;
    expect(line).not.toBeNull();
    expect(line!.textContent).toContain('first token 0.8 s');
    expect(line!.textContent).toContain('total 6.2 s');
    expect(line!.textContent).toContain('42 tok/s');
    // Present-only: hidden until the reply's action row is hovered.
    expect(line!.className).toContain('opacity-0');
    expect(line!.className).not.toContain('opacity-100');
    const row = container.querySelector('[data-od-id="msg-actions-mt-timed"]')!;
    fireEvent.mouseEnter(row);
    expect(line!.className).toContain('opacity-100');
    fireEvent.mouseLeave(row);
    expect(line!.className).toContain('opacity-0');
  });

  it('a short stream omits the noisy speed figure (never fabricated)', () => {
    recordTurnTiming('mt-quick', { firstMs: 800, totalMs: 900 });
    const { container } = renderMessage({ id: 'mt-quick', text: 'Quick.', ts: '' });
    const line = container.querySelector('[data-od-id="msg-timing-mt-quick"]')!;
    expect(line.textContent).toContain('first token 0.8 s');
    expect(line.textContent).toContain('total 0.9 s');
    expect(line.textContent).not.toContain('tok/s');
  });

  it('a turn that is still streaming renders no timing line', () => {
    recordTurnTiming('mt-live', { firstMs: 100, totalMs: 200, tps: 5 });
    const { container } = render(
      <AgentMessage
        m={{ id: 'mt-live', text: 'still going…', ts: '' }}
        agent={{ name: 'Atlas' }} inChannel={false}
        busy isLast
        onCopy={vi.fn()} onRefresh={vi.fn()} onBranch={vi.fn()} members={[]}
      />
    );
    expect(container.querySelector('[data-od-id^="msg-timing-"]')).toBeNull();
    // The whole action row is chrome for a finished reply — absent mid-turn.
    expect(container.querySelector('[data-od-id="msg-actions-mt-live"]')).toBeNull();
  });

  it('hydrated history carries no timing — no registry entry, no line anywhere', () => {
    const { container } = renderMessage({ id: 'mt-hydrated', text: 'From history', ts: '2026-08-27T00:00:00Z' });
    expect(container.querySelector('[data-od-id^="msg-timing-"]')).toBeNull();
  });
});

describe('components/chat/AgentMessage — math rendering (11.1–11.2)', () => {
  it('gates the KaTeX load on math delimiters', () => {
    expect(containsMathDelimiters('plain text, no math')).toBe(false);
    // Prose money stays prose — remark-math could not close these fences.
    expect(containsMathDelimiters('Costs $5 and $10 today')).toBe(false);
    expect(containsMathDelimiters('Energy is $E=mc^2$ indeed')).toBe(true);
    expect(containsMathDelimiters('$$\\int_0^1 x\\,dx$$')).toBe(true);
  });

  it('renders inline and block math once the lazy KaTeX chunk loads', async () => {
    const { container } = renderMessage({
      id: 'm1',
      // remark-math reads single-line $…$ as inline math and the dollar-fence
      // block form as display math (katex-display).
      text: 'Inline $E=mc^2$ and a block:\n\n$$\n\\int_0^1 x\\,dx\n$$',
    });
    await waitFor(() => expect(container.querySelector('.katex')).not.toBeNull());
    expect(container.querySelector('.katex-display')).not.toBeNull();
  });

  it('leaves a non-math message exactly as before — no katex nodes, gate closed', () => {
    const { container } = renderMessage({ id: 'm1', text: 'Costs $5 and $10 today.' });
    expect(container.querySelector('.katex')).toBeNull();
    expect(container.textContent).toContain('$5 and $10');
    // The gate is what keeps the KaTeX bundle + stylesheet from loading.
    expect(containsMathDelimiters('Costs $5 and $10 today.')).toBe(false);
  });
});

describe('components/chat/AgentMessage — local file links open in right panel', () => {
  it('renders local file links as button with data-od-id="link-open-panel" and opens the panel', () => {
    useStore.setState({ panel: { open: false, tabs: [], activeId: null, badge: false } });
    const { container } = renderMessage({
      id: 'm1',
      text: 'Here is the report: [Project Report](/workspace/reports/summary.md) and an external link [GitHub](https://github.com).',
    });
    const panelBtn = container.querySelector('[data-od-id="link-open-panel"]');
    expect(panelBtn).not.toBeNull();
    expect(panelBtn?.textContent).toBe('Project Report');
    expect(panelBtn?.getAttribute('title')).toBe('Open summary.md in panel');

    const extLink = container.querySelector('a[href="https://github.com"]');
    expect(extLink).not.toBeNull();
    expect(extLink?.getAttribute('target')).toBe('_blank');

    fireEvent.click(panelBtn!);
    const panelState = useStore.getState().panel;
    expect(panelState.open).toBe(true);
    expect(panelState.tabs).toHaveLength(1);
    expect(panelState.tabs[0].payload).toEqual({ path: 'reports/summary.md' });
    expect(panelState.tabs[0].title).toBe('summary.md');
  });

  it('handles relative markdown links and files-api links', () => {
    useStore.setState({ panel: { open: false, tabs: [], activeId: null, badge: false } });
    const { container } = renderMessage({
      id: 'm2',
      text: 'Check [Notes](notes.txt) and [API File](/api/v1/workspaces/ws1/agents/ag1/files?path=docs/guide.md).',
    });
    const buttons = container.querySelectorAll('[data-od-id="link-open-panel"]');
    expect(buttons).toHaveLength(2);

    fireEvent.click(buttons[0]);
    expect(useStore.getState().panel.tabs[0].payload).toEqual({ path: 'notes.txt' });

    fireEvent.click(buttons[1]);
    expect(useStore.getState().panel.tabs[1].payload).toEqual({ path: 'docs/guide.md' });
  });
});

