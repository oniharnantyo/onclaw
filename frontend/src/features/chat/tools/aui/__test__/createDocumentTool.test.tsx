/**
 * create_document card scenarios (spec chat-tool-cards "Generated document
 * access", add-create-document task 6.1): a successful call shows the
 * reported file name and a human-readable size and offers the generated
 * PDF, fetched through the bound read by the reported name and the active
 * agent; a missing or refused read renders the readable failure state; a
 * rejected call shows the backend's readable error; and the name carries
 * through the result — two results render their own names. The Save As
 * export (add-artifact-save-as design D4) shows only on the desktop build
 * and drives the native save dialog through the client, with the backend's
 * sentinel rejection messages as the UI's match keys. The renderer mounts
 * standalone through the registry map with the shared harness's part props;
 * the Wails client is mocked at the module seam (the sanctioned pattern
 * from toolBodies.test.tsx).
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import { mockedSurface } from "../../../__test__/mockedSurface";
import { renderUI } from "../../../__test__/test-utils";
import type { Metadata } from "../../../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type {
  Client,
  GeneratedDocumentContent,
} from "../../../../../lib/client";
import { dismissToast, getToasts, type Toast } from "../../../../../lib/toasts";
import { resetCollapseMap } from "../../collapse";
import { useChatStore } from "../../../store";
import { partProps } from "./harness";
import { toolRenderers } from "../renderers";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client surface this suite drives: the catalog fetch every card
 * triggers on mount, the app metadata the Save As gate reads, and the
 * generated-document bytes read plus the export call. */
type MockedClient = Client & {
  appInfo: { get: MockFn };
  settings: { tools: MockFn };
  chat: { generatedDocument: MockFn; saveGeneratedDocumentAs: MockFn };
};

/** A minimal PDF-shaped base64 payload (the "%PDF" header). */
const PDF_B64 = "JVBERi0xLjQK";

/** The read's served content for report.pdf. */
const DOC_CONTENT: GeneratedDocumentContent = {
  name: "report.pdf",
  sizeBytes: 2048,
  data: PDF_B64,
};

/** The desktop metadata the Save As gate passes on (default). */
const DESKTOP_METADATA: Metadata = {
  name: "buddies",
  version: "0.1.0",
  nativeDialogs: true,
};

/** The export's default resolution: the user-chosen destination. */
const SAVE_DESTINATION = "/Users/x/Client/report.pdf";

/** The client the hoisted mock factory installs. */
function makeClient(): MockedClient {
  // SAFETY: the literal covers the surface this suite drives; the Client
  // intersection defeats the structural check, so the conversion funnels
  // through the shared mockedSurface bridge (the audited conversion seam).
  return mockedSurface<MockedClient, object>({
    appInfo: {
      get: vi.fn(async (): Promise<Metadata> => ({ ...DESKTOP_METADATA })),
    },
    settings: { tools: vi.fn(async (): Promise<never[]> => []) },
    chat: {
      generatedDocument: vi.fn(async (): Promise<GeneratedDocumentContent> => ({
        ...DOC_CONTENT,
      })),
      saveGeneratedDocumentAs: vi.fn(
        async (): Promise<string> => SAVE_DESTINATION
      ),
    },
  });
}

