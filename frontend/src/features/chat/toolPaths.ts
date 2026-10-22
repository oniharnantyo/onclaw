/**
 * Path-token extraction for tool-card link wiring (task 8.3, spec
 * chat-panels "Preview sources"): tool outputs carry the workspace paths the
 * agent's file tools address — mount-scoped ("/workspace/notes.md"), plain
 * absolute ("/etc/passwd"), or workspace-relative (".tasks/task-1.md"). The
 * splitter finds those tokens so the cards can render them as preview links;
 * the mapping hands the jailed reader a workspace-relative path whenever the
 * token names the mount, and passes anything else through unchanged — the
 * backend refuses paths outside the workspace, which the preview renders as
 * the refused outcome.
 */

/** The absolute prefix the agent's file tools address the workspace by
 * (backend DefaultMountPoint). */
export const WORKSPACE_MOUNT = "/workspace";

/**
 * Path-shaped tokens: an absolute POSIX path (leading "/"), or a
 * slash-carrying relative path whose final segment has an extension (keeps
 * plain words, versions like "5/5", and date-like fragments unlinked). The
 * lookbehind keeps URL segments ("https://…", "example.com/foo") and the
 * middle of longer tokens from matching.
 */
const PATH_PATTERN =
  /(?<![\w:/.-])(\/[\w.-]+(?:\/[\w.-]+)*|[\w.-]*\/[\w.-]+(?:\/[\w.-]+)*\.[A-Za-z0-9]+)/g;

/** Sentence punctuation a token may swallow before a real path ends. */
const TRAILING_PUNCTUATION = /[.,;:)\]]+$/;

/** One chunk of tool output text: plain text, or a linkable path token. */
export interface PathSegment {
  text: string;
  /** The path token, or null for plain text between tokens. */
  path: string | null;
}

/** Splits tool output into plain segments and path tokens, so cards can
 * render the tokens as right-panel preview links without reshaping the
 * surrounding text. Trailing punctuation is left as plain text. */
export function splitPaths(text: string): PathSegment[] {
  const segments: PathSegment[] = [];

  let cursor = 0;

  for (const match of text.matchAll(PATH_PATTERN)) {
    const start = match.index ?? 0;
    const token = match[0].replace(TRAILING_PUNCTUATION, "");

    // The whole match was punctuation (never true today, but the trim above
    // is only safe while something remains).
    if (token === "") continue;

    if (start > cursor)
      segments.push({ text: text.slice(cursor, start), path: null });

    segments.push({ text: token, path: token });
    cursor = start + token.length;
  }

  if (cursor < text.length)
    segments.push({ text: text.slice(cursor), path: null });

  return segments;
}

/** Maps a raw output token to the workspace-relative path the jailed reader
 * receives (task 8.3): the /workspace mount prefix is stripped; foreign
 * absolute paths pass through and are refused by the backend. */
export function toWorkspaceRel(token: string): string {
  if (token === WORKSPACE_MOUNT) return "";

  if (token.startsWith(`${WORKSPACE_MOUNT}/`))
    return token.slice(WORKSPACE_MOUNT.length + 1);

  return token;
}
