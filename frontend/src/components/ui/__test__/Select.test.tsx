/**
 * Select tests (task 3.3): the onclaw-v2 field chain applied to a NATIVE
 * select (no appearance-none — the picker follows the theme's color-scheme
 * per the theming spec), options render through, invalid/disabled states,
 * and ref forwarding.
 */
import { createRef } from "react";
import { describe, expect, it, vi } from "vite-plus/test";

import { Select } from "../Select";
import { renderUI } from "../../__test__/test-utils";

function selectEl(container: HTMLElement): HTMLSelectElement {
  const el = container.querySelector("select");

  // SAFETY: Select always renders a native <select>.
  return el as HTMLSelectElement;
}

const CHILDREN = (
  <>
    <option value="openai">OpenAI</option>
    <option value="anthropic">Anthropic</option>
  </>
);

describe("Select", () => {
  it("renders the field chain on token utilities, keeping native chrome", async () => {
    const { container, unmount } = await renderUI(
      <Select defaultValue="openai">{CHILDREN}</Select>
    );

    const className = selectEl(container).className;

    expect(className).toContain("h-9");
    expect(className).toContain("rounded-md");
    expect(className).toContain("border-line");
    expect(className).toContain("text-fg2");
    expect(className).toContain("focus:border-accent");
    // Native chrome: the kit never strips the picker.
    expect(className).not.toContain("appearance-none");
    expect(className).not.toMatch(/#[0-9a-fA-F]{3,8}|rgb\(/i);

    await unmount();
  });

  it("renders options and reports change events", async () => {
    const onChange = vi.fn();

    const { container, unmount } = await renderUI(
      <Select defaultValue="openai" onChange={onChange}>
        {CHILDREN}
      </Select>
    );

    const select = selectEl(container);

    expect(select.options.length).toBe(2);
    expect(select.value).toBe("openai");

    select.value = "anthropic";
    select.dispatchEvent(new Event("change", { bubbles: true }));

    expect(onChange).toHaveBeenCalledTimes(1);

    await unmount();
  });

  it("invalid adds the danger border and aria-invalid", async () => {
    const valid = await renderUI(<Select>{CHILDREN}</Select>);
    const invalid = await renderUI(<Select invalid>{CHILDREN}</Select>);

    const ok = selectEl(valid.container);
    const bad = selectEl(invalid.container);

    expect(ok.getAttribute("aria-invalid")).toBeNull();

    expect(bad.getAttribute("aria-invalid")).toBe("true");
    expect(bad.className).toContain("border-danger");

    await valid.unmount();
    await invalid.unmount();
  });

  it("disabled collapses into the native attribute", async () => {
    const { container, unmount } = await renderUI(
      <Select disabled>{CHILDREN}</Select>
    );

    const select = selectEl(container);

    expect(select.disabled).toBe(true);
    expect(select.className).toContain("disabled:opacity-50");

    await unmount();
  });

  it("hands the ref to the underlying select element", async () => {
    const ref = createRef<HTMLSelectElement>();

    const { container, unmount } = await renderUI(
      <Select ref={ref}>{CHILDREN}</Select>
    );

    expect(ref.current).toBe(selectEl(container));

    await unmount();
  });

  it("merges caller classes over the chain", async () => {
    const { container, unmount } = await renderUI(
      <Select className="mt-2 h-8">{CHILDREN}</Select>
    );

    const className = selectEl(container).className;

    expect(className).toContain("h-8");
    expect(className).not.toContain("h-9");
    expect(className).toContain("border-line");

    await unmount();
  });
});
