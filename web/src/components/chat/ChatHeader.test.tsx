/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render } from '@testing-library/react';
import { ChatHeader } from './ChatHeader';

const agent = {
  id: 'a1', name: 'Atlas', slug: 'atlas', status: 'idle', model: 'gpt-5',
  lastActive: '2m ago',
  effective_context_window: 200000,
  summarization_trigger_tokens: 150000,
};

const agentTarget = { kind: 'agent', obj: { id: 'a1', name: 'Atlas' } };
const channelTarget = { kind: 'channel', obj: { id: 'ch-ops', name: 'ops', purpose: 'Ops coordination' } };

// The context meter itself moved to the composer's left rail
// (adopt-assistant-ui-elements D1) — its suites live in ContextRing.test.tsx.
// This file pins the header's remaining contract: no meter, no agent-configure
// button (add-right-panel replaced it with the panel toggle), the toggle's
// dot badge, and the members stack as a members-tab opener.

function renderHeader(props: any) {
  return render(<ChatHeader channelMembers={[]} onOpenMembers={() => {}} panelOpen={false} panelBadge={false} onTogglePanel={() => {}} {...props}/>);
}

describe('components/chat/ChatHeader — no context meter (adopt-assistant-ui-elements)', () => {
  it('renders no meter for an agent chat even with usage present', () => {
    const utils = renderHeader({
      target: agentTarget, agent,
      usage: { finalInput: 68000, at: '2:34 PM' },
    });
    expect(utils.container.querySelector('[data-od-id="context-meter"]')).toBeNull();
    expect(utils.container.querySelector('[data-od-id="context-meter-details"]')).toBeNull();
    expect(utils.container.textContent).not.toContain('%');
  });
});

describe('components/chat/ChatHeader — panel toggle replaces configure (add-right-panel 1.4)', () => {
  it('an agent chat header has the panel toggle and NO configure button', () => {
    const onTogglePanel = vi.fn();
    const utils = renderHeader({ target: agentTarget, agent, onTogglePanel });
    expect(utils.container.querySelector('[data-od-id="btn-configure-agent"]')).toBeNull();
    const toggle = utils.container.querySelector('[data-od-id="btn-panel-toggle"]') as HTMLButtonElement;
    expect(toggle).not.toBeNull();
    fireEvent.click(toggle);
    expect(onTogglePanel).toHaveBeenCalledTimes(1);
  });

  it('the toggle shows in every chat kind and reflects the open state', () => {
    const utils = renderHeader({ target: agentTarget, agent, panelOpen: true });
    const toggle = utils.container.querySelector('[data-od-id="btn-panel-toggle"]') as HTMLButtonElement;
    expect(toggle.getAttribute('aria-pressed')).toBe('true');
    expect(toggle.getAttribute('title')).toBe('Hide panel');
  });

  it('the dot badge renders only while a badge is pending and the panel is closed', () => {
    const pending = renderHeader({ target: agentTarget, agent, panelBadge: true, panelOpen: false });
    expect(pending.container.querySelector('[data-od-id="panel-badge"]')).not.toBeNull();
    // Open panel: nothing pending to reveal.
    const open = renderHeader({ target: agentTarget, agent, panelBadge: true, panelOpen: true });
    expect(open.container.querySelector('[data-od-id="panel-badge"]')).toBeNull();
    // No badge pending.
    const clear = renderHeader({ target: agentTarget, agent, panelBadge: false, panelOpen: false });
    expect(clear.container.querySelector('[data-od-id="panel-badge"]')).toBeNull();
  });
});

describe('components/chat/ChatHeader — members stack opens the members tab (add-right-panel 1.5)', () => {
  const members = [
    { id: 'a1', kind: 'agent', name: 'Atlas' },
    { id: 'p1', kind: 'person', name: 'Alice', presence: 'online' },
  ];

  it('clicking the avatar stack opens the members tab (no more onToggleMembers)', () => {
    const onOpenMembers = vi.fn();
    const utils = renderHeader({ target: channelTarget, agent: null, channelMembers: members, onOpenMembers });
    const stack = utils.container.querySelector('[data-od-id="btn-channel-members"]') as HTMLButtonElement;
    expect(stack).not.toBeNull();
    expect(stack.getAttribute('title')).toBe('Open members panel');
    fireEvent.click(stack);
    expect(onOpenMembers).toHaveBeenCalledTimes(1);
  });

  it('no members stack without members (agent chats never offer one)', () => {
    const utils = renderHeader({ target: agentTarget, agent, channelMembers: [] });
    expect(utils.container.querySelector('[data-od-id="btn-channel-members"]')).toBeNull();
  });
});

describe('components/chat/ChatHeader — Langfuse link (integrate-langfuse-tracing 4.1)', () => {
  const lfUrl = (utils: ReturnType<typeof renderHeader>) =>
    utils.container.querySelector('a[data-od-id="btn-open-langfuse"]') as HTMLAnchorElement | null;

  it('offers "Open in Langfuse" when the opened run carries a langfuse_url', () => {
    const utils = renderHeader({ target: agentTarget, agent, langfuseUrl: 'https://langfuse.acme.example.com/trace/tr-123' });
    const link = lfUrl(utils);
    expect(link).not.toBeNull();
    expect(link!.getAttribute('href')).toBe('https://langfuse.acme.example.com/trace/tr-123');
    expect(link!.getAttribute('target')).toBe('_blank');
    expect(link!.getAttribute('rel')).toBe('noopener');
    expect(link!.getAttribute('aria-label')).toBe('Open in Langfuse');
  });

  it('renders no Langfuse action when the run has none (absent / null / empty)', () => {
    for (const langfuseUrl of [undefined, null, '']) {
      const utils = renderHeader({ target: agentTarget, agent, langfuseUrl });
      expect(lfUrl(utils)).toBeNull();
      expect(utils.container.textContent).not.toContain('Langfuse');
    }
  });
});
