import { useEffect, useState } from "react";
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
import PresetEditor from "./PresetEditor";
import {
  buildConfigJSON,
  decodeConfig,
  emptyForm,
  HEX_COLOR_RE,
  MARGIN_MAX,
  MARGIN_MIN,
  parsePresetConfig,
  type CoverForm,
  type PresetForm,
  TOC_DEPTH_MAX,
  TOC_DEPTH_MIN,
  WATERMARK_ANGLE_MAX,
  WATERMARK_ANGLE_MIN,
  WATERMARK_OPACITY_MAX,
} from "./presetConfig";

/** The only format engine in v1 (design D5). The other tabs render disabled
 * so future formats slot in without reshaping the dialog — no engines are
 * invented for them. */
const FORMAT_PDF = "pdf";

/** One format tab's descriptor. */
interface FormatTab {
  id: string;
  label: string;
  enabled: boolean;
}

const FORMATS: FormatTab[] = [
  { id: "pdf", label: "PDF", enabled: true },
  { id: "docx", label: "DOCX", enabled: false },
  { id: "xlsx", label: "XLSX", enabled: false },
  { id: "pptx", label: "PPTX", enabled: false },
];

/** The row's summary line: theme, page geometry, and the features on. */
function presetMeta(entry: DocumentPresetEntry): string {
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

/**
 * DocumentConfigDialog: the create_document tool's preset manager (spec
 * add-create-document "Preset management in the Tools pane") — format tabs
 * (PDF only in v1), the format's presets with create/edit/duplicate/delete,
 * the format's default-preset selector, and PresetEditor, the grouped form
 * saving each preset atomically as one config JSON document.
 */
export default function DocumentConfigDialog({
  onClose,
}: {
  onClose: () => void;
}): ReactElement {
  const [format, setFormat] = useState(FORMAT_PDF);
  const [presets, setPresets] = useState<DocumentPresetEntry[]>([]);
  const [defaultName, setDefaultName] = useState("");
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [generalError, setGeneralError] = useState<string | null>(null);

  const [editing, setEditing] = useState<PresetForm | null>(null);
  const [editorWarn, setEditorWarn] = useState<string | null>(null);
  const [editorError, setEditorError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  /** The row's armed inline action ("delete" or "duplicate"), mirroring the
   * BrowserConfigDialog's arm-then-confirm delete. */
  const [confirming, setConfirming] = useState<{
    id: string;
    action: "delete" | "duplicate";
  } | null>(null);

  const [duplicateName, setDuplicateName] = useState("");
  const [duplicateError, setDuplicateError] = useState<string | null>(null);

  useEffect(() => {
    const load = async (): Promise<void> => {
      try {
        const [list, def] = await Promise.all([
          client.settings.documentPresets(FORMAT_PDF),
          client.settings.defaultDocumentPreset(FORMAT_PDF),
        ]);

        setPresets(list);
        // A stored default naming a missing preset reads as none — the
        // backend leaves it dangling by design.
        setDefaultName(
          list.some((p: DocumentPresetEntry): boolean => p.name === def)
            ? def
            : ""
        );
      } catch (err) {
        setLoadError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
      }
    };

    void load();
  }, []);

  /** Re-reads the format's presets and default after a mutation. */
  const refresh = async (): Promise<void> => {
    const [list, def] = await Promise.all([
      client.settings.documentPresets(format),
      client.settings.defaultDocumentPreset(format),
    ]);

    setPresets(list);
    setDefaultName(
      list.some((p: DocumentPresetEntry): boolean => p.name === def) ? def : ""
    );
  };

  const changeDefault = async (name: string): Promise<void> => {
    setGeneralError(null);

    try {
      await client.settings.setDefaultDocumentPreset(format, name);
      setDefaultName(await client.settings.defaultDocumentPreset(format));
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    }
  };

  const openEditor = (entry?: DocumentPresetEntry): void => {
    setGeneralError(null);
    setConfirming(null);
    setEditorError(null);

    if (!entry) {
      setEditing(emptyForm());
      setEditorWarn(null);

      return;
    }

    const parsed = parsePresetConfig(entry.config);

    setEditing({ ...parsed.form, id: entry.id, name: entry.name });
    setEditorWarn(parsed.warn);
  };

  const patch = (update: Partial<PresetForm>): void => {
    setEditing((prev: PresetForm | null): PresetForm | null =>
      prev ? { ...prev, ...update } : prev
    );
  };

  const patchCover = (key: keyof CoverForm, value: string): void => {
    setEditing((prev: PresetForm | null): PresetForm | null =>
      prev ? { ...prev, cover: { ...prev.cover, [key]: value } } : prev
    );
  };

  /** A number field's range problem, or null when empty/valid. Empty is the
   * library default for every numeric field. */
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

  /** Saves the editor as ONE call — create or update of the whole config
   * document; there are no partial field writes. */
  const savePreset = async (): Promise<void> => {
    if (!editing) return;
    setGeneralError(null);

    const name = editing.name.trim();

    if (!name) {
      setEditorError("Give the preset a name.");

      return;
    }

    const clash = presets.some(
      (p: DocumentPresetEntry): boolean =>
        p.id !== editing.id && p.name.toLowerCase() === name.toLowerCase()
    );

    if (clash) {
      setEditorError(`A preset named “${name}” already exists for PDF.`);

      return;
    }

    const margin =
      editing.pageMargin.trim() === "" ? 0 : Number(editing.pageMargin.trim());

    if (
      !Number.isFinite(margin) ||
      (margin !== 0 && (margin < MARGIN_MIN || margin > MARGIN_MAX))
    ) {
      setEditorError(
        `Page margin must be empty (library default) or between ${MARGIN_MIN} and ${MARGIN_MAX} inches.`
      );

      return;
    }

    const color = editing.watermarkColor.trim();

    if (color && !HEX_COLOR_RE.test(color)) {
      setEditorError("The watermark color must be hex, like #888888.");

      return;
    }

    const problem =
      rangeProblem(
        "Watermark opacity",
        editing.watermarkOpacity,
        0,
        WATERMARK_OPACITY_MAX
      ) ??
      rangeProblem(
        "Watermark angle",
        editing.watermarkAngle,
        WATERMARK_ANGLE_MIN,
        WATERMARK_ANGLE_MAX
      ) ??
      rangeProblem(
        "TOC minimum depth",
        editing.tocMinDepth,
        TOC_DEPTH_MIN,
        TOC_DEPTH_MAX
      ) ??
      rangeProblem(
        "TOC maximum depth",
        editing.tocMaxDepth,
        TOC_DEPTH_MIN,
        TOC_DEPTH_MAX
      );

    if (problem) {
      setEditorError(problem);

      return;
    }

    if (
      editing.tocMinDepth.trim() &&
      editing.tocMaxDepth.trim() &&
      Number(editing.tocMinDepth.trim()) > Number(editing.tocMaxDepth.trim())
    ) {
      setEditorError("The TOC's minimum depth cannot exceed its maximum.");

      return;
    }

    setEditorError(null);
    setSaving(true);

    try {
      const configJSON = buildConfigJSON(editing);

      if (editing.id) {
        await client.settings.updateDocumentPreset(
          editing.id,
          name,
          configJSON
        );
      } else {
        await client.settings.createDocumentPreset(format, name, configJSON);
      }

      setEditing(null);
      await refresh();
    } catch (err) {
      setEditorError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const armDuplicate = (entry: DocumentPresetEntry): void => {
    setGeneralError(null);
    setConfirming({ id: entry.id, action: "duplicate" });
    setDuplicateName(`${entry.name} copy`);
    setDuplicateError(null);
  };

  const runDuplicate = async (): Promise<void> => {
    if (!confirming) return;

    const entry = presets.find(
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
      presets.some(
        (p: DocumentPresetEntry): boolean =>
          p.name.toLowerCase() === newName.toLowerCase()
      )
    ) {
      setDuplicateError(`A preset named “${newName}” already exists for PDF.`);

      return;
    }

    try {
      await client.settings.duplicateDocumentPreset(entry.id, newName);
      setConfirming(null);
      await refresh();
    } catch (err) {
      setDuplicateError(err instanceof Error ? err.message : String(err));
    }
  };

  const removePreset = async (entry: DocumentPresetEntry): Promise<void> => {
    setGeneralError(null);
    setConfirming(null);

    try {
      await client.settings.deleteDocumentPreset(entry.id);

      if (editing && editing.id === entry.id) setEditing(null);
      await refresh();
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <ModalShell
      title={
        editing
          ? editing.id
            ? "Edit preset"
            : "New preset"
          : "Document presets"
      }
      description={
        editing
          ? "Saved as one document — no partial writes."
          : "Presets style the PDFs agents generate with create_document."
      }
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
              onClick={() => void savePreset()}
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

          <PresetEditor
            form={editing}
            onPatch={patch}
            onPatchCover={patchCover}
          />
        </div>
      ) : (
        <div className="space-y-4">
          <div
            className="flex gap-1 border-b border-line"
            role="tablist"
            aria-label="Document formats"
            data-testid="document-tabs"
          >
            {FORMATS.map((f: FormatTab) => (
              <button
                key={f.id}
                type="button"
                role="tab"
                aria-selected={format === f.id}
                disabled={!f.enabled}
                title={
                  f.enabled ? undefined : `${f.label} arrives with its engine`
                }
                data-testid={`document-tab-${f.id}`}
                onClick={() => {
                  if (f.enabled) setFormat(f.id);
                }}
                className={cn(
                  "-mb-px rounded-t-md border-b-2 px-3 py-1.5 text-[12px] font-medium transition-colors",
                  format === f.id
                    ? "border-accent text-fg"
                    : "border-transparent text-muted hover:text-fg",
                  !f.enabled && "cursor-not-allowed opacity-40 hover:text-muted"
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

          {presets.length > 0 ? (
            <div>
              <label
                className="mb-1.5 block text-[12px] font-medium text-fg2"
                htmlFor="document-default-preset"
              >
                Default preset
              </label>
              <Select
                id="document-default-preset"
                data-testid="document-default-preset"
                className="h-8 w-48 text-[13px]"
                value={defaultName}
                onChange={(e: ChangeEvent<HTMLSelectElement>) =>
                  void changeDefault(e.target.value)
                }
              >
                <option value="">None (library defaults)</option>
                {presets.map((p: DocumentPresetEntry) => (
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
              <Button
                variant="ghost"
                size="sm"
                data-testid="document-preset-new"
                onClick={() => openEditor()}
              >
                <Icon name="plus" size={13} /> New preset
              </Button>
            </div>
            <p className="mt-1 text-[11px] leading-4 text-muted">
              Theme, page setup, cover, footer, watermark, and table of contents
              for one PDF style.
            </p>
          </div>

          {presets.length === 0 ? (
            <p
              className="rounded-md border border-dashed border-line px-3 py-4 text-center text-[12px] text-muted"
              data-testid="document-presets-empty"
            >
              No presets yet — create_document uses the library defaults until
              you add one.
            </p>
          ) : (
            <div className="space-y-2">
              {presets.map((entry: DocumentPresetEntry) => {
                const confirmingRow =
                  confirming && confirming.id === entry.id
                    ? confirming.action
                    : null;

                const isDefault = entry.name === defaultName;

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
                          onClick={() => openEditor(entry)}
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
