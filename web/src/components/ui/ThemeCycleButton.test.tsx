// ThemeCycleButton contract (change add-dark-theme, task 3.6): click-cycling
// across the three states, icon-per-state, the exact aria-label/tooltip
// contract text, and the applyTheme side effect on <html data-theme>.
// The vitest jsdom env has no localStorage and no window.matchMedia — stub
// both (unstubbed in afterEach), same fakes as lib/theme.test.ts.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { ThemeCycleButton } from './ThemeCycleButton';
import { THEME_STORAGE_KEY } from '../../lib/theme';

function stubLocalStorage(initial: Record<string, string> = {}) {
  const map = new Map(Object.entries(initial));
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => (map.has(key) ? map.get(key)! : null),
    setItem: (key: string, value: string) => {
      map.set(key, String(value));
    },
    removeItem: (key: string) => {
      map.delete(key);
    },
  });
}

// Hand-rolled MediaQueryList fake with a triggerable listeners array.
class FakeMediaQueryList {
  matches: boolean;
  constructor(matches: boolean) {
    this.matches = matches;
  }
  addEventListener() {}
  removeEventListener() {}
  addListener() {}
  removeListener() {}
}

beforeEach(() => {
  delete document.documentElement.dataset.theme;
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('components/ui/ThemeCycleButton', () => {
  it('cycles light → dark → system → light with per-state icon and contract label', () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: 'light' });
    vi.stubGlobal('matchMedia', () => new FakeMediaQueryList(false));

    render(<ThemeCycleButton variant="icon" />);
    const btn = screen.getByTestId('theme-cycle');

    // light — sun (circle marker), contract text
    expect(btn.getAttribute('aria-label')).toBe('Theme: light (click for dark)');
    expect(btn.querySelector('circle')).not.toBeNull();
    expect(btn.querySelector('rect')).toBeNull();

    fireEvent.click(btn);
    expect(btn.getAttribute('aria-label')).toBe('Theme: dark (click for system)');
    // dark — moon (neither circle nor rect marker)
    expect(btn.querySelector('circle')).toBeNull();
    expect(btn.querySelector('rect')).toBeNull();
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
    expect(document.documentElement.dataset.theme).toBe('dark');

    fireEvent.click(btn);
    // system — monitor (rect marker) regardless of the rendered scheme
    expect(btn.getAttribute('aria-label')).toBe('Theme: system (click for light)');
    expect(btn.querySelector('rect')).not.toBeNull();
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('system');
    // OS scheme in this stub is light, so system resolves to light
    expect(document.documentElement.dataset.theme).toBe('light');

    fireEvent.click(btn);
    expect(btn.getAttribute('aria-label')).toBe('Theme: light (click for dark)');
    expect(btn.querySelector('circle')).not.toBeNull();
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('light');
    expect(document.documentElement.dataset.theme).toBe('light');
  });

  it('initializes from the stored preference (dark → moon)', () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: 'dark' });
    vi.stubGlobal('matchMedia', () => new FakeMediaQueryList(false));

    render(<ThemeCycleButton variant="icon" />);
    const btn = screen.getByTestId('theme-cycle');

    expect(btn.getAttribute('aria-label')).toBe('Theme: dark (click for system)');
    expect(btn.querySelector('circle')).toBeNull();
    expect(btn.querySelector('rect')).toBeNull();
  });

  it('shows the monitor icon for system even when the OS renders dark', () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: 'system' });
    vi.stubGlobal('matchMedia', () => new FakeMediaQueryList(true));

    render(<ThemeCycleButton variant="icon" />);
    const btn = screen.getByTestId('theme-cycle');

    expect(btn.getAttribute('aria-label')).toBe('Theme: system (click for light)');
    expect(btn.querySelector('rect')).not.toBeNull();
    // Clicking from system cycles to light, which overrides the dark OS stub
    fireEvent.click(btn);
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(btn.getAttribute('aria-label')).toBe('Theme: light (click for dark)');
  });

  it('shows a tooltip with the contract text on hover (icon variant)', () => {
    vi.useFakeTimers();
    stubLocalStorage({ [THEME_STORAGE_KEY]: 'light' });
    vi.stubGlobal('matchMedia', () => new FakeMediaQueryList(false));

    render(<ThemeCycleButton variant="icon" />);
    const btn = screen.getByTestId('theme-cycle');

    fireEvent.mouseEnter(btn);
    act(() => {
      vi.advanceTimersByTime(200);
    });

    const tooltip = screen.getByRole('tooltip');
    expect(tooltip.textContent).toBe('Theme: light (click for dark)');

    fireEvent.mouseLeave(btn);
    expect(screen.queryByRole('tooltip')).toBeNull();
    vi.useRealTimers();
  });

  it('rail-expanded renders the visible label row and disables the tooltip', () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: 'dark' });
    vi.stubGlobal('matchMedia', () => new FakeMediaQueryList(false));

    render(<ThemeCycleButton variant="rail" expanded testId="rail-theme" odId="rail-theme" />);
    const btn = screen.getByTestId('rail-theme');

    // Visible label row (Settings-row shape), tooltip suppressed
    expect(screen.getByText('Theme: dark')).not.toBeNull();
    expect(btn.className).toContain('w-[calc(100%-16px)]');
    expect(btn.className).toContain('text-left');

    vi.useFakeTimers();
    fireEvent.mouseEnter(btn);
    act(() => {
      vi.advanceTimersByTime(400);
    });
    expect(screen.queryByRole('tooltip')).toBeNull();
    vi.useRealTimers();

    // aria-label still carries the full contract even when the tooltip is off
    expect(btn.getAttribute('aria-label')).toBe('Theme: dark (click for system)');
  });
});
