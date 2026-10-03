import { useEffect, useMemo, useState } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../../components/ui/Icon";
import { Segmented } from "../../components/ui/Segmented";
import { Toggle } from "../../components/ui/Toggle";
import { Chip } from "../../components/ui/Chip";
import { Modal } from "../../components/ui/Modal";
import { ModelCombobox } from "../../components/ui/ModelCombobox";
import { ErrorState } from "../../components/ErrorState";
import {
  api,
  ApiError,
  formatApiError,
  type ApiMemoryNote,
  type ApiMemoryNoteCounts,
  type ApiMemorySettings,
  type ApiMorningReport,
  type ApiModelsResult,
} from "../../lib/api";
import { useCanWriteWorkspace } from "../../lib/writePerms";
import {
  classifyEmbeddingModel,
  KNOWN_MODEL_DIMS,
  SUPPORTED_EMBEDDING_DIMS,
  knownModelDimension,
} from "../../lib/embedding";
import { isDecisionProviderType } from "../../modals/ProviderFormDialog";
import serverErrorSvg from "../../assets/server-error.svg";

export interface MemoryPaneProps {
  tenant: any;
  /** Unused: every surface here is API-backed; kept for SettingsPage compat. */
  onUpdate?: (fn: any) => void;
  onToast?: (text: string, kind?: string) => void;
  /** Override the derived workspace.write check (tests). */
  canWrite?: boolean;
}

// ---------------------------------------------------------------------------
// Embedding-model classification lives in lib/embedding.ts (shared with the
// model comboboxes' embedding marker icon).
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Small views
// ---------------------------------------------------------------------------

const VISIBILITY_META: Record<string, { label: string; cls: string }> = {
  shared: { label: "shared", cls: "text-accenttext bg-[color-mix(in_oklab,var(--accent)_13%,transparent)]" },
  user: { label: "private", cls: "text-fg2 bg-[color-mix(in_oklab,var(--fg)_8%,transparent)]" },
  agent: { label: "agent", cls: "text-muted bg-[color-mix(in_oklab,var(--fg)_6%,transparent)]" },
};

function VisibilityChip({ visibility }: { visibility: string }) {
  const meta = VISIBILITY_META[visibility] || VISIBILITY_META.agent;
  return (
    <span
      className={cx("inline-flex items-center rounded-[5px] px-1.5 py-0.5 font-mono text-[10px]", meta.cls)}
      data-testid={"memory-visibility-" + visibility}
    >
      {meta.label}
    </span>
  );
}

function SectionHeader({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="mb-3">
      <h3 className="text-[15px] font-semibold text-fg">{title}</h3>
      {hint && <p className="mt-0.5 text-[12px] leading-5 text-muted">{hint}</p>}
    </div>
  );
}

// A stored-but-never-run report carries the zero time; that is "no report
// yet", not a real timestamp to render.
function hasReportTime(generatedAt?: string | null): boolean {
  if (!generatedAt) return false;
  const t = Date.parse(generatedAt);
  return Number.isFinite(t) && t > 0;
}

// The decision backend's model field prefills this when revealed unchecked
// (add-configurable-decision-backend 5.4).
const DECISION_MODEL_DEFAULT = "jev-latest";

// ---------------------------------------------------------------------------
// Pane
// ---------------------------------------------------------------------------

