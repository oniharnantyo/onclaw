import {
  isNumber,
  isObject,
  isString,
  type Json,
} from "../../chat/tools/parse";

/** The preset config JSON's schema_version stamp (add-create-document D5,
 * carried into the DOCX/PPTX schemas); every save re-stamps it. */
const SCHEMA_VERSION = 1;

/** The preset flavors a DOCX or PPTX preset may carry (docgen's
 * PresetFlavorConfig / PresetFlavorTemplate): a config preset parametrizes
 * the built-in default template; a template preset references a
 * user-uploaded skeleton. PDF presets carry no flavor (the v1 schema). */
export const FLAVOR_CONFIG = "config";

export const FLAVOR_TEMPLATE = "template";

/** DOCX config bounds, from docgen's validateDOCXPreset — the form fails
 * typos here before the round trip. */
export const DOCX_FONT_SIZE_MIN = 6;

export const DOCX_FONT_SIZE_MAX = 72;

export const DOCX_MARGIN_MAX = 4;

/** The DOCX heading color must be six hex digits with an optional # (docgen
 * isHexColor). */
export const DOCX_COLOR_RE = /^#?[0-9a-fA-F]{6}$/;

/** The file extension a format's template upload must carry (the service
 * validates the same rule — .docx for DOCX, .pptx for PPTX). */
export function templateExtension(format: string): string {
  return format === "docx" ? ".docx" : ".pptx";
}

/** Backend save-time bounds, from picoloom's validators via docgen's
 * validatePreset — the form fails typos here before the round trip. */
export const MARGIN_MIN = 0.25;

export const MARGIN_MAX = 3;

export const WATERMARK_OPACITY_MAX = 1;

export const WATERMARK_ANGLE_MIN = -90;

export const WATERMARK_ANGLE_MAX = 90;

export const TOC_DEPTH_MIN = 1;

export const TOC_DEPTH_MAX = 6;

/** The watermark color must be hex when set (picoloom isValidHexColor). */
export const HEX_COLOR_RE = /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/;

/** The v1 page setup section (docgen's PageSetup JSON). */
export interface PresetPageSetup {
  size?: string;
  orientation?: string;
  margin_inches?: number;
}

/** The v1 cover section (docgen's Cover JSON). */
export interface PresetCover {
  title?: string;
  subtitle?: string;
  author?: string;
  author_title?: string;
  organization?: string;
  date?: string;
  version?: string;
}

/** The v1 footer section (docgen's FooterStyle JSON). */
export interface PresetFooterStyle {
  position?: string;
  show_page_number?: boolean;
  text?: string;
}

/** The v1 watermark section (docgen's Watermark JSON). */
export interface PresetWatermark {
  text?: string;
  color?: string;
  opacity?: number;
  angle?: number;
}

/** The v1 table-of-contents section (docgen's TOCSettings JSON). */
export interface PresetTOCSettings {
  title?: string;
  min_depth?: number;
  max_depth?: number;
}

/** One v1 preset config document (internal/agents/tools/docgen/preset.go's
 * JSON); absent properties mean the library default. */
export interface PresetConfig {
  schema_version?: number;
  theme?: string;
  page?: PresetPageSetup;
  cover?: PresetCover;
  footer?: PresetFooterStyle;
  watermark?: PresetWatermark;
  toc?: PresetTOCSettings;
}

/** The editor's cover fields, keyed by their config JSON property. */
export interface CoverForm {
  title: string;
  subtitle: string;
  author: string;
  authorTitle: string;
  organization: string;
  date: string;
  version: string;
}

/** The editor's form state. Numeric fields stay text until save (the
 * BrowserConfigDialog pattern) so typos surface as readable errors. */
export interface PresetForm {
  /** The preset's id; "" while creating. */
  id: string;
  name: string;
  theme: string;
  /** Presence toggles: the object maps only when its toggle is on. */
  pageOn: boolean;
  pageSize: string;
  pageOrientation: string;
  pageMargin: string;
  coverOn: boolean;
  cover: CoverForm;
  /** Footer and watermark are content-toggled: they map only when they
   * carry a page number or text. */
  footerPosition: string;
  footerPageNumber: boolean;
  footerText: string;
  watermarkText: string;
  watermarkColor: string;
  watermarkOpacity: string;
  watermarkAngle: string;
  tocOn: boolean;
  tocTitle: string;
  tocMinDepth: string;
  tocMaxDepth: string;
}

