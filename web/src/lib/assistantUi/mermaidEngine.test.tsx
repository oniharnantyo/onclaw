/**
 * @vitest-environment jsdom
 */
// Lazy mermaid engine lane tests (markdown-card-elements 1.6, design D9):
// the engine chunk is a mocked dynamic import — the suite pins the degraded
// first paint (raw source until loaded), the in-place upgrade, the typed
// parse-error contract, and the loader singleton.
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor, renderHook } from '@testing-library/react';
import { readMermaidPalette, useMermaidEngine, loadMermaidEngine, MermaidRenderError } from './mermaidEngine';
import { MermaidDiagram } from '@/components/assistant-ui/elements/mermaid-diagram';
import { readAppTheme } from './appTheme';

vi.mock('beautiful-mermaid', () => ({
  renderMermaidSVG: vi.fn((code: string) => {
    if (code.includes('BAD')) throw new Error('parse failed at line 1');
    return `<svg data-code="${code}"></svg>`;
  }),
}));

import { renderMermaidSVG } from 'beautiful-mermaid';
const mockRender = vi.mocked(renderMermaidSVG);

beforeEach(() => {
  mockRender.mockClear();
  delete document.documentElement.dataset.theme;
});

describe('loadMermaidEngine (mocked loader)', () => {
  it('shares one load across callers (singleton promise)', () => {
    expect(loadMermaidEngine()).toBe(loadMermaidEngine());
  });

  it('renders through the engine; parse failures come back typed, never thrown', async () => {
    const hook = renderHook(() => useMermaidEngine());
    await waitFor(() => expect(hook.result.current.engine).not.toBeNull());
    const ready = hook.result.current.engine!;

    const ok = ready.render('graph TD;A-->B', { transparent: true });
    expect(ok).toEqual({ svg: '<svg data-code="graph TD;A-->B"></svg>', error: null });
    expect(mockRender).toHaveBeenCalledWith('graph TD;A-->B', { transparent: true });

    const bad = ready.render('BAD');
    expect(bad.svg).toBeNull();
    expect(bad.error).toBeInstanceOf(MermaidRenderError);
    expect(bad.error.message).toBe('parse failed at line 1');
  });

  it('exposes the resolved app theme; unset reads as light', () => {
    expect(readAppTheme()).toBe('light');
    document.documentElement.dataset.theme = 'dark';
    expect(readAppTheme()).toBe('dark');
  });
});

describe('useMermaidEngine degraded first paint (D9)', () => {
  it('starts with engine null and upgrades in place once the chunk lands', async () => {
    const hook = renderHook(() => useMermaidEngine());
    expect(hook.result.current.engine).toBeNull();
    expect(hook.result.current.loading).toBe(true);
    await waitFor(() => expect(hook.result.current.engine).not.toBeNull());
    expect(hook.result.current.loading).toBe(false);
  });
});

describe('MermaidDiagram lazy mount', () => {
  it('renders raw source first, then upgrades to the diagram in place', async () => {
    const { container } = render(<MermaidDiagram code="graph TD;A-->B"/>);
    // Degraded first paint: raw source, no engine caption.
    expect(container.querySelector('[data-slot="mermaid-pending"]')).not.toBeNull();
    expect(container.querySelector('[data-slot="mermaid-pending"]')!.textContent).toContain('graph TD;A-->B');
    expect(container.textContent).not.toContain('diagram could not be rendered');
    // Upgraded in place once the mocked engine arrives.
    await waitFor(() =>
      expect(container.querySelector('[data-slot="mermaid-diagram"]')).not.toBeNull(),
    );
    expect(container.querySelector('[data-slot="mermaid-diagram"]')!.innerHTML).toContain('<svg');
    // Colors are CONCRETE per-theme values: beautiful-mermaid emits each
    // option as a custom property on the rendered <svg>, so a var() value
    // self-clobbers and nodes lose strokes/text loses color (the dark-theme
    // breakage the live pass caught — pinned against here).
    expect(mockRender).toHaveBeenCalledWith(
      'graph TD;A-->B',
      expect.objectContaining({ bg: expect.any(String), fg: expect.any(String) }),
    );
    const opts = vi.mocked(mockRender).mock.calls.at(-1)?.[1] as Record<string, string>;
    for (const key of ['bg', 'fg', 'muted', 'border', 'surface', 'accent']) {
      expect(opts?.[key], key).toBeTruthy();
      expect(opts?.[key], key).not.toMatch(/var\(/);
    }
  });

  it('a parse failure renders raw source with the typed-error caption', async () => {
    const { container } = render(<MermaidDiagram code="BAD graph"/>);
    await waitFor(() =>
      expect(container.querySelector('[data-slot="mermaid-fallback"]')).not.toBeNull(),
    );
    expect(container.textContent).toContain('BAD graph');
    expect(container.textContent).toContain('diagram could not be rendered');
    expect(container.querySelector('svg')).toBeNull();
  });

  it('streaming renders the skeleton and never touches the engine', () => {
    const calls = mockRender.mock.calls.length;
    const { container } = render(<MermaidDiagram code="graph TD;A-->B" streaming/>);
    expect(container.querySelector('[data-slot="mermaid-skeleton"]')).not.toBeNull();
    expect(mockRender.mock.calls.length).toBe(calls);
  });
});

describe('readMermaidPalette (live-pass dark-theme fix)', () => {
  it('resolves concrete token values (never var() strings — beautiful-mermaid emits options as svg custom properties)', () => {
    const orig = document.documentElement.getAttribute.bind(document.documentElement);
    const mockGet = vi.fn().mockReturnValue('#111111');
    vi.spyOn(window, 'getComputedStyle').mockReturnValue({
      getPropertyValue: mockGet,
    } as unknown as CSSStyleDeclaration);
    try {
      const palette = readMermaidPalette();
      for (const key of ['bg', 'fg', 'muted', 'border', 'surface', 'accent']) {
        const v = (palette as Record<string, string | undefined | boolean>)[key];
        expect(v, key).toBeTruthy();
        expect(String(v), key).not.toMatch(/var\(/);
      }
      expect(palette.transparent).toBe(true);
      // dark values resolved from the tokens read, not the light fallbacks
      expect(palette.bg).toBe('#111111');
    } finally {
      vi.mocked(window.getComputedStyle).mockRestore();
      document.documentElement.getAttribute = orig;
    }
  });
});
