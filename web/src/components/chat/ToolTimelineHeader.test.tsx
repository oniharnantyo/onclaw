/**
 * @vitest-environment jsdom
 */
// ToolTimelineHeader (fix-tool-timeline-fold 3.1): while the turn streams the
// resting "N steps" summary is replaced by the shared ActivityLabel — same
// button, same chevron, same toggle semantics — and yields back to the
// untouched resting summary when the turn ends.
import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render } from '@testing-library/react';
import { ToolTimelineHeader } from './ToolTimelineHeader';

const base = {
  steps: 6,
  filesChanged: 0,
  open: false,
  onToggle: vi.fn(),
  odId: 'tool-group-m1',
};

const header = (container: HTMLElement): HTMLElement =>
  container.querySelector('[data-od-id="tool-group-m1"]')!;

describe('components/chat/ToolTimelineHeader', () => {
  it('resting renders the N steps summary — unchanged by the streaming branch', () => {
    const { container } = render(
      <ToolTimelineHeader {...base} filesChanged={2}/>
    );
    expect(header(container).textContent).toContain('6 steps · 2 files changed');
    expect(container.querySelector('.od-shimmer')).toBeNull();
    expect(container.querySelector('[aria-live="polite"]')).toBeNull();
  });

  it('streaming renders the ActivityLabel with the given label and elapsed', () => {
    const { container } = render(
      <ToolTimelineHeader {...base} streaming activityLabel="Running Shell" elapsedMs={2000}/>
    );
    expect(header(container).textContent).toContain('Running Shell');
    expect(header(container).textContent).toContain('2 s');
    expect(container.querySelector('.od-shimmer')).not.toBeNull();
    expect(container.querySelector('[aria-live="polite"]')).not.toBeNull();
  });

  it('streaming with no label falls back to "Thinking"', () => {
    const { container } = render(
      <ToolTimelineHeader {...base} streaming activityLabel={null}/>
    );
    expect(header(container).textContent).toContain('Thinking');
    // Null elapsed is present-only: no suffix until the clock has a value.
    expect(header(container).textContent).not.toContain('·');
  });

  it('streaming without an elapsed value renders no suffix', () => {
    const { container } = render(
      <ToolTimelineHeader {...base} streaming activityLabel="Running Grep"/>
    );
    expect(header(container).textContent).toBe('Running Grep');
  });

  it('the streaming header keeps the button semantics — chevron, data-od-id, aria-expanded, toggle', () => {
    const onToggle = vi.fn();
    const { container } = render(
      <ToolTimelineHeader {...base} onToggle={onToggle} streaming activityLabel="Running Shell"/>
    );
    const btn = header(container);
    expect(btn.tagName).toBe('BUTTON');
    expect(btn.getAttribute('aria-expanded')).toBe('false');
    expect(btn.querySelector('svg')).not.toBeNull(); // the chevron
    fireEvent.click(btn);
    expect(onToggle).toHaveBeenCalledTimes(1);
  });
});
