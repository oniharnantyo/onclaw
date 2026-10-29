import { useRef } from "react";
import type { ChangeEvent, ReactElement, ReactNode } from "react";

import { Button } from "../../../components/ui/Button";
import { Input } from "../../../components/ui/Input";
import { Select } from "../../../components/ui/Select";
import { Toggle } from "../../../components/ui/Toggle";
import {
  type CoverForm,
  type DOCXForm,
  type PresetForm,
  DOCX_FONT_SIZE_MAX,
  DOCX_FONT_SIZE_MIN,
  DOCX_MARGIN_MAX,
  MARGIN_MAX,
  MARGIN_MIN,
  TOC_DEPTH_MAX,
  TOC_DEPTH_MIN,
  WATERMARK_ANGLE_MAX,
  WATERMARK_ANGLE_MIN,
  WATERMARK_OPACITY_MAX,
} from "./presetConfig";

/** The v1 theme list (internal/agents/tools/docgen/preset.go); the empty
 * theme is the library default. */
const THEMES = [
  "academic",
  "corporate",
  "creative",
  "default",
  "invoice",
  "legal",
  "manuscript",
  "technical",
];

/** Props for the grouped editor: the live form plus its two patch
 * callbacks, both owned by the dialog. */
interface PresetEditorProps {
  form: PresetForm;
  onPatch: (update: Partial<PresetForm>) => void;
  onPatchCover: (key: keyof CoverForm, value: string) => void;
}

/** One grouped concern block in the editor (theme, page setup, cover, …). */
function FieldGroup({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}): ReactElement {
  return (
    <div className="rounded-md border border-line p-3">
      <p className="mb-2 text-[12px] font-semibold text-fg2">{title}</p>
      {children}
    </div>
  );
}

/**
 * PresetEditor: the grouped form for one document preset — name, theme,
 * page setup, cover, footer, watermark, and table of contents. Sections
 * follow the v1 schema (design D5): page setup, cover, and TOC are
 * presence-toggled; the footer and watermark map only when they carry
 * content. Stateless — the dialog owns the form state and the save.
 */
