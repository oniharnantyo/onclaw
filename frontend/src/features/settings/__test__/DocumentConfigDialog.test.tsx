import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vite-plus/test";
import userEvent from "@testing-library/user-event";
import type { DocumentPresetEntry } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client } from "../../../lib/client";

import DocumentConfigDialog from "../components/DocumentConfigDialog";
import type { PresetConfig } from "../components/presetConfig";
import { renderUI } from "./test-utils";

/** The pdf preset fixture store the mocked client reads and writes. */
let presets: DocumentPresetEntry[] = [];

/** The stored default preset name; "" when none. Deleting the default
 * leaves it dangling (the backend's design), so the mock mirrors that. */
let defaultName = "";

let idSeq = 0;

function entry(name: string, config: string): DocumentPresetEntry {
  idSeq += 1;

  return {
    id: `preset-${idSeq}`,
    name,
    format: "pdf",
    config,
    createdAt: "2026-10-01T12:00:00Z",
    updatedAt: "2026-10-01T12:00:00Z",
  };
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

          const created = entry(name, configJSON);
          created.format = format;
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

          const copy = entry(newName, source.config);
          presets = [...presets, copy];

          return copy;
        }
      ),
      deleteDocumentPreset: vi.fn(async (id: string): Promise<void> => {
        presets = presets.filter(
          (p: DocumentPresetEntry): boolean => p.id !== id
        );
      }),
      defaultDocumentPreset: vi.fn(async (format: string): Promise<string> =>
        format === "pdf" ? defaultName : ""
      ),
      setDefaultDocumentPreset: vi.fn(
        async (_format: string, name: string): Promise<void> => {
          defaultName = name;
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
  };
};

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

describe("DocumentConfigDialog", () => {
  beforeEach(() => {
    presets = [];
    defaultName = "";
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
    defaultName = "Client Report";

    const { getByTestId, findByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");

    const pdfTab = getByTestId("document-tab-pdf");
    expect(pdfTab.getAttribute("aria-selected")).toBe("true");
    // Future formats render as disabled placeholders — no engines invented.
    expect(getByTestId("document-tab-docx").hasAttribute("disabled")).toBe(
      true
    );

    expect(getByTestId("document-preset-row-Client Report")).toBeTruthy();
    expect(
      getByTestId("document-preset-row-Client Report").textContent
    ).toContain("academic theme");
    expect(getByTestId("document-preset-row-Invoice")).toBeTruthy();

    // SAFETY: the query targets this dialog's default-preset select, an
    // HTMLSelectElement by construction.
    expect(
      (getByTestId("document-default-preset") as HTMLSelectElement).value
    ).toBe("Client Report");
  });

  it("shows a stored default naming a missing preset as none", async () => {
    presets = [entry("Invoice", JSON.stringify({ schema_version: 1 }))];
    defaultName = "Ghost";

    const { getByTestId, findByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");

    // SAFETY: the query targets this dialog's default-preset select, an
    // HTMLSelectElement by construction.
    expect(
      (getByTestId("document-default-preset") as HTMLSelectElement).value
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
    expect(queryByTestId("document-default-preset")).toBeNull();
    expect(getByTestId("document-preset-new")).toBeTruthy();
  });

  it("creates a preset through the editor with a schema-stamped config", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <DocumentConfigDialog onClose={vi.fn()} />
    );

    await findByTestId("document-config-dialog");
    await user.click(getByTestId("document-preset-new"));
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
    await user.click(getByTestId("document-preset-new"));

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
    defaultName = "Client Report";
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
      (getByTestId("document-default-preset") as HTMLSelectElement).value
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

    await user.selectOptions(getByTestId("document-default-preset"), "Memo");
    expect(mockedClient.settings.setDefaultDocumentPreset).toHaveBeenCalledWith(
      "pdf",
      "Memo"
    );

    await user.selectOptions(getByTestId("document-default-preset"), "");
    expect(mockedClient.settings.setDefaultDocumentPreset).toHaveBeenCalledWith(
      "pdf",
      ""
    );
  });
});