/** One decoded preset: its form state plus a drift warning when the stored
 * config could not be read or carries a foreign schema version. */
export interface ParsedPreset {
  form: PresetForm;
  warn: string | null;
}

/** A fresh form: every feature off, every field at the library default. */
export function emptyForm(): PresetForm {
  return {
    id: "",
    name: "",
    theme: "",
    pageOn: false,
    pageSize: "",
    pageOrientation: "",
    pageMargin: "",
    coverOn: false,
    cover: {
      title: "",
      subtitle: "",
      author: "",
      authorTitle: "",
      organization: "",
      date: "",
      version: "",
    },
    footerPosition: "",
    footerPageNumber: false,
    footerText: "",
    watermarkText: "",
    watermarkColor: "",
    watermarkOpacity: "",
    watermarkAngle: "",
    tocOn: false,
    tocTitle: "",
    tocMinDepth: "",
    tocMaxDepth: "",
  };
}

/** A section's string field; absent or wrong-typed reads as empty. */
function readString(value: Json | undefined): string {
  return isString(value) ? value : "";
}

/** A section's numeric field as decimal text; absent, zero, or wrong-typed
 * reads as empty (the library default). */
function readDecimal(value: number | undefined): string {
  return value !== undefined && value !== 0 ? String(value) : "";
}

/** Decodes a stored config JSON into the schema type; null when the text is
 * not JSON or not an object. Wrong-typed fields drop to absent — the form
 * reads them as the library default. */
export function decodeConfig(configJSON: string): PresetConfig | null {
  let raw: Json;

  try {
    // SAFETY: the try/catch bounds the parse; the isObject guard establishes
    // the object contract before any field is read.
    raw = JSON.parse(configJSON) as Json;
  } catch {
    return null;
  }

  if (!isObject(raw)) return null;

  const config: PresetConfig = {};

  if (isNumber(raw.schema_version)) config.schema_version = raw.schema_version;

  if (isString(raw.theme)) config.theme = raw.theme;

  if (isObject(raw.page)) {
    config.page = {
      size: readString(raw.page.size),
      orientation: readString(raw.page.orientation),
      margin_inches: isNumber(raw.page.margin_inches)
        ? raw.page.margin_inches
        : undefined,
    };
  }

  if (isObject(raw.cover)) {
    config.cover = {
      title: readString(raw.cover.title),
      subtitle: readString(raw.cover.subtitle),
      author: readString(raw.cover.author),
      author_title: readString(raw.cover.author_title),
      organization: readString(raw.cover.organization),
      date: readString(raw.cover.date),
      version: readString(raw.cover.version),
    };
  }

  if (isObject(raw.footer)) {
    config.footer = {
      position: readString(raw.footer.position),
      show_page_number: raw.footer.show_page_number === true,
      text: readString(raw.footer.text),
    };
  }

  if (isObject(raw.watermark)) {
    config.watermark = {
      text: readString(raw.watermark.text),
      color: readString(raw.watermark.color),
      opacity: isNumber(raw.watermark.opacity)
        ? raw.watermark.opacity
        : undefined,
      angle: isNumber(raw.watermark.angle) ? raw.watermark.angle : undefined,
    };
  }

  if (isObject(raw.toc)) {
    config.toc = {
      title: readString(raw.toc.title),
      min_depth: isNumber(raw.toc.min_depth) ? raw.toc.min_depth : undefined,
      max_depth: isNumber(raw.toc.max_depth) ? raw.toc.max_depth : undefined,
    };
  }

  return config;
}

/** Decodes a stored config JSON into form state. A config that fails to
 * parse, or carries an unknown schema version, still opens — the editor
 * flags it and the next save rewrites it (design D5's drift plan). */
