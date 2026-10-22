/**
 * RecurrenceBuilder component tests (task 7.2, design D11): preset switching
 * seeds defaults, every preset composes its canonical expression, and the
 * initial value decomposes — a preset-shaped expression reconstructs that
 * preset (weekly `30 17 * * 1,5` → Mon+Fri 17:30), anything else opens in
 * Advanced showing the expression verbatim. No client mock: the builder is
 * pure over lib/cronPresets and emits through onChange.
 */
import { act } from "react";
import { fireEvent, screen } from "@testing-library/dom";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import { renderUI } from "../../../__test__/test-utils";
import { RecurrenceBuilder } from "../components/RecurrenceBuilder";

const unmounts: Array<() => Promise<void>> = [];

async function renderBuilder(
  value: string,
  onChange: (cron: string) => void
): Promise<void> {
  const view = await renderUI(
    <RecurrenceBuilder value={value} onChange={onChange} />
  );

  unmounts.push(view.unmount);
}

afterEach(async (): Promise<void> => {
  while (unmounts.length > 0) {
    const unmount = unmounts.pop();

    if (unmount) await unmount();
  }

  document.body.innerHTML = "";
});

/** Reads a rendered field: the builder's inputs are HTMLInputElements by
 * construction — narrowed through instanceof, no assertion. */
function inputOf(testId: string): HTMLInputElement {
  const el = screen.getByTestId(testId);

  if (!(el instanceof HTMLInputElement))
    throw new Error(`${testId} is not an input`);

  return el;
}

/** Clicks a preset chip inside an act batch. */
async function clickPreset(kind: string): Promise<void> {
  await act(async () => {
    fireEvent.click(screen.getByTestId(`recurrence-preset-${kind}`));
  });
}

describe("RecurrenceBuilder", () => {
  it("renders the six presets and marks the decomposed one active", async () => {
    await renderBuilder("0 9 * * *", vi.fn());

    for (const kind of [
      "minutely",
      "hourly",
      "daily",
      "weekly",
      "monthly",
      "advanced",
    ]) {
      expect(screen.getByTestId(`recurrence-preset-${kind}`)).toBeTruthy();
    }

    // `0 9 * * *` decomposes to Daily; the strip reflects it.
    expect(
      screen.getByTestId("recurrence-preset-daily").getAttribute("aria-checked")
    ).toBe("true");
    expect(
      screen
        .getByTestId("recurrence-preset-weekly")
        .getAttribute("aria-checked")
    ).toBe("false");
  });

  it("seeds preset defaults on switch and emits each composed expression", async () => {
    const onChange = vi.fn();
    await renderBuilder("0 9 * * *", onChange);

    await clickPreset("minutely");
    await clickPreset("hourly");
    await clickPreset("daily");
    await clickPreset("weekly");
    await clickPreset("monthly");

    expect(onChange.mock.calls.map((c: unknown[]) => c[0])).toEqual([
      "*/5 * * * *", // minutely: every 5 minutes
      "0 * * * *", // hourly: every hour at :00
      "0 9 * * *", // daily: 09:00
      "0 9 * * 1", // weekly: Monday 09:00
      "0 9 1 * *", // monthly: the 1st at 09:00
    ]);
  });

  it("composes weekly day chips and the time input into one expression", async () => {
    const onChange = vi.fn();
    await renderBuilder("0 9 * * *", onChange);

    await clickPreset("weekly");
    await act(async () => {
      fireEvent.click(screen.getByTestId("recurrence-weekday-5"));
    });
    await act(async () => {
      fireEvent.change(screen.getByTestId("recurrence-time"), {
        target: { value: "17:30" },
      });
    });

    expect(onChange).toHaveBeenLastCalledWith("30 17 * * 1,5");

    // Deselecting Monday keeps Friday.
    await act(async () => {
      fireEvent.click(screen.getByTestId("recurrence-weekday-1"));
    });

    expect(onChange).toHaveBeenLastCalledWith("30 17 * * 5");
  });

  it("composes the monthly day and hourly fields", async () => {
    const onChange = vi.fn();
    await renderBuilder("0 9 * * *", onChange);

    await clickPreset("monthly");
    await act(async () => {
      fireEvent.change(screen.getByTestId("recurrence-monthly-day"), {
        target: { value: "15" },
      });
    });

    expect(onChange).toHaveBeenLastCalledWith("0 9 15 * *");

    await clickPreset("hourly");
    await act(async () => {
      fireEvent.change(screen.getByTestId("recurrence-hourly-every"), {
        target: { value: "6" },
      });
    });
    await act(async () => {
      fireEvent.change(screen.getByTestId("recurrence-hourly-minute"), {
        target: { value: "30" },
      });
    });

    expect(onChange).toHaveBeenLastCalledWith("30 */6 * * *");
  });

  it("emits the minutely step field", async () => {
    const onChange = vi.fn();
    await renderBuilder("* * * * *", onChange);

    await act(async () => {
      fireEvent.change(screen.getByTestId("recurrence-minutely-every"), {
        target: { value: "15" },
      });
    });

    expect(onChange).toHaveBeenLastCalledWith("*/15 * * * *");
  });

  it("reconstructs a preset-shaped value on mount", async () => {
    await renderBuilder("30 17 * * 1,5", vi.fn());

    expect(
      screen
        .getByTestId("recurrence-preset-weekly")
        .getAttribute("aria-checked")
    ).toBe("true");
    expect(
      screen.getByTestId("recurrence-weekday-1").getAttribute("aria-pressed")
    ).toBe("true");
    expect(
      screen.getByTestId("recurrence-weekday-5").getAttribute("aria-pressed")
    ).toBe("true");
    expect(inputOf("recurrence-time").value).toBe("17:30");
  });

  it("opens Advanced for a non-preset expression showing it verbatim", async () => {
    const onChange = vi.fn();
    await renderBuilder("*/15 9-17 * * 1-5", onChange);

    expect(
      screen
        .getByTestId("recurrence-preset-advanced")
        .getAttribute("aria-checked")
    ).toBe("true");
    expect(inputOf("recurrence-advanced-input").value).toBe(
      "*/15 9-17 * * 1-5"
    );

    // Typing in Advanced emits the raw expression.
    await act(async () => {
      fireEvent.change(screen.getByTestId("recurrence-advanced-input"), {
        target: { value: "0 12 */2 * 1" },
      });
    });

    expect(onChange).toHaveBeenLastCalledWith("0 12 */2 * 1");
  });
});
