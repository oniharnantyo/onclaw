/**
 * @vitest-environment jsdom
 */
import { describe, it, expect } from 'vitest';
import { render, fireEvent } from '@testing-library/react';
import { ChatHeader } from './ChatHeader';

const agent = {
  id: 'a1', name: 'Atlas', slug: 'atlas', status: 'idle', model: 'gpt-5',
  lastActive: '2m ago',
  effective_context_window: 200000,
  summarization_trigger_tokens: 150000,
};

const agentTarget = { kind: 'agent', obj: { id: 'a1', name: 'Atlas' } };
const channelTarget = { kind: 'channel', obj: { id: 'c1', name: 'ops', purpose: 'Ops chatter' } };
const personTarget = { kind: 'person', obj: { id: 'p1', name: 'Rae Ito', presence: 'online' } };

const usage = { finalInput: 68000, at: '2:34 PM' };

function renderHeader(props: any) {
  const utils = render(<ChatHeader channelMembers={[]} onToggleMembers={() => {}} onConfigure={() => {}} {...props}/>);
  return {
    meter: () => utils.container.querySelector('[data-od-id="context-meter"]'),
    meterPct: () => utils.container.querySelector('[data-od-id="context-meter-pct"]'),
    meterFill: () => utils.container.querySelector('[data-od-id="context-meter-fill"]'),
    details: () => utils.container.querySelector('[data-od-id="context-meter-details"]'),
    detailsUsed: () => utils.container.querySelector('[data-od-id="context-meter-used"]'),
    triggerTick: () => utils.container.querySelector('[data-od-id="context-meter-trigger-tick"]'),
    ...utils,
  };
}

describe('components/chat/ChatHeader context meter', () => {
  it('renders percent and exact-count tooltip from the usage data', () => {
    const h = renderHeader({ target: agentTarget, agent, usage });

    expect(h.meterPct()?.textContent).toBe('34%');
    expect(h.meter()?.getAttribute('title')).toBe('68k / 200k');
    expect(h.meterFill()?.getAttribute('style')).toBe('width: 34%;');
  });

  it('keeps one decimal below 10% so short turns stay legible at the default window', () => {
    // 8,412 / 200,000 rounds to 4% — the value the user reported as "static".
    const h = renderHeader({ target: agentTarget, agent, usage: { finalInput: 8412, at: '2:34 PM' } });

    expect(h.meterPct()?.textContent).toBe('4.2%');
    expect(h.meterFill()?.getAttribute('style')).toBe('width: 4.2%;');
  });

  it('warns at or above the published trigger, accent below it', () => {
    const atTrigger = renderHeader({ target: agentTarget, agent, usage: { finalInput: 150000, at: '' } });
    expect(atTrigger.meterFill()?.className).toContain('bg-warn');
    expect(atTrigger.meterPct()?.className).toContain('text-[color-mix(in_oklab,var(--warn),black_38%)]');

    const belowTrigger = renderHeader({ target: agentTarget, agent, usage });
    expect(belowTrigger.meterFill()?.className).toContain('bg-accent');
    expect(belowTrigger.meterPct()?.className).toContain('text-muted');
  });

  it('is hidden for channel and person targets even with usage present', () => {
    const channel = renderHeader({ target: channelTarget, agent, usage, channelMembers: [{ id: 'm1', name: 'Atlas', kind: 'agent' }] });
    expect(channel.meter()).toBeNull();

    const person = renderHeader({ target: personTarget, agent, usage });
    expect(person.meter()).toBeNull();
  });

  it('is hidden without usage — no meter, no 0%', () => {
    const noUsage = renderHeader({ target: agentTarget, agent, usage: undefined });
    expect(noUsage.meter()).toBeNull();
    expect(noUsage.container.textContent).not.toContain('%');

    const zeroInput = renderHeader({ target: agentTarget, agent, usage: { finalInput: 0, at: '' } });
    expect(zeroInput.meter()).toBeNull();

    const noWindow = renderHeader({ target: agentTarget, agent: { ...agent, effective_context_window: undefined }, usage });
    expect(noWindow.meter()).toBeNull();
  });

  it('restores the meter from a hydrated usage prop with no interaction', () => {
    const h = renderHeader({ target: agentTarget, agent, usage });
    expect(h.meterPct()?.textContent).toBe('34%');
  });

  it('opens exact-count details on click: used/window, trigger line, trigger tick, updated time', () => {
    const h = renderHeader({ target: agentTarget, agent, usage });
    expect(h.details()).toBeNull();

    fireEvent.click(h.meter() as Element);
    const d = h.details();
    expect(d).not.toBeNull();
    expect(h.meter()?.getAttribute('aria-expanded')).toBe('true');
    expect(h.detailsUsed()?.textContent).toBe('68,000 / 200,000');
    expect(d?.textContent).toContain('34% of the context window');
    expect(d?.textContent).not.toContain('Summarizes');
    expect(d?.textContent).not.toContain('Updated');
    expect(h.triggerTick()?.getAttribute('style')).toBe('left: 75%;');
  });

  it('Escape and outside pointerdown close the details', () => {
    const h = renderHeader({ target: agentTarget, agent, usage });

    fireEvent.click(h.meter() as Element);
    expect(h.details()).not.toBeNull();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(h.details()).toBeNull();

    fireEvent.click(h.meter() as Element);
    expect(h.details()).not.toBeNull();
    fireEvent.pointerDown(document.body);
    expect(h.details()).toBeNull();
  });

  it('toggles closed when the meter is clicked again', () => {
    const h = renderHeader({ target: agentTarget, agent, usage });

    fireEvent.click(h.meter() as Element);
    expect(h.details()).not.toBeNull();
    fireEvent.click(h.meter() as Element);
    expect(h.details()).toBeNull();
  });
});

describe('components/chat/ChatHeader — Langfuse link (integrate-langfuse-tracing 4.1)', () => {
  let utils: ReturnType<typeof renderHeader>;
  const lfUrl = () => utils.container.querySelector('a[data-od-id="btn-open-langfuse"]') as HTMLAnchorElement | null;

  it('offers "Open in Langfuse" when the opened run carries a langfuse_url', () => {
    utils = renderHeader({ target: agentTarget, agent, usage, langfuseUrl: 'https://langfuse.acme.example.com/trace/tr-123' });
    const link = lfUrl();
    expect(link).not.toBeNull();
    expect(link!.getAttribute('href')).toBe('https://langfuse.acme.example.com/trace/tr-123');
    expect(link!.getAttribute('target')).toBe('_blank');
    expect(link!.getAttribute('rel')).toBe('noopener');
    expect(link!.getAttribute('aria-label')).toBe('Open in Langfuse');
  });

  it('renders no Langfuse action when the run has none (absent / null / empty)', () => {
    for (const langfuseUrl of [undefined, null, '']) {
      utils = renderHeader({ target: agentTarget, agent, usage, langfuseUrl });
      expect(lfUrl()).toBeNull();
      expect(utils.container.textContent).not.toContain('Langfuse');
    }
  });
});