export function parsePresetConfig(configJSON: string): ParsedPreset {
  const form = emptyForm();
  const config = decodeConfig(configJSON);

  if (config === null) {
    return {
      form,
      warn: "The preset's stored settings could not be read; saving replaces them.",
    };
  }

  let warn: string | null = null;

  const version = config.schema_version ?? SCHEMA_VERSION;

  if (version !== SCHEMA_VERSION) {
    warn = `This preset uses schema version ${version}; saving upgrades it to version ${SCHEMA_VERSION}.`;
  }

  form.theme = config.theme ?? "";

  if (config.page) {
    form.pageOn = true;
    form.pageSize = readString(config.page.size);
    form.pageOrientation = readString(config.page.orientation);
    form.pageMargin = readDecimal(config.page.margin_inches);
  }

  if (config.cover) {
    form.coverOn = true;
    form.cover = {
      title: readString(config.cover.title),
      subtitle: readString(config.cover.subtitle),
      author: readString(config.cover.author),
      authorTitle: readString(config.cover.author_title),
      organization: readString(config.cover.organization),
      date: readString(config.cover.date),
      version: readString(config.cover.version),
    };
  }

  if (config.footer) {
    form.footerPosition = readString(config.footer.position);
    form.footerPageNumber = config.footer.show_page_number === true;
    form.footerText = readString(config.footer.text);
  }

  if (config.watermark) {
    form.watermarkText = readString(config.watermark.text);
    form.watermarkColor = readString(config.watermark.color);
    form.watermarkOpacity = readDecimal(config.watermark.opacity);
    form.watermarkAngle = readDecimal(config.watermark.angle);
  }

  if (config.toc) {
    form.tocOn = true;
    form.tocTitle = readString(config.toc.title);
    form.tocMinDepth = readDecimal(config.toc.min_depth);
    form.tocMaxDepth = readDecimal(config.toc.max_depth);
  }

  return { form, warn };
}

/** Serializes the form into the v1 preset config JSON. Section presence
 * follows the schema's semantics: page/cover/toc ride their toggles, the
 * footer maps only with a page number or text, the watermark only with
 * text. Callers validate numeric fields first. */
export function buildConfigJSON(form: PresetForm): string {
  const payload: PresetConfig = { schema_version: SCHEMA_VERSION };

  if (form.theme) payload.theme = form.theme;

  if (form.pageOn) {
    const page: PresetPageSetup = {};

    if (form.pageSize) page.size = form.pageSize;

    if (form.pageOrientation) page.orientation = form.pageOrientation;

    if (form.pageMargin.trim())
      page.margin_inches = Number(form.pageMargin.trim());
    payload.page = page;
  }

  if (form.coverOn) {
    const cover: PresetCover = {};

    if (form.cover.title.trim()) cover.title = form.cover.title.trim();

    if (form.cover.subtitle.trim()) cover.subtitle = form.cover.subtitle.trim();

    if (form.cover.author.trim()) cover.author = form.cover.author.trim();

    if (form.cover.authorTitle.trim()) {
      cover.author_title = form.cover.authorTitle.trim();
    }

    if (form.cover.organization.trim()) {
      cover.organization = form.cover.organization.trim();
    }

    if (form.cover.date.trim()) cover.date = form.cover.date.trim();

    if (form.cover.version.trim()) cover.version = form.cover.version.trim();
    payload.cover = cover;
  }

  if (form.footerPageNumber || form.footerText.trim()) {
    const footer: PresetFooterStyle = {};

    if (form.footerPosition) footer.position = form.footerPosition;

    if (form.footerPageNumber) footer.show_page_number = true;

    if (form.footerText.trim()) footer.text = form.footerText.trim();
    payload.footer = footer;
  }

  if (form.watermarkText.trim()) {
    const watermark: PresetWatermark = {
      text: form.watermarkText.trim(),
    };

    if (form.watermarkColor.trim())
      watermark.color = form.watermarkColor.trim();

    if (form.watermarkOpacity.trim())
      watermark.opacity = Number(form.watermarkOpacity.trim());

    if (form.watermarkAngle.trim())
      watermark.angle = Number(form.watermarkAngle.trim());
    payload.watermark = watermark;
  }

  if (form.tocOn) {
    const toc: PresetTOCSettings = {};

    if (form.tocTitle.trim()) toc.title = form.tocTitle.trim();

    if (form.tocMinDepth.trim())
      toc.min_depth = Number(form.tocMinDepth.trim());

    if (form.tocMaxDepth.trim())
      toc.max_depth = Number(form.tocMaxDepth.trim());
    payload.toc = toc;
  }

  return JSON.stringify(payload);
}

/** The DOCX config preset's font selection (docgen's DOCXFonts JSON): the
 * body and heading families and sizes, in points. */
export interface DOCXFontsConfig {
  body_family?: string;
  body_size?: number;
  heading_family?: string;
  heading_size?: number;
}

/** The DOCX config preset's heading treatment (docgen's DOCXHeadingStyle
 * JSON): color and weight; bold absent keeps the built-in template's bold
 * headings. */
export interface DOCXHeadingConfig {
  color?: string;
  bold?: boolean;
}

