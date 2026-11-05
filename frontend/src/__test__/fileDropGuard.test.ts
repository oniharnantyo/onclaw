import { createEvent } from "@testing-library/dom";
import { describe, expect, it } from "vite-plus/test";

import { subscribeFileDropGuard } from "../fileDropGuard";

// jsdom has no real DataTransfer (and its DragEvent may lack one), so tests
// dispatch drag events whose `dataTransfer` is a stub — the guard only reads
// `types` and writes `dropEffect` (design D4/D5). Events are created
// explicitly so the test can assert on the very object the window listener
// saw. Stubs are seeded with dropEffect "none" — the value the Wails JS
// runtime's documentElement listener forces onto every file drag (module
// comment) — so the repair on dragover is provable as an override.
function dispatchDrag(
  type: "dragover" | "drop",
  dataTransfer?: DataTransfer
): DragEvent {
  // SAFETY: createEvent returns the generic Event type and jsdom's DragEvent
  // has no usable constructor; the guard reads only defaultPrevented and
  // dataTransfer off this event, never the DragEvent-specific members.
  const event = createEvent(type, window, { cancelable: true }) as DragEvent;

  if (dataTransfer) {
    Object.defineProperty(event, "dataTransfer", { value: dataTransfer });
  }

  window.dispatchEvent(event);

  return event;
}

function stubDataTransfer(types: string[]): DataTransfer {
  const stub: Partial<DataTransfer> = { types, dropEffect: "none" };

  // SAFETY: jsdom has no DataTransfer constructor (module comment); the stub
  // carries exactly the two members subscribeFileDropGuard touches — `types`
  // (read) and `dropEffect` (written) — and the tests assert on this very
  // object, so no other DataTransfer member is ever exercised.
  return stub as DataTransfer;
}

describe("fileDropGuard", () => {
  it("prevents default on Files dragover and drop", () => {
    const off = subscribeFileDropGuard();

    const over = dispatchDrag("dragover", stubDataTransfer(["Files"]));
    const drop = dispatchDrag("drop", stubDataTransfer(["Files"]));

    expect(over.defaultPrevented).toBe(true);
    expect(drop.defaultPrevented).toBe(true);
    off();
  });

  it("re-claims the drag with dropEffect copy on dragover so the drop fires", () => {
    const off = subscribeFileDropGuard();

    const over = dispatchDrag("dragover", stubDataTransfer(["Files"]));

    expect(over.dataTransfer?.dropEffect).toBe("copy");
    off();
  });

  it("leaves the drop event's dropEffect untouched", () => {
    const off = subscribeFileDropGuard();

    const drop = dispatchDrag("drop", stubDataTransfer(["Files"]));

    expect(drop.dataTransfer?.dropEffect).toBe("none");
    off();
  });

  it("leaves non-Files drags untouched so text drops keep working", () => {
    const off = subscribeFileDropGuard();

    const over = dispatchDrag("dragover", stubDataTransfer(["text/plain"]));
    const drop = dispatchDrag("drop", stubDataTransfer(["text/plain"]));

    expect(over.defaultPrevented).toBe(false);
    expect(drop.defaultPrevented).toBe(false);
    expect(over.dataTransfer?.dropEffect).toBe("none");
    off();
  });

  it("stops guarding after the remover runs", () => {
    const off = subscribeFileDropGuard();
    off();

    const over = dispatchDrag("dragover", stubDataTransfer(["Files"]));
    const drop = dispatchDrag("drop", stubDataTransfer(["Files"]));

    expect(over.defaultPrevented).toBe(false);
    expect(drop.defaultPrevented).toBe(false);
  });

  it("ignores drag events without a dataTransfer", () => {
    const off = subscribeFileDropGuard();

    expect(() => {
      const over = dispatchDrag("dragover");
      const drop = dispatchDrag("drop");
      expect(over.defaultPrevented).toBe(false);
      expect(drop.defaultPrevented).toBe(false);
    }).not.toThrow();
    off();
  });
});
