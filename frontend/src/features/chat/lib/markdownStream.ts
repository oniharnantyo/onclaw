/**
 * Streaming-safe markdown pre-render pass (task 9.2, design D13 verdict B).
 * While an entry streams, react-markdown would parse the partial source and
 * paint transient artifacts: an unclosed ``` fence swallows every following
 * line as code, and an unclosed emphasis or inline-code marker styles the
 * remainder of the message (spec chat-view "Streaming-safe markdown").
 *
 * `stabilizeMarkdown` closes those dangling constructs so the partial source
 * parses to what the completed text will keep: a still-open fence gets its
 * closing fence appended, an unmatched emphasis marker gets its closer at
 * the end of the prose, and an unmatched backtick run closes at the end of
 * its own line (a code span never reaches past its line's intended end).
 * When nothing is dangling the source is returned UNTOUCHED — byte-for-byte
 * — so the final render of a streamed message equals a one-pass render of
 * the complete text (the caller only routes streaming input through this
 * module at all).
 *
 * Scope is deliberately the spec's artifact set: fences (``` and ~~~, with
 * info strings, indented up to three spaces), backtick inline code, and
 * asterisk emphasis (** then *). Underscore emphasis is skipped on purpose —
 * CommonMark's intraword rules mean `snake_case` holds emphasis markers only
 * to over-eager counters, and appending an underscore mid-word would itself
 * be a transient artifact. Markers inside inline code or fenced code are
 * never counted: CommonMark treats that content as literal.
 *
 * Task 5.3 (design D5) adds an incremental wrapper: the scan is still
 * synchronous, but only the unfinalized tail is rescanned per token. The
 * full-document scan stays as the reference (`stabilizeMarkdownFull`) and
 * runs on a cache miss; the wrapper's output is byte-identical to it at
 * every prefix.
 */

/** A fence opener: up to three leading spaces, then 3+ backticks or tildes. */
const FENCE_OPEN = /^ {0,3}(`{3,}|~{3,})/;

/** A fence closer: the same marker run alone (CommonMark forbids an info
 * string on the closing fence). */
function bareFence(line: string, char: string, len: number): boolean {
  return line.trim() === char.repeat(len);
}

interface FenceState {
  char: string;
  len: number;
}

/** One line's role in the fence scan. */
const enum LineRole {
  Prose = 0,
  FenceMarker = 1,
  Fenced = 2,
}

/** The fence scan's result: per-line roles plus the fence still open at EOF,
 * if any. */
interface FenceScan {
  lines: string[];
  roles: LineRole[];
  dangling: FenceState | null;
}

/** Classifies every line: prose, fence marker, or content inside a fence.
 * A bare same-char, at-least-as-long marker run closes an open fence; any
 * other line inside a fence is content. */
function classifyLines(source: string): FenceScan {
  const lines = source.split("\n");
  const roles: LineRole[] = new Array(lines.length).fill(LineRole.Prose);
  let open: FenceState | null = null;

  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i] ?? "";

    if (open !== null) {
      if (bareFence(line, open.char, open.len)) {
        roles[i] = LineRole.FenceMarker;
        open = null;
      } else {
        roles[i] = LineRole.Fenced;
      }

      continue;
    }

    const m = FENCE_OPEN.exec(line);

    if (m) {
      const marker = m[1] ?? "";

      open = { char: marker[0] ?? "`", len: marker.length };
      roles[i] = LineRole.FenceMarker;
    }
  }

  return { lines, roles, dangling: open };
}

/**
 * One pass of non-overlapping literal search: occurrences of `needle` in
 * `text`, as [start, end) ranges.
 */
function rangesOf(text: string, needle: string): Array<[number, number]> {
  const ranges: Array<[number, number]> = [];

  let at = text.indexOf(needle);

  while (at !== -1) {
    ranges.push([at, at + needle.length]);
    at = text.indexOf(needle, at + needle.length);
  }

  return ranges;
}

/** One contiguous prose segment's dangling markers. */
interface SegmentFix {
  /** "`"s closing the unmatched backtick run, inserted at `tickAt`. */
  tick: string;
  tickAt: number;
  /** "**"/"*" closers appended at the segment's end. */
  emphasis: string;
}

/** Finds the dangling inline constructs of one prose segment (the text
 * between fences). Backtick runs close code spans — a run closes only a run
 * of the same length, and the unmatched run closes at the end of ITS line;
 * then, outside code spans (the closed ones plus the span the pending
 * closer will form), `**` pairs count bold and the remaining single `*`s
 * count italic. */
