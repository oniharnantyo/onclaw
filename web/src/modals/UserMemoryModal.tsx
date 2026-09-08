import { useEffect, useState } from "react";
import { Modal } from "../components/ui/Modal";
import { Icon } from "../components/ui/Icon";
import { Chip } from "../components/ui/Chip";
import { api, formatApiError, type ApiMemory } from "../lib/api";
import { useStore } from "../store";
import { cx } from "../lib/helpers";

/** The signed-in member's own USER.md editor for the active workspace.
 * Memory is unstructured markdown, so a free-form textarea is the field; the
 * size cap lives server-side and arrives in every GET payload as `max_chars`
 * (the UI holds no client-side constant for it). An over-cap save keeps the
 * modal open with the content intact and surfaces the 422 inline. */
export function UserMemoryModal({ wsSlug, onClose }: { wsSlug: string; onClose: () => void }) {
  const [loading, setLoading] = useState(true);
  const [content, setContent] = useState("");
  const [maxChars, setMaxChars] = useState<number | null>(null);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let mounted = true;
    setLoading(true);
    api.memory
      .getMine(wsSlug)
      .then((res: ApiMemory) => {
        if (!mounted) return;
        setContent(res.content ?? "");
        setMaxChars(res.max_chars);
      })
      .catch(() => {
        // Start from an empty editor; a failed save surfaces the real error.
      })
      .finally(() => {
        if (mounted) setLoading(false);
      });
    return () => {
      mounted = false;
    };
  }, [wsSlug]);

  const save = async () => {
    if (loading || saving) return;
    setSaving(true);
    setError(null);
    try {
      const res = await api.memory.updateMine(wsSlug, { content });
      setContent(res.content ?? content);
      setMaxChars(res.max_chars);
      setSaved(true);
      useStore.getState().toast("Memory saved");
    } catch (err: unknown) {
      // Inline only — the modal stays open and the draft is preserved.
      setError(formatApiError(err, "Failed to save memory"));
    } finally {
      setSaving(false);
    }
  };

  const overCap = maxChars !== null && content.length > maxChars;

  return (
    <Modal
      title="My memory"
      onClose={onClose}
      odId="modal-user-memory"
      data-testid="modal-user-memory"
      footer={
        <>
          {saved && !error && (
            <span
              data-testid="memory-saved-indicator"
              className="mr-auto flex items-center gap-1.5 text-[12px] font-medium text-fg2"
            >
              <Icon name="check" size={14} className="text-accent" />
              Saved
            </span>
          )}
          <button
            type="button"
            onClick={onClose}
            className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={save}
            disabled={loading || saving}
            data-od-id="btn-memory-save"
            data-testid="btn-memory-save"
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-50"
          >
            {saving ? "Saving…" : "Save memory"}
          </button>
        </>
      }
    >
      <div className="space-y-3 p-5">
        <div className="flex items-center justify-between gap-3">
          <p className="text-[12.5px] leading-5 text-muted">
            Notes about you that every agent in this workspace can remember.
          </p>
          <Chip mono>USER.md</Chip>
        </div>
        {loading ? (
          <div data-testid="memory-loading" className="py-8 text-center text-[13px] font-mono text-muted">
            Loading memory…
          </div>
        ) : (
          <>
            <textarea
              data-od-id="input-user-memory"
              data-testid="input-user-memory"
              aria-label="My memory"
              value={content}
              onChange={(e) => {
                setContent(e.target.value);
                setSaved(false);
              }}
              rows={12}
              placeholder="Write anything you want your agents to remember about you."
              className="w-full resize-y rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] p-3 font-mono text-[12.5px] leading-relaxed text-fg2 placeholder:text-muted focus:border-accent focus:outline-none"
            />
            {maxChars !== null && (
              <p
                data-testid="memory-char-counter"
                className={cx("text-right font-mono text-[11px]", overCap ? "text-danger" : "text-muted")}
              >
                {content.length} / {maxChars} chars
              </p>
            )}
            {error && (
              <p data-testid="memory-save-error" className="text-[12px] text-danger" role="alert">
                {error}
              </p>
            )}
          </>
        )}
      </div>
    </Modal>
  );
}
