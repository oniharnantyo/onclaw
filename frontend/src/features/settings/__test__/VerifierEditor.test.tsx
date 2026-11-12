import { useState } from "react";
import type { ReactElement } from "react";
import { beforeEach, describe, expect, it } from "vite-plus/test";
import userEvent from "@testing-library/user-event";

import { VerifierEditor } from "../components/webhooks/VerifierEditor";
import {
  draftFromView,
  emptyDraft,
  isLoopbackAddr,
  randomSecret,
  type VerifierDraft,
} from "../components/webhooks/shared";
import type { WebhookVerifierView } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { renderUI, setInput } from "./test-utils";

/** One raw-hmac verifier row shaped like the GitHub preset's seed. */
function makeVerifierRow(
  patch: Partial<WebhookVerifierView> = {}
): WebhookVerifierView {
  return {
    id: "v1",
    position: 0,
    kind: "raw-hmac",
    preset: "",
    headerName: "",
    timestampHeader: "",
    algo: "sha256",
    encoding: "hex",
    prefix: "",
    scheme: "",
    secretSet: false,
    secretHint: "",
    ...patch,
  };
}

describe("webhooks shared helpers", () => {
  it("detects loopback listener addresses, empty as the loopback default", () => {
    expect(isLoopbackAddr("")).toBe(true);
    expect(isLoopbackAddr("127.0.0.1:8787")).toBe(true);
    expect(isLoopbackAddr("localhost:8787")).toBe(true);
    expect(isLoopbackAddr("127.7.5.4:9000")).toBe(true);
    expect(isLoopbackAddr("::1:8787")).toBe(false); // parsed as bare IPv6-ish
    expect(isLoopbackAddr("[::1]:8787")).toBe(true);
    expect(isLoopbackAddr("0.0.0.0:8787")).toBe(false);
    expect(isLoopbackAddr("192.168.1.10:8787")).toBe(false);
  });

  it("generates URL-safe secrets of usable length", () => {
    const a = randomSecret();
    const b = randomSecret();

    expect(a).toMatch(/^[A-Za-z0-9_-]{40,}$/);
    expect(a).not.toBe(b);
  });
});

describe("VerifierEditor", () => {
  let drafts: VerifierDraft[];

  /**
   * The editor is controlled — the wizard owns its state — so the harness
   * holds it in React state (seeding from the test's `drafts` at mount) and
   * mirrors every onChange back into the module variable for assertions.
   */
  function EditorHarness(): ReactElement {
    const [rows, setRows] = useState<VerifierDraft[]>(drafts);

    return (
      <VerifierEditor
        drafts={rows}
        onChange={(next: VerifierDraft[]): void => {
          drafts = next;
          setRows(next);
        }}
      />
    );
  }

  beforeEach(() => {
    drafts = [];
  });

  async function renderEditor(): Promise<Awaited<ReturnType<typeof renderUI>>> {
    return renderUI(<EditorHarness />);
  }

  it("renders per-kind config fields for a raw-hmac row", async () => {
    drafts = [{ ...emptyDraft("raw-hmac"), headerName: "X-Signature" }];
    const { findByTestId } = await renderEditor();

    expect(await findByTestId("verifier-editor-row-0")).toBeTruthy();
    // SAFETY: the testid names the editor's kit Input, an HTMLInputElement.
    expect(
      (
        (await findByTestId(
          "verifier-editor-row-0-header-name"
        )) as HTMLInputElement
      ).value
    ).toBe("X-Signature");
    expect(await findByTestId("verifier-editor-row-0-algo")).toBeTruthy();
    expect(await findByTestId("verifier-editor-row-0-encoding")).toBeTruthy();
    expect(await findByTestId("verifier-editor-row-0-prefix")).toBeTruthy();
  });

  it("shows the timestamp header only for the v0 scheme", async () => {
    drafts = [emptyDraft("ts-hmac")];
    const { findByTestId, queryByTestId } = await renderEditor();

    expect(
      await findByTestId("verifier-editor-row-0-timestamp-header")
    ).toBeTruthy();

    await userEvent.selectOptions(
      await findByTestId("verifier-editor-row-0-scheme"),
      "stripe"
    );

    // The stripe scheme rides the timestamp inside the signature header.
    expect(queryByTestId("verifier-editor-row-0-timestamp-header")).toBeNull();
    expect(drafts[0].scheme).toBe("stripe");
  });

  it("switching the engine kind drops the old engine's structural fields", async () => {
    drafts = [{ ...emptyDraft("header"), headerName: "X-Token" }];
    const { findByTestId } = await renderEditor();

    await userEvent.selectOptions(
      await findByTestId("verifier-editor-row-0-kind"),
      "stdweb"
    );

    expect(drafts[0].kind).toBe("stdweb");
    expect(drafts[0].headerName).toBe("");
  });

  it("adds a row of the picked kind and removes rows", async () => {
    drafts = [emptyDraft("header")];
    const user = userEvent.setup();
    const { findByTestId } = await renderEditor();

    await user.selectOptions(
      await findByTestId("verifier-editor-add-kind"),
      "path"
    );
    await user.click(await findByTestId("verifier-editor-add"));

    expect(drafts).toHaveLength(2);
    expect(drafts[1].kind).toBe("path");

    await user.click(await findByTestId("verifier-editor-row-1-remove"));

    expect(drafts).toHaveLength(1);
    expect(drafts[0].kind).toBe("header");
  });

  it("regenerate fills a fresh write-only secret", async () => {
    drafts = [{ ...emptyDraft("header"), storedSet: true, storedHint: "abcd" }];
    const user = userEvent.setup();
    const { findByTestId } = await renderEditor();

    // SAFETY: the testid names the editor's kit Input, an HTMLInputElement.
    const secret = (await findByTestId(
      "verifier-editor-row-0-secret"
    )) as HTMLInputElement;

    expect(secret.value).toBe("");

    await user.click(await findByTestId("verifier-editor-row-0-regenerate"));

    expect(drafts[0].secret).toMatch(/^[A-Za-z0-9_-]{40,}$/);

    // The field mirrors the draft — the value the save will carry.
    const refreshed = (await findByTestId(
      "verifier-editor-row-0-secret"
    )) as HTMLInputElement;

    expect(refreshed.value).toBe(drafts[0].secret);
  });

  it("seeds drafts from the read shape with the secret left blank", () => {
    const draft = draftFromView(
      makeVerifierRow({ preset: "github", secretSet: true, secretHint: "9f8e" })
    );

    expect(draft.secret).toBe("");
    expect(draft.storedSet).toBe(true);
    expect(draft.storedHint).toBe("9f8e");
    expect(draft.kind).toBe("raw-hmac");
  });

  it("accepts typed values user-event's keyboard parser would choke on", async () => {
    drafts = [emptyDraft("header")];
    const { findByTestId } = await renderEditor();

    const header = await findByTestId("verifier-editor-row-0-header-name");
    await setInput(header, "{id}.{timestamp}.{body}");

    expect(drafts[0].headerName).toBe("{id}.{timestamp}.{body}");
  });
});
