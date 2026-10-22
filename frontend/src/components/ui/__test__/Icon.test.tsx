/**
 * Icon tests (task 2.1): renders the named glyph as an aria-hidden
 * currentColor SVG at the requested size and stroke width.
 */
import { describe, expect, it } from "vite-plus/test";

import { Icon } from "../Icon";
import { renderUI } from "../../__test__/test-utils";

describe("Icon", () => {
  it("renders the named glyph as a hidden currentColor svg", async () => {
    const { container, unmount } = await renderUI(<Icon name="check" />);

    const svg = container.querySelector("svg");

    expect(svg).not.toBeNull();
    expect(svg?.getAttribute("viewBox")).toBe("0 0 24 24");
    expect(svg?.getAttribute("stroke")).toBe("currentColor");
    expect(svg?.getAttribute("aria-hidden")).toBe("true");
    expect(svg?.querySelector("path")).not.toBeNull();

    await unmount();
  });

  it("passes size, stroke width, and className through", async () => {
    const { container, unmount } = await renderUI(
      <Icon name="sun" size={19} sw={1.7} className="shrink-0" />
    );

    const svg = container.querySelector("svg");

    expect(svg?.getAttribute("width")).toBe("19");
    expect(svg?.getAttribute("height")).toBe("19");
    expect(svg?.getAttribute("stroke-width")).toBe("1.7");
    expect(svg?.className.baseVal ?? svg?.className).toContain("shrink-0");
    // The sun glyph is the circle-marked one the ThemeCycleButton relies on.
    expect(svg?.querySelector("circle")).not.toBeNull();

    await unmount();
  });
});
