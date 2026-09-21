/**
 * @vitest-environment jsdom
 */
// Lazy shiki lane tests (markdown-card-elements 1.6, design D9): the shiki
// chunk is a mocked dynamic import — the suite pins the theme mapping off
// <html data-theme>, the loader singleton, and the vendored highlighter's
// degraded first paint (plain code until loaded, streaming stays plain).
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor, renderHook, act } from '@testing-library/react';
import {
  loadShikiModule,
  shikiThemeFor,
  useShikiTheme,
  SHIKI_CODE_THEMES,
} from './shikiHighlighter';
import { SyntaxHighlighter } from '@/components/assistant-ui/elements/shiki-highlighter';

vi.mock('react-shiki', async () => {
  const { createElement } = await import('react');
  return {
    // The real hook returns a React element (highlighted tokens), not an
    // HTML string — mirror that shape.
    useShikiHighlighter: vi.fn(
      (code: string, language: string, theme: string, options: Record<string, unknown>) =>
        createElement(
          'span',
          {
            'data-shiki': true,
            'data-lang': String(language),
            'data-theme': String(theme),
            'data-delay': String(options.delay),
          },
          code,
        ),
    ),
  };
});

import { useShikiHighlighter } from 'react-shiki';
const mockHook = vi.mocked(useShikiHighlighter);

beforeEach(() => {
  mockHook.mockClear();
  delete document.documentElement.dataset.theme;
});

describe('shiki theme mapping (1.6)', () => {
  it('maps app light/dark onto the vendored element\'s default theme pair', () => {
    expect(shikiThemeFor('light')).toBe('github-light-default');
    expect(shikiThemeFor('dark')).toBe('github-dark-default');
    expect(SHIKI_CODE_THEMES.light).toBe('github-light-default');
    expect(SHIKI_CODE_THEMES.dark).toBe('github-dark-default');
  });

  it('useShikiTheme follows live data-theme flips', async () => {
    const hook = renderHook(() => useShikiTheme());
    expect(hook.result.current).toBe('github-light-default');
    act(() => {
      document.documentElement.dataset.theme = 'dark';
    });
    await waitFor(() => expect(hook.result.current).toBe('github-dark-default'));
  });
});

describe('loadShikiModule (mocked loader)', () => {
  it('shares one load across callers (singleton promise)', () => {
    expect(loadShikiModule()).toBe(loadShikiModule());
  });
});

describe('SyntaxHighlighter lazy mount', () => {
  it('renders plain code first, then upgrades to highlighted in place', async () => {
    const { container } = render(<SyntaxHighlighter code="const x = 1" language="ts"/>);
    // Degraded first paint: plain code, no shiki call yet.
    expect(container.querySelector('[data-shiki]')).toBeNull();
    expect(container.querySelector('pre code')!.textContent).toBe('const x = 1');
    // Upgraded once the mocked module lands — with the app-mapped theme.
    await waitFor(() => expect(container.querySelector('[data-shiki]')).not.toBeNull());
    const out = container.querySelector('[data-shiki]')!;
    expect(out.getAttribute('data-lang')).toBe('ts');
    expect(out.getAttribute('data-theme')).toBe('github-light-default');
    expect(out.textContent).toBe('const x = 1');
  });

  it('follows the app theme, and an explicit theme prop wins over the mapping', async () => {
    document.documentElement.dataset.theme = 'dark';
    const { container, rerender } = render(
      <SyntaxHighlighter code="x" language="go"/>,
    );
    await waitFor(() => expect(container.querySelector('[data-shiki]')).not.toBeNull());
    expect(container.querySelector('[data-shiki]')!.getAttribute('data-theme')).toBe(
      'github-dark-default',
    );
    rerender(
      <SyntaxHighlighter code="x" language="go" theme="one-dark-pro"/>,
    );
    await waitFor(() =>
      expect(container.querySelector('[data-shiki]')!.getAttribute('data-theme')).toBe(
        'one-dark-pro',
      ),
    );
  });

  it('passing a two-theme object keeps the light-dark() pairing', async () => {
    const { container } = render(
      <SyntaxHighlighter
        code="x"
        language="go"
        theme={{ dark: 'github-dark-default', light: 'github-light-default' }}
      />,
    );
    await waitFor(() => expect(container.querySelector('[data-shiki]')).not.toBeNull());
    expect(mockHook.mock.calls.at(-1)![3]).toMatchObject({ defaultColor: 'light-dark()' });
  });

  it('streaming renders plain and never calls the highlighter', () => {
    const calls = mockHook.mock.calls.length;
    const { container } = render(
      <SyntaxHighlighter code="const y = 2" language="ts" streaming/>,
    );
    expect(container.querySelector('pre code')!.textContent).toBe('const y = 2');
    expect(container.querySelector('[data-shiki]')).toBeNull();
    expect(mockHook.mock.calls.length).toBe(calls);
  });

  it('the default settle delay rides through to the hook options', async () => {
    const { container } = render(<SyntaxHighlighter code="z" language="ts"/>);
    await waitFor(() => expect(container.querySelector('[data-shiki]')).not.toBeNull());
    expect(mockHook.mock.calls.at(-1)![3]).toMatchObject({ delay: 150 });
  });
});