export default function PresetEditor({
  form,
  onPatch,
  onPatchCover,
}: PresetEditorProps): ReactElement {
  return (
    <>
      <div>
        <label
          className="mb-1.5 block text-[12px] font-medium text-fg2"
          htmlFor="document-preset-name"
        >
          Name
        </label>
        <Input
          id="document-preset-name"
          placeholder="e.g. Client Report"
          className="h-8 max-w-64 text-[13px]"
          data-testid="document-preset-name"
          value={form.name}
          onChange={(e: ChangeEvent<HTMLInputElement>) =>
            onPatch({ name: e.target.value })
          }
        />
      </div>

      <FieldGroup title="Theme">
        <Select
          aria-label="Theme"
          data-testid="document-theme"
          className="h-8 w-48 text-[13px]"
          value={form.theme}
          onChange={(e: ChangeEvent<HTMLSelectElement>) =>
            onPatch({ theme: e.target.value })
          }
        >
          <option value="">Library default</option>
          {THEMES.map((theme: string) => (
            <option key={theme} value={theme}>
              {theme}
            </option>
          ))}
        </Select>
      </FieldGroup>

      <FieldGroup title="Page setup">
        <label className="mb-2 flex items-center gap-2 text-[12px] font-medium text-fg2">
          <Toggle
            on={form.pageOn}
            label="Custom page setup"
            testId="document-page-toggle"
            onChange={(next: boolean) => onPatch({ pageOn: next })}
          />
          Custom page setup
        </label>
        {form.pageOn ? (
          <div className="flex flex-wrap gap-3">
            <div>
              <label
                className="mb-1.5 block text-[12px] text-fg2"
                htmlFor="document-page-size"
              >
                Size
              </label>
              <Select
                id="document-page-size"
                data-testid="document-page-size"
                className="h-8 w-32 text-[13px]"
                value={form.pageSize}
                onChange={(e: ChangeEvent<HTMLSelectElement>) =>
                  onPatch({ pageSize: e.target.value })
                }
              >
                <option value="">Library default</option>
                <option value="letter">Letter</option>
                <option value="a4">A4</option>
                <option value="legal">Legal</option>
              </Select>
            </div>
            <div>
              <label
                className="mb-1.5 block text-[12px] text-fg2"
                htmlFor="document-page-orientation"
              >
                Orientation
              </label>
              <Select
                id="document-page-orientation"
                data-testid="document-page-orientation"
                className="h-8 w-32 text-[13px]"
                value={form.pageOrientation}
                onChange={(e: ChangeEvent<HTMLSelectElement>) =>
                  onPatch({ pageOrientation: e.target.value })
                }
              >
                <option value="">Library default</option>
                <option value="portrait">Portrait</option>
                <option value="landscape">Landscape</option>
              </Select>
            </div>
            <div>
              <label
                className="mb-1.5 block text-[12px] text-fg2"
                htmlFor="document-page-margin"
              >
                Margin (inches)
              </label>
              <Input
                id="document-page-margin"
                type="number"
                min={MARGIN_MIN}
                max={MARGIN_MAX}
                step={0.25}
                className="h-8 w-32 text-[13px]"
                data-testid="document-page-margin"
                value={form.pageMargin}
                onChange={(e: ChangeEvent<HTMLInputElement>) =>
                  onPatch({ pageMargin: e.target.value })
                }
              />
              <p className="mt-1 text-[11px] leading-4 text-muted">
                Empty = library default (0.5).
              </p>
            </div>
          </div>
        ) : (
          <p className="text-[11px] leading-4 text-muted">
            Off — the library's page defaults apply.
          </p>
        )}
      </FieldGroup>

      <FieldGroup title="Cover page">
        <label className="mb-2 flex items-center gap-2 text-[12px] font-medium text-fg2">
          <Toggle
            on={form.coverOn}
            label="Cover page"
            testId="document-cover-toggle"
            onChange={(next: boolean) => onPatch({ coverOn: next })}
          />
          Cover page
        </label>
        {form.coverOn ? (
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            <div className="sm:col-span-2">
              <Input
                aria-label="Cover title"
                placeholder="Title"
                className="h-8 text-[13px]"
                data-testid="document-cover-title"
                value={form.cover.title}
                onChange={(e: ChangeEvent<HTMLInputElement>) =>
                  onPatchCover("title", e.target.value)
                }
              />
            </div>
            <Input
              aria-label="Cover subtitle"
              placeholder="Subtitle"
              className="h-8 text-[13px]"
              data-testid="document-cover-subtitle"
              value={form.cover.subtitle}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatchCover("subtitle", e.target.value)
              }
            />
            <Input
              aria-label="Cover author"
              placeholder="Author"
              className="h-8 text-[13px]"
              data-testid="document-cover-author"
              value={form.cover.author}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatchCover("author", e.target.value)
              }
            />
            <Input
              aria-label="Cover author title"
              placeholder="Author title"
              className="h-8 text-[13px]"
              data-testid="document-cover-author-title"
              value={form.cover.authorTitle}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatchCover("authorTitle", e.target.value)
              }
            />
            <Input
              aria-label="Cover organization"
              placeholder="Organization"
              className="h-8 text-[13px]"
              data-testid="document-cover-organization"
              value={form.cover.organization}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatchCover("organization", e.target.value)
              }
            />
            <Input
              aria-label="Cover date"
              placeholder="Date"
              className="h-8 text-[13px]"
              data-testid="document-cover-date"
              value={form.cover.date}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatchCover("date", e.target.value)
              }
            />
            <Input
              aria-label="Cover version"
              placeholder="Version"
              className="h-8 text-[13px]"
              data-testid="document-cover-version"
              value={form.cover.version}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatchCover("version", e.target.value)
              }
            />
          </div>
        ) : (
          <p className="text-[11px] leading-4 text-muted">
            Off — no cover page.
          </p>
        )}
      </FieldGroup>

      <FieldGroup title="Footer">
        <div className="flex flex-wrap items-end gap-3">
          <div>
            <label
              className="mb-1.5 block text-[12px] text-fg2"
              htmlFor="document-footer-position"
            >
              Position
            </label>
            <Select
              id="document-footer-position"
              data-testid="document-footer-position"
              className="h-8 w-36 text-[13px]"
              value={form.footerPosition}
              onChange={(e: ChangeEvent<HTMLSelectElement>) =>
                onPatch({ footerPosition: e.target.value })
              }
            >
              <option value="">Library default</option>
              <option value="left">Left</option>
              <option value="center">Center</option>
              <option value="right">Right</option>
            </Select>
          </div>
          <label className="flex items-center gap-2 pb-1.5 text-[12px] font-medium text-fg2">
            <Toggle
              on={form.footerPageNumber}
              label="Show page number"
              testId="document-footer-page-number"
              onChange={(next: boolean) => onPatch({ footerPageNumber: next })}
            />
            Show page number
          </label>
          <div className="min-w-0 flex-1">
            <Input
              aria-label="Footer text"
              placeholder="Footer text"
              className="h-8 text-[13px]"
              data-testid="document-footer-text"
              value={form.footerText}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatch({ footerText: e.target.value })
              }
            />
          </div>
        </div>
        <p className="mt-1.5 text-[11px] leading-4 text-muted">
          Included when a page number or footer text is set.
        </p>
      </FieldGroup>

      <FieldGroup title="Watermark">
        <div className="flex flex-wrap items-end gap-3">
          <Input
            aria-label="Watermark text"
            placeholder="Text, e.g. DRAFT"
            className="h-8 w-40 text-[13px]"
            data-testid="document-watermark-text"
            value={form.watermarkText}
            onChange={(e: ChangeEvent<HTMLInputElement>) =>
              onPatch({ watermarkText: e.target.value })
            }
          />
          <Input
            aria-label="Watermark color"
            placeholder="#888888"
            className="h-8 w-28 font-mono text-[13px]"
            data-testid="document-watermark-color"
            value={form.watermarkColor}
            onChange={(e: ChangeEvent<HTMLInputElement>) =>
              onPatch({ watermarkColor: e.target.value })
            }
          />
          <div>
            <Input
              aria-label="Watermark opacity"
              type="number"
              min={0}
              max={WATERMARK_OPACITY_MAX}
              step={0.1}
              className="h-8 w-24 text-[13px]"
              data-testid="document-watermark-opacity"
              value={form.watermarkOpacity}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatch({ watermarkOpacity: e.target.value })
              }
            />
            <p className="mt-1 text-[11px] leading-4 text-muted">
              Opacity 0–1.
            </p>
          </div>
          <div>
            <Input
              aria-label="Watermark angle"
              type="number"
              min={WATERMARK_ANGLE_MIN}
              max={WATERMARK_ANGLE_MAX}
              className="h-8 w-24 text-[13px]"
              data-testid="document-watermark-angle"
              value={form.watermarkAngle}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatch({ watermarkAngle: e.target.value })
              }
            />
            <p className="mt-1 text-[11px] leading-4 text-muted">
              Angle −90–90.
            </p>
          </div>
        </div>
        <p className="mt-1.5 text-[11px] leading-4 text-muted">
          Included when the watermark text is set.
        </p>
      </FieldGroup>

      <FieldGroup title="Table of contents">
        <label className="mb-2 flex items-center gap-2 text-[12px] font-medium text-fg2">
          <Toggle
            on={form.tocOn}
            label="Table of contents"
            testId="document-toc-toggle"
            onChange={(next: boolean) => onPatch({ tocOn: next })}
          />
          Table of contents
        </label>
        {form.tocOn ? (
          <div className="flex flex-wrap items-end gap-3">
            <div className="min-w-0 flex-1">
              <Input
                aria-label="TOC title"
                placeholder="Title (empty = none)"
                className="h-8 text-[13px]"
                data-testid="document-toc-title"
                value={form.tocTitle}
                onChange={(e: ChangeEvent<HTMLInputElement>) =>
                  onPatch({ tocTitle: e.target.value })
                }
              />
            </div>
            <div>
              <Input
                aria-label="TOC minimum depth"
                type="number"
                min={TOC_DEPTH_MIN}
                max={TOC_DEPTH_MAX}
                className="h-8 w-24 text-[13px]"
                data-testid="document-toc-min"
                value={form.tocMinDepth}
                onChange={(e: ChangeEvent<HTMLInputElement>) =>
                  onPatch({ tocMinDepth: e.target.value })
                }
              />
              <p className="mt-1 text-[11px] leading-4 text-muted">
                Min depth 1–6.
              </p>
            </div>
            <div>
              <Input
                aria-label="TOC maximum depth"
                type="number"
                min={TOC_DEPTH_MIN}
                max={TOC_DEPTH_MAX}
                className="h-8 w-24 text-[13px]"
                data-testid="document-toc-max"
                value={form.tocMaxDepth}
                onChange={(e: ChangeEvent<HTMLInputElement>) =>
                  onPatch({ tocMaxDepth: e.target.value })
                }
              />
              <p className="mt-1 text-[11px] leading-4 text-muted">
                Max depth 1–6.
              </p>
            </div>
          </div>
        ) : (
          <p className="text-[11px] leading-4 text-muted">
            Off — no table of contents.
          </p>
        )}
      </FieldGroup>
    </>
  );
}

