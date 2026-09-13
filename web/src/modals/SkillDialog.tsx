import { useMemo, useState } from "react";
import { Modal } from "../components/ui/Modal";
import { inputCls, labelCls } from "../components/ui/constants";
import { cx } from "../lib/helpers";
import { Icon } from "../components/ui/Icon";
import { Chip } from "../components/ui/Chip";
import { Toggle } from "../components/ui/Toggle";
import {
  api,
  ApiError,
  formatApiError,
  type ApiWorkspaceSkill,
  type ApiSkillDependencyStatus,
  type ApiSkillInspectResult,
  type ApiDiscoveredSkill,
} from "../lib/api";

export type InstallSource = "author" | "upload" | "git" | "fork";

const SOURCE_OPTIONS: Array<{ id: InstallSource; label: string; hint: string; icon: string }> = [
  { id: "author", label: "Author", hint: "Write the SKILL.md body here — installs as version 0.1.0.", icon: "edit" },
  { id: "upload", label: "Upload", hint: "Drop a zip archive with SKILL.md at its root.", icon: "clip" },
  { id: "git", label: "Git or URL", hint: "Shallow-fetch a repo or archive and pick the skills inside.", icon: "link" },
  { id: "fork", label: "Fork a system skill", hint: "Copy an embedded skill into this workspace to customize it.", icon: "spark" },
];

const STEP_LABELS = ["Source", "Content", "Dependencies"];

export interface SkillInstallWizardProps {
  wsSlug: string;
  /** System-tier entries (locked) — fork source and fork launches feed from these. */
  systemSkills: ApiWorkspaceSkill[];
  existingNames: string[];
  initialSource?: InstallSource;
  initialFork?: string | null;
  onClose: () => void;
  onInstalled: (skill: ApiWorkspaceSkill) => void;
  onToast: (text: string, kind?: string) => void;
}

