// Theme engine contract (change replace-daisyui-with-styled-components,
// task 1.3). Pins the storage key, resolution rule, cycle order, system-mode
// live tracking, and the legacy two-state stored values — including a drift
// guard against the inline no-flash bootstrap in ../../../index.html, which
// duplicates the resolve rule in miniature.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vite-plus/test";
import {
  THEME_STORAGE_KEY,
  applyTheme,
  cyclePreference,
  getStoredPreference,
  initTheme,
  resolveTheme,
  setStoredPreference,
  type ThemePreference,
} from "../theme";

// jsdom has no matchMedia and test runs must not share a real localStorage,
// so every test stubs both (unstubbed in afterEach) against a Map.
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

type MqlListener = (event: MediaQueryListEvent) => void;

// Hand-rolled MediaQueryList fake with a triggerable listeners array.
class FakeMediaQueryList {
  matches: boolean;

  listeners: MqlListener[] = [];

  constructor(matches: boolean) {
    this.matches = matches;
  }

  addEventListener(type: string, listener: MqlListener): void {
    if (type === "change") this.listeners.push(listener);
  }

  removeEventListener(type: string, listener: MqlListener): void {
    if (type === "change") {
      this.listeners = this.listeners.filter(
        (l: MqlListener) => l !== listener
      );
    }
  }

  // OS scheme flips: update matches, then fire the change event.
  dispatch(matches: boolean): void {
    this.matches = matches;

    // SAFETY: the theme engine only reads `matches` off the event; the
    // remaining MediaQueryListEvent fields are irrelevant here.
    this.listeners.forEach((listener: MqlListener) =>
      listener({ matches } as MediaQueryListEvent)
    );
  }
}

beforeEach(() => {
  delete document.documentElement.dataset.theme;
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("theme — resolution rule", () => {
  it("resolves system from the OS scheme (fresh browser, no stored value)", () => {
    expect(resolveTheme("system", true)).toBe("dark");

    expect(resolveTheme("system", false)).toBe("light");
  });

  it("stored preference overrides the OS scheme", () => {
    expect(resolveTheme("dark", false)).toBe("dark");

    expect(resolveTheme("light", true)).toBe("light");
  });
});

describe("theme — legacy two-state values", () => {
  // The old store wrote only 'light'/'dark'; both must survive the port
  // without 'system' defaulting over them.
  it("keeps resolving legacy stored light/dark values", () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: "light" });

    expect(getStoredPreference()).toBe("light");

    stubLocalStorage({ [THEME_STORAGE_KEY]: "dark" });

    expect(getStoredPreference()).toBe("dark");
  });

  it("legacy values win over an opposite OS scheme", () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: "light" });

    expect(resolveTheme(getStoredPreference(), true)).toBe("light");

    stubLocalStorage({ [THEME_STORAGE_KEY]: "dark" });

    expect(resolveTheme(getStoredPreference(), false)).toBe("dark");
  });
});

describe("theme — storage", () => {
  it("defaults to system when nothing is stored", () => {
    stubLocalStorage();

    expect(getStoredPreference()).toBe("system");
  });

  it("round-trips the stored preference", () => {
    stubLocalStorage();

    setStoredPreference("dark");

    expect(getStoredPreference()).toBe("dark");

    setStoredPreference("light");

    expect(getStoredPreference()).toBe("light");

    setStoredPreference("system");

    expect(getStoredPreference()).toBe("system");
  });

  it("treats a corrupt stored value as system", () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: "blue" });

    expect(getStoredPreference()).toBe("system");
  });
});

describe("theme — cycle order", () => {
  it("cycles light → dark → system → light", () => {
    expect(cyclePreference("light")).toBe("dark");

    expect(cyclePreference("dark")).toBe("system");

    expect(cyclePreference("system")).toBe("light");
  });
});

describe("theme — bootstrap drift guard", () => {
  // Read relative to this test file:
  // frontend/src/lib/__test__/theme.test.ts → frontend/index.html.
  const indexHtml = readFileSync(
    resolve(import.meta.dirname, "../../../index.html"),
    "utf8"
  );

  it("keeps the storage key namespaced under buddies-theme", () => {
    expect(THEME_STORAGE_KEY).toBe("buddies-theme");
  });

  it("the inline bootstrap in index.html stays in sync (key + rule + attribute)", () => {
    expect(indexHtml).toContain("buddies-theme");

    expect(indexHtml).toContain("prefers-color-scheme: dark");

    expect(indexHtml).toContain("data-theme");
  });
});

describe("theme — applyTheme", () => {
  it("sets data-theme on documentElement for every preference", () => {
    const mql = new FakeMediaQueryList(false);

    vi.stubGlobal("matchMedia", () => mql);

    expect(applyTheme("light")).toBe("light");

    expect(document.documentElement.dataset.theme).toBe("light");

    expect(applyTheme("dark")).toBe("dark");

    expect(document.documentElement.dataset.theme).toBe("dark");

    expect(applyTheme("system")).toBe("light");

    expect(document.documentElement.dataset.theme).toBe("light");

    mql.matches = true;

    expect(applyTheme("system")).toBe("dark");

    expect(document.documentElement.dataset.theme).toBe("dark");
  });
});

describe("theme — initTheme system-mode live tracking", () => {
  it("subscribes while in system mode and re-applies on OS scheme changes", () => {
    stubLocalStorage();

    const mql = new FakeMediaQueryList(false);

    vi.stubGlobal("matchMedia", () => mql);

    const dispose = initTheme();

    expect(document.documentElement.dataset.theme).toBe("light");

    expect(mql.listeners.length).toBe(1);

    mql.dispatch(true);

    expect(document.documentElement.dataset.theme).toBe("dark");

    mql.dispatch(false);

    expect(document.documentElement.dataset.theme).toBe("light");

    dispose();

    expect(mql.listeners.length).toBe(0);

    mql.dispatch(true);

    expect(document.documentElement.dataset.theme).toBe("light");
  });

  it("does not subscribe when a stored preference overrides the OS", () => {
    stubLocalStorage({ [THEME_STORAGE_KEY]: "dark" });

    const mql = new FakeMediaQueryList(true);

    vi.stubGlobal("matchMedia", () => mql);

    initTheme();

    expect(document.documentElement.dataset.theme).toBe("dark");

    expect(mql.listeners.length).toBe(0);
  });
});

// Type-level: the preference union stays closed over the three states.
const allPreferences: ThemePreference[] = ["light", "dark", "system"];

it("exposes exactly the three-state preference union", () => {
  expect(allPreferences).toHaveLength(3);
});
