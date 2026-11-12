/**
 * VerifierEditor (add-webhook-triggers tasks 8.2/8.3): the manual builder
 * over the five verifier engines and the editor for any endpoint's existing
 * rows. Each row carries its engine kind, the per-kind config fields
 * mirroring the store columns (header name, algo, encoding, prefix, scheme,
 * timestamp header), a write-only secret field with Generate/Regenerate
 * (client-side random generation — the save carries it, design D6), and
 * Remove. The ≥1-credential rule is enforced by the wizard's save gate; this
 * component just lets the list empty so that gate has something to say.
 */
import { useState } from "react";
import type { ChangeEvent, ReactElement } from "react";

import { Chip } from "../../../../components/ui/Chip";
import { Icon } from "../../../../components/ui/Icon";
import { Input } from "../../../../components/ui/Input";
import { Select } from "../../../../components/ui/Select";
import { Button } from "../../../../components/ui/Button";
import {
  HMAC_ALGOS,
  HMAC_ENCODINGS,
  TS_SCHEMES,
  VERIFIER_KIND_LABELS,
  VERIFIER_KINDS,
  emptyDraft,
  presetLabel,
  randomSecret,
  type VerifierDraft,
} from "./shared";

export interface VerifierEditorProps {
  drafts: VerifierDraft[];
  onChange: (next: VerifierDraft[]) => void;
  testIdPrefix?: string;
}

/** The kind-specific config fields one engine reads (the store columns). */
function KindFields({
  draft,
  prefix,
  onPatch,
}: {
  draft: VerifierDraft;
  prefix: string;
  onPatch: (patch: Partial<VerifierDraft>) => void;
}): ReactElement | null {
  switch (draft.kind) {
    case "header":
      return (
        <label className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="text-[11px] font-medium text-fg2">Header name</span>
          <Input
            className="font-mono text-[13px]"
            data-testid={`${prefix}-header-name`}
            aria-label="Header name"
            placeholder="X-Webhook-Token"
            value={draft.headerName}
            onChange={(e: ChangeEvent<HTMLInputElement>): void =>
              onPatch({ headerName: e.target.value })
            }
          />
        </label>
      );
    case "raw-hmac":
      return (
        <>
          <label className="flex min-w-0 flex-1 flex-col gap-1">
            <span className="text-[11px] font-medium text-fg2">
              Signature header
            </span>
            <Input
              className="font-mono text-[13px]"
              data-testid={`${prefix}-header-name`}
              aria-label="Signature header"
              placeholder="X-Hub-Signature-256"
              value={draft.headerName}
              onChange={(e: ChangeEvent<HTMLInputElement>): void =>
                onPatch({ headerName: e.target.value })
              }
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-[11px] font-medium text-fg2">Algorithm</span>
            <Select
              className="w-24 text-[13px]"
              data-testid={`${prefix}-algo`}
              aria-label="HMAC algorithm"
              value={draft.algo}
              onChange={(e: ChangeEvent<HTMLSelectElement>): void =>
                onPatch({ algo: e.target.value })
              }
            >
              {HMAC_ALGOS.map((algo: string) => (
                <option key={algo} value={algo}>
                  {algo}
                </option>
              ))}
            </Select>
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-[11px] font-medium text-fg2">Encoding</span>
            <Select
              className="w-24 text-[13px]"
              data-testid={`${prefix}-encoding`}
              aria-label="Signature encoding"
              value={draft.encoding}
              onChange={(e: ChangeEvent<HTMLSelectElement>): void =>
                onPatch({ encoding: e.target.value })
              }
            >
              {HMAC_ENCODINGS.map((enc: string) => (
                <option key={enc} value={enc}>
                  {enc}
                </option>
              ))}
            </Select>
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-[11px] font-medium text-fg2">
              Prefix <span className="font-normal text-muted">(optional)</span>
            </span>
            <Input
              className="w-28 font-mono text-[13px]"
              data-testid={`${prefix}-prefix`}
              aria-label="Signature prefix"
              placeholder="sha256="
              value={draft.prefix}
              onChange={(e: ChangeEvent<HTMLInputElement>): void =>
                onPatch({ prefix: e.target.value })
              }
            />
          </label>
        </>
      );
    case "ts-hmac":
      return (
        <>
          <label className="flex min-w-0 flex-1 flex-col gap-1">
            <span className="text-[11px] font-medium text-fg2">
              Signature header
            </span>
            <Input
              className="font-mono text-[13px]"
              data-testid={`${prefix}-header-name`}
              aria-label="Signature header"
              placeholder="Stripe-Signature"
              value={draft.headerName}
              onChange={(e: ChangeEvent<HTMLInputElement>): void =>
                onPatch({ headerName: e.target.value })
              }
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-[11px] font-medium text-fg2">Scheme</span>
            <Select
              className="w-24 text-[13px]"
              data-testid={`${prefix}-scheme`}
              aria-label="Timestamp scheme"
              value={draft.scheme}
              onChange={(e: ChangeEvent<HTMLSelectElement>): void =>
                onPatch({
                  scheme: e.target.value,
                  // The stripe scheme rides the timestamp inside the
                  // signature header; the v0 scheme needs its own header.
                  timestampHeader:
                    e.target.value === "v0" && draft.timestampHeader === ""
                      ? "X-Webhook-Timestamp"
                      : draft.timestampHeader,
                })
              }
            >
              {TS_SCHEMES.map((scheme: string) => (
                <option key={scheme} value={scheme}>
                  {scheme}
                </option>
              ))}
            </Select>
          </label>
          {draft.scheme === "v0" && (
            <label className="flex flex-col gap-1">
              <span className="text-[11px] font-medium text-fg2">
                Timestamp header
              </span>
              <Input
                className="w-44 font-mono text-[13px]"
                data-testid={`${prefix}-timestamp-header`}
                aria-label="Timestamp header"
                placeholder="X-Slack-Request-Timestamp"
                value={draft.timestampHeader}
                onChange={(e: ChangeEvent<HTMLInputElement>): void =>
                  onPatch({ timestampHeader: e.target.value })
                }
              />
            </label>
          )}
        </>
      );
    default:
      // path and stdweb carry no structural config beyond the secret.
      return null;
  }
}

