import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import { useNavStore } from "../stores/nav";

// The menu signal reaches the shell through the client's events surface; the
// mock captures the registered handler so a test can fire it (design D5)
// without the Wails runtime.
let handler: (() => void) | null = null;

vi.mock("../lib/client", () => ({
  client: {
    events: {
      onSettingsOpen: vi.fn((h: () => void) => {
        handler = h;

        return (): void => {
          handler = null;
        };
      }),
    },
  },
}));

import { subscribeSettingsShortcut } from "../settingsShortcut";

describe("settingsShortcut", () => {
  beforeEach(() => {
    handler = null;
    vi.clearAllMocks();
    useNavStore.setState({ menu: "chat", chatTarget: null });
  });

  it("navigates to settings when the native menu emits settings:open", () => {
    const off = subscribeSettingsShortcut();
    expect(handler).toBeTruthy();

    handler?.();

    expect(useNavStore.getState().menu).toBe("settings");
    off();
  });

  it("detaches the handler on unsubscribe so later signals are dropped", () => {
    const off = subscribeSettingsShortcut();

    off();

    expect(handler).toBeNull();
  });
});
