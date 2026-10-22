import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

// The store reads window._wails.environment at import time, registers its
// runtime-config-ready listener at import time, and syncs the html
// data-glass flag as a side effect — so every test stages the window first
// and re-imports the module for a fresh store.
async function loadPlatform(): Promise<typeof import("../platform")> {
  return import("../platform");
}

function stageWindow(env?: { OS?: string }): void {
  // SAFETY: the desktop runtime injects `_wails` on the window object; the
  // test stages and removes exactly that key.
  const w = window as { _wails?: unknown };

  if (env) {
    w._wails = { environment: env };
  } else {
    delete w._wails;
  }

  delete document.documentElement.dataset.glass;
}

describe("platform store", () => {
  beforeEach(() => {
    vi.resetModules();
    stageWindow();
  });

  it("reports a plain browser: not desktop, not mac, no glass", async () => {
    const { usePlatformStore } = await loadPlatform();

    expect(usePlatformStore.getState().isDesktopApp).toBe(false);
    expect(usePlatformStore.getState().isMac).toBe(false);
    expect(document.documentElement.dataset.glass).toBeUndefined();
  });

  it("flips on the runtime config event and enables glass on mac desktop", async () => {
    const { usePlatformStore } = await loadPlatform();

    // The desktop runtime injects the environment after this bundle has run,
    // so the initial state is still browser-shaped.
    expect(usePlatformStore.getState().isDesktopApp).toBe(false);

    stageWindow({ OS: "darwin" });
    window.dispatchEvent(new Event("wails:runtime-config-ready"));

    expect(usePlatformStore.getState().isDesktopApp).toBe(true);
    expect(usePlatformStore.getState().isMac).toBe(true);
    expect(document.documentElement.dataset.glass).toBe("on");
  });

  it("never enables glass for the desktop app on other OSes", async () => {
    const { usePlatformStore } = await loadPlatform();

    stageWindow({ OS: "windows" });
    window.dispatchEvent(new Event("wails:runtime-config-ready"));

    expect(usePlatformStore.getState().isDesktopApp).toBe(true);
    expect(usePlatformStore.getState().isMac).toBe(false);
    expect(document.documentElement.dataset.glass).toBeUndefined();
  });

  it("detects an already-injected environment synchronously", async () => {
    stageWindow({ OS: "darwin" });

    const { usePlatformStore } = await loadPlatform();

    expect(usePlatformStore.getState().isDesktopApp).toBe(true);
    expect(usePlatformStore.getState().isMac).toBe(true);
    expect(document.documentElement.dataset.glass).toBe("on");
  });
});
