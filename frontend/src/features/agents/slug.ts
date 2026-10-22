/**
 * Client-side mirror of the backend's slug rules (internal/workspaces,
 * design D3): the wizard suggests a slug from the name as the user types and
 * validates the DNS-label shape before advancing, so the backend re-check is
 * a formality in the happy path. The Go implementations are the source of
 * truth; this mirror exists only to give the same answer pre-submit.
 */

/** Maximum slug length: a DNS label cap (internal/workspaces maxSlugLen). */
export const MAX_SLUG_LEN = 63;

/** DNS-label shape: lowercase letters/digits, single interior dashes. */
const SLUG_PATTERN = /^[a-z0-9](-?[a-z0-9])*$/;

/** Reports whether slug is a DNS label: the backend's ValidSlug shape. */
export function isValidSlug(slug: string): boolean {
  return (
    slug.length > 0 && slug.length <= MAX_SLUG_LEN && SLUG_PATTERN.test(slug)
  );
}

/**
 * Folds a free-form agent name into a valid slug: lowercase, every run of
 * characters outside [a-z0-9] becomes a single dash, leading and trailing
 * dashes are trimmed, and over-long results truncate to 63 characters at a
 * dash boundary (a hard cut when no dash exists in range). Mirrors the Go
 * Slugify so the suggestion matches what the backend would derive.
 */
export function suggestSlug(name: string): string {
  let out = "";
  let prevDash = false;

  for (const ch of name.toLowerCase()) {
    const alnum = (ch >= "a" && ch <= "z") || (ch >= "0" && ch <= "9");

    if (!alnum) {
      prevDash = true;

      continue;
    }

    if (prevDash && out.length > 0) {
      out += "-";
    }

    prevDash = false;

    out += ch;
  }

  if (out.length <= MAX_SLUG_LEN) {
    return out;
  }

  out = out.slice(0, MAX_SLUG_LEN);

  // Break at the last dash in range so the slug splits between words rather
  // than mid-word; trim any dash the cut left dangling.
  const cut = out.lastIndexOf("-");

  if (cut > 0) {
    out = out.slice(0, cut);
  }

  return out.replace(/-+$/, "");
}
