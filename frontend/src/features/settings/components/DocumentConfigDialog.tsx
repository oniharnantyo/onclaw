import { useEffect, useRef, useState } from "react";
import type { ChangeEvent, ReactElement } from "react";

import type { DocumentPresetEntry } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { ModalShell } from "../../../components";
import { Alert } from "../../../components/ui/Alert";
import { Button } from "../../../components/ui/Button";
import { Icon } from "../../../components/ui/Icon";
import { Input } from "../../../components/ui/Input";
import { Select } from "../../../components/ui/Select";
import { client } from "../../../lib/client";
import { cn } from "../../../lib/cn";
import PresetEditor, {
  DOCXPresetEditor,
  TemplatePresetEditor,
} from "./PresetEditor";
import {
  buildConfigJSON,
  buildDOCXConfigJSON,
  buildTemplateConfigJSON,
  decodeConfig,
  decodeDOCXConfig,
  emptyDOCXForm,
  emptyForm,
  HEX_COLOR_RE,
  MARGIN_MAX,
  MARGIN_MIN,
  parseDOCXConfig,
  parsePresetConfig,
  presetFlavor,
  templateExtension,
  FLAVOR_TEMPLATE,
  type CoverForm,
  type DOCXForm,
  type PresetForm,
  TOC_DEPTH_MAX,
  TOC_DEPTH_MIN,
  DOCX_COLOR_RE,
  DOCX_FONT_SIZE_MAX,
  DOCX_FONT_SIZE_MIN,
  DOCX_MARGIN_MAX,
  WATERMARK_ANGLE_MAX,
  WATERMARK_ANGLE_MIN,
  WATERMARK_OPACITY_MAX,
} from "./presetConfig";

/** The supported format tabs (spec add-document-editing "Preset management
 * in the Tools pane"). PDF presets are config presets; DOCX presets come in
 * both flavors; PPTX presets are template-document presets only. */
const FORMATS = [
  { id: "pdf", label: "PDF" },
  { id: "docx", label: "DOCX" },
  { id: "pptx", label: "PPTX" },
] as const;

type FormatID = (typeof FORMATS)[number]["id"];

const FORMAT_LABELS: Record<FormatID, string> = {
  pdf: "PDF",
  docx: "DOCX",
  pptx: "PPTX",
};

/** The list view's one-line description per format. */
const FORMAT_DESCRIPTIONS: Record<FormatID, string> = {
  pdf: "Presets style the PDFs agents generate with create_document.",
  docx: "Config presets style the built-in DOCX template; template presets generate from an uploaded .docx skeleton.",
  pptx: "Template presets are uploaded .pptx skeletons; generation fills their existing layouts.",
};

/** The list's hint under the Presets header per format. */
const FORMAT_HINTS: Record<FormatID, string> = {
  pdf: "Theme, page setup, cover, footer, watermark, and table of contents for one PDF style.",
  docx: "Config presets restyle the built-in template; template presets generate from an uploaded skeleton.",
  pptx: "Each preset is an uploaded .pptx skeleton; generation fills its existing layouts and placeholders.",
};

/** The empty-state line per format. */
const FORMAT_EMPTY: Record<FormatID, string> = {
  pdf: "No presets yet — create_document uses the library defaults until you add one.",
  docx: "No presets yet — DOCX generation uses the built-in template until you add one.",
  pptx: "No templates yet — PPTX generation needs an uploaded .pptx skeleton.",
};

/** One format's loaded presets and default selection. */
interface FormatData {
  presets: DocumentPresetEntry[];
  defaultName: string;
}

/** The open editor: the PDF grouped form, the DOCX grouped form, or the
 * shared template-document form. The dialog owns all three states. */
type Editor =
  | { kind: "pdf"; form: PresetForm }
  | { kind: "docx"; form: DOCXForm }
  | {
      kind: "template";
      format: FormatID;
      /** The preset's id; "" while creating. */
      id: string;
      name: string;
      /** The stored config JSON, passed through verbatim on update so the
       * service's has-template metadata survives the rename. */
      configJSON: string;
      /** The stored skeleton's name; "" while the preset has none. */
      templateName: string;
      /** The pending upload, or null. */
      file: File | null;
    };

