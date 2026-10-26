/**
 * create_document card scenarios (spec chat-tool-cards "Generated document
 * access", add-create-document task 6.1): a successful call shows the
 * reported file name and a human-readable size and offers the generated
 * PDF, fetched through the bound read by the reported name and the active
 * agent; a missing or refused read renders the readable failure state; a
 * rejected call shows the backend's readable error; and the name carries
 * through the result — two results render their own names. The renderer
 * mounts standalone through the registry map with the shared harness's
 * part props; the Wails client is mocked at the module seam (the
 * sanctioned pattern from toolBodies.test.tsx).
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import { mockedSurface } from "../../../__test__/mockedSurface";
import { renderUI } from "../../../__test__/test-utils";
import type {
  Client,
  GeneratedDocumentContent,
} from "../../../../../lib/client";
import { resetCollapseMap } from "../../collapse";
import { useChatStore } from "../../../store";
import { partProps } from "./harness";
import { toolRenderers } from "../renderers";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client surface this suite drives: the catalog fetch every card
 * triggers on mount and the generated-document bytes read. */
type MockedClient = Client & {
  settings: { tools: MockFn };
  chat: { generatedDocument: MockFn };
};

/** A minimal PDF-shaped base64 payload (the "%PDF" header). */
const PDF_B64 = "JVBERi0xLjQK";

/** The read's served content for report.pdf. */
const DOC_CONTENT: GeneratedDocumentContent = {
  name: "report.pdf",
  sizeBytes: 2048,
  data: PDF_B64,
};

/** The client the hoisted mock factory installs. */
function makeClient(): MockedClient {
  // SAFETY: the literal covers the surface this suite drives; the Client
  // intersection defeats the structural check, so the conversion funnels
  // through the shared mockedSurface bridge (the audited conversion seam).
  return mockedSurface<MockedClient, object>({
    settings: { tools: vi.fn(async (): Promise<never[]> => []) },
    chat: {
      generatedDocument: vi.fn(async (): Promise<GeneratedDocumentContent> => ({
        ...DOC_CONTENT,
      })),
    },
  });
}

vi.mock("../../../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../../../lib/client";

// SAFETY: the client module is mocked with makeClient() above, so its
// runtime shape is exactly the mock object this suite drives.
const mockGenerated = (client as MockedClient).chat.generatedDocument;

beforeEach(async () => {
  vi.clearAllMocks();
  resetCollapseMap();

  mockGenerated.mockImplementation(
    async (): Promise<GeneratedDocumentContent> => ({ ...DOC_CONTENT })
  );

  await act(async () => {
    useChatStore.setState({
      activeAgentID: "a1",
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
      expect(mockGenerated).toHaveBeenCalledWith("a1", "report.pdf");
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
      expect(mockGenerated).toHaveBeenCalledWith("a1", "summary-2026.pdf");
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

describe("the renderer registry", () => {
  it("resolves create_document to its own renderer, not the fallback", () => {
    expect(toolRenderers.create_document).toBeDefined();
    expect(toolRenderers.create_document).not.toBe(toolRenderers.fallback);
  });
});