// Install wizard: source → content → dependency review. One pipeline, four
// sources (author / upload / git / fork). Installing with unmet dependencies
// is allowed — the row keeps a persistent warning chip.
export function SkillInstallWizard({
  wsSlug,
  systemSkills,
  existingNames,
  initialSource,
  initialFork,
  onClose,
  onInstalled,
  onToast,
}: SkillInstallWizardProps) {
  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [source, setSource] = useState<InstallSource>(initialSource || "author");

  // Author fields
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [body, setBody] = useState("");

  // Upload fields
  const [file, setFile] = useState<File | null>(null);
  const [inspect, setInspect] = useState<ApiSkillInspectResult | null>(null);
  const [inspectError, setInspectError] = useState<string | null>(null);
  const [inspecting, setInspecting] = useState(false);

  // Git fields
  const [gitUrl, setGitUrl] = useState("");
  const [gitRef, setGitRef] = useState("");
  const [gitToken, setGitToken] = useState("");
  const [discovered, setDiscovered] = useState<ApiDiscoveredSkill[] | null>(null);
  const [selected, setSelected] = useState<string[]>([]);
  const [discovering, setDiscovering] = useState(false);
  const [discoveryError, setDiscoveryError] = useState<string | null>(null);

  // Fork fields
  const [forkName, setForkName] = useState(initialFork || systemSkills[0]?.name || "");

  // Dependency review
  const [enableEverywhere, setEnableEverywhere] = useState(true);
  const [provisionPython, setProvisionPython] = useState(true);
  const [dependencyStatus, setDependencyStatus] = useState<ApiSkillDependencyStatus[]>([]);
  const [rechecking, setRechecking] = useState(false);

  const [installing, setInstalling] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [conflict, setConflict] = useState<string | null>(null);

  const slug = name.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
  const authorValid = source !== "author" || (slug.length > 0 && body.trim().length > 0);
  const uploadValid = source !== "upload" || (inspect !== null && !inspectError);
  const gitValid = source !== "git" || (discovered !== null && selected.length > 0);
  const forkValid = source !== "fork" || forkName.length > 0;
  const contentValid = authorValid && uploadValid && gitValid && forkValid;

  const skillsToInstall = useMemo(
    () => (source === "git" ? selected : [source === "fork" ? forkName : slug || inspect?.skill.name || ""]),
    [source, selected, forkName, slug, inspect]
  );
  const hasConflict = skillsToInstall.some((n) => existingNames.includes(n));

  const runInspect = async (f: File) => {
    setInspecting(true);
    setInspectError(null);
    setInspect(null);
    try {
      const res = await api.skills.inspectUpload(wsSlug, f);
      setInspect(res);
    } catch (err: unknown) {
      setInspectError(formatApiError(err, "The archive could not be inspected."));
    } finally {
      setInspecting(false);
    }
  };

  const runDiscovery = async () => {
    setDiscovering(true);
    setDiscoveryError(null);
    try {
      const res = await api.skills.inspectGit(wsSlug, {
        url: gitUrl.trim(),
        ...(gitRef.trim() ? { ref: gitRef.trim() } : {}),
        ...(gitToken.trim() ? { token: gitToken.trim() } : {}),
      });
      const found = res.skills || [];
      setDiscovered(found);
      setSelected(found.map((s) => s.name));
    } catch (err: unknown) {
      setDiscoveryError(formatApiError(err, "The repository could not be fetched."));
    } finally {
      setDiscovering(false);
    }
  };

  const gatherDependencies = (): ApiSkillDependencyStatus[] => {
    if (source === "upload" && inspect) return inspect.skill.dependency_status || [];
    if (source === "git" && discovered) {
      return (discovered || [])
        .filter((s) => selected.includes(s.name))
        .flatMap((s) => s.dependency_status || []);
    }
    if (source === "fork") {
      const sys = systemSkills.find((s) => s.name === forkName);
      return sys?.dependency_status || [];
    }
    return [];
  };

  const handleContinue = () => {
    if (step === 1) {
      setStep(2);
      return;
    }
    if (step === 2) {
      if (!contentValid) return;
      setDependencyStatus(gatherDependencies());
      setStep(3);
    }
  };

  const recheck = async () => {
    setRechecking(true);
    try {
      if (source === "upload" && file) {
        const res = await api.skills.inspectUpload(wsSlug, file);
        setInspect(res);
        setDependencyStatus(res.skill.dependency_status || []);
      } else if (source === "git" && discovered) {
        const res = await api.skills.inspectGit(wsSlug, {
          url: gitUrl.trim(),
          ...(gitRef.trim() ? { ref: gitRef.trim() } : {}),
          ...(gitToken.trim() ? { token: gitToken.trim() } : {}),
        });
        setDiscovered(res.skills || []);
        setDependencyStatus((res.skills || []).filter((s) => selected.includes(s.name)).flatMap((s) => s.dependency_status || []));
      }
    } catch (err: unknown) {
      onToast(formatApiError(err, "Re-check failed"), "danger");
    } finally {
      setRechecking(false);
    }
  };

  const install = async (overwrite: boolean) => {
    setInstalling(true);
    setError(null);
    try {
      const options = {
        enable_everywhere: enableEverywhere,
        provision_python: provisionPython,
        overwrite,
      };
      let result;
      if (source === "author") {
        result = await api.skills.create(wsSlug, {
          source: "authored",
          name: slug,
          description: description.trim() || undefined,
          body: body,
          ...options,
        });
      } else if (source === "upload" && file && inspect) {
        result = await api.skills.createUpload(wsSlug, { archive: file, name: inspect.skill.name, ...options });
      } else if (source === "git") {
        result = await api.skills.create(wsSlug, {
          source: "git",
          url: gitUrl.trim(),
          ...(gitRef.trim() ? { ref: gitRef.trim() } : {}),
          ...(gitToken.trim() ? { token: gitToken.trim() } : {}),
          names: selected,
          ...options,
        });
      } else if (source === "fork") {
        result = await api.skills.create(wsSlug, { source: "fork", system_skill: forkName, ...options });
      } else {
        return;
      }
      onInstalled(result.skill);
      const unmet = (result.dependency_status || []).filter((d) => d.status !== "met");
      onToast(
        unmet.length
          ? `${result.skill.name} installed with ${unmet.length} unmet ${unmet.length === 1 ? "dependency" : "dependencies"} — live on all agents`
          : `${result.skill.name} installed — live on all agents`
      );
      onClose();
    } catch (err: unknown) {
      const status = err instanceof ApiError ? err.status : (err as any)?.status;
      const code = err instanceof ApiError ? err.code : (err as any)?.code;
      const message = err instanceof Error ? err.message : String(err);
      if (status === 409 || code === "conflict") {
        setConflict(message || "A skill with this name already exists in this workspace.");
      } else {
        setError(formatApiError(err, "Install failed"));
      }
    } finally {
      setInstalling(false);
    }
  };

  const back = () => (step === 1 ? onClose() : setStep((step - 1) as 1 | 2));

  const canContinue =
    step === 1 ||
    (step === 2 && contentValid) ||
    (step === 3 && !installing);

  return (
    <Modal
      title="Install skill"
      onClose={onClose}
      odId="modal-skill"
      data-testid="modal-skill"
      footer={
        <>
          <button
            type="button"
            onClick={back}
            data-testid="btn-skill-back"
            className="flex h-9 items-center gap-1.5 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            <Icon name="arrow-left" size={14} />
            {step === 1 ? "Cancel" : "Back"}
          </button>
          <div className="flex-1" />
          {conflict && step === 3 ? (
            <div className="mr-2 flex items-center gap-2" data-testid="skill-conflict">
              <span className="text-[12px] text-fg2">{conflict}</span>
              <button
                type="button"
                onClick={() => install(true)}
                disabled={installing}
                data-testid="btn-skill-overwrite"
                className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
              >
                Overwrite
              </button>
            </div>
          ) : null}
          {step < 3 ? (
            <button
              type="button"
              onClick={handleContinue}
              disabled={!canContinue}
              data-testid="btn-skill-continue"
              className="flex h-9 items-center gap-1.5 rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40"
            >
              {step === 1 ? "Choose content" : "Review dependencies"}
              <Icon name="arrow-right" size={14} />
            </button>
          ) : (
            <button
              type="button"
              onClick={() => install(Boolean(conflict))}
              disabled={installing}
              data-testid="btn-skill-install"
              className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40"
            >
              {installing ? "Installing…" : conflict ? "Install anyway" : "Install"}
            </button>
          )}
        </>
      }
    >
      <div className="space-y-5 p-5">
        {/* Step indicator */}
        <div className="flex items-center gap-3 border-b border-linesoft pb-3">
          {STEP_LABELS.map((label, i) => {
            const n = (i + 1) as 1 | 2 | 3;
            return (
              <div key={label} className="flex items-center gap-2">
                {n > 1 && <span className="text-muted">/</span>}
                <span
                  className={cx(
                    "flex h-6 w-6 items-center justify-center rounded-full text-[12px] font-semibold",
                    step === n ? "bg-accent text-accenton" : "bg-[color-mix(in_oklab,var(--fg)_10%,transparent)] text-muted"
                  )}
                >
                  {n}
                </span>
                <span className={cx("text-[13px] font-medium", step === n ? "text-fg" : "text-muted")}>{label}</span>
              </div>
            );
          })}
        </div>

        {error ? (
          <p className="text-[13px] text-danger" data-testid="skill-wizard-error">
            {error}
          </p>
        ) : null}

        {/* Step 1 — source */}
        {step === 1 && (
          <div className="space-y-2" data-testid="wizard-source-step">
            {SOURCE_OPTIONS.map((opt) => (
              <button
                key={opt.id}
                type="button"
                onClick={() => setSource(opt.id)}
                aria-pressed={source === opt.id}
                data-testid={"wizard-source-" + opt.id}
                className={cx(
                  "flex w-full items-start gap-3 rounded-md border p-3 text-left transition-colors",
                  source === opt.id
                    ? "border-accent bg-[color-mix(in_oklab,var(--accent)_8%,transparent)]"
                    : "border-line hover:border-[color-mix(in_oklab,var(--fg)_26%,transparent)]"
                )}
              >
                <span className={cx("mt-0.5", source === opt.id ? "text-accent" : "text-muted")}>
                  <Icon name={opt.icon} size={16} />
                </span>
                <span>
                  <span className="block text-[13px] font-medium text-fg">{opt.label}</span>
                  <span className="mt-0.5 block text-[12px] leading-4 text-muted">{opt.hint}</span>
                </span>
              </button>
            ))}
          </div>
        )}

        {/* Step 2 — content */}
        {step === 2 && source === "author" && (
          <div className="space-y-4" data-testid="wizard-author-step">
            <div>
              <label className={labelCls} htmlFor="skill-name">
                Skill name
              </label>
              <input
                id="skill-name"
                className={inputCls}
                placeholder="e.g. Changelog sweeper"
                value={name}
                aria-label="Skill name"
                data-testid="input-skill-name"
                onChange={(e) => setName(e.target.value)}
                autoFocus
              />
              {slug ? <p className="mt-1 font-mono text-[11px] text-muted">installs as ${slug || "name"}</p> : null}
            </div>
            <div>
              <label className={labelCls} htmlFor="skill-desc">
                Description <span className="text-muted font-normal">(optional)</span>
              </label>
              <input
                id="skill-desc"
                className={cx(inputCls, "text-[13px]")}
                placeholder="One-line description (optional)"
                value={description}
                aria-label="Skill description"
                data-testid="input-skill-desc"
                onChange={(e) => setDescription(e.target.value)}
              />
            </div>
            <div>
              <label className={labelCls} htmlFor="skill-body">
                SKILL.md body <span className="text-accenttext font-normal">(markdown)</span>
              </label>
              <textarea
                id="skill-body"
                rows={10}
                className={cx(inputCls, "h-auto py-2 font-mono text-[12px] leading-relaxed")}
                placeholder={"---\ndescription: What this skill does\n---\n\nInstructions the agent follows when this skill is invoked…"}
                value={body}
                aria-label="Skill body"
                data-testid="input-skill-body"
                onChange={(e) => setBody(e.target.value)}
              />
            </div>
          </div>
        )}

        {step === 2 && source === "upload" && (
          <div className="space-y-4" data-testid="wizard-upload-step">
            <div>
              <label className={labelCls} htmlFor="skill-archive">
                Archive <span className="text-muted font-normal">(zip with SKILL.md at its root)</span>
              </label>
              <input
                id="skill-archive"
                type="file"
                accept=".zip,application/zip"
                className={inputCls}
                aria-label="Skill archive"
                data-testid="input-skill-archive"
                onChange={(e) => {
                  const f = e.target.files?.[0] || null;
                  setFile(f);
                  setInspect(null);
                  setInspectError(null);
                  if (f) void runInspect(f);
                }}
              />
            </div>
            {inspecting ? <p className="text-[12px] text-muted">Inspecting archive…</p> : null}
            {inspectError ? (
              <p className="rounded-md border border-danger/30 bg-danger/10 p-2.5 text-[12px] text-danger" data-testid="upload-inspect-error">
                {inspectError}
              </p>
            ) : null}
            {inspect ? (
              <div data-testid="upload-tree-preview">
                <p className="mb-1.5 text-[13px] font-medium text-fg">
                  {inspect.skill.name}
                  {inspect.skill.description ? <span className="ml-2 font-normal text-muted">{inspect.skill.description}</span> : null}
                </p>
                <ul className="max-h-48 overflow-auto rounded-md border border-line bg-warm p-2 font-mono text-[11px] leading-5 text-fg2">
                  {inspect.files.map((f) => (
                    <li key={f}>{f}</li>
                  ))}
                </ul>
              </div>
            ) : null}
          </div>
        )}

        {step === 2 && source === "git" && (
          <div className="space-y-4" data-testid="wizard-git-step">
            <div>
              <label className={labelCls} htmlFor="skill-git-url">
                Git URL
              </label>
              <input
                id="skill-git-url"
                className={inputCls}
                placeholder="https://github.com/acme/agent-skills"
                value={gitUrl}
                aria-label="Git URL"
                data-testid="input-skill-git-url"
                onChange={(e) => setGitUrl(e.target.value)}
              />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className={labelCls} htmlFor="skill-git-ref">
                  Ref <span className="text-muted font-normal">(optional)</span>
                </label>
                <input
                  id="skill-git-ref"
                  className={inputCls}
                  placeholder="branch, tag, or commit"
                  value={gitRef}
                  aria-label="Git ref"
                  data-testid="input-skill-git-ref"
                  onChange={(e) => setGitRef(e.target.value)}
                />
              </div>
              <div>
                <label className={labelCls} htmlFor="skill-git-token">
                  Token <span className="text-muted font-normal">(one-time, never stored)</span>
                </label>
                <input
                  id="skill-git-token"
                  type="password"
                  className={inputCls}
                  value={gitToken}
                  aria-label="Fetch token"
                  data-testid="input-skill-git-token"
                  onChange={(e) => setGitToken(e.target.value)}
                />
              </div>
            </div>
            <button
              type="button"
              onClick={runDiscovery}
              disabled={!gitUrl.trim() || discovering}
              data-testid="btn-skill-discover"
              className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-40"
            >
              {discovering ? "Fetching…" : "Fetch skills"}
            </button>
            {discoveryError ? (
              <p className="rounded-md border border-danger/30 bg-danger/10 p-2.5 text-[12px] text-danger" data-testid="git-discovery-error">
                {discoveryError}
              </p>
            ) : null}
            {discovered ? (
              <div className="space-y-1.5" data-testid="git-skill-list">
                <p className="text-[12px] text-muted">{discovered.length} skill{discovered.length === 1 ? "" : "s"} found — only the checked ones install.</p>
                {discovered.map((s) => (
                  <label
                    key={s.name}
                    className={cx(
                      "flex cursor-pointer items-start gap-2.5 rounded-md border p-2.5 transition-colors",
                      selected.includes(s.name) ? "border-accent bg-[color-mix(in_oklab,var(--accent)_8%,transparent)]" : "border-line"
                    )}
                  >
                    <input
                      type="checkbox"
                      checked={selected.includes(s.name)}
                      onChange={(e) =>
                        setSelected((prev) => (e.target.checked ? [...prev, s.name] : prev.filter((n) => n !== s.name)))
                      }
                      data-testid={"git-skill-" + s.name}
                      className="mt-0.5"
                    />
                    <span>
                      <span className="block font-mono text-[12px] text-fg">{s.name}</span>
                      {s.description ? <span className="block text-[12px] text-muted">{s.description}</span> : null}
                    </span>
                  </label>
                ))}
              </div>
            ) : null}
          </div>
        )}

        {step === 2 && source === "fork" && (
          <div className="space-y-4" data-testid="wizard-fork-step">
            <div>
              <label className={labelCls} htmlFor="skill-fork-source">
                System skill to fork
              </label>
              <select
                id="skill-fork-source"
                className={inputCls}
                value={forkName}
                aria-label="System skill to fork"
                data-testid="select-skill-fork-source"
                onChange={(e) => setForkName(e.target.value)}
              >
                {systemSkills.map((s) => (
                  <option key={s.name} value={s.name}>
                    {s.name}
                  </option>
                ))}
              </select>
              <p className="mt-1.5 text-[12px] leading-4 text-muted">
                The system skill stays locked and unchanged — the fork lands in this workspace with a {" "}
                <span className="font-mono">fork</span> badge and opens for editing.
              </p>
            </div>
          </div>
        )}

        {/* Step 3 — dependency review */}
        {step === 3 && (
          <div className="space-y-4" data-testid="wizard-review-step">
            {dependencyStatus.length === 0 ? (
              <p className="text-[13px] text-muted">No dependencies declared or inferred — install as-is.</p>
            ) : (
              <ul className="divide-y divide-linesoft rounded-md border border-line">
                {dependencyStatus.map((d, i) => (
                  <li key={d.kind + "-" + d.name + "-" + i} className="flex items-start gap-3 p-3" data-testid={"dep-" + d.kind + "-" + d.name}>
                    <span
                      className={cx(
                        "mt-0.5 font-mono text-[11px]",
                        d.status === "met" ? "text-success" : "text-[color-mix(in_oklab,var(--warn),black_38%)]"
                      )}
                    >
                      {d.kind}
                    </span>
                    <div className="min-w-0 flex-1">
                      <p className="font-mono text-[12px] text-fg">{d.name}</p>
                      {d.kind === "binaries" && d.install_hint ? (
                        <div className="mt-1 flex items-center gap-2">
                          <code className="rounded bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] px-1.5 py-0.5 font-mono text-[11px] text-fg2">
                            {d.install_hint}
                          </code>
                          <button
                            type="button"
                            onClick={() => {
                              void navigator.clipboard?.writeText(d.install_hint || "");
                              onToast("Install command copied");
                            }}
                            data-testid={"btn-copy-" + d.name}
                            className="text-[11px] font-medium text-accenttext transition-colors hover:text-[var(--accent-hover)]"
                          >
                            Copy
                          </button>
                        </div>
                      ) : null}
                      {d.detail ? <p className="mt-0.5 text-[11px] text-muted">{d.detail}</p> : null}
                    </div>
                    <Chip className={d.status === "met" ? "" : "border-[color-mix(in_oklab,var(--warn)_45%,transparent)] text-[color-mix(in_oklab,var(--warn),black_38%)]"}>
                      {d.status}
                    </Chip>
                  </li>
                ))}
              </ul>
            )}

            {dependencyStatus.some((d) => d.kind === "binaries" && d.status !== "met") && (source === "upload" || source === "git") ? (
              <button
                type="button"
                onClick={recheck}
                disabled={rechecking}
                data-testid="btn-dep-recheck"
                className="flex h-8 items-center rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-40"
              >
                {rechecking ? "Re-checking…" : "Re-check"}
              </button>
            ) : null}

            <label className="flex cursor-pointer items-start gap-2.5 rounded-md border border-line p-3" data-testid="dep-enable-everywhere">
              <input
                type="checkbox"
                checked={enableEverywhere}
                onChange={(e) => setEnableEverywhere(e.target.checked)}
                className="mt-0.5"
              />
              <span>
                <span className="block text-[13px] font-medium text-fg">Enable missing tools everywhere</span>
                <span className="mt-0.5 block text-[12px] leading-4 text-muted">
                  Adds missing tools to the workspace gate and every agent&apos;s allowlist in one confirm.
                </span>
              </span>
            </label>

            <label className="flex cursor-pointer items-start gap-2.5 rounded-md border border-line p-3" data-testid="dep-provision-python">
              <input
                type="checkbox"
                checked={provisionPython}
                onChange={(e) => setProvisionPython(e.target.checked)}
                className="mt-0.5"
              />
              <span>
                <span className="block text-[13px] font-medium text-fg">Provision python packages</span>
                <span className="mt-0.5 block text-[12px] leading-4 text-muted">
                  Pip-installs the union of enabled skills&apos; requirements into the shared workspace venv.
                </span>
              </span>
            </label>

            {hasConflict && !conflict ? (
              <p className="rounded-md border border-[color-mix(in_oklab,var(--warn)_45%,transparent)] bg-[color-mix(in_oklab,var(--warn)_8%,transparent)] p-2.5 text-[12px] text-fg2" data-testid="skill-name-conflict-hint">
                A skill with this name already exists — installing will replace its files and bump the version.
              </p>
            ) : null}
          </div>
        )}
      </div>
    </Modal>
  );
}