/** The row's summary line for a PDF preset: theme, page geometry, and the
 * features on. */
function pdfMeta(entry: DocumentPresetEntry): string {
  const config = decodeConfig(entry.config);

  if (config === null) return "custom settings";

  const parts: string[] = [];

  parts.push(config.theme ? `${config.theme} theme` : "library default");

  if (config.page) {
    const size = (config.page.size ?? "").toUpperCase();
    const orientation = config.page.orientation ?? "";
    const geometry = [size, orientation].filter(Boolean).join(" · ");

    if (geometry) parts.push(geometry);
  }

  if (config.cover) parts.push("cover");

  if (config.footer) parts.push("footer");

  if (config.watermark) parts.push("watermark");

  if (config.toc) parts.push("TOC");

  return parts.join(" · ");
}

/** The row's summary line for a DOCX config preset: the parameters set over
 * the built-in template. */
function docxConfigMeta(entry: DocumentPresetEntry): string {
  const config = decodeDOCXConfig(entry.config);

  if (config === null) return "custom settings";

  const parts: string[] = [];

  if (config.fonts) {
    if (config.fonts.body_family || config.fonts.body_size) {
      parts.push(
        `Body ${config.fonts.body_family || "default"}${
          config.fonts.body_size ? ` ${config.fonts.body_size}pt` : ""
        }`.trim()
      );
    }

    if (config.fonts.heading_family || config.fonts.heading_size) {
      parts.push(
        `Headings ${config.fonts.heading_family || "default"}${
          config.fonts.heading_size ? ` ${config.fonts.heading_size}pt` : ""
        }`.trim()
      );
    }
  }

  if (config.heading) parts.push("heading style");

  if (config.page) {
    const size = (config.page.size ?? "").toUpperCase();
    const orientation = config.page.orientation ?? "";
    const geometry = [size, orientation].filter(Boolean).join(" · ");

    if (geometry) parts.push(geometry);

    if (config.page.margin_inches !== undefined) {
      parts.push(`margin ${config.page.margin_inches}in`);
    }
  }

  if (config.footer) parts.push("footer");

  return parts.length > 0 ? parts.join(" · ") : "built-in template defaults";
}

/** The row's summary line for a template-document preset: its skeleton's
 * state (DocumentPresetEntry mirrors has_template/template_name). */
function templateMeta(entry: DocumentPresetEntry): string {
  return entry.hasTemplate
    ? `Template: ${entry.templateName ?? ""}`
    : "No template yet";
}

/** The row's summary line, dispatched by format and flavor. */
function presetMeta(entry: DocumentPresetEntry): string {
  if (entry.format === "pdf") return pdfMeta(entry);

  if (entry.format === "docx" && presetFlavor(entry.config) !== FLAVOR_TEMPLATE)
    return docxConfigMeta(entry);

  return templateMeta(entry);
}

/** File → base64 (without the data-URL prefix) for the template upload
 * binding call (the chat-attachments FileReader shape, local to the
 * dialog). */
function fileToBase64(file: File): Promise<string> {
  return new Promise(
    (
      resolve: (value: string) => void,
      reject: (cause: Error) => void
    ): void => {
      const reader = new FileReader();

      reader.onload = (): void => {
        // SAFETY: readAsDataURL always resolves a data: URL string (never an
        // ArrayBuffer); an empty result means the read produced nothing.
        const result = reader.result as string;

        if (result === "") {
          reject(new Error("Could not read the file"));

          return;
        }

        const comma = result.indexOf(",");

        resolve(comma === -1 ? result : result.slice(comma + 1));
      };

      reader.onerror = (): void => reject(new Error("Could not read the file"));
      reader.readAsDataURL(file);
    }
  );
}

