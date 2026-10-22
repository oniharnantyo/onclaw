import { act } from "react";
import { describe, expect, it, vi } from "vite-plus/test";

import { ModalShell } from "../ModalShell";
import { renderUI } from "./test-utils";

/** Fires a keydown on the window inside act so the shell's listener runs. */
async function pressKey(key: string): Promise<KeyboardEvent> {
  const event = new KeyboardEvent("keydown", { key, cancelable: true });

  await act(async () => {
    window.dispatchEvent(event);
  });

  return event;
}

describe("ModalShell", () => {
  it("closes on Escape and swallows the key", async () => {
    const onClose = vi.fn();

    const { unmount } = await renderUI(
      <ModalShell title="Dialog" onClose={onClose}>
        <p>Body</p>
      </ModalShell>
    );

    const event = await pressKey("Escape");

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(event.defaultPrevented).toBe(true);

    await unmount();
  });

  it("ignores other keys", async () => {
    const onClose = vi.fn();

    const { unmount } = await renderUI(
      <ModalShell title="Dialog" onClose={onClose} />
    );

    await pressKey("Enter");
    await pressKey("Tab");

    expect(onClose).not.toHaveBeenCalled();

    await unmount();
  });

  it("stops listening after unmount", async () => {
    const onClose = vi.fn();

    const { unmount } = await renderUI(
      <ModalShell title="Dialog" onClose={onClose} />
    );

    await unmount();
    await pressKey("Escape");

    expect(onClose).not.toHaveBeenCalled();
  });

  it("renders the dialog on the ported kit Modal, not daisyUI classes", async () => {
    const { getByRole, getByText, unmount } = await renderUI(
      <ModalShell
        title="Configure"
        description="Point the agent at a provider."
        testId="config-dialog"
        wide
        onClose={() => {}}
        actions={<button type="button">Save</button>}
      >
        <p>Body</p>
      </ModalShell>
    );

    const dialog = getByRole("dialog", { name: "Configure" });

    expect(dialog.getAttribute("aria-modal")).toBe("true");
    expect(dialog.getAttribute("data-testid")).toBe("config-dialog");
    expect(dialog.textContent).toContain("Body");
    expect(getByText("Point the agent at a provider.")).toBeTruthy();
    expect(getByText("Save")).toBeTruthy();

    // The daisyUI modal families and their wells are gone; the box keeps the
    // glass-overlay hook and the token surface chain.
    // SAFETY: ModalShell always passes the glass-overlay boxClassName, so the
    // box exists; tests fail loudly if not.
    const box = dialog.querySelector(".glass-overlay") as HTMLElement;

    expect(box.className).toContain("bg-surface");
    expect(box.className).toContain("border-line");
    expect(dialog.className).not.toContain("modal");
    expect(box.className).not.toContain("modal-box");

    await unmount();
  });

  it("locks body scroll while open and releases it on unmount (task 3.6)", async () => {
    const { unmount } = await renderUI(
      <ModalShell title="Dialog" onClose={() => {}} />
    );

    expect(document.body.style.overflow).toBe("hidden");
    expect(document.documentElement.style.overflow).toBe("hidden");

    await unmount();

    expect(document.body.style.overflow).toBe("");
    expect(document.documentElement.style.overflow).toBe("");
  });

  it("keeps the lock while a stacked dialog is open (reference counted)", async () => {
    const first = await renderUI(
      <ModalShell title="Dialog" onClose={() => {}} />
    );

    const second = await renderUI(
      <ModalShell title="Confirm" onClose={() => {}} />
    );

    expect(document.body.style.overflow).toBe("hidden");

    await first.unmount();

    expect(document.body.style.overflow).toBe("hidden");

    await second.unmount();

    expect(document.body.style.overflow).toBe("");
  });
});
