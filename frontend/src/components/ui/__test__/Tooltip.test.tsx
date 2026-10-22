/**
 * Tooltip tests (onclaw-v2 parity, task 2.1): delay-gated opening, the
 * aria-describedby wiring, every hide path (mouse leave, trigger Escape,
 * global window Escape), placement classes, child-handler passthrough, and
 * the disabled/empty-content short-circuit. Rendered through the repo's
 * renderUI helper (no @testing-library/react) with fake timers.
 *
 * Enter/leave and focus/blur are dispatched as the bubbling events React's
 * delegated listeners actually receive in jsdom (mouseover/mouseout,
 * focusin/focusout) — fireEvent.focus/mouseEnter emit non-bubbling events
 * this environment never delivers to React (same approach as the
 * ModalShell test's direct dispatchEvent).
 */
import { act } from "react";
import { fireEvent } from "@testing-library/dom";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vite-plus/test";

import { Tooltip } from "../Tooltip";
import { renderUI } from "../../__test__/test-utils";

function fireEnter(el: HTMLElement): void {
  el.dispatchEvent(new MouseEvent("mouseover", { bubbles: true }));
}

function fireLeave(el: HTMLElement): void {
  el.dispatchEvent(new MouseEvent("mouseout", { bubbles: true }));
}

function fireFocus(el: HTMLElement): void {
  el.dispatchEvent(new FocusEvent("focusin", { bubbles: true }));
}

function fireBlur(el: HTMLElement): void {
  el.dispatchEvent(new FocusEvent("focusout", { bubbles: true }));
}

/** Opens the tooltip on the trigger by focusing it through the 150ms delay. */
async function openByFocus(trigger: HTMLElement): Promise<void> {
  await act(async () => {
    fireFocus(trigger);
    vi.advanceTimersByTime(150);
  });
}

