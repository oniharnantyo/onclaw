/**
 * Textarea tests (task 3.3): the onclaw-v2 bordered textarea chain on token
 * utilities, resize behavior, hover/focus/disabled states, the invalid
 * danger border + aria-invalid, ref forwarding, and className merging
 * (the mono payload case).
 */
import { createRef } from "react";
import { describe, expect, it } from "vite-plus/test";

import { Textarea } from "../Textarea";
import { renderUI } from "../../__test__/test-utils";

function textareaEl(container: HTMLElement): HTMLTextAreaElement {
  const el = container.querySelector("textarea");

  // SAFETY: Textarea always renders a native <textarea>.
  return el as HTMLTextAreaElement;
}

describe("Textarea", () => {
  it("renders the onclaw-v2 textarea chain on token utilities", async () => {
    const { container, unmount } = await renderUI(
      <Textarea placeholder="System prompt" />
    );

    const className = textareaEl(container).className;

    expect(className).toContain("resize-y");
    expect(className).toContain("rounded-md");
    expect(className).toContain("border-line");
    expect(className).toContain("text-fg2");
    expect(className).toContain("placeholder:text-muted");
    expect(className).toContain("focus:border-accent");
    expect(className).toContain("focus:outline-none");
    expect(className).not.toMatch(/#[0-9a-fA-F]{3,8}|rgb\(/i);

    await unmount();
  });

  it("adds no second focus ring (global :focus-visible contract suffices)", async () => {
    const { container, unmount } = await renderUI(<Textarea />);

    const className = textareaEl(container).className;

    expect(className).not.toContain("ring");
    expect(className).not.toContain("shadow");
    expect(className).not.toContain("focus-visible");

    await unmount();
  });

  it("invalid adds the danger border and aria-invalid", async () => {
    const valid = await renderUI(<Textarea />);
    const invalid = await renderUI(<Textarea invalid />);

    const ok = textareaEl(valid.container);
    const bad = textareaEl(invalid.container);

    expect(ok.getAttribute("aria-invalid")).toBeNull();
    expect(ok.className).not.toContain("border-danger");

    expect(bad.getAttribute("aria-invalid")).toBe("true");
    expect(bad.className).toContain("border-danger");
    expect(bad.className).toContain("focus:border-danger");

    await valid.unmount();
    await invalid.unmount();
  });

  it("disabled collapses into the native attribute", async () => {
    const { container, unmount } = await renderUI(
      <Textarea disabled defaultValue="locked" />
    );

    const textarea = textareaEl(container);

    expect(textarea.disabled).toBe(true);
    expect(textarea.className).toContain("disabled:opacity-50");
    expect(textarea.className).toContain("disabled:cursor-not-allowed");

    await unmount();
  });

  it("hands the ref to the underlying textarea element", async () => {
    const ref = createRef<HTMLTextAreaElement>();

    const { container, unmount } = await renderUI(<Textarea ref={ref} />);

    expect(ref.current).toBe(textareaEl(container));

    await unmount();
  });

  it("merges caller classes over the chain (mono payload case)", async () => {
    const { container, unmount } = await renderUI(
      <Textarea className="font-mono text-[12.5px] min-h-32" />
    );

    const className = textareaEl(container).className;

    expect(className).toContain("font-mono");
    expect(className).toContain("min-h-32");
    expect(className).not.toContain("min-h-20");
    // The field chain survives the override.
    expect(className).toContain("border-line");

    await unmount();
  });
});