function segmentFix(seg: string): SegmentFix {
  const nofix: SegmentFix = { tick: "", tickAt: seg.length, emphasis: "" };

  if (seg === "") return nofix;

  // --- inline code: the first unmatched backtick run stays open ---
  let openRun = 0;
  let openRunEnd = seg.length; // where its closer will land (line end)
  const codeRanges: Array<[number, number]> = [];
  let runStart = -1;
  let runLen = 0;

  const flushRun = (end: number): void => {
    if (openRun === 0) {
      openRun = runLen;

      // The pending closer lands at the end of the run's own line — a code
      // span never reaches past it.
      const nl = seg.indexOf("\n", runStart);

      openRunEnd = nl === -1 ? seg.length : nl;
      codeRanges.push([runStart, openRunEnd]);
    } else if (runLen === openRun) {
      const last = codeRanges[codeRanges.length - 1];

      if (last) last[1] = end;

      openRun = 0;
    }

    // A run of a different length while a span is open stays literal
    // content (CommonMark closes spans only on an equal-length run).
    runStart = -1;
    runLen = 0;
  };

  for (let i = 0; i <= seg.length; i += 1) {
    const ch = i < seg.length ? seg[i] : null;

    if (ch === "`") {
      if (runStart === -1) runStart = i;

      runLen += 1;

      continue;
    }

    if (runLen > 0) flushRun(i);
  }

  const outsideCode = (start: number, end: number): boolean =>
    !codeRanges.some(([s, e]: [number, number]) => s <= start && end <= e);

  // --- emphasis: counted only outside code spans ---
  let bold = 0;

  for (const [s, e] of rangesOf(seg, "**")) {
    if (outsideCode(s, e)) bold += 1;
  }

  const boldMask = new Array<boolean>(seg.length).fill(false);

  for (const [s, e] of rangesOf(seg, "**")) boldMask.fill(true, s, e);

  let italic = 0;

  for (let i = 0; i < seg.length; i += 1) {
    if (seg[i] === "*" && !boldMask[i] && outsideCode(i, i + 1)) italic += 1;
  }

  let emphasis = "";

  if (bold % 2 === 1) emphasis += "**";

  if (italic % 2 === 1) emphasis += "*";

  return {
    tick: openRun > 0 ? "`".repeat(openRun) : "",
    tickAt: openRunEnd,
    emphasis,
  };
}

/** Index just past the segment's last non-blank line (its newline excluded),
 * or -1 when every line is blank — emphasis cannot cross a blank line, so
 * its closer belongs on the last line that could still be open. */
function lastNonBlankEnd(text: string): number {
  const lines = text.split("\n");

  for (let i = lines.length - 1; i >= 0; i -= 1) {
    if ((lines[i] ?? "").trim() !== "") {
      return lines.slice(0, i + 1).join("\n").length;
    }
  }

  return -1;
}

/** Applies one segment's fixes: the backtick closer at its line end, the
 * emphasis closers at the end of the last non-blank line. Line count is
 * preserved. */
function stabilizeSegment(seg: string): string {
  const fix = segmentFix(seg);

  if (fix.tick === "" && fix.emphasis === "") return seg;

  const withTick =
    fix.tick === ""
      ? seg
      : seg.slice(0, fix.tickAt) + fix.tick + seg.slice(fix.tickAt);

  if (fix.emphasis === "") return withTick;

  const at = lastNonBlankEnd(withTick);

  return at === -1
    ? withTick
    : withTick.slice(0, at) + fix.emphasis + withTick.slice(at);
}

/**
 * Closes dangling markdown constructs in a streaming partial. With nothing
 * dangling the input passes through byte-identical.
 *
 * This is the full-document scan (task 5.3's reference path): the public
 * `stabilizeMarkdown` is an incremental wrapper that delegates here on a
 * cache miss. Exported so the parity tests can compare the incremental
 * result against the scan it replaces at every prefix.
 */
export function stabilizeMarkdownFull(source: string): string {
  const { lines, roles, dangling } = classifyLines(source);

  let changed = false;
  let out = lines;

  // Contiguous prose segments are stabilized independently, so a marker
  // dangling in one never leaks across a fence into the next.
  let start = -1; // segment start (inclusive), -1 = not in a segment

  const flush = (end: number): void => {
    if (start === -1) return;

    const seg = out.slice(start, end).join("\n");
    const fixed = stabilizeSegment(seg);

    if (fixed !== seg) {
      out = [...out.slice(0, start), ...fixed.split("\n"), ...out.slice(end)];
      changed = true;
    }

    start = -1;
  };

  for (let i = 0; i < lines.length; i += 1) {
    if (roles[i] === LineRole.Prose) {
      if (start === -1) start = i;

      continue;
    }

    flush(i);
  }

  flush(lines.length);

  // A fence still open at EOF gets its closer so the block parses complete.
  // The streamed trailing newline is the in-progress line terminator, not a
  // blank code line — the closer joins the last content line.
  if (dangling !== null) {
    let base = out.join("\n");

    if (base.endsWith("\n")) base = base.slice(0, -1);

    return base + "\n" + dangling.char.repeat(dangling.len);
  }

  return changed ? out.join("\n") : source;
}

