/**
 * Attachment chip tray store (task 5.4, design D5): the composer's pending
 * uploads as a small dedicated store — the chips must re-render both the
 * composer's tray and the transcript's drop surface, while the chat store's
 * public shape (types.ts) sits outside this wave's file scope.
 *
 * Caps mirror the backend's lane classifier exactly (internal/attachments:
 * at most 4 files per message; images 5 MB, PDFs 20 MB, videos 20 MB
 * (design D6), text 1 MB; a mime in no lane is unsupported). The client-side check runs BEFORE the upload so an
 * over-cap or unsupported file rejects with the reason without a round trip;
 * the backend stays the authority. Upload progress rides the base64 binding
 * call, which exposes no progress callback — the chip shows an honest
 * indeterminate uploading state, with cancel (drop before resolution) and
 * retry (re-attempt after failure).
 */
import { create, type StoreApi } from "zustand";

import { client } from "../../lib/client";

/** One pending upload in the composer's tray. */
export interface AttachmentChip {
  id: string;
  name: string;
  mime: string;
  size: number;
  /** uploading → ready (capability key set) or rejected (reason set). */
  status: "uploading" | "ready" | "rejected";
  /** The backend's rejection or failure reason (rejected only). */
  reason?: string;
  /** The capability key once stored — the send path's only handle. */
  key?: string;
  /** True when a retry can succeed: an upload failure, not a cap rejection
   * (the same file would reject again). */
  retryable?: boolean;
  /** The original file handle backing retry (never rendered). */
  file: File;
}

export type AttachmentChipState = {
  chips: AttachmentChip[];
  addFiles: (files: File[]) => Promise<void>;
  remove: (id: string) => void;
  retry: (id: string) => Promise<void>;
  /** Clears ready (already sent) and rejected chips; uploading ones stay. */
  clearFinished: () => void;
  /** Drops everything (session switch, sent message teardown). */
  clear: () => void;
};

/** Per-message file-count cap (spec chat-attachments). */
export const MAX_MESSAGE_FILES = 4;

/** Lane caps, byte-identical to internal/attachments. */
export const MAX_IMAGE_BYTES = 5 << 20;

export const MAX_PDF_BYTES = 20 << 20;

export const MAX_VIDEO_BYTES = 20 << 20;

export const MAX_TEXT_BYTES = 1 << 20;

/** The lane's label in the over-cap reason ("PDF", "image", "video",
 * "text-file") — a helper because four branches outgrew the ternary. */
function laneLabel(mime: string): string {
  if (mime === "application/pdf") return "PDF";

  if (mime.startsWith("image/")) return "image";

  if (mime.startsWith("video/")) return "video";

  return "text-file";
}

/** The lane classification the backend applies: application/pdf, image/*,
 * video/*, text/*; anything else is unsupported. Returns the rejection
 * reason or null when the file would pass the size cap. */
export function laneRejection(
  name: string,
  mime: string,
  size: number
): string | null {
  let limit: number;

  if (mime === "application/pdf") {
    limit = MAX_PDF_BYTES;
  } else if (mime.startsWith("image/")) {
    limit = MAX_IMAGE_BYTES;
  } else if (mime.startsWith("video/")) {
    limit = MAX_VIDEO_BYTES;
  } else if (mime.startsWith("text/")) {
    limit = MAX_TEXT_BYTES;
  } else {
    return `Unsupported file type "${mime || name}" — images, PDFs, videos, and text files are accepted`;
  }

  if (size > limit) {
    const mb = limit >> 20;

    return `Exceeds the ${mb} MB ${laneLabel(mime)} limit`;
  }

  return null;
}

type AttachmentSetState = StoreApi<AttachmentChipState>["setState"];

let nextChipID = 0;

/** File → base64 (without the data-URL prefix) for the binding call. */
function fileToBase64(file: File): Promise<string> {
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

async function upload(id: string, file: File): Promise<void> {
  let base64: string;

  try {
    base64 = await fileToBase64(file);
  } catch (err: unknown) {
    useAttachmentStore.setState((prev: AttachmentChipState) => ({
      chips: prev.chips.map((c: AttachmentChip): AttachmentChip =>
        c.id === id
          ? {
              ...c,
              status: "rejected",
              reason:
                err instanceof Error ? err.message : "Could not read the file",
            }
          : c
      ),
    }));

    return;
  }

  // A chip cancelled (removed) or re-queued (retry) while reading must not
  // write its result back.
  const current = useAttachmentStore
    .getState()
    .chips.find((c: AttachmentChip): boolean => c.id === id);

  if (!current || current.status !== "uploading") return;

  try {
    const stored = await client.chat.saveAttachment(
      file.name,
      file.type,
      base64
    );

    useAttachmentStore.setState((prev: AttachmentChipState) => ({
      chips: prev.chips.map((c: AttachmentChip): AttachmentChip =>
        c.id === id ? { ...c, status: "ready", key: stored.key } : c
      ),
    }));
  } catch (err: unknown) {
    useAttachmentStore.setState((prev: AttachmentChipState) => ({
      chips: prev.chips.map((c: AttachmentChip): AttachmentChip =>
        c.id === id
          ? {
              ...c,
              status: "rejected",
              retryable: true,
              reason: err instanceof Error ? err.message : "Upload failed",
            }
          : c
      ),
    }));
  }
}

export const useAttachmentStore = create<AttachmentChipState>(
  (
    set: AttachmentSetState,
    get: () => AttachmentChipState
  ): AttachmentChipState => ({
    chips: [],

    addFiles: async (files: File[]): Promise<void> => {
      for (const file of files) {
        const count = get().chips.filter(
          (c: AttachmentChip): boolean => c.status !== "rejected"
        ).length;

        nextChipID += 1;

        const id = `chip-${nextChipID}`;

        const reason =
          count >= MAX_MESSAGE_FILES
            ? `At most ${MAX_MESSAGE_FILES} files per message`
            : laneRejection(file.name, file.type, file.size);

        const chip: AttachmentChip = {
          id,
          name: file.name,
          mime: file.type,
          size: file.size,
          status: reason === null ? "uploading" : "rejected",
          file,
        };

        if (reason !== null) chip.reason = reason;

        set((prev: AttachmentChipState) => ({ chips: [...prev.chips, chip] }));

        if (reason === null) void upload(id, file);
      }
    },

    remove: (id: string): void => {
      set((prev: AttachmentChipState) => ({
        chips: prev.chips.filter((c: AttachmentChip) => c.id !== id),
      }));
    },

    // Retry re-runs the upload for a failed (not cap-rejected) chip; the
    // original File rides the chip for exactly this path.
    retry: async (id: string): Promise<void> => {
      const chip = get().chips.find((c: AttachmentChip) => c.id === id);

      if (!chip || !chip.retryable) return;

      set((prev: AttachmentChipState) => ({
        chips: prev.chips.map((c: AttachmentChip): AttachmentChip =>
          c.id === id
            ? {
                ...c,
                status: "uploading",
                reason: undefined,
                retryable: undefined,
              }
            : c
        ),
      }));
      await upload(id, chip.file);
    },

    clearFinished: (): void => {
      set((prev: AttachmentChipState) => ({
        chips: prev.chips.filter(
          (c: AttachmentChip): boolean => c.status === "uploading"
        ),
      }));
    },

    clear: (): void => {
      set({ chips: [] });
    },
  })
);
