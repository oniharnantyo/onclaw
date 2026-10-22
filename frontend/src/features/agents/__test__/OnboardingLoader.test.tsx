/**
 * Component tests for the OnboardingLoader (task 5.2, design D9): the
 * "Onboarding <name>…" label, the 5-second rotation over the exact informal
 * pool, the elapsed timer after 30 seconds, and the reduced-motion collapse.
 * Fake timers drive the clock; jsdom has no matchMedia, which the loader
 * treats as motion allowed.
 */
import { act } from "react";
import { screen } from "@testing-library/dom";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vite-plus/test";

import { renderUI } from "../../../__test__/test-utils";
import { OnboardingLoader } from "../components/OnboardingLoader";

// jsdom has no matchMedia; save whatever is there so the reduced-motion stub
// can be undone per test.
const savedMatchMedia = window.matchMedia;

/** The piece of the MediaQueryList the loader reads. */
interface MediaMatches {
  matches: boolean;
}

function stubMatchMedia(matches: boolean): void {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: (query: string): MediaMatches => ({
      matches: matches && query === "(prefers-reduced-motion: reduce)",
    }),
  });
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(async () => {
  vi.useRealTimers();
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: savedMatchMedia,
  });
  document.body.innerHTML = "";
});

describe("OnboardingLoader", () => {
  it("labels the onboarding, rotates the line pool every 5 seconds, and starts the timer at 30", async () => {
    const view = await renderUI(<OnboardingLoader name="Scout" />);

    expect(screen.getByTestId("onboarding-label").textContent).toBe(
      "Onboarding Scout…"
    );
    expect(screen.getByTestId("onboarding-line").textContent).toBe(
      "Reading the brief twice…"
    );
    expect(screen.queryByTestId("onboarding-elapsed")).toBeNull();

    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(screen.getByTestId("onboarding-line").textContent).toBe(
      "Sketching a personality…"
    );

    // Just before 30 seconds there is still no timer.
    act(() => {
      vi.advanceTimersByTime(20000);
    });
    expect(screen.queryByTestId("onboarding-elapsed")).toBeNull();

    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(screen.getByTestId("onboarding-elapsed").textContent).toBe(
      "30s elapsed"
    );

    // The pool cycles: 50 seconds in, the tenth line wraps to the first.
    act(() => {
      vi.advanceTimersByTime(20000);
    });
    expect(screen.getByTestId("onboarding-line").textContent).toBe(
      "Reading the brief twice…"
    );
    expect(screen.getByTestId("onboarding-elapsed").textContent).toBe(
      "50s elapsed"
    );

    await view.unmount();
  });

  it("keeps working after the pool wraps and the timer keeps ticking", async () => {
    const view = await renderUI(<OnboardingLoader name="Radar" />);

    act(() => {
      vi.advanceTimersByTime(31000);
    });
    expect(screen.getByTestId("onboarding-line").textContent).toBe(
      "Stretching its context window…"
    );
    expect(screen.getByTestId("onboarding-elapsed").textContent).toBe(
      "31s elapsed"
    );

    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(screen.getByTestId("onboarding-elapsed").textContent).toBe(
      "32s elapsed"
    );

    await view.unmount();
  });

  it("collapses the rotation to a static line under prefers-reduced-motion while the timer still appears", async () => {
    stubMatchMedia(true);
    const view = await renderUI(<OnboardingLoader name="Scout" />);

    act(() => {
      vi.advanceTimersByTime(12000);
    });
    // Two rotation periods have passed; the line must not have moved.
    expect(screen.getByTestId("onboarding-line").textContent).toBe(
      "Reading the brief twice…"
    );

    act(() => {
      vi.advanceTimersByTime(18000);
    });
    expect(screen.getByTestId("onboarding-elapsed").textContent).toBe(
      "30s elapsed"
    );

    await view.unmount();
  });
});
