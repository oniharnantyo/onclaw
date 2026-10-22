/**
 * Transcript attachments on the assistant-ui thread (task 5.5 spec port, spec
 * chat-attachments "Transcript replay includes attachments"): the four legacy
 * `transcript attachment rendering` scenarios re-mounted through the thread
 * harness. A replayed prompt renders its images as real thumbnails (blob
 * object URLs minted from the capability-gated read) and its documents as
 * name/size chips, an attachment-only prompt renders without a text bubble,
 * a refused key renders unavailable, and the capability key is the only
 * address ever requested — object URLs are revoked on unmount.
 */
import { screen } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import {
  Kind,
  type Event as SessionEvent,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { AttachmentContent } from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { mockedSurface } from "../../__test__/mockedSurface";
import { client, type Client } from "../../../../lib/client";
import { applyEvent, type Entry } from "../../entries";
import { useChatStore } from "../../store";
import { ev, renderThread } from "./harness";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the transcript lanes this suite drives. */
type MockedClient = Client & {
  chat: {
    send: MockFn;
    cancel: MockFn;
    regenerate: MockFn;
    transcriptPage: MockFn;
    resolveApproval: MockFn;
    subscribe: MockFn;
    attachmentContent: MockFn;
  };
};

/** The default capability read: the key's bytes (base64 "data"). */
async function readAttachment(key: string): Promise<AttachmentContent> {
  return { key, name: key, mime: "image/png", size: 4, data: "ZGF0YQ==" };
}

function makeClient(): MockedClient {
  // SAFETY: the literal covers exactly the surface this suite drives; the
  // Client-typed intersection defeats the structural check, so the conversion
  // funnels through the shared mockedSurface bridge.
  return mockedSurface<MockedClient, object>({
    chat: {
      send: vi.fn(async (): Promise<string> => "turn-mock"),
      cancel: vi.fn(async (): Promise<void> => undefined),
      regenerate: vi.fn(async (): Promise<string> => "turn-mock"),
      transcriptPage: vi.fn(async (): Promise<SessionEvent[]> => []),
      resolveApproval: vi.fn(async (): Promise<void> => undefined),
      subscribe: vi.fn(async (): Promise<() => void> => (): void => undefined),
      attachmentContent: vi.fn(readAttachment),
    },
  });
}

vi.mock("../../../../lib/client", () => ({ client: makeClient() }));

// SAFETY: the client module is mocked with makeClient(), so its runtime shape
// is exactly this mock object.
const mockedClient = client as MockedClient;

/** jsdom lacks the blob URL factory; the suite stubs it (the legacy suite's
 * pattern). */
const createObjectURL = vi.fn<(obj: Blob | MediaSource) => string>(
  (): string => "blob:mock-url"
);

const revokeObjectURL = vi.fn<(fileURL: string) => void>((): void => undefined);

beforeEach(() => {
  vi.clearAllMocks();
  mockedClient.chat.attachmentContent.mockReset();
  mockedClient.chat.attachmentContent.mockImplementation(readAttachment);
  window.URL.createObjectURL = createObjectURL;
  window.URL.revokeObjectURL = revokeObjectURL;
  useChatStore.setState({ busy: false, activeSessionID: "s1", entries: [] });
});

describe("transcript attachment rendering", () => {
  it("renders an image thumbnail and a document chip from metadata", async () => {
    const entries: Entry[] = applyEvent(
      [],
      ev(Kind.KindUserMessage, {
        turn_id: "t1",
        id: "e1",
        text: "see attached",
        attachments: [
          { key: "img-1", name: "shot.png", mime: "image/png", size: 4 },
          {
            key: "doc-1",
            name: "spec.pdf",
            mime: "application/pdf",
            size: 2048,
          },
        ],
      })
    );

    const h = await renderThread(entries);

    await h.settle();

    // The image is a real thumbnail over the blob URL the capability read
    // minted — never a raw path.
    expect(
      screen
        .getByTestId("attachment-image")
        .querySelector("img")
        ?.getAttribute("src")
    ).toBe("blob:mock-url");
    expect(screen.getByTestId("attachment-document").textContent).toContain(
      "spec.pdf"
    );
    expect(screen.getByTestId("attachment-meta").textContent).toContain(
      "2.0 KB"
    );
    // The prompt's text keeps its bubble beside the chips.
    expect(screen.getByTestId("user-text").textContent).toContain(
      "see attached"
    );

    // The capability key is the only address ever requested.
    expect(mockedClient.chat.attachmentContent).toHaveBeenCalledTimes(2);
    expect(mockedClient.chat.attachmentContent).toHaveBeenNthCalledWith(
      1,
      "img-1"
    );
    expect(mockedClient.chat.attachmentContent).toHaveBeenNthCalledWith(
      2,
      "doc-1"
    );

    // The blob URL lives exactly as long as the row that owns it.
    expect(revokeObjectURL).not.toHaveBeenCalled();
    await h.unmount();
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:mock-url");
  });

  it("an attachment-only message renders without a text bubble", async () => {
    const entries: Entry[] = applyEvent(
      [],
      ev(Kind.KindUserMessage, {
        turn_id: "t1",
        id: "e1",
        text: "",
        attachments: [
          { key: "doc-1", name: "spec.pdf", mime: "application/pdf", size: 10 },
        ],
      })
    );

    const h = await renderThread(entries);

    await h.settle();

    // The chip renders and no prompt bubble is minted around the empty text.
    expect(screen.getByTestId("attachment-document").textContent).toContain(
      "spec.pdf"
    );
    expect(screen.queryByTestId("user-text")).toBeNull();

    await h.unmount();
  });

  it("reload-from-metadata reconstructs the chips through the reducer", async () => {
    // The persisted event replays through applyEvent exactly as the live
    // stream delivered it — the chips re-render from metadata alone.
    const stored = ev(Kind.KindUserMessage, {
      turn_id: "t1",
      id: "e1",
      text: "see attached",
      attachments: [
        { key: "img-1", name: "shot.png", mime: "image/png", size: 4 },
      ],
    });

    const first: Entry[] = applyEvent([], stored);
    const replay: Entry[] = applyEvent([], stored);

    expect(first[0]).toMatchObject({ kind: "user", text: "see attached" });
    expect(replay[0]).toMatchObject({ kind: "user", text: "see attached" });

    const h = await renderThread(replay);

    await h.settle();

    expect(screen.getByTestId("attachment-image")).toBeTruthy();
    expect(mockedClient.chat.attachmentContent).toHaveBeenCalledWith("img-1");

    await h.unmount();
  });

  it("a refused key renders as unavailable, never a raw path", async () => {
    mockedClient.chat.attachmentContent.mockRejectedValueOnce(
      new Error("unknown key")
    );

    const entries: Entry[] = applyEvent(
      [],
      ev(Kind.KindUserMessage, {
        turn_id: "t1",
        id: "e1",
        text: "",
        attachments: [
          { key: "gone", name: "lost.png", mime: "image/png", size: 4 },
        ],
      })
    );

    const h = await renderThread(entries);

    await h.settle();

    expect(screen.getByTestId("attachment-unavailable")).toBeTruthy();
    expect(screen.getByTestId("entry-user").textContent).toContain("lost.png");
    // Nothing was materialized from the refusal: no blob, no anchor, no path.
    expect(createObjectURL).not.toHaveBeenCalled();
    expect(
      screen.getByTestId("attachment-unavailable").querySelector("a")
    ).toBeNull();
    expect(mockedClient.chat.attachmentContent).toHaveBeenCalledTimes(1);
    expect(mockedClient.chat.attachmentContent).toHaveBeenCalledWith("gone");

    await h.unmount();
  });
});
