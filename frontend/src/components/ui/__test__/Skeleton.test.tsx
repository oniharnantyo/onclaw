/**
 * Skeleton tests (task 3.3): the onclaw-v2 pulse idiom on token utilities,
 * caller sizing merges over the default, and the test id flows through.
 */
import { describe, expect, it } from "vite-plus/test";

import { Skeleton } from "../Skeleton";
import { renderUI } from "../../__test__/test-utils";

describe("Skeleton", () => {
  it("renders the pulse block on token utilities", async () => {
    const { container, unmount } = await renderUI(<Skeleton />);

    const el = container.firstElementChild!;

    expect(el.tagName).toBe("DIV");
    expect(el.getAttribute("aria-hidden")).toBe("true");
    expect(el.className).toContain("animate-pulse");
    expect(el.className).toContain("rounded-md");
    expect(el.className).toContain("border-line");
    expect(el.className).toContain(
      "bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]"
    );
    expect(el.className).not.toMatch(/#[0-9a-fA-F]{3,8}|rgb\(/i);

    await unmount();
  });

  it("sizes per call site via className", async () => {
    const { container, unmount } = await renderUI(
      <Skeleton className="h-8 w-32" testId="row-skeleton" />
    );

    const el = container.querySelector('[data-testid="row-skeleton"]')!;

    expect(el.className).toContain("h-8");
    expect(el.className).toContain("w-32");
    expect(el.className).not.toContain("h-16");
    expect(el.className).not.toContain("w-full");

    await unmount();
  });
});
