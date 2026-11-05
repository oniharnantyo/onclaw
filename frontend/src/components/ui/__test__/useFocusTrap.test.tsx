// useFocusTrap tests (task 7.1): the shared Tab trap cycles focus inside the
// container — Tab at the last focusable wraps to the first, Shift+Tab at the
// first wraps to the last — and can be switched off via `enabled`.
import { act, useRef } from "react";
import { describe, expect, it } from "vite-plus/test";

import { useFocusTrap } from "../useFocusTrap";
import { renderUI } from "../../__test__/test-utils";

function TrapHarness({ enabled = true }: { enabled?: boolean }) {
  const boxRef = useRef<HTMLDivElement>(null);

  useFocusTrap(boxRef, enabled);

  return (
    <div ref={boxRef} data-testid="box">
      <button type="button">First</button>
      <button type="button">Last</button>
    </div>
  );
}

/** Fires a cancelable keydown on the window inside act (Modal.test pattern). */
async function pressTab(shiftKey = false): Promise<KeyboardEvent> {
  const event = new KeyboardEvent("keydown", {
    key: "Tab",
    shiftKey,
    cancelable: true,
  });

  await act(async () => {
    window.dispatchEvent(event);
  });

  return event;
}

describe("useFocusTrap", () => {
  it("wraps Tab at the last focusable back to the first", async () => {
    const { unmount } = await renderUI(<TrapHarness />);
    const buttons = () => document.querySelectorAll("button");

    buttons()[1]!.focus();
    expect(document.activeElement).toBe(buttons()[1]);

    const event = await pressTab();

    expect(event.defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(buttons()[0]);

    await unmount();
  });

  it("wraps Shift+Tab at the first focusable back to the last", async () => {
    const { unmount } = await renderUI(<TrapHarness />);
    const buttons = () => document.querySelectorAll("button");

    buttons()[0]!.focus();
    expect(document.activeElement).toBe(buttons()[0]);

    const event = await pressTab(true);

    expect(event.defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(buttons()[1]);

    await unmount();
  });

  it("does not intercept Tab when disabled", async () => {
    const { unmount } = await renderUI(<TrapHarness enabled={false} />);

    document.querySelectorAll("button")[1]!.focus();

    const event = await pressTab();

    expect(event.defaultPrevented).toBe(false);

    await unmount();
  });
});
