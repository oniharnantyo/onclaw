/**
 * useTablist unit tests (fix-ui-audit-findings 3.2, design D7b): roving
 * tabindex follows the active tab (active 0, others −1), aria-selected and
 * the tab/panel id pairing ride the same ids, arrow keys move the selection
 * and stop at the strip's ends (the documented behavior — no wrap-around;
 * Home/End cover the one-press jump to first/last), focus moves with the
 * selection, and an active id outside the strip (the pinned-identity state)
 * enters the walk at the near end.
 */
import { useState } from "react";
import userEvent from "@testing-library/user-event";
import { screen } from "@testing-library/dom";
import { describe, expect, it } from "vite-plus/test";

import { useTablist } from "../useTablist";
import { renderUI } from "../../components/__test__/test-utils";

const IDS = ["a", "b", "c"] as const;

/** Three-tab harness mirroring the real strips' wiring. */
function Harness(): React.ReactElement {
  const [active, setActive] = useState<(typeof IDS)[number]>("a");

  const { tab, panel } = useTablist({
    idPrefix: "test",
    ids: IDS,
    active,
    onChange: setActive,
  });

  return (
    <div>
      <div role="tablist" aria-label="Test">
        {IDS.map((id) => (
          <button key={id} type="button" {...tab(id)} data-testid={`tab-${id}`}>
            {id}
          </button>
        ))}
      </div>
      <div {...panel(active)} data-testid="panel">
        {active}
      </div>
    </div>
  );
}

describe("useTablist", () => {
  it("marks the active tab selected with a roving tabindex and labels the panel by it", async () => {
    const { unmount } = await renderUI(<Harness />);

    expect(screen.getByTestId("tab-a").getAttribute("aria-selected")).toBe(
      "true"
    );
    expect(screen.getByTestId("tab-a").getAttribute("tabindex")).toBe("0");
    expect(screen.getByTestId("tab-b").getAttribute("aria-selected")).toBe(
      "false"
    );
    expect(screen.getByTestId("tab-b").getAttribute("tabindex")).toBe("-1");
    expect(screen.getByTestId("tab-c").getAttribute("tabindex")).toBe("-1");

    expect(screen.getByTestId("tab-a").id).toBe("test-tab-a");
    const panel = screen.getByTestId("panel");
    expect(panel.getAttribute("role")).toBe("tabpanel");
    expect(panel.getAttribute("aria-labelledby")).toBe("test-tab-a");

    await unmount();
  });

  it("moves selection and focus with the arrow keys and stops at the ends", async () => {
    const user = userEvent.setup();
    const { unmount } = await renderUI(<Harness />);

    screen.getByTestId("tab-a").focus();
    await user.keyboard("{ArrowRight}");
    expect(screen.getByTestId("tab-b").getAttribute("aria-selected")).toBe(
      "true"
    );
    expect(screen.getByTestId("tab-b").getAttribute("tabindex")).toBe("0");
    expect(screen.getByTestId("tab-a").getAttribute("tabindex")).toBe("-1");
    expect(document.activeElement).toBe(screen.getByTestId("tab-b"));

    await user.keyboard("{ArrowRight}");
    expect(screen.getByTestId("tab-c").getAttribute("aria-selected")).toBe(
      "true"
    );

    // Stopped at the end — no wrap-around.
    await user.keyboard("{ArrowRight}");
    expect(screen.getByTestId("tab-c").getAttribute("aria-selected")).toBe(
      "true"
    );
    expect(screen.getByTestId("tab-a").getAttribute("aria-selected")).toBe(
      "false"
    );

    await user.keyboard("{ArrowLeft}");
    expect(screen.getByTestId("tab-b").getAttribute("aria-selected")).toBe(
      "true"
    );

    await user.keyboard("{ArrowLeft}");
    await user.keyboard("{ArrowLeft}");
    expect(screen.getByTestId("tab-a").getAttribute("aria-selected")).toBe(
      "true"
    );

    // Stopped at the start.
    await user.keyboard("{ArrowLeft}");
    expect(screen.getByTestId("tab-a").getAttribute("aria-selected")).toBe(
      "true"
    );

    await user.keyboard("{End}");
    expect(screen.getByTestId("tab-c").getAttribute("aria-selected")).toBe(
      "true"
    );
    expect(document.activeElement).toBe(screen.getByTestId("tab-c"));

    await user.keyboard("{Home}");
    expect(screen.getByTestId("tab-a").getAttribute("aria-selected")).toBe(
      "true"
    );
    expect(document.activeElement).toBe(screen.getByTestId("tab-a"));

    await unmount();
  });

  it("supports the vertical axes the same way for vertical strips", async () => {
    const user = userEvent.setup();
    const { unmount } = await renderUI(<Harness />);

    screen.getByTestId("tab-a").focus();
    await user.keyboard("{ArrowDown}");
    expect(screen.getByTestId("tab-b").getAttribute("aria-selected")).toBe(
      "true"
    );

    await user.keyboard("{ArrowUp}");
    expect(screen.getByTestId("tab-a").getAttribute("aria-selected")).toBe(
      "true"
    );

    await unmount();
  });

  it("enters the walk at the near end when no tab in the strip is active", async () => {
    const user = userEvent.setup();

    function OffStrip(): React.ReactElement {
      const [active, setActive] = useState<(typeof IDS)[number] | null>(null);

      const { tab, panel } = useTablist({
        idPrefix: "off",
        ids: IDS,
        active,
        onChange: (id): void => setActive(id),
      });

      return (
        <div>
          <div role="tablist" aria-label="Off">
            {IDS.map((id) => (
              <button
                key={id}
                type="button"
                {...tab(id)}
                data-testid={`off-tab-${id}`}
              >
                {id}
              </button>
            ))}
          </div>
          {active !== null && (
            <div {...panel(active)} data-testid="off-panel" />
          )}
        </div>
      );
    }

    const { unmount } = await renderUI(<OffStrip />);

    expect(screen.getByTestId("off-tab-a").getAttribute("tabindex")).toBe("-1");
    expect(screen.getByTestId("off-tab-c").getAttribute("tabindex")).toBe("-1");

    screen.getByTestId("off-tab-a").focus();
    await user.keyboard("{ArrowRight}");
    expect(screen.getByTestId("off-tab-a").getAttribute("aria-selected")).toBe(
      "true"
    );

    await unmount();

    const second = await renderUI(<OffStrip />);

    screen.getByTestId("off-tab-c").focus();
    await user.keyboard("{ArrowLeft}");
    expect(screen.getByTestId("off-tab-c").getAttribute("aria-selected")).toBe(
      "true"
    );

    await second.unmount();
  });
});
