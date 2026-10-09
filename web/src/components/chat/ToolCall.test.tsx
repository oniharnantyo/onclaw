/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, afterEach, beforeEach } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { ToolCall } from './ToolCall';
import { useStore } from '../../store';
import { registerPanelCandidate, resetPanelRegistries } from '../../lib/panel/registry';
import { applyTaskList, type TaskEntry } from '../../lib/tasks';

const BLOCKED_RESULT = '{"blocked_by_hook":true,"hook":"Policy Gate","reason":"shell commands are blocked by policy"}';

describe('components/chat/ToolCall — hook enforcement rendering', () => {
  it('renders a hook-blocked call as a marked card with the hook name and reason', () => {
    const { container } = render(
      <ToolCall
        t={{
          name: 'execute',
          args: '{"command":"rm -rf /tmp/scratch"}',
          res: BLOCKED_RESULT,
          ms: 12,
        }}
        running={false}
      />
    );

    // Header: tool name, blocked badge, and the enforcing hook + reason in
    // the one-liner slot — all in the card's red-tinted container.
    const header = container.querySelector('button[data-od-id="tool-execute"]') as HTMLButtonElement;
    expect(header).not.toBeNull();
    expect(header.textContent).toContain('Policy Gate');
    expect(header.textContent).toContain('shell commands are blocked by policy');
    const card = container.firstElementChild as HTMLElement;
    expect(card.className).toContain('danger');

    // Not styled as an error: a block is a policy outcome, not a failure.
    expect(header.textContent).not.toContain('error ·');

    // Expanded: the block replaces the result (raw view keeps the JSON).
    fireEvent.click(header);
    expect(container.textContent).toContain('Blocked by hook Policy Gate');
    expect(container.textContent).toContain('shell commands are blocked by policy');
    // The result label no longer shows the raw envelope in formatted view.
    expect(container.textContent).not.toContain('blocked_by_hook');
  });

  it('keeps the raw JSON in the raw view of a blocked card', () => {
    const { container } = render(
      <ToolCall t={{ name: 'execute', args: '{}', res: BLOCKED_RESULT, ms: 12 }} running={false} />
    );
    fireEvent.click(container.querySelector('button[data-od-id="tool-execute"]') as HTMLButtonElement);
    fireEvent.click(screen.getByTitle('Show raw JSON'));
    expect(container.textContent).toContain('"blocked_by_hook"');
  });

  it('renders normal and errored cards unchanged (no block styling)', () => {
    const { container } = render(
      <>
        <ToolCall
          t={{ name: 'web.search', args: '{"query":"hooks"}', res: '{"results":[]}', ms: 420 }}
          running={false}
        />
        <ToolCall t={{ name: 'web.fetch', args: '{"url":"https://x"}', res: 'nope', error: 'nope', ms: 88 }} running={false} />
      </>
    );
    expect(container.textContent).not.toContain('blocked');
    expect(container.textContent).not.toContain('Blocked by');
    expect(container.textContent).toContain('error · 88 ms');
    expect(container.textContent).toContain('420 ms');
  });

  it('does not treat result JSON that merely mentions the key as a block', () => {
    const { container } = render(
      <ToolCall
        t={{ name: 'memory', args: '{"action":"read","path":"x"}', res: '{"note":"blocked_by_hook was mentioned"}', ms: 5 }}
        running={false}
      />
    );
    expect(container.textContent).not.toContain('Blocked by');
    expect(container.textContent).not.toContain('blocked by policy');
  });
});

