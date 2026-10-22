import { create } from "zustand";

// True only inside a Wails desktop webview. The native runtime injects
// window._wails.environment (OS/Arch/Debug) there and nowhere else — plain
// browsers, whether the dev server or server mode, never see it.
interface WailsEnvironment {
  OS?: unknown;
}

/** Desktop/browser environment facts derived from the injected runtime. */
export interface PlatformEnvironment {
  isDesktopApp: boolean;
  isMac: boolean;
}

function detectEnvironment(): PlatformEnvironment {
  // SAFETY: the native runtime injects window._wails; a browser simply lacks
  // the property, which the optional chain treats as absent.
  const env = (window as { _wails?: { environment?: WailsEnvironment } })._wails
    ?.environment;

  return {
    isDesktopApp: Boolean(env),
    isMac: env?.OS === "darwin",
  };
}

interface PlatformStore extends PlatformEnvironment {}

export const usePlatformStore = create<PlatformStore>()(() =>
  detectEnvironment()
);

// macOS injects the runtime core at WebViewDidFinishNavigation — after this
// bundle has run — so the initial check above is still false in the desktop
// app. The injected script ends by dispatching wails:runtime-config-ready
// (a signal browsers never send), so listen once and re-evaluate. The initial
// false is already correct for every browser, so no timeout is needed.
if (!usePlatformStore.getState().isDesktopApp) {
  window.addEventListener(
    "wails:runtime-config-ready",
    () => {
      if (detectEnvironment().isDesktopApp) {
        usePlatformStore.setState(detectEnvironment());
      }
    },
    { once: true }
  );
}

// Liquid Glass is a native window effect, so it exists only where the window
// is ours: the desktop app on macOS. styles.css turns page surfaces
// translucent under html[data-glass] so the system glass shows through;
// browsers and other OSes keep the opaque theme.
function syncGlassFlag(): void {
  const { isDesktopApp, isMac } = usePlatformStore.getState();

  if (isDesktopApp && isMac) {
    document.documentElement.dataset.glass = "on";
  } else {
    delete document.documentElement.dataset.glass;
  }
}

syncGlassFlag();

usePlatformStore.subscribe(syncGlassFlag);
