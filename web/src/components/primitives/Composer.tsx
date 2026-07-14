import {
  createContext,
  useContext,
  useState,
  useRef,
  useEffect,
  useCallback,
  type ReactNode
} from 'react';
import { Plus } from '@phosphor-icons/react';
import { useComposer } from '../ChatProvider';
import type { ContentBlock } from '../../types/chat';

export interface Attachment {
  id: string;
  name: string;
  type: string;
  base64: string;
  url?: string;
}

interface ComposerContextValue {
  prompt: string;
  setPrompt: React.Dispatch<React.SetStateAction<string>>;
  attachments: Attachment[];
  setAttachments: React.Dispatch<React.SetStateAction<Attachment[]>>;
  textareaRef: React.RefObject<HTMLTextAreaElement | null>;
  showSkills: boolean;
  setShowSkills: React.Dispatch<React.SetStateAction<boolean>>;
  skillSearch: string;
  setSkillSearch: React.Dispatch<React.SetStateAction<string>>;
  submit: () => void;
  isStreaming: boolean;
  contextOverLimit: boolean;
  errorMsg: string | null;
  setErrorMsg: React.Dispatch<React.SetStateAction<string | null>>;
}

const ComposerContext = createContext<ComposerContextValue | null>(null);

export function useComposerContext() {
  const ctx = useContext(ComposerContext);
  if (!ctx) throw new Error('useComposerContext must be used within Composer.Root');
  return ctx;
}

export interface ComposerRootProps {
  children: ReactNode;
  className?: string;
}

export function ComposerRoot({ children, className = '' }: ComposerRootProps) {
  const { runChat, isStreaming, contextOverLimit } = useComposer();
  const [prompt, setPrompt] = useState('');
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [showSkills, setShowSkills] = useState(false);
  const [skillSearch, setSkillSearch] = useState('');
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const submit = useCallback(() => {
    if (isStreaming) return;
    if (contextOverLimit) {
      setErrorMsg('Context window exceeded — reduce the conversation or raise max context tokens before sending.');
      return;
    }
    if (!prompt.trim()) {
      setErrorMsg('Prompt is required');
      return;
    }
    setErrorMsg(null);

    // Map attachments to ContentBlock wire DTO
    const blockAttachments: ContentBlock[] = attachments.map((att) => {
      if (att.type.startsWith('image/')) {
        return {
          type: 'user_input_image',
          user_input_image: {
            base64_data: att.base64,
            mime_type: att.type,
          },
        };
      } else {
        return {
          type: 'user_input_file',
          user_input_file: {
            name: att.name,
            base64_data: att.base64,
            mime_type: att.type,
          },
        };
      }
    });

    runChat(prompt, blockAttachments);
    setPrompt('');
    // Revoke attachment URLs to avoid memory leaks
    attachments.forEach((att) => {
      if (att.url) URL.revokeObjectURL(att.url);
    });
    setAttachments([]);

    // Reset textarea height
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
    }
  }, [prompt, attachments, runChat, isStreaming, contextOverLimit]);

  const value: ComposerContextValue = {
    prompt,
    setPrompt,
    attachments,
    setAttachments,
    textareaRef,
    showSkills,
    setShowSkills,
    skillSearch,
    setSkillSearch,
    submit,
    isStreaming,
    contextOverLimit,
    errorMsg,
    setErrorMsg,
  };

  return (
    <ComposerContext.Provider value={value}>
      <div className={`composer-root ${className}`}>{children}</div>
    </ComposerContext.Provider>
  );
}

export interface ComposerInputProps {
  className?: string;
  placeholder?: string;
}

export function ComposerInput({ className = '', placeholder = 'Ask agent to perform a task…' }: ComposerInputProps) {
  const {
    prompt,
    setPrompt,
    textareaRef,
    setShowSkills,
    setSkillSearch,
    submit,
    isStreaming,
    contextOverLimit,
    setAttachments,
    setErrorMsg,
  } = useComposerContext();

  const adjustHeight = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
  }, [textareaRef]);

  useEffect(() => {
    adjustHeight();
  }, [prompt, adjustHeight]);

  const handleInputChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const val = e.target.value;
    setPrompt(val);
    if (val.trim()) {
      setErrorMsg(null);
    }

    if (val.endsWith('/')) {
      setShowSkills(true);
      setSkillSearch('');
    } else if (val.includes('/')) {
      const parts = val.split('/');
      const lastPart = parts[parts.length - 1];
      if (!lastPart.includes(' ')) {
        setShowSkills(true);
        setSkillSearch(lastPart);
      } else {
        setShowSkills(false);
      }
    } else {
      setShowSkills(false);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      submit();
    }
  };

  const handlePaste = (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const items = e.clipboardData?.items;
    if (!items) return;

    for (let i = 0; i < items.length; i++) {
      const item = items[i];
      if (item.kind === 'file') {
        const file = item.getAsFile();
        if (file) {
          e.preventDefault();
          const reader = new FileReader();
          reader.onload = (event) => {
            const result = event.target?.result as string;
            const base64 = result.split(',')[1];
            const newAtt: Attachment = {
              id: Math.random().toString(36).substring(7),
              name: file.name,
              type: file.type,
              base64,
              url: file.type.startsWith('image/') ? URL.createObjectURL(file) : undefined,
            };
            setAttachments((prev) => [...prev, newAtt]);
          };
          reader.readAsDataURL(file);
        }
      }
    }
    // If no files/images are pasted, let default text paste proceed without error.
  };

  return (
    <textarea
      ref={textareaRef}
      className={`composer-input ${className}`}
      placeholder={isStreaming ? 'Agent is responding…' : contextOverLimit ? 'Context window exceeded — cannot send' : placeholder}
      value={prompt}
      onChange={handleInputChange}
      onKeyDown={handleKeyDown}
      onPaste={handlePaste}
      disabled={isStreaming || contextOverLimit}
      rows={1}
      aria-label="Message input"
    />
  );
}