vi.mock("../../../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../../../lib/client";

// SAFETY: the client module is mocked with makeClient() above, so its
// runtime shape is exactly the mock object this suite drives.
const mockGenerated = (client as MockedClient).chat.generatedDocument;

// SAFETY: as above — the mock object carries the export call this suite drives.
const mockSaveAs = (client as MockedClient).chat.saveGeneratedDocumentAs;

// SAFETY: as above — the mock object carries the metadata probe this suite
// drives.
const mockAppInfo = (client as MockedClient).appInfo.get;

beforeEach(async () => {
  vi.clearAllMocks();
  resetCollapseMap();

  mockGenerated.mockImplementation(
    async (): Promise<GeneratedDocumentContent> => ({ ...DOC_CONTENT })
  );
  mockAppInfo.mockImplementation(async (): Promise<Metadata> => ({
    ...DESKTOP_METADATA,
  }));
  mockSaveAs.mockImplementation(async (): Promise<string> => SAVE_DESTINATION);

  // Leftover toasts would leak into the save-as assertions.
  getToasts().forEach((t: Toast): void => dismissToast(t.id));

  await act(async () => {
    useChatStore.setState({
      activeAgentID: "a1",
      activeSessionID: "s1",
      panelTabs: [],
      entries: [],
    });
  });
});

/** Opens one tool card: the body mounts only while the card is open. */
async function open(toolCallId: string): Promise<void> {
  await act(async () => {
    fireEvent.click(screen.getByTestId(`tool-header-${toolCallId}`));
  });
}

/** The create_document tool result shapes the scenarios drive
 * (docgen_tool.go's output). */
type DocToolResult =
  | { ok: true; filename: string; size: number }
  | { ok: false; error: string };

/** Mounts the card for one create_document result. */
async function renderDoc(
  toolCallId: string,
  result: DocToolResult
): Promise<Awaited<ReturnType<typeof renderUI>>> {
  const Create = toolRenderers.create_document;

  return renderUI(
    <Create
      {...partProps({
        toolCallId,
        toolName: "create_document",
        argsText: JSON.stringify({
          markdown: "# Report",
          filename: result.ok ? result.filename : "report.pdf",
        }),
        result: JSON.stringify(result),
      })}
    />
  );
}

describe("generated document card (spec: Generated document access)", () => {
  it("renders the reported name and size and offers the PDF through the bound read", async () => {
    const { unmount } = await renderDoc("doc-1", {
      ok: true,
      filename: "report.pdf",
      size: 2048,
    });

    await open("doc-1");

    expect(screen.getByTestId("create-doc-name").textContent).toBe(
      "report.pdf"
    );
    expect(screen.getByTestId("create-doc-size").textContent).toBe("2.0 KB");

    // The bound read received the reported name with the active agent —
    // never a path.
    await waitFor((): void => {
      expect(mockGenerated).toHaveBeenCalledWith("a1", "s1", "report.pdf");
    });

    await waitFor((): void => {
      expect(screen.getByTestId("create-doc-pdf")).toBeTruthy();
    });

    expect(screen.getByTestId("create-doc-pdf").getAttribute("data")).toBe(
      `data:application/pdf;base64,${PDF_B64}`
    );
    expect(screen.queryByTestId("create-doc-unavailable")).toBeNull();

    await unmount();
  });

  it("the name comes from the tool result, not a constant", async () => {
    const { unmount } = await renderDoc("doc-2", {
      ok: true,
      filename: "summary-2026.pdf",
      size: 51200,
    });

    await open("doc-2");

    expect(screen.getByTestId("create-doc-name").textContent).toBe(
      "summary-2026.pdf"
    );

    await waitFor((): void => {
      expect(mockGenerated).toHaveBeenCalledWith(
        "a1",
        "s1",
        "summary-2026.pdf"
      );
    });

    expect(screen.getByTestId("create-doc-size").textContent).toBe("50 KB");

    await unmount();
  });

  it("a missing file renders the readable failure state, not the viewer", async () => {
    mockGenerated.mockRejectedValueOnce(
      new Error("generated document: report.pdf not found")
    );

    const { unmount } = await renderDoc("doc-3", {
      ok: true,
      filename: "report.pdf",
      size: 2048,
    });

    await open("doc-3");

    await waitFor((): void => {
      expect(screen.getByTestId("create-doc-unavailable")).toBeTruthy();
    });

    expect(screen.getByTestId("create-doc-unavailable").textContent).toContain(
      "Document unavailable"
    );
    expect(screen.queryByTestId("create-doc-pdf")).toBeNull();

    await unmount();
  });

  it("a name carrying directory components is refused before any call", async () => {
    const { unmount } = await renderDoc("doc-4", {
      ok: true,
      filename: "../secrets/evil.pdf",
      size: 10,
    });

    await open("doc-4");

    // No bound read and no viewer — the failure state only.
    expect(mockGenerated).not.toHaveBeenCalled();
    expect(screen.getByTestId("create-doc-unavailable")).toBeTruthy();
    expect(screen.queryByTestId("create-doc-pdf")).toBeNull();

    await unmount();
  });

  it("a rejected call shows the backend's readable error", async () => {
    const { unmount } = await renderDoc("doc-5", {
      ok: false,
      error: 'preset: no PDF preset named "nope"',
    });

    await open("doc-5");

    expect(screen.getByTestId("create-doc-rejected").textContent).toContain(
      'no PDF preset named "nope"'
    );
    expect(mockGenerated).not.toHaveBeenCalled();

    await unmount();
  });
});

describe("save as (add-artifact-save-as)", () => {
  /** The Save As control, once the card body is open. */
  function saveAsButton(): HTMLElement {
    return screen.getByTestId("create-doc-save-as");
  }

  it("is hidden when the host reports no native dialogs, and never calls the export", async () => {
    mockAppInfo.mockResolvedValueOnce({
      name: "buddies",
      version: "0.1.0",
      nativeDialogs: false,
    });

    const { unmount } = await renderDoc("save-1", {
      ok: true,
      filename: "report.pdf",
      size: 2048,
    });

    await open("save-1");

    // The gate's probe ran and resolved; the control stays hidden.
    await waitFor((): void => {
      expect(mockAppInfo).toHaveBeenCalledTimes(1);
    });
    await act(async () => {});

    expect(screen.queryByTestId("create-doc-save-as")).toBeNull();
    expect(mockSaveAs).not.toHaveBeenCalled();

    await unmount();
  });

  it("exports through the client with the active agent and reported name, and toasts the destination's base name", async () => {
    const { unmount } = await renderDoc("save-2", {
      ok: true,
      filename: "report.pdf",
      size: 2048,
    });

    await open("save-2");

    await waitFor((): void => {
      expect(saveAsButton()).toBeTruthy();
    });

    await act(async () => {
      fireEvent.click(saveAsButton());
    });

    await waitFor((): void => {
      expect(mockSaveAs).toHaveBeenCalledWith("a1", "s1", "report.pdf");
    });
    await waitFor((): void => {
      expect(
        getToasts().some(
          (t: Toast): boolean =>
            t.kind === "info" && t.text.includes("report.pdf")
        )
      ).toBe(true);
    });

    await unmount();
  });

  it("a user cancellation stays silent and re-enables the button", async () => {
    mockSaveAs.mockRejectedValueOnce(new Error("save as: cancelled"));

    const { unmount } = await renderDoc("save-3", {
      ok: true,
      filename: "report.pdf",
      size: 2048,
    });

    await open("save-3");

    await waitFor((): void => {
      expect(saveAsButton()).toBeTruthy();
    });

    await act(async () => {
      fireEvent.click(saveAsButton());
    });

    // The busy cycle ran: disabled while the dialog was open, re-enabled
    // once the cancellation settled.
    await waitFor((): void => {
      // SAFETY: the test id targets the Save As <button> this suite rendered.
      expect((saveAsButton() as HTMLButtonElement).disabled).toBe(false);
    });

    expect(getToasts().length).toBe(0);

    await unmount();
  });

  it("a host without a dialog service toasts the readable danger message", async () => {
    mockSaveAs.mockRejectedValueOnce(
      new Error("save as: no native dialog on this host")
    );

    const { unmount } = await renderDoc("save-4", {
      ok: true,
      filename: "report.pdf",
      size: 2048,
    });

    await open("save-4");

    await waitFor((): void => {
      expect(saveAsButton()).toBeTruthy();
    });

    await act(async () => {
      fireEvent.click(saveAsButton());
    });

    await waitFor((): void => {
      expect(
        getToasts().some(
          (t: Toast): boolean =>
            t.kind === "danger" && t.text === "Save As is not available here"
        )
      ).toBe(true);
    });

    await unmount();
  });

  it("a second click while the dialog is open does not double-invoke the export", async () => {
    let resolve!: (path: string) => void;
    mockSaveAs.mockImplementationOnce(
      (): Promise<string> =>
        new Promise<string>((res: (path: string) => void): void => {
          resolve = res;
        })
    );

    const { unmount } = await renderDoc("save-5", {
      ok: true,
      filename: "report.pdf",
      size: 2048,
    });

    await open("save-5");

    await waitFor((): void => {
      expect(saveAsButton()).toBeTruthy();
    });

    await act(async () => {
      fireEvent.click(saveAsButton());
    });

    // In flight: the button is busy, so the second click is a no-op.
    // SAFETY: the test id targets the Save As <button> this suite rendered.
    expect((saveAsButton() as HTMLButtonElement).disabled).toBe(true);

    await act(async () => {
      fireEvent.click(saveAsButton());
    });

    expect(mockSaveAs).toHaveBeenCalledTimes(1);

    await act(async () => {
      resolve(SAVE_DESTINATION);
    });

    await waitFor((): void => {
      expect(
        getToasts().some(
          (t: Toast): boolean =>
            t.kind === "info" && t.text.includes("report.pdf")
        )
      ).toBe(true);
    });
    await waitFor((): void => {
      // SAFETY: the test id targets the Save As <button> this suite rendered.
      expect((saveAsButton() as HTMLButtonElement).disabled).toBe(false);
    });

    await unmount();
  });
});

describe("the renderer registry", () => {
  it("resolves create_document to its own renderer, not the fallback", () => {
    expect(toolRenderers.create_document).toBeDefined();
    expect(toolRenderers.create_document).not.toBe(toolRenderers.fallback);
  });
});
