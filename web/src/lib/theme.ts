// Theme preference engine (change add-dark-theme, design D3): a standalone
// module — deliberately not part of the zustand store — that resolves the
// three-state preference against the OS scheme and writes `data-theme` on
// <html>. The inline no-flash bootstrap in index.html duplicates the resolve
// rule in miniature — keep in sync with THEME_STORAGE_KEY + resolveTheme here;
// pinned by theme.test.ts.

export type ThemePreference = 'light' | 'dark' | 'system';
export type ResolvedTheme = 'light' | 'dark';

export const THEME_STORAGE_KEY = 'od_theme';

// Stored 'light'/'dark' overrides OS; 'system' (and anything else) resolves
// via systemPrefersDark.
export function resolveTheme(pref: ThemePreference, systemPrefersDark: boolean): ResolvedTheme {
  if (pref === 'dark') return 'dark';
  if (pref === 'light') return 'light';
  return systemPrefersDark ? 'dark' : 'light';
}

export function cyclePreference(pref: ThemePreference): ThemePreference {
  return pref === 'light' ? 'dark' : pref === 'dark' ? 'system' : 'light';
}

export function getStoredPreference(): ThemePreference {
  try {
    const stored = localStorage.getItem(THEME_STORAGE_KEY);
    if (stored === 'light' || stored === 'dark' || stored === 'system') return stored;
  } catch {
    // LocalStorage might be disabled or unavailable
  }
  return 'system';
}

export function setStoredPreference(pref: ThemePreference): void {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, pref);
  } catch {
    // LocalStorage might be disabled or unavailable
  }
}

export function applyTheme(pref: ThemePreference): ResolvedTheme {
  const resolved = resolveTheme(pref, window.matchMedia('(prefers-color-scheme: dark)').matches);
  document.documentElement.dataset.theme = resolved;
  return resolved;
}

// Applies the stored preference immediately; while it is 'system', tracks
// OS scheme changes live. Returns a cleanup that removes the listener.
export function initTheme(): () => void {
  const pref = getStoredPreference();
  applyTheme(pref);
  if (pref !== 'system') {
    return () => {};
  }
  const mql = window.matchMedia('(prefers-color-scheme: dark)');
  const onChange = () => {
    applyTheme(getStoredPreference());
  };
  if (typeof mql.addEventListener === 'function') {
    mql.addEventListener('change', onChange);
  } else {
    mql.addListener(onChange);
  }
  return () => {
    if (typeof mql.removeEventListener === 'function') {
      mql.removeEventListener('change', onChange);
    } else {
      mql.removeListener(onChange);
    }
  };
}
