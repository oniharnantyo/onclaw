/**
 * Alert tests (task 3.3): the four tones ride onclaw-v2's tint idiom
 * (hairline tone border + tone-tinted bg on token color-mixes), the leading
 * icon is tone-colored, danger text uses the lifted --danger-text tier, and
 * no literal colors leak into the class lists.
 */
import { describe, expect, it } from "vite-plus/test";

import { Alert } from "../Alert";
import { renderUI } from "../../__test__/test-utils";

describe("Alert", () => {
  it("renders the info tone with the accent tint and bell icon", async () => {
    const { container, getByText, unmount } = await renderUI(
      <Alert testId="info-alert">Provider connected.</Alert>
    );

    const alert = container.querySelector('[data-testid="info-alert"]')!;
    const className = alert.className;
    const icon = alert.querySelector("svg")!;

    expect(className).toContain(
      "border-[color-mix(in_oklab,var(--accent)_35%,transparent)]"
    );
    expect(className).toContain(
      "bg-[color-mix(in_oklab,var(--accent)_8%,transparent)]"
    );
    expect(icon.getAttribute("class")).toContain("text-accent");
    expect(getByText("Provider connected.")).toBeTruthy();

    await unmount();
  });

  it("renders success, warn, and danger tints per tone", async () => {
    const success = await renderUI(<Alert tone="success">Saved.</Alert>);
    const warn = await renderUI(<Alert tone="warn">Rate limited.</Alert>);

    const danger = await renderUI(
      <Alert tone="danger">Connection lost.</Alert>
    );

    const successEl = success.container.firstElementChild!;
    const warnEl = warn.container.firstElementChild!;
    const dangerEl = danger.container.firstElementChild!;

    expect(successEl.className).toContain(
      "border-[color-mix(in_oklab,var(--success)_35%,transparent)]"
    );
    expect(warnEl.className).toContain(
      "border-[color-mix(in_oklab,var(--warn)_35%,transparent)]"
    );
    expect(dangerEl.className).toContain(
      "border-[color-mix(in_oklab,var(--danger)_35%,transparent)]"
    );
    // Danger body text rides the lifted dark tier (contrast, D9).
    expect(dangerEl.className).toContain("text-dangertext");

    await success.unmount();
    await warn.unmount();
    await danger.unmount();
  });

  it("never leaks literal colors into the class list", async () => {
    for (const tone of ["info", "success", "warn", "danger"] as const) {
      const { container, unmount } = await renderUI(
        <Alert tone={tone}>Body</Alert>
      );

      expect(container.firstElementChild!.className).not.toMatch(
        /#[0-9a-fA-F]{3,8}|rgb\(/i
      );

      await unmount();
    }
  });

  it("merges caller classes over the chain (margin case)", async () => {
    const { container, unmount } = await renderUI(
      <Alert tone="danger" className="mb-4">
        Body
      </Alert>
    );

    const className = container.firstElementChild!.className;

    expect(className).toContain("mb-4");
    expect(className).toContain("rounded-md");

    await unmount();
  });
});
