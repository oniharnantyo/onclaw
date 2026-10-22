/**
 * Input tests (task 3.3): the onclaw-v2 inputCls chain on token utilities,
 * the hover/focus/disabled states, the invalid state's danger border +
 * aria-invalid (D9 detached-validation fix), ref forwarding, and the
 * single-ring discipline (no added focus ring classes).
 */
import { createRef } from "react";
import { describe, expect, it, vi } from "vite-plus/test";

import { Input } from "../Input";
import { renderUI } from "../../__test__/test-utils";

function inputEl(container: HTMLElement): HTMLInputElement {
  const el = container.querySelector("input");

  // SAFETY: Input always renders a native <input>; tests fail loudly if not.
  return el as HTMLInputElement;
}

describe("Input", () => {
  it("renders the onclaw-v2 field chain on token utilities", async () => {
    const { container, unmount } = await renderUI(
      <Input placeholder="Agent name" />
    );

    const className = inputEl(container).className;

    expect(className).toContain("h-9");
    expect(className).toContain("rounded-md");
    expect(className).toContain("border-line");
    expect(className).toContain("text-fg2");
    expect(className).toContain("placeholder:text-muted");
    expect(className).toContain("focus:border-accent");
    expect(className).not.toMatch(/#[0-9a-fA-F]{3,8}|rgb\(/i);

    await unmount();
  });

  it("hovers to a stronger border and suppresses the outline locally", async () => {
    const { container, unmount } = await renderUI(<Input />);

    const className = inputEl(container).className;

    expect(className).toContain("hover:border-");
    expect(className).toContain("focus:outline-none");

    await unmount();
  });

  it("adds no second focus ring (global :focus-visible contract suffices)", async () => {
    const { container, unmount } = await renderUI(<Input />);

    const className = inputEl(container).className;

    expect(className).not.toContain("ring");
    expect(className).not.toContain("shadow");
    expect(className).not.toContain("focus-visible");

    await unmount();
  });

  it("invalid adds the danger border and aria-invalid", async () => {
    const valid = await renderUI(<Input />);
    const invalid = await renderUI(<Input invalid />);

    const ok = inputEl(valid.container);
    const bad = inputEl(invalid.container);

    expect(ok.getAttribute("aria-invalid")).toBeNull();
    expect(ok.className).not.toContain("border-danger");

    expect(bad.getAttribute("aria-invalid")).toBe("true");
    expect(bad.className).toContain("border-danger");
    expect(bad.className).toContain("focus:border-danger");

    await valid.unmount();
    await invalid.unmount();
  });

  it("disabled collapses into the native attribute", async () => {
    const onChange = vi.fn();

    const { container, unmount } = await renderUI(
      <Input disabled onChange={onChange} defaultValue="x" />
    );

    const input = inputEl(container);

    expect(input.disabled).toBe(true);
    expect(input.className).toContain("disabled:opacity-50");
    expect(input.className).toContain("disabled:cursor-not-allowed");

    await unmount();
  });

  it("hands the ref to the underlying input element", async () => {
    const ref = createRef<HTMLInputElement>();

    const { container, unmount } = await renderUI(<Input ref={ref} />);

    expect(ref.current).toBe(inputEl(container));

    await unmount();
  });

  it("merges caller classes over the chain (mono payload case)", async () => {
    const { container, unmount } = await renderUI(
      <Input className="font-mono text-[12px]" />
    );

    const className = inputEl(container).className;

    expect(className).toContain("font-mono");
    expect(className).toContain("text-[12px]");
    // The field chain survives the override.
    expect(className).toContain("border-line");

    await unmount();
  });

  it("passes value/onChange and attributes through", async () => {
    const { container, unmount } = await renderUI(
      <Input
        id="agent-name"
        type="text"
        defaultValue="buddy"
        autoFocus
        data-testid="name-field"
      />
    );

    const input = inputEl(container);

    expect(input.id).toBe("agent-name");
    expect(input.value).toBe("buddy");
    expect(input.getAttribute("data-testid")).toBe("name-field");

    await unmount();
  });
});
