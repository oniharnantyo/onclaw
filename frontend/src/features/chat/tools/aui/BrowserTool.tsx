/**
 * BrowserTool (task 4.2, design D6): the browser tool's body, keyed by the
 * result's typed JSON shape — navigation and action results show the page
 * address and title with the action's outcome, a snapshot renders the
 * elements as a ref/role/name table, and a screenshot result shows the
 * captured image as a thumbnail that opens the lightbox. Screenshot bytes
 * ride the bound `BrowserScreenshot` read (spec "Browser screenshot
 * access": basename-shaped names only, resolved inside the tool's
 * screenshots directory, never a filesystem path in the webview) and render
 * from a Blob object URL; a refused or missing read degrades to an
 * unavailable state.
 */
import { useEffect, useMemo, useState } from "react";
import type { ReactElement } from "react";
import { ImageOff } from "lucide-react";
import type { ToolCallMessagePartProps } from "@assistant-ui/react";

import { client, type BrowserScreenshotContent } from "../../../../lib/client";

import {
  isNumber,
  isObject,
  isString,
  parseArgs,
  parseJSONResult,
  str,
  type Json,
} from "../parse";
import { Lightbox } from "../primitives/Lightbox";
import { ClampedText } from "../primitives/ClampedText";
import { ToolCard } from "./ToolCallFrame";

/** The narrow type guard the body reads results through. */
function isTextResult(value: unknown): value is string {
  return typeof value === "string";
}

/** One snapshot element as persisted (browser_types.go). */
interface BrowserElement {
  ref: string;
  role: string;
  name: string;
}

/** Validates one wire element; null when the entry breaks the shape. */
function toElement(entry: Json): BrowserElement | null {
  if (!isObject(entry)) return null;

  if (!isString(entry.ref) || !isString(entry.role)) return null;

  return {
    ref: entry.ref,
    role: entry.role,
    name: isString(entry.name) ? entry.name : "",
  };
}

/** Validates a wire elements array; null on any broken entry. */
function toElements(list: Json[]): BrowserElement[] | null {
  const elements: BrowserElement[] = [];

  for (const entry of list) {
    const element = toElement(entry);

    if (element === null) return null;

    elements.push(element);
  }

  return elements;
}

/** The elements table (spec "Browser snapshot table"): reference, role,
 * name. Large snapshots scroll in place. */
