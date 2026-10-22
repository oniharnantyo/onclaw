/**
 * SectionLabel tests (task 2.1): the mono uppercase header row renders its
 * children as token muted ink and slots an optional action node at the end.
 */
import { describe, expect, it } from "vite-plus/test";

import { SectionLabel } from "../SectionLabel";
import { renderUI } from "../../__test__/test-utils";

describe("SectionLabel", () => {
  it("renders the label row with the mono uppercase muted chain", async () => {
    const { getByText, container, unmount } = await renderUI(
      <SectionLabel>Channels</SectionLabel>
    );

    const row = container.querySelector("div");

    expect(row?.className).toContain("justify-between");
    expect(getByText("Channels").className).toContain("uppercase");
    expect(getByText("Channels").className).toContain("text-muted");

    await unmount();
  });

  it("renders the optional action inside the same row", async () => {
    const { getByTestId, unmount } = await renderUI(
      <SectionLabel
        action={
          <button type="button" data-testid="add-row">
            +
          </button>
        }
      >
        Agents
      </SectionLabel>
    );

    const row = getByTestId("add-row").parentElement;

    expect(row?.className).toContain("justify-between");
    expect(getByTestId("add-row")).toBeTruthy();

    await unmount();
  });
});
