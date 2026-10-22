/**
 * SideRow tests (task 2.1): a type=button row that renders icon/label/sub/
 * right nodes, defaults its title to a string label, tints the active row
 * with the accent color-mix and the inactive row with the hover chain, and
 * reports clicks plus the odId.
 */
import { act } from "react";
import { fireEvent } from "@testing-library/dom";
import { describe, expect, it, vi } from "vite-plus/test";

import { SideRow } from "../SideRow";
import { renderUI } from "../../__test__/test-utils";

function rowEl(container: HTMLElement): HTMLButtonElement {
  const el = container.querySelector("button");

  if (!el) throw new Error("SideRow rendered no button");

  return el;
}

describe("SideRow", () => {
  it("renders a button row with icon, label, sub, and right nodes", async () => {
    const { container, getByTestId, getByText, unmount } = await renderUI(
      <SideRow
        label="Atlas"
        odId="side-agent-1"
        icon={<i data-testid="row-icon" />}
        sub={<span data-testid="row-sub">gpt-5</span>}
        right={<span data-testid="row-right">3</span>}
      />
    );

    const row = rowEl(container);

    expect(row.type).toBe("button");
    expect(row.getAttribute("data-od-id")).toBe("side-agent-1");
    expect(getByText("Atlas")).not.toBeNull();
    expect(getByTestId("row-icon")).not.toBeNull();
    expect(getByTestId("row-sub")).not.toBeNull();
    expect(getByTestId("row-right")).not.toBeNull();
    // String labels become the fallback title, like onclaw's `title || label`.
    expect(row.title).toBe("Atlas");

    await unmount();
  });

  it("tints the active row with the accent mix and inactive with the hover chain", async () => {
    const active = await renderUI(<SideRow label="On" active />);
    const inactive = await renderUI(<SideRow label="Off" />);

    const activeClass = rowEl(active.container).className;
    const inactiveClass = rowEl(inactive.container).className;

    expect(activeClass).toContain("var(--accent)");
    expect(activeClass).toContain("text-fg");
    expect(inactiveClass).toContain("text-fg2");
    expect(inactiveClass).toContain("hover:text-fg");
    // Colors are token utilities (both themes re-skin), never literal colors.
    expect(activeClass).not.toMatch(/#[0-9a-fA-F]{3,8}|rgb\(/i);

    await active.unmount();
    await inactive.unmount();
  });

  it("prefers the explicit title and reports clicks", async () => {
    const onClick = vi.fn();

    const { container, unmount } = await renderUI(
      <SideRow label="Atlas" title="Open agent" onClick={onClick} />
    );

    const row = rowEl(container);

    expect(row.title).toBe("Open agent");

    await act(async () => {
      fireEvent.click(row);
    });

    expect(onClick).toHaveBeenCalledTimes(1);

    await unmount();
  });
});
