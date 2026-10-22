// ThemeCycleButton contract (onclaw-v2 parity, task 2.1): click-cycling
// across the three states, icon-per-state, the exact aria-label/tooltip
// contract text, and the applyTheme side effect on <html data-theme>.
//
// Buddies adaptation: the component reads the zustand store
// (stores/theme.ts) instead of local state over lib/theme, so tests drive
// the store through setPreference. stores/theme.ts captures
// window.matchMedia at module init and jsdom has none — so matchMedia is
// stubbed BEFORE the store module is imported (lazily, in beforeAll) and
// re-stubbed per test (lib/theme's applyTheme reads window.matchMedia again
// at call time). The fake's `matches` reads the file-level osDark flag so
// the "system with a dark OS" case can flip it per test.
import { act } from "react";
import { fireEvent } from "@testing-library/dom";
import {
  afterEach,
  beforeEach,
  beforeAll,
  describe,
  expect,
  it,
  vi,
} from "vite-plus/test";

import { THEME_STORAGE_KEY, type ThemePreference } from "../../../lib/theme";
import { renderUI } from "../../__test__/test-utils";

let osDark = false;

// Hand-rolled MediaQueryList fake; matches tracks osDark live so the
// captured mql reflects per-test OS schemes.
class FakeMediaQueryList {
  get matches(): boolean {
    return osDark;
  }

  addEventListener(): void {}

  removeEventListener(): void {}

  addListener(): void {}

  removeListener(): void {}
}

// jsdom's localStorage would leak state between tests; every test stubs it
// against a Map instead (same fake as lib/theme.test.ts).
function stubLocalStorage(initial: Record<string, string> = {}): void {
  const map = new Map(Object.entries(initial));

  vi.stubGlobal("localStorage", {
    getItem: (key: string): string | null => map.get(key) ?? null,
    setItem: (key: string, value: string): void => {
      map.set(key, String(value));
    },
    removeItem: (key: string): void => {
      map.delete(key);
    },
  });
}

// Under test: imported lazily in beforeAll so the stubs above exist when
// stores/theme.ts captures window.matchMedia at module init.
let ThemeCycleButton: (typeof import("../ThemeCycleButton"))["ThemeCycleButton"];

let useThemeStore: (typeof import("../../../stores/theme"))["useThemeStore"];

beforeAll(async () => {
  vi.stubGlobal("matchMedia", () => new FakeMediaQueryList());

  stubLocalStorage();

  const component = await import("../ThemeCycleButton");
  const store = await import("../../../stores/theme");

  ThemeCycleButton = component.ThemeCycleButton;
  useThemeStore = store.useThemeStore;
});

