/**
 * @vitest-environment jsdom
 */
import React from 'react';
import { act, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ThinkingRow } from './ThinkingRow';

describe('ThinkingRow', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders the shimmering Thinking label with avatar, od id and status role', () => {
    const { container, getByText } = render(<ThinkingRow agent={{ name: 'Atlas' }}/>);
    expect(getByText('Thinking')).toBeTruthy();
    expect(container.querySelector('.od-shimmer')?.textContent).toBe('Thinking');
    expect(container.querySelector('[data-od-id="msg-thinking"]')).toBeTruthy();
    expect(container.querySelector('[role="status"]')).toBeTruthy();
    expect(container.querySelector('.flex.gap-3 > *:first-child')).toBeTruthy();
  });

  it('has no elapsed suffix initially, then ticks and grows once a second', () => {
    const { container } = render(<ThinkingRow agent={null as any}/>);
    // No "·" suffix before the first tick.
    expect(container.textContent).not.toContain('·');
    act(() => { vi.advanceTimersByTime(1100); });
    expect(container.textContent).toMatch(/· .+/);
    const first = container.textContent;
    act(() => { vi.advanceTimersByTime(1000); });
    expect(container.textContent).toMatch(/· .+/);
    expect(container.textContent).not.toBe(first);
  });

  it('clears the tick interval on unmount', () => {
    const { unmount } = render(<ThinkingRow agent={null as any}/>);
    unmount();
    expect(() => { act(() => { vi.advanceTimersByTime(5000); }); }).not.toThrow();
  });
});
