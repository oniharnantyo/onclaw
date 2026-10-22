/**
 * StatusDot tests: renders the token-based dot for each status, exposes the
 * status as a title tooltip, and gates the live pulse on both the `live` prop
 * and the status opting in.
 */
import { describe, expect, it } from "vite-plus/test";

import { StatusDot } from "../StatusDot";
import { renderUI } from "../../__test__/test-utils";

/** Class list of the rendered dot; StatusDot renders exactly one span. */
function dotClass(container: HTMLElement): string {
  const el = container.querySelector("span");

  if (!el) throw new Error("StatusDot rendered no span");

  return el.className;
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

  it("exposes the status as a title tooltip", async () => {
    const { container, unmount } = await renderUI(<StatusDot status="error" />);

    const el = container.querySelector("span");

    expect(el?.title).toBe("Needs attention");

    await unmount();
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
