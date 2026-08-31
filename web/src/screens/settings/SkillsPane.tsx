import { useState } from "react";
import { cx, fmtUses } from "../../lib/helpers";
import { Icon } from "../../components/ui/Icon";
import { Toggle } from "../../components/ui/Toggle";
import { Chip } from "../../components/ui/Chip";
import { SkillDialog } from "../../modals/SkillDialog";
import type { Workspace, Skill } from "../../data/types";

export function SkillsPane({
  tenant,
  onUpdate,
  onToast,
}: {
  tenant: Workspace;
  onUpdate: (fn: (t: Workspace) => Workspace) => void;
  onToast: (msg: string, type?: string) => void;
}) {
  const [dialogState, setDialogState] = useState<
    { mode: 'install' } | { mode: 'edit'; skill: Skill } | null
  >(null);

  const lib = tenant.skillLib || [];
  const usersOf = (id: string) => tenant.agents.filter((a: any) => (a.skills || []).includes(id));

  return (
    <div className="max-w-xl" data-od-id="pane-skills" data-testid="pane-skills">
      {lib.length > 0 && (
        <div className="mb-4 flex justify-end">
          <button
            type="button"
            data-od-id="btn-skill-add"
            data-testid="btn-skill-add"
            onClick={() => setDialogState({ mode: 'install' })}
            className="h-9 shrink-0 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
          >
            Install skill
          </button>
        </div>
      )}

      <ul className="divide-y divide-[var(--border-soft)]">
        {lib.map((s: any) => {
          const users = usersOf(s.id);
          return (
            <li
              key={s.id}
              className={cx('flex items-center gap-3 py-3', !s.enabled && 'opacity-70')}
              data-od-id={'skill-' + s.id}
              data-testid={'skill-' + s.id}
            >
              <span
                className={cx(
                  'flex h-9 w-9 shrink-0 items-center justify-center rounded-md',
                  s.enabled
                    ? 'bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent'
                    : 'bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted'
                )}
              >
                <Icon name="spark" size={16} />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="text-[14px] font-medium text-fg">{s.name}</p>
                  <Chip mono>v{s.version}</Chip>
                  {s.source === 'workspace' && <Chip>custom</Chip>}
                </div>
                <p className="mt-0.5 text-[12px] leading-4 text-muted">{s.desc}</p>
              </div>
              <div
                className="shrink-0 text-right"
                title={users.map((u: any) => u.name).join(', ') || undefined}
              >
                <p className="font-mono text-[11px] text-fg2">
                  {fmtUses(s.uses)} {s.uses === 1 ? 'run' : 'runs'}
                </p>
                <p className="font-mono text-[11px] text-muted">
                  {users.length === 0
                    ? 'no agents'
                    : users.length + (users.length === 1 ? ' agent' : ' agents')}
                </p>
              </div>
              <Toggle
                on={s.enabled}
                label={'Enable ' + s.name}
                onChange={(v: boolean) => {
                  onUpdate((t: any) => ({
                    ...t,
                    skillLib: t.skillLib.map((x: Skill) =>
                      x.id === s.id ? { ...x, enabled: v } : x
                    ),
                  }));
                  onToast(
                    v
                      ? s.name + ' enabled'
                      : s.name + ' disabled — agents fall back to base behavior'
                  );
                }}
              />
              <button
                type="button"
                aria-label={'Edit ' + s.name}
                data-od-id={'btn-edit-' + s.id}
                data-testid={'btn-edit-' + s.id}
                onClick={() => setDialogState({ mode: 'edit', skill: s })}
                className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
              >
                <Icon name="edit" size={13} />
              </button>
            </li>
          );
        })}
        {lib.length === 0 && (
          <li className="py-8 text-center" data-od-id="skills-empty" data-testid="skills-empty">
            <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
              <Icon name="spark" size={20} />
            </div>
            <p className="text-[14px] font-medium text-fg">No skills installed</p>
            <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
              Install a capability package to extend your agents' abilities.
            </p>
            <button
              type="button"
              data-od-id="btn-skill-empty-add"
              data-testid="btn-skill-empty-add"
              onClick={() => setDialogState({ mode: 'install' })}
              className="mt-3 inline-flex h-8 items-center gap-1.5 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
            >
              <Icon name="plus" size={13} /> Install your first skill
            </button>
          </li>
        )}
      </ul>

      {dialogState && (
        <SkillDialog
          skill={dialogState.mode === 'edit' ? dialogState.skill : null}
          existingSkills={lib}
          onClose={() => setDialogState(null)}
          onSave={(savedSkill) => {
            if (dialogState.mode === 'edit') {
              onUpdate((t: any) => ({
                ...t,
                skillLib: (t.skillLib || []).map((x: any) =>
                  x.id === savedSkill.id ? savedSkill : x
                ),
              }));
            } else {
              onUpdate((t: any) => ({
                ...t,
                skillLib: [...(t.skillLib || []), savedSkill],
              }));
            }
          }}
          onToast={onToast}
        />
      )}
    </div>
  );
}
