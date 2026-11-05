/**
 * StatusDot tests: renders the token-based dot for each status, exposes the
 * state as visually-hidden text (the dot itself is aria-hidden), announces
 * the live-flipping running state via role="status", and gates the live
 * pulse on both the `live` prop and the status opting in.
 */
import { describe, expect, it } from "vite-plus/test";

import { StatusDot } from "../StatusDot";
import { renderUI } from "../../__test__/test-utils";

/** The decorative dot span (aria-hidden); StatusDot renders a wrapper around it. */
function dotClass(container: HTMLElement): string {
  const el = container.querySelector("span[aria-hidden='true']");

  if (!el) throw new Error("StatusDot rendered no dot span");

  return el.className;
}

/** The visually-hidden state text — the dot's accessible content. */
function statusText(container: HTMLElement): string | null {
  return container.querySelector(".sr-only")?.textContent ?? null;
}

describe("StatusDot", () => {
  it("renders the token-based dot class per status", async () => {
    const running = await renderUI(<StatusDot status="running" />);
    const idle = await renderUI(<StatusDot status="idle" />);
    const error = await renderUI(<StatusDot status="error" />);
    const warn = await renderUI(<StatusDot status="warn" />);

    // Colors are token utilities (both themes re-skin), never literal colors.
    expect(dotClass(running.container)).toContain("bg-success");
    expect(dotClass(idle.container)).toContain("bg-muted");
    expect(dotClass(error.container)).toContain("bg-danger");
    expect(dotClass(warn.container)).toContain("bg-warn");

    await running.unmount();
    await idle.unmount();
    await error.unmount();
    await warn.unmount();
  });

  it("exposes the state as visually-hidden text, not a tooltip", async () => {
    const error = await renderUI(<StatusDot status="error" />);
    const running = await renderUI(<StatusDot status="running" />);

    // The dot is decorative; the text label is the accessible content.
    // (dotClass queries `span[aria-hidden='true']`, so it throws if the
    // dot is not aria-hidden.)
    expect(statusText(error.container)).toBe("Needs attention");
    expect(statusText(running.container)).toBe("Running");

    await error.unmount();
    await running.unmount();
  });

  it("announces the live-flipping running state via role=status", async () => {
    const running = await renderUI(<StatusDot status="running" live />);
    const runningQuiet = await renderUI(<StatusDot status="running" />);
    const idle = await renderUI(<StatusDot status="idle" />);

    expect(running.container.querySelector("[role='status']")).not.toBeNull();
    expect(runningQuiet.container.querySelector("[role='status']")).toBeNull();
    expect(idle.container.querySelector("[role='status']")).toBeNull();

    await running.unmount();
    await runningQuiet.unmount();
    await idle.unmount();
  });

  it("pulses only when live is requested and the status animates", async () => {
    const live = await renderUI(<StatusDot status="running" live />);
    const liveOff = await renderUI(<StatusDot status="running" />);
    const notLiveStatus = await renderUI(<StatusDot status="idle" live />);

    expect(dotClass(live.container)).toContain("od-live");
    expect(dotClass(liveOff.container)).not.toContain("od-live");
    expect(dotClass(notLiveStatus.container)).not.toContain("od-live");

    await live.unmount();
    await liveOff.unmount();
    await notLiveStatus.unmount();
  });
});
