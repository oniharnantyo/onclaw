/**
 * @vitest-environment jsdom
 */
// ActivityLabel (fix-tool-timeline-fold 1.3): shimmer re-keys on label flip,
// elapsed suffix is present-only, reduced-motion falls back via CSS.
import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import fs from 'node:fs';
import path from 'node:path';
import { ActivityLabel } from './ActivityLabel';

function shimmerSpan(): HTMLElement | null {
  return document.querySelector('.od-shimmer');
}

describe('components/chat/ActivityLabel', () => {
  it('re-mounts the shimmer span when the label flips', () => {
    const { rerender } = render(<ActivityLabel label="Running Shell" />);
    const first = shimmerSpan();
    expect(first?.textContent).toBe('Running Shell');
    rerender(<ActivityLabel label="Reading files" />);
    const second = shimmerSpan();
    expect(second?.textContent).toBe('Reading files');
    expect(second).not.toBe(first);
  });

  it('renders no elapsed suffix when elapsedMs is absent', () => {
    const { container } = render(<ActivityLabel label="Running Shell" />);
    expect(container.textContent).not.toContain('·');
    expect(container.textContent).toBe('Running Shell');
  });

  it('renders no elapsed suffix when elapsedMs is null', () => {
    const { container } = render(<ActivityLabel label="Running Shell" elapsedMs={null} />);
    expect(container.textContent).not.toContain('·');
  });

  it('appends a muted elapsed suffix separated by · when provided', () => {
    const { container } = render(<ActivityLabel label="Running Shell" elapsedMs={2000} />);
    expect(container.textContent).toBe('Running Shell · 2 s');
  });

  it('keeps the outer span aria-live="polite"', () => {
    const { container } = render(<ActivityLabel label="Running Shell" />);
    expect(container.querySelector('[aria-live="polite"]')).not.toBeNull();
  });

  it('reduced-motion fallback exists in index.css for .od-shimmer', () => {
    const css = fs.readFileSync(path.resolve(process.cwd(), 'src/index.css'), 'utf8');
    expect(css).toContain('prefers-reduced-motion');
    expect(css).toContain('.od-shimmer');
  });
});
