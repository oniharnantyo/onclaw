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
});
