/**
 * Toggle tests (task 4.3): a role="switch" button that reflects the
 * controlled state in aria-checked, reports the NEXT state on click, names
 * itself from `label`, goes inert while disabled, and rides tokens only.
 */
import { act } from "react";
import { fireEvent } from "@testing-library/dom";
import { describe, expect, it, vi } from "vite-plus/test";

import { Toggle } from "../Toggle";
import { renderUI } from "../../__test__/test-utils";

function switchEl(container: HTMLElement): HTMLButtonElement {
  // SAFETY: the component renders exactly one role=switch button.
  const el = container.querySelector(
    'button[role="switch"]'
  ) as HTMLButtonElement | null;

  if (!el) throw new Error("Toggle rendered no switch");

  return el;
}

describe("Toggle", () => {
  it("renders a labelled switch reflecting the controlled state", async () => {
    const on = await renderUI(
      <Toggle on onChange={() => {}} label="Enable atlas" />
    );

    const off = await renderUI(
      <Toggle on={false} onChange={() => {}} label="Enable atlas" />
    );

    const onEl = switchEl(on.container);
    const offEl = switchEl(off.container);

    expect(onEl.getAttribute("aria-checked")).toBe("true");
    expect(onEl.getAttribute("aria-label")).toBe("Enable atlas");
    expect(onEl.title).toBe("Enable atlas");
    expect(offEl.getAttribute("aria-checked")).toBe("false");
    // On rides the accent token; off is a transparent fg mix (both themes).
    expect(onEl.className).toContain("bg-accent");
    expect(offEl.className).toContain("var(--fg)");
    expect(onEl.className).not.toMatch(/#[0-9a-fA-F]{3,8}|rgb\(/i);

    await on.unmount();
    await off.unmount();
  });

  it("reports the next state on click", async () => {
    const onChange = vi.fn();

    const { container, unmount } = await renderUI(
      <Toggle on onChange={onChange} label="Enable atlas" />
    );

    await act(async () => {
      fireEvent.click(switchEl(container));
    });

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith(false);

    await unmount();
  });

  it("moves the knob only when on", async () => {
    const on = await renderUI(<Toggle on onChange={() => {}} label="t" />);

    const off = await renderUI(
      <Toggle on={false} onChange={() => {}} label="t" />
    );

    expect(switchEl(on.container).textContent).toBe("");
    expect(switchEl(on.container).firstElementChild?.className).toContain(
      "translate-x-4"
    );
    expect(switchEl(off.container).firstElementChild?.className).not.toContain(
      "translate-x-4"
    );

    await on.unmount();
    await off.unmount();
  });

  it("does not report changes while disabled", async () => {
    const onChange = vi.fn();

    const { container, unmount } = await renderUI(
      <Toggle on={false} onChange={onChange} label="Enable atlas" disabled />
    );

    const el = switchEl(container);

    expect(el.disabled).toBe(true);

    await act(async () => {
      fireEvent.click(el);
    });

    expect(onChange).not.toHaveBeenCalled();

    await unmount();
  });

  it("exposes the testId for pane tests", async () => {
    const { getByTestId, unmount } = await renderUI(
      <Toggle
        on={false}
        onChange={() => {}}
        label="Enable atlas"
        testId="x-toggle"
      />
    );

    expect(getByTestId("x-toggle")).toBeTruthy();

    await unmount();
  });
});
