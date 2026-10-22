/**
 * ConfirmDialog tests: defaults and label overrides, tone → kit Button
 * variant mapping (error renders the destructive chain), the disabled
 * confirm path, and the onClose/onConfirm wiring through the footer row.
 */
import { act } from "react";
import userEvent from "@testing-library/user-event";
import { fireEvent } from "@testing-library/dom";
import { describe, expect, it, vi } from "vite-plus/test";

import { ConfirmDialog } from "../ConfirmDialog";
import { renderUI } from "./test-utils";

describe("ConfirmDialog", () => {
  it("renders default labels and the cannot-undo line", async () => {
    const { getByRole, getByText, unmount } = await renderUI(
      <ConfirmDialog
        title="Remove agent?"
        onConfirm={() => {}}
        onClose={() => {}}
      />
    );

    expect(getByRole("dialog", { name: "Remove agent?" })).toBeTruthy();
    expect(getByText("This cannot be undone.")).toBeTruthy();
    expect(getByRole("button", { name: "Delete" })).toBeTruthy();
    expect(getByRole("button", { name: "Cancel" })).toBeTruthy();

    await unmount();
  });

  it("maps the error tone to the destructive button chain", async () => {
    const { getByRole, unmount } = await renderUI(
      <ConfirmDialog title="D" onConfirm={() => {}} onClose={() => {}} />
    );

    expect(getByRole("button", { name: "Delete" }).className).toContain(
      "bg-danger"
    );

    await unmount();
  });

  it("maps the primary tone to the accent button chain", async () => {
    const { getByRole, unmount } = await renderUI(
      <ConfirmDialog
        title="D"
        tone="primary"
        confirmLabel="Save"
        onConfirm={() => {}}
        onClose={() => {}}
      />
    );

    expect(getByRole("button", { name: "Save" }).className).toContain(
      "bg-accent"
    );

    await unmount();
  });

  it("renders body copy above the action row", async () => {
    const { getByText, unmount } = await renderUI(
      <ConfirmDialog title="D" onConfirm={() => {}} onClose={() => {}}>
        <p>Sessions stay orphaned.</p>
      </ConfirmDialog>
    );

    expect(getByText("Sessions stay orphaned.")).toBeTruthy();

    await unmount();
  });

  it("fires onConfirm and onClose from the action row", async () => {
    const onConfirm = vi.fn();
    const onClose = vi.fn();

    const { getByRole, unmount } = await renderUI(
      <ConfirmDialog
        title="D"
        confirmTestId="confirm"
        cancelTestId="cancel"
        onConfirm={onConfirm}
        onClose={onClose}
      />
    );

    await act(async () => {
      fireEvent.click(getByRole("button", { name: "Delete" }));
    });

    expect(onConfirm).toHaveBeenCalledTimes(1);

    await act(async () => {
      fireEvent.click(getByRole("button", { name: "Cancel" }));
    });

    expect(onClose).toHaveBeenCalledTimes(1);

    await unmount();
  });

  it("honors a disabled confirm", async () => {
    const onConfirm = vi.fn();
    const user = userEvent.setup();

    const { getByRole, unmount } = await renderUI(
      <ConfirmDialog
        title="D"
        confirmDisabled
        onConfirm={onConfirm}
        onClose={() => {}}
      />
    );

    // SAFETY: ConfirmDialog always renders a kit <Button> per action; tests
    // fail loudly if not.
    const confirm = getByRole("button", {
      name: "Delete",
    }) as HTMLButtonElement;

    expect(confirm.disabled).toBe(true);

    await user.click(confirm);

    expect(onConfirm).not.toHaveBeenCalled();

    await unmount();
  });
});
