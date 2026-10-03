import { useCallback, useEffect, useMemo, useState } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../../components/ui/Icon";
import { Toggle } from "../../components/ui/Toggle";
import { Chip } from "../../components/ui/Chip";
import { Modal } from "../../components/ui/Modal";
import { ErrorState } from "../../components/ErrorState";
import {
  api,
  ApiError,
  formatApiError,
  type ApiWorkspaceSkill,
} from "../../lib/api";
import { useCanWriteSkills, unmetDependencies } from "../../lib/skills";
import serverErrorSvg from "../../assets/server-error.svg";
import { SkillInstallWizard, SkillEditDialog } from "../../modals/SkillDialog";
import { CurationTab } from "./curation/CurationTab";

export interface SkillsPaneProps {
  tenant: any;
  onUpdate?: (fn: any) => void;
  onToast?: (text: string, kind?: string) => void;
  /** Override the derived skills.write check (tests). */
  canWrite?: boolean;
}

type SkillsTab = 'skills' | 'curation';

/** Initial tab from the deep link (?tab=curation) — read once, router-free so
 * the pane also mounts bare (tests) without crashing. */
function initialTabFromLocation(): SkillsTab {
  try {
    return new URLSearchParams(window.location.search).get('tab') === 'curation' ? 'curation' : 'skills';
  } catch {
    return 'skills';
  }
}

/** Candidate id the in-chat chip deep-links (?candidate=<id>). */
function focusCandidateFromLocation(): string | null {
  try {
    return new URLSearchParams(window.location.search).get('candidate');
  } catch {
    return null;
  }
}

