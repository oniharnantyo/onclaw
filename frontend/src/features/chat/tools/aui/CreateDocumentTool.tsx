/**
 * CreateDocumentTool (add-create-document task 6.1): the create_document
 * tool's body, keyed by the result's typed JSON shape — a successful call
 * shows the generated PDF's file name and size and renders the document
 * inline. Bytes ride the bound `generatedDocument` read (spec
 * "Generated document access": addressed by the plain workspace-relative
 * file name the tool reported and the active agent's workspace; the
 * on-disk absolute path never crosses to the webview and the body never
 * derives one) and render through the app's existing PDF path, the
 * knowledge panel's data-URL `<object>` embed. A refused or missing read —
 * a file deleted from the workspace, no active agent — degrades to a
 * readable failure state, and a rejected call (`ok:false`) shows the
 * backend's readable error.
 */
import { useEffect, useState } from "react";
import type { ReactElement } from "react";
import { FileX } from "lucide-react";
import type { ToolCallMessagePartProps } from "@assistant-ui/react";

import { client, type GeneratedDocumentContent } from "../../../../lib/client";

import { isNumber, isObject, isString, parseJSONResult } from "../parse";
import { ClampedText } from "../primitives/ClampedText";
import { formatBytes } from "../../components/AttachmentView";
import { useChatStore } from "../../store";
import type { ChatStore } from "../../types";
import { ToolCard } from "./ToolCallFrame";

/** The narrow type guard the body reads results through. */
function isTextResult(value: unknown): value is string {
  return typeof value === "string";
}

/** The plain-name shape the bound read accepts (spec: no directory
 * components) — mirrored before the call so a malformed name is refused
 * without touching the backend. */
const PLAIN_DOC_NAME = /^[^/\\]+$/;

/** The unavailable state: the read was refused or the file is gone. */
function Unavailable(): ReactElement {
  return (
    <span
      className="border-line text-muted-foreground inline-flex items-center gap-1.5 rounded-md border px-2 py-1.5 text-[12px]"
      data-testid="create-doc-unavailable"
    >
      <FileX aria-hidden="true" className="size-3.5" />
      Document unavailable — it may have been removed from the workspace
    </span>
  );
}

/** The fetched document's render state. */
interface PdfState {
  loading: boolean;
  /** The document's base64 bytes once fetched; null when unavailable. */
  base64: string | null;
}

/** The generated PDF: fetched through the bound read by the reported file
 * name and the active agent's workspace, rendered inline through the
 * app's existing data-URL `<object>` embed (the knowledge panel's PDF
 * path). Fetches only while the card body is mounted. */
function PdfDocument({ name }: { name: string }): ReactElement {
  const agentID = useChatStore((s: ChatStore) => s.activeAgentID);

  // Without an active agent there is no workspace to read from, and a name
  // carrying directory components is refused before any call — both render
  // the unavailable state directly, never a path-shaped request.
  const refused = agentID === null || !PLAIN_DOC_NAME.test(name);

  const [state, setState] = useState<PdfState>({
    loading: true,
    base64: null,
  });

  useEffect((): void | (() => void) => {
    if (agentID === null || !PLAIN_DOC_NAME.test(name)) return undefined;

    let mounted = true;

    client.chat
      .generatedDocument(agentID, name)
      .then((content: GeneratedDocumentContent): void => {
        if (!mounted) return;

        if (!isString(content?.data)) {
          setState({ loading: false, base64: null });

          return;
        }

        setState({ loading: false, base64: content.data });
      })
      .catch((): void => {
        if (mounted) setState({ loading: false, base64: null });
      });

    return (): void => {
      mounted = false;
    };
  }, [agentID, name]);

  if (refused || (!state.loading && state.base64 === null)) {
    return <Unavailable />;
  }

  if (state.loading) {
    return (
      <p
        className="text-muted-foreground text-[12px]"
        data-testid="create-doc-loading"
      >
        Loading document…
      </p>
    );
  }

  return (
    <object
      className="border-line h-96 w-full rounded-md border"
      type="application/pdf"
      aria-label={`Generated document ${name}`}
      data={`data:application/pdf;base64,${state.base64}`}
      data-testid="create-doc-pdf"
    >
      <p className="text-muted-foreground text-xs">
        This view cannot display the PDF inline.
      </p>
    </object>
  );
}

/** The successful call's body: the reported file name and size above the
 * inline PDF. */
function DocumentBody({
  filename,
  size,
}: {
  filename: string;
  size: number | null;
}): ReactElement {
  return (
    <div className="space-y-1.5 px-3 pb-2" data-testid="create-doc-body">
      <div className="flex items-center gap-2">
        <span
          className="border-line text-foreground/70 inline-flex max-w-full items-center truncate rounded-md border px-1.5 py-0.5 font-mono text-[11px]"
          data-testid="create-doc-name"
        >
          {filename}
        </span>
        {size !== null ? (
          <span
            className="text-muted-foreground shrink-0 text-[11px] tabular-nums"
            data-testid="create-doc-size"
          >
            {formatBytes(size)}
          </span>
        ) : null}
      </div>
      <PdfDocument name={filename} />
    </div>
  );
}

function CreateDocumentBody({
  resultText,
  isError,
}: {
  resultText: string;
  isError: boolean;
}): ReactElement {
  if (isError) {
    return (
      <div className="px-3 pb-2" data-testid="create-doc-body">
        {resultText !== "" ? (
          <ClampedText text={resultText} testidPrefix="create-doc-error" />
        ) : null}
      </div>
    );
  }

  const parsed = parseJSONResult(resultText);

  // A result that is not the typed JSON (a legacy plain-text record) shows
  // verbatim, like the other bodies' honest degradation.
  if (!isObject(parsed)) {
    return (
      <div className="px-3 pb-2" data-testid="create-doc-body">
        {resultText !== "" ? (
          <ClampedText text={resultText} testidPrefix="create-doc-plain" />
        ) : null}
      </div>
    );
  }

  // The tool reports every rejection (bad name, unknown preset, rendering
  // failure) as a readable {ok:false} result, never a failed call.
  if (parsed.ok === false) {
    return (
      <div className="px-3 pb-2" data-testid="create-doc-body">
        <p
          className="border-danger/35 text-dangertext rounded-md border px-2 py-1 text-xs"
          data-testid="create-doc-rejected"
        >
          {isString(parsed.error)
            ? parsed.error
            : "The document was not created"}
        </p>
      </div>
    );
  }

  const filename = isString(parsed.filename) ? parsed.filename : "";
  const size = isNumber(parsed.size) ? parsed.size : null;

  if (filename === "")
    throw new Error("create_document: no filename in result");

  return <DocumentBody filename={filename} size={size} />;
}

export function CreateDocumentTool(
  props: ToolCallMessagePartProps
): ReactElement {
  const resultText = isTextResult(props.result) ? props.result : "";
  const isError = props.isError === true;

  return (
    <ToolCard part={props}>
      <CreateDocumentBody resultText={resultText} isError={isError} />
    </ToolCard>
  );
}
