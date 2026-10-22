/**
 * Tool payload parsing (design D2): the only doors from the raw
 * ToolActivity strings into structured data. Both are total functions — a
 * malformed payload returns `null`, never throws — so callers fall back to
 * the raw text pane instead of blanking the card (spec chat-tool-cards
 * "Honest fallback"). Value checks run through the shared type guards; no
 * representation check guesses a contract it has not established.
 */

/** Any JSON value the runtime can deliver in a tool payload. */
export type Json = string | number | boolean | null | Json[] | JsonObject;

/** One JSON object: every property is again a Json value. */
export interface JsonObject {
  [key: string]: Json;
}

/** The parsed arguments object of one tool call. */
export type ToolArgs = JsonObject;

/** The parsed result payload of one tool call (null = not JSON). */
export type ParsedResult = Json;

/** Type guard: a Json value that is a string. */
export function isString(value: Json | undefined): value is string {
  return typeof value === "string";
}

/** Type guard: a Json value that is a finite number. */
export function isNumber(value: Json | undefined): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

/** Type guard: a Json value that is a boolean. */
export function isBoolean(value: Json | undefined): value is boolean {
  return typeof value === "boolean";
}

/** Type guard: a Json value that is an object (never an array or null). */
export function isObject(value: Json | undefined): value is JsonObject {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/**
 * Parses a call's `arguments` string as a JSON object. Whitespace-only or
 * absent arguments yield an empty object (calls like `current_time` carry
 * none); any other non-object JSON — and any parse failure — yields null.
 */
export function parseArgs(
  argsText: string | undefined | null
): ToolArgs | null {
  if (argsText === undefined || argsText === null) return {};

  const trimmed = argsText.trim();

  if (trimmed === "") return {};

  let parsed: Json;

  try {
    // SAFETY: the try/catch bounds the parse; the isObject guard below
    // establishes the object contract before the value escapes this module.
    parsed = JSON.parse(trimmed) as Json;
  } catch {
    return null;
  }

  return isObject(parsed) ? parsed : null;
}

/**
 * Parses a call's `result` string as JSON of any shape (the browser tool,
 * web_search, and todo_write return typed JSON; the file tools return plain
 * text). Absent or empty results yield null (nothing to show); a string that
 * is not JSON — the overwhelmingly common case — yields null too, so callers
 * treat the raw text as the payload.
 */
export function parseJSONResult(
  result: string | undefined | null
): ParsedResult | null {
  if (result === undefined || result === null) return null;

  const trimmed = result.trim();

  if (trimmed === "") return null;

  try {
    // SAFETY: the try/catch bounds the parse; every consumer re-narrows the
    // value through the isObject/isString guards before trusting it.
    return JSON.parse(trimmed) as ParsedResult;
  } catch {
    return null;
  }
}

/**
 * Reads a string field off parsed arguments with a default. A wrong-typed or
 * missing field degrades to the default instead of throwing.
 */
export function str(
  record: ToolArgs | null,
  key: string,
  fallback: string = ""
): string {
  const value = record?.[key];

  return isString(value) ? value : fallback;
}

/** Reads a boolean field off parsed arguments with a default. */
export function bool(
  record: ToolArgs | null,
  key: string,
  fallback: boolean = false
): boolean {
  const value = record?.[key];

  return isBoolean(value) ? value : fallback;
}

/** Reads a finite number field off parsed arguments with a default. */
export function num(
  record: ToolArgs | null,
  key: string,
  fallback: number = 0
): number {
  const value = record?.[key];

  return isNumber(value) ? value : fallback;
}

/** The shape `duration_ms` rides on once the parallel-wave binding lands. */
interface WithDuration {
  duration_ms?: Json;
}

/**
 * Reads `duration_ms` off a tool call defensively (design D5). The field is
 * optional on the generated binding; a zero value marks a record persisted
 * before durations were recorded — such a record shows no latency rather
 * than a fabricated one, so zero reads as absent like any wrong-typed or
 * missing field.
 */
export function callDurationMs(
  call: WithDuration | null | undefined
): number | undefined {
  const value = call?.duration_ms;

  return isNumber(value) && value > 0 ? value : undefined;
}
