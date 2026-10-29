/**
 * Composer attachment suite (task 5.1, spec chat-attachments "Attachment
 * selection and limits" + "Model capability degradation"): a send is blocked
 * while an upload is in flight and carries the capability keys once the
 * upload lands, and the composer warns when the effective model cannot read
 * the attached kind (images, PDFs, video) or when the provider connector
 * cannot carry a kind the model reads (fix-pdf-attachment-carriage D6).
 *
 * The ported scenarios from the legacy `features/chat/__test__/attachments.test.tsx`
 * (the old composer tray) — the store-side lanes and the chip store stay
 * covered by that file's remaining suites.
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

// The client mock factory is imported FIRST on purpose: the hoisted
// `vi.mock` below runs the moment the chat store pulls the client module in,
// so the factory's module must already be evaluated by then.
import { makeClient, type MockedClient } from "./clientMock";
import { useAttachmentStore, type AttachmentChip } from "../../attachments";
import { useChatStore } from "../../store";
import { renderComposer, seedStore } from "./harness";

vi.mock("../../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../../lib/client";

// SAFETY: the client module is mocked with makeClient(), so its runtime shape
// is exactly that mock object.
const mockedClient = client as MockedClient;

/** The capability key the mocked save returns. */
interface StoredAttachment {
  key: string;
}

/** Adds a file through the composer's attach control (the runtime's hidden
 * file input), the way the paperclip does. */
