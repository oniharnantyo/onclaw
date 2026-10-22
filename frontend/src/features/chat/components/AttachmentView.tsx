/**
 * AttachmentView (task 5.5, design D5): one transcript attachment — an inline
 * thumbnail for images, a document chip (name/type/size) for everything
 * else. Content is fetched LAZILY on mount through the capability-gated
 * binding call and rendered from a blob object URL that is revoked on
 * unmount; the capability key is the only address involved, never a raw
 * filesystem path. A key the backend refuses (gone, foreign) renders as an
 * unavailable chip instead of breaking the row.
 */
import { useEffect, useState } from "react";
import type { ReactElement } from "react";
import { FileText, TriangleAlert } from "lucide-react";

import type { Attachment as SessionAttachment } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { AttachmentContent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { cn } from "../../../lib/cn";
import { client } from "../../../lib/client";

/** Compact byte size for chip meta (512 B / 1.2 MB). */
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;

  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10 * 1024 ? 1 : 0)} KB`;

  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

/** The lazy object URL state behind one capability key (fetched on mount). */
interface AttachmentURLState {
  /** The blob object URL once fetched; null while loading or refused. */
  url: string | null;
  /** True when the backend refused the key (gone, foreign). */
  failed: boolean;
}

/** The lazy object URL behind one capability key (fetched on mount). */
export function useAttachmentURL(key: string): AttachmentURLState {
  const [url, setURL] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect((): (() => void) => {
    let mounted = true;
    let created: string | null = null;

    client.chat
      .attachmentContent(key)
      .then((content: AttachmentContent): void => {
        if (!mounted) return;

        // base64 → bytes → Blob → object URL; the renderer only ever holds
        // the blob URL, never a path.
        const binary = window.atob(content.data);
        const bytes = new Uint8Array(binary.length);

        for (let i = 0; i < binary.length; i += 1) {
          bytes[i] = binary.charCodeAt(i);
        }

        created = URL.createObjectURL(
          new Blob([bytes], { type: content.mime })
        );
        setURL(created);
      })
      .catch((): void => {
        if (mounted) setFailed(true);
      });

    return (): void => {
      mounted = false;

      if (created) URL.revokeObjectURL(created);
    };
  }, [key]);

  return { url, failed };
}

/** Image attachment: a small inline thumbnail, click to open. */
function ImageThumb({
  attachment,
  url,
  failed,
}: {
  attachment: SessionAttachment;
  url: string | null;
  failed: boolean;
}): ReactElement {
  if (failed || !url) {
    return (
      <span
        className="flex items-center gap-1.5 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] px-2 py-1.5 text-[12px] text-muted"
        data-testid="attachment-unavailable"
      >
        <TriangleAlert aria-hidden="true" className="size-3.5" />
        {attachment.name} unavailable
      </span>
    );
  }

  return (
    <a
      href={url}
      download={attachment.name}
      target="_blank"
      rel="noreferrer"
      className="block overflow-hidden rounded-md border border-line"
      data-testid="attachment-image"
      aria-label={`Open image ${attachment.name}`}
    >
      <img
        src={url}
        alt={attachment.name}
        className="max-h-40 max-w-56 object-contain"
      />
    </a>
  );
}

/** Document attachment: an icon chip with name, type, and size, click to
 * download through the object URL. */
function DocumentChip({
  attachment,
  url,
  failed,
}: {
  attachment: SessionAttachment;
  url: string | null;
  failed: boolean;
}): ReactElement {
  return (
    <a
      href={failed || !url ? undefined : url}
      download={attachment.name}
      target="_blank"
      rel="noreferrer"
      className={cn(
        "flex max-w-64 items-center gap-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] px-2.5 py-1.5",
        url &&
          !failed &&
          "hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)]"
      )}
      data-testid="attachment-document"
    >
      {failed ? (
        <TriangleAlert
          aria-hidden="true"
          className="size-4 shrink-0 text-danger"
        />
      ) : (
        <FileText aria-hidden="true" className="size-4 shrink-0 text-muted" />
      )}
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[12px] font-medium text-fg2">
          {attachment.name}
        </span>
        <span
          className="block font-mono text-[10px] text-muted"
          data-testid="attachment-meta"
        >
          {failed
            ? "Unavailable"
            : `${attachment.mime || "file"} · ${formatBytes(attachment.size)}`}
        </span>
      </span>
    </a>
  );
}

export function AttachmentView({
  attachment,
}: {
  attachment: SessionAttachment;
}): ReactElement {
  const { url, failed } = useAttachmentURL(attachment.key);
  const isImage = attachment.mime.startsWith("image/");

  return isImage ? (
    <ImageThumb attachment={attachment} url={url} failed={failed} />
  ) : (
    <DocumentChip attachment={attachment} url={url} failed={failed} />
  );
}
