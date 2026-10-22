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
 * never counted: CommonMark treats that content as literal. Pure function —
 * no imports, no state.
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
 */
export function stabilizeMarkdown(source: string): string {
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