function ElementsTable({
  elements,
}: {
  elements: BrowserElement[];
}): ReactElement | null {
  if (elements.length === 0) return null;

  return (
    <div
      className="border-line max-h-56 overflow-auto rounded-md border"
      data-testid="browser-elements"
    >
      <table className="w-full border-collapse text-xs">
        <thead>
          <tr className="border-line text-muted-foreground border-b text-left text-[10px] uppercase tracking-wide">
            <th className="px-2 py-1 font-medium">Ref</th>
            <th className="px-2 py-1 font-medium">Role</th>
            <th className="px-2 py-1 font-medium">Name</th>
          </tr>
        </thead>
        <tbody>
          {elements.map((el: BrowserElement, i: number): ReactElement => (
            <tr
              key={`${el.ref}-${i}`}
              className="border-line border-b last:border-b-0"
            >
              <td
                className="text-foreground/70 px-2 py-0.5 font-mono text-[11px]"
                data-testid="browser-element-ref"
              >
                {el.ref}
              </td>
              <td className="text-foreground/70 px-2 py-0.5">{el.role}</td>
              <td className="text-foreground/70 px-2 py-0.5">{el.name}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** The exact name shape the browser tool writes (design D6). */
const SCREENSHOT_NAME = /^\d+\.png$/;

/** The screenshot thumbnail: the bound read returns base64 PNG; the bytes
 * become a Blob object URL; the thumbnail sizes from the tool's reported
 * width/height so the transcript does not shift on arrival. */
function ScreenshotThumb({
  screenshot,
}: {
  screenshot: { path: string; width?: number; height?: number };
}): ReactElement {
  // The bound read addresses the file by basename only (spec: screenshots
  // are "addressed by the file name … resolve them solely inside the
  // screenshots directory"), so the guard tests the basename shape; a path
  // carrying directories passes only if its basename is a screenshot name.
  const base = screenshot.path.split("/").pop() ?? "";
  const valid = SCREENSHOT_NAME.test(base);

  const [url, setURL] = useState<string | null>(null);
  const [failed, setFailed] = useState(!valid);
  const [open, setOpen] = useState(false);

  useEffect((): void | (() => void) => {
    if (!valid) return undefined;

    let mounted = true;
    let created: string | null = null;

    client.chat
      .browserScreenshot(base)
      .then((content: BrowserScreenshotContent): void => {
        if (!mounted || !isString(content?.data)) {
          if (mounted) setFailed(true);

          return;
        }

        const binary = window.atob(content.data);
        const bytes = new Uint8Array(binary.length);

        for (let i = 0; i < binary.length; i += 1) {
          bytes[i] = binary.charCodeAt(i);
        }

        created = URL.createObjectURL(new Blob([bytes], { type: "image/png" }));
        setURL(created);
      })
      .catch((): void => {
        if (mounted) setFailed(true);
      });

    return (): void => {
      mounted = false;

      if (created) URL.revokeObjectURL(created);
    };
  }, [base, valid]);

  if (failed || !url) {
    return (
      <span
        className="border-line text-muted-foreground inline-flex items-center gap-1.5 rounded-md border px-2 py-1.5 text-[12px]"
        data-testid="screenshot-unavailable"
      >
        <ImageOff aria-hidden="true" className="size-3.5" />
        Screenshot unavailable
      </span>
    );
  }

  return (
    <div>
      <button
        type="button"
        className="border-line block overflow-hidden rounded-md border"
        data-testid="screenshot-thumb"
        aria-label="Enlarge screenshot"
        onClick={(): void => setOpen(true)}
      >
        <img
          src={url}
          alt="Browser screenshot"
          width={screenshot.width}
          height={screenshot.height}
          className="max-h-40 max-w-56 object-contain"
          data-testid="screenshot-image"
        />
      </button>
      {open ? (
        <Lightbox
          src={url}
          alt={`Screenshot ${base}`}
          onClose={(): void => setOpen(false)}
        />
      ) : null}
    </div>
  );
}

export function BrowserTool(props: ToolCallMessagePartProps): ReactElement {
  const action = useMemo((): string => {
    const args = parseArgs(props.argsText);

    return args === null ? "" : str(args, "action");
  }, [props.argsText]);

  const resultText = isTextResult(props.result) ? props.result : "";
  const isError = props.isError === true;

  return (
    <ToolCard part={props}>
      <BrowserBody action={action} resultText={resultText} isError={isError} />
    </ToolCard>
  );
}

function BrowserBody({
  action,
  resultText,
  isError,
}: {
  action: string;
  resultText: string;
  isError: boolean;
}): ReactElement {
  if (action === "") throw new Error("browser: no action in arguments");

  // The screenshot body is driven by the args action, not just the shape —
  // a navigate result is also `{url,title}`.
  if (action === "screenshot") {
    const parsed = parseJSONResult(resultText);
    const path = isObject(parsed) && isString(parsed.path) ? parsed.path : null;

    if (path === null || isError) {
      return (
        <div className="px-3 pb-2" data-testid="browser-body">
          <span
            className="border-line text-muted-foreground inline-flex items-center gap-1.5 rounded-md border px-2 py-1.5 text-[12px]"
            data-testid="screenshot-unavailable"
          >
            <ImageOff aria-hidden="true" className="size-3.5" />
            Screenshot unavailable
          </span>
          {isError && resultText !== "" ? (
            <ClampedText text={resultText} testidPrefix="browser-error" />
          ) : null}
        </div>
      );
    }

    const width =
      isObject(parsed) && isNumber(parsed.width) ? parsed.width : undefined;

    const height =
      isObject(parsed) && isNumber(parsed.height) ? parsed.height : undefined;

    return (
      <div className="px-3 pb-2" data-testid="browser-body">
        <ScreenshotThumb screenshot={{ path, width, height }} />
      </div>
    );
  }

  const parsed = parseJSONResult(resultText);

  if (!isObject(parsed)) {
    return (
      <div className="px-3 pb-2" data-testid="browser-body">
        <ClampedText text={resultText} testidPrefix="browser-plain" />
      </div>
    );
  }

  // The cookies result renders through the same table, one row per cookie.
  const cookieList = Array.isArray(parsed.cookies) ? parsed.cookies : [];

  const cookies =
    cookieList.length > 0 && !Array.isArray(parsed.elements)
      ? toElements(
          cookieList.map((cookie: Json): Json =>
            isObject(cookie)
              ? {
                  ref: isString(cookie.name) ? cookie.name : "",
                  role: isString(cookie.domain) ? cookie.domain : "cookie",
                  name: isString(cookie.value) ? cookie.value : "",
                }
              : { ref: "", role: "cookie", name: "" }
          )
        )
      : null;

  if (cookies !== null && cookies.length > 0) {
    return (
      <div className="px-3 pb-2" data-testid="browser-body">
        <ElementsTable elements={cookies} />
      </div>
    );
  }

  const url = isString(parsed.url) ? parsed.url : "";
  const title = isString(parsed.title) ? parsed.title : "";
  const ok = parsed.ok === true ? "ok" : parsed.ok === false ? "failed" : null;

  const elementList = Array.isArray(parsed.elements)
    ? toElements(parsed.elements)
    : [];

  const elements = elementList ?? [];

  return (
    <div className="space-y-1.5 px-3 pb-2" data-testid="browser-body">
      <div className="flex items-center gap-2">
        {url !== "" ? (
          <span
            className="border-line text-foreground/70 inline-flex max-w-full items-center truncate rounded-md border px-1.5 py-0.5 font-mono text-[11px]"
            data-testid="browser-url"
          >
            {url}
          </span>
        ) : null}
        {ok !== null ? (
          <span
            className={`rounded-[6px] border px-1.5 py-0.5 text-[10px] leading-4 ${
              ok === "ok"
                ? "border-line text-foreground/70"
                : "border-danger/35 text-dangertext"
            }`}
            data-testid="browser-ok"
          >
            {ok}
          </span>
        ) : null}
      </div>
      {title !== "" ? (
        <p className="text-foreground text-xs" data-testid="browser-title">
          {title}
        </p>
      ) : null}
      <ElementsTable elements={elements} />
    </div>
  );
}
