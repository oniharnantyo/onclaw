/**
 * Webhooks section vocabulary and draft helpers (add-webhook-triggers tasks
 * 8.1-8.3): the verifier engine kinds mirror the store.WebhookVerifier*
 * constants and the wizard's editable rows mirror the service's
 * WebhookVerifierInput/View shapes. Secrets in drafts are write-only —
 * `secret` holds only a value typed or generated THIS session; the stored
 * secret never crosses back (design D6: presence + last-four hint at most).
 */
import type {
  WebhookPresetView,
  WebhookVerifierInput,
  WebhookVerifierView,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";

/** The five verifier engine kinds (store.WebhookVerifier* vocabulary). */
export const VERIFIER_KINDS = [
  "path",
  "header",
  "raw-hmac",
  "stdweb",
  "ts-hmac",
] as const;

/** Display label per engine kind; unknown kinds fall back to the raw value. */
export const VERIFIER_KIND_LABELS: Record<string, string> = {
  path: "URL path token",
  header: "Header equality",
  "raw-hmac": "Raw-body HMAC",
  stdweb: "Standard Webhooks",
  "ts-hmac": "Timestamp HMAC",
};

/** Raw-body HMAC algorithms (webhooks.Algo* vocabulary). */
export const HMAC_ALGOS = ["sha256", "sha1"] as const;

/** Raw-body HMAC signature encodings (webhooks.Enc* vocabulary). */
export const HMAC_ENCODINGS = ["hex", "base64"] as const;

/** Timestamp-composite HMAC schemes (webhooks.Scheme* vocabulary). */
export const TS_SCHEMES = ["v0", "stripe"] as const;

/** Preset-name → gallery-card display label; "header" is itself the title. */
export function presetLabel(name: string): string {
  if (name === "") return "Custom";

  return name.charAt(0).toUpperCase() + name.slice(1);
}

/**
 * One editable verifier row in the wizard. `key` is a UI-only stable
 * identity for React lists; `secret` is write-only (blank on save keeps the
 * stored value, mirroring SaveChannel's blank-keeps at row position);
 * `storedSet`/`storedHint` carry the read shape's presence + hint.
 */
export interface VerifierDraft {
  key: string;
  kind: string;
  preset: string;
  headerName: string;
  timestampHeader: string;
  algo: string;
  encoding: string;
  prefix: string;
  scheme: string;
  secret: string;
  storedSet: boolean;
  storedHint: string;
}

let draftSeq = 0;

/** A fresh, blank manual row of one kind. */
export function emptyDraft(kind: string = "header"): VerifierDraft {
  draftSeq += 1;

  return {
    key: `draft-${draftSeq}`,
    kind,
    preset: "",
    headerName: "",
    timestampHeader: "",
    algo: "sha256",
    encoding: "hex",
    prefix: "",
    scheme: "v0",
    secret: "",
    storedSet: false,
    storedHint: "",
  };
}

/** Seeds a draft from the read shape: every config field, secret blank. */
export function draftFromView(row: WebhookVerifierView): VerifierDraft {
  draftSeq += 1;

  return {
    key: `draft-${draftSeq}`,
    kind: row.kind,
    preset: row.preset,
    headerName: row.headerName,
    timestampHeader: row.timestampHeader,
    algo: row.algo,
    encoding: row.encoding,
    prefix: row.prefix,
    scheme: row.scheme,
    secret: "",
    storedSet: row.secretSet,
    storedHint: row.secretHint,
  };
}

/** Seeds drafts from one gallery preset's rows (secrets blank). */
export function draftsFromPreset(preset: WebhookPresetView): VerifierDraft[] {
  return (preset.verifiers ?? []).map(
    (row: WebhookVerifierView): VerifierDraft => draftFromView(row)
  );
}

/** The write shape for one draft; the secret rides verbatim (blank keeps). */
export function draftToInput(draft: VerifierDraft): WebhookVerifierInput {
  return {
    kind: draft.kind,
    preset: draft.preset,
    headerName: draft.headerName,
    timestampHeader: draft.timestampHeader,
    algo: draft.algo,
    encoding: draft.encoding,
    prefix: draft.prefix,
    scheme: draft.scheme,
    secret: draft.secret,
  };
}

/**
 * A cryptographically random URL-safe secret for Generate/Regenerate
 * (frontend-only workaround per task 8.3 — 32 random bytes, base64url).
 */
export function randomSecret(): string {
  const bytes = new Uint8Array(32);
  crypto.getRandomValues(bytes);

  let binary = "";
  bytes.forEach((b: number): void => {
    binary += String.fromCharCode(b);
  });

  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

/**
 * Copies one value through the async clipboard API, falling back to the
 * deprecated execCommand path for webviews without the API. Resolves false
 * only when both refuse — the caller keeps the value selectable regardless.
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);

      return true;
    }
  } catch {
    // Fall through to the execCommand path.
  }

  try {
    const helper = document.createElement("textarea");

    helper.value = text;
    helper.setAttribute("readonly", "");
    helper.style.position = "fixed";
    helper.style.opacity = "0";
    document.body.appendChild(helper);
    helper.select();

    const ok = document.execCommand("copy");

    helper.remove();

    return ok;
  } catch {
    return false;
  }
}

/**
 * Loopback check for the listener-address warning: empty (the loopback
 * default), localhost, 127.0.0.0/8's canonical name, and IPv6 ::1 (bracketed
 * or bare) count as loopback; anything else warns (design Risks: a
 * non-loopback bind exposes live endpoints).
 */
export function isLoopbackAddr(addr: string): boolean {
  const trimmed = addr.trim();

  if (trimmed === "") return true;

  // Split host:port — a bracketed IPv6 keeps everything inside its brackets,
  // and an unbracketed multi-colon value (a bare IPv6) stays whole.
  let host = trimmed;

  const bracketEnd = trimmed.indexOf("]");

  if (bracketEnd !== -1) {
    host = trimmed.slice(0, bracketEnd + 1);
  } else if (trimmed.indexOf(":") === trimmed.lastIndexOf(":")) {
    host = trimmed.slice(0, trimmed.lastIndexOf(":"));
  }

  const bare = host.replace(/^\[|\]$/g, "").toLowerCase();

  if (bare === "" || bare === "localhost" || bare === "::1") return true;

  return bare.startsWith("127.");
}

/**
 * The outcome chip's tone family per delivery outcome (the store's
 * WebhookOutcome* vocabulary): accepted/ran succeed, auth-failed/failed are
 * the diagnosable failures (danger), paused/too-large warn, duplicate and
 * anything unknown stay neutral.
 */
export type OutcomeTone = "success" | "danger" | "warn" | "neutral";

export function outcomeTone(outcome: string): OutcomeTone {
  if (outcome === "accepted" || outcome === "ran") return "success";
  if (outcome === "auth-failed" || outcome === "failed") return "danger";
  if (outcome === "paused" || outcome === "too-large") return "warn";

  return "neutral";
}

/** Chip classes per outcome tone — token mixes only, both themes re-skin. */
export const OUTCOME_TONE_CLASSES: Record<OutcomeTone, string> = {
  success:
    "border-[color-mix(in_oklab,var(--success)_35%,transparent)] bg-[color-mix(in_oklab,var(--success)_8%,transparent)] text-successtext",
  danger:
    "border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] text-dangertext",
  warn: "border-[color-mix(in_oklab,var(--warn)_35%,transparent)] bg-[color-mix(in_oklab,var(--warn)_8%,transparent)] text-warntext",
  neutral: "",
};

/** Local time for one attempt/delivery stamp, or the raw value when absent. */
export function formatAttemptTime(iso: string): string {
  const at = new Date(iso);

  return Number.isNaN(at.getTime())
    ? iso !== ""
      ? iso
      : "—"
    : at.toLocaleString();
}

/**
 * The endpoint's preset identity for the list row and the wizard: the
 * distinct preset labels of its verifier rows ("GitLab", "GitHub + Header");
 * rows without a preset label make it Custom.
 */
export function presetIdentity(verifiers: VerifierDraft[]): string {
  const labels = new Set<string>();
  let manual = false;

  verifiers.forEach((row: VerifierDraft): void => {
    if (row.preset === "") manual = true;
    else labels.add(presetLabel(row.preset));
  });

  const named = [...labels].join(" + ");

  if (named === "") return "Custom";
  if (manual) return `${named} + Custom`;

  return named;
}
