import { useState, useRef, useEffect, useImperativeHandle } from "react";
import { memberHandle, uid } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { COMMANDS } from "../../lib/constants";
import { useStore } from "../../store";
import {
  uploadAttachment, precheckAttachment, defaultPasteName, formatSize, mimeLabel,
  ATTACHMENTS_PER_MESSAGE, PICKER_ACCEPT,
} from "../../lib/attachments";
import type { AttachmentChip, UploadError, DocumentMentionChip } from "../../lib/attachments";

import { SlashMenu } from "./SlashMenu";
import { MentionMenu } from "./MentionMenu";
import { SkillMenu } from "./SkillMenu";
import type { SkillMenuGroup } from "./SkillMenu";
import { ContextRing } from "./ContextRing";

// One tray chip, per galleries B–E: uploading (progress + cancel), ready
// (thumbnail for images / icon + "PDF · 4.8 MB" for docs, remove), rejected
// (server reason inline), failed ("Upload failed" + Retry on the held File).
// `warning` (fix-image-attachment-lane D5) is a soft, non-blocking capability
// notice rendered under the chip name — never a gate on sending.
function AttachmentChipCard({ chip, warning, onRemove, onRetry }: {
  chip: AttachmentChip; warning?: string; onRemove: () => void; onRetry: () => void;
}) {
  const thumbnail = chip.state === 'ready' && (chip.mime || '').startsWith('image/') && chip.url;
  return (
    <div data-testid="attachment-chip" data-chip-state={chip.state}
      className="flex w-[230px] max-w-full items-center gap-2 rounded-[12px] border border-line bg-surface px-2 py-1.5">
      {thumbnail ? (
        <img src={chip.url} alt={chip.name}
          className="h-9 w-9 shrink-0 rounded-[8px] border border-line object-cover"/>
      ) : (
        <span aria-hidden
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-[8px] bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] text-muted">
          <Icon name="file" size={15}/>
        </span>
      )}
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[12px] leading-4 text-fg" title={chip.name}>{chip.name}</span>
        {chip.state === 'uploading' && (
          <span className="mt-1 flex items-center gap-1.5">
            <span aria-hidden className="h-1 flex-1 overflow-hidden rounded-full bg-line">
              <span className="block h-full rounded-full bg-accent transition-[width] duration-150"
                style={{ width: chip.progress + '%' }}/>
            </span>
            <span className="text-[10px] tabular-nums text-muted">{chip.progress}%</span>
          </span>
        )}
        {chip.state === 'ready' && (
          <span className="block truncate text-[11px] leading-4 text-muted">
            {mimeLabel(chip.mime, chip.name)} · {formatSize(chip.size)}
          </span>
        )}
        {chip.state === 'rejected' && (
          <span className="block truncate text-[11px] leading-4 text-danger" title={chip.reason}>{chip.reason}</span>
        )}
        {chip.state === 'failed' && (
          <span className="mt-0.5 flex items-center gap-1.5">
            <span className="text-[11px] leading-4 text-danger">Upload failed</span>
            <button type="button" onClick={onRetry} data-testid="chip-retry"
              className="rounded-full border border-line px-1.5 py-0.5 text-[10px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg">
              Retry
            </button>
          </span>
        )}
        {warning && (
          <span data-testid="chip-modality-warning" title={warning}
            className="mt-0.5 block truncate text-[11px] leading-4 text-[color-mix(in_oklab,var(--warn),black_38%)]">
            {warning}
          </span>
        )}
      </span>
      <button type="button" onClick={onRemove} data-testid="chip-remove"
        aria-label={(chip.state === 'uploading' ? 'Cancel upload ' : 'Remove ') + chip.name}
        title={chip.state === 'uploading' ? 'Cancel upload' : 'Remove'}
        className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg">
        <Icon name="x" size={11}/>
      </button>
    </div>
  );
}

