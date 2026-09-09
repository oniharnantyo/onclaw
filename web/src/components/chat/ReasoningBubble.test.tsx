/**
 * @vitest-environment jsdom
 */
import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { ReasoningBubble } from './ReasoningBubble';

describe('ReasoningBubble', () => {
  it('labels a live segment Thinking, then Thought with measured latency', () => {
    const { container, rerender } = render(<ReasoningBubble text="partial thought" live odId="r1"/>);
    expect(container.textContent).toContain('Thinking');
    expect(container.textContent).not.toContain('Thought');

    // The turn moved on (live flips false): the label swaps and the segment's
    // wall-clock duration freezes in.
    rerender(<ReasoningBubble text="done thinking" odId="r1"/>);
    expect(container.textContent).toMatch(/Thought · \d+(\.\d+)? (ms|s)/);
  });

  it('renders hydrated segments as plain Thought with no fabricated latency', () => {
    // Hydrated segments mount already finished — events carry no reasoning
    // timing — so no duration renders (present-only).
    const { container } = render(<ReasoningBubble text="hydrated reasoning" odId="r2"/>);
    expect(container.textContent).toContain('Thought');
    expect(container.textContent).not.toContain('Thought · ');
  });
});
