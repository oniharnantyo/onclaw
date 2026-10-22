/**
 * Checkbox tests (task 4.2): a sr-only native input under a styled box — the
 * input carries the semantics (controlled state, label naming, disabled
 * collapse, testId), the box mirrors it with the kit's check glyph, and
 * clicks anywhere on the label report the next state through onChange.
 */
import { act } from "react";
import { fireEvent } from "@testing-library/dom";
import { describe, expect, it, vi } from "vite-plus/test";

import { Checkbox } from "../Checkbox";
import { renderUI } from "../../__test__/test-utils";

function boxEl(container: HTMLElement): HTMLSpanElement {
  // SAFETY: the component renders exactly one aria-hidden box span (the
  // check glyph's svg lives inside it and is not a span).
  const el = container.querySelector(
    'label > span[aria-hidden="true"]'
  ) as HTMLSpanElement | null;

  if (!el) throw new Error("Checkbox rendered no box");

  return el;
}

describe("Checkbox", () => {
  it("renders the label and reflects the controlled state on the input", async () => {
    const on = await renderUI(
      <Checkbox checked onChange={() => {}} label="Include shell outputs" />
    );

    const off = await renderUI(
      <Checkbox
        checked={false}
        onChange={() => {}}
        label="Include shell outputs"
      />
    );

    // Native semantics: the input's checked state is the accessible state —
    // the role query reads it back through the a11y tree.
    expect(on.getByRole("checkbox", { checked: true })).toBeTruthy();
    expect(off.getByRole("checkbox", { checked: false })).toBeTruthy();
    // SAFETY: the component renders exactly one native checkbox input.
    expect((on.getByRole("checkbox") as HTMLInputElement).checked).toBe(true);
    expect(on.getByText("Include shell outputs")).toBeTruthy();
    // On rides the accent token; off is surface with the muted line border.
    expect(boxEl(on.container).className).toContain("bg-accent");
    expect(boxEl(off.container).className).toContain("border-line");
    expect(on.container.firstElementChild?.className).not.toMatch(
      /#[0-9a-fA-F]{3,8}|rgb\(/i
    );

    await on.unmount();
    await off.unmount();
  });

  it("shows the check glyph only when checked", async () => {
    const on = await renderUI(
      <Checkbox checked onChange={() => {}} label="t" />
    );

    const off = await renderUI(
      <Checkbox checked={false} onChange={() => {}} label="t" />
    );

    expect(boxEl(on.container).querySelector("svg")).not.toBeNull();
    expect(boxEl(off.container).querySelector("svg")).toBeNull();

    await on.unmount();
    await off.unmount();
  });

  it("toggles when the label is clicked, reporting the next state", async () => {
    const onChange = vi.fn();

    const { getByText, unmount } = await renderUI(
      <Checkbox
        checked={false}
        onChange={onChange}
        label="Include shell outputs"
      />
    );

    await act(async () => {
      fireEvent.click(getByText("Include shell outputs"));
    });

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith(true);

    await unmount();
  });

  it("toggles when the box is clicked", async () => {
    const onChange = vi.fn();

    const { container, unmount } = await renderUI(
      <Checkbox
        checked={false}
        onChange={onChange}
        label="Include shell outputs"
      />
    );

    await act(async () => {
      fireEvent.click(boxEl(container));
    });

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith(true);

    await unmount();
  });

  it("toggles from Space on the focused input", async () => {
    const onChange = vi.fn();

    const { getByTestId, unmount } = await renderUI(
      <Checkbox
        checked={false}
        onChange={onChange}
        label="Include shell outputs"
        testId="cb-input"
      />
    );

    // SAFETY: testId names the component's only element — the native input.
    const input = getByTestId("cb-input") as HTMLInputElement;

    input.focus();
    // The sr-only input keeps keyboard focus (the styled box is aria-hidden).
    expect(document.activeElement).toBe(input);

    // SAFETY: jsdom does not synthesize the activation click a browser
    // produces from Space on a focused checkbox, so fire the click the
    // keypress would trigger. The component adds no key handler by design —
    // the native input owns keyboard toggling.
    await act(async () => {
      fireEvent.click(input);
    });

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith(true);

    await unmount();
  });

  it("does not report changes while disabled and collapses into the native attribute", async () => {
    const onChange = vi.fn();

    const { getByTestId, getByText, container, unmount } = await renderUI(
      <Checkbox
        checked={false}
        onChange={onChange}
        label="Include shell outputs"
        disabled
        testId="cb-input"
      />
    );

    // SAFETY: testId names the component's only element — the native input.
    const input = getByTestId("cb-input") as HTMLInputElement;

    expect(input.disabled).toBe(true);

    // The user-level interactions (label-forwarded and box clicks) are inert.
    // A direct dispatch on the disabled input is deliberately not fired: jsdom
    // runs activation behavior for programmatic clicks that a real browser
    // suppresses on disabled controls, so it would test jsdom, not the kit.
    await act(async () => {
      fireEvent.click(getByText("Include shell outputs"));
      fireEvent.click(boxEl(container));
    });

    expect(onChange).not.toHaveBeenCalled();

    await unmount();
  });

  it("exposes the testId on the native input for pane tests", async () => {
    const { getByTestId, unmount } = await renderUI(
      <Checkbox
        checked={false}
        onChange={() => {}}
        label="Include shell outputs"
        testId="x-checkbox"
      />
    );

    const input = getByTestId("x-checkbox");

    expect(input.tagName).toBe("INPUT");
    // SAFETY: the tagName assertion above proves it is an input element.
    expect((input as HTMLInputElement).type).toBe("checkbox");

    await unmount();
  });
});
