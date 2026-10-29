/**
 * Provider and service brand marks (redesign-shell-layout D6,
 * add-integrations): static maps from a provider type or integration service
 * id to a simple-icons glyph, with an initial-glyph tile for unknown ids.
 * The path data is inlined so the desktop app renders offline — a CDN
 * `<img>` would need the network and paints black on the dark theme. All
 * glyphs use the simple-icons 24x24 viewBox.
 */
import type { ReactElement } from "react";

/** One brand glyph: simple-icons path data plus its fill. */
interface BrandGlyph {
  /** SVG fill; "currentColor" follows the theme's foreground. */
  fill: string;
  /** Path data from simple-icons (viewBox 0 0 24 24), chunked into one
   * string segment per line so the file stays reviewable. */
  path: string;
}

/** Known provider marks by backend type id; other ids fall back to the
 * initial-glyph tile, so lookup stays `BrandGlyph | undefined`. */
interface BrandGlyphMap {
  anthropic: BrandGlyph;
  openai: BrandGlyph;
  /** Channel ids resolve through the same grammar (add-telegram-channel
   * task 9.3): the Channels pane leads each channel card with its mark. */
  telegram: BrandGlyph;
  [type: string]: BrandGlyph | undefined;
}

/** The anthropic clay brand color, via the `--brand` token contract. */
export const ANTHROPIC_CLAY = "var(--brand)";

// Anthropic's clay brand color; OpenAI stays monochrome and theme-following.
const brandGlyphs: BrandGlyphMap = {
  anthropic: {
    fill: ANTHROPIC_CLAY,
    path:
      "M17.3041 3.541h-3.6718l6.696 16.918H24Zm-10.6082 0L0 " +
      "20.459h3.7442l1.3693-3.5527h7.0052l1.3693 " +
      "3.5528h3.7442L10.5363 3.5409Zm-.3712 10.2232 2.2914-5.9456 " +
      "2.2914 5.9456Z",
  },
  openai: {
    fill: "currentColor",
    path:
      "M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 " +
      "6.0462 0 0 0-6.5098-2.9A6.0651 6.0651 0 0 0 4.9807 " +
      "4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 " +
      ".7427 7.0966 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 " +
      "6.5146 2.9001A5.9847 5.9847 0 0 0 13.2599 24a6.0557 6.0557 0 " +
      "0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 " +
      "6.0557 0 0 0-.7475-7.0729zm-9.022 12.6081a4.4755 4.4755 0 0 " +
      "1-2.8764-1.0408l.1419-.0804 4.7783-2.7582a.7948.7948 0 0 0 " +
      ".3927-.6813v-6.7369l2.02 1.1686a.071.071 0 0 1 " +
      ".038.052v5.5826a4.504 4.504 0 0 1-4.4945 " +
      "4.4944zm-9.6607-4.1254a4.4708 4.4708 0 0 " +
      "1-.5346-3.0137l.142.0852 4.783 2.7582a.7712.7712 0 0 0 .7806 " +
      "0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615L9.74 " +
      "19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 " +
      "7.8956a4.485 4.485 0 0 1 2.3655-1.9728V11.6a.7664.7664 0 0 0 " +
      ".3879.6765l5.8144 3.3543-2.0201 1.1685a.0757.0757 0 0 1-.071 " +
      "0l-4.8303-2.7865A4.504 4.504 0 0 1 2.3408 7.872zm16.5963 " +
      "3.8558L13.1038 8.364 15.1192 7.2a.0757.0757 0 0 1 .071 " +
      "0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 " +
      "8.1042v-5.6772a.79.79 0 0 " +
      "0-.407-.667zm2.0107-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 " +
      "0 0 0-.7854 0L9.409 9.2297V6.8974a.0662.0662 0 0 1 " +
      ".0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 " +
      "4.66zM8.3065 12.863l-2.02-1.1638a.0804.0804 0 0 " +
      "1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 " +
      "7.3757-3.4537l-.142.0805L8.704 5.459a.7948.7948 0 0 " +
      "0-.3927.6813zm1.0976-2.3654l2.602-1.4998 2.6069 " +
      "1.4998v2.9994l-2.5974 1.4997-2.6067-1.4997Z",
  },
  // Telegram stays monochrome and theme-following like OpenAI: on the quiet
  // settings surface the mark reads as an identity, not an accent.
  telegram: {
    fill: "currentColor",
    path:
      "M11.944 0A12 12 0 0 0 0 12a12 12 0 0 0 12 12 12 12 0 0 0 12-12A12 12 " +
      "0 0 0 12 0a12 12 0 0 0-.056 0zm4.962 7.224c.1-.002.321.023.465.14a.506.506 " +
      "0 0 1 .171.325c.016.093.036.306.02.472-.18 1.898-.962 6.502-1.36 " +
      "8.627-.168.9-.499 1.201-.82 1.23-.696.065-1.225-.46-1.9-.902-1.056-.693-1.653-1.124-2.678-1.8-1.185-.78-.417-1.21.258-1.91.177-.184 " +
      "3.247-2.977 3.307-3.23.007-.032.014-.15-.056-.212s-.174-.041-.249-.024c-.106.024-1.793 " +
      "1.14-5.061 3.345-.48.33-.913.49-1.302.48-.428-.008-1.252-.241-1.865-.44-.752-.245-1.349-.374-1.297-.789.027-.216.325-.437.893-.663 " +
      "3.498-1.524 5.83-2.529 6.998-3.014 3.332-1.386 4.025-1.627 4.476-1.635z",
  },
};

