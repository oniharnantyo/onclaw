import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import { BINDING_COPY_TIMEOUT_MS, copyText } from "../lib/copyText";

function stubClipboard(writeText: (text: string) => Promise<void>): void {
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText },
    configurable: true,
  });
}

const refused = (): Promise<void> => Promise.reject(new Error("denied"));

describe("copyText (design D12)", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("resolves true on the primary write without touching the fallback", async () => {
    stubClipboard((): Promise<void> => Promise.resolve());

    const setText = vi.fn<(text: string) => Promise<void>>();

    await expect(copyText("hello", { setText })).resolves.toBe(true);
    expect(setText).not.toHaveBeenCalled();
  });

  it("falls back to the binding when the webview refuses", async () => {
    stubClipboard(refused);

    const setText = vi.fn<(text: string) => Promise<void>>((): Promise<void> =>
      Promise.resolve()
    );

    await expect(copyText("hello", { setText })).resolves.toBe(true);
    expect(setText).toHaveBeenCalledWith("hello");
  });

  it("resolves false when both the webview and the binding refuse", async () => {
    stubClipboard(refused);

    const setText = (): Promise<void> =>
      Promise.reject(new Error("clipboard: unavailable in server mode"));

    await expect(copyText("hello", { setText })).resolves.toBe(false);
  });

  // The task 10.3 smoke found the server build's binding call never settling
  // (no run loop for the platform clipboard): the button stayed inert with
  // neither a check state nor a toast. The timeout turns that into a refusal.
  it("resolves false when the binding never settles", async () => {
    vi.useFakeTimers();
    stubClipboard(refused);

    const setText = (): Promise<void> => new Promise<void>((): void => {});
    const pending = copyText("hello", { setText });

    await vi.advanceTimersByTimeAsync(BINDING_COPY_TIMEOUT_MS + 1);
    await expect(pending).resolves.toBe(false);
  });
});
