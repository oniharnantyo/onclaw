/**
 * Segmented tests (task 2.1): radiogroup semantics (role=radio +
 * aria-checked), onChange reports the clicked option id, testid passthrough,
 * and the token-based active/inactive chains (no literal colors).
 */
import { act } from "react";
import { fireEvent } from "@testing-library/dom";
import { describe, expect, it, vi } from "vite-plus/test";

import { Segmented } from "../Segmented";
import { renderUI } from "../../__test__/test-utils";

const OPTIONS = [
  { id: "sessions", label: "Sessions", testid: "seg-sessions" },
  { id: "agents", label: "Agents", testid: "seg-agents" },
];

describe("Segmented", () => {
  it("renders a radiogroup with aria-checked state per option", async () => {
    const { getByRole, unmount } = await renderUI(
      <Segmented value="sessions" options={OPTIONS} onChange={() => {}} />
    );

    expect(getByRole("radiogroup")).toBeTruthy();
    expect(
      getByRole("radio", { name: "Sessions" }).getAttribute("aria-checked")
    ).toBe("true");
    expect(
      getByRole("radio", { name: "Agents" }).getAttribute("aria-checked")
    ).toBe("false");

    await unmount();
  });

  it("reports the clicked option id through onChange", async () => {
    const onChange = vi.fn();

    const { getByTestId, unmount } = await renderUI(
      <Segmented value="sessions" options={OPTIONS} onChange={onChange} />
    );

    await act(async () => {
      fireEvent.click(getByTestId("seg-agents"));
    });

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith("agents");

    await unmount();
  });

  it("styles the active option with the raised surface chain", async () => {
    const { getByTestId, unmount } = await renderUI(
      <Segmented value="agents" options={OPTIONS} onChange={() => {}} />
    );

    const active = getByTestId("seg-agents");
    const inactive = getByTestId("seg-sessions");

    // Colors are token utilities (both themes re-skin), never literal colors.
    expect(active.className).toContain("bg-surface");
    expect(active.className).toContain("text-fg");
    expect(inactive.className).toContain("text-muted");
    expect(active.className).not.toMatch(/#[0-9a-fA-F]{3,8}|rgb\(/i);

    await unmount();
  });
});
