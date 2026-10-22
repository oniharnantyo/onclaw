import { useEffect, useRef, useState } from "react";
import type { ChangeEvent, ReactElement } from "react";

import type {
  WebSearchChainEntry,
  WebSearchConfig,
  WebSearchProviderInfo,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { ModalShell } from "../../../components";
import { Alert } from "../../../components/ui/Alert";
import { Button } from "../../../components/ui/Button";
import { Icon } from "../../../components/ui/Icon";
import { Input } from "../../../components/ui/Input";
import { Select } from "../../../components/ui/Select";
import { client } from "../../../lib/client";
import { cn } from "../../../lib/cn";

/** Human label for a credential kind, as the row's credential line shows it. */
function credentialLabel(kind: string): string {
  return kind === "base_url" ? "Base URL" : "API key";
}

/** Catalog metadata for a draft's provider, or undefined while the row's
 * provider select still sits on "Select…". */
function providerInfo(
  providers: WebSearchProviderInfo[],
  id: string
): WebSearchProviderInfo | undefined {
  return providers.find((p: WebSearchProviderInfo): boolean => p.id === id);
}

/** One editable chain row. `id` carries the persisted identifier ("" for a
 * row added this session); `localKey` keys React state and test ids. */
interface SearchEntryDraft {
  localKey: string;
  id: string;
  name: string;
  provider: string;
  apiKey: string;
  baseUrl: string;
}

/** Per-field validation messages keyed by draft localKey, plus the timeout
 * field's own message carried under the empty key. */
interface DraftErrors {
  rows: Record<string, string>;
  timeout: string | null;
}

const UNDO_MS = 5000;

/**
 * WebSearchConfigDialog: the web_search provider chain editor (design D8),
 * restated in onclaw-v2's provider-stack dialog — ordered rows with a
 * reorder pair, an index bubble, name, provider select, and a nested
 * credential line; removal is undoable for a few seconds; nothing persists
 * until Save configuration. The chain tries entries in order and the first
 * that answers serves the query.
 */
export default function WebSearchConfigDialog({
  onClose,
}: {
  onClose: () => void;
}): ReactElement {
  const [providers, setProviders] = useState<WebSearchProviderInfo[]>([]);
  const [drafts, setDrafts] = useState<SearchEntryDraft[]>([]);
  const [timeoutText, setTimeoutText] = useState("0");
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [errors, setErrors] = useState<DraftErrors>({
    rows: {},
    timeout: null,
  });

  const [generalError, setGeneralError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const [pendingRemoval, setPendingRemoval] = useState<{
    draft: SearchEntryDraft;
    index: number;
  } | null>(null);

  const localKeySeq = useRef(0);
  const undoTimer = useRef<number | null>(null);

  const nextLocalKey = (): string => {
    localKeySeq.current += 1;

    return `draft-${localKeySeq.current}`;
  };

  useEffect((): (() => void) => {
    return (): void => {
      if (undoTimer.current !== null) window.clearTimeout(undoTimer.current);
    };
  }, []);

  useEffect(() => {
    const load = async (): Promise<void> => {
      try {
        const [catalog, config]: [WebSearchProviderInfo[], WebSearchConfig] =
          await Promise.all([
            client.settings.webSearchProviders(),
            client.settings.webSearchConfig(),
          ]);

        setProviders(catalog);
        setDrafts(
          (config.entries ?? []).map((entry: WebSearchChainEntry) => ({
            localKey: nextLocalKey(),
            id: entry.id,
            name: entry.name,
            provider: entry.provider,
            apiKey: entry.api_key ?? "",
            baseUrl: entry.base_url ?? "",
          }))
        );
        setTimeoutText(String(config.request_timeout_seconds ?? 0));
      } catch (err) {
        setLoadError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
      }
    };

    void load();
  }, []);

  const clearUndoTimer = (): void => {
    if (undoTimer.current !== null) {
      window.clearTimeout(undoTimer.current);
      undoTimer.current = null;
    }
  };

  const clearRowError = (localKey: string): void => {
    setErrors((prev: DraftErrors): DraftErrors => {
      if (!(localKey in prev.rows)) return prev;

      const rows = { ...prev.rows };
      delete rows[localKey];

      return { ...prev, rows };
    });
  };

  const updateDraft = (
    localKey: string,
    patch: Partial<SearchEntryDraft>
  ): void => {
    setDrafts((prev: SearchEntryDraft[]): SearchEntryDraft[] =>
      prev.map((d: SearchEntryDraft): SearchEntryDraft =>
        d.localKey === localKey ? { ...d, ...patch } : d
      )
    );
    clearRowError(localKey);
  };

  const addDraft = (): void => {
    setGeneralError(null);
    setDrafts((prev: SearchEntryDraft[]): SearchEntryDraft[] => [
      ...prev,
      {
        localKey: nextLocalKey(),
        id: "",
        name: "",
        provider: "",
        apiKey: "",
        baseUrl: "",
      },
    ]);
  };

  const moveDraft = (index: number, delta: number): void => {
    const target = index + delta;

    if (target < 0 || target >= drafts.length) return;
    setDrafts((prev: SearchEntryDraft[]): SearchEntryDraft[] => {
      const next = [...prev];
      const moved = next[index];
      next[index] = next[target];
      next[target] = moved;

      return next;
    });
  };

  const removeDraft = (index: number): void => {
    const removed = drafts[index];

    if (!removed) return;
    clearUndoTimer();
    setDrafts((prev: SearchEntryDraft[]): SearchEntryDraft[] =>
      prev.filter(
        (d: SearchEntryDraft): boolean => d.localKey !== removed.localKey
      )
    );
    setPendingRemoval({ draft: removed, index });
    undoTimer.current = window.setTimeout((): void => {
      setPendingRemoval(null);
      undoTimer.current = null;
    }, UNDO_MS);
  };

  const undoRemove = (): void => {
    if (!pendingRemoval) return;
    clearUndoTimer();
    setDrafts((prev: SearchEntryDraft[]): SearchEntryDraft[] => {
      const next = [...prev];
      next.splice(
        Math.min(pendingRemoval.index, next.length),
        0,
        pendingRemoval.draft
      );

      return next;
    });
    setPendingRemoval(null);
  };

  const validate = (): DraftErrors => {
    const rows: Record<string, string> = {};
    let timeout: string | null = null;

    for (const draft of drafts) {
      const info = providerInfo(providers, draft.provider);

      if (!draft.provider) {
        rows[draft.localKey] = "Select a provider.";
        continue;
      }

      if (!info) continue;

      if (info.credential === "base_url" && !draft.baseUrl.trim()) {
        rows[draft.localKey] = `${info.label} requires a base URL.`;
      }

      if (info.credential === "api_key" && !draft.apiKey.trim()) {
        rows[draft.localKey] = `${info.label} requires an API key.`;
      }
    }

    const timeoutSeconds = Number(timeoutText);

    // 0 (or blank) is the runtime default; a set value bounds 1..60.
    if (
      timeoutText.trim() !== "" &&
      (!Number.isFinite(timeoutSeconds) ||
        timeoutSeconds < 0 ||
        timeoutSeconds > 60)
    ) {
      timeout = "Timeout must be between 0 and 60 seconds.";
    }

    return { rows, timeout };
  };

  const save = async (): Promise<void> => {
    setGeneralError(null);
    const found = validate();
    setErrors(found);

    if (Object.keys(found.rows).length > 0 || found.timeout) return;

    setSaving(true);

    try {
      await client.settings.saveWebSearchConfig({
        entries: drafts.map((d: SearchEntryDraft): WebSearchChainEntry => ({
          id: d.id,
          name: d.name,
          provider: d.provider,
          api_key: d.apiKey || undefined,
          base_url: d.baseUrl || undefined,
        })),
        request_timeout_seconds: Number(timeoutText) || 0,
      });

      onClose();
    } catch (err) {
      setGeneralError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <ModalShell
      title="Web Search configuration"
      testId="tool-config-dialog"
      onClose={onClose}
      actions={
        <>
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button
            size="sm"
            loading={saving}
            data-testid="btn-tool-config-save"
            onClick={() => void save()}
          >
            Save configuration
          </Button>
        </>
      }
    >
      {loading ? (
        <p className="text-[13px] text-muted">Loading…</p>
      ) : loadError ? (
        <div role="alert" className="mb-3">
          <Alert tone="danger" testId="websearch-load-error">
            {loadError}
          </Alert>
        </div>
      ) : (
        <div className="space-y-4">
          <p className="text-[12px] text-muted">
            Search the web and return top results.
          </p>

          {generalError ? (
            <div role="alert">
              <Alert tone="danger" testId="websearch-error">
                {generalError}
              </Alert>
            </div>
          ) : null}

          <div>
            <div className="flex items-center justify-between">
              <span className="text-[12px] font-medium text-fg2">
                Provider stack
              </span>
              <Button
                variant="ghost"
                size="sm"
                data-testid="btn-web-search-add"
                onClick={addDraft}
              >
                <Icon name="plus" size={13} /> Add
              </Button>
            </div>
            <p className="mt-1 text-[11px] leading-4 text-muted">
              Requests try the chain in order — the first that answers serves
              the query.
            </p>
          </div>

          {drafts.length === 0 ? (
            <p
              className="rounded-md border border-dashed border-line px-3 py-4 text-center text-[12px] text-muted"
              data-testid="web-search-empty"
            >
              No providers configured — web_search will error until you add one.
            </p>
          ) : (
            <div className="space-y-2.5">
              {drafts.map((draft: SearchEntryDraft, index: number) => {
                const info = providerInfo(providers, draft.provider);
                const isBaseURL = info?.credential === "base_url";

                return (
                  <div
                    key={draft.localKey}
                    className="rounded-lg border border-line bg-[color-mix(in_oklab,var(--fg)_2%,var(--surface))] p-3 transition-colors"
                    data-testid="web-search-row"
                  >
                    <div className="flex flex-wrap items-center gap-x-2 gap-y-2">
                      <div className="flex shrink-0 overflow-hidden rounded-md border border-line">
                        <button
                          type="button"
                          aria-label="Move up"
                          disabled={index === 0}
                          onClick={() => moveDraft(index, -1)}
                          className="flex h-6 w-6 items-center justify-center text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg disabled:pointer-events-none disabled:opacity-35"
                        >
                          <Icon name="up" size={12} />
                        </button>
                        <span
                          aria-hidden
                          className="w-px self-stretch bg-line"
                        />
                        <button
                          type="button"
                          aria-label="Move down"
                          disabled={index === drafts.length - 1}
                          onClick={() => moveDraft(index, 1)}
                          className="flex h-6 w-6 items-center justify-center text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg disabled:pointer-events-none disabled:opacity-35"
                        >
                          <Icon name="down" size={12} />
                        </button>
                      </div>
                      <span
                        className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] font-mono text-[10px] leading-none text-muted"
                        data-testid="web-search-row-number"
                      >
                        {index + 1}
                      </span>
                      <Input
                        aria-label="Name"
                        placeholder="Name"
                        className="h-8 min-w-0 flex-1 basis-[150px] text-[13px]"
                        data-testid={`web-search-name-${draft.localKey}`}
                        value={draft.name}
                        onChange={(e: ChangeEvent<HTMLInputElement>) =>
                          updateDraft(draft.localKey, { name: e.target.value })
                        }
                      />
                      <Select
                        aria-label="Provider"
                        className="h-8 w-auto shrink-0 grow-0 text-[13px]"
                        data-testid={`web-search-provider-${draft.localKey}`}
                        value={draft.provider}
                        onChange={(e: ChangeEvent<HTMLSelectElement>) =>
                          updateDraft(draft.localKey, {
                            provider: e.target.value,
                          })
                        }
                      >
                        <option value="">Select…</option>
                        {providers.map((p: WebSearchProviderInfo) => (
                          <option key={p.id} value={p.id}>
                            {p.label}
                          </option>
                        ))}
                      </Select>
                      <button
                        type="button"
                        aria-label="Remove entry"
                        data-testid={`btn-web-search-remove-${draft.localKey}`}
                        onClick={() => removeDraft(index)}
                        className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-dangertext"
                      >
                        <Icon name="x" size={13} />
                      </button>
                    </div>

                    {draft.provider !== "" ? (
                      <div className="mt-2 flex items-center gap-2 pl-1 md:pl-[86px]">
                        <span
                          aria-hidden
                          className="hidden select-none font-mono text-[12px] leading-none text-[color-mix(in_oklab,var(--fg)_24%,transparent)] md:inline"
                        >
                          └─
                        </span>
                        <label
                          className="w-14 shrink-0 text-[11px] text-muted"
                          htmlFor={`ws-cred-${draft.localKey}`}
                        >
                          {credentialLabel(info?.credential ?? "api_key")}
                        </label>
                        <Input
                          id={`ws-cred-${draft.localKey}`}
                          aria-label={credentialLabel(
                            info?.credential ?? "api_key"
                          )}
                          type={isBaseURL ? "text" : "password"}
                          className={cn("h-8 flex-1 font-mono text-[13px]")}
                          data-testid={`ws-cred-${draft.localKey}`}
                          value={isBaseURL ? draft.baseUrl : draft.apiKey}
                          onChange={(e: ChangeEvent<HTMLInputElement>) =>
                            updateDraft(
                              draft.localKey,
                              isBaseURL
                                ? { baseUrl: e.target.value }
                                : { apiKey: e.target.value }
                            )
                          }
                        />
                      </div>
                    ) : null}

                    {errors.rows[draft.localKey] ? (
                      <p
                        className="mt-1.5 flex items-center gap-1 text-[12px] text-dangertext"
                        data-testid="web-search-row-error"
                      >
                        <Icon name="alert" size={12} />{" "}
                        {errors.rows[draft.localKey]}
                      </p>
                    ) : null}
                  </div>
                );
              })}
            </div>
          )}

          {pendingRemoval ? (
            <div
              className="flex items-center gap-2.5 rounded-md border border-line bg-warm px-3.5 py-2.5"
              data-testid="web-search-undo-bar"
              role="status"
            >
              <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] text-dangertext">
                <Icon name="x" size={12} />
              </span>
              <p className="flex-1 text-[12px] text-fg2">
                Removed{" "}
                {pendingRemoval.draft.name ||
                  providerInfo(providers, pendingRemoval.draft.provider)
                    ?.label ||
                  "provider entry"}
              </p>
              <button
                type="button"
                data-testid="web-search-undo"
                onClick={undoRemove}
                className="h-7 rounded-md border border-line px-2.5 text-[12px] font-medium text-accenttext transition-colors hover:border-accent"
              >
                Undo
              </button>
            </div>
          ) : null}

          <div data-testid="web-search-timeout-field">
            <label
              className="mb-1.5 block text-[12px] font-medium text-fg2"
              htmlFor="web-search-timeout"
            >
              Request timeout (per attempt)
            </label>
            <div className="relative w-24">
              <Input
                id="web-search-timeout"
                type="number"
                min={1}
                max={60}
                className="pr-8"
                data-testid="web-search-timeout"
                value={timeoutText}
                onChange={(e: ChangeEvent<HTMLInputElement>) => {
                  setTimeoutText(e.target.value);

                  if (errors.timeout)
                    setErrors((prev: DraftErrors): DraftErrors => ({
                      ...prev,
                      timeout: null,
                    }));
                }}
              />
              <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-[12px] text-muted">
                s
              </span>
            </div>
            {errors.timeout ? (
              <p
                className="mt-1 text-[12px] text-dangertext"
                data-testid="web-search-timeout-error"
              >
                {errors.timeout}
              </p>
            ) : null}
            <p className="mt-1 text-[11px] leading-4 text-muted">
              Bounds each attempt — worst case ≈ 3 × timeout.
            </p>
          </div>
        </div>
      )}
    </ModalShell>
  );
}
