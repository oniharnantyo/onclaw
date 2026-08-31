import { useState } from "react";
import { Modal } from "../components/ui/Modal";
import { inputCls, labelCls } from "../components/ui/constants";
import { cx, uid, slugify } from "../lib/helpers";

export interface SkillDialogProps {
  skill?: any | null;
  existingSkills?: any[];
  onClose: () => void;
  onSave: (skill: any) => void;
  onToast: (text: string, kind?: string) => void;
}

export function SkillDialog({
  skill,
  existingSkills = [],
  onClose,
  onSave,
  onToast,
}: SkillDialogProps) {
  const isEdit = Boolean(skill);
  const [name, setName] = useState(skill?.name || '');
  const [desc, setDesc] = useState(skill?.desc || '');

  const isValid = name.trim().length > 0;

  const handleSubmit = (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    if (!isValid) return;

    const trimmedName = name.trim();
    const trimmedDesc = desc.trim();

    if (isEdit && skill) {
      const updated = {
        ...skill,
        name: trimmedName,
        desc: trimmedDesc || skill.desc,
      };
      onSave(updated);
      onToast(trimmedName + ' updated');
      onClose();
    } else {
      let slug = slugify(trimmedName) || uid('sk');
      while (existingSkills.some((x: any) => x.id === slug)) slug += '-2';
      const newSkill = {
        id: slug,
        name: trimmedName,
        version: '0.1.0',
        desc: trimmedDesc || 'Custom workspace skill — no description yet.',
        uses: 0,
        enabled: true,
        source: 'workspace',
      };
      onSave(newSkill);
      onToast(trimmedName + " installed — assign it from any agent's capabilities");
      onClose();
    }
  };

  const submitBtnId = isEdit ? 'btn-skill-save' : 'btn-skill-add-confirm';

  return (
    <Modal
      title={isEdit ? `Edit ${skill?.name}` : 'Install custom skill'}
      onClose={onClose}
      odId="modal-skill"
      data-testid="modal-skill"
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            data-od-id="btn-skill-cancel"
            data-testid="btn-skill-cancel"
            className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="submit"
            form="skill-form"
            data-od-id={submitBtnId}
            data-testid={submitBtnId}
            disabled={!isValid}
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40 disabled:hover:bg-accent"
          >
            {isEdit ? 'Save changes' : 'Install'}
          </button>
        </>
      }
    >
      <form id="skill-form" onSubmit={handleSubmit} className="space-y-4 p-5">
        <div>
          <label className={labelCls} htmlFor="skill-name">
            Skill name
          </label>
          <input
            id="skill-name"
            className={inputCls}
            placeholder="Skill name — e.g. Changelog sweeper"
            value={name}
            aria-label="Skill name"
            data-od-id="input-skill-name"
            data-testid="input-skill-name"
            onChange={(e) => setName(e.target.value)}
            autoFocus
          />
        </div>

        <div>
          <label className={labelCls} htmlFor="skill-desc">
            Description <span className="text-muted font-normal">(optional)</span>
          </label>
          <input
            id="skill-desc"
            className={cx(inputCls, 'text-[13px]')}
            placeholder="One-line description (optional)"
            value={desc}
            aria-label="Skill description"
            data-od-id="input-skill-desc"
            data-testid="input-skill-desc"
            onChange={(e) => setDesc(e.target.value)}
          />
        </div>
      </form>
    </Modal>
  );
}
