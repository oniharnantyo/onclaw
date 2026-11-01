/**
 * The generated-document export flow (add-artifact-save-as design D4),
 * shared by the create_document tool card and the transcript's artifact
 * chips: one bound Save As call through the host's native save dialog, with
 * the sentinel-message toasts both surfaces agree on — a user cancellation
 * stays silent, a host without a dialog service gets its own readable
 * toast, anything else is generic, and success surfaces only the chosen
 * destination's base name (the on-disk path never crosses to the UI). The
 * session id passed through locates the generation's session-scoped
 * artifacts home (sessions/<id>/artifacts) on the backend; the legacy homes
 * sit behind it.
 */
import { client } from "../../lib/client";
import { toast } from "../../lib/toasts";

/** The plain-name shape the bound export accepts (no directory components)
 * — mirrored before the call so a malformed name is refused without
 * touching the backend. */
export const PLAIN_DOC_NAME = /^[^/\\]+$/;

/** Exports one generated document by its plain file name through the
 * host's native save dialog: resolves when the copy is written, having
 * toasted the outcome; never throws (every rejection is a toast). */
export async function exportGeneratedDocument(
  agentID: string,
  sessionID: string,
  name: string
): Promise<void> {
  try {
    const path = await client.chat.saveGeneratedDocumentAs(
      agentID,
      sessionID,
      name
    );

    // The destination is only ever surfaced as its base name (design
    // D4: the on-disk path never crosses to the webview's UI) — the
    // basename after the last "/" or "\" separator, computed locally.
    const base = path.substring(
      Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\")) + 1
    );

    toast(`Saved to ${base}`, "info");
  } catch (err) {
    // The sentinel error MESSAGES are the contract's match keys (design
    // D4): a user cancellation stays silent, a host without a dialog
    // service gets its own readable toast, anything else is generic.
    const message = err instanceof Error ? err.message : String(err);

    if (message === "save as: cancelled") return;

    if (message === "save as: no native dialog on this host") {
      toast("Save As is not available here", "danger");

      return;
    }

    toast("Could not save the document", "danger");
  }
}
