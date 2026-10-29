import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vite-plus/test";
import { fireEvent, waitFor } from "@testing-library/dom";
import userEvent from "@testing-library/user-event";
import type { DocumentPresetEntry } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client } from "../../../lib/client";

import DocumentConfigDialog from "../components/DocumentConfigDialog";
import type { DOCXConfig, PresetConfig } from "../components/presetConfig";
import { renderUI } from "./test-utils";

/** The preset fixture store the mocked client reads and writes; rows carry
 * their format (add-document-editing's per-format tabs). */
let presets: DocumentPresetEntry[] = [];

/** The stored default preset name per format; absent when none. Deleting
 * the default leaves it dangling (the backend's design), so the mock
 * mirrors that. */
interface StoredDefaults {
  pdf?: string;
  docx?: string;
  pptx?: string;
}

let defaults: StoredDefaults = {};

let idSeq = 0;

function entry(
  name: string,
  config: string,
  overrides: Partial<DocumentPresetEntry> = {}
): DocumentPresetEntry {
  idSeq += 1;

  return {
    id: `preset-${idSeq}`,
    name,
    format: "pdf",
    config,
    hasTemplate: false,
    createdAt: "2026-10-01T12:00:00Z",
    updatedAt: "2026-10-01T12:00:00Z",
    ...overrides,
  };
}

/** A minimal skeleton upload for the template-editor tests. */
function docxFile(name: string = "skeleton.docx"): File {
  return new File(["PK"], name, {
    type: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  });
}

vi.mock("../../../lib/client", () => ({
  client: {
    settings: {
      documentPresets: vi.fn(
        async (format: string): Promise<DocumentPresetEntry[]> =>
          presets.filter((p: DocumentPresetEntry) => p.format === format)
      ),
      createDocumentPreset: vi.fn(
        async (
          format: string,
          name: string,
          configJSON: string
        ): Promise<DocumentPresetEntry> => {
          if (
            presets.some(
              (p: DocumentPresetEntry): boolean =>
                p.format === format &&
                p.name.toLowerCase() === name.toLowerCase()
            )
          ) {
            throw new Error(`preset name already taken: ${name}`);
          }

          const created = entry(name, configJSON, { format });
          presets = [...presets, created];

          return created;
        }
      ),
      updateDocumentPreset: vi.fn(
        async (
          id: string,
          name: string,
          configJSON: string
        ): Promise<DocumentPresetEntry> => {
          const source = presets.find(
            (p: DocumentPresetEntry): boolean => p.id === id
          );

          if (!source) throw new Error("preset not found");

          if (
            presets.some(
              (p: DocumentPresetEntry): boolean =>
                p.id !== id && p.name.toLowerCase() === name.toLowerCase()
            )
          ) {
            throw new Error(`preset name already taken: ${name}`);
          }

          source.name = name;
          source.config = configJSON;

          return source;
        }
      ),
      duplicateDocumentPreset: vi.fn(
        async (id: string, newName: string): Promise<DocumentPresetEntry> => {
          const source = presets.find(
            (p: DocumentPresetEntry): boolean => p.id === id
          );

          if (!source) throw new Error("preset not found");

          if (
            presets.some(
              (p: DocumentPresetEntry): boolean =>
                p.name.toLowerCase() === newName.toLowerCase()
            )
          ) {
            throw new Error(`preset name already taken: ${newName}`);
          }

          const copy = entry(newName, source.config, { format: source.format });
          presets = [...presets, copy];

          return copy;
        }
      ),
      deleteDocumentPreset: vi.fn(async (id: string): Promise<void> => {
        presets = presets.filter(
          (p: DocumentPresetEntry): boolean => p.id !== id
        );
      }),
      defaultDocumentPreset: vi.fn(
        async (format: string): Promise<string> =>
          // SAFETY: the mock stores one default per supported format; the
          // key cast narrows the service's free-form format string to the
          // fixture's keys.
          defaults[format as keyof StoredDefaults] ?? ""
      ),
      setDefaultDocumentPreset: vi.fn(
        async (format: string, name: string): Promise<void> => {
          // SAFETY: as above — the fixture keys are the supported formats.
          defaults[format as keyof StoredDefaults] = name;
        }
      ),
      setDocumentPresetTemplate: vi.fn(
        async (
          presetID: string,
          filename: string,
          _base64: string
        ): Promise<void> => {
          // Mirror the service: the upload stamps the row's template
          // metadata (has_template, the file's name).
          const source = presets.find(
            (p: DocumentPresetEntry): boolean => p.id === presetID
          );

          if (!source) throw new Error("preset not found");

          source.hasTemplate = true;
          source.templateName = filename;
        }
      ),
    },
  },
}));

