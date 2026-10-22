/**
 * The remaining shipped tools' renderers (task 4.2, spec chat-tool-cards
 * "Interactive tool bodies" last bullet): `http_get` and `current_time`
 * render their result as key-value pairs when structured and as text
 * otherwise; `delete_file` rides the frame with its result clamped.
 */
import { useMemo } from "react";
import type { ReactElement } from "react";
import type { ToolCallMessagePartProps } from "@assistant-ui/react";

import {
  isBoolean,
  isNumber,
  isObject,
  isString,
  parseArgs,
  parseJSONResult,
  str,
  type Json,
} from "../parse";
import { ClampedText } from "../primitives/ClampedText";
import { ToolCard } from "./ToolCallFrame";

/** The narrow type guard the bodies read results through. */
function isTextResult(value: unknown): value is string {
  return typeof value === "string";
}

/** One label/value row of the flattened result. */
interface KeyValueRow {
  label: string;
  value: string;
}

/** Type guard: a Json array of only strings. */
function isStringArray(value: Json): value is string[] {
  return Array.isArray(value) && value.every((v: Json): boolean => isString(v));
}

/** Flattens one level of a structured result into label/value rows. */
export function flattenPairs(parsed: Json): KeyValueRow[] | null {
  if (!isObject(parsed)) return null;

  const rows: KeyValueRow[] = [];

  for (const [key, value] of Object.entries(parsed)) {
    if (
      isString(value) ||
      isNumber(value) ||
      isBoolean(value) ||
      value === null
    ) {
      rows.push({
        label: key,
        value: value === null ? "null" : String(value),
      });
    } else if (isStringArray(value)) {
      rows.push({ label: key, value: value.join(", ") });
    } else {
      // Nested structures stay inspectable as JSON in the value column.
      try {
        rows.push({ label: key, value: JSON.stringify(value) });
      } catch {
        return null;
      }
    }
  }

  return rows.length > 0 ? rows : null;
}

/** The key-value pane: pairs when structured, clamped text otherwise. */
function KeyValuePane({
  resultText,
  isError,
}: {
  resultText: string;
  isError: boolean;
}): ReactElement {
  const parsed = isError ? null : parseJSONResult(resultText);
  const pairs = parsed === null ? null : flattenPairs(parsed);

  if (pairs === null) {
    return <ClampedText text={resultText} testidPrefix="kv-text" />;
  }

  return (
    <dl
      className="border-line rounded-md border px-2 py-1"
      data-testid="kv-pairs"
    >
      {pairs.map((row: KeyValueRow): ReactElement => (
        <div key={row.label} className="flex gap-2 py-0.5" data-testid="kv-row">
          <dt className="text-muted-foreground w-28 shrink-0 truncate text-[11px]">
            {row.label}
          </dt>
          <dd className="text-foreground/70 min-w-0 flex-1 break-all font-mono text-xs">
            {row.value}
          </dd>
        </div>
      ))}
    </dl>
  );
}

export function HTTPGetTool(props: ToolCallMessagePartProps): ReactElement {
  const url = useMemo((): string => {
    const args = parseArgs(props.argsText);

    return args === null ? "" : str(args, "url");
  }, [props.argsText]);

  const resultText = isTextResult(props.result) ? props.result : "";
  const isError = props.isError === true;

  return (
    <ToolCard part={props}>
      <UrlBody
        url={url}
        resultText={resultText}
        isError={isError}
        testidPrefix="httpget"
      />
    </ToolCard>
  );
}

export function CurrentTimeTool(props: ToolCallMessagePartProps): ReactElement {
  const resultText = isTextResult(props.result) ? props.result : "";
  const isError = props.isError === true;

  return (
    <ToolCard part={props}>
      <div className="px-3 pb-2" data-testid="time-body">
        {resultText !== "" ? (
          <KeyValuePane resultText={resultText} isError={isError} />
        ) : null}
      </div>
    </ToolCard>
  );
}

export function DeleteFileTool(props: ToolCallMessagePartProps): ReactElement {
  const resultText = isTextResult(props.result) ? props.result : "";

  return (
    <ToolCard part={props}>
      <div className="px-3 pb-2" data-testid="delete-body">
        {resultText !== "" ? (
          <ClampedText text={resultText} testidPrefix="delete-result" />
        ) : null}
      </div>
    </ToolCard>
  );
}

function UrlBody({
  url,
  resultText,
  isError,
  testidPrefix,
}: {
  url: string;
  resultText: string;
  isError: boolean;
  testidPrefix: string;
}): ReactElement {
  if (url === "") throw new Error("http_get: no url in arguments");

  return (
    <div className="space-y-1.5 px-3 pb-2" data-testid={`${testidPrefix}-body`}>
      <span
        className="border-line text-foreground/70 inline-flex max-w-full items-center truncate rounded-md border px-1.5 py-0.5 font-mono text-[11px]"
        data-testid={`${testidPrefix}-url`}
      >
        {url}
      </span>
      {resultText !== "" ? (
        <KeyValuePane resultText={resultText} isError={isError} />
      ) : null}
    </div>
  );
}