describe("Tooltip", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders child element and keeps tooltip hidden initially", async () => {
    const { getByRole, queryByRole, unmount } = await renderUI(
      <Tooltip content="Helpful info">
        <button type="button">Hover me</button>
      </Tooltip>
    );

    expect(getByRole("button", { name: "Hover me" })).toBeTruthy();
    expect(queryByRole("tooltip")).toBeNull();

    await unmount();
  });

  it("shows tooltip after 150ms hover delay and sets aria-describedby", async () => {
    const { getByRole, queryByRole, unmount } = await renderUI(
      <Tooltip content="Workspaces" id="ws-tooltip">
        <button type="button">WS</button>
      </Tooltip>
    );

    const btn = getByRole("button", { name: "WS" });

    expect(btn.getAttribute("aria-describedby")).toBeNull();

    await act(async () => {
      fireEnter(btn);
    });
    // Not visible immediately
    expect(queryByRole("tooltip")).toBeNull();

    // Advance 149ms - still not visible
    await act(async () => {
      vi.advanceTimersByTime(149);
    });
    expect(queryByRole("tooltip")).toBeNull();

    // Advance 1ms - visible at 150ms
    await act(async () => {
      vi.advanceTimersByTime(1);
    });
    const tooltip = getByRole("tooltip");

    expect(tooltip.textContent).toBe("Workspaces");
    expect(tooltip.id).toBe("ws-tooltip");
    expect(btn.getAttribute("aria-describedby")).toBe("ws-tooltip");

    await unmount();
  });

  it("hides tooltip on mouse leave and clears pending timer", async () => {
    const { getByRole, queryByRole, unmount } = await renderUI(
      <Tooltip content="Workspaces">
        <button type="button">WS</button>
      </Tooltip>
    );

    const btn = getByRole("button", { name: "WS" });

    // Start hover then leave before delay
    await act(async () => {
      fireEnter(btn);
      vi.advanceTimersByTime(50);
    });
    await act(async () => {
      fireLeave(btn);
      vi.advanceTimersByTime(200);
    });
    expect(queryByRole("tooltip")).toBeNull();

    // Hover again, let it open, then leave
    await act(async () => {
      fireEnter(btn);
      vi.advanceTimersByTime(150);
    });
    expect(queryByRole("tooltip")).not.toBeNull();

    await act(async () => {
      fireLeave(btn);
    });
    expect(queryByRole("tooltip")).toBeNull();

    await unmount();
  });

  it("shows tooltip on focus and hides on blur", async () => {
    const { getByRole, queryByRole, unmount } = await renderUI(
      <Tooltip content="Settings">
        <button type="button">Settings</button>
      </Tooltip>
    );

    const btn = getByRole("button", { name: "Settings" });

    await openByFocus(btn);
    expect(getByRole("tooltip").textContent).toBe("Settings");

    await act(async () => {
      fireBlur(btn);
    });
    expect(queryByRole("tooltip")).toBeNull();

    await unmount();
  });

  it("dismisses tooltip on Escape key press on trigger", async () => {
    const { getByRole, queryByRole, unmount } = await renderUI(
      <Tooltip content="Settings">
        <button type="button">Settings</button>
      </Tooltip>
    );

    const btn = getByRole("button", { name: "Settings" });

    await openByFocus(btn);
    expect(queryByRole("tooltip")).not.toBeNull();

    await act(async () => {
      fireEvent.keyDown(btn, { key: "Escape" });
    });
    expect(queryByRole("tooltip")).toBeNull();

    await unmount();
  });

  it("dismisses tooltip on global window Escape key press", async () => {
    const { getByRole, queryByRole, unmount } = await renderUI(
      <Tooltip content="Global Escape Test">
        <button type="button">Trigger</button>
      </Tooltip>
    );

    const btn = getByRole("button", { name: "Trigger" });

    await act(async () => {
      fireEnter(btn);
      vi.advanceTimersByTime(150);
    });
    expect(queryByRole("tooltip")).not.toBeNull();

    // The component's own window listener sees this directly.
    await act(async () => {
      fireEvent.keyDown(window, { key: "Escape" });
    });
    expect(queryByRole("tooltip")).toBeNull();

    await unmount();
  });

  it("applies custom placement classes", async () => {
    // renderUI has no rerender; each placement mounts fresh.
    const top = await renderUI(
      <Tooltip content="Top tip" placement="top">
        <button type="button">Btn</button>
      </Tooltip>
    );

    await openByFocus(top.getByRole("button", { name: "Btn" }));
    expect(top.getByRole("tooltip").className).toContain(
      "bottom-[calc(100%+8px)]"
    );

    await top.unmount();

    const left = await renderUI(
      <Tooltip content="Left tip" placement="left">
        <button type="button">Btn</button>
      </Tooltip>
    );

    await openByFocus(left.getByRole("button", { name: "Btn" }));
    expect(left.getByRole("tooltip").className).toContain(
      "right-[calc(100%+8px)]"
    );

    await left.unmount();
  });

  it("preserves existing child event handlers", async () => {
    const onClick = vi.fn();
    const onMouseEnter = vi.fn();

    const { getByRole, unmount } = await renderUI(
      <Tooltip content="Info">
        <button type="button" onClick={onClick} onMouseEnter={onMouseEnter}>
          Clickable
        </button>
      </Tooltip>
    );

    const btn = getByRole("button", { name: "Clickable" });

    await act(async () => {
      fireEnter(btn);
    });
    expect(onMouseEnter).toHaveBeenCalledTimes(1);

    await act(async () => {
      fireEvent.click(btn);
    });
    expect(onClick).toHaveBeenCalledTimes(1);

    await unmount();
  });

  it("does not show tooltip when disabled or empty content", async () => {
    const empty = await renderUI(
      <Tooltip content="" disabled={false}>
        <button type="button">Disabled content</button>
      </Tooltip>
    );

    const btn1 = empty.getByRole("button", { name: "Disabled content" });

    await act(async () => {
      fireEnter(btn1);
      vi.advanceTimersByTime(200);
    });
    expect(empty.queryByRole("tooltip")).toBeNull();

    await empty.unmount();

    const disabled = await renderUI(
      <Tooltip content="Info" disabled={true}>
        <button type="button">Disabled flag</button>
      </Tooltip>
    );

    const btn2 = disabled.getByRole("button", { name: "Disabled flag" });

    await act(async () => {
      fireEnter(btn2);
      vi.advanceTimersByTime(200);
    });
    expect(disabled.queryByRole("tooltip")).toBeNull();

    await disabled.unmount();
  });
});