/** The DOCX config preset's page geometry (docgen's DOCXPageSetup JSON);
 * the size vocabulary is the PDF presets'. */
export interface DOCXPageConfig {
  size?: string;
  orientation?: string;
  margin_inches?: number;
}

/** The DOCX config preset's footer (docgen's DOCXFooterStyle JSON). */
export interface DOCXFooterConfig {
  text?: string;
  show_page_number?: boolean;
}

/** One DOCX preset config document (docgen's DOCXPreset JSON): one of the
 * two flavors; template presets carry only the has-template metadata. */
export interface DOCXConfig {
  schema_version?: number;
  flavor?: string;
  has_template?: boolean;
  template_name?: string;
  fonts?: DOCXFontsConfig;
  heading?: DOCXHeadingConfig;
  page?: DOCXPageConfig;
  footer?: DOCXFooterConfig;
}

/** One PPTX preset config document (docgen's PPTXPreset JSON): template
 * flavor only — the uploaded skeleton is the whole preset. */
export interface PPTXConfig {
  schema_version?: number;
  flavor?: string;
  has_template?: boolean;
  template_name?: string;
}

/** The DOCX config editor's form state. Numeric fields stay text until save
 * (the PresetForm pattern); every empty field means the built-in default
 * template's value. */
export interface DOCXForm {
  /** The preset's id; "" while creating. */
  id: string;
  name: string;
  fontsBodyFamily: string;
  fontsBodySize: string;
  fontsHeadingFamily: string;
  fontsHeadingSize: string;
  headingColor: string;
  /** "" keeps the built-in bold; "bold"/"regular" map to true/false. */
  headingBold: string;
  pageSize: string;
  pageOrientation: string;
  pageMargin: string;
  footerText: string;
  footerPageNumber: boolean;
}

/** A fresh DOCX config form: every field at the built-in default. */
export function emptyDOCXForm(): DOCXForm {
  return {
    id: "",
    name: "",
    fontsBodyFamily: "",
    fontsBodySize: "",
    fontsHeadingFamily: "",
    fontsHeadingSize: "",
    headingColor: "",
    headingBold: "",
    pageSize: "",
    pageOrientation: "",
    pageMargin: "",
    footerText: "",
    footerPageNumber: false,
  };
}

/** Leniently reads a stored config's flavor; "" when unreadable (a PDF
 * preset's v1 config, or a corrupt document). */
export function presetFlavor(configJSON: string): string {
  try {
    // SAFETY: the try/catch bounds the parse; only the flavor field is read
    // and a wrong-typed flavor reads as absent.
    const raw = JSON.parse(configJSON) as { flavor?: Json };

    return isString(raw.flavor) ? raw.flavor : "";
  } catch {
    return "";
  }
}

/** Decodes a stored DOCX preset config into the schema type; null when the
 * text is not JSON or not an object. Wrong-typed fields drop to absent. */
export function decodeDOCXConfig(configJSON: string): DOCXConfig | null {
  let raw: Json;

  try {
    // SAFETY: the try/catch bounds the parse; the isObject guard establishes
    // the object contract before any field is read.
    raw = JSON.parse(configJSON) as Json;
  } catch {
    return null;
  }

  if (!isObject(raw)) return null;

  const config: DOCXConfig = {};

  if (isNumber(raw.schema_version)) config.schema_version = raw.schema_version;

  if (isString(raw.flavor)) config.flavor = raw.flavor;

  if (raw.has_template === true) config.has_template = true;

  if (isString(raw.template_name)) config.template_name = raw.template_name;

  if (isObject(raw.fonts)) {
    config.fonts = {
      body_family: readString(raw.fonts.body_family),
      body_size: isNumber(raw.fonts.body_size)
        ? raw.fonts.body_size
        : undefined,
      heading_family: readString(raw.fonts.heading_family),
      heading_size: isNumber(raw.fonts.heading_size)
        ? raw.fonts.heading_size
        : undefined,
    };
  }

  if (isObject(raw.heading)) {
    config.heading = {
      color: readString(raw.heading.color),
      bold:
        raw.heading.bold === true || raw.heading.bold === false
          ? raw.heading.bold
          : undefined,
    };
  }

  if (isObject(raw.page)) {
    config.page = {
      size: readString(raw.page.size),
      orientation: readString(raw.page.orientation),
      margin_inches: isNumber(raw.page.margin_inches)
        ? raw.page.margin_inches
        : undefined,
    };
  }

  if (isObject(raw.footer)) {
    config.footer = {
      text: readString(raw.footer.text),
      show_page_number: raw.footer.show_page_number === true,
    };
  }

  return config;
}