/** Props for the DOCX config editor: the live form plus its patch callback,
 * owned by the dialog. */
interface DOCXPresetEditorProps {
  form: DOCXForm;
  onPatch: (update: Partial<DOCXForm>) => void;
}

/**
 * DOCXPresetEditor: the grouped form for one DOCX config preset, its fields
 * following the backend's DOCXPreset schema (docgen preset.go) — fonts,
 * heading style, page setup, footer. Every empty field means the built-in
 * default template's value; there are no presence toggles (the schema maps
 * per-field). Stateless — the dialog owns the form state and the save.
 */
export function DOCXPresetEditor({
  form,
  onPatch,
}: DOCXPresetEditorProps): ReactElement {
  return (
    <>
      <div>
        <label
          className="mb-1.5 block text-[12px] font-medium text-fg2"
          htmlFor="document-preset-name"
        >
          Name
        </label>
        <Input
          id="document-preset-name"
          placeholder="e.g. Client Report"
          className="h-8 max-w-64 text-[13px]"
          data-testid="document-preset-name"
          value={form.name}
          onChange={(e: ChangeEvent<HTMLInputElement>) =>
            onPatch({ name: e.target.value })
          }
        />
      </div>

      <FieldGroup title="Fonts">
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
          <div>
            <label
              className="mb-1.5 block text-[12px] text-fg2"
              htmlFor="document-docx-body-family"
            >
              Body font
            </label>
            <Input
              id="document-docx-body-family"
              placeholder="e.g. Aptos"
              className="h-8 text-[13px]"
              data-testid="document-docx-body-family"
              value={form.fontsBodyFamily}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatch({ fontsBodyFamily: e.target.value })
              }
            />
          </div>
          <div>
            <label
              className="mb-1.5 block text-[12px] text-fg2"
              htmlFor="document-docx-body-size"
            >
              Body size (points)
            </label>
            <Input
              id="document-docx-body-size"
              type="number"
              min={DOCX_FONT_SIZE_MIN}
              max={DOCX_FONT_SIZE_MAX}
              step={0.5}
              className="h-8 text-[13px]"
              data-testid="document-docx-body-size"
              value={form.fontsBodySize}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatch({ fontsBodySize: e.target.value })
              }
            />
          </div>
          <div>
            <label
              className="mb-1.5 block text-[12px] text-fg2"
              htmlFor="document-docx-heading-family"
            >
              Heading font
            </label>
            <Input
              id="document-docx-heading-family"
              placeholder="e.g. Aptos Display"
              className="h-8 text-[13px]"
              data-testid="document-docx-heading-family"
              value={form.fontsHeadingFamily}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatch({ fontsHeadingFamily: e.target.value })
              }
            />
          </div>
          <div>
            <label
              className="mb-1.5 block text-[12px] text-fg2"
              htmlFor="document-docx-heading-size"
            >
              Heading size (points)
            </label>
            <Input
              id="document-docx-heading-size"
              type="number"
              min={DOCX_FONT_SIZE_MIN}
              max={DOCX_FONT_SIZE_MAX}
              step={0.5}
              className="h-8 text-[13px]"
              data-testid="document-docx-heading-size"
              value={form.fontsHeadingSize}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatch({ fontsHeadingSize: e.target.value })
              }
            />
            <p className="mt-1 text-[11px] leading-4 text-muted">
              The Heading 1 size; empty fields use the built-in template.
            </p>
          </div>
        </div>
      </FieldGroup>

      <FieldGroup title="Heading style">
        <div className="flex flex-wrap items-end gap-3">
          <div>
            <label
              className="mb-1.5 block text-[12px] text-fg2"
              htmlFor="document-docx-heading-color"
            >
              Heading color
            </label>
            <Input
              id="document-docx-heading-color"
              placeholder="#1F3864"
              className="h-8 w-28 font-mono text-[13px]"
              data-testid="document-docx-heading-color"
              value={form.headingColor}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatch({ headingColor: e.target.value })
              }
            />
          </div>
          <div>
            <label
              className="mb-1.5 block text-[12px] text-fg2"
              htmlFor="document-docx-heading-weight"
            >
              Heading weight
            </label>
            <Select
              id="document-docx-heading-weight"
              data-testid="document-docx-heading-weight"
              className="h-8 w-36 text-[13px]"
              value={form.headingBold}
              onChange={(e: ChangeEvent<HTMLSelectElement>) =>
                onPatch({ headingBold: e.target.value })
              }
            >
              <option value="">Built-in (bold)</option>
              <option value="bold">Bold</option>
              <option value="regular">Regular</option>
            </Select>
          </div>
        </div>
      </FieldGroup>

      <FieldGroup title="Page setup">
        <div className="flex flex-wrap gap-3">
          <div>
            <label
              className="mb-1.5 block text-[12px] text-fg2"
              htmlFor="document-docx-page-size"
            >
              Size
            </label>
            <Select
              id="document-docx-page-size"
              data-testid="document-docx-page-size"
              className="h-8 w-32 text-[13px]"
              value={form.pageSize}
              onChange={(e: ChangeEvent<HTMLSelectElement>) =>
                onPatch({ pageSize: e.target.value })
              }
            >
              <option value="">Built-in default</option>
              <option value="letter">Letter</option>
              <option value="a4">A4</option>
              <option value="legal">Legal</option>
            </Select>
          </div>
          <div>
            <label
              className="mb-1.5 block text-[12px] text-fg2"
              htmlFor="document-docx-page-orientation"
            >
              Orientation
            </label>
            <Select
              id="document-docx-page-orientation"
              data-testid="document-docx-page-orientation"
              className="h-8 w-32 text-[13px]"
              value={form.pageOrientation}
              onChange={(e: ChangeEvent<HTMLSelectElement>) =>
                onPatch({ pageOrientation: e.target.value })
              }
            >
              <option value="">Built-in default</option>
              <option value="portrait">Portrait</option>
              <option value="landscape">Landscape</option>
            </Select>
          </div>
          <div>
            <label
              className="mb-1.5 block text-[12px] text-fg2"
              htmlFor="document-docx-page-margin"
            >
              Margin (inches)
            </label>
            <Input
              id="document-docx-page-margin"
              type="number"
              min={0}
              max={DOCX_MARGIN_MAX}
              step={0.25}
              className="h-8 w-32 text-[13px]"
              data-testid="document-docx-page-margin"
              value={form.pageMargin}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatch({ pageMargin: e.target.value })
              }
            />
            <p className="mt-1 text-[11px] leading-4 text-muted">
              Empty = built-in default (1); one margin for all sides.
            </p>
          </div>
        </div>
      </FieldGroup>

      <FieldGroup title="Footer">
        <div className="flex flex-wrap items-end gap-3">
          <label className="flex items-center gap-2 pb-1.5 text-[12px] font-medium text-fg2">
            <Toggle
              on={form.footerPageNumber}
              label="Show page number"
              testId="document-docx-footer-page-number"
              onChange={(next: boolean) => onPatch({ footerPageNumber: next })}
            />
            Show page number
          </label>
          <div className="min-w-0 flex-1">
            <Input
              aria-label="Footer text"
              placeholder="Footer text"
              className="h-8 text-[13px]"
              data-testid="document-docx-footer-text"
              value={form.footerText}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                onPatch({ footerText: e.target.value })
              }
            />
          </div>
        </div>
        <p className="mt-1.5 text-[11px] leading-4 text-muted">
          Included when a page number or footer text is set.
        </p>
      </FieldGroup>
    </>
  );
}

