/**
 * Knowledge helpers unit tests (add-knowledge-corpus, widened by
 * add-document-editing): the upload pre-gates mirror the corpus gate
 * byte-for-byte (PDF and OOXML 20 MiB, text 1 MiB, text+PDF+OOXML kinds
 * only, the rejection names the supported kinds), the base64 text decode
 * survives multi-byte UTF-8, and the row formatting helpers stay
 * human-readable.
 */
import { describe, expect, it } from "vite-plus/test";

import {
  MAX_OOXML_BYTES,
  MAX_PDF_BYTES,
  MAX_TEXT_BYTES,
  decodeBase64ToText,
  formatBytes,
  formatCreatedDate,
  gateRejection,
  isMarkdownName,
} from "../helpers";

describe("knowledge helpers", () => {
  it("admits text, PDF, and OOXML documents", () => {
    expect(gateRejection("notes.txt", "text/plain", 10)).toBeNull();
    expect(gateRejection("doc.pdf", "application/pdf", 10)).toBeNull();
    expect(
      gateRejection(
        "report.docx",
        "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
        10
      )
    ).toBeNull();
    expect(
      gateRejection(
        "book.xlsx",
        "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
        10
      )
    ).toBeNull();
    expect(
      gateRejection(
        "deck.pptx",
        "application/vnd.openxmlformats-officedocument.presentationml.presentation",
        10
      )
    ).toBeNull();
  });

  it("resolves an office document by extension when the mime is empty", () => {
    expect(gateRejection("report.docx", "", 10)).toBeNull();
    expect(gateRejection("book.XLSX", "", 10)).toBeNull();
    expect(gateRejection("deck.pptx", "", 10)).toBeNull();
    expect(gateRejection("doc.pdf", "", 10)).toBeNull();
  });

  it("rejects other kinds with a reason naming text, PDF, and OOXML", () => {
    expect(gateRejection("clip.mp4", "video/mp4", 10)).toBe(
      'Unsupported file type "video/mp4" — only text, PDF, and OOXML documents are accepted'
    );
    expect(gateRejection("archive.zip", "application/zip", 10)).toContain(
      "only text, PDF, and OOXML documents are accepted"
    );
    // Legacy word-processing stays rejected, named by the file name when no
    // mime was declared.
    expect(gateRejection("memo.doc", "", 10)).toContain(
      "only text, PDF, and OOXML documents are accepted"
    );
  });

  it("gates sizes: PDFs and OOXML 20 MiB and text 1 MiB, with the size reason", () => {
    expect(
      gateRejection(
        "big.docx",
        "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
        MAX_OOXML_BYTES + 1
      )
    ).toBe("Exceeds the 20 MB OOXML limit");
    expect(gateRejection("big.pdf", "application/pdf", MAX_PDF_BYTES + 1)).toBe(
      "Exceeds the 20 MB PDF limit"
    );
    expect(gateRejection("big.txt", "text/plain", MAX_TEXT_BYTES + 1)).toBe(
      "Exceeds the 1 MB text-file limit"
    );
  });

  it("treats a markdown name by extension, case-insensitively", () => {
    expect(isMarkdownName("notes.md")).toBe(true);
    expect(isMarkdownName("notes.markdown")).toBe(true);
    expect(isMarkdownName("README.MD")).toBe(true);
    expect(isMarkdownName("notes.txt")).toBe(false);
    expect(isMarkdownName("notes.md.bak")).toBe(false);
  });

  it("decodes base64 to UTF-8 text without breaking multi-byte characters", () => {
    // "héllo ✓" — the é and ✓ are multi-byte; a naive atob string would
    // mojibake them.
    const base64 = btoa(
      new TextEncoder()
        .encode("héllo ✓")
        .reduce(
          (acc: string, byte: number): string =>
            acc + String.fromCharCode(byte),
          ""
        )
    );

    expect(decodeBase64ToText(base64)).toBe("héllo ✓");
  });

  it("formats sizes human-readably", () => {
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(2048)).toBe("2 KB");
    expect(formatBytes(1536)).toBe("1.5 KB");
    expect(formatBytes(20 << 20)).toBe("20 MB");
  });

  it("formats the creation date, with a dash for an unparseable stamp", () => {
    expect(formatCreatedDate("2026-01-05T00:00:00Z")).toContain("2026");
    expect(formatCreatedDate("not-a-date")).toBe("—");
  });
});
