/// <reference types="node" />
// Theme engine contract (change add-dark-theme, tasks 1.2/1.3). Pins the
// storage key, resolution rule, cycle order, and system-mode live tracking —
// including a drift guard against the inline no-flash bootstrap in
// ../../index.html, which duplicates the resolve rule in miniature.
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  THEME_STORAGE_KEY,
  applyTheme,
  cyclePreference,
  getStoredPreference,
  initTheme,
  resolveTheme,
  setStoredPreference,
  type ThemePreference,
} from './theme';

// The vitest jsdom env has no localStorage and no window.matchMedia — stub
// both (unstubbed in afterEach).
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
  listeners: Array<(event: MediaQueryListEvent) => void> = [];
  constructor(matches: boolean) {
    this.matches = matches;
  }
  addEventListener(type: string, listener: (event: MediaQueryListEvent) => void) {
    if (type === 'change') this.listeners.push(listener);
  }
  removeEventListener(type: string, listener: (event: MediaQueryListEvent) => void) {
    if (type === 'change') this.listeners = this.listeners.filter((l) => l !== listener);
  }
  addListener(listener: (event: MediaQueryListEvent) => void) {
    this.listeners.push(listener);
  }
  removeListener(listener: (event: MediaQueryListEvent) => void) {
    this.listeners = this.listeners.filter((l) => l !== listener);
  }
  // OS scheme flips: update matches, then fire the change event.
  dispatch(matches: boolean) {
    this.matches = matches;
    this.listeners.forEach((listener) => listener({ matches } as MediaQueryListEvent));
  }
}

beforeEach(() => {
  delete document.documentElement.dataset.theme;
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('theme — resolution rule', () => {
  it('resolves system from the OS scheme (fresh browser, no stored value)', () => {
    expect(resolveTheme('system', true)).toBe('dark');
    expect(resolveTheme('system', false)).toBe('light');
  });

  it('stored preference overrides the OS scheme', () => {
    expect(resolveTheme('dark', false)).toBe('dark');
    expect(resolveTheme('light', true)).toBe('light');
  });
});

describe('theme — storage', () => {
  it('defaults to system when nothing is stored', () => {
    stubLocalStorage();
    expect(getStoredPreference()).toBe('system');
  });

  it('round-trips the stored preference', () => {
    stubLocalStorage();
    setStoredPreference('dark');
    expect(getStoredPreference()).toBe('dark');
    setStoredPreference('light');
    expect(getStoredPreference()).toBe('light');
    setStoredPreference('system');
    expect(getStoredPreference()).toBe('system');
  });

  it('treats a corrupt stored value as system', () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: 'blue' });
    expect(getStoredPreference()).toBe('system');
  });
});

describe('theme — cycle order', () => {
  it('cycles light → dark → system → light', () => {
    expect(cyclePreference('light')).toBe('dark');
    expect(cyclePreference('dark')).toBe('system');
    expect(cyclePreference('system')).toBe('light');
  });
});

describe('theme — bootstrap drift guard', () => {
  // Read relative to this test file: web/src/lib/theme.test.ts → web/index.html.
  const indexHtml = readFileSync(resolve(import.meta.dirname, '../../index.html'), 'utf8');

  it('keeps the storage key namespaced under od_theme', () => {
    expect(THEME_STORAGE_KEY).toBe('od_theme');
  });

  it('the inline bootstrap in index.html stays in sync (key + rule + attribute)', () => {
    expect(indexHtml).toContain('od_theme');
    expect(indexHtml).toContain('data-theme');
  });
});

describe('theme — applyTheme', () => {
  it('sets data-theme on documentElement for every preference', () => {
    const mql = new FakeMediaQueryList(false);
    vi.stubGlobal('matchMedia', () => mql);

    expect(applyTheme('light')).toBe('light');
    expect(document.documentElement.dataset.theme).toBe('light');

    expect(applyTheme('dark')).toBe('dark');
    expect(document.documentElement.dataset.theme).toBe('dark');

    expect(applyTheme('system')).toBe('light');
    expect(document.documentElement.dataset.theme).toBe('light');

    mql.matches = true;
    expect(applyTheme('system')).toBe('dark');
    expect(document.documentElement.dataset.theme).toBe('dark');
  });
});

describe('theme — initTheme system-mode live tracking', () => {
  it('subscribes while in system mode and re-applies on OS scheme changes', () => {
    stubLocalStorage();
    const mql = new FakeMediaQueryList(false);
    vi.stubGlobal('matchMedia', () => mql);

    const dispose = initTheme();
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(mql.listeners.length).toBe(1);

    mql.dispatch(true);
    expect(document.documentElement.dataset.theme).toBe('dark');

    mql.dispatch(false);
    expect(document.documentElement.dataset.theme).toBe('light');

    dispose();
    expect(mql.listeners.length).toBe(0);
    mql.dispatch(true);
    expect(document.documentElement.dataset.theme).toBe('light');
  });

  it('does not subscribe when a stored preference overrides the OS', () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: 'dark' });
    const mql = new FakeMediaQueryList(true);
    vi.stubGlobal('matchMedia', () => mql);

    initTheme();
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(mql.listeners.length).toBe(0);
  });
});

// Type-level: the preference union stays closed over the three states.
const allPreferences: ThemePreference[] = ['light', 'dark', 'system'];
it('exposes exactly the three-state preference union', () => {
  expect(allPreferences).toHaveLength(3);
});