// Workspace skill library + locked system tier, backed by the skills API.
// The pane is a two-tab shell (add-skill-curation-from-traces 9.1): Skills —
// the library, exactly as before, the default view — and Curation, the
// review loop, carrying a pending badge that clears when the queue empties.
// The workspace master switch and uninstall are the only library lifecycle
// controls — Members (skills.read without skills.write) get the same lists,
// read-only, on both tabs.
export function SkillsPane({ tenant, onToast = () => {}, canWrite }: SkillsPaneProps) {
  const derivedCanWrite = useCanWriteSkills(tenant);
  const writer = canWrite !== undefined ? canWrite : derivedCanWrite;

  const [tab, setTab] = useState<SkillsTab>(initialTabFromLocation);
  const focusCandidateId = useMemo(focusCandidateFromLocation, []);
  // Pending badge: the review queue's size, null while unknown (fetch failed
  // / workspace absent) — a null or zero badge renders nothing.
  const [pendingCount, setPendingCount] = useState<number | null>(null);

  const [skills, setSkills] = useState<ApiWorkspaceSkill[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<ApiError | Error | null>(null);

  const [wizard, setWizard] = useState(false);
  const [forkTarget, setForkTarget] = useState<string | null>(null);
  const [editing, setEditing] = useState<ApiWorkspaceSkill | null>(null);
  const [uninstalling, setUninstalling] = useState<ApiWorkspaceSkill | null>(null);
  const [busy, setBusy] = useState(false);

  const targetWsId = tenant?.sub || tenant?.id;

  const refreshPendingCount = useCallback(async () => {
    if (!targetWsId) return;
    try {
      const res = await api.curation.listCandidates(targetWsId, 'pending');
      setPendingCount(res.count ?? (res.candidates || []).length);
    } catch (err: unknown) {
      // Offline (status 0) rides the connection banner — don't flash a badge.
      if (err instanceof ApiError && err.status === 0) return;
      setPendingCount(null);
    }
  }, [targetWsId]);

  useEffect(() => {
    if (targetWsId) void refreshPendingCount();
  }, [targetWsId, refreshPendingCount]);

  const loadSkills = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const res = await api.skills.list(targetWsId);
      setSkills(res.skills || []);
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
    if (targetWsId) void loadSkills();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reload per workspace only
  }, [targetWsId]);

  const replaceSkill = (updated: ApiWorkspaceSkill) => {
    setSkills((prev) => prev.map((s) => (s.name === updated.name ? updated : s)));
  };

  const library = useMemo(() => skills.filter((s) => s.tier !== "system"), [skills]);
  const systemSkills = useMemo(() => skills.filter((s) => s.tier === "system"), [skills]);

  const handleToggle = async (skill: ApiWorkspaceSkill) => {
    const next = !(skill.enabled !== false);
    try {
      const res = await api.skills.setEnabled(targetWsId, skill.name, next);
      replaceSkill(res.skill);
      onToast(
        next
          ? `${skill.name} enabled — live on every agent`
          : `${skill.name} disabled — removed from every agent`
      );
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to update ${skill.name}`), "danger");
    }
  };

  const handleUninstall = async (skill: ApiWorkspaceSkill) => {
    setBusy(true);
    try {
      await api.skills.uninstall(targetWsId, skill.name);
      setSkills((prev) => prev.filter((s) => s.name !== skill.name));
      setUninstalling(null);
      onToast(`${skill.name} uninstalled`);
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to uninstall ${skill.name}`), "danger");
    } finally {
      setBusy(false);
    }
  };

  const handleFork = async (skill: ApiWorkspaceSkill) => {
    setBusy(true);
    try {
      const res = await api.skills.create(targetWsId, { source: "fork", system_skill: skill.name });
      setSkills((prev) => [...prev, res.skill]);
      onToast(`${res.skill.name} forked to this workspace — opened for editing`);
      setEditing(res.skill);
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to fork ${skill.name}`), "danger");
    } finally {
      setBusy(false);
    }
  };

  const openEdit = async (skill: ApiWorkspaceSkill) => {
    try {
      const res = await api.skills.get(targetWsId, skill.name);
      setEditing(res.skill);
    } catch {
      setEditing(skill); // list row carries enough for a best-effort edit
    }
  };

  const depChipTitle = (skill: ApiWorkspaceSkill) =>
    "Unmet: " + unmetDependencies(skill).map((d) => d.name).join(", ");

  const renderRow = (skill: ApiWorkspaceSkill, locked: boolean) => {
    const enabled = skill.enabled !== false;
    const unmet = unmetDependencies(skill);
    return (
      <li
        key={skill.tier + "-" + skill.name}
        className={cx("flex items-center gap-3 py-3", !enabled && "opacity-70")}
        data-od-id={"skill-" + skill.name}
        data-testid={"skill-" + skill.name}
      >
        <span
          className={cx(
            "flex h-9 w-9 shrink-0 items-center justify-center rounded-md",
            enabled
              ? "bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent"
              : "bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted"
          )}
        >
          <Icon name={locked ? "lock" : "spark"} size={16} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <p className="font-mono text-[13px] font-medium text-fg">{skill.name}</p>
            <Chip mono>v{skill.version}</Chip>
            <Chip>{skill.source}</Chip>
            {unmet.length > 0 ? (
              <Chip
                className="border-[color-mix(in_oklab,var(--warn)_45%,transparent)] text-[color-mix(in_oklab,var(--warn),black_38%)]"
                // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
              >
                <span title={depChipTitle(skill)} data-testid={"skill-dep-warning-" + skill.name}>
                  {unmet.length} unmet {unmet.length === 1 ? "dependency" : "dependencies"}
                </span>
              </Chip>
            ) : null}
            {locked ? <span className="text-[11px] text-muted">always on</span> : null}
          </div>
          {skill.description ? <p className="mt-0.5 text-[12px] leading-4 text-muted">{skill.description}</p> : null}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {writer && locked ? (
            <button
              type="button"
              onClick={() => handleFork(skill)}
              disabled={busy}
              data-testid={"btn-fork-" + skill.name}
              className="flex h-7 items-center gap-1.5 rounded-[6px] border border-line px-2 text-[11px] font-medium text-muted transition-colors hover:border-accent hover:text-fg"
            >
              <Icon name="spark" size={12} /> Fork to workspace
            </button>
          ) : null}
          {writer && !locked ? (
            <>
              <Toggle
                on={enabled}
                label={"Enable " + skill.name}
                onChange={() => handleToggle(skill)}
              />
              <button
                type="button"
                aria-label={"Edit " + skill.name}
                data-od-id={"btn-edit-" + skill.name}
                data-testid={"btn-edit-" + skill.name}
                onClick={() => void openEdit(skill)}
                className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
              >
                <Icon name="edit" size={13} />
              </button>
              <button
                type="button"
                aria-label={"Uninstall " + skill.name}
                data-testid={"btn-uninstall-" + skill.name}
                onClick={() => setUninstalling(skill)}
                className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger"
              >
                <Icon name="trash" size={13} />
              </button>
            </>
          ) : null}
        </div>
      </li>
    );
  };

  return (
    <div className="max-w-xl" data-od-id="pane-skills" data-testid="pane-skills">
      {/* Two-tab shell (skill-curation spec): Skills — the library, the
          default view, identical with or without curation — and Curation,
          badged with the pending review count. */}
      <div className="mb-4 flex gap-1 border-b border-linesoft" role="tablist" aria-label="Skills sections" data-testid="skills-pane-tabs">
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'skills'}
          onClick={() => setTab('skills')}
          data-od-id="skills-tab-library"
          data-testid="skills-tab-library"
          className={cx(
            '-mb-px flex h-9 items-center border-b-2 px-3 text-[13px] transition-colors',
            tab === 'skills'
              ? 'border-accent font-medium text-fg'
              : 'border-transparent text-muted hover:text-fg2'
          )}
        >
          Skills
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'curation'}
          onClick={() => setTab('curation')}
          data-od-id="skills-tab-curation"
          data-testid="skills-tab-curation"
          className={cx(
            '-mb-px flex h-9 items-center gap-1.5 border-b-2 px-3 text-[13px] transition-colors',
            tab === 'curation'
              ? 'border-accent font-medium text-fg'
              : 'border-transparent text-muted hover:text-fg2'
          )}
        >
          Curation
          {pendingCount ? (
            <span
              className="flex h-[18px] min-w-[18px] items-center justify-center rounded-full bg-accent px-1 font-mono text-[10px] font-semibold text-accenton"
              data-testid="curation-pending-badge"
            >
              {pendingCount}
            </span>
          ) : null}
        </button>
      </div>

      {tab === 'curation' ? (
        <CurationTab
          tenant={tenant}
          canWrite={writer}
          onToast={onToast}
          focusCandidateId={focusCandidateId}
          onQueueChanged={() => void refreshPendingCount()}
        />
      ) : (
        <>
      <div className="mb-4 flex items-center justify-between">
        <div>
          <h3 className="text-[15px] font-semibold text-fg">Workspace skills</h3>
          <p className="mt-0.5 text-[12px] text-muted">
            Enabled skills attach to every agent in this workspace. The master switch and uninstall are the only controls.
          </p>
        </div>
        {writer ? (
          <button
            type="button"
            onClick={() => setWizard(true)}
            data-od-id="btn-skill-add"
            data-testid="btn-skill-add"
            className="h-9 shrink-0 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
          >
            Install skill
          </button>
        ) : null}
      </div>

      {loading && skills.length === 0 ? (
        <div className="space-y-3">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-16 animate-pulse rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]" />
          ))}
        </div>
      ) : loadError ? (
        <div className="py-6">
          <ErrorState
            illustration={serverErrorSvg}
            title="Couldn't load skills"
            detail={loadError.message}
            primaryAction={{ label: "Retry", onClick: loadSkills }}
          />
        </div>
      ) : (
        <ul className="divide-y divide-[var(--border-soft)]" data-testid="skills-library">
          {library.map((s) => renderRow(s, false))}
          {library.length === 0 && (
            <li className="py-8 text-center" data-od-id="skills-empty" data-testid="skills-empty">
              <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
                <Icon name="spark" size={20} />
              </div>
              <p className="text-[14px] font-medium text-fg">No skills installed</p>
              <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
                Install a capability package to extend every agent&apos;s abilities.
              </p>
              {writer ? (
                <button
                  type="button"
                  onClick={() => setWizard(true)}
                  data-od-id="btn-skill-empty-add"
                  data-testid="btn-skill-empty-add"
                  className="mt-3 inline-flex h-8 items-center gap-1.5 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
                >
                  <Icon name="plus" size={13} /> Install your first skill
                </button>
              ) : null}
            </li>
          )}
        </ul>
      )}

      {systemSkills.length > 0 ? (
        <div className="mt-8" data-testid="system-skills-section">
          <h3 className="text-[15px] font-semibold text-fg">System skills</h3>
          <p className="mt-0.5 text-[12px] text-muted">
            Embedded and always on for every agent — fork one to customize it in this workspace.
          </p>
          <ul className="mt-2 divide-y divide-[var(--border-soft)]">
            {systemSkills.map((s) => renderRow(s, true))}
          </ul>
        </div>
      ) : null}

      {wizard ? (
        <SkillInstallWizard
          wsSlug={targetWsId}
          systemSkills={systemSkills}
          existingNames={library.map((s) => s.name)}
          initialSource={forkTarget ? "fork" : undefined}
          initialFork={forkTarget}
          onClose={() => {
            setWizard(false);
            setForkTarget(null);
          }}
          onInstalled={(skill) => {
            setSkills((prev) => [...prev.filter((s) => s.name !== skill.name), skill]);
          }}
          onToast={onToast}
        />
      ) : null}

      {editing ? (
        <SkillEditDialog
          skill={editing}
          wsSlug={targetWsId}
          onClose={() => setEditing(null)}
          onSaved={(updated) => replaceSkill(updated)}
          onToast={onToast}
        />
      ) : null}

      {uninstalling ? (
        <Modal
          title={`Uninstall ${uninstalling.name}`}
          onClose={() => setUninstalling(null)}
          odId="modal-skill-uninstall"
          data-testid="modal-skill-uninstall"
          footer={
            <>
              <button
                type="button"
                onClick={() => setUninstalling(null)}
                data-testid="btn-uninstall-cancel"
                className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => handleUninstall(uninstalling)}
                disabled={busy}
                data-testid="btn-uninstall-confirm"
                className="flex h-9 items-center rounded-md bg-danger px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
              >
                {busy ? "Uninstalling…" : "Uninstall"}
              </button>
            </>
          }
        >
          <p className="p-5 text-[13px] leading-5 text-fg2">
            {uninstalling.name} is removed from every agent in this workspace and its files are deleted. This cannot be
            undone.
          </p>
        </Modal>
      ) : null}
        </>
      )}
    </div>
  );
}