/** One engine row's config summary for the preset-seeded card. */
export function verifierSummary(draft: VerifierDraft): string {
  const parts: string[] = [];

  if (draft.kind === "header") parts.push(draft.headerName);
  if (draft.kind === "raw-hmac")
    parts.push(
      draft.headerName,
      draft.algo,
      draft.encoding,
      ...(draft.prefix !== "" ? [draft.prefix] : [])
    );
  if (draft.kind === "ts-hmac")
    parts.push(
      draft.headerName,
      draft.scheme,
      ...(draft.scheme === "v0" && draft.timestampHeader !== ""
        ? [draft.timestampHeader]
        : [])
    );

  return parts.filter((p: string): boolean => p !== "").join(" · ");
}

/**
 * VerifierEditor: the ordered verifier rows with add/edit/regenerate/remove.
 * Stateless — the wizard owns the drafts, the ≥1 gate, and the save.
 */
export function VerifierEditor({
  drafts,
  onChange,
  testIdPrefix = "verifier-editor",
}: VerifierEditorProps): ReactElement {
  const [newKind, setNewKind] = useState<string>("header");

  const patchRow = (key: string, patch: Partial<VerifierDraft>): void => {
    onChange(
      drafts.map((row: VerifierDraft): VerifierDraft =>
        row.key === key ? { ...row, ...patch } : row
      )
    );
  };

  const removeRow = (key: string): void => {
    onChange(drafts.filter((row: VerifierDraft): boolean => row.key !== key));
  };

  const regenerate = (key: string): void => {
    patchRow(key, { secret: randomSecret() });
  };

  const addRow = (): void => {
    onChange([...drafts, emptyDraft(newKind)]);
  };

  return (
    <div className="flex flex-col gap-3" data-testid={testIdPrefix}>
      {drafts.map((draft: VerifierDraft, index: number) => {
        const rowPrefix = `${testIdPrefix}-row-${index}`;

        return (
          <div
            key={draft.key}
            className="flex flex-col gap-2 rounded-md border border-line p-3"
            data-testid={`${testIdPrefix}-row-${index}`}
          >
            <div className="flex items-center gap-2">
              <span className="text-[12px] font-semibold text-fg2">
                Verifier {index + 1}
              </span>
              {draft.preset !== "" && (
                <Chip data-testid={`${rowPrefix}-preset`}>
                  {presetLabel(draft.preset)} preset
                </Chip>
              )}
              <div className="ml-auto">
                <Button
                  variant="ghost"
                  size="sm"
                  data-testid={`${rowPrefix}-remove`}
                  onClick={(): void => removeRow(draft.key)}
                >
                  <Icon name="trash" size={14} />
                  Remove
                </Button>
              </div>
            </div>

            <div className="flex flex-wrap items-end gap-3">
              <label className="flex flex-col gap-1">
                <span className="text-[11px] font-medium text-fg2">Engine</span>
                <Select
                  className="w-44 text-[13px]"
                  data-testid={`${rowPrefix}-kind`}
                  aria-label={`Verifier ${index + 1} engine kind`}
                  value={draft.kind}
                  onChange={(e: ChangeEvent<HTMLSelectElement>): void => {
                    // Switching engines drops the old engine's structural
                    // fields — the new engine reads a different subset.
                    patchRow(draft.key, {
                      kind: e.target.value,
                      headerName: "",
                      timestampHeader: "",
                      prefix: "",
                    });
                  }}
                >
                  {VERIFIER_KINDS.map((kind: string) => (
                    <option key={kind} value={kind}>
                      {VERIFIER_KIND_LABELS[kind] ?? kind}
                    </option>
                  ))}
                </Select>
              </label>

              <KindFields
                draft={draft}
                prefix={rowPrefix}
                onPatch={(patch: Partial<VerifierDraft>): void =>
                  patchRow(draft.key, patch)
                }
              />
            </div>

            <div className="flex items-center gap-2">
              <Input
                type="password"
                autoComplete="off"
                className="max-w-md flex-1 font-mono text-[13px]"
                data-testid={`${rowPrefix}-secret`}
                aria-label={`Verifier ${index + 1} secret`}
                placeholder={
                  draft.storedSet
                    ? `Stored ••••${draft.storedHint} — leave blank to keep`
                    : "Secret"
                }
                value={draft.secret}
                onChange={(e: ChangeEvent<HTMLInputElement>): void =>
                  patchRow(draft.key, { secret: e.target.value })
                }
              />
              <Button
                variant="ghost"
                size="sm"
                data-testid={`${rowPrefix}-regenerate`}
                title="Generate a fresh secret — shown once after saving"
                onClick={(): void => regenerate(draft.key)}
              >
                <Icon name="rotate-ccw" size={14} />
                {draft.storedSet ? "Regenerate" : "Generate"}
              </Button>
            </div>
          </div>
        );
      })}

      <div className="flex items-center gap-2">
        <Select
          className="w-44 text-[13px]"
          data-testid={`${testIdPrefix}-add-kind`}
          aria-label="New verifier engine kind"
          value={newKind}
          onChange={(e: ChangeEvent<HTMLSelectElement>): void =>
            setNewKind(e.target.value)
          }
        >
          {VERIFIER_KINDS.map((kind: string) => (
            <option key={kind} value={kind}>
              {VERIFIER_KIND_LABELS[kind] ?? kind}
            </option>
          ))}
        </Select>
        <Button
          variant="ghost"
          size="sm"
          data-testid={`${testIdPrefix}-add`}
          onClick={addRow}
        >
          <Icon name="plus" size={14} />
          Add verifier
        </Button>
      </div>
    </div>
  );
}
