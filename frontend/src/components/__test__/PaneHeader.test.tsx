import { describe, expect, it } from "vite-plus/test";

import { PaneHeader } from "../PaneHeader";
import { renderUI } from "./test-utils";

describe("PaneHeader", () => {
  it("renders the title alone when no description or actions are given", async () => {
    const { getByTestId, queryByTestId, unmount } = await renderUI(
      <PaneHeader title="Providers" />
    );

    expect(getByTestId("settings-pane-header")).toBeTruthy();
    expect(getByTestId("settings-pane-title").textContent).toBe("Providers");
    expect(queryByTestId("settings-pane-description")).toBeNull();
    await unmount();
  });

  it("renders the description and an action inside the one header row", async () => {
    const { getByTestId, unmount } = await renderUI(
      <PaneHeader
        title="Providers"
        description="Add model providers and manage their API credentials."
        actions={
          <button type="button" data-testid="pane-action">
            Add provider
          </button>
        }
      />
    );

    const header = getByTestId("settings-pane-header");
    expect(getByTestId("settings-pane-title").textContent).toBe("Providers");
    expect(getByTestId("settings-pane-description").textContent).toContain(
      "Add model providers"
    );
    // The action lives in the same header container as the title.
    expect(header.contains(getByTestId("pane-action"))).toBe(true);
    await unmount();
  });
});