/** Known service marks by integration service id (add-integrations);
 * other ids — including family products — fall back to the initial-glyph
 * tile, so lookup stays `BrandGlyph | undefined`. */
interface ServiceGlyphMap {
  gitlab: BrandGlyph;
  github: BrandGlyph;
  atlassian: BrandGlyph;
  linear: BrandGlyph;
  notion: BrandGlyph;
  sentry: BrandGlyph;
  slack: BrandGlyph;
  google: BrandGlyph;
  "google-maps": BrandGlyph;
  [service: string]: BrandGlyph | undefined;
}

// Brand fills use each brand's official hex where it stays legible on both
// themes; near-black marks (GitHub, Notion, Sentry, Slack) follow the theme's
// foreground via currentColor instead.
const serviceGlyphs: ServiceGlyphMap = {
  gitlab: {
    fill: "#FC6D26",
    path:
      "m23.6004 9.5927-.0337-.0862L20.3.9814a.851.851 0 0 0-.3362-.405.8748.8748 0 0 " +
      "0-.9997.0539.8748.8748 0 0 0-.29.4399l-2.2055 6.748H7.5375l-2.2057-6.748a.8573.8573 " +
      "0 0 0-.29-.4412.8748.8748 0 0 0-.9997-.0537.8585.8585 0 0 0-.3362.4049L.4332 " +
      "9.5015l-.0325.0862a6.0657 6.0657 0 0 0 2.0119 7.0105l.0113.0087.03.0213 4.976 " +
      "3.7264 2.462 1.8633 1.4995 1.1321a1.0085 1.0085 0 0 0 1.2197 0l1.4995-1.1321 " +
      "2.4619-1.8633 5.006-3.7489.0125-.01a6.0682 6.0682 0 0 0 2.0094-7.003z",
  },
  github: {
    fill: "currentColor",
    path:
      "M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 " +
      "0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 " +
      "17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 " +
      "1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 " +
      "1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 " +
      "1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 " +
      "3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 " +
      "1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12",
  },
  atlassian: {
    fill: "#0052CC",
    path:
      "M7.12 11.084a.683.683 0 00-1.16.126L.075 22.974a.703.703 0 00.63 1.018h8.19a.678.678 " +
      "0 00.63-.39c1.767-3.65.696-9.203-2.406-12.52zM11.434.386a15.515 15.515 0 00-.906 " +
      "15.317l3.95 7.9a.703.703 0 00.628.388h8.19a.703.703 0 00.63-1.017L12.63.38a.664.664 " +
      "0 00-1.196.006z",
  },
  linear: {
    fill: "#5E6AD2",
    path:
      "M2.886 4.18A11.982 11.982 0 0 1 11.99 0C18.624 0 24 5.376 24 12.009c0 3.64-1.62 6.903-4.18 " +
      "9.105L2.887 4.18ZM1.817 5.626l16.556 16.556c-.524.33-1.075.62-1.65.866L.951 " +
      "7.277c.247-.575.537-1.126.866-1.65ZM.322 9.163l14.515 14.515c-.71.172-1.443.282-2.195.322L0 " +
      "11.358a12 12 0 0 1 .322-2.195Zm-.17 4.862 9.823 9.824a12.02 12.02 0 0 1-9.824-9.824Z",
  },
  notion: {
    fill: "currentColor",
    path:
      "M4.459 4.208c.746.606 1.026.56 2.428.466l13.215-.793c.28 0 .047-.28-.046-.326L17.86 " +
      "1.968c-.42-.326-.981-.7-2.055-.607L3.01 2.295c-.466.046-.56.28-.374.466zm.793 3.08v13.904c0 " +
      ".747.373 1.027 1.214.98l14.523-.84c.841-.046.935-.56.935-1.167V6.354c0-.606-.233-.933-.748-.887l-15.177.887c-.56.047-.747.327-.747.933zm14.337.745c.093.42 " +
      "0 .84-.42.888l-.7.14v10.264c-.608.327-1.168.514-1.635.514-.748 0-.935-.234-1.495-.933l-4.577-7.186v6.952L12.21 " +
      "19s0 .84-1.168.84l-3.222.186c-.093-.186 0-.653.327-.746l.84-.233V9.854L7.822 " +
      "9.76c-.094-.42.14-1.026.793-1.073l3.456-.233 4.764 7.279v-6.44l-1.215-.139c-.093-.514.28-.887.747-.933zM1.936 " +
      "1.035l13.31-.98c1.634-.14 2.055-.047 3.082.7l4.249 2.986c.7.513.934.653.934 1.213v16.378c0 " +
      "1.026-.373 1.634-1.68 1.726l-15.458.934c-.98.047-1.448-.093-1.962-.747l-3.129-4.06c-.56-.747-.793-1.306-.793-1.96V2.667c0-.839.374-1.54 " +
      "1.447-1.632z",
  },
  sentry: {
    fill: "currentColor",
    path:
      "M13.91 2.505c-.873-1.448-2.972-1.448-3.844 0L6.904 7.92a15.478 15.478 0 0 1 8.53 " +
      "12.811h-2.221A13.301 13.301 0 0 0 5.784 9.814l-2.926 5.06a7.65 7.65 0 0 1 4.435 " +
      "5.848H2.194a.365.365 0 0 1-.298-.534l1.413-2.402a5.16 5.16 0 0 0-1.614-.913L.296 " +
      "19.275a2.182 2.182 0 0 0 .812 2.999 2.24 2.24 0 0 0 1.086.288h6.983a9.322 9.322 0 0 " +
      "0-3.845-8.318l1.11-1.922a11.47 11.47 0 0 1 4.95 10.24h5.915a17.242 17.242 0 0 " +
      "0-7.885-15.28l2.244-3.845a.37.37 0 0 1 .504-.13c.255.14 9.75 16.708 9.928 16.9a.365.365 " +
      "0 0 1-.327.543h-2.287c.029.612.029 1.223 0 1.831h2.297a2.206 2.206 0 0 0 1.922-3.31z",
  },
  slack: {
    fill: "currentColor",
    path:
      "M5.042 15.165a2.528 2.528 0 0 1-2.52 2.523A2.528 2.528 0 0 1 0 15.165a2.527 2.527 0 0 1 " +
      "2.522-2.52h2.52v2.52zM6.313 15.165a2.527 2.527 0 0 1 2.521-2.52 2.527 2.527 0 0 1 2.521 " +
      "2.52v6.313A2.528 2.528 0 0 1 8.834 24a2.528 2.528 0 0 1-2.521-2.522v-6.313zM8.834 " +
      "5.042a2.528 2.528 0 0 1-2.521-2.52A2.528 2.528 0 0 1 8.834 0a2.528 2.528 0 0 1 2.521 " +
      "2.522v2.52H8.834zM8.834 6.313a2.528 2.528 0 0 1 2.521 2.521 2.528 2.528 0 0 1-2.521 " +
      "2.521H2.522A2.528 2.528 0 0 1 0 8.834a2.528 2.528 0 0 1 2.522-2.521h6.312zM18.956 " +
      "8.834a2.528 2.528 0 0 1 2.522-2.521A2.528 2.528 0 0 1 24 8.834a2.528 2.528 0 0 1-2.522 " +
      "2.521h-2.522V8.834zM17.688 8.834a2.528 2.528 0 0 1-2.523 2.521 2.527 2.527 0 0 " +
      "1-2.52-2.521V2.522A2.527 2.527 0 0 1 15.165 0a2.528 2.528 0 0 1 2.523 2.522v6.312zM15.165 " +
      "18.956a2.528 2.528 0 0 1 2.523 2.522A2.528 2.528 0 0 1 15.165 24a2.527 2.527 0 0 " +
      "1-2.52-2.522v-2.522h2.52zM15.165 17.688a2.527 2.527 0 0 1-2.52-2.523 2.526 2.526 0 0 1 " +
      "2.52-2.52h6.313A2.527 2.527 0 0 1 24 15.165a2.528 2.528 0 0 1-2.522 2.523h-6.313z",
  },
  google: {
    fill: "#4285F4",
    path:
      "M12.48 10.92v3.28h7.84c-.24 1.84-.853 3.187-1.787 4.133-1.147 1.147-2.933 2.4-6.053 " +
      "2.4-4.827 0-8.6-3.893-8.6-8.72s3.773-8.72 8.6-8.72c2.6 0 4.507 1.027 5.907 " +
      "2.347l2.307-2.307C18.747 1.44 16.133 0 12.48 0 5.867 0 .307 5.387.307 12s5.56 12 12.173 " +
      "12c3.573 0 6.267-1.173 8.373-3.36 2.16-2.16 2.84-5.213 2.84-7.667 0-.76-.053-1.467-.173-2.053H12.48z",
  },
  "google-maps": {
    fill: "#4285F4",
    path:
      "M19.527 4.799c1.212 2.608.937 5.678-.405 8.173-1.101 2.047-2.744 3.74-4.098 " +
      "5.614-.619.858-1.244 1.75-1.669 2.727-.141.325-.263.658-.383.992-.121.333-.224.673-.34 " +
      "1.008-.109.314-.236.684-.627.687h-.007c-.466-.001-.579-.53-.695-.887-.284-.874-.581-1.713-1.019-2.525-.51-.944-1.145-1.817-1.79-2.671L19.527 " +
      "4.799zM8.545 7.705l-3.959 4.707c.724 1.54 1.821 2.863 2.871 4.18.247.31.494.622.737.936l4.984-5.925-.029.01c-1.741.601-3.691-.291-4.392-1.987a3.377 " +
      "3.377 0 0 1-.209-.716c-.063-.437-.077-.761-.004-1.198l.001-.007zM5.492 3.149l-.003.004c-1.947 2.466-2.281 5.88-1.117 " +
      "8.77l4.785-5.689-.058-.05-3.607-3.035zM14.661.436l-3.838 4.563a.295.295 0 0 1 .027-.01c1.6-.551 3.403.15 4.22 " +
      "1.626.176.319.323.683.377 1.045.068.446.085.773.012 1.22l-.003.016 3.836-4.561A8.382 8.382 0 0 0 14.67.439l-.009-.003zM9.466 " +
      "5.868L14.162.285l-.047-.012A8.31 8.31 0 0 0 11.986 0a8.439 8.439 0 0 0-6.169 2.766l-.016.018 3.665 3.084z",
  },
};