beforeEach(() => {
  delete document.documentElement.dataset.theme;

  osDark = false;

  vi.stubGlobal("matchMedia", () => new FakeMediaQueryList());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/** Drives the store's public API to the given preference. */
async function storePreference(pref: ThemePreference): Promise<void> {
  await act(async () => {
    useThemeStore.getState().setPreference(pref);
  });
}

/** Clicks the cycle control inside act (store update re-renders it). */
async function click(btn: HTMLElement): Promise<void> {
  await act(async () => {
    fireEvent.click(btn);
  });
}

/** Hovers the trigger via the bubbling event React's delegation receives. */
function hover(btn: HTMLElement): void {
  btn.dispatchEvent(new MouseEvent("mouseover", { bubbles: true }));
}

function unhover(btn: HTMLElement): void {
  btn.dispatchEvent(new MouseEvent("mouseout", { bubbles: true }));
}

describe("components/ui/ThemeCycleButton", () => {
  it("cycles light → dark → system → light with per-state icon and contract label", async () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: "light" });
    await storePreference("light");

    const { getByTestId, unmount } = await renderUI(
      <ThemeCycleButton variant="icon" />
    );

    const btn = getByTestId("theme-cycle");

    // light — sun (circle marker), contract text
    expect(btn.getAttribute("aria-label")).toBe(
      "Theme: light (click for dark)"
    );
    expect(btn.querySelector("circle")).not.toBeNull();
    expect(btn.querySelector("rect")).toBeNull();

    await click(btn);
    expect(btn.getAttribute("aria-label")).toBe(
      "Theme: dark (click for system)"
    );
    // dark — moon (neither circle nor rect marker)
    expect(btn.querySelector("circle")).toBeNull();
    expect(btn.querySelector("rect")).toBeNull();
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(useThemeStore.getState().preference).toBe("dark");

    await click(btn);
    // system — monitor (rect marker) regardless of the rendered scheme
    expect(btn.getAttribute("aria-label")).toBe(
      "Theme: system (click for light)"
    );
    expect(btn.querySelector("rect")).not.toBeNull();
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("system");
    // OS scheme in this stub is light, so system resolves to light
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(useThemeStore.getState().theme).toBe("light");

    await click(btn);
    expect(btn.getAttribute("aria-label")).toBe(
      "Theme: light (click for dark)"
    );
    expect(btn.querySelector("circle")).not.toBeNull();
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
    expect(document.documentElement.dataset.theme).toBe("light");

    await unmount();
  });

  it("initializes from the stored preference (dark → moon)", async () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: "dark" });
    await storePreference("dark");

    const { getByTestId, unmount } = await renderUI(
      <ThemeCycleButton variant="icon" />
    );

    const btn = getByTestId("theme-cycle");

    expect(btn.getAttribute("aria-label")).toBe(
      "Theme: dark (click for system)"
    );
    expect(btn.querySelector("circle")).toBeNull();
    expect(btn.querySelector("rect")).toBeNull();
    expect(document.documentElement.dataset.theme).toBe("dark");

    await unmount();
  });

  it("shows the monitor icon for system even when the OS renders dark", async () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: "system" });
    osDark = true;
    await storePreference("system");

    const { getByTestId, unmount } = await renderUI(
      <ThemeCycleButton variant="icon" />
    );

    const btn = getByTestId("theme-cycle");

    expect(btn.getAttribute("aria-label")).toBe(
      "Theme: system (click for light)"
    );
    expect(btn.querySelector("rect")).not.toBeNull();
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(useThemeStore.getState().theme).toBe("dark");

    // Clicking from system cycles to light, which overrides the dark OS stub
    await click(btn);
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(btn.getAttribute("aria-label")).toBe(
      "Theme: light (click for dark)"
    );

    await unmount();
  });

  it("shows a tooltip with the contract text on hover (icon variant)", async () => {
    vi.useFakeTimers();
    stubLocalStorage({ [THEME_STORAGE_KEY]: "light" });
    await storePreference("light");

    const { getByTestId, getByRole, queryByRole, unmount } = await renderUI(
      <ThemeCycleButton variant="icon" />
    );

    const btn = getByTestId("theme-cycle");

    await act(async () => {
      hover(btn);
      vi.advanceTimersByTime(200);
    });

    const tooltip = getByRole("tooltip");

    expect(tooltip.textContent).toBe("Theme: light (click for dark)");

    await act(async () => {
      unhover(btn);
    });
    expect(queryByRole("tooltip")).toBeNull();

    await unmount();
    vi.useRealTimers();
  });

  it("rail-expanded renders the visible label row and disables the tooltip", async () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: "dark" });
    await storePreference("dark");

    const { getByTestId, getByText, queryByRole, unmount } = await renderUI(
      <ThemeCycleButton
        variant="rail"
        expanded
        testId="rail-theme"
        odId="rail-theme"
      />
    );

    const btn = getByTestId("rail-theme");

    // Visible label row (Settings-row shape), tooltip suppressed
    expect(getByText("Theme: dark")).not.toBeNull();
    expect(btn.className).toContain("w-[calc(100%-16px)]");
    expect(btn.className).toContain("text-left");

    vi.useFakeTimers();
    await act(async () => {
      hover(btn);
      vi.advanceTimersByTime(400);
    });
    expect(queryByRole("tooltip")).toBeNull();
    vi.useRealTimers();

    // aria-label still carries the full contract even when the tooltip is off
    expect(btn.getAttribute("aria-label")).toBe(
      "Theme: dark (click for system)"
    );

    await unmount();
  });
});