describe('components/chat/ToolCall — document.create download link', () => {
  it('renders the download anchor with the capability URL when the envelope has one', () => {
    const { container } = render(
      <ToolCall
        t={{
          name: 'document.create',
          args: '{"name":"brief.docx","content":"# Brief"}',
          res: '{"name":"brief.docx","url":"/files/ws-1/att-1/brief.docx"}',
          ms: 210,
        }}
        running={false}
      />
    );
    fireEvent.click(container.querySelector('button[data-od-id="tool-document.create"]') as HTMLButtonElement);
    const link = container.querySelector('a[download]') as HTMLAnchorElement;
    expect(link).not.toBeNull();
    expect(link.getAttribute('href')).toBe('/files/ws-1/att-1/brief.docx');
    expect(link.textContent).toContain('Download document');
  });

  it('renders no download anchor when the envelope has no url', () => {
    const { container } = render(
      <ToolCall
        t={{
          name: 'document.create',
          args: '{"name":"brief.docx","content":"# Brief"}',
          res: '{"name":"brief.docx"}',
          ms: 210,
        }}
        running={false}
      />
    );
    fireEvent.click(container.querySelector('button[data-od-id="tool-document.create"]') as HTMLButtonElement);
    expect(container.querySelector('a[download]')).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Generative-UI registry mount (adopt-assistant-ui-elements 1.3): recognized
// envelopes and tool-keyed cards replace the generic rendering; unrecognized
// input keeps today's card. The dedicated search card and the generic header
// share the `tool-web.search` od-id (div vs button), so the assertions below
// check the tag too.
// ---------------------------------------------------------------------------

const SEARCH_RESULTS = JSON.stringify({
  query: 'onclaw agent',
  results: [
    { title: 'OnClaw', url: 'https://onclaw.dev', snippet: 'agent workspace' },
    { title: 'Docs', url: 'https://onclaw.dev/docs', snippet: 'docs' },
  ],
});

describe('components/chat/ToolCall — web search card (4.x)', () => {
  it('replaces the generic list with the dedicated card when results parse', () => {
    const { container } = render(
      <ToolCall t={{ name: 'web.search', args: '{"query":"onclaw agent"}', res: SEARCH_RESULTS, ms: 420 }} running={false}/>
    );
    expect(container.querySelector('div[data-od-id="tool-web.search"]')).not.toBeNull();
    expect(container.querySelector('button[data-od-id="tool-web.search"]')).toBeNull();
    // Query pill, status line, one row per source with a monospace domain.
    expect(container.textContent).toContain('onclaw agent');
    expect(container.textContent).toContain('Read 2 sources');
    expect(container.textContent).toContain('420 ms');
    const rows = container.querySelectorAll('li[data-od-id^="search-source-"]');
    expect(rows).toHaveLength(2);
    expect(rows[0].textContent).toContain('onclaw.dev');
    // The generic result list does not also render.
    expect(container.querySelector('ol.list-decimal')).toBeNull();
  });

  it('shows the query pill with a searching status and no rows while in flight', () => {
    const { container } = render(
      <ToolCall t={{ name: 'web.search', args: '{"query":"onclaw agent"}' }} running/>
    );
    expect(container.querySelector('div[data-od-id="tool-web.search"]')).not.toBeNull();
    expect(container.textContent).toContain('Searching');
    expect(container.textContent).toContain('onclaw agent');
    expect(container.querySelectorAll('li')).toHaveLength(0);
  });

  it('falls back to the generic card when the results do not parse', () => {
    const { container } = render(
      <ToolCall t={{ name: 'web.search', args: '{"query":"q"}', res: 'gateway timeout', ms: 88 }} running={false}/>
    );
    expect(container.querySelector('button[data-od-id="tool-web.search"]')).not.toBeNull();
    expect(container.querySelector('div[data-od-id="tool-web.search"]')).toBeNull();
    // The generic one-liner renders, never the source card's status line.
    expect(container.textContent).toContain("Searched for 'q'");
    expect(container.textContent).toContain('88 ms');
    expect(container.textContent).not.toContain('Read');
  });

  it('a hook-blocked search keeps the enforcement card, never the source card', () => {
    const { container } = render(
      <ToolCall
        t={{ name: 'web.search', args: '{"query":"q"}', res: BLOCKED_RESULT, ms: 12 }}
        running={false}
      />
    );
    expect(container.querySelector('div[data-od-id="tool-web.search"]')).toBeNull();
    expect(container.querySelector('button[data-od-id="tool-web.search"]')).not.toBeNull();
    expect(container.textContent).toContain('Policy Gate');
  });

  it('stagger: live arrivals reveal, hydrated renders skip the animation', () => {
    const live = render(
      <ToolCall t={{ name: 'web.search', args: '{"query":"q"}', res: SEARCH_RESULTS, ms: 5 }} running={false} live/>
    );
    expect(live.container.querySelectorAll('.od-genui-reveal')).toHaveLength(2);
    const hydrated = render(
      <ToolCall t={{ name: 'web.search', args: '{"query":"q"}', res: SEARCH_RESULTS, ms: 5 }} running={false}/>
    );
    expect(hydrated.container.querySelectorAll('.od-genui-reveal')).toHaveLength(0);
    expect(hydrated.container.querySelectorAll('li')).toHaveLength(2);
  });
});

describe('components/chat/ToolCall — todo checklist card (6.1)', () => {
  const TODO_ARGS = JSON.stringify({
    revision: 3,
    items: [
      { key: 'setup', text: 'Set up env', status: 'done' },
      { key: 'tests', text: 'Run tests', status: 'done' },
      { key: 'deploy', text: 'Deploy staging', status: 'active' },
      { key: 'dns', text: 'Flip DNS', status: 'failed', reason: 'registrar API down' },
    ],
  });

  it('renders the checklist with the done-only numerator and failed-in-denominator', () => {
    const { container } = render(
      <ToolCall t={{ name: 'todo_write', args: TODO_ARGS, res: '{"revision":3}', ms: 4 }} running={false}/>
    );
    expect(container.querySelector('div[data-od-id="tool-todo_write"]')).not.toBeNull();
    // 2 done of 4 items (the failed one counts in the denominator only) · rev 3.
    expect(container.textContent).toContain('2/4 · rev 3');
    const done = container.querySelector('li[data-od-id="todo-row-setup"] span') as HTMLElement;
    expect(done.className).toContain('line-through');
    expect(container.querySelector('li[data-od-id="todo-row-deploy"] .od-genui-spin')).not.toBeNull();
    const failed = container.querySelector('li[data-od-id="todo-row-dns"] span') as HTMLElement;
    expect(failed.className).toContain('text-danger');
    const failedRow = container.querySelector('li[data-od-id="todo-row-dns"]') as HTMLElement;
    expect(failedRow.textContent).toContain('registrar API down');
  });

  it('keys rows by stable item id so rewrites restyle in place', () => {
    const { container, rerender } = render(
      <ToolCall t={{ name: 'todo_write', args: TODO_ARGS, ms: 4 }} running={false}/>
    );
    const row = container.querySelector('li[data-od-id="todo-row-deploy"]') as HTMLElement;
    const revised = JSON.stringify({
      revision: 4,
      items: [
        { key: 'setup', text: 'Set up env', status: 'done' },
        { key: 'tests', text: 'Run tests', status: 'done' },
        { key: 'deploy', text: 'Deploy staging', status: 'done' },
        { key: 'dns', text: 'Flip DNS', status: 'failed', reason: 'registrar API down' },
      ],
    });
    rerender(<ToolCall t={{ name: 'todo_write', args: revised, ms: 4 }} running={false}/>);
    const restyled = container.querySelector('li[data-od-id="todo-row-deploy"]') as HTMLElement;
    expect(restyled).toBe(row); // same node — restyled in place, not remounted
    expect((restyled.querySelector('span') as HTMLElement).className).toContain('line-through');
  });

  it('a failed call or unparsable args falls back to the generic card', () => {
    const errored = render(
      <ToolCall t={{ name: 'todo_write', args: TODO_ARGS, error: 'validation failed', ms: 9 }} running={false}/>
    );
    expect(errored.container.querySelector('div[data-od-id="tool-todo_write"]')).toBeNull();
    expect(errored.container.textContent).toContain('error · 9 ms');
    const unparsable = render(
      <ToolCall t={{ name: 'todo_write', args: '{"revision":1}', ms: 9 }} running={false}/>
    );
    expect(unparsable.container.querySelector('button[data-od-id="tool-todo_write"]')).not.toBeNull();
  });
});

// LEGACY PATH PIN (markdown-fences): the ui.* echo tools were removed from the
// backend catalog — rich cards now arrive via markdown fences. The $type
// envelope dispatch below is retained only so OLD transcripts that stored echo
// results from the removed tools still render their cards. The live transport
// is markdown fences, not tool results.
describe('components/chat/ToolCall — legacy $type echo envelopes (7.x, removed-tool transcripts)', () => {
  it('a chart envelope renders the chart card instead of the generic result', () => {
    const { container } = render(
      <ToolCall
        t={{
          name: 'ui.chart',
          args: '{"$type":"chart","label":"Revenue","value":"$12.4k","points":[1,2,3]}',
          res: '{"$type":"chart","label":"Revenue","value":"$12.4k","points":[1,2,3]}',
          ms: 7,
        }}
        running={false}
      />
    );
    expect(container.querySelector('div[data-od-id="tool-ui.chart"]')).not.toBeNull();
    expect(container.querySelector('svg')).not.toBeNull();
    expect(container.querySelector('button[data-od-id="tool-ui.chart"]')).toBeNull();
  });

  it('an unknown $type keeps the standard collapsed card with no raw JSON in the body', () => {
    const { container } = render(
      <ToolCall
        t={{ name: 'mcp__x__widget', args: '{}', res: '{"$type":"mystery","payload":"secret"}', ms: 3 }}
        running={false}
      />
    );
    expect(container.querySelector('button[data-od-id="tool-mcp__x__widget"]')).not.toBeNull();
    expect(container.textContent).not.toContain('secret');
    // The raw escape hatch still works when the user opens the card.
    fireEvent.click(container.querySelector('button[data-od-id="tool-mcp__x__widget"]') as HTMLButtonElement);
    fireEvent.click(screen.getByTitle('Show raw JSON'));
    expect(container.textContent).toContain('mystery');
  });
});

// ---------------------------------------------------------------------------
// Panel open affordance (add-right-panel 1.6): a registered candidate matcher
// puts an explicit open control on the card — driving the store's
// openPanelTab — for live and hydrated cards alike (both derive from the same
// folded card data). Fake matchers prove the mechanism; the real file/browser
// candidates register in a later slice.
// ---------------------------------------------------------------------------
describe('components/chat/ToolCall — panel open affordance (add-right-panel 1.6)', () => {
  const PUBLISH_ARGS = '{"path":"docs/brief.md"}';

  /** One fake matcher for an artifact flow + a clean panel before each case. */
  const registerFakeCandidate = () => {
    resetPanelRegistries();
    useStore.setState({ panel: { open: false, tabs: [], activeId: null, badge: false } } as any);
    registerPanelCandidate((card) =>
      card.name === 'artifact.publish'
        ? { kind: 'artifact', title: 'Published artifact', payload: { path: String(JSON.parse(card.res || '{}').path || '') } }
        : null
    );
  };

  afterEach(() => {
    resetPanelRegistries();
    useStore.setState({ panel: { open: false, tabs: [], activeId: null, badge: false } } as any);
  });

  it('a card whose tool has a registered candidate renders the open affordance', () => {
    registerFakeCandidate();
    const { container } = render(
      <ToolCall t={{ name: 'artifact.publish', args: PUBLISH_ARGS, res: '{"path":"docs/brief.md"}', ms: 40 }} running={false}/>
    );
    expect(container.querySelector('[data-od-id="tool-open-panel"]')).not.toBeNull();
  });

  it('no candidate match → no affordance', () => {
    registerFakeCandidate();
    const { container } = render(
      <ToolCall t={{ name: 'execute', args: '{}', res: 'ok', ms: 4 }} running={false}/>
    );
    expect(container.querySelector('[data-od-id="tool-open-panel"]')).toBeNull();
  });

  it('an in-flight or errored card offers nothing to open', () => {
    registerFakeCandidate();
    const running = render(
      <ToolCall t={{ name: 'artifact.publish', args: PUBLISH_ARGS, ms: 0 }} running/>
    );
    expect(running.container.querySelector('[data-od-id="tool-open-panel"]')).toBeNull();
    const failed = render(
      <ToolCall t={{ name: 'artifact.publish', args: PUBLISH_ARGS, res: 'boom', error: 'boom', ms: 9 }} running={false}/>
    );
    expect(failed.container.querySelector('[data-od-id="tool-open-panel"]')).toBeNull();
  });

  it('activating the affordance opens the panel with the candidate focused', () => {
    registerFakeCandidate();
    const { container } = render(
      <ToolCall t={{ name: 'artifact.publish', args: PUBLISH_ARGS, res: '{"path":"docs/brief.md"}', ms: 40 }} running={false}/>
    );
    fireEvent.click(container.querySelector('[data-od-id="tool-open-panel"]') as HTMLButtonElement);
    const p = useStore.getState().panel;
    expect(p.open).toBe(true);
    expect(p.tabs).toHaveLength(1);
    expect(p.tabs[0]).toMatchObject({ kind: 'artifact', title: 'Published artifact', payload: { path: 'docs/brief.md' } });
    expect(p.activeId).toBe(p.tabs[0].id);
  });

  it('parity: the affordance derives from card data alone — a hydrated card gets the identical control', () => {
    registerFakeCandidate();
    // The exact card content a history replay folds (hydrated ids and
    // timestamps differ; {callId, name, args, res, ms} do not) — the
    // affordance is identical and both clicks land on ONE tab (dedup).
    const card = { callId: 'c1', name: 'artifact.publish', args: PUBLISH_ARGS, res: '{"path":"docs/brief.md"}', ms: 40 };
    const hydrated = render(<ToolCall t={card} running={false}/>);
    const live = render(<ToolCall t={card} running={false} live/>);
    expect(hydrated.container.querySelector('[data-od-id="tool-open-panel"]')).not.toBeNull();
    expect(live.container.querySelector('[data-od-id="tool-open-panel"]')).not.toBeNull();
    fireEvent.click(hydrated.container.querySelector('[data-od-id="tool-open-panel"]') as HTMLButtonElement);
    fireEvent.click(live.container.querySelector('[data-od-id="tool-open-panel"]') as HTMLButtonElement);
    expect(useStore.getState().panel.tabs).toHaveLength(1);
  });

  it('the affordance never swallows the card toggle: expand still works', () => {
    registerFakeCandidate();
    const { container } = render(
      <ToolCall t={{ name: 'artifact.publish', args: PUBLISH_ARGS, res: '{"path":"docs/brief.md"}', ms: 40 }} running={false}/>
    );
    fireEvent.click(container.querySelector('button[data-od-id="tool-artifact.publish"]') as HTMLButtonElement);
    expect(container.textContent).toContain('result');
  });
});

// ---------------------------------------------------------------------------
// Subagents deep-link affordance (add-subagent-runs-surface 6.3): reserved
// subagent-lane cards (`agent`, `background_shell`) open the right panel's
// Subagents source — trajectory detail on the D5-joined task, list view when
// unjoinable. The affordance replaces the generic open-in-panel control for
// these tools and works on running cards (spec: live cards deep-link).
// ---------------------------------------------------------------------------
describe('components/chat/ToolCall — Subagents deep-link (add-subagent-runs-surface 6.3)', () => {
  const CHAT = 'a-atlas';
  const DESCRIPTION = 'Research the vendor landscape';

  const agentCard = (over: Record<string, unknown> = {}) => ({
    callId: 'c1', name: 'agent', args: JSON.stringify({ description: DESCRIPTION }), ms: 0, ...over,
  });

  const seed = (entries: TaskEntry[]) => {
    useStore.setState({ pos: { ...useStore.getState().pos, tenantId: 'acme', chatId: CHAT } });
    if (entries.length) applyTaskList(CHAT, entries);
  };

  beforeEach(() => {
    useStore.setState({
      tasks: {},
      panel: { open: false, tabs: [], activeId: null, badge: false },
      pos: { ...useStore.getState().pos, tenantId: 'acme', chatId: CHAT },
    });
  });

  afterEach(() => {
    useStore.setState({ tasks: {}, panel: { open: false, tabs: [], activeId: null, badge: false } });
  });

  it('a joinable card opens the panel on the joined task trajectory detail', () => {
    seed([{ taskId: 'task-1', kind: 'delegation', status: 'running', description: DESCRIPTION }]);
    const { container } = render(<ToolCall t={agentCard()} running={false}/>);
    const btn = container.querySelector('[data-od-id="tool-open-subagents"]');
    expect(btn).not.toBeNull();
    fireEvent.click(btn as HTMLButtonElement);
    const p = useStore.getState().panel;
    expect(p.open).toBe(true);
    expect(p.tabs).toHaveLength(1);
    expect(p.tabs[0]).toMatchObject({ kind: 'subagents', title: 'Subagents', payload: { chatId: CHAT, taskId: 'task-1' } });
  });

  it('an unjoinable card still deep-links — to the list view (payload without taskId)', () => {
    seed([]);
    const { container } = render(<ToolCall t={agentCard()} running={false}/>);
    fireEvent.click(container.querySelector('[data-od-id="tool-open-subagents"]') as HTMLButtonElement);
    const p = useStore.getState().panel;
    expect(p.open).toBe(true);
    expect(p.tabs[0]).toMatchObject({ kind: 'subagents', payload: { chatId: CHAT } });
    expect(p.tabs[0].payload.taskId).toBeUndefined();
  });

  it('same-description siblings tiebreak by order: first card joins the first task, second the second', () => {
    seed([
      { taskId: 'task-a', kind: 'delegation', status: 'running', description: DESCRIPTION },
      { taskId: 'task-b', kind: 'delegation', status: 'running', description: DESCRIPTION },
    ]);
    const first = agentCard({ callId: 'c1' });
    const second = agentCard({ callId: 'c2' });
    const siblings = [first, second];
    const one = render(<ToolCall t={first} siblings={siblings} running={false}/>);
    fireEvent.click(one.container.querySelector('[data-od-id="tool-open-subagents"]') as HTMLButtonElement);
    const two = render(<ToolCall t={second} siblings={siblings} running={false}/>);
    fireEvent.click(two.container.querySelector('[data-od-id="tool-open-subagents"]') as HTMLButtonElement);
    const payloads = useStore.getState().panel.tabs.map((t) => t.payload.taskId);
    expect(payloads).toEqual(['task-a', 'task-b']);
  });

  it('without siblings context only a UNIQUE description match carries the taskId', () => {
    // Unique match → detail.
    seed([{ taskId: 'task-1', kind: 'delegation', status: 'running', description: DESCRIPTION }]);
    const unique = render(<ToolCall t={agentCard()} running={false}/>);
    fireEvent.click(unique.container.querySelector('[data-od-id="tool-open-subagents"]') as HTMLButtonElement);
    expect(useStore.getState().panel.tabs[0].payload.taskId).toBe('task-1');

    // Ambiguous (two same-description entries, no ordinal) → list view.
    seed([
      { taskId: 'task-a', kind: 'delegation', status: 'running', description: DESCRIPTION },
      { taskId: 'task-b', kind: 'delegation', status: 'running', description: DESCRIPTION },
    ]);
    const ambiguous = render(<ToolCall t={agentCard()} running={false}/>);
    fireEvent.click(ambiguous.container.querySelector('[data-od-id="tool-open-subagents"]') as HTMLButtonElement);
    const tabs = useStore.getState().panel.tabs;
    expect(tabs[tabs.length - 1].payload.taskId).toBeUndefined();
  });

  it('a running card keeps the affordance (live cards deep-link)', () => {
    seed([{ taskId: 'task-1', kind: 'delegation', status: 'running', description: DESCRIPTION }]);
    const { container } = render(<ToolCall t={agentCard()} running/>);
    expect(container.querySelector('[data-od-id="tool-open-subagents"]')).not.toBeNull();
  });

  it('background_shell joins on its command', () => {
    const cmd = 'npm test -- --coverage';
    seed([{ taskId: 'task-sh', kind: 'shell', status: 'running', description: cmd }]);
    const { container } = render(
      <ToolCall t={{ callId: 'c9', name: 'background_shell', args: JSON.stringify({ command: cmd }), ms: 0 }} running={false}/>
    );
    fireEvent.click(container.querySelector('[data-od-id="tool-open-subagents"]') as HTMLButtonElement);
    expect(useStore.getState().panel.tabs[0]).toMatchObject({
      kind: 'subagents',
      payload: { chatId: CHAT, taskId: 'task-sh' },
    });
  });

  it('replaces (never doubles) the generic open-in-panel control for these tools', () => {
    resetPanelRegistries();
    registerPanelCandidate((card) =>
      card.name === 'agent' ? { kind: 'subagents', title: 'Subagents', payload: {} } : null
    );
    const { container } = render(<ToolCall t={agentCard()} running={false}/>);
    expect(container.querySelector('[data-od-id="tool-open-panel"]')).toBeNull();
    expect(container.querySelector('[data-od-id="tool-open-subagents"]')).not.toBeNull();
    resetPanelRegistries();
  });

  it('other tools never get the subagents button', () => {
    const { container } = render(
      <ToolCall t={{ name: 'execute', args: '{"command":"ls"}', res: 'ok', ms: 4 }} running={false}/>
    );
    expect(container.querySelector('[data-od-id="tool-open-subagents"]')).toBeNull();
  });

  it('clicking twice lands on ONE tab — dedup by payload identity', () => {
    seed([{ taskId: 'task-1', kind: 'delegation', status: 'running', description: DESCRIPTION }]);
    const { container } = render(<ToolCall t={agentCard()} running={false}/>);
    fireEvent.click(container.querySelector('[data-od-id="tool-open-subagents"]') as HTMLButtonElement);
    fireEvent.click(container.querySelector('[data-od-id="tool-open-subagents"]') as HTMLButtonElement);
    expect(useStore.getState().panel.tabs).toHaveLength(1);
  });
});