export interface SkillEditDialogProps {
  skill: ApiWorkspaceSkill;
  wsSlug: string;
  onClose: () => void;
  onSaved: (skill: ApiWorkspaceSkill) => void;
  onToast: (text: string, kind?: string) => void;
}

export function SkillEditDialog({ skill, wsSlug, onClose, onSaved, onToast }: SkillEditDialogProps) {
  const [description, setDescription] = useState(skill.description || "");
  const [body, setBody] = useState(skill.body || "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      const res = await api.skills.update(wsSlug, skill.name, {
        description: description.trim() || undefined,
        body,
      });
      onSaved(res.skill);
      onToast(`${res.skill.name} updated`);
      onClose();
    } catch (err: unknown) {
      setError(formatApiError(err, "Failed to save skill"));
      onToast(formatApiError(err, "Failed to save skill"), "danger");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      title={`Edit ${skill.name}`}
      onClose={onClose}
      odId="modal-skill-edit"
      data-testid="modal-skill-edit"
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            data-testid="btn-skill-edit-cancel"
            className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={save}
            disabled={saving}
            data-testid="btn-skill-save"
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] disabled:opacity-40"
          >
            {saving ? "Saving…" : "Save changes"}
          </button>
        </>
      }
    >
      <div className="space-y-4 p-5">
        {error ? <p className="text-[13px] text-danger">{error}</p> : null}
        <div className="flex items-center gap-2">
          <p className="font-mono text-[13px] text-fg">{skill.name}</p>
          <Chip mono>v{skill.version}</Chip>
          <Chip>{skill.source}</Chip>
        </div>
        <div>
          <label className={labelCls} htmlFor="skill-edit-desc">
            Description
          </label>
          <input
            id="skill-edit-desc"
            className={inputCls}
            value={description}
            aria-label="Skill description"
            data-testid="input-skill-edit-desc"
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
        <div>
          <label className={labelCls} htmlFor="skill-edit-body">
            SKILL.md body <span className="text-muted font-normal">(markdown)</span>
          </label>
          <textarea
            id="skill-edit-body"
            rows={12}
            className={cx(inputCls, "h-auto py-2 font-mono text-[12px] leading-relaxed")}
            value={body}
            aria-label="Skill body"
            data-testid="input-skill-edit-body"
            onChange={(e) => setBody(e.target.value)}
          />
        </div>
      </div>
    </Modal>
  );
}
