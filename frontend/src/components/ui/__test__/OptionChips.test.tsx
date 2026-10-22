/**
 * OptionChips tests (tasks.md 4.2 port): toggle membership through onChange,
 * aria-pressed reflects the selection, disabled chips render locked and
 * never toggle, and the chip chains stay token-based (no literal colors).
 */
import { act } from "react";
import { fireEvent } from "@testing-library/dom";
import { describe, expect, it, vi } from "vite-plus/test";

import { OptionChips, type OptionChipOption } from "../OptionChips";
import { renderUI } from "../../__test__/test-utils";

const OPTIONS = [
  { id: "browser", label: "Browser", detail: "Opens pages" },
  { id: "shell", label: "Shell" },
  { id: "secret", label: "Secret" },
];

describe("OptionChips", () => {
  it("toggles selection through aria-pressed chips and reports the new array", async () => {
    const onChange = vi.fn();

    const { getByRole, unmount } = await renderUI(
      <OptionChips
        options={OPTIONS}
        value={["browser"]}
        onChange={onChange}
        iconOf={() => "wrench"}
      />
    );

    const browser = getByRole("button", { name: "Browser" });
    expect(browser.getAttribute("aria-pressed")).toBe("true");

    await act(async () => {
      fireEvent.click(browser);
    });
    expect(onChange).toHaveBeenCalledWith([]);

    await act(async () => {
      fireEvent.click(getByRole("button", { name: "Shell" }));
    });
    expect(onChange).toHaveBeenCalledWith(["browser", "shell"]);

    await unmount();
  });

  it("renders a disabled option locked and keeps it from toggling", async () => {
    const onChange = vi.fn();

    const { getByRole, unmount } = await renderUI(
      <OptionChips
        options={OPTIONS}
        value={[]}
        onChange={onChange}
        disabledOf={(o: OptionChipOption) => o.id === "secret"}
      />
    );

    // SAFETY: every option renders a chip button.
    const secret = getByRole("button", { name: "Secret" }) as HTMLButtonElement;

    expect(secret.disabled).toBe(true);
    expect(secret.getAttribute("aria-pressed")).toBe("false");
    expect(secret.title).toBe("Disabled in Settings → Tools");

    await act(async () => {
      fireEvent.click(secret);
    });
    expect(onChange).not.toHaveBeenCalled();

    await unmount();
  });

  it("keeps the chains on token utilities, never literal colors", async () => {
    const { getByRole, unmount } = await renderUI(
      <OptionChips options={OPTIONS} value={["browser"]} onChange={() => {}} />
    );

    const on = getByRole("button", { name: "Browser" });
    const off = getByRole("button", { name: "Shell" });

    expect(on.className).toContain("border-accent");
    expect(off.className).toContain("border-line");
    expect(on.className).not.toMatch(/#[0-9a-fA-F]{3,8}|rgb\(/i);

    await unmount();
  });
});
