/**
 * @vitest-environment jsdom
 */
import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { ChatHeader } from './ChatHeader';

const agent = {
  id: 'a1', name: 'Atlas', slug: 'atlas', status: 'idle', model: 'gpt-5',
  lastActive: '2m ago',
  effective_context_window: 200000,
  summarization_trigger_tokens: 150000,
};

const agentTarget = { kind: 'agent', obj: { id: 'a1', name: 'Atlas' } };

// The context meter itself moved to the composer's left rail
// (adopt-assistant-ui-elements D1) — its suites live in ContextRing.test.tsx.
// This file pins the header's remaining contract, chiefly that it renders NO
// meter anymore.

function renderHeader(props: any) {
  return render(<ChatHeader channelMembers={[]} onToggleMembers={() => {}} onConfigure={() => {}} {...props}/>);
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