/**
 * DocumentConfigDialog: the create_document tool's preset manager (spec
 * add-document-editing "Preset management in the Tools pane") — one tab per
 * supported format (PDF, DOCX, PPTX), each listing that format's presets
 * with create/edit/duplicate/delete and hosting the format's default-preset
 * selector. The DOCX tab offers both the grouped config-preset editor and
 * the template upload; the PPTX tab is template-upload only. Every config
 * save is one Create/Update call — the whole config JSON, no partial
 * writes; a template upload rides SetDocumentPresetTemplate.
 */
export default function DocumentConfigDialog({
  onClose,
}: {
  onClose: () => void;
}): ReactElement {
  const [format, setFormat] = useState<FormatID>("pdf");
  const [byFormat, setByFormat] = useState<Record<string, FormatData>>({});
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [generalError, setGeneralError] = useState<string | null>(null);

  const [editing, setEditing] = useState<Editor | null>(null);
  const [editorWarn, setEditorWarn] = useState<string | null>(null);
  const [editorError, setEditorError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  /** The formats whose presets were already read; the load effect only
   * fetches a format once (mutations refresh the active one). */
  const loadedRef = useRef<Set<string>>(new Set());

  /** The row's armed inline action ("delete" or "duplicate"), mirroring the
   * BrowserConfigDialog's arm-then-confirm delete. */
  const [confirming, setConfirming] = useState<{
    id: string;
    action: "delete" | "duplicate";
  } | null>(null);

  const [duplicateName, setDuplicateName] = useState("");
  const [duplicateError, setDuplicateError] = useState<string | null>(null);

  useEffect(() => {
    if (loadedRef.current.has(format)) return;

    setLoading(true);
    setLoadError(null);

    let cancelled = false;

    const load = async (): Promise<void> => {
      try {
        const [list, def] = await Promise.all([
          client.settings.documentPresets(format),
          client.settings.defaultDocumentPreset(format),
        ]);

        if (cancelled) return;

        setByFormat((prev: Record<string, FormatData>) => ({
          ...prev,
          [format]: {
            presets: list,
            // A stored default naming a missing preset reads as none — the
            // backend leaves it dangling by design.
            defaultName: list.some(
              (p: DocumentPresetEntry): boolean => p.name === def
            )
              ? def
              : "",
          },
        }));
        loadedRef.current.add(format);
      } catch (err) {
        if (!cancelled) {
          setLoadError(err instanceof Error ? err.message : String(err));
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    };

    void load();

    return (): void => {
      cancelled = true;
    };
  }, [format]);

  /** Re-reads one format's presets and default after a mutation. */
  const refresh = async (fmt: FormatID): Promise<void> => {
    const [list, def] = await Promise.all([
      client.settings.documentPresets(fmt),
      client.settings.defaultDocumentPreset(fmt),
    ]);

    setByFormat((prev: Record<string, FormatData>) => ({
      ...prev,
      [fmt]: {
        presets: list,
        defaultName: list.some(
          (p: DocumentPresetEntry): boolean => p.name === def
        )
          ? def
          : "",
      },
    }));
    loadedRef.current.add(fmt);
  };

  const data: FormatData = byFormat[format] ?? { presets: [], defaultName: "" };

  const changeDefault = async (name: string): Promise<void> => {
    setGeneralError(null);

    try {
      await client.settings.setDefaultDocumentPreset(format, name);
      setDefaultSelection(await client.settings.defaultDocumentPreset(format));
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    }
  };

  /** Writes the freshly read default back into the format's cache. */
  const setDefaultSelection = (def: string): void => {
    setByFormat((prev: Record<string, FormatData>) => {
      const current = prev[format];

      if (!current) return prev;

      return {
        ...prev,
        [format]: {
          ...current,
          defaultName: current.presets.some(
            (p: DocumentPresetEntry): boolean => p.name === def
          )
            ? def
            : "",
        },
      };
    });
  };

  const openPdfEditor = (entry?: DocumentPresetEntry): void => {
    setGeneralError(null);
    setConfirming(null);
    setEditorError(null);

    if (!entry) {
      setEditing({ kind: "pdf", form: emptyForm() });
      setEditorWarn(null);

      return;
    }

    const parsed = parsePresetConfig(entry.config);

    setEditing({
      kind: "pdf",
      form: { ...parsed.form, id: entry.id, name: entry.name },
    });
    setEditorWarn(parsed.warn);
  };

  const openDocxEditor = (entry?: DocumentPresetEntry): void => {
    setGeneralError(null);
    setConfirming(null);
    setEditorError(null);

    if (!entry) {
      setEditing({ kind: "docx", form: emptyDOCXForm() });
      setEditorWarn(null);

      return;
    }

    const parsed = parseDOCXConfig(entry.config);

    setEditing({
      kind: "docx",
      form: { ...parsed.form, id: entry.id, name: entry.name },
    });
    setEditorWarn(parsed.warn);
  };

  const openTemplateEditor = (
    fmt: FormatID,
    entry?: DocumentPresetEntry
  ): void => {
    setGeneralError(null);
    setConfirming(null);
    setEditorError(null);
    setEditorWarn(null);

    setEditing({
      kind: "template",
      format: fmt,
      id: entry?.id ?? "",
      name: entry?.name ?? "",
      // The stored config passes through verbatim: the service stamps the
      // has-template metadata over it and a rename must keep it.
      configJSON: entry?.config ?? "",
      templateName: entry?.templateName ?? "",
      file: null,
    });
  };

  /** Opens the right editor for a listed row: PDFs are config presets, PPTX
   * presets are templates, and a DOCX preset's flavor decides. */
  const editPreset = (entry: DocumentPresetEntry): void => {
    if (entry.format === "pdf") {
      openPdfEditor(entry);

      return;
    }

    if (
      entry.format === "docx" &&
      presetFlavor(entry.config) !== FLAVOR_TEMPLATE
    ) {
      openDocxEditor(entry);

      return;
    }

    openTemplateEditor(entry.format === "docx" ? "docx" : "pptx", entry);
  };

  const patchPdf = (update: Partial<PresetForm>): void => {
    setEditing((prev: Editor | null): Editor | null =>
      prev && prev.kind === "pdf"
        ? { ...prev, form: { ...prev.form, ...update } }
        : prev
    );
  };

  const patchPdfCover = (key: keyof CoverForm, value: string): void => {
    setEditing((prev: Editor | null): Editor | null =>
      prev && prev.kind === "pdf"
        ? {
            ...prev,
            form: { ...prev.form, cover: { ...prev.form.cover, [key]: value } },
          }
        : prev
    );
  };

  const patchDocx = (update: Partial<DOCXForm>): void => {
    setEditing((prev: Editor | null): Editor | null =>
      prev && prev.kind === "docx"
        ? { ...prev, form: { ...prev.form, ...update } }
        : prev
    );
  };

  const patchTemplate = (update: {
    name?: string;
    file?: File | null;
  }): void => {
    setEditing((prev: Editor | null): Editor | null =>
      prev && prev.kind === "template" ? { ...prev, ...update } : prev
    );
  };

  /** A number field's range problem, or null when empty/valid. Empty is the
   * default for every numeric field. */
  const rangeProblem = (
    label: string,
    text: string,
    min: number,
    max: number
  ): string | null => {
    if (text.trim() === "") return null;

    const value = Number(text.trim());

    if (!Number.isFinite(value) || value < min || value > max) {
      return `${label} must be between ${min} and ${max}.`;
    }

    return null;
  };

  /** The editor's name-clash problem, or null. Names are unique per format
   * case-insensitively (the store's rule). */
  const nameProblem = (name: string, id: string): string | null => {
    if (!name) return "Give the preset a name.";

    const clash = data.presets.some(
      (p: DocumentPresetEntry): boolean =>
        p.id !== id && p.name.toLowerCase() === name.toLowerCase()
    );

    if (clash) {
      return `A preset named “${name}” already exists for ${FORMAT_LABELS[format]}.`;
    }

    return null;
  };

  /** Saves the PDF editor as ONE call — create or update of the whole config
   * document; there are no partial field writes. */
  const savePdf = async (): Promise<void> => {
    if (editing === null || editing.kind !== "pdf") return;

    const form = editing.form;
    const name = form.name.trim();

    const problem = nameProblem(name, form.id);

    if (problem) {
      setEditorError(problem);

      return;
    }

    const margin =
      form.pageMargin.trim() === "" ? 0 : Number(form.pageMargin.trim());

    if (
      !Number.isFinite(margin) ||
      (margin !== 0 && (margin < MARGIN_MIN || margin > MARGIN_MAX))
    ) {
      setEditorError(
        `Page margin must be empty (library default) or between ${MARGIN_MIN} and ${MARGIN_MAX} inches.`
      );

      return;
    }

    const color = form.watermarkColor.trim();

    if (color && !HEX_COLOR_RE.test(color)) {
      setEditorError("The watermark color must be hex, like #888888.");

      return;
    }

    const range =
      rangeProblem(
        "Watermark opacity",
        form.watermarkOpacity,
        0,
        WATERMARK_OPACITY_MAX
      ) ??
      rangeProblem(
        "Watermark angle",
        form.watermarkAngle,
        WATERMARK_ANGLE_MIN,
        WATERMARK_ANGLE_MAX
      ) ??
      rangeProblem(
        "TOC minimum depth",
        form.tocMinDepth,
        TOC_DEPTH_MIN,
        TOC_DEPTH_MAX
      ) ??
      rangeProblem(
        "TOC maximum depth",
        form.tocMaxDepth,
        TOC_DEPTH_MIN,
        TOC_DEPTH_MAX
      );

    if (range) {
      setEditorError(range);

      return;
    }

    if (
      form.tocMinDepth.trim() &&
      form.tocMaxDepth.trim() &&
      Number(form.tocMinDepth.trim()) > Number(form.tocMaxDepth.trim())
    ) {
      setEditorError("The TOC's minimum depth cannot exceed its maximum.");

      return;
    }

    setEditorError(null);
    setSaving(true);

    try {
      const configJSON = buildConfigJSON(form);

      if (form.id) {
        await client.settings.updateDocumentPreset(form.id, name, configJSON);
      } else {
        await client.settings.createDocumentPreset(format, name, configJSON);
      }

      setEditing(null);
      await refresh(format);
    } catch (err) {
      setEditorError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  /** Saves the DOCX config editor as ONE call with the full config JSON. */
  const saveDocx = async (): Promise<void> => {
    if (editing === null || editing.kind !== "docx") return;

    const form = editing.form;
    const name = form.name.trim();

    const problem = nameProblem(name, form.id);

    if (problem) {
      setEditorError(problem);

      return;
    }

    const range =
      rangeProblem(
        "Body size",
        form.fontsBodySize,
        DOCX_FONT_SIZE_MIN,
        DOCX_FONT_SIZE_MAX
      ) ??
      rangeProblem(
        "Heading size",
        form.fontsHeadingSize,
        DOCX_FONT_SIZE_MIN,
        DOCX_FONT_SIZE_MAX
      );

    if (range) {
      setEditorError(range);

      return;
    }

    const color = form.headingColor.trim();

    if (color && !DOCX_COLOR_RE.test(color)) {
      setEditorError("The heading color must be hex, like #1F3864.");

      return;
    }

    const margin = form.pageMargin.trim();

    if (
      margin !== "" &&
      (!Number.isFinite(Number(margin)) ||
        Number(margin) < 0 ||
        Number(margin) > DOCX_MARGIN_MAX)
    ) {
      setEditorError(
        `Page margin must be empty (built-in default) or between 0 and ${DOCX_MARGIN_MAX} inches.`
      );

      return;
    }

    setEditorError(null);
    setSaving(true);

    try {
      const configJSON = buildDOCXConfigJSON(form);

      if (form.id) {
        await client.settings.updateDocumentPreset(form.id, name, configJSON);
      } else {
        await client.settings.createDocumentPreset(format, name, configJSON);
      }

      setEditing(null);
      await refresh(format);
    } catch (err) {
      setEditorError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  /** Saves the template editor: the preset row first (one create or update
   * call), then the pending skeleton through SetDocumentPresetTemplate. */
  const saveTemplate = async (): Promise<void> => {
    if (editing === null || editing.kind !== "template") return;

    const name = editing.name.trim();

    const problem = nameProblem(name, editing.id);

    if (problem) {
      setEditorError(problem);

      return;
    }

    if (editing.file) {
      const ext = templateExtension(editing.format);

      if (!editing.file.name.toLowerCase().endsWith(ext)) {
        setEditorError(`The template must be a ${ext} file.`);

        return;
      }
    }

    setEditorError(null);
    setSaving(true);

    try {
      const created = editing.id
        ? null
        : await client.settings.createDocumentPreset(
            editing.format,
            name,
            buildTemplateConfigJSON()
          );

      if (created) {
        // The row exists now; an upload failure below keeps the editor open
        // as an edit of the created preset, so a retry never re-creates.
        setEditing({ ...editing, id: created.id, configJSON: created.config });
      } else {
        await client.settings.updateDocumentPreset(
          editing.id,
          name,
          editing.configJSON
        );
      }

      // SAFETY: exactly one branch above ran, so the id is set either way.
      const presetID = created ? created.id : editing.id;

      if (editing.file) {
        const base64 = await fileToBase64(editing.file);

        await client.settings.setDocumentPresetTemplate(
          presetID,
          editing.file.name,
          base64
        );
      }

      setEditing(null);
      await refresh(editing.format);
    } catch (err) {
      setEditorError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const saveEditor = (): void => {
    if (editing === null) return;

    if (editing.kind === "pdf") void savePdf();
    else if (editing.kind === "docx") void saveDocx();
    else void saveTemplate();
  };

  const armDuplicate = (entry: DocumentPresetEntry): void => {
    setGeneralError(null);
    setConfirming({ id: entry.id, action: "duplicate" });
    setDuplicateName(`${entry.name} copy`);
    setDuplicateError(null);
  };

  const runDuplicate = async (): Promise<void> => {
    if (!confirming) return;

    const entry = data.presets.find(
      (p: DocumentPresetEntry): boolean => p.id === confirming.id
    );

    if (!entry) {
      setConfirming(null);

      return;
    }

    const newName = duplicateName.trim();

    if (!newName) {
      setDuplicateError("Give the copy a name.");

      return;
    }

    if (
      data.presets.some(
        (p: DocumentPresetEntry): boolean =>
          p.name.toLowerCase() === newName.toLowerCase()
      )
    ) {
      setDuplicateError(
        `A preset named “${newName}” already exists for ${FORMAT_LABELS[format]}.`
      );

      return;
    }

    try {
      await client.settings.duplicateDocumentPreset(entry.id, newName);
      setConfirming(null);
      await refresh(format);
    } catch (err) {
      setDuplicateError(err instanceof Error ? err.message : String(err));
    }
  };

  const removePreset = async (entry: DocumentPresetEntry): Promise<void> => {
    setGeneralError(null);
    setConfirming(null);

    try {
      await client.settings.deleteDocumentPreset(entry.id);

      if (
        editing &&
        ((editing.kind !== "template" && editing.form.id === entry.id) ||
          (editing.kind === "template" && editing.id === entry.id))
      ) {
        setEditing(null);
      }

      await refresh(format);
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    }
  };

  const editorPresetID =
    editing === null
      ? ""
      : editing.kind === "template"
        ? editing.id
        : editing.form.id;

  const editorTitle =
    editing === null
      ? "Document presets"
      : editorPresetID
        ? "Edit preset"
        : "New preset";

  const editorDescription =
    editing === null
      ? FORMAT_DESCRIPTIONS[format]
      : editing.kind === "template"
        ? "The uploaded skeleton styles the generated document."
        : "Saved as one document — no partial writes.";

  return (
    <ModalShell
      title={editorTitle}
      description={editorDescription}
      testId="document-config-dialog"
      wide
      onClose={onClose}
      actions={
        editing ? (
          <>
            <Button
              variant="ghost"
              size="sm"
              data-testid="document-preset-cancel"
              onClick={() => setEditing(null)}
            >
              Cancel
            </Button>
            <Button
              size="sm"
              loading={saving}
              data-testid="document-preset-save"
              onClick={saveEditor}
            >
              Save preset
            </Button>
          </>
        ) : (
          <Button variant="ghost" size="sm" onClick={onClose}>
            Close
          </Button>
        )
      }
    >
      {loading ? (
        <p className="text-[13px] text-muted">Loading…</p>
      ) : loadError ? (
        <div role="alert" className="mb-3">
          <Alert tone="danger" testId="document-load-error">
            {loadError}
          </Alert>
        </div>
      ) : editing ? (
        <div className="space-y-3">
          {editorWarn ? (
            <div role="alert">
              <Alert tone="warn" testId="document-editor-warn">
                {editorWarn}
              </Alert>
            </div>
          ) : null}

          {editorError ? (
            <div role="alert">
              <Alert tone="danger" testId="document-editor-error">
                {editorError}
              </Alert>
            </div>
          ) : null}

          {editing.kind === "pdf" ? (
            <PresetEditor
              form={editing.form}
              onPatch={patchPdf}
              onPatchCover={patchPdfCover}
            />
          ) : editing.kind === "docx" ? (
            <DOCXPresetEditor form={editing.form} onPatch={patchDocx} />
          ) : (
            <TemplatePresetEditor
              name={editing.name}
              onName={(name: string) => patchTemplate({ name })}
              accept={templateExtension(editing.format)}
              templateName={editing.templateName}
              file={editing.file}
              onFile={(file: File | null) => patchTemplate({ file })}
            />
          )}
        </div>
      ) : (
        <div className="space-y-4">
          <div
            className="flex gap-1 border-b border-line"
            role="tablist"
            aria-label="Document formats"
            data-testid="document-tabs"
          >
            {FORMATS.map((f: (typeof FORMATS)[number]) => (
              <button
                key={f.id}
                type="button"
                role="tab"
                aria-selected={format === f.id}
                data-testid={`document-tab-${f.id}`}
                onClick={() => setFormat(f.id)}
                className={cn(
                  "-mb-px rounded-t-md border-b-2 px-3 py-1.5 text-[12px] font-medium transition-colors",
                  format === f.id
                    ? "border-accent text-fg"
                    : "border-transparent text-muted hover:text-fg"
                )}
              >
                {f.label}
              </button>
            ))}
          </div>

          {generalError ? (
            <div role="alert">
              <Alert tone="danger" testId="document-error">
                {generalError}
              </Alert>
            </div>
          ) : null}

          {data.presets.length > 0 ? (
            <div>
              <label
                className="mb-1.5 block text-[12px] font-medium text-fg2"
                htmlFor={`document-default-preset-${format}`}
              >
                Default preset
              </label>
              <Select
                id={`document-default-preset-${format}`}
                data-testid={`document-default-preset-${format}`}
                className="h-8 w-48 text-[13px]"
                value={data.defaultName}
                onChange={(e: ChangeEvent<HTMLSelectElement>) =>
                  void changeDefault(e.target.value)
                }
              >
                <option value="">None (library defaults)</option>
                {data.presets.map((p: DocumentPresetEntry) => (
                  <option key={p.id} value={p.name}>
                    {p.name}
                  </option>
                ))}
              </Select>
              <p className="mt-1 text-[11px] leading-4 text-muted">
                Used when a create_document call names no preset.
              </p>
            </div>
          ) : null}

          <div>
            <div className="flex items-center justify-between">
              <span className="text-[12px] font-medium text-fg2">Presets</span>
              <div className="flex items-center gap-2">
                {format !== "pptx" ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    data-testid={`document-preset-new-${format}`}
                    onClick={() =>
                      format === "pdf" ? openPdfEditor() : openDocxEditor()
                    }
                  >
                    <Icon name="plus" size={13} /> New preset
                  </Button>
                ) : null}
                {format !== "pdf" ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    data-testid={`document-template-new-${format}`}
                    onClick={() => openTemplateEditor(format)}
                  >
                    <Icon name="file" size={13} /> Upload template
                  </Button>
                ) : null}
              </div>
            </div>
            <p className="mt-1 text-[11px] leading-4 text-muted">
              {FORMAT_HINTS[format]}
            </p>
          </div>

          {data.presets.length === 0 ? (
            <p
              className="rounded-md border border-dashed border-line px-3 py-4 text-center text-[12px] text-muted"
              data-testid="document-presets-empty"
            >
              {FORMAT_EMPTY[format]}
            </p>
          ) : (
            <div className="space-y-2">
              {data.presets.map((entry: DocumentPresetEntry) => {
                const confirmingRow =
                  confirming && confirming.id === entry.id
                    ? confirming.action
                    : null;

                const isDefault = entry.name === data.defaultName;

                return (
                  <div
                    key={entry.id}
                    className="rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_2%,var(--surface))] px-3 py-2"
                    data-testid={`document-preset-row-${entry.name}`}
                  >
                    {confirmingRow === null ? (
                      <div className="flex items-center gap-2">
                        <div className="min-w-0 flex-1">
                          <p className="truncate text-[13px] font-medium text-fg">
                            {entry.name}
                            {isDefault ? (
                              <span className="ml-2 font-sans text-[11px] font-normal text-muted">
                                default
                              </span>
                            ) : null}
                          </p>
                          <p className="truncate text-[11px] text-muted">
                            {presetMeta(entry)}
                          </p>
                        </div>
                        <Button
                          variant="ghost"
                          size="sm"
                          aria-label={`Edit preset ${entry.name}`}
                          data-testid={`document-preset-edit-${entry.name}`}
                          onClick={() => editPreset(entry)}
                        >
                          <Icon name="edit" size={12} /> Edit
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          aria-label={`Duplicate preset ${entry.name}`}
                          data-testid={`document-preset-duplicate-${entry.name}`}
                          onClick={() => armDuplicate(entry)}
                        >
                          <Icon name="copy" size={12} /> Duplicate
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          aria-label={`Delete preset ${entry.name}`}
                          data-testid={`document-preset-delete-${entry.name}`}
                          onClick={() =>
                            setConfirming({ id: entry.id, action: "delete" })
                          }
                        >
                          <Icon name="x" size={12} /> Delete
                        </Button>
                      </div>
                    ) : confirmingRow === "delete" ? (
                      <div className="flex items-center gap-2">
                        <span className="flex-1 text-[11px] text-muted">
                          Delete this preset?
                        </span>
                        <Button
                          variant="destructive"
                          size="sm"
                          aria-label={`Confirm delete preset ${entry.name}`}
                          data-testid={`document-preset-delete-${entry.name}`}
                          onClick={() => void removePreset(entry)}
                        >
                          Delete
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          aria-label="Cancel delete"
                          onClick={() => setConfirming(null)}
                        >
                          Cancel
                        </Button>
                      </div>
                    ) : (
                      <div className="space-y-1.5">
                        <div className="flex items-center gap-2">
                          <label
                            className="shrink-0 text-[11px] text-muted"
                            htmlFor={`document-duplicate-name-${entry.id}`}
                          >
                            New name
                          </label>
                          <Input
                            id={`document-duplicate-name-${entry.id}`}
                            aria-label="Duplicate preset name"
                            className="h-8 flex-1 text-[13px]"
                            data-testid="document-duplicate-name"
                            invalid={duplicateError !== null}
                            value={duplicateName}
                            onChange={(e: ChangeEvent<HTMLInputElement>) => {
                              setDuplicateName(e.target.value);

                              if (duplicateError) setDuplicateError(null);
                            }}
                          />
                        </div>
                        {duplicateError ? (
                          <p
                            className="text-[12px] text-dangertext"
                            data-testid="document-duplicate-error"
                          >
                            {duplicateError}
                          </p>
                        ) : null}
                        <div className="flex items-center justify-end gap-2">
                          <Button
                            size="sm"
                            data-testid="document-duplicate-confirm"
                            onClick={() => void runDuplicate()}
                          >
                            Duplicate
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            aria-label="Cancel duplicate"
                            onClick={() => setConfirming(null)}
                          >
                            Cancel
                          </Button>
                        </div>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}
    </ModalShell>
  );
}
