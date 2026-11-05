/**
 * Drag-and-drop attachment suite (add-chat-drag-drop-attachments, designs
 * D1/D3/D5): the chat column wrapped in ComposerPrimitive.AttachmentDropzone
 * maps a dropped file onto the same adapter entry the paperclip uses — one
 * chip per file, each judged independently — while a non-Files drag and a
 * drop outside the column stay inert. The affordance is the dropzone's own
 * data-dragging latch; the hint pill and the composer ring read it through
 * the group class (no new state anywhere).
 *
 * jsdom has no DataTransfer/DragEvent plumbing: every drag event is
 * dispatched through @testing-library/dom with a `{ types, files }` stub —
 * the two fields the primitive reads — and relatedTarget is defined onto the
 * event for the leave-containment check. Dropping text into the textarea
 * inserts it via platform-native browser behavior jsdom cannot exercise; the
 * non-Files case here covers the spec's inertness guard.
 */
// The client mock factory is imported FIRST on purpose: the hoisted
// `vi.mock` below runs the moment any chat module pulls the client in, so
// the factory's module must already be evaluated by then.
import {
  makeClient,
  type MockFn,
  type MockedClient,
} from "../composer/__test__/clientMock";

import { act } from "react";
import { createEvent, fireEvent, screen, waitFor } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import { seedStore } from "../composer/__test__/harness";
import { renderUI } from "./test-utils";
import {
  MAX_FILE_BYTES,
  useAttachmentStore,
  type AttachmentChip,
} from "../attachments";
import ChatView from "../index";

type ViewClient = MockedClient & {
  chat: MockedClient["chat"] & {
    turnState: MockFn;
    deleteSession: MockFn;
    regenerate: MockFn;
    unsubscribe: MockFn;
  };
  events: {
    onSchedulesChanged: MockFn;
    onTurnsChanged: MockFn;
  };
};

vi.mock("../../../lib/client", () => {
  const base = makeClient();

  // The composer suites' client plus the members the full ChatView touches
  // on mount (the schedules/turns subscriptions and the turn-state poll).
  const c = {
    ...base,
    chat: {
      ...base.chat,
      turnState: vi.fn(async () => ({ busy: false, turn_id: "" })),
      deleteSession: vi.fn(async (): Promise<void> => undefined),
      regenerate: vi.fn(async (): Promise<string> => "turn-dnd"),
      unsubscribe: vi.fn(async (): Promise<void> => undefined),
    },
    events: {
      onSchedulesChanged: vi.fn(() => (): void => undefined),
      onTurnsChanged: vi.fn(() => (): void => undefined),
    },
  };

  return { client: c };
});

import { client } from "../../../lib/client";

// SAFETY: the client module is mocked with makeViewClient(), so its runtime
// shape is exactly that mock object.
const mockedClient = client as ViewClient;

// The Wails runtime's own documentElement drop listeners (they ride the
// transport import) call document.elementFromPoint on every file drag, which
// jsdom lacks — a null stub keeps their hover bookkeeping inert while the
// dispatched drag events bubble past the React tree (their target lookup
// returns null and they dropEffect "none", touching nothing we assert).
if (!document.elementFromPoint) {
  document.elementFromPoint = (): Element | null => null;
}

beforeEach(() => {
  vi.clearAllMocks();
  useAttachmentStore.getState().clear();
  seedStore();
});

type DragType = "dragEnter" | "dragOver" | "dragLeave" | "drop";

interface DragInit {
  types: string[];
  files?: File[];
  relatedTarget?: Node;
}

/** Dispatches a synthetic drag event on the node: the DataTransfer stub rides
 * the event the way @testing-library defines it for jsdom (no DragEvent
 * constructor exists there), and relatedTarget is defined onto it after the
 * fact for the leave-containment check. */
function drag(node: Element, type: DragType, init: DragInit): void {
  // jsdom has no DataTransfer constructor, so the stub carries exactly the
  // members the composer's drop lane reads — `types` and `files` — and rides
  // the event init below as an opaque payload; nothing re-reads it as a
  // DataTransfer, so it needs no DataTransfer typing at all.
  const dataTransfer = {
    types: init.types,
    files: init.files ?? [],
  };

  // SAFETY: createEvent's typed index is a union of per-event creators; the
  // relabel picks the one generic creator signature the dispatch below needs
  // for the four drag types the suite names.
  const creator = createEvent[type] as (
    node: Node,
    init: {
      bubbles: boolean;
      cancelable: boolean;
      dataTransfer: { types: string[]; files: File[] };
    }
  ) => Event;

  const event = creator(node, {
    bubbles: true,
    cancelable: true,
    dataTransfer,
  });

  if (init.relatedTarget !== undefined) {
    Object.defineProperty(event, "relatedTarget", {
      value: init.relatedTarget,
    });
  }

  fireEvent(node, event);
}

/** A File whose byte payload matches the size, so the lane caps read an
 * honest File.size. */
function makeFile(name: string, mime: string, size: number): File {
  return new File([new Uint8Array(size)], name, { type: mime });
}

