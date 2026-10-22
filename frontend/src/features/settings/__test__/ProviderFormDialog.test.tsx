import { describe, expect, it, vi } from "vite-plus/test";

import ProviderFormDialog from "../components/ProviderFormDialog";
import { renderUI } from "./test-utils";

describe("ProviderFormDialog", () => {
  // Native-dialog discipline (task 2.2): the dialog's primary input takes
  // focus on open so typing can start immediately.
  it("focuses the name input when the dialog opens", async () => {
    const { getByTestId, unmount } = await renderUI(
      <ProviderFormDialog
        mode="add"
        provider={null}
        saving={false}
        onSave={vi.fn()}
        onCancel={vi.fn()}
      />
    );

    // SAFETY: this test id renders an input element.
    const name = getByTestId("provider-name") as HTMLInputElement;
    expect(document.activeElement).toBe(name);

    await unmount();
  });
});