export function MemoryPane({ tenant, onToast = () => {}, canWrite }: MemoryPaneProps) {
  // fix-role-permission-audit: memory mutations (configuration save, fact
  // promote/delete, consolidation) gate on workspace.write, not tools.write.
  const derivedWriter = useCanWriteWorkspace(tenant);
  const writer = canWrite !== undefined ? canWrite : derivedWriter;

  const targetWsId = tenant?.sub || tenant?.id;

  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<ApiError | Error | null>(null);

  // Page tabs: the derived tier lives here; the documents stay in their own
  // homes (user menu "My memory" for USER.md, Workspace settings for
  // WORKSPACE.md).
  const [tab, setTab] = useState<"facts" | "report" | "config">("config");

  // Notes browser — the derived tier.
  const [notes, setNotes] = useState<ApiMemoryNote[]>([]);
  const [counts, setCounts] = useState<ApiMemoryNoteCounts>({ shared: 0, user: 0, agent: 0 });
  const [noteQuery, setNoteQuery] = useState("");
  const [noteTopic, setNoteTopic] = useState("");
  const [noteVisibility, setNoteVisibility] = useState<"" | "shared" | "user" | "agent">("");
  const [promoting, setPromoting] = useState<ApiMemoryNote | null>(null);
  const [deleting, setDeleting] = useState<ApiMemoryNote | null>(null);
  const [noteBusy, setNoteBusy] = useState(false);

  // Morning report.
  const [report, setReport] = useState<ApiMorningReport | null>(null);
  const [consolidating, setConsolidating] = useState(false);

  // Memory configuration (D16).
  const [posture, setPosture] = useState<"narrow" | "org-shared">("narrow");
  const [ingestion, setIngestion] = useState(true);
  const [sidecallMode, setSidecallMode] = useState<"agent_default" | "specific">("agent_default");
  const [sidecallProviderId, setSidecallProviderId] = useState("");
  const [sidecallModel, setSidecallModel] = useState("");
  // Decision backend (5.4): unchecked by default; checked reveals the pair.
  const [useDecisionBackend, setUseDecisionBackend] = useState(false);
  const [decisionProviderId, setDecisionProviderId] = useState("");
  const [decisionModel, setDecisionModel] = useState("");
  const [model, setModel] = useState("");
  const [dimension, setDimension] = useState("");
  // The model id whose known dimension we auto-preselected (D7). Cleared
  // whenever the dimension is set by anything else — load, save sync, test
  // discovery, or an explicit user choice — so a later model switch never
  // clobbers a dimension the user (or the test) actually chose.
  const [dimensionAutoFor, setDimensionAutoFor] = useState<string | null>(null);
  // Provider configs keep their type so the pane can split language-model
  // pickers (side-call, embedding) from the decision-backend select —
  // decision providers never appear in a model picker (5.3).
  const [providers, setProviders] = useState<{ id: string; name: string; type: string }[]>([]);
  const [providerId, setProviderId] = useState("");
  const [modelsResult, setModelsResult] = useState<ApiModelsResult | null>(null);
  const [savingSettings, setSavingSettings] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<string | null>(null);
  const [testError, setTestError] = useState<string | null>(null);

  const load = async () => {
    if (!targetWsId) return;
    setLoading(true);
    setLoadError(null);
    try {
      const [notesRes, reportRes, settingsRes, providerRes] = await Promise.all([
        api.memory.notes(targetWsId),
        api.memory.report(targetWsId),
        api.memory.getSettings(targetWsId),
        api.providers.list(targetWsId).catch(() => null),
      ]);
      setNotes(notesRes.notes || []);
      setCounts(notesRes.counts || { shared: 0, user: 0, agent: 0 });
      setReport(reportRes.report || null);
      const s = settingsRes.settings;
      setPosture(s.visibility_posture);
      setIngestion(s.ingestion_enabled);
      if (s.side_call_model) {
        setSidecallMode("specific");
        setSidecallProviderId(s.side_call_model.provider_id);
        setSidecallModel(s.side_call_model.model);
      } else {
        setSidecallMode("agent_default");
        setSidecallProviderId("");
        setSidecallModel("");
      }
      // The GET view carries the flat decision keys only when BOTH are stored.
      if (s.decision_provider_id && s.decision_model) {
        setUseDecisionBackend(true);
        setDecisionProviderId(s.decision_provider_id);
        setDecisionModel(s.decision_model);
      } else {
        setUseDecisionBackend(false);
        setDecisionProviderId("");
        setDecisionModel("");
      }
      setModel(s.embedding?.model || "");
      setDimension(s.embedding?.dimension ? String(s.embedding.dimension) : "");
      setProviderId(s.embedding?.provider_id || "");
      setProviders((providerRes?.providers || []).map((p) => ({ id: p.id, name: p.name, type: p.type })));
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 0) {
        return;
      }
      setLoadError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reload per workspace only
  }, [targetWsId]);

  // Embedding-classified models for the chosen provider (D16): provider-
  // scoped resolution; when nothing classifies — unknown gateways and
  // unmapped providers — the model entry falls back to free text and the
  // connection test discovers the dimension.
  useEffect(() => {
    let cancelled = false;
    setModelsResult(null);
    if (!targetWsId || !providerId) return;
    void (async () => {
      try {
        const res = await api.providers.models(targetWsId, providerId);
        if (!cancelled) setModelsResult(res);
      } catch {
        if (!cancelled) setModelsResult({ source: "none", models: [] });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [targetWsId, providerId]);

  const embeddingModels = useMemo(
    () => (modelsResult?.models || []).filter((m) => classifyEmbeddingModel(m) === "embedding"),
    [modelsResult]
  );
  // Tri-state: only classified embedding models populate the dropdown; when
  // the source resolves models but none classifies, stay on free text.
  const modelDropdown = embeddingModels.length > 0;

  // Model pickers list language-model providers only (5.3): the side-call and
  // embedding selects never offer a decision config, and the decision backend
  // select offers nothing else.
  const languageProviders = useMemo(
    () => providers.filter((p) => !isDecisionProviderType(p.type)),
    [providers]
  );
  const decisionProviders = useMemo(
    () => providers.filter((p) => isDecisionProviderType(p.type)),
    [providers]
  );

  // Revealing the block prefills the model (5.4) unless the pane already
  // holds one — a retick never clobbers a model the user typed.
  const handleDecisionBackendToggle = (on: boolean) => {
    setUseDecisionBackend(on);
    if (on && !decisionModel.trim()) setDecisionModel(DECISION_MODEL_DEFAULT);
  };

  // Known-model preselect (D7): picking a model whose dimension the map knows
  // fills the Dimension dropdown — over Auto, or over a dimension the previous
  // model auto-preselected — but never over an explicitly chosen or
  // test-discovered dimension. Free-text entry (unknown catalog) never
  // preselects; the connection test remains the authority there.
  const handleEmbeddingModelChange = (id: string) => {
    setModel(id);
    const known = knownModelDimension(id);
    if (known === undefined) return;
    const autoDim = dimensionAutoFor ? KNOWN_MODEL_DIMS[dimensionAutoFor] : undefined;
    if (!dimension || (autoDim !== undefined && dimension === String(autoDim))) {
      setDimension(String(known));
      setDimensionAutoFor(id);
    }
  };

  // Notes fetch with the current filters (search uses the server's lexical
  // query; topic and visibility ride the structured filters).
  const refreshNotes = async () => {
    if (!targetWsId) return;
    try {
      const res = await api.memory.notes(targetWsId, {
        q: noteQuery.trim() || undefined,
        topic: noteTopic.trim() || undefined,
        visibility: noteVisibility || undefined,
      });
      setNotes(res.notes || []);
      setCounts(res.counts || { shared: 0, user: 0, agent: 0 });
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to load memory notes"), "danger");
    }
  };

  const refreshReport = async () => {
    if (!targetWsId) return;
    try {
      const res = await api.memory.report(targetWsId);
      setReport(res.report || null);
    } catch {
      // The report card keeps its last content on a refresh failure.
    }
  };

  const consolidateNow = async () => {
    if (!targetWsId) return;
    setConsolidating(true);
    try {
      const res = await api.memory.consolidate(targetWsId);
      setReport(res.report || null);
      onToast("Consolidation finished — report refreshed");
    } catch (err: unknown) {
      onToast(formatApiError(err, "Consolidation failed"), "danger");
    } finally {
      setConsolidating(false);
    }
  };

  const handlePromote = async () => {
    if (!targetWsId || !promoting) return;
    setNoteBusy(true);
    try {
      const res = await api.memory.promoteNote(targetWsId, promoting.id);
      setNotes((prev) => prev.map((n) => (n.id === res.note.id ? res.note : n)));
      setPromoting(null);
      onToast("Note promoted to shared — the widening is recorded in its provenance");
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to promote note"), "danger");
    } finally {
      setNoteBusy(false);
    }
  };

  const handleDelete = async () => {
    if (!targetWsId || !deleting) return;
    setNoteBusy(true);
    try {
      await api.memory.deleteNote(targetWsId, deleting.id);
      setNotes((prev) => prev.filter((n) => n.id !== deleting.id));
      setDeleting(null);
      onToast("Note deleted — hidden everywhere, the deletion is recorded");
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to delete note"), "danger");
    } finally {
      setNoteBusy(false);
    }
  };

  const saveSettings = async () => {
    if (!targetWsId) return;
    setSavingSettings(true);
    try {
      if (sidecallMode === "specific" && (!sidecallProviderId || !sidecallModel.trim())) {
        onToast("Pick a provider and model for the memory side-call, or switch back to agent default", "danger");
        return;
      }
      if (useDecisionBackend && (!decisionProviderId || !decisionModel.trim())) {
        onToast("Pick a provider and model for the decision backend, or untick Use decision backend", "danger");
        return;
      }
      const res = await api.memory.updateSettings(targetWsId, {
        visibility_posture: posture,
        ingestion_enabled: ingestion,
        side_call_model:
          sidecallMode === "specific"
            ? { provider_id: sidecallProviderId, model: sidecallModel.trim() }
            : null,
        embedding: {
          provider_id: providerId,
          model,
          ...(dimension.trim() ? { dimension: Number(dimension) } : {}),
        },
        // Checked sends both flat keys; unchecked omits BOTH — omission is
        // the clear (server tri-state), so a save never sends a half pair.
        ...(useDecisionBackend
          ? { decision_provider_id: decisionProviderId, decision_model: decisionModel.trim() }
          : {}),
      });
      const s = res.settings;
      setPosture(s.visibility_posture);
      setIngestion(s.ingestion_enabled);
      if (s.side_call_model) {
        setSidecallMode("specific");
        setSidecallProviderId(s.side_call_model.provider_id);
        setSidecallModel(s.side_call_model.model);
      } else {
        setSidecallMode("agent_default");
        setSidecallProviderId("");
        setSidecallModel("");
      }
      if (s.decision_provider_id && s.decision_model) {
        setUseDecisionBackend(true);
        setDecisionProviderId(s.decision_provider_id);
        setDecisionModel(s.decision_model);
      } else {
        setUseDecisionBackend(false);
        setDecisionProviderId("");
        setDecisionModel("");
      }
      setModel(s.embedding?.model || "");
      setDimension(s.embedding?.dimension ? String(s.embedding.dimension) : "");
      setProviderId(s.embedding?.provider_id || "");
      setTestResult(null);
      onToast("Memory settings saved");
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to save memory settings"), "danger");
    } finally {
      setSavingSettings(false);
    }
  };

  const testConnection = async () => {
    if (!targetWsId) return;
    if (!providerId) {
      setTestError("Pick a provider first — the endpoint and credential come from it");
      return;
    }
    setTesting(true);
    setTestResult(null);
    setTestError(null);
    try {
      const res = await api.memory.testSettings(targetWsId, {
        provider_id: providerId,
        model,
        ...(dimension.trim() ? { dimension: Number(dimension) } : {}),
      });
      setTestResult(`Connection ok — embedding dimension ${res.dimension}`);
      if (!dimension.trim()) setDimension(String(res.dimension));
    } catch (err: unknown) {
      setTestError(formatApiError(err, "Connection test failed"));
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className="max-w-2xl space-y-8" data-od-id="pane-memory" data-testid="pane-memory">
      {/* Precedence rule (design D7): one page, two tiers, stated in copy. */}
      <p className="text-[12px] leading-5 text-muted" data-testid="memory-precedence-copy">
        Documents and facts are two memory tiers: the documents (USER.md and WORKSPACE.md) are manually
        curated, live in their own editors, and always win conflicts; everything on this page is derived
        from conversations — it never edits the documents, and any fact can be deleted.
      </p>

      {loading ? (
        <div className="space-y-3">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="h-20 animate-pulse rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]" />
          ))}
        </div>
      ) : loadError ? (
        <div className="py-6">
          <ErrorState
            illustration={serverErrorSvg}
            title="Couldn't load memory"
            detail={loadError.message}
            primaryAction={{ label: "Retry", onClick: load }}
          />
        </div>
      ) : (
        <>
          <Segmented
            value={tab}
            onChange={(t) => setTab(t as "facts" | "report" | "config")}
            options={[
              { id: "config", label: "Configuration", testid: "memory-tab-config" },
              { id: "facts", label: "Facts", testid: "memory-tab-facts" },
              { id: "report", label: "Morning report", testid: "memory-tab-report" },
            ]}
          />
          {/* ------------------------------------------------ Morning report */}
          {tab === "report" && (
          <section data-testid="memory-report">
            <SectionHeader
              title="Morning report"
              hint="What the nightly consolidation pass found: conflicts with the documents, near-duplicates merged, and extractions that failed."
            />
            <div className="rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] p-4">
              {report ? (
                <>
                  <div className="flex flex-wrap items-center gap-2" data-testid="memory-report-counts">
                    <Chip>
                      {report.conflicts.length} {report.conflicts.length === 1 ? "conflict" : "conflicts"}
                    </Chip>
                    <Chip>
                      {report.merges.length} {report.merges.length === 1 ? "merge" : "merges"}
                    </Chip>
                    <Chip>
                      {report.extraction_failures} extraction{" "}
                      {report.extraction_failures === 1 ? "failure" : "failures"}
                    </Chip>
                    {hasReportTime(report.generated_at) && (
                      <span className="ml-auto font-mono text-[10px] text-muted">
                        {new Date(report.generated_at).toLocaleString()}
                      </span>
                    )}
                  </div>
                  {report.conflicts.length > 0 && (
                    <ul className="mt-3 space-y-1.5">
                      {report.conflicts.map((c) => (
                        <li key={c.note_id + c.document} className="text-[12px] leading-5 text-fg2">
                          <span className="font-mono text-[11px] text-[color-mix(in_oklab,var(--warn),black_25%)]">
                            {c.document}
                          </span>{" "}
                          — {c.excerpt}
                        </li>
                      ))}
                    </ul>
                  )}
                  {report.extraction_failures > 0 && (
                    <p className="mt-3 text-[12px] text-muted">
                      {report.extraction_failures}{" "}
                      {report.extraction_failures === 1 ? "turn" : "turns"} failed extraction overnight — the raw
                      conversation is kept and can be reprocessed.
                    </p>
                  )}
                </>
              ) : (
                <p className="text-[12px] text-muted" data-testid="memory-report-empty">
                  No report yet — run a consolidation or wait for the nightly pass.
                </p>
              )}
              {writer && (
                <div className="mt-3 flex justify-end">
                  <button
                    type="button"
                    data-testid="btn-memory-consolidate"
                    disabled={consolidating}
                    onClick={() => void consolidateNow()}
                    className="inline-flex h-8 items-center gap-1.5 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-40"
                  >
                    <Icon name="refresh" size={12} />
                    {consolidating ? "Consolidating…" : "Consolidate now"}
                  </button>
                </div>
              )}
            </div>
          </section>
          )}

          {/* ------------------------------------------------ Facts */}
          {tab === "facts" && (
          <section data-testid="memory-notes-browser">
            <SectionHeader
              title="Facts"
              hint="Derived from conversations, with provenance back to the source turn. Promotion is audited; deletion is a recorded tombstone."
            />
            <div className="mb-3 flex flex-wrap items-center gap-2">
              <div className="relative min-w-[180px] flex-1">
                <input
                  data-testid="memory-notes-search"
                  aria-label="Search facts"
                  className="h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] pl-8 pr-3 text-[13px] text-fg2 placeholder:text-muted focus:border-accent"
                  placeholder="Search facts…"
                  value={noteQuery}
                  onChange={(e) => setNoteQuery(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") void refreshNotes();
                  }}
                />
                <Icon name="search" size={13} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-muted" />
              </div>
              <input
                data-testid="memory-notes-topic"
                aria-label="Filter by topic"
                className="h-9 w-32 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-3 text-[13px] text-fg2 placeholder:text-muted focus:border-accent"
                placeholder="Topic…"
                value={noteTopic}
                onChange={(e) => setNoteTopic(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") void refreshNotes();
                }}
              />
              <select
                data-testid="memory-notes-visibility"
                aria-label="Filter by visibility"
                className="h-9 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2 text-[13px] text-fg2 focus:border-accent"
                value={noteVisibility}
                onChange={(e) => setNoteVisibility(e.target.value as typeof noteVisibility)}
              >
                <option value="">All visibility</option>
                <option value="shared">shared</option>
                <option value="user">private</option>
                <option value="agent">agent</option>
              </select>
              <button
                type="button"
                data-testid="btn-memory-notes-apply"
                onClick={() => void refreshNotes()}
                className="h-9 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
              >
                Filter
              </button>
            </div>
            {notes.length === 0 ? (
              <div className="py-8 text-center" data-testid="memory-notes-empty">
                <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
                  <Icon name="memory" size={20} />
                </div>
                <p className="text-[14px] font-medium text-fg">No facts extracted yet</p>
                <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
                  As conversations finish, durable facts land here with their source — this workspace has none
                  matching the current filters.
                </p>
              </div>
            ) : (
              <ul className="space-y-2.5">
                {notes.map((n) => (
                  <li
                    key={n.id}
                    data-testid={"memory-note-" + n.id}
                    className="rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] p-3.5"
                  >
                    <p className="text-[13px] leading-5 text-fg">{n.content}</p>
                    <div className="mt-2 flex flex-wrap items-center gap-1.5">
                      <VisibilityChip visibility={n.visibility} />
                      {n.topic && <Chip mono>{n.topic}</Chip>}
                      {n.pinned && (
                        <span className="inline-flex items-center gap-1 text-[10px] text-muted" title="Pinned — explicitly requested to remember">
                          <Icon name="pin" size={10} /> pinned
                        </span>
                      )}
                      {n.conflict_flag && (
                        <span
                          className="inline-flex items-center gap-1 rounded-[5px] bg-[color-mix(in_oklab,var(--warn)_12%,transparent)] px-1.5 py-0.5 text-[10px] text-[color-mix(in_oklab,var(--warn),black_25%)]"
                          title={"Conflicts with a kept document — review before trusting (" + n.conflict_flag + ")"}
                          data-testid={"memory-note-conflict-" + n.id}
                        >
                          <Icon name="alert" size={10} /> conflicts with docs
                        </span>
                      )}
                      {n.superseded_by && (
                        <span className="rounded-[5px] bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] px-1.5 py-0.5 font-mono text-[10px] text-muted">
                          superseded
                        </span>
                      )}
                      <span
                        className="ml-auto font-mono text-[10px] text-muted"
                        title={"Origin " + n.origin + " · learned " + n.learned_at}
                      >
                        {n.origin} · {new Date(n.learned_at).toLocaleDateString()} ·{" "}
                        <span className="font-mono">{n.source_event_id.slice(0, 8)}</span>
                      </span>
                      {writer && (
                        <span className="flex shrink-0 items-center gap-1">
                          {n.visibility !== "shared" && !n.superseded_by && (
                            <button
                              type="button"
                              data-testid={"btn-memory-promote-" + n.id}
                              aria-label="Promote to shared"
                              onClick={() => setPromoting(n)}
                              className="flex h-7 items-center rounded-[6px] border border-line px-2 text-[11px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
                            >
                              Promote
                            </button>
                          )}
                          <button
                            type="button"
                            aria-label={"Delete fact"}
                            data-testid={"btn-memory-delete-" + n.id}
                            onClick={() => setDeleting(n)}
                            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger"
                          >
                            <Icon name="trash" size={13} />
                          </button>
                        </span>
                      )}
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </section>
          )}

          {/* ------------------------------------------------ Configuration */}
          {tab === "config" && (
          <section data-testid="memory-configuration">
            <SectionHeader
              title="Memory configuration"
              hint="How ingestion behaves in this workspace and which embedding provider indexes its facts."
            />
            <div className="space-y-5 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] p-4">
              {!writer ? (
                <p className="text-[12px] text-muted">Only Owner and Admin can change memory configuration.</p>
              ) : (
                <>
                  <div>
                    <p className="mb-1.5 text-[12px] font-medium text-fg2">Visibility default posture</p>
                    <div className="inline-flex rounded-md border border-line p-0.5" role="group" aria-label="Visibility default posture">
                      {(["narrow", "org-shared"] as const).map((p) => (
                        <button
                          key={p}
                          type="button"
                          data-testid={"memory-posture-" + p}
                          aria-pressed={posture === p}
                          onClick={() => setPosture(p)}
                          className={cx(
                            "h-7 rounded-[6px] px-3 text-[12px] font-medium transition-colors",
                            posture === p
                              ? "bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg"
                              : "text-muted hover:text-fg2"
                          )}
                        >
                          {p === "narrow" ? "Narrow" : "Org-shared"}
                        </button>
                      ))}
                    </div>
                    <p className="mt-1.5 text-[11px] leading-5 text-muted">
                      {posture === "narrow"
                        ? "New facts default to the narrowest tier that fits their session — promotion stays cheap, leaks stay rare."
                        : "New facts default to workspace-wide within the session's ceiling — a direct chat still births at most private facts."}
                    </p>
                  </div>

                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <p className="text-[12px] font-medium text-fg2">Ingestion</p>
                      <p className="text-[11px] leading-5 text-muted">
                        Extract facts from finished conversations in the background. Off stops new extraction —
                        existing facts and both documents stay.
                      </p>
                    </div>
                    <Toggle on={ingestion} label="Enable memory ingestion" onChange={setIngestion} />
                  </div>

                  <div>
                    <p className="mb-1.5 text-[12px] font-medium text-fg2">Memory side-call model</p>
                    <div className="inline-flex rounded-md border border-line p-0.5" role="group" aria-label="Memory side-call model">
                      {(["agent_default", "specific"] as const).map((m) => (
                        <button
                          key={m}
                          type="button"
                          data-testid={"memory-sidecall-" + m}
                          aria-pressed={sidecallMode === m}
                          onClick={() => setSidecallMode(m)}
                          className={cx(
                            "h-7 rounded-[6px] px-3 text-[12px] font-medium transition-colors",
                            sidecallMode === m
                              ? "bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg"
                              : "text-muted hover:text-fg2"
                          )}
                        >
                          {m === "agent_default" ? "Agent default model" : "Specific model"}
                        </button>
                      ))}
                    </div>
                    <p className="mt-1.5 text-[11px] leading-5 text-muted">
                      {sidecallMode === "agent_default"
                        ? "Extraction and intent detection run on each agent's own model — or that agent's override in its config."
                        : "Every memory side-call runs on this provider and model, whatever agent produced the turn."}
                    </p>
                    {sidecallMode === "specific" && (
                      <div className="mt-3 grid gap-3 sm:grid-cols-2">
                        <div>
                          <label className="mb-1.5 block text-[12px] font-medium text-fg2" htmlFor="memory-sidecall-provider">
                            Provider
                          </label>
                          <select
                            id="memory-sidecall-provider"
                            data-testid="memory-sidecall-provider"
                            value={sidecallProviderId}
                            onChange={(e) => setSidecallProviderId(e.target.value)}
                            className="h-9 w-full rounded-md border border-line bg-surface px-2 text-[13px] text-fg2 focus:border-accent"
                          >
                            <option value="">Select a provider…</option>
                            {languageProviders.map((pr) => (
                              <option key={pr.id} value={pr.id}>
                                {pr.name}
                              </option>
                            ))}
                          </select>
                        </div>
                        {/* Catalog-backed combobox (same component as the agent
                            config Model tab): lists the provider's catalog, keeps
                            the custom-id escape, and falls back to free text when
                            the catalog resolves nothing. Mounted only in specific
                            mode, so hydration never trips its provider-reset. */}
                        <ModelCombobox
                          workspaceId={targetWsId || undefined}
                          providerId={sidecallProviderId || undefined}
                          model={sidecallModel}
                          onModelChange={setSidecallModel}
                          hideEffort
                        />
                      </div>
                    )}
                  </div>

                  {/* Decision backend (5.4): the intent gate's classifier can
                      ride a decision provider instead of the side-call model.
                      Unchecked stores nothing; unticking and saving omits the
                      pair, which is the server's clear. */}
                  <div data-testid="memory-decision-backend">
                    <div className="flex items-center justify-between gap-3">
                      <div>
                        <p className="text-[12px] font-medium text-fg2">Decision backend</p>
                        <p className="text-[11px] leading-5 text-muted">
                          Classifies each turn&apos;s memory need in place of the side-call model. Entity
                          routing (associative) requires the language model.
                        </p>
                      </div>
                      <Toggle
                        on={useDecisionBackend}
                        label="Use decision backend"
                        onChange={handleDecisionBackendToggle}
                      />
                    </div>
                    {useDecisionBackend && (
                      <div className="mt-3 grid gap-3 sm:grid-cols-2" data-testid="memory-decision-fields">
                        <div>
                          <label className="mb-1.5 block text-[12px] font-medium text-fg2" htmlFor="memory-decision-provider">
                            Provider
                          </label>
                          <select
                            id="memory-decision-provider"
                            data-testid="memory-decision-provider"
                            value={decisionProviderId}
                            onChange={(e) => setDecisionProviderId(e.target.value)}
                            className="h-9 w-full rounded-md border border-line bg-surface px-2 text-[13px] text-fg2 focus:border-accent"
                          >
                            <option value="">Select a provider…</option>
                            {decisionProviders.map((pr) => (
                              <option key={pr.id} value={pr.id}>
                                {pr.name}
                              </option>
                            ))}
                          </select>
                        </div>
                        <div>
                          <label className="mb-1.5 block text-[12px] font-medium text-fg2" htmlFor="memory-decision-model">
                            Model
                          </label>
                          <input
                            id="memory-decision-model"
                            data-testid="memory-decision-model"
                            className="h-9 w-full rounded-md border border-line bg-surface px-3 font-mono text-[13px] text-fg2 placeholder:text-muted focus:border-accent"
                            value={decisionModel}
                            onChange={(e) => setDecisionModel(e.target.value)}
                          />
                        </div>
                      </div>
                    )}
                  </div>

                  <div className="space-y-3 border-t border-[var(--border-soft)] pt-4">
                    <p className="text-[12px] font-medium text-fg2">Embedding provider</p>
                    <div>
                      <label className="mb-1.5 block text-[12px] font-medium text-fg2" htmlFor="memory-embedding-provider">
                        Provider
                      </label>
                      <select
                        id="memory-embedding-provider"
                        data-testid="memory-embedding-provider"
                        className="h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2 text-[13px] text-fg2 focus:border-accent"
                        value={providerId}
                        onChange={(e) => setProviderId(e.target.value)}
                      >
                        <option value="">Select a provider…</option>
                        {languageProviders.map((p) => (
                          <option key={p.id} value={p.id}>
                            {p.name}
                          </option>
                        ))}
                      </select>
                      <p className="mt-1 text-[11px] leading-5 text-muted">
                        Endpoint and API key come from the workspace provider — manage them in Settings → Providers.
                      </p>
                    </div>
                    <div>
                      <label className="mb-1.5 block text-[12px] font-medium text-fg2" htmlFor="memory-embedding-model">
                        Embedding model
                      </label>
                      {modelDropdown ? (
                        <select
                          id="memory-embedding-model"
                          data-testid="memory-embedding-model"
                          className="h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2 text-[13px] text-fg2 focus:border-accent"
                          value={model}
                          onChange={(e) => handleEmbeddingModelChange(e.target.value)}
                        >
                          <option value="">Select an embedding model…</option>
                          {embeddingModels.map((m) => (
                            <option key={m.id} value={m.id}>
                              {m.name || m.id}
                            </option>
                          ))}
                        </select>
                      ) : (
                        <input
                          id="memory-embedding-model"
                          data-testid="memory-embedding-model"
                          className="h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-3 font-mono text-[13px] text-fg2 placeholder:text-muted focus:border-accent"
                          placeholder="text-embedding-3-small — free text; the connection test discovers the dimension"
                          value={model}
                          onChange={(e) => setModel(e.target.value)}
                        />
                      )}
                      <p className="mt-1 text-[11px] leading-5 text-muted">
                        {modelDropdown
                          ? "Embedding-classified models from the provider's catalog."
                          : "No embedding-classified models resolved for this provider — enter the model id; the connection test discovers its dimension."}
                      </p>
                    </div>
                    <div>
                      <label className="mb-1.5 block text-[12px] font-medium text-fg2" htmlFor="memory-embedding-dimension">
                        Dimension
                      </label>
                      <select
                        id="memory-embedding-dimension"
                        data-testid="memory-embedding-dimension"
                        className="h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2 text-[13px] text-fg2 focus:border-accent"
                        value={dimension}
                        onChange={(e) => {
                          setDimension(e.target.value);
                          setDimensionAutoFor(null);
                        }}
                      >
                        <option value="">Auto — detect on test</option>
                        {SUPPORTED_EMBEDDING_DIMS.map((d) => (
                          <option key={d} value={String(d)}>
                            {d}
                          </option>
                        ))}
                      </select>
                    </div>
                    <div className="flex flex-wrap items-center justify-end gap-2">
                      <button
                        type="button"
                        data-testid="btn-memory-test"
                        disabled={testing || !providerId || !model.trim()}
                        onClick={() => void testConnection()}
                        className="h-8 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-40"
                      >
                        {testing ? "Testing…" : "Test connection"}
                      </button>
                      <button
                        type="button"
                        data-testid="btn-memory-save-settings"
                        disabled={savingSettings}
                        onClick={() => void saveSettings()}
                        className="h-8 rounded-md bg-accent px-3.5 text-[12px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
                      >
                        {savingSettings ? "Saving…" : "Save settings"}
                      </button>
                    </div>
                    {testResult && (
                      <p className="text-[12px] text-[color-mix(in_oklab,var(--success),black_25%)]" data-testid="memory-test-result" role="status">
                        {testResult}
                      </p>
                    )}
                    {testError && (
                      <p className="text-[12px] text-danger" data-testid="memory-test-error" role="alert">
                        {testError}
                      </p>
                    )}
                  </div>
                </>
              )}
            </div>
          </section>
          )}
        </>
      )}

      {promoting ? (
        <Modal
          title="Promote to shared?"
          onClose={() => setPromoting(null)}
          odId="modal-memory-promote"
          data-testid="modal-memory-promote"
          footer={
            <>
              <button
                type="button"
                data-testid="btn-memory-promote-cancel"
                onClick={() => setPromoting(null)}
                className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
              >
                Cancel
              </button>
              <button
                type="button"
                data-testid="btn-memory-promote-confirm"
                disabled={noteBusy}
                onClick={() => void handlePromote()}
                className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
              >
                {noteBusy ? "Promoting…" : "Promote"}
              </button>
            </>
          }
        >
          <p className="p-5 text-[13px] leading-5 text-fg2">
            This fact becomes visible to the whole workspace. Promotion is the only widening path and it is
            recorded in the fact's provenance.
          </p>
        </Modal>
      ) : null}

      {deleting ? (
        <Modal
          title="Delete this fact?"
          onClose={() => setDeleting(null)}
          odId="modal-memory-delete"
          data-testid="modal-memory-delete"
          footer={
            <>
              <button
                type="button"
                data-testid="btn-memory-delete-cancel"
                onClick={() => setDeleting(null)}
                className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
              >
                Cancel
              </button>
              <button
                type="button"
                data-testid="btn-memory-delete-confirm"
                disabled={noteBusy}
                onClick={() => void handleDelete()}
                className="flex h-9 items-center rounded-md bg-danger px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
              >
                {noteBusy ? "Deleting…" : "Delete"}
              </button>
            </>
          }
        >
          <p className="p-5 text-[13px] leading-5 text-fg2">
            The fact disappears from every retrieval and injection. The deletion itself is recorded — history
            stays answerable.
          </p>
        </Modal>
      ) : null}
    </div>
  );
}
