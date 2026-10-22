/**
 * Toasts tests (task 2.1): the stack renders the lib/toasts module store —
 * toast() appends into a polite status region, danger toasts carry the alert
 * glyph in text-danger while others carry the check glyph in text-accent,
 * and toasts auto-dismiss after 3.2s (onclaw-v2 parity).
 */
import { act } from "react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import {
  dismissToast,
  getToasts,
  toast,
  type Toast,
} from "../../../lib/toasts";
import { Toasts } from "../Toasts";
import { renderUI } from "../../__test__/test-utils";

afterEach(() => {
  // The module store outlives each test; drain it so assertions stay local.
  getToasts().forEach((t: Toast) => dismissToast(t.id));
  vi.useRealTimers();
});

describe("Toasts", () => {
  it("renders appended toasts inside a polite status region", async () => {
    const { getByRole, getByText, queryByText, unmount } = await renderUI(
      <Toasts />
    );

    expect(getByRole("status")).toBeTruthy();

    await act(async () => {
      toast("Saved");
    });

    expect(getByText("Saved")).toBeTruthy();

    await act(async () => {
      dismissToast(getToasts()[0]?.id ?? "");
    });
    expect(queryByText("Saved")).toBeNull();

    await unmount();
  });

  it("marks danger toasts with text-danger and others with text-accent", async () => {
    const { getByText, unmount } = await renderUI(<Toasts />);

    await act(async () => {
      toast("Deleted", "danger");
      toast("Saved");
    });

    const dangerGlyph =
      getByText("Deleted").parentElement?.querySelector("span");

    const infoGlyph = getByText("Saved").parentElement?.querySelector("span");

    expect(dangerGlyph?.className).toContain("text-danger");
    expect(dangerGlyph?.querySelector("svg")).not.toBeNull();
    expect(infoGlyph?.className).toContain("text-accent");
    expect(infoGlyph?.className).not.toContain("text-danger");

    await unmount();
  });

  it("auto-dismisses toasts after 3.2s", async () => {
    vi.useFakeTimers();

    const { getByText, queryByText, unmount } = await renderUI(<Toasts />);

    await act(async () => {
      toast("Temporary");
    });
    expect(getByText("Temporary")).toBeTruthy();

    await act(async () => {
      vi.advanceTimersByTime(3199);
    });
    expect(getByText("Temporary")).toBeTruthy();

    await act(async () => {
      vi.advanceTimersByTime(1);
    });
    expect(queryByText("Temporary")).toBeNull();

    await unmount();
  });
});