/**
 * The latest line start up to which a scan can no longer change (task 5.3,
 * design D5): the finalized-prefix length. A line start is safe only when
 * everything before it is closed — no open fence, no unmatched backtick run
 * whose equal-length closer could still arrive, and an even `**`/`*` count.
 * Under those conditions a later append can neither open a construct that
 * reaches back across the boundary nor move a closer before it, so the
 * prefix is byte-identical in every future full scan.
 *
 * Conservative by construction: every uncertainty (an open construct,
 * escaped markers the full scan still counts, HTML blocks, list continuity)
 * simply keeps the boundary where it was, and the tail is rescanned.
 */
function finalizedPrefixLength(source: string): number {
  const lines = source.split("\n");
  let fence: FenceState | null = null;
  let openRun = 0;
  let bold = 0;
  let italic = 0;
  let best = 0;
  let offset = 0;

  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i] ?? "";
    const opener = fence === null ? FENCE_OPEN.exec(line) : null;

    // A fence opener starts a fresh segment: the prose before it is closed
    // (its fix is already determined), so the boundary is safe at this line
    // start even when that segment carried a dangling marker. Otherwise the
    // shared prose state must be clean.
    const clean =
      opener !== null ||
      (fence === null && openRun === 0 && bold % 2 === 0 && italic % 2 === 0);

    if (clean) best = offset;

    if (fence !== null) {
      if (bareFence(line, fence.char, fence.len)) fence = null;
    } else if (opener !== null) {
      const marker = opener[1] ?? "";

      fence = { char: marker[0] ?? "`", len: marker.length };
      // A fence ends the prose segment: the next one starts clean.
      openRun = 0;
      bold = 0;
      italic = 0;
    } else {
      // Prose line: mirror segmentFix's counters exactly (backtick runs
      // close only equal-length runs; emphasis is counted outside the
      // pending span).
      let at = 0;

      while (at < line.length) {
        const ch = line[at];

        if (ch === "`") {
          let end = at;

          while (end < line.length && line[end] === "`") end += 1;

          const run = end - at;

          if (openRun === 0) openRun = run;
          else if (run === openRun) openRun = 0;

          at = end;

          continue;
        }

        if (openRun === 0) {
          if (ch === "*") {
            if (line[at + 1] === "*") {
              bold += 1;
              at += 2;

              continue;
            }

            italic += 1;
          }
        }

        at += 1;
      }
    }

    offset += line.length + 1;
  }

  return best;
}

/** Incremental cache (task 5.3): the previous input, its finalized prefix
 * length, and that prefix's output. A streaming caller passes growing
 * prefixes, so each call rescans only the unfinalized tail; the finalized
 * prefix carries over byte-identically by the boundary rule above. */
interface StreamCache {
  source: string;
  boundary: number;
  prefix: string;
  /** The full stabilized result for `source` (an unchanged re-render asks
   * for the same text again). */
  result: string;
}

let streamCache: StreamCache | null = null;

/** Clears the incremental cache. Tests call it to pin a full-scan parity
 * starting point; production callers never need it (a non-prefix input
 * falls back to the full scan on its own). */
export function resetStabilizeMarkdownCache(): void {
  streamCache = null;
}

/**
 * Streaming entry point (task 5.3): a byte-identical incremental wrapper
 * around `stabilizeMarkdownFull`. When the new source extends the previous
 * input, only the tail after the finalized boundary is rescanned; any other
 * input (a reload, a different message, a shrunk text) takes the full scan.
 * The result equals the full scan at EVERY prefix, not just at the end —
 * the tests pin that equality over the fence/emphasis corpus.
 *
 * The parse stays synchronous (the documented first-token trade): the
 * incremental path only removes redundant rescanning, it never defers a
 * render.
 */
export function stabilizeMarkdown(source: string): string {
  const cache = streamCache;

  if (cache !== null) {
    if (source === cache.source) return cache.result;

    if (
      source.length > cache.source.length &&
      source.startsWith(cache.source)
    ) {
      const tail = source.slice(cache.boundary);
      const tailBoundary = finalizedPrefixLength(tail);
      const boundary = cache.boundary + tailBoundary;

      // Fixes insert bytes (a closer), so the finalized prefix is built by
      // scanning the newly closed region — each input byte is finalized
      // exactly once — and the still-open tail is rescanned from the clean
      // boundary. A clean boundary splits the full scan: its left side can
      // never change, and its right side scans from fresh state.
      const prefix =
        cache.prefix + stabilizeMarkdownFull(tail.slice(0, tailBoundary));

      const result = prefix + stabilizeMarkdownFull(tail.slice(tailBoundary));

      streamCache = { source, boundary, prefix, result };

      return result;
    }
  }

  const result = stabilizeMarkdownFull(source);
  const boundary = finalizedPrefixLength(source);
  const prefix = stabilizeMarkdownFull(source.slice(0, boundary));

  streamCache = { source, boundary, prefix, result };

  return result;
}
