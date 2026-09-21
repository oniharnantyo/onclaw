/**
 * @vitest-environment jsdom
 */
import { describe, it, expect } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { ToolCall } from './ToolCall';

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

describe('components/chat/ToolCall — $type echo envelopes (7.x)', () => {
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
