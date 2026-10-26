/**
 * DocumentPreview (add-knowledge-corpus, "Knowledge management menu"): the
 * selected document's content — text documents render as decoded text;
 * `.md`/`.markdown` names grow the Raw|Preview segmented toggle (Raw the
 * default, rendered view through the chat MarkdownBody pipeline, matching
 * the workspace preview's behavior); PDFs embed as a document via a blob
 * URL (blob, not a data URL: WKWebView navigates data URLs only up to a
 * size limit, and 20 MiB PDFs exceed it). The parent keys this component by
 * document id, so a mount is always a fresh fetch and the toggle state
 * resets per selection.
 */
import { useEffect, useMemo, useRef, useState } from "react";
import type { ReactElement } from "react";

import { MarkdownBody } from "../../../components/assistant-ui/elements/MarkdownBody";
import { LoadingHint } from "../../../components/EmptyState";
import { client, type KnowledgeDocument } from "../../../lib/client";
import { cn } from "../../../lib/cn";
import {
  decodeBase64ToBytes,
  decodeBase64ToText,
  errorMessage,
  isMarkdownName,
} from "../helpers";

interface PreviewState {
  loading: boolean;
  /** The document's base64 bytes once read. */
  base64: string | null;
  /** The blob URL backing the PDF embed (PDF documents only). */
  pdfUrl: string | null;
  error: string | null;
}

export function DocumentPreview({
  doc,
}: {
  doc: KnowledgeDocument;
}): ReactElement {
  const [state, setState] = useState<PreviewState>({
    loading: true,
    base64: null,
    pdfUrl: null,
    error: null,
  });

  // Raw stays the default (the workspace preview's behavior); the rendered
  // markdown view is opt-in per selection.
  const [renderMarkdown, setRenderMarkdown] = useState(false);

  // The blob URL is revoked on unmount — a ref, because the URL is created
  // inside the fetch callback and the cleanup runs after it.
  const pdfUrlRef = useRef<string | null>(null);

  const isPdf = doc.mime === "application/pdf";

  useEffect((): (() => void) => {
    let live = true;

    client.knowledge
      .content(doc.id)
      .then((base64: string): void => {
        if (!live) return;

        let pdfUrl: string | null = null;

        if (isPdf) {
          const url = URL.createObjectURL(
            new Blob([decodeBase64ToBytes(base64)], { type: "application/pdf" })
          );

          pdfUrl = url;
          pdfUrlRef.current = url;
        }

        setState({ loading: false, base64, pdfUrl, error: null });
      })
      .catch((err: Error): void => {
        if (!live) return;

        setState({
          loading: false,
          base64: null,
          pdfUrl: null,
          error: errorMessage(err, "Failed to read the document"),
        });
      });

    return (): void => {
      live = false;

      if (pdfUrlRef.current !== null) {
        URL.revokeObjectURL(pdfUrlRef.current);
        pdfUrlRef.current = null;
      }
    };
  }, [doc.id, isPdf]);

  const text = useMemo((): string => {
    if (state.base64 === null || isPdf) return "";

    return decodeBase64ToText(state.base64);
  }, [state.base64, isPdf]);

  if (state.loading) {
    return (
      <LoadingHint
        label="Loading preview…"
        testId="knowledge-preview-loading"
      />
    );
  }

  if (state.error !== null) {
    return (
      <p
        className="text-sm text-danger"
        role="alert"
        data-testid="knowledge-preview-error"
      >
        {state.error}
      </p>
    );
  }

  if (isPdf) {
    return state.pdfUrl !== null ? (
      <object
        data={state.pdfUrl}
        type="application/pdf"
        className="h-[480px] w-full rounded-md border border-line"
        data-testid="knowledge-preview-pdf"
      >
        <p className="text-[13px] text-muted">
          The PDF cannot be displayed here — the viewer is unavailable.
        </p>
      </object>
    ) : (
      <p
        className="text-[13px] text-muted"
        data-testid="knowledge-preview-empty"
      >
        Empty document
      </p>
    );
  }

  if (state.base64 === null || text === "") {
    return (
      <p
        className="text-[13px] text-muted"
        data-testid="knowledge-preview-empty"
      >
        Empty document
      </p>
    );
  }

  return (
    <div data-testid="knowledge-preview">
      {isMarkdownName(doc.filename) ? (
        <div
          className="mb-2 inline-flex items-center gap-0.5 rounded-md border border-line p-0.5"
          data-testid="knowledge-preview-modes"
        >
          <button
            type="button"
            className={cn(
              "cursor-pointer rounded px-1.5 py-0.5 text-[11px]",
              renderMarkdown
                ? "text-muted"
                : "bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] text-fg"
            )}
            data-testid="knowledge-preview-mode-raw"
            onClick={() => setRenderMarkdown(false)}
          >
            Raw
          </button>
          <button
            type="button"
            className={cn(
              "cursor-pointer rounded px-1.5 py-0.5 text-[11px]",
              renderMarkdown
                ? "bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] text-fg"
                : "text-muted"
            )}
            data-testid="knowledge-preview-mode-preview"
            onClick={() => setRenderMarkdown(true)}
          >
            Preview
          </button>
        </div>
      ) : null}
      {renderMarkdown ? (
        <div data-testid="knowledge-preview-markdown">
          <MarkdownBody text={text} />
        </div>
      ) : (
        <pre
          className="whitespace-pre-wrap break-words font-mono text-xs text-fg2"
          data-testid="knowledge-preview-content"
        >
          {text}
        </pre>
      )}
    </div>
  );
}
