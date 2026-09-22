// Markdown pre-render pass for the right panel's file source (add-right-panel):
// pure text transforms a renderer composes BEFORE handing markdown to the
// markdown component — YAML frontmatter extraction and relative-asset URL
// rewriting. No dependencies; both halves are hand-rolled and must never
// touch fenced code blocks or inline code.

export interface Frontmatter {
  /** Flat `key: value` pairs, or null when the text has no frontmatter block. */
  meta: Record<string, string> | null;
  /** Everything after the frontmatter block (the original text when absent). */
  body: string;
}

// A fence line is exactly '---' — trailing spaces/tabs and a CR (CRLF files)
// are tolerated. A '---' inside a value shares its line with the key, so it
// can never match.
const FENCE_LINE = /^---[ \t]*\r?$/;

// Flat pair: key starts non-indented, contains no colon, and is followed by
// '':'' + value or end of line (YAML's `key: value` needs the space; that
// keeps bare URLs like `https://x` from parsing as pairs).
const PAIR_LINE = /^([A-Za-z0-9_.-][A-Za-z0-9_.\- ]*):(?:[ \t]+(.*))?$/;

export function extractFrontmatter(text: string): Frontmatter {
  // The opening fence must be the file's very first line — a leading space
  // or blank line makes it an ordinary thematic break, not frontmatter.
  const firstNl = text.indexOf('\n');
  const firstLine = firstNl === -1 ? text : text.slice(0, firstNl);
  if (!FENCE_LINE.test(firstLine)) return { meta: null, body: text };

  // Scan for the closing fence: the next lone '---' line. Not found -> no
  // frontmatter at all (the leading '---' stays part of the untouched body).
  let cursor = firstNl + 1;
  let closeStart = -1;
  while (cursor < text.length) {
    const nl = text.indexOf('\n', cursor);
    const line = nl === -1 ? text.slice(cursor) : text.slice(cursor, nl);
    if (FENCE_LINE.test(line)) {
      closeStart = cursor;
      break;
    }
    if (nl === -1) break;
    cursor = nl + 1;
  }
  if (closeStart === -1) return { meta: null, body: text };

  // The slice keeps the last key line's own terminator — drop it before parsing.
  const raw = text.slice(firstNl + 1, closeStart).replace(/\r?\n$/, '');
  // Body starts after the closing fence line's own terminator (CRLF handled
  // by slicing the original string); a file ending at the fence yields ''.
  const closeNl = text.indexOf('\n', closeStart);
  const body = closeNl === -1 ? '' : text.slice(closeNl + 1);

  return { meta: parseFrontmatter(raw), body };
}

// Flat `key: value` pairs only. Indented or non-`key:` lines continue the
// previous key's value as its raw string (nested lists/maps render verbatim
// instead of being dropped); such a line before any key is ignored. Matching
// surrounding quotes are stripped from flat values — `""` YAML escapes are
// not interpreted; this is a display header, not a data pipeline.
function parseFrontmatter(raw: string): Record<string, string> {
  const meta: Record<string, string> = {};
  let currentKey: string | null = null;
  for (const rawLine of raw.split('\n')) {
    const line = rawLine.replace(/\r$/, '');
    const pair = PAIR_LINE.exec(line);
    if (pair) {
      currentKey = pair[1].trim();
      meta[currentKey] = unquote((pair[2] ?? '').trim());
    } else if (currentKey !== null) {
      const prev = meta[currentKey];
      meta[currentKey] = prev ? prev + '\n' + line : line;
    }
  }
  return meta;
}