describe("drag-and-drop attachments (add-chat-drag-drop-attachments D5)", () => {
  it("a file dropped on the column mints one chip and uploads", async () => {
    await renderChat();

    await act(async () => {
      drag(dropzone(), "drop", {
        types: ["Files"],
        files: [makeFile("shot.png", "image/png", 10)],
      });
    });

    await waitFor(() =>
      expect(useAttachmentStore.getState().chips).toHaveLength(1)
    );
    await waitFor(() =>
      expect(useAttachmentStore.getState().chips[0]?.status).toBe("ready")
    );
    expect(screen.getByTestId("attachment-tray").textContent).toContain(
      "shot.png"
    );
    expect(mockedClient.chat.saveAttachment).toHaveBeenCalledOnce();
  });

  it("five files dropped in one gesture mint five chips, one each", async () => {
    await renderChat();

    const files = [0, 1, 2, 3, 4].map((i: number): File =>
      makeFile(`f${i}.png`, "image/png", 10)
    );

    await act(async () => {
      drag(dropzone(), "drop", { types: ["Files"], files });
    });

    await waitFor(() =>
      expect(useAttachmentStore.getState().chips).toHaveLength(5)
    );
    // Each drop mints its own chip; the per-message lane cap applies
    // unchanged (design D2), so any chip beyond it may ride rejected — this
    // case only pins that the drop produced a chip per file.
    expect(
      screen
        .getByTestId("attachment-tray")
        .querySelectorAll("[data-testid^='attachment-chip-']")
    ).toHaveLength(5);
  });

  it("an over-cap file in a multi-drop rejects alone while the siblings attach", async () => {
    await renderChat();

    const files = [
      makeFile("a.txt", "text/plain", 10),
      makeFile("big.bin", "application/octet-stream", MAX_FILE_BYTES + 1),
      makeFile("b.txt", "text/plain", 12),
    ];

    await act(async () => {
      drag(dropzone(), "drop", { types: ["Files"], files });
    });

    await waitFor(() => {
      const byName = new Map(
        useAttachmentStore
          .getState()
          .chips.map((c: AttachmentChip): [string, string] => [
            c.name,
            c.status,
          ])
      );

      expect(byName.get("big.bin")).toBe("rejected");
      expect(byName.get("a.txt")).toBe("ready");
      expect(byName.get("b.txt")).toBe("ready");
    });

    // The rejected chip carries the size-limit reason; the siblings' uploads
    // went out on their own calls.
    const tray = screen.getByTestId("attachment-tray");

    expect(tray.querySelectorAll('[data-status="rejected"]')).toHaveLength(1);
    expect(tray.textContent).toContain("big.bin");
    expect(tray.textContent).toContain("Exceeds the 20 MB file limit");
    expect(mockedClient.chat.saveAttachment).toHaveBeenCalledTimes(2);
  });

  it("a non-Files drag (selected text) latches nothing and attaches nothing", async () => {
    await renderChat();

    // Text drops insert into the textarea via platform-native browser
    // behavior jsdom cannot exercise; the guard is that a no-Files drag
    // never latches the affordance nor mints a chip.
    await act(async () => {
      drag(dropzone(), "dragEnter", { types: ["text/plain"] });
      drag(dropzone(), "dragOver", { types: ["text/plain"] });
      drag(dropzone(), "drop", { types: ["text/plain"] });
    });

    expect(dropzone().getAttribute("data-dragging")).toBeNull();
    expect(useAttachmentStore.getState().chips).toHaveLength(0);
    expect(screen.queryByTestId("attachment-tray")).toBeNull();
  });

  it("a drop outside the column attaches nothing and does not throw", async () => {
    await renderChat();

    await act(async () => {
      drag(document.body, "drop", {
        types: ["Files"],
        files: [makeFile("shot.png", "image/png", 10)],
      });
    });

    expect(useAttachmentStore.getState().chips).toHaveLength(0);
    // The view stays mounted: no navigation, no crash (the window guard is
    // unit-covered by its own suite).
    expect(screen.getByTestId("chat-composer")).toBeTruthy();
  });

  it("the affordance latches on file-drag enter and clears on leave and drop", async () => {
    await renderChat();

    const hint = screen.getByTestId("drop-hint");

    // Wiring: the pill hides by default and shows only via the group's latch.
    // SAFETY: the overlay hosts exactly one child — the pill span.
    const pill = hint.querySelector("span") as HTMLElement;

    expect(pill.className).toContain("opacity-0");
    expect(pill.className).toContain("group-data-[dragging=true]:opacity-100");
    expect(dropzone().getAttribute("data-dragging")).toBeNull();

    await act(async () => {
      drag(dropzone(), "dragEnter", { types: ["Files"] });
    });
    expect(dropzone().getAttribute("data-dragging")).toBe("true");

    // Leaving to a node outside the column clears the latch.
    await act(async () => {
      drag(dropzone(), "dragLeave", {
        types: ["Files"],
        relatedTarget: document.body,
      });
    });
    expect(dropzone().getAttribute("data-dragging")).toBeNull();

    // A completed drop clears it too.
    await act(async () => {
      drag(dropzone(), "dragEnter", { types: ["Files"] });
    });
    expect(dropzone().getAttribute("data-dragging")).toBe("true");

    await act(async () => {
      drag(dropzone(), "drop", {
        types: ["Files"],
        files: [makeFile("shot.png", "image/png", 10)],
      });
    });
    expect(dropzone().getAttribute("data-dragging")).toBeNull();
  });
});

async function renderChat(): Promise<void> {
  await renderUI(<ChatView />);
  await waitFor(() => expect(screen.getByTestId("chat-composer")).toBeTruthy());
}

/** The chat column element the dropzone is slotted onto. */
function dropzone(): HTMLElement {
  // SAFETY: the dropzone carries this seam by contract (the column div the
  // primitive wraps).
  return screen.getByTestId("chat-dropzone");
}
