/**
 * Modal tests (task 3.1): dialog semantics, Escape swallows the key and
 * closes, Tab is trapped inside the box, focus parks on the shell on open
 * and returns to the trigger on close, the backdrop and close button
 * dismiss, and the wide/testId props flow through.
 */
import { act } from "react";
import { fireEvent } from "@testing-library/dom";
import { describe, expect, it, vi } from "vite-plus/test";

import { Modal } from "../Modal";
import { renderUI } from "../../__test__/test-utils";

/** Fires a keydown on the window inside act so the modal's listener runs. */
async function pressKey(
  key: string,
  shiftKey: boolean = false
): Promise<KeyboardEvent> {
  const event = new KeyboardEvent("keydown", {
    key,
    shiftKey,
    cancelable: true,
  });

  await act(async () => {
    window.dispatchEvent(event);
  });

  return event;
}

describe("Modal", () => {
  it("renders a named modal dialog with the title, body, and footer", async () => {
    const { getByRole, getByText, unmount } = await renderUI(
      <Modal title="Configure" onClose={() => {}} footer={<button>Add</button>}>
        <p>Body copy</p>
      </Modal>
    );

    expect(
      getByRole("dialog", { name: "Configure" }).getAttribute("aria-modal")
    ).toBe("true");
    expect(getByText("Body copy")).toBeTruthy();
    expect(getByText("Add")).toBeTruthy();

    await unmount();
  });

  it("closes on Escape, swallowing the key", async () => {
    const onClose = vi.fn();

    const { unmount } = await renderUI(<Modal title="D" onClose={onClose} />);

    const event = await pressKey("Escape");

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(event.defaultPrevented).toBe(true);

    await unmount();
  });

  it("ignores other keys", async () => {
    const onClose = vi.fn();

    const { unmount } = await renderUI(<Modal title="D" onClose={onClose} />);

    await pressKey("Enter");
    await pressKey("a");

    expect(onClose).not.toHaveBeenCalled();

    await unmount();
  });

  it("stops listening after unmount", async () => {
    const onClose = vi.fn();

    const { unmount } = await renderUI(<Modal title="D" onClose={onClose} />);

    await unmount();
    await pressKey("Escape");

    expect(onClose).not.toHaveBeenCalled();
  });

  it("parks focus on the box on open and restores it to the trigger on close", async () => {
    const trigger = await renderUI(<button type="button">Open</button>);
    const triggerButton = trigger.getByRole("button");
    triggerButton.focus();
    expect(document.activeElement).toBe(triggerButton);

    const modal = await renderUI(<Modal title="D" onClose={() => {}} />);

    // SAFETY: Modal always renders the .od-pop dialog box; tests fail loudly
    // if not.
    const box = modal
      .getByRole("dialog")
      .querySelector(".od-pop") as HTMLElement;

    expect(document.activeElement).toBe(box);

    await modal.unmount();

    expect(document.activeElement).toBe(triggerButton);

    await trigger.unmount();
  });

  it("traps Tab: shift-tab from the shell wraps to the last control", async () => {
    const { getByRole, unmount } = await renderUI(
      <Modal title="D" onClose={() => {}} footer={<button>Go</button>} />
    );

    // SAFETY: Modal always renders the .od-pop dialog box; tests fail loudly
    // if not.
    const box = getByRole("dialog").querySelector(".od-pop") as HTMLElement;
    box.focus();

    const event = await pressKey("Tab", true);

    expect(event.defaultPrevented).toBe(true);
    // Last control in DOM order: the footer button sits after the header's
    // close button.
    expect(document.activeElement).toBe(getByRole("button", { name: "Go" }));

    await unmount();
  });

  it("closes from the backdrop and the close button", async () => {
    const onClose = vi.fn();

    const first = await renderUI(<Modal title="D" onClose={onClose} />);

    await act(async () => {
      fireEvent.click(
        first.getByRole("dialog").querySelector('[aria-hidden="true"]')!
      );
    });
    expect(onClose).toHaveBeenCalledTimes(1);

    const second = await renderUI(<Modal title="D" onClose={onClose} />);

    await act(async () => {
      fireEvent.click(second.getByRole("button", { name: "Close dialog" }));
    });
    expect(onClose).toHaveBeenCalledTimes(2);

    await first.unmount();
    await second.unmount();
  });

  it("honors wide and testId", async () => {
    const wide = await renderUI(
      <Modal title="D" onClose={() => {}} wide testId="agent-dialog" />
    );

    // SAFETY: Modal always renders the .od-pop dialog box; tests fail loudly
    // if not.
    const box = wide
      .getByRole("dialog")
      .querySelector(".od-pop") as HTMLElement;

    expect(box.className).toContain("max-w-4xl");
    expect(wide.getByTestId("agent-dialog")).toBeTruthy();

    const narrow = await renderUI(<Modal title="D" onClose={() => {}} />);

    // SAFETY: Modal always renders the .od-pop dialog box; tests fail loudly
    // if not.
    const narrowBox = narrow
      .getByRole("dialog")
      .querySelector(".od-pop") as HTMLElement;

    expect(narrowBox.className).toContain("max-w-lg");

    await wide.unmount();
    await narrow.unmount();
  });
});