export interface ComposerSendProps {
  children: ReactNode;
  className?: string;
  style?: React.CSSProperties;
}

export function ComposerSend({ children, className = '', style }: ComposerSendProps) {
  const { submit, isStreaming, prompt, contextOverLimit } = useComposerContext();
  return (
    <button
      onClick={submit}
      className={`composer-send ${className}`}
      type="button"
      style={style}
      disabled={isStreaming || contextOverLimit || !prompt.trim()}
      aria-label="Send message"
    >
      {children}
    </button>
  );
}

export interface ComposerCancelProps {
  children: ReactNode;
  className?: string;
  onClick?: () => void;
}

export function ComposerCancel({ children, className = '', onClick }: ComposerCancelProps) {
  const { isStreaming } = useComposerContext();
  return (
    <button
      onClick={onClick}
      className={`composer-cancel ${className}`}
      type="button"
      disabled={!isStreaming}
      aria-label="Cancel streaming"
    >
      {children}
    </button>
  );
}

export interface ComposerTriggerPopoverProps {
  children: (skills: { name: string; description: string }[], insertSkill: (name: string) => void) => ReactNode;
  className?: string;
}

export function ComposerTriggerPopover({ children, className = '' }: ComposerTriggerPopoverProps) {
  const { skills } = useComposer();
  const { showSkills, setShowSkills, skillSearch, setPrompt, textareaRef } = useComposerContext();
  const popoverRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!showSkills) return;
    const handler = (e: MouseEvent) => {
      if (popoverRef.current && !popoverRef.current.contains(e.target as Node)) {
        setShowSkills(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [showSkills, setShowSkills]);

  const insertSkill = useCallback((skillName: string) => {
    setPrompt((prev) => {
      // Find the last index of '/'
      const lastSlashIdx = prev.lastIndexOf('/');
      if (lastSlashIdx !== -1) {
        return prev.substring(0, lastSlashIdx) + `/${skillName} `;
      }
      return `/${skillName} `;
    });
    setShowSkills(false);
    textareaRef.current?.focus();
  }, [setPrompt, setShowSkills, textareaRef]);

  if (!showSkills) return null;

  const filteredSkills = skills.filter((s) =>
    s.name.toLowerCase().includes(skillSearch.toLowerCase())
  );

  if (filteredSkills.length === 0) return null;

  return (
    <div ref={popoverRef} className={`composer-popover ${className}`} role="listbox" aria-label="Available skills">
      {children(filteredSkills, insertSkill)}
    </div>
  );
}

export interface ComposerPastePreviewProps {
  children: (att: Attachment, remove: () => void) => ReactNode;
  className?: string;
}

export function ComposerPastePreview({ children, className = '' }: ComposerPastePreviewProps) {
  const { attachments, setAttachments } = useComposerContext();

  if (attachments.length === 0) return null;

  const removeAttachment = (id: string) => {
    setAttachments((prev) => {
      const item = prev.find((att) => att.id === id);
      if (item?.url) URL.revokeObjectURL(item.url);
      return prev.filter((att) => att.id !== id);
    });
  };

  return (
    <div className={`composer-paste-preview ${className}`} role="none">
      {attachments.map((att) => children(att, () => removeAttachment(att.id)))}
    </div>
  );
}

export interface ComposerAttachProps {
  className?: string;
  accept?: string;
}

export function ComposerAttach({ className = '', accept = 'image/*,.pdf,.doc,.docx,.txt,.md,.json' }: ComposerAttachProps) {
  const { setAttachments, setErrorMsg, contextOverLimit } = useComposerContext();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleClick = () => {
    if (contextOverLimit) return;
    fileInputRef.current?.click();
  };

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;

    const file = files[0];
    const reader = new FileReader();
    reader.onload = (event) => {
      const result = event.target?.result as string;
      const base64 = result.split(',')[1];
      const newAtt: Attachment = {
        id: Math.random().toString(36).substring(7),
        name: file.name,
        type: file.type,
        base64,
        url: file.type.startsWith('image/') ? URL.createObjectURL(file) : undefined,
      };
      setAttachments((prev) => [...prev, newAtt]);
      setErrorMsg(null);
    };
    reader.readAsDataURL(file);

    // Reset input so same file can be selected again
    e.target.value = '';
  };

  return (
    <>
      <button
        onClick={handleClick}
        className={`composer-attach ${className}`}
        type="button"
        aria-label="Attach file"
        title="Attach file or image"
      >
        <Plus size={20} weight="regular" />
      </button>
      <input
        ref={fileInputRef}
        type="file"
        accept={accept}
        onChange={handleFileChange}
        style={{ display: 'none' }}
        aria-hidden="true"
        tabIndex={-1}
      />
    </>
  );
}

export const Composer = Object.assign(ComposerRoot, {
  Root: ComposerRoot,
  Input: ComposerInput,
  Send: ComposerSend,
  Cancel: ComposerCancel,
  TriggerPopover: ComposerTriggerPopover,
  PastePreview: ComposerPastePreview,
  Attach: ComposerAttach,
});