/** Props for the template-preset editor: the dialog owns the name, the
 * pending file, and the save; the editor renders the fields. */
export interface TemplatePresetEditorProps {
  /** The preset name and its setter (the dialog owns the state). */
  name: string;
  onName: (name: string) => void;
  /** The accepted skeleton extension, ".docx" or ".pptx". */
  accept: string;
  /** The stored skeleton's name; "" while the preset has none. */
  templateName: string;
  /** The pending upload, or null. */
  file: File | null;
  /** Receives the picked file (null re-picks nothing). */
  onFile: (file: File | null) => void;
}

/**
 * TemplatePresetEditor: the name field plus the skeleton upload for one
 * template-document preset (DOCX or PPTX). The upload rides the hidden
 * file input (the UploadModal pattern); the existing template's name shows
 * as the current state. Stateless — the dialog owns the state and the save.
 */
export function TemplatePresetEditor({
  name,
  onName,
  accept,
  templateName,
  file,
  onFile,
}: TemplatePresetEditorProps): ReactElement {
  const fileInputRef = useRef<HTMLInputElement>(null);

  return (
    <>
      <div>
        <label
          className="mb-1.5 block text-[12px] font-medium text-fg2"
          htmlFor="document-preset-name"
        >
          Name
        </label>
        <Input
          id="document-preset-name"
          placeholder="e.g. Brand Deck"
          className="h-8 max-w-64 text-[13px]"
          data-testid="document-preset-name"
          value={name}
          onChange={(e: ChangeEvent<HTMLInputElement>) =>
            onName(e.target.value)
          }
        />
      </div>
      <FieldGroup title="Template document">
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            data-testid="document-template-choose"
            onClick={() => fileInputRef.current?.click()}
          >
            Choose file…
          </Button>
          <span
            className="min-w-0 flex-1 truncate text-[13px] text-fg2"
            data-testid="document-template-filename"
          >
            {file
              ? `${file.name} · ${file.size} bytes`
              : templateName
                ? `Current template: ${templateName}`
                : "No file chosen"}
          </span>
        </div>
        <input
          ref={fileInputRef}
          type="file"
          accept={accept}
          className="hidden"
          data-testid="document-template-file"
          onChange={(e: ChangeEvent<HTMLInputElement>) => {
            onFile(e.target.files?.[0] ?? null);
            e.target.value = "";
          }}
        />
        <p className="mt-1.5 text-[11px] leading-4 text-muted">
          The uploaded skeleton's styles and layouts control the generated
          document's look.
        </p>
      </FieldGroup>
    </>
  );
}
