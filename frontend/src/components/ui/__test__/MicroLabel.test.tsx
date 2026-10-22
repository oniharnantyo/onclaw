/**
 * MicroLabel tests (task 2.1): renders its children as a <p> with the mono
 * uppercase eyebrow chain in token muted ink.
 */
import { describe, expect, it } from "vite-plus/test";

import { MicroLabel } from "../MicroLabel";
import { renderUI } from "../../__test__/test-utils";

describe("MicroLabel", () => {
  it("renders children with the mono uppercase muted chain", async () => {
    const { container, getByText, unmount } = await renderUI(
      <MicroLabel>Identity &amp; Persona</MicroLabel>
    );

    const p = container.querySelector("p");

    expect(p).not.toBeNull();
    expect(p?.className).toContain("font-mono");
    expect(p?.className).toContain("uppercase");
    expect(p?.className).toContain("tracking-[0.14em]");
    expect(p?.className).toContain("text-muted");
    expect(getByText("Identity & Persona")).not.toBeNull();
    expect(p?.className).not.toMatch(/#[0-9a-fA-F]{3,8}|rgb\(/i);

    await unmount();
  });
});