function unquote(value: string): string {
  if (
    value.length >= 2 &&
    ((value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'")))
  ) {
    return value.slice(1, -1);
  }
  return value;
}

// ---------------------------------------------------------------------------
// rewriteRelativeAssets — a linear state machine over the markdown. States:
// fenced code block (``` or ~~~, up to 3 leading spaces, same-char closer),
// inline code span (backtick run of N closed by an exact run of N), HTML
// tag (<img>/<a>), and plain markdown where inline images/links are matched.
// Only RELATIVE URLs reach resolve(); output is byte-identical elsewhere.
// ---------------------------------------------------------------------------

export type AssetResolver = (url: string) => string;

// Absolute schemes (http:, https:, data:, mailto:, …). Protocol-relative '//'
// and in-page anchors are handled separately in isRelative().
const SCHEME = /^[a-zA-Z][a-zA-Z0-9+.-]*:/;

function isRelative(url: string): boolean {
  if (url === '') return false;
  if (SCHEME.test(url)) return false;
  if (url.startsWith('//') || url.startsWith('#')) return false;
  // Root-relative paths stay relative to the file source — resolve() decides.
  return true;
}

function resolveIfRelative(url: string, resolve: AssetResolver): string | null {
  return isRelative(url) ? resolve(url) : null;
}

// Sticky matchers, anchored at the scan position.
const IMAGE_OPEN = /!\[([^\]\n]*)\]\(/y;
const LINK_OPEN = /\[([^\]\n]*)\]\(/y;
// Badge/link-with-image: [![alt](img)](target) — matched as one unit so both
// destinations rewrite (plain link matching would swallow the inner image).
const BADGE_LINK = /\[!\[([^\]\n]*)\]\(([^()\s]+)\)\]\(([^()\s]+)\)/y;
// Destination after '](': angle-wrapped or bare, with optional title(s).
// NOTE: no '^' anchor — with the sticky flag '^' would assert true string
// start instead of matching at lastIndex.
const DEST = /[ \t]*(?:<([^>\n]*)>|([^\s)]+))((?:[ \t]+(?:"[^"]*"|'[^']*'|\([^)]*\)))*[ \t]*)/y;
// <img>/<a> tag starts (HTML tag names are case-insensitive).
const HTML_TAG_OPEN = /<(img|a)(?=[\t\n\f\r />])/iy;

interface ParsedDest {
  url: string;
  /** Raw whitespace/titles between the URL and ')' — re-emitted verbatim. */
  tail: string;
  /** Index just past the closing ')'. */
  end: number;
}

function parseDest(s: string, start: number): ParsedDest | null {
  DEST.lastIndex = start;
  const m = DEST.exec(s);
  if (!m || s[DEST.lastIndex] !== ')') return null;
  return {
    url: m[1] !== undefined ? m[1] : (m[2] ?? ''),
    tail: m[3] ?? '',
    end: DEST.lastIndex + 1,
  };
}

function backtickRunEnd(s: string, start: number): number {
  let end = start;
  while (end < s.length && s[end] === '`') end++;
  return end;
}

/** Start index of the run of exactly `openLen` backticks closing the span
 * opened at openStart, or -1 (an unterminated run is literal text). */
function findCodeSpanClose(s: string, openStart: number): number {
  const openLen = backtickRunEnd(s, openStart) - openStart;
  let i = openStart + openLen;
  while (i < s.length) {
    if (s[i] === '`') {
      const runEnd = backtickRunEnd(s, i);
      if (runEnd - i === openLen) return i;
      i = runEnd;
    } else {
      i++;
    }
  }
  return -1;
}

function isLineStart(s: string, i: number): boolean {
  return i === 0 || s[i - 1] === '\n';
}

interface FenceOpen {
  char: string;
  len: number;
  /** Index of the first character after the opener line's newline. */
  end: number;
}

function fenceOpenerAt(s: string, i: number): FenceOpen | null {
  let j = i;
  let spaces = 0;
  while (s[j] === ' ') {
    spaces++;
    j++;
  }
  // A tab counts as 4 columns in CommonMark — already too far indented.
  if (spaces > 3) return null;
  const ch = s[j];
  if (ch !== '`' && ch !== '~') return null;
  let len = 0;
  while (s[j + len] === ch) len++;
  if (len < 3) return null;
  const nl = s.indexOf('\n', j + len);
  return { char: ch, len, end: nl === -1 ? s.length : nl + 1 };
}

/** True when the line (newline excluded, CR included) is only the marker. */
function isClosingFence(line: string, char: string, len: number): boolean {
  let i = 0;
  let spaces = 0;
  while (line[i] === ' ') {
    spaces++;
    i++;
  }
  if (spaces > 3) return false;
  let run = 0;
  while (line[i + run] === char) run++;
  if (run < len) return false;
  i += run;
  while (i < line.length && (line[i] === ' ' || line[i] === '\t' || line[i] === '\r')) i++;
  return i === line.length;
}

/** Rewrites src/href inside one <img>/<a> tag body (quotes honored). */
function rewriteHtmlTagAt(
  s: string,
  start: number,
  resolve: AssetResolver
): { text: string; end: number } | null {
  HTML_TAG_OPEN.lastIndex = start;
  const m = HTML_TAG_OPEN.exec(s);
  if (!m) return null;
  // Find the tag's '>' honoring quoted attribute values ('>' may sit in one).
  let i = HTML_TAG_OPEN.lastIndex;
  let quote: string | null = null;
  while (i < s.length) {
    const c = s[i];
    if (quote !== null) {
      if (c === quote) quote = null;
    } else if (c === '"' || c === "'") {
      quote = c;
    } else if (c === '>') {
      break;
    }
    i++;
  }
  if (i >= s.length) return null; // unterminated tag — treat as plain text
  const tag = s.slice(start, i);
  const attr = m[1].toLowerCase() === 'img' ? 'src' : 'href';
  // Groups: 1 = "\ssrc=", 2 = double-quoted value, 3 = single-quoted value,
  // 4 = bare value — quote delimiters stay out of the values.
  const attrRe = new RegExp(
    `(\\s${attr}\\s*=\\s*)(?:"([^"]*)"|'([^']*)'|([^\\s>]*))`,
    'i'
  );
  const body = tag.replace(
    attrRe,
    (full: string, lead: string, dq: string | undefined, sq: string | undefined, bare: string | undefined): string => {
      if (dq !== undefined) {
        const v = resolveIfRelative(dq, resolve);
        return v === null ? full : `${lead}"${v}"`;
      }
      if (sq !== undefined) {
        const v = resolveIfRelative(sq, resolve);
        return v === null ? full : `${lead}'${v}'`;
      }
      if (bare !== undefined && bare !== '') {
        const v = resolveIfRelative(bare, resolve);
        return v === null ? full : lead + v;
      }
      return full;
    }
  );
  return { text: body + '>', end: i + 1 };
}

export function rewriteRelativeAssets(markdown: string, resolve: AssetResolver): string {
  let out = '';
  let pos = 0;
  let fenceChar: string | null = null;
  let fenceLen = 0;
  const n = markdown.length;

  while (pos < n) {
    // Inside a fence the whole line is copied verbatim; only a lone marker
    // line (same char, at least the opening length) closes the block.
    if (fenceChar !== null) {
      const nl = markdown.indexOf('\n', pos);
      const lineEnd = nl === -1 ? n : nl + 1;
      const line = markdown.slice(pos, nl === -1 ? n : nl);
      if (isClosingFence(line, fenceChar, fenceLen)) fenceChar = null;
      out += markdown.slice(pos, lineEnd);
      pos = lineEnd;
      continue;
    }

    if (isLineStart(markdown, pos)) {
      const open = fenceOpenerAt(markdown, pos);
      if (open) {
        out += markdown.slice(pos, open.end);
        pos = open.end;
        fenceChar = open.char;
        fenceLen = open.len;
        continue;
      }
    }

    const ch = markdown[pos];

    // Inline code span: a run of N backticks closes on an exact run of N —
    // runs of other lengths (nested backticks) are span content.
    if (ch === '`') {
      const close = findCodeSpanClose(markdown, pos);
      if (close !== -1) {
        const closeEnd = backtickRunEnd(markdown, close);
        out += markdown.slice(pos, closeEnd);
        pos = closeEnd;
      } else {
        const runEnd = backtickRunEnd(markdown, pos);
        out += markdown.slice(pos, runEnd);
        pos = runEnd;
      }
      continue;
    }

    if (ch === '<') {
      const tag = rewriteHtmlTagAt(markdown, pos, resolve);
      if (tag) {
        out += tag.text;
        pos = tag.end;
        continue;
      }
    }

    if (ch === '!') {
      IMAGE_OPEN.lastIndex = pos;
      const m = IMAGE_OPEN.exec(markdown);
      if (m) {
        const dest = parseDest(markdown, IMAGE_OPEN.lastIndex);
        if (dest) {
          const url = resolveIfRelative(dest.url, resolve) ?? dest.url;
          out += `![${m[1]}](${url}${dest.tail})`;
          pos = dest.end;
          continue;
        }
      }
      out += ch;
      pos++;
      continue;
    }

    if (ch === '[') {
      BADGE_LINK.lastIndex = pos;
      const badge = BADGE_LINK.exec(markdown);
      if (badge) {
        const img = resolveIfRelative(badge[2], resolve) ?? badge[2];
        const target = resolveIfRelative(badge[3], resolve) ?? badge[3];
        out += `[![${badge[1]}](${img})](${target})`;
        pos = BADGE_LINK.lastIndex;
        continue;
      }
      LINK_OPEN.lastIndex = pos;
      const m = LINK_OPEN.exec(markdown);
      if (m) {
        const dest = parseDest(markdown, LINK_OPEN.lastIndex);
        if (dest) {
          const url = resolveIfRelative(dest.url, resolve) ?? dest.url;
          out += `[${m[1]}](${url}${dest.tail})`;
          pos = dest.end;
          continue;
        }
      }
      out += ch;
      pos++;
      continue;
    }

    out += ch;
    pos++;
  }
  return out;
}