import { client } from "../../../lib/client";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  settings: {
    documentPresets: MockFn;
    createDocumentPreset: MockFn;
    updateDocumentPreset: MockFn;
    duplicateDocumentPreset: MockFn;
    deleteDocumentPreset: MockFn;
    defaultDocumentPreset: MockFn;
    setDefaultDocumentPreset: MockFn;
    setDocumentPresetTemplate: MockFn;
  };
};

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

/** Picks the hidden template-file input (the knowledge suites' pattern for
 * display:none inputs, which user-event's pointer cannot reach); the query
 * is scoped to the rendered container, which renderUI binds its queries to. */
async function pickFile(
  container: HTMLElement,
  testId: string,
  file: File
): Promise<void> {
  const input = container.querySelector<HTMLInputElement>(
    `input[data-testid="${testId}"]`
  );

  if (input === null) throw new Error("the editor opened no file input");

  fireEvent.change(input, { target: { files: [file] } });
}

describe("DocumentConfigDialog", () => {
  beforeEach(() => {
    presets = [];
    defaults = {};
    idSeq = 0;
    vi.clearAllMocks();
  });

  afterEach(() => {
    document.documentElement.style.overflow = "";
    document.body.style.overflow = "";
  });

  it("renders the PDF tab, the preset rows, and the default selector", async () => {
    presets = [
      entry(
        "Client Report",
        JSON.stringify({ schema_version: 1, theme: "academic" })
      ),
      entry("Invoice", JSON.stringify({ schema_version: 1 })),
    ];
    defaults = { pdf: "Client Report" };

    const { getByTestId, findByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");

    const pdfTab = getByTestId("document-tab-pdf");
    expect(pdfTab.getAttribute("aria-selected")).toBe("true");
    // All three format tabs are live (spec add-document-editing): PDF,
    // DOCX, and PPTX.
    expect(getByTestId("document-tab-docx").hasAttribute("disabled")).toBe(
      false
    );
    expect(getByTestId("document-tab-pptx").hasAttribute("disabled")).toBe(
      false
    );

    expect(getByTestId("document-preset-row-Client Report")).toBeTruthy();
    expect(
      getByTestId("document-preset-row-Client Report").textContent
    ).toContain("academic theme");
    expect(getByTestId("document-preset-row-Invoice")).toBeTruthy();

    // SAFETY: the query targets this dialog's PDF default-preset select, an
    // HTMLSelectElement by construction.
    expect(
      (getByTestId("document-default-preset-pdf") as HTMLSelectElement).value
    ).toBe("Client Report");
  });

  it("shows a stored default naming a missing preset as none", async () => {
    presets = [entry("Invoice", JSON.stringify({ schema_version: 1 }))];
    defaults = { pdf: "Ghost" };

    const { getByTestId, findByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");

    // SAFETY: the query targets this dialog's default-preset select, an
    // HTMLSelectElement by construction.
    expect(
      (getByTestId("document-default-preset-pdf") as HTMLSelectElement).value
    ).toBe("");
  });

  it("offers create in the empty state", async () => {
    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");

    expect(getByTestId("document-presets-empty").textContent).toContain(
      "No presets yet"
    );
    expect(queryByTestId("document-default-preset-pdf")).toBeNull();
    expect(getByTestId("document-preset-new-pdf")).toBeTruthy();
  });

  it("creates a preset through the editor with a schema-stamped config", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");
    await user.click(getByTestId("document-preset-new-pdf"));
    await findByTestId("document-preset-name");

    await user.type(getByTestId("document-preset-name"), "Client Report");
    await user.selectOptions(getByTestId("document-theme"), "academic");
    await user.click(getByTestId("document-page-toggle"));
    await user.selectOptions(getByTestId("document-page-size"), "a4");
    await user.click(getByTestId("document-footer-page-number"));
    await user.click(getByTestId("document-preset-save"));

    expect(mockedClient.settings.createDocumentPreset).toHaveBeenCalledTimes(1);
    expect(mockedClient.settings.createDocumentPreset).toHaveBeenCalledWith(
      "pdf",
      "Client Report",
      expect.any(String)
    );

    // SAFETY: the save ran exactly once; a mock call entry is its argument list.
    const call = mockedClient.settings.createDocumentPreset.mock
      .calls[0] as unknown[];

    // SAFETY: the third argument is the config JSON the dialog stringified.
    const config = JSON.parse(call[2] as string) as PresetConfig;

    expect(config).toEqual({
      schema_version: 1,
      theme: "academic",
      page: { size: "a4" },
      footer: { show_page_number: true },
    });

    // The created preset lands in the list.
    expect(
      await findByTestId("document-preset-row-Client Report")
    ).toBeTruthy();
  });

  it("edits a preset and saves atomically with one update call", async () => {
    presets = [
      entry(
        "Client Report",
        JSON.stringify({ schema_version: 1, page: { size: "letter" } })
      ),
    ];
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");
    await user.click(getByTestId("document-preset-edit-Client Report"));
    await findByTestId("document-preset-name");

    // The stored page section reopens with its toggle on and size loaded.
    expect(
      getByTestId("document-page-toggle").getAttribute("aria-checked")
    ).toBe("true");
    // SAFETY: the query targets this dialog's page-size select, an
    // HTMLSelectElement by construction.
    expect((getByTestId("document-page-size") as HTMLSelectElement).value).toBe(
      "letter"
    );

    await user.selectOptions(getByTestId("document-theme"), "legal");
    await user.selectOptions(getByTestId("document-page-size"), "a4");
    await user.click(getByTestId("document-preset-save"));

    expect(mockedClient.settings.updateDocumentPreset).toHaveBeenCalledTimes(1);
    expect(mockedClient.settings.updateDocumentPreset).toHaveBeenCalledWith(
      "preset-1",
      "Client Report",
      expect.any(String)
    );

    // SAFETY: the save ran exactly once; a mock call entry is its argument list.
    const call = mockedClient.settings.updateDocumentPreset.mock
      .calls[0] as unknown[];

    // SAFETY: the third argument is the config JSON the dialog stringified.
    const config = JSON.parse(call[2] as string) as PresetConfig;

    expect(config).toEqual({
      schema_version: 1,
      theme: "legal",
      page: { size: "a4" },
    });
  });

  it("duplicates a preset with the suggested copy name", async () => {
    presets = [entry("Report", JSON.stringify({ schema_version: 1 }))];
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");
    await user.click(getByTestId("document-preset-duplicate-Report"));

    // SAFETY: the query targets the armed duplicate row's name field, an
    // HTMLInputElement by construction.
    expect(
      (getByTestId("document-duplicate-name") as HTMLInputElement).value
    ).toBe("Report copy");

    await user.click(getByTestId("document-duplicate-confirm"));

    expect(mockedClient.settings.duplicateDocumentPreset).toHaveBeenCalledWith(
      "preset-1",
      "Report copy"
    );
    expect(await findByTestId("document-preset-row-Report copy")).toBeTruthy();
  });

  it("surfaces a taken duplicate name as an inline error", async () => {
    presets = [
      entry("Report", JSON.stringify({ schema_version: 1 })),
      entry("Report copy", JSON.stringify({ schema_version: 1 })),
    ];
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");
    await user.click(getByTestId("document-preset-duplicate-Report"));

    // The suggested name "Report copy" is taken; confirming surfaces the
    // rejection inline and keeps the row armed for a rename.
    await user.click(getByTestId("document-duplicate-confirm"));

    expect(getByTestId("document-duplicate-error").textContent).toContain(
      "already exists"
    );
    // The row stays armed — its action buttons are replaced by the form.
    expect(queryByTestId("document-preset-duplicate-Report")).toBeNull();
    expect(getByTestId("document-duplicate-name")).toBeTruthy();
  });

  it("rejects a duplicate name in the editor without calling the backend", async () => {
    presets = [entry("Report", JSON.stringify({ schema_version: 1 }))];
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");
    await user.click(getByTestId("document-preset-new-pdf"));

    // Case-insensitive collision with the stored preset.
    await user.type(getByTestId("document-preset-name"), "report");
    await user.click(getByTestId("document-preset-save"));

    expect(getByTestId("document-editor-error").textContent).toContain(
      "already exists"
    );
    expect(mockedClient.settings.createDocumentPreset).not.toHaveBeenCalled();
  });

  it("deletes a preset after the inline confirm and resets the default", async () => {
    presets = [
      entry("Client Report", JSON.stringify({ schema_version: 1 })),
      entry("Memo", JSON.stringify({ schema_version: 1 })),
    ];
    defaults = { pdf: "Client Report" };
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");

    // First click arms the inline confirm; the second deletes.
    await user.click(getByTestId("document-preset-delete-Client Report"));
    await user.click(getByTestId("document-preset-delete-Client Report"));

    expect(mockedClient.settings.deleteDocumentPreset).toHaveBeenCalledWith(
      "preset-1"
    );
    expect(queryByTestId("document-preset-row-Client Report")).toBeNull();
    expect(getByTestId("document-preset-row-Memo")).toBeTruthy();

    // The deleted preset was the default; the dangling stored default reads
    // as none (the tool falls back to the library defaults).
    // SAFETY: the query targets this dialog's default-preset select, an
    // HTMLSelectElement by construction.
    expect(
      (getByTestId("document-default-preset-pdf") as HTMLSelectElement).value
    ).toBe("");
  });

  it("sets and resets the default preset", async () => {
    presets = [
      entry("Client Report", JSON.stringify({ schema_version: 1 })),
      entry("Memo", JSON.stringify({ schema_version: 1 })),
    ];
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");

    await user.selectOptions(
      getByTestId("document-default-preset-pdf"),
      "Memo"
    );
    expect(mockedClient.settings.setDefaultDocumentPreset).toHaveBeenCalledWith(
      "pdf",
      "Memo"
    );

    await user.selectOptions(getByTestId("document-default-preset-pdf"), "");
    expect(mockedClient.settings.setDefaultDocumentPreset).toHaveBeenCalledWith(
      "pdf",
      ""
    );
  });

  it("switching tabs lists that format's presets and its default selector", async () => {
    presets = [
      entry("Client Report", JSON.stringify({ schema_version: 1 })),
      entry(
        "Brand Memo",
        JSON.stringify({ schema_version: 1, flavor: "config" }),
        { format: "docx" }
      ),
    ];
    defaults = { docx: "Brand Memo" };
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");

    // The PDF tab shows only the PDF presets.
    expect(getByTestId("document-preset-row-Client Report")).toBeTruthy();
    expect(queryByTestId("document-preset-row-Brand Memo")).toBeNull();

    await user.click(getByTestId("document-tab-docx"));

    // The DOCX tab shows only the DOCX presets, with their own default
    // selector.
    expect(await findByTestId("document-preset-row-Brand Memo")).toBeTruthy();
    expect(queryByTestId("document-preset-row-Client Report")).toBeNull();
    // SAFETY: the query targets the DOCX tab's default-preset select, an
    // HTMLSelectElement by construction.
    expect(
      (getByTestId("document-default-preset-docx") as HTMLSelectElement).value
    ).toBe("Brand Memo");
  });

  it("the DOCX tab offers both the config editor and the template upload", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");
    await user.click(getByTestId("document-tab-docx"));
    await findByTestId("document-preset-new-docx");

    // Both creation paths live on the DOCX tab.
    expect(getByTestId("document-template-new-docx")).toBeTruthy();
    expect(getByTestId("document-preset-new-docx")).toBeTruthy();

    // The config-preset editor groups its fields by concern.
    await user.click(getByTestId("document-preset-new-docx"));
    await findByTestId("document-preset-name");
    expect(getByTestId("document-docx-body-family")).toBeTruthy();
    expect(getByTestId("document-docx-body-size")).toBeTruthy();
    expect(getByTestId("document-docx-heading-color")).toBeTruthy();
    expect(getByTestId("document-docx-heading-weight")).toBeTruthy();
    expect(getByTestId("document-docx-page-size")).toBeTruthy();
    expect(getByTestId("document-docx-page-margin")).toBeTruthy();
    expect(getByTestId("document-docx-footer-text")).toBeTruthy();
    expect(getByTestId("document-docx-footer-page-number")).toBeTruthy();

    await user.click(getByTestId("document-preset-cancel"));
    expect(queryByTestId("document-docx-body-family")).toBeNull();
  });

  it("creates a DOCX config preset with the grouped config JSON", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");
    await user.click(getByTestId("document-tab-docx"));
    await user.click(getByTestId("document-preset-new-docx"));
    await findByTestId("document-preset-name");

    await user.type(getByTestId("document-preset-name"), "Brand Memo");
    await user.type(getByTestId("document-docx-body-size"), "11");
    await user.type(getByTestId("document-docx-heading-color"), "#1F3864");
    await user.selectOptions(getByTestId("document-docx-page-size"), "a4");
    await user.click(getByTestId("document-docx-footer-page-number"));
    await user.click(getByTestId("document-preset-save"));

    expect(mockedClient.settings.createDocumentPreset).toHaveBeenCalledTimes(1);
    expect(mockedClient.settings.createDocumentPreset).toHaveBeenCalledWith(
      "docx",
      "Brand Memo",
      expect.any(String)
    );

    // SAFETY: the save ran exactly once; a mock call entry is its argument list.
    const call = mockedClient.settings.createDocumentPreset.mock
      .calls[0] as unknown[];

    // SAFETY: the third argument is the config JSON the dialog stringified.
    const config = JSON.parse(call[2] as string) as DOCXConfig;

    expect(config).toEqual({
      schema_version: 1,
      flavor: "config",
      fonts: { body_size: 11 },
      heading: { color: "#1F3864" },
      page: { size: "a4" },
      footer: { show_page_number: true },
    });

    expect(await findByTestId("document-preset-row-Brand Memo")).toBeTruthy();
  });

  it("creates a DOCX template preset and uploads the skeleton", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId, container } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");
    await user.click(getByTestId("document-tab-docx"));
    await user.click(getByTestId("document-template-new-docx"));
    await findByTestId("document-preset-name");

    await user.type(getByTestId("document-preset-name"), "Brand Skeleton");
    await pickFile(container, "document-template-file", docxFile());
    await user.click(getByTestId("document-preset-save"));

    // The save chain is create → FileReader (a macrotask) → upload →
    // refresh, so the mock's call count cannot be asserted synchronously:
    // wait for the upload call to land before anything else.
    await waitFor(() =>
      expect(
        mockedClient.settings.setDocumentPresetTemplate
      ).toHaveBeenCalledTimes(1)
    );

    // The preset row is created first (one call), then the skeleton rides
    // SetDocumentPresetTemplate for the created preset.
    expect(mockedClient.settings.createDocumentPreset).toHaveBeenCalledTimes(1);
    expect(mockedClient.settings.createDocumentPreset).toHaveBeenCalledWith(
      "docx",
      "Brand Skeleton",
      JSON.stringify({ schema_version: 1, flavor: "template" })
    );
    expect(
      mockedClient.settings.setDocumentPresetTemplate
    ).toHaveBeenCalledWith(
      expect.any(String),
      "skeleton.docx",
      expect.any(String)
    );

    // The row re-lists with its template state — the chain's final write,
    // so nothing from this save is left pending when the test ends.
    const row = await findByTestId("document-preset-row-Brand Skeleton");

    expect(row.textContent).toContain("Template: skeleton.docx");
  });

  it("rejects a template upload whose extension does not match the format", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId, container } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");
    await user.click(getByTestId("document-tab-pptx"));
    await user.click(getByTestId("document-template-new-pptx"));
    await findByTestId("document-preset-name");

    await user.type(getByTestId("document-preset-name"), "Deck");
    await pickFile(container, "document-template-file", docxFile("wrong.docx"));
    await user.click(getByTestId("document-preset-save"));

    expect(getByTestId("document-editor-error").textContent).toContain(
      ".pptx file"
    );
    expect(mockedClient.settings.createDocumentPreset).not.toHaveBeenCalled();
    expect(
      mockedClient.settings.setDocumentPresetTemplate
    ).not.toHaveBeenCalled();
  });

  it("the PPTX tab hosts the template upload and no config editor", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId, queryByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");
    await user.click(getByTestId("document-tab-pptx"));
    await findByTestId("document-template-new-pptx");

    // PPTX presets are template-document presets only: no "New preset"
    // config editor exists on the tab.
    expect(queryByTestId("document-preset-new-pptx")).toBeNull();

    await user.click(getByTestId("document-template-new-pptx"));
    await findByTestId("document-preset-name");

    // The template editor opens: a name, the file picker, and none of the
    // config editor's fields.
    expect(getByTestId("document-template-file")).toBeTruthy();
    expect(getByTestId("document-template-choose")).toBeTruthy();
    expect(queryByTestId("document-theme")).toBeNull();
    expect(queryByTestId("document-page-toggle")).toBeNull();
  });

  it("shows a template preset's skeleton state on its row", async () => {
    presets = [
      entry("Deck", JSON.stringify({ schema_version: 1, flavor: "template" }), {
        format: "pptx",
        hasTemplate: true,
        templateName: "brand.pptx",
      }),
    ];
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");
    await user.click(getByTestId("document-tab-pptx"));

    const row = await findByTestId("document-preset-row-Deck");

    expect(row.textContent).toContain("Template: brand.pptx");
  });
});
