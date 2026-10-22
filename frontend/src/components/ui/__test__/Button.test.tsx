/**
 * Button tests (task 1.4): variant chains are token-based, disabled/loading
 * collapse into native semantics with the spinner shown, clicks pass through
 * only when interactive, the ref reaches the element, and — per the D9
 * double-ring fix — no focus/shadow ring classes are added (the global
 * :focus-visible outline is the single ring).
 */
import { createRef } from "react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vite-plus/test";

import { Button } from "../Button";
import { renderUI } from "../../__test__/test-utils";

/** The single <button> a Button render produces. */
function buttonEl(container: HTMLElement): HTMLButtonElement {
  const el = container.querySelector("button");

  // SAFETY: Button always renders a native <button>; tests fail loudly if not.
  return el as HTMLButtonElement;
}

describe("Button", () => {
  it("renders variant chains as token utilities, not literal colors", async () => {
    const primary = await renderUI(<Button>Save</Button>);
    const ghost = await renderUI(<Button variant="ghost">Cancel</Button>);

    const destructive = await renderUI(
      <Button variant="destructive">Delete</Button>
    );

    expect(buttonEl(primary.container).className).toContain("bg-accent");
    expect(buttonEl(primary.container).className).toContain("text-accenton");
    expect(buttonEl(ghost.container).className).toContain("border-line");
    expect(buttonEl(ghost.container).className).toContain("text-fg2");
    expect(buttonEl(destructive.container).className).toContain("bg-danger");

    // No raw hex / rgb literals anywhere in the class lists.
    for (const rendered of [primary, ghost, destructive]) {
      expect(buttonEl(rendered.container).className).not.toMatch(
        /#[0-9a-fA-F]{3,8}|rgb\(/i
      );
    }

    await primary.unmount();
    await ghost.unmount();
    await destructive.unmount();
  });

  it("renders sm and md sizes", async () => {
    const sm = await renderUI(<Button size="sm">Go</Button>);
    const md = await renderUI(<Button>Go</Button>);

    expect(buttonEl(sm.container).className).toContain("h-8");
    expect(buttonEl(md.container).className).toContain("h-9");

    await sm.unmount();
    await md.unmount();
  });

  it("defaults type to button and passes explicit types through", async () => {
    const def = await renderUI(<Button>Save</Button>);
    const submit = await renderUI(<Button type="submit">Save</Button>);

    expect(buttonEl(def.container).type).toBe("button");
    expect(buttonEl(submit.container).type).toBe("submit");

    await def.unmount();
    await submit.unmount();
  });

  it("hands the ref to the underlying button element", async () => {
    const ref = createRef<HTMLButtonElement>();

    const { container, unmount } = await renderUI(
      <Button ref={ref}>Save</Button>
    );

    expect(ref.current).toBe(buttonEl(container));

    await unmount();
  });

  it("passes clicks through while enabled and with loading off", async () => {
    const onClick = vi.fn();
    const user = userEvent.setup();

    const { container, unmount } = await renderUI(
      <Button onClick={onClick}>Save</Button>
    );

    await user.click(buttonEl(container));

    expect(onClick).toHaveBeenCalledTimes(1);

    await unmount();
  });

  it("disabled renders native-disabled and swallows clicks", async () => {
    const onClick = vi.fn();
    const user = userEvent.setup();

    const { container, unmount } = await renderUI(
      <Button disabled onClick={onClick}>
        Save
      </Button>
    );

    const button = buttonEl(container);

    expect(button.disabled).toBe(true);
    expect(button.className).toContain("disabled:opacity-50");
    expect(button.className).not.toContain("pointer-events-none");

    await user.click(button);

    expect(onClick).not.toHaveBeenCalled();

    await unmount();
  });

  it("loading implies disabled, marks aria-busy, and shows the spinner", async () => {
    const onClick = vi.fn();
    const user = userEvent.setup();

    const { container, unmount } = await renderUI(
      <Button loading onClick={onClick}>
        Save
      </Button>
    );

    const button = buttonEl(container);

    expect(button.disabled).toBe(true);
    expect(button.getAttribute("aria-busy")).toBe("true");
    expect(button.querySelector(".animate-spin")).toBeTruthy();

    await user.click(button);

    expect(onClick).not.toHaveBeenCalled();

    await unmount();
  });

  it("adds no second focus ring (global :focus-visible contract suffices)", async () => {
    const { container, unmount } = await renderUI(<Button>Save</Button>);

    const className = buttonEl(container).className;

    expect(className).not.toContain("ring");
    expect(className).not.toContain("focus-visible");
    expect(className).not.toContain("shadow");
    expect(className).not.toContain("outline");

    await unmount();
  });

  it("merges caller classes over the chain (icon-only send button case)", async () => {
    const { container, unmount } = await renderUI(
      <Button aria-label="Send" className="size-9 rounded-full p-0">
        +
      </Button>
    );

    const className = buttonEl(container).className;

    expect(className).toContain("rounded-full");
    expect(className).not.toContain("rounded-md");
    expect(className).not.toContain("px-3.5");
    expect(className).toContain("size-9");
    // The primary fill survives the override.
    expect(className).toContain("bg-accent");

    await unmount();
  });
});
