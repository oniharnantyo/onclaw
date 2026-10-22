import { describe, expect, it } from "vite-plus/test";

import { BrandMark, ANTHROPIC_CLAY } from "../brandMarks";
import { renderUI } from "../../components/__test__/test-utils";

describe("BrandMark", () => {
  it("renders the anthropic glyph inline in its clay fill", async () => {
    const { container, unmount } = await renderUI(
      <BrandMark type="anthropic" name="anthropic" />
    );

    const svg = container.querySelector("svg");
    expect(svg).toBeTruthy();
    expect(svg?.getAttribute("fill")).toBe(ANTHROPIC_CLAY);
    expect(svg?.getAttribute("viewBox")).toBe("0 0 24 24");
    await unmount();
  });

  it("renders the openai glyph theme-following", async () => {
    const { container, unmount } = await renderUI(
      <BrandMark type="openai" name="openai" />
    );

    expect(container.querySelector("svg")?.getAttribute("fill")).toBe(
      "currentColor"
    );
    await unmount();
  });

  it("falls back to the name's initial on an unknown type", async () => {
    const { container, getByText, unmount } = await renderUI(
      <BrandMark type="acme" name="acme" />
    );

    expect(container.querySelector("svg")).toBeNull();
    expect(getByText("A")).toBeTruthy();
    await unmount();
  });

  it("falls back to the type's initial when the name is empty", async () => {
    const { getByText, unmount } = await renderUI(
      <BrandMark type="vertex" name="" />
    );

    expect(getByText("V")).toBeTruthy();
    await unmount();
  });
});