// One pending document-mention chip (add-reference-documents 10.4): document
// icon + name + mount path + remove. Identity only — the pill the transcript
// shows is the markdown link in the sent text, never a second chip card.
function DocumentChipCard({ chip, onRemove }: { chip: DocumentMentionChip; onRemove: () => void }) {
  return (
    <div data-testid="document-chip" data-document-id={chip.documentId}
      className="flex w-[230px] max-w-full items-center gap-2 rounded-[12px] border border-line bg-surface px-2 py-1.5">
      <span aria-hidden
        className="flex h-9 w-9 shrink-0 items-center justify-center rounded-[8px] bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] text-muted">
        <Icon name="file" size={15}/>
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[12px] leading-4 text-fg" title={chip.name}>{chip.name}</span>
        <span className="block truncate font-mono text-[11px] leading-4 text-muted">{chip.path}</span>
      </span>
      <button type="button" onClick={onRemove} data-testid="doc-chip-remove"
        aria-label={'Remove ' + chip.name} title="Remove"
        className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg">
        <Icon name="x" size={11}/>
      </button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Draft restore (adopt-assistant-ui-elements 9.3): unsent composer text
// persists per thread in localStorage under `onclaw.draft.<chatId>` and is
// restored when the thread's Composer mounts again (ChatRoute remounts the
// Composer per chat, so the mount-time state initializer is the restore
// point). Sending clears the saved draft. Drafts are local to this client —
// never synced, never in any API surface.
// ---------------------------------------------------------------------------

const DRAFT_PREFIX = 'onclaw.draft.';

const loadDraft = (chatId?: string): string => {
  if (!chatId) return '';
  try {
    return localStorage.getItem(DRAFT_PREFIX + chatId) || '';
  } catch {
    // storage unavailable (private mode) — the composer just starts empty
    return '';
  }
};

const saveDraft = (chatId: string | undefined, value: string) => {
  if (!chatId) return;
  try {
    if (value) localStorage.setItem(DRAFT_PREFIX + chatId, value);
    else localStorage.removeItem(DRAFT_PREFIX + chatId);
  } catch {
    // storage unavailable — the draft just won't persist
  }
};

export function Composer({ agent, running, onSend, onCancel, mentionOptions, allowCommands, skillGroups,
  allowAttachments, workspaceSlug, chatId, registerDocMention, ref }: any) {
  const [text, setText] = useState(() => loadDraft(chatId));
  // Every user-driven text change persists the draft; an empty value clears
  // the stored one (send, or deleting everything typed).
  const changeText = (v: string) => {
    setText(v);
    saveDraft(chatId, v);
  };
  const [idx, setIdx] = useState(0);
  const [menuDismissed, setMenuDismissed] = useState(false);
  const ta = useRef(null);
  // Attachment chips are COMPOSER-LOCAL state (design D11): progress ticks
  // never touch the global store. Controllers track in-flight uploads so the
  // ✕ cancel and conversation-switch teardown can abort them.
  const [chips, setChips] = useState<AttachmentChip[]>([]);
  const controllersRef = useRef(new Map<string, AbortController>());
  const fileInputRef = useRef(null);
  const toast = useStore((s: any) => s.toast);

  // Slash commands are an agent-chat affordance (chat-compact-command D8):
  // channel/team composers never open the menu — a typed /command passes
  // through as ordinary text.
  const slashQ = allowCommands && text.startsWith('/') ? text.split(' ')[0] : '';
  const slashOpen = !!slashQ && !text.includes(' ') && COMMANDS.some((c) => c.cmd.startsWith(slashQ.toLowerCase()));
  const slashList = slashOpen ? COMMANDS.filter((c) => c.cmd.startsWith(slashQ.toLowerCase())) : [];
  const mentionMatch = mentionOptions ? (text.match(/@([A-Za-z]*)$/) || null) : null;
  const mentionList = mentionMatch
    ? mentionOptions.filter((m: any) => memberHandle(m).startsWith(mentionMatch[1].toLowerCase()))
    : [];
  // $token triggers the skill invocation menu (groups: System / Workspace /
  // This agent). An unmatched $name sends as ordinary text.
  const skillMatch = skillGroups && skillGroups.length ? (text.match(/(^|\s)\$([A-Za-z0-9-]*)$/) || null) : null;
  const skillQuery = skillMatch ? skillMatch[2] : '';
  const skillList = skillMatch
    ? (skillGroups as SkillMenuGroup[])
        .flatMap((g) => g.skills)
        .filter((s) => s.name.toLowerCase().startsWith(skillQuery.toLowerCase()))
    : [];
  const menu = menuDismissed
    ? null
    : slashOpen
      ? 'slash'
      : mentionMatch && mentionList.length
        ? 'mention'
        : skillMatch && skillList.length
          ? 'skill'
          : null;
  const menuLength = menu === 'slash' ? slashList.length : menu === 'mention' ? mentionList.length : skillList.length;
  const [lastMenu, setLastMenu] = useState(menu);
  if (menu !== lastMenu) { setLastMenu(menu); setIdx(0); }

  const grow = () => { const el = ta.current; if (el) { el.style.height = 'auto'; el.style.height = Math.min(192, el.scrollHeight) + 'px'; } };

  // --- Attachment tray (add-chat-attachments D11/D13/D14) -------------------

  const patchChip = (key: string, patch: Partial<AttachmentChip>) => {
    setChips((cs) => cs.map((c) => (c.key === key ? { ...c, ...patch } : c)));
  };

  const startUpload = (chip: AttachmentChip) => {
    const file = chip.file;
    if (!file || !workspaceSlug) return;
    const controller = new AbortController();
    controllersRef.current.set(chip.key, controller);
    uploadAttachment(workspaceSlug, file, {
      signal: controller.signal,
      onProgress: (p) => patchChip(chip.key, { progress: p }),
    })
      .then((up) => patchChip(chip.key, { state: 'ready', progress: 100, id: up.id, url: up.url, mime: up.mime || chip.mime, size: up.size ?? chip.size }))
      .catch((err: UploadError | DOMException) => {
        // User cancel (✕ removes the chip itself) or conversation-switch
        // teardown — the chip is already gone or dying; do nothing.
        if (controller.signal.aborted || (err as DOMException).name === 'AbortError') return;
        const status = (err as UploadError)?.status ?? 0;
        if (status >= 400 && status < 500) {
          // The server's verdict (magic-byte sniff, caps) — rejected chip with
          // its reason inline (spec: "rejected … with the server's reason").
          patchChip(chip.key, { state: 'rejected', progress: 0, reason: (err as Error).message });
        } else {
          patchChip(chip.key, { state: 'failed', progress: 0 });
        }
      })
      .finally(() => controllersRef.current.delete(chip.key));
  };

  const addFiles = (files: File[]) => {
    if (!files || !files.length) return;
    const additions: AttachmentChip[] = [];
    let overCap = false;
    for (const file of files) {
      const active = chips.filter((c) => c.state !== 'rejected').length + additions.length;
      if (active >= ATTACHMENTS_PER_MESSAGE) { overCap = true; break; }
      const reason = precheckAttachment(file);
      additions.push(reason
        ? { key: uid('att'), name: file.name || 'file', mime: file.type, size: file.size, state: 'rejected', progress: 0, reason }
        : { key: uid('att'), file, name: file.name || defaultPasteName(file), mime: file.type, size: file.size, state: 'uploading', progress: 0 });
    }
    if (overCap) toast('Up to 4 attachments per message');
    if (additions.length) {
      setChips([...chips, ...additions]);
      for (const chip of additions) if (chip.state === 'uploading') startUpload(chip);
    }
  };

  // ChatView hands drag-dropped files here through the ref — chip state stays
  // inside the Composer (D11).
  useImperativeHandle(ref, () => ({ addFiles }), [chips, workspaceSlug]);

  const removeChip = (chip: AttachmentChip) => {
    // Abort first: an in-flight upload's catch sees the aborted signal and
    // stays quiet; settled uploads just fall through to the filter.
    controllersRef.current.get(chip.key)?.abort();
    setChips((cs) => cs.filter((c) => c.key !== chip.key));
  };

  const retryChip = (chip: AttachmentChip) => {
    if (!chip.file) return;
    patchChip(chip.key, { state: 'uploading', progress: 0 });
    startUpload({ ...chip, state: 'uploading', progress: 0 });
  };

  // Capability hint (spec web-app/chat): warn ONLY when the agent's model is
  // affirmatively unable to take the input kind — supported, unknown, or an
  // absent field all stay silent. Purely informational: sending is never gated.
  const chipWarning = (chip: AttachmentChip): string | undefined => {
    if (chip.state === 'rejected') return undefined;
    const modalities = (agent as any)?.input_modalities;
    if (!modalities) return undefined;
    const mime = (chip.mime || '').toLowerCase();
    if (mime.startsWith('image/') && modalities.image === 'unsupported') {
      return "this model can't see images — will attach as reference only";
    }
    if (mime === 'application/pdf' && modalities.pdf === 'unsupported') {
      return "this model can't read PDFs — will attach as reference only";
    }
    return undefined;
  };

  // --- Pending document mention chips (add-reference-documents 10.4) -------

  // Pending document mention chips are COMPOSER-LOCAL state, like the
  // attachment chips — never global stores (React 19 nested-update lessons).
  // The floating popover that used to mint them is gone (rework-document-chat-
  // surfaces D2): chips are added through the mention bridge ChatRoute
  // registers from the panel's Documents listing (task 2.2), and the composer
  // only carries/removes them onto the send.
  const [docChips, setDocChips] = useState<DocumentMentionChip[]>([]);

  const removeDocChip = (chip: DocumentMentionChip) => {
    setDocChips((cs) => cs.filter((c) => c.documentId !== chip.documentId));
  };

  // --- Mention insertion (rework-document-chat-surfaces 2.2, design D3) -----
  //
  // The panel's Documents listing inserts mentions through the ChatRoute
  // bridge: the composer registers its insertion callback (text token +
  // docChips state) with the mounting screen via the ref-style registration
  // prop — the chips stay composer-local, never lifted into a store (same
  // rule as the attachment chips). Reconstructed popover semantics: the
  // markdown link token in the text IS the visible pill the transcript
  // renders (runtime contract: `[📄 name](references/name)`), and the chip
  // rides the send as document identity only.
  const insertDocMention = (doc: { id: string; name: string }) => {
    const path = 'references/' + doc.name;
    const token = '[📄 ' + doc.name + '](' + path + ')';
    changeText((text ? text.replace(/\s+$/, '') + ' ' : '') + token + ' ');
    setDocChips((cs) => cs.some((c) => c.documentId === doc.id)
      ? cs
      : [...cs, { kind: 'document', documentId: doc.id, name: doc.name, path }]);
    requestAnimationFrame(() => { if (ta.current) ta.current.focus(); });
  };
  // Registration happens once per bridge identity; a stable trampoline
  // forwards to the freshest insertion closure through a ref (refreshed each
  // commit), so a callback held across keystrokes never inserts stale text.
  const insertDocRef = useRef<((doc: { id: string; name: string }) => void) | null>(null);
  useEffect(() => {
    insertDocRef.current = insertDocMention;
  });
  useEffect(() => {
    registerDocMention?.((doc: { id: string; name: string }) => insertDocRef.current?.(doc));
    return () => registerDocMention?.(null);
  }, [registerDocMention]);

  // The documents affordance moved to the chat header (rework-document-chat-
  // surfaces D2, 2026-09-28 user pivot): ChatView's header toggle opens the
  // panel's Documents listing; the composer keeps only the mention bridge.

  // Conversation switch unmounts the Composer (ChatRoute keys ChatView on the
  // chat id): the tray dies with it and in-flight uploads abort (spec:
  // "switching conversations clears it and aborts in-flight uploads").
  useEffect(() => {
    const controllers = controllersRef.current;
    return () => {
      for (const controller of controllers.values()) controller.abort();
      controllers.clear();
    };
  }, []);

  const onPaste = (e: any) => {
    if (!allowAttachments) return;
    const items = e.clipboardData?.items;
    if (!items) return;
    const files: File[] = [];
    for (let i = 0; i < items.length; i++) {
      const item = items[i];
      if (item.kind === 'file') {
        const f = item.getAsFile();
        if (f) files.push(f);
      }
    }
    if (!files.length) return; // text paste untouched
    e.preventDefault();
    addFiles(files.map((f) => new File([f], defaultPasteName(f), { type: f.type })));
  };

  // --- Send gate (design D12, hard) ------------------------------------------
  // Blocked while any chip is uploading; enabled when text is non-empty OR at
  // least one chip is ready — attachment-only sends are valid, and so are
  // mention-only sends (the document identity is the payload). Rejected and
  // failed chips are absent from the gate.
  const uploadingAny = chips.some((c) => c.state === 'uploading');
  const readyChips = chips.filter((c) => c.state === 'ready');

  const submit = (raw?: any) => {
    // Deliberately no `if (running) return` here: while an agent run is active
    // the runtime's send gate queues the message (design D9 / spec Message
    // queue) — blocking submit would make the queue unreachable.
    if (uploadingAny) return;
    const val = (raw !== undefined ? raw : text).trim();
    if (!val && readyChips.length === 0 && docChips.length === 0) return;
    onSend(val, [...readyChips, ...docChips]);
    changeText('');
    setChips([]);
    setDocChips([]);
    requestAnimationFrame(grow);
  };

  const pickMention = (m) => {
    const handle = m.kind === 'agent' ? m.name : m.name.split(' ')[0];
    changeText(text.replace(/@[A-Za-z]*$/, '@' + handle + ' '));
    requestAnimationFrame(() => { if (ta.current) ta.current.focus(); });
  };

  const pickSkill = (name: string) => {
    changeText(text.replace(/(^|\s)\$[A-Za-z0-9-]*$/, (m0, pre) => (pre || '') + '$' + name + ' '));
    setIdx(0);
    requestAnimationFrame(() => { if (ta.current) ta.current.focus(); });
  };

  const onKeyDown = (e) => {
    if (menu && e.key === 'ArrowDown') { e.preventDefault(); setIdx((i) => Math.min(menuLength - 1, i + 1)); return; }
    if (menu && e.key === 'ArrowUp') { e.preventDefault(); setIdx((i) => Math.max(0, i - 1)); return; }
    if (e.key === 'Escape' && menu) { e.preventDefault(); setMenuDismissed(true); return; }
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      if (menu === 'slash' && slashList[idx]) { changeText(slashList[idx].cmd + ' '); setIdx(0); return; }
      if (menu === 'mention' && mentionList[idx]) { pickMention(mentionList[idx]); setIdx(0); return; }
      if (menu === 'skill' && skillList[idx]) { pickSkill(skillList[idx].name); return; }
      submit(undefined as any);
    }
  };

  return (
    <div className="relative" data-od-id="composer">
      {menu === 'slash' && <SlashMenu q={slashQ} idx={idx} onPick={(cmd) => { changeText(cmd + ' '); setIdx(0); if (ta.current) ta.current.focus(); }}/>}
      {menu === 'mention' && <MentionMenu options={mentionList} idx={idx} onPick={pickMention}/>}
      {menu === 'skill' && <SkillMenu groups={skillGroups} query={skillQuery} idx={idx} onPick={pickSkill}/>}
      <div onClick={() => { if (ta.current) ta.current.focus(); }}
        className="cursor-text rounded-[24px] border border-line bg-surface p-2 transition-colors focus-within:border-accent">
        <textarea ref={ta} rows={1} value={text} autoFocus aria-label={'Message input'}
          onChange={(e) => { changeText(e.target.value); setMenuDismissed(false); grow(); }}
          onPaste={onPaste}
          onKeyDown={onKeyDown}
          placeholder={mentionOptions ? 'Message the channel — @ to mention' : agent ? 'Message ' + agent.name + '…' : 'Send a message…'}
          enterKeyHint="send"
          className="max-h-48 min-h-10 w-full resize-none bg-transparent px-2.5 py-1.5 text-[15px] leading-6 text-fg outline-none placeholder:text-muted"/>
        {((allowAttachments && chips.length > 0) || docChips.length > 0) && (
          <div className="flex flex-wrap items-stretch gap-2 px-1 pb-1.5 pt-0.5"
            data-testid={chips.length > 0 ? 'attachment-tray' : 'document-chip-tray'}>
            {chips.map((chip) => (
              <AttachmentChipCard key={chip.key} chip={chip} warning={chipWarning(chip)}
                onRemove={() => removeChip(chip)} onRetry={() => retryChip(chip)}/>
            ))}
            {docChips.map((chip) => (
              <DocumentChipCard key={chip.documentId} chip={chip} onRemove={() => removeDocChip(chip)}/>
            ))}
          </div>
        )}
        <div className="flex items-center justify-between pt-0.5">
          {/* LEFT control rail: attach (agent chats) + the context ring
              (adopt-assistant-ui-elements D1 — agent chats only, so the
              allowCommands gate doubles as the surface check). */}
          <div className="flex items-center gap-1">
            {allowAttachments && (
              <>
                <input ref={fileInputRef} type="file" multiple hidden data-testid="composer-file-input" accept={PICKER_ACCEPT}
                  onChange={(e) => { addFiles(Array.from(e.target.files || [])); e.target.value = ''; }}/>
                <button type="button" data-testid="btn-attach"
                  onClick={(e) => { e.stopPropagation(); fileInputRef.current?.click(); }}
                  aria-label="Attach a file" title="Attach a file"
                  className="flex h-7 w-7 items-center justify-center rounded-full text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg">
                  <Icon name="clip" size={14}/>
                </button>
              </>
            )}
            <ContextRing agent={agent} enabled={Boolean(allowCommands)}/>
          </div>
          {!running ? (
            <button type="button" onClick={(e) => { e.stopPropagation(); submit(undefined as any); }} disabled={uploadingAny || (!text.trim() && readyChips.length === 0 && docChips.length === 0)} data-od-id="btn-send" data-testid="btn-send"
              aria-label="Send message" title="Send message"
              className="flex h-8 w-8 items-center justify-center rounded-full bg-accent text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-35 disabled:hover:bg-accent">
              <Icon name="up" size={15} sw={2.4}/>
            </button>
          ) : (
            <button type="button" onClick={(e) => { e.stopPropagation(); onCancel(null as any); }} data-od-id="btn-cancel"
              aria-label="Stop generating" title="Stop generating"
              className="flex h-8 w-8 items-center justify-center rounded-full bg-accent text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]">
              <Icon name="stop" size={12}/>
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
