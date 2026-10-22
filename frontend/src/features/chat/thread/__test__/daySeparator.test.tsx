/**
 * Day separator tests (task 3.3, spec chat-view "Day dividers"): the divider
 * between consecutive messages of different calendar days (Today /
 * Yesterday / the locale date), none before the first message, the full
 * timestamp carried on the divider, and the per-message time revealed on
 * hover/focus without reserving space — asserted on the class mechanism,
 * since jsdom applies no CSS (the SessionRail suite's precedent).
 */
import { screen } from "@testing-library/dom";
import { describe, expect, it } from "vite-plus/test";

import { ChatDayDivider, dayDividerLabel } from "../DayDivider";
import { ev, fold, renderThread } from "./harness";
import { renderUI } from "../../__test__/test-utils";
import { Kind } from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";

/** Noon local — a mid-day timestamp can never straddle a DST day boundary. */
function noonLocal(dayOffset: number): string {
  const now = new Date();

  return new Date(
    now.getFullYear(),
    now.getMonth(),
    now.getDate() + dayOffset,
    12
  ).toISOString();
}

function prompt(at: string, text: string): ReturnType<typeof ev> {
  return ev(Kind.KindUserMessage, { occurred_at: at, text });
}

describe("dayDividerLabel", () => {
  it("labels the current day Today", () => {
    expect(dayDividerLabel(noonLocal(0), new Date())).toBe("Today");
  });

  it("labels the previous day Yesterday", () => {
    expect(dayDividerLabel(noonLocal(-1), new Date())).toBe("Yesterday");
  });

  it("labels an older day with the locale date, year included", () => {
    const label = dayDividerLabel("2019-03-05T12:00:00", new Date());

    expect(label).not.toBe("Today");
    expect(label).not.toBe("Yesterday");
    expect(label).toContain("2019");
  });

  it("renders nothing for an unparsable timestamp", async () => {
    await renderUI(<ChatDayDivider at="not-a-date" />);

    expect(screen.queryByTestId("day-divider")).toBeNull();
  });
});

describe("thread day dividers (task 3.3)", () => {
  it("inserts one divider at the day boundary between two days", async () => {
    const today = noonLocal(0);

    const h = await renderThread(
      fold(prompt(noonLocal(-1), "from yesterday"), prompt(today, "from today"))
    );

    const dividers = screen.getAllByTestId("day-divider");

    expect(dividers).toHaveLength(1);
    expect(dividers[0]?.textContent).toContain("Today");
    expect(dividers[0]?.getAttribute("title")).toBe(today);

    await h.unmount();
  });

  it("shows no leading divider above the first message", async () => {
    const h = await renderThread(
      fold(
        prompt(noonLocal(-1), "first"),
        prompt(noonLocal(-1), "second same day")
      )
    );

    expect(screen.queryAllByTestId("day-divider")).toHaveLength(0);

    await h.unmount();
  });

  it("reveals a message's time on hover and focus without reserving space", async () => {
    const h = await renderThread(
      fold(prompt(noonLocal(0), "hello"), prompt(noonLocal(0), "again"))
    );

    const times = screen.getAllByTestId("message-time");

    expect(times.length).toBe(2);

    for (const time of times) {
      // Hidden at rest (occupies no space); hover and focus-within reveal
      // it — the class mechanism the stylesheet keys on.
      expect(time.className).toContain("hidden");
      expect(time.className).toContain("group-hover/msg:block");
      expect(time.className).toContain("group-focus-within/msg:block");
      expect(time.textContent).toMatch(/^\d{2}:\d{2}/);
      // The time hangs beside its message, absolute — it reserves no space
      // in the message row itself.
      expect(time.className).toContain("absolute");
    }

    await h.unmount();
  });
});

describe("the divider's timestamp (port of the legacy DayDivider suite)", () => {
  it("carries the full timestamp on the divider for exact inspection", async () => {
    const at = noonLocal(-1);

    const { unmount } = await renderUI(<ChatDayDivider at={at} />);

    const divider = screen.getByTestId("day-divider");

    expect(divider.getAttribute("title")).toBe(at);
    expect(divider.textContent).toBe("Yesterday");

    await unmount();
  });
});
