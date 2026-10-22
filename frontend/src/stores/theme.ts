import { create, type StoreApi } from "zustand";

import { client } from "../lib/client";
import {
  applyTheme,
  cyclePreference,
  getStoredPreference,
  initTheme,
  setStoredPreference,
  type ResolvedTheme,
  type ThemePreference,
} from "../lib/theme";
import { usePlatformStore, type PlatformEnvironment } from "./platform";

// jsdom (the vitest environment) has no window.matchMedia; the real browser
// and the Wails webview always do. One probe keeps module init and toggles
// safe when tests import this module.
const systemMql: MediaQueryList | undefined = window.matchMedia?.(
  "(prefers-color-scheme: dark)"
);

// The native Liquid Glass window renders its frost from NSWindow.appearance,
// which follows the SYSTEM appearance rather than the app — keep it in sync
// so a light theme actually gets light frost. Web sessions have no window;
// a failed call (runtime not injected yet) is retried when the platform
// store flips ready.
function syncWindowAppearance(theme: ResolvedTheme): void {
  if (!usePlatformStore.getState().isDesktopApp) return;

  client.windowTheme.setLight(theme === "light").catch(() => {});
}

// Resolves like lib/theme.ts against the real OS scheme; without matchMedia
// (tests), 'system' falls back to dark — buddies' historical default.
function resolveAndApply(pref: ThemePreference): ResolvedTheme {
  setStoredPreference(pref);

  if (!systemMql) {
    return pref === "light" ? "light" : "dark";
  }

  return applyTheme(pref);
}

interface ThemeStore {
  theme: ResolvedTheme;
  preference: ThemePreference;
  toggleTheme: () => void;
  setPreference: (pref: ThemePreference) => void;
}

type ThemeSetState = StoreApi<ThemeStore>["setState"];

type ThemeGetState = StoreApi<ThemeStore>["getState"];

// Module load: apply the stored preference so the shell always matches
// storage (the index.html no-flash bootstrap already handled first paint).
// initTheme additionally starts OS-following while the preference is
// 'system'; the store's state is seeded from the resolved result.
let initialTheme: ResolvedTheme = "dark";

if (systemMql) {
  initTheme();

  initialTheme = applyTheme(getStoredPreference());

  syncWindowAppearance(initialTheme);
}

export const useThemeStore = create<ThemeStore>(
  (set: ThemeSetState, get: ThemeGetState) => ({
    theme: initialTheme,
    preference: getStoredPreference(),
    toggleTheme: (): void => {
      get().setPreference(cyclePreference(getStoredPreference()));
    },
    setPreference: (pref: ThemePreference): void => {
      const resolved = resolveAndApply(pref);

      syncWindowAppearance(resolved);

      set({ theme: resolved, preference: pref });
    },
  })
);

// While the stored preference is 'system', an OS scheme flip must update the
// React-facing store too (initTheme already re-applies the data-theme
// attribute; this listener keeps the store in step and covers entering
// system mode later via toggleTheme). One module-level listener, added once.
if (systemMql) {
  const onSystemSchemeChange = (): void => {
    if (getStoredPreference() !== "system") return;

    const resolved = applyTheme("system");

    syncWindowAppearance(resolved);

    useThemeStore.setState({ theme: resolved });
  };

  systemMql.addEventListener("change", onSystemSchemeChange);
}

// The initial sync above can race the desktop runtime injection; re-sync
// once the platform store reports the app is actually in the desktop.
usePlatformStore.subscribe(
  (state: PlatformEnvironment, prev: PlatformEnvironment) => {
    if (state.isDesktopApp && !prev.isDesktopApp) {
      syncWindowAppearance(useThemeStore.getState().theme);
    }
  }
);
