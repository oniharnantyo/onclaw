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