/** A DOCX config parse result: the form plus its drift warning. */
export interface ParsedDOCX {
  form: DOCXForm;
  warn: string | null;
}

/** Decodes a stored DOCX config into form state, mirroring parsePresetConfig's
 * drift contract: unreadable or foreign-version configs still open, flagged. */
export function parseDOCXConfig(configJSON: string): ParsedDOCX {
  const form = emptyDOCXForm();
  const config = decodeDOCXConfig(configJSON);

  if (config === null) {
    return {
      form,
      warn: "The preset's stored settings could not be read; saving replaces them.",
    };
  }

  let warn: string | null = null;

  const version = config.schema_version ?? SCHEMA_VERSION;

  if (version !== SCHEMA_VERSION) {
    warn = `This preset uses schema version ${version}; saving upgrades it to version ${SCHEMA_VERSION}.`;
  }

  if (config.fonts) {
    form.fontsBodyFamily = readString(config.fonts.body_family);
    form.fontsBodySize = readDecimal(config.fonts.body_size);
    form.fontsHeadingFamily = readString(config.fonts.heading_family);
    form.fontsHeadingSize = readDecimal(config.fonts.heading_size);
  }

  if (config.heading) {
    form.headingColor = readString(config.heading.color);
    form.headingBold =
      config.heading.bold === true
        ? "bold"
        : config.heading.bold === false
          ? "regular"
          : "";
  }

  if (config.page) {
    form.pageSize = readString(config.page.size);
    form.pageOrientation = readString(config.page.orientation);
    form.pageMargin = readDecimal(config.page.margin_inches);
  }

  if (config.footer) {
    form.footerText = readString(config.footer.text);
    form.footerPageNumber = config.footer.show_page_number === true;
  }

  return { form, warn };
}

/** Serializes the DOCX config form into the config JSON. Section presence
 * follows the schema's semantics: every parameter maps only when it carries
 * a value; the footer maps only with text or a page number. Callers validate
 * numeric fields first. */
export function buildDOCXConfigJSON(form: DOCXForm): string {
  const payload: DOCXConfig = {
    schema_version: SCHEMA_VERSION,
    flavor: FLAVOR_CONFIG,
  };

  if (
    form.fontsBodyFamily.trim() ||
    form.fontsBodySize.trim() ||
    form.fontsHeadingFamily.trim() ||
    form.fontsHeadingSize.trim()
  ) {
    const fonts: DOCXFontsConfig = {};

    if (form.fontsBodyFamily.trim())
      fonts.body_family = form.fontsBodyFamily.trim();

    if (form.fontsBodySize.trim())
      fonts.body_size = Number(form.fontsBodySize.trim());

    if (form.fontsHeadingFamily.trim())
      fonts.heading_family = form.fontsHeadingFamily.trim();

    if (form.fontsHeadingSize.trim())
      fonts.heading_size = Number(form.fontsHeadingSize.trim());
    payload.fonts = fonts;
  }

  if (form.headingColor.trim() || form.headingBold) {
    const heading: DOCXHeadingConfig = {};

    if (form.headingColor.trim()) heading.color = form.headingColor.trim();

    if (form.headingBold === "bold") heading.bold = true;

    if (form.headingBold === "regular") heading.bold = false;
    payload.heading = heading;
  }

  if (form.pageSize || form.pageOrientation || form.pageMargin.trim()) {
    const page: DOCXPageConfig = {};

    if (form.pageSize) page.size = form.pageSize;

    if (form.pageOrientation) page.orientation = form.pageOrientation;

    if (form.pageMargin.trim())
      page.margin_inches = Number(form.pageMargin.trim());
    payload.page = page;
  }

  if (form.footerText.trim() || form.footerPageNumber) {
    const footer: DOCXFooterConfig = {};

    if (form.footerText.trim()) footer.text = form.footerText.trim();

    if (form.footerPageNumber) footer.show_page_number = true;
    payload.footer = footer;
  }

  return JSON.stringify(payload);
}

/** A fresh template preset's config JSON: template flavor, no metadata yet —
 * the service stamps has_template/template_name over the upload. */
export function buildTemplateConfigJSON(): string {
  return JSON.stringify({
    schema_version: SCHEMA_VERSION,
    flavor: FLAVOR_TEMPLATE,
  });
}
