/**
 * Knowledge menu helpers (add-knowledge-corpus): the client-side pre-gates
 * for the menu's upload (byte-identical to the corpus gate's limits — PDFs
 * 20 MiB, text 1 MiB, text+PDF kinds only, mirrors the chat-attachments
 * lane pattern so an over-cap or unsupported file rejects with the reason
 * without a round trip; the backend stays the authority), the FileReader →
 * base64 read the upload rides, and the small formatting/decoding helpers
 * the list rows and preview pane need.
 */

/** Lane caps, byte-identical to the backend's corpus gate. */
export const MAX_PDF_BYTES = 20 << 20;

export const MAX_TEXT_BYTES = 1 << 20;

/** True when a document name previews as markdown (the workspace preview's
 * rule): `.md` or `.markdown`, case-insensitive. */
export function isMarkdownName(name: string): boolean {
  return /\.(md|markdown)$/i.test(name);
}

/** The corpus gate's classification: application/pdf and text/* pass; any
 * other kind (word-processing, spreadsheets, video, images, archives) is
 * rejected with a one-line reason naming the supported kinds. Returns the
 * rejection reason, or null when the file would pass the size cap. */
export function gateRejection(
  name: string,
  mime: string,
  size: number
): string | null {
  let limit: number;

  let label: string;

  if (mime === "application/pdf") {
    limit = MAX_PDF_BYTES;
    label = "PDF";
  } else if (mime.startsWith("text/")) {
    limit = MAX_TEXT_BYTES;
    label = "text-file";
  } else {
    return `Unsupported file type "${mime || name}" — only text and PDF documents are accepted`;
  }

  if (size > limit) {
    const mb = limit >> 20;

    return `Exceeds the ${mb} MB ${label} limit`;
  }

  return null;
}

/** File → base64 (without the data-URL prefix) for the upload binding call
 * (the chat-attachments read pattern). */
export function fileToBase64(file: File): Promise<string> {
  return new Promise(
    (
      resolve: (value: string) => void,
      reject: (cause: Error) => void
    ): void => {
      const reader = new FileReader();

      reader.onload = (): void => {
        // SAFETY: readAsDataURL always resolves a data: URL string (never an
        // ArrayBuffer); an empty result means the read produced nothing.
        const result = reader.result as string;

        if (result === "") {
          reject(new Error("Could not read the file"));

          return;
        }

        const comma = result.indexOf(",");

        resolve(comma === -1 ? result : result.slice(comma + 1));
      };

      reader.onerror = (): void => reject(new Error("Could not read the file"));
      reader.readAsDataURL(file);
    }
  );
}

/** Base64 → UTF-8 text for the preview pane: atob yields a binary string,
 * so the bytes go through TextDecoder (not string conversion) to survive
 * multi-byte characters. */
export function decodeBase64ToText(base64: string): string {
  const binary = atob(base64);

  const bytes = new Uint8Array(binary.length);

  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }

  return new TextDecoder().decode(bytes);
}

/** Base64 → bytes for the PDF preview's blob. */
export function decodeBase64ToBytes(base64: string): Uint8Array<ArrayBuffer> {
  const binary = atob(base64);

  const bytes = new Uint8Array(binary.length);

  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }

  return bytes;
}

/** The error message a rejection surface shows: the cause's own message
 * when it is an Error (the binding layer's readable reasons), else the
 * caller's fallback (the scheduler view's shape). */
export function errorMessage(cause: unknown, fallback: string): string {
  return cause instanceof Error ? cause.message : fallback;
}

/** Human-readable byte size for the list rows ("1.2 KB", "3.4 MB"). */
export function formatBytes(size: number): string {
  if (size < 1024) return `${size} B`;

  const units = ["KB", "MB", "GB"];

  let value = size;

  let unit = "B";

  for (const next of units) {
    value /= 1024;

    unit = next;

    if (value < 1024) break;
  }

  return `${value >= 10 ? Math.round(value) : Math.round(value * 10) / 10} ${unit}`;
}

/** Compact creation date for the list rows ("Jan 5, 2026"). */
export function formatCreatedDate(iso: string): string {
  const at = new Date(iso).getTime();

  if (Number.isNaN(at)) return "—";

  return new Intl.DateTimeFormat(undefined, {
    day: "numeric",
    month: "short",
    year: "numeric",
  }).format(at);
}