export interface BrandMarkProps {
  /** Provider type id exactly as the backend knows it. */
  type: string;
  /** Provider display name; the fallback tile's initial comes from it. */
  name: string;
  /** Square tile size in px. */
  size?: number;
}

/** Shared tile inputs: a pre-resolved glyph (or undefined for the fallback)
 * plus the fallback id/name and tile size. */
interface BrandTileProps {
  glyph: BrandGlyph | undefined;
  type: string;
  name: string;
  size: number;
}

/** The neutral brand tile: a known id renders its inline brand glyph, any
 * other id an initial letter (`name`, else `type`) on the same tile. */
function BrandTile({ glyph, type, name, size }: BrandTileProps): ReactElement {
  return (
    <div
      className="grid shrink-0 place-items-center rounded-lg border border-line bg-surface"
      style={{ width: size, height: size }}
    >
      {glyph ? (
        <svg
          aria-hidden="true"
          viewBox="0 0 24 24"
          width={size * 0.625}
          height={size * 0.625}
          fill={glyph.fill}
        >
          <path d={glyph.path} />
        </svg>
      ) : (
        <span
          aria-hidden="true"
          className="text-muted"
          style={{ fontSize: size * 0.375 }}
        >
          {(name || type).trim().charAt(0).toUpperCase()}
        </span>
      )}
    </div>
  );
}

/**
 * The brand tile leading a provider row (redesign-shell-layout D6): a known
 * type renders its inline brand glyph, any other type an initial letter
 * (`name`, else `type`) on the same neutral tile.
 */
export function BrandMark({
  type,
  name,
  size = 32,
}: BrandMarkProps): ReactElement {
  return (
    <BrandTile glyph={brandGlyphs[type]} type={type} name={name} size={size} />
  );
}

export interface ServiceMarkProps {
  /** Integration service id exactly as the catalog knows it. */
  type: string;
  /** Service display name; the fallback tile's initial comes from it. */
  name: string;
  /** Square tile size in px. */
  size?: number;
}

/**
 * The brand tile leading an integration tile (add-integrations): a known
 * service id renders its inline brand glyph, any other id — including the
 * Google Workspace family products — an initial letter on the same neutral
 * tile.
 */
export function ServiceMark({
  type,
  name,
  size = 32,
}: ServiceMarkProps): ReactElement {
  return (
    <BrandTile
      glyph={serviceGlyphs[type]}
      type={type}
      name={name}
      size={size}
    />
  );
}