async function attach(file: File): Promise<void> {
  const user = userEvent.setup();

  await user.click(screen.getByRole("button", { name: "Add Attachment" }));

  const input = document.querySelector<HTMLInputElement>('input[type="file"]');

  if (input === null)
    throw new Error("the attach control opened no file input");

  await act(async () => {
    fireEvent.change(input, { target: { files: [file] } });
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  useAttachmentStore.getState().clear();
  seedStore();
});

describe("the composer's attachments (task 5.1)", () => {
  it("blocks send while an upload is in flight, then sends with the keys", async () => {
    // The resolve handle is assigned inside the mock's executor, so the
    // invocation reads the declared type through a closure.
    let release: ((value: StoredAttachment) => void) | null = null;

    const unblock = (value: StoredAttachment): void => {
      release?.(value);
    };

    mockedClient.chat.saveAttachment.mockImplementationOnce(
      (): Promise<StoredAttachment> =>
        new Promise((resolve: (value: StoredAttachment) => void) => {
          release = resolve;
        })
    );

    await renderComposer();

    await attach(new File(["data"], "shot.png", { type: "image/png" }));

    // The tray shows the upload in flight.
    await waitFor(() =>
      expect(screen.getByTestId("attachment-tray").textContent).toContain(
        "Uploading"
      )
    );

    // Send blocked: no text and an in-flight upload.
    // SAFETY: the send control renders a button by contract.
    expect((screen.getByTestId("btn-send") as HTMLButtonElement).disabled).toBe(
      true
    );

    // The read must reach the binding before the gate opens.
    await waitFor(() =>
      expect(mockedClient.chat.saveAttachment).toHaveBeenCalled()
    );

    unblock({ key: "cap-key-1" });

    await waitFor(() =>
      expect(
        useAttachmentStore
          .getState()
          .chips.map((c: AttachmentChip): string => c.status)
      ).toEqual(["ready"])
    );

    // A ready attachment alone makes the send available.
    // SAFETY: the send control renders a button by contract.
    expect((screen.getByTestId("btn-send") as HTMLButtonElement).disabled).toBe(
      false
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-send"));
    });

    await waitFor(() =>
      expect(mockedClient.chat.send).toHaveBeenCalledWith("s1", "", [
        "cap-key-1",
      ])
    );
  });

  it("warns when the effective model cannot read an attached kind", async () => {
    useChatStore.setState({
      models: [
        {
          ID: "m-pro",
          ContextWindow: 200000,
          Efforts: [],
          ImageInput: false,
          PDFInput: false,
          VideoInput: false,
          PDFParts: true,
          VideoParts: true,
        },
      ],
    });

    await renderComposer();

    await attach(new File(["data"], "shot.png", { type: "image/png" }));

    const warning = screen.getByTestId("attachment-capability-warning");

    expect(warning.textContent).toContain("can't read images");
  });

  it("warns when the effective model cannot read video", async () => {
    useChatStore.setState({
      models: [
        {
          ID: "m-pro",
          ContextWindow: 200000,
          Efforts: [],
          ImageInput: true,
          PDFInput: true,
          VideoInput: false,
          PDFParts: true,
          VideoParts: true,
        },
      ],
    });

    await renderComposer();

    await attach(new File(["data"], "clip.mp4", { type: "video/mp4" }));

    const warning = screen.getByTestId("attachment-capability-warning");

    expect(warning.textContent).toContain("can't read videos");
  });

  it("warns when the connector cannot carry a PDF the model reads (design D6)", async () => {
    useChatStore.setState({
      models: [
        {
          ID: "m-pro",
          ContextWindow: 200000,
          Efforts: [],
          ImageInput: true,
          PDFInput: true,
          VideoInput: false,
          PDFParts: false,
          VideoParts: true,
        },
      ],
    });

    await renderComposer();

    await attach(new File(["data"], "doc.pdf", { type: "application/pdf" }));

    const warning = screen.getByTestId("attachment-connector-warning");

    expect(warning.textContent).toContain(
      "PDF will arrive as a reference note (this provider cannot carry PDF parts)"
    );
    // The model reads the PDF, so the model-gap warning stays quiet — the
    // connector gap is its own distinct line (spec: "distinct from the
    // model-capability warning").
    expect(screen.queryByTestId("attachment-capability-warning")).toBeNull();
  });

  it("a connector that carries PDF parts warns nothing for a readable PDF", async () => {
    useChatStore.setState({
      models: [
        {
          ID: "m-pro",
          ContextWindow: 200000,
          Efforts: [],
          ImageInput: true,
          PDFInput: true,
          VideoInput: false,
          PDFParts: true,
          VideoParts: false,
        },
      ],
    });

    await renderComposer();

    await attach(new File(["data"], "doc.pdf", { type: "application/pdf" }));

    await waitFor(() =>
      expect(screen.getByTestId("attachment-tray")).toBeTruthy()
    );
    expect(screen.queryByTestId("attachment-connector-warning")).toBeNull();
    expect(screen.queryByTestId("attachment-capability-warning")).toBeNull();
  });

  it("warns when the connector cannot carry a video the model reads", async () => {
    useChatStore.setState({
      models: [
        {
          ID: "m-pro",
          ContextWindow: 200000,
          Efforts: [],
          ImageInput: true,
          PDFInput: false,
          VideoInput: true,
          PDFParts: true,
          VideoParts: false,
        },
      ],
    });

    await renderComposer();

    await attach(new File(["data"], "clip.mp4", { type: "video/mp4" }));

    const warning = screen.getByTestId("attachment-connector-warning");

    expect(warning.textContent).toContain(
      "Video will arrive as a reference note (this provider cannot carry video parts)"
    );
  });

  it("a capable model warns nothing", async () => {
    await renderComposer();

    await attach(new File(["data"], "shot.png", { type: "image/png" }));

    await waitFor(() =>
      expect(screen.getByTestId("attachment-tray")).toBeTruthy()
    );
    expect(screen.queryByTestId("attachment-capability-warning")).toBeNull();
  });

  it("removes an attachment from the tray", async () => {
    await renderComposer();

    await attach(new File(["data"], "shot.png", { type: "image/png" }));

    await waitFor(() =>
      expect(useAttachmentStore.getState().chips).toHaveLength(1)
    );

    const chip = useAttachmentStore.getState().chips[0];

    await act(async () => {
      fireEvent.click(screen.getByTestId(`chip-remove-${chip.id}`));
    });

    await waitFor(() =>
      expect(useAttachmentStore.getState().chips).toHaveLength(0)
    );
  });
});
