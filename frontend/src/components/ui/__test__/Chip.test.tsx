/**
 * Chip tests (task 2.1): the bordered pill chain is token-based, `mono` adds
 * the mono face, and caller classes merge over the base chain.
 */
import { describe, expect, it } from "vite-plus/test";

import { Chip } from "../Chip";
import { renderUI } from "../../__test__/test-utils";

describe("Chip", () => {
  it("renders children with the token-based pill chain", async () => {
    const { container, getByText, unmount } = await renderUI(
      <Chip>openai</Chip>
    );

    const chip = container.querySelector("span");

    expect(chip?.className).toContain("border-line");
    expect(chip?.className).toContain("rounded-[6px]");
    expect(chip?.className).toContain("text-fg2");
    expect(chip?.className).not.toContain("font-mono");
    expect(getByText("openai")).not.toBeNull();
    // Colors are token utilities (both themes re-skin), never literal colors.
    expect(chip?.className).not.toMatch(/#[0-9a-fA-F]{3,8}|rgb\(/i);

    await unmount();
  });

  it("adds the mono face for mono chips and merges caller classes", async () => {
    const { container, unmount } = await renderUI(
      <Chip mono className="uppercase">
        v2
      </Chip>
    );

    const chip = container.querySelector("span");

    expect(chip?.className).toContain("font-mono");
    expect(chip?.className).toContain("uppercase");
    // The base chain survives the override.
    expect(chip?.className).toContain("text-fg2");

    await unmount();
  });
});
