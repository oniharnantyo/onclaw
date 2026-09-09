/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, vi } from 'vitest';
import { render } from '@testing-library/react';
import { ChatView } from './ChatView';

vi.mock('../../lib/api', () => ({
  api: {
    onUnauthorized: vi.fn(),
    skills: { list: vi.fn().mockResolvedValue({ skills: [] }) },
    tools: { list: vi.fn().mockResolvedValue({ tools: [] }) },
    agents: { listSkills: vi.fn().mockResolvedValue({ skills: [] }) },
  },
}));

const tenant = {
  id: 't1',
  sub: 't1',
  name: 'Acme',
  agents: [{ id: 'a1', name: 'Atlas', slug: 'atlas', status: 'idle' }],
};

const target = { kind: 'agent', obj: { id: 'a1', name: 'Atlas' } };
const agent = { id: 'a1', name: 'Atlas', slug: 'atlas', status: 'idle' };

const base = {
  tenant, target, agent, channelMembers: [], thread: [],
  onToggleMembers: vi.fn(), onConfigure: vi.fn(), onSend: vi.fn(),
  onCancel: vi.fn(), onAttach: vi.fn(), onCopy: vi.fn(),
  onRefresh: vi.fn(), onBranch: vi.fn(), onEditSubmit: vi.fn(),
  busy: false,
};

describe('ChatView loading states', () => {
  it('shows a single agent avatar while a live turn streams (no thinking row)', () => {
    // Live turns push an optimistic empty agent message and keep running=true
    // for the whole stream — the message row carries the loading dots itself.
    const thread = [
      { id: 'u1', author: 'you', text: 'hello', ts: '' },
      { id: 'm1', author: 'agent', agentId: 'a1', text: '', ts: '' },
    ];
    const { container } = render(<ChatView {...base} thread={thread} typing busy />);
    expect(container.querySelectorAll('[data-od-id="msg-m1"]').length).toBe(1);
    expect(container.querySelectorAll('[data-od-id="msg-thinking"]').length).toBe(0);
  });

  it('shows the thinking row while running with no agent message yet', () => {
    const thread = [{ id: 'u1', author: 'you', text: 'hello', ts: '' }];
    const { container } = render(<ChatView {...base} thread={thread} typing busy />);
    expect(container.querySelectorAll('[data-od-id="msg-thinking"]').length).toBe(1);
  });

  it('renders latency for finished tool cards and pulsing dots only for pending cards during a live turn', () => {
    // Regression: a turn that emits multiple tool calls in sequence must show
    // latency for completed calls (e.g. clickElement that returned an error,
    // navigate that succeeded) and only pulse dots on the call still waiting
    // for output. The whole turn having busy=true must not turn every card into
    // pulsing dots.
    const thread = [
      { id: 'u1', author: 'you', text: 'check lilianweng', ts: '' },
      {
        id: 'm1',
        author: 'agent',
        agentId: 'a1',
        text: '',
        ts: '',
        tools: [
          { name: 'browser.navigate', args: '{"url":"https://lilianweng.github.io/"}', res: '{"loaded":true}', ms: 1420 },
          { name: 'browser.click', args: '{"ref":"182"}', error: 'unknown ref "182"', res: 'unknown ref "182"', ms: 380 },
          { name: 'browser.read', args: '{}', ms: 0 },
        ],
        parts: [
          { k: 'tool', i: 0 },
          { k: 'tool', i: 1 },
          { k: 'tool', i: 2 },
        ],
      },
    ];
    const { container } = render(<ChatView {...base} thread={thread} typing busy />);
    const buttons = container.querySelectorAll('button[data-od-id^="tool-"]');
    expect(buttons.length).toBe(3);

    // Card 0: finished success → shows latency, no dots
    expect(buttons[0].textContent).toContain('1.4 s');
    expect(buttons[0].querySelectorAll('.od-dot').length).toBe(0);

    // Card 1: finished error → shows error latency, no dots
    expect(buttons[1].textContent).toContain('error · 380 ms');
    expect(buttons[1].querySelectorAll('.od-dot').length).toBe(0);

    // Card 2: still pending (no res/error) → pulses dots, no ms label
    expect(buttons[2].querySelectorAll('.od-dot').length).toBe(3);
    expect(buttons[2].textContent).not.toContain('0 ms');
  });

  it('renders a hook-blocked prompt as a notice entry in place of the assistant reply', () => {
    // integrate-agent-hooks: a prompt_blocked transcript entry hydrates (and
    // streams) as an author:'notice' message — a compact notice line, never
    // a spinner or an empty assistant bubble.
    const thread = [
      { id: 'u1', author: 'you', text: 'wipe the database', ts: '' },
      { id: 'n1', author: 'notice', text: '', ts: '9:14 AM', notice: { hook: 'Compliance Gate', reason: 'destructive prompts require approval' } },
    ];
    const { container } = render(<ChatView {...base} thread={thread} />);
    const notice = container.querySelector('[data-testid="prompt-blocked-notice"]');
    expect(notice).not.toBeNull();
    expect(notice?.textContent).toContain('Compliance Gate');
    expect(notice?.textContent).toContain('destructive prompts require approval');
    // No agent message row was minted for the blocked turn.
    expect(container.querySelectorAll('[data-role="assistant"]').length).toBe(0);
  });
});
