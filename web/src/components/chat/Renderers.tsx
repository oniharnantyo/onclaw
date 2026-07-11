import { useState, type ReactNode } from 'react';
import {
  CheckSquare,
  Sparkle,
  Plug,
  ImageSquare,
  FileText,
  CaretDown,
  CaretRight,
  Copy,
  Check,
  ArrowsCounterClockwise,
  XCircle,
} from '@phosphor-icons/react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeHighlight from 'rehype-highlight';
import { useThread } from '../ChatProvider';
import type { ContentBlock } from '../../types/chat';

/* ── Diff Renderer ─────────────────────────────────────────── */

export function DiffCode({ children }: { children?: ReactNode }) {
  const text = String(children || '');
  const lines = text.split('\n');

  return (
    <div className="diff-pre">
      {lines.map((line, i) => {
        let color: string | undefined;
        if (line.startsWith('+') && !line.startsWith('+++')) color = 'var(--success)';
        else if (line.startsWith('-') && !line.startsWith('---')) color = 'var(--error)';
        else if (line.startsWith('@@')) color = 'var(--info)';
        return (
          <div key={i} style={{ color, whiteSpace: 'pre' }}>
            {line}
          </div>
        );
      })}
    </div>
  );
}

/* ── Code Block with Copy and Language Label ─────────────────────────────── */

interface CodeBlockProps {
  language?: string;
  code: string;
}

function CodeBlockWithCopy({ language, code }: CodeBlockProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // ignore clipboard error
    }
  };

  return (
    <div className="code-block-wrapper">
      {language && (
        <div className="code-block-language">{language}</div>
      )}
      <button
        className="code-block-copy"
        onClick={handleCopy}
        type="button"
        aria-label="Copy code"
        title={copied ? 'Copied!' : 'Copy code'}
      >
        {copied ? (
          <Check size={16} weight="fill" />
        ) : (
          <Copy size={16} weight="regular" />
        )}
      </button>
    </div>
  );
}

/* ── Markdown Renderer ─────────────────────────────────────── */

export function MarkdownBlock({ text }: { text: string }) {
  if (!text) return null;

  return (
    <div className="block-markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={{
          code({ className, children, ...props }: { className?: string; children?: ReactNode; node?: any }) {
            const match = /language-(\w+)/.exec(className || '');
            const language = match ? match[1] : undefined;
            const codeText = String(children || '');
            const isDiff = language === 'diff';

            // Check if this is inline code (no className) or a code block
            const isInline = !className;

            if (isInline) {
              return (
                <code className={className} {...props}>
                  {children}
                </code>
              );
            }

            if (isDiff) {
              return (
                <div className="code-block-container">
                  <CodeBlockWithCopy language="diff" code={codeText} />
                  <DiffCode>{children}</DiffCode>
                </div>
              );
            }

            return (
              <div className="code-block-container">
                <CodeBlockWithCopy language={language} code={codeText} />
                <pre
                  style={{
                    fontFamily: 'var(--mono)',
                    fontSize: '12.5px',
                    overflowX: 'auto',
                  }}
                >
                  <code className={className} {...props}>
                    {children}
                  </code>
                </pre>
              </div>
            );
          },
          pre({ children }: { children?: ReactNode }) {
            // If this is a code block container, return children directly
            // Otherwise wrap in pre (for inline code cases)
            const isCodeBlock = (children as any)?.type?.displayName === 'CodeBlockWithCopy' ||
                               (children as any)?.props?.className?.includes('code-block-container');

            if (isCodeBlock) {
              return <>{children}</>;
            }

            return (
              <pre
                style={{
                  fontFamily: 'var(--mono)',
                  fontSize: '12.5px',
                  overflowX: 'auto',
                }}
              >
                {children}
              </pre>
            );
          },
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}

/* ── Reasoning (Collapsible CoT) ───────────────────────────── */

export function ReasoningBlock({ block }: { block: ContentBlock }) {
  const [open, setOpen] = useState(false);
  const text = block.reasoning?.text || '';
  if (!text) return null;

  return (
    <div className="block-reasoning" role="region" aria-label="Reasoning">
      <button
        className="block-reasoning-header"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        type="button"
      >
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}>
          {open ? <CaretDown size={12} weight="fill" /> : <CaretRight size={12} weight="fill" />}
          <Sparkle size={13} weight="fill" />
          <strong>Reasoning</strong>
        </span>
      </button>
      {open && (
        <div className="block-reasoning-content">
          <code
            style={{
              fontFamily: 'var(--mono)',
              fontSize: '12px',
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
              display: 'block',
              background: 'none',
              border: 'none',
              padding: 0,
              color: 'var(--text-muted)',
            }}
          >
            {text}
          </code>
        </div>
      )}
    </div>
  );
}

/* ── Skill Activated ───────────────────────────────────────── */

export function SkillActivatedBlock({ block }: { block: ContentBlock }) {
  const [open, setOpen] = useState(false);
  const tc = block.function_tool_call;
  const tr = block.function_tool_result;

  let skillName = 'skill';
  if (tc?.arguments) {
    try {
      const args = JSON.parse(tc.arguments);
      skillName = args.name || args.skill || args.tool || 'skill';
    } catch {
      // use default
    }
  }

  const resultContent = tr?.content?.[0]?.text?.text || '';

  return (
    <div className="block-skill-activated" role="region" aria-label={`Skill: ${skillName}`}>
      <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
        <Sparkle size={13} weight="fill" style={{ color: 'var(--text-muted)' }} />
        <strong style={{ color: 'var(--text-bright)', fontSize: '12.5px' }}>{skillName}</strong>
      </div>
      {resultContent && (
        <button
          className="block-toggle-btn"
          onClick={() => setOpen(!open)}
          aria-expanded={open}
          type="button"
          style={{ marginTop: '4px', fontSize: '11px' }}
        >
          {open ? 'Hide' : 'Show'} result
        </button>
      )}
      {open && resultContent && (
        <div
          style={{
            marginTop: '6px',
            fontSize: '12px',
            color: 'var(--text-muted)',
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-word',
          }}
        >
          {resultContent}
        </div>
      )}
    </div>
  );
}

/* ── Generic Tool Call ─────────────────────────────────────── */

export function ToolCallBlock({ block }: { block: ContentBlock }) {
  const { isStreaming, messages } = useThread();
  const [open, setOpen] = useState(false);
  const tc = block.function_tool_call;
  if (!tc) return null;
  const allBlocks = messages.flatMap((m) => m.content_blocks || []);

  const tcId = (tc as any).call_id || tc.id;
  const resultBlock = allBlocks.find(
    (b) => {
      if (b.type !== 'function_tool_result' || !b.function_tool_result) return false;
      const tr = b.function_tool_result;
      const trId = tr.call_id || (tr as any).id;
      if (tcId && trId) return tcId === trId;
      return tr.name === tc.name;
    }
  );

  const hasResult = !!resultBlock;
  const isCancelled = !hasResult && !isStreaming;
  const content = resultBlock?.function_tool_result?.content?.[0]?.text?.text || '';

  let argsDisplay = tc.arguments || '';
  try {
    argsDisplay = JSON.stringify(JSON.parse(argsDisplay), null, 2);
  } catch {
    // keep raw
  }

  let resultDisplay = content;
  try {
    resultDisplay = JSON.stringify(JSON.parse(resultDisplay), null, 2);
  } catch {
    // keep raw
  }

  // Determine state icon and labels
  let IconComponent = <ArrowsCounterClockwise size={14} className="tool-icon-spinning" />;
  let labelText = 'Used tool:';
  let isMuted = false;

  if (hasResult) {
    IconComponent = <Check size={14} weight="bold" className="tool-icon-success" />;
    labelText = 'Used tool:';
  } else if (isCancelled) {
    IconComponent = <XCircle size={14} weight="bold" className="tool-icon-error" style={{ color: 'var(--text-muted)' }} />;
    labelText = 'Cancelled tool:';
    isMuted = true;
  }

  return (
    <div 
      className="tool-fallback-root" 
      role="region" 
      aria-label={`Tool: ${tc.name}`}
      style={isMuted ? { opacity: 0.6 } : undefined}
    >
      <button
        className="tool-fallback-trigger"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        type="button"
      >
        <span className="tool-fallback-header-left">
          {IconComponent}
          <span className="tool-use-text" style={isMuted ? { color: 'var(--text-muted)' } : undefined}>
            {labelText} <strong className="tool-name" style={isMuted ? { textDecoration: 'line-through' } : undefined}>{tc.name}</strong>
          </span>
        </span>
        <span className="tool-fallback-header-right">
          {open ? <CaretDown size={12} weight="bold" /> : <CaretRight size={12} weight="bold" />}
        </span>
      </button>
      
      {open && (
        <div className="tool-fallback-content">
          <pre className="tool-code-block">
            <code>{argsDisplay}</code>
          </pre>
          
          {hasResult && (
            <div style={{ marginTop: '10px' }}>
              <div 
                style={{ 
                  fontSize: '13px', 
                  color: 'var(--text-muted)', 
                  marginBottom: '6px', 
                  marginTop: '12px',
                  fontWeight: 500
                }}
              >
                Result:
              </div>
              <pre className="tool-code-block">
                <code>{resultDisplay}</code>
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export function ToolResultBlock({ block }: { block: ContentBlock }) {
  const { messages } = useThread();
  const [open, setOpen] = useState(false);

  const tr = block.function_tool_result;
  if (!tr) return null;

  const allBlocks = messages.flatMap((m) => m.content_blocks || []);

  const trId = tr.call_id || (tr as any).id;
  const hasCall = allBlocks.some(
    (b) => {
      if (b.type !== 'function_tool_call' || !b.function_tool_call) return false;
      const tc = b.function_tool_call;
      const tcId = (tc as any).call_id || tc.id;
      if (tcId && trId) return tcId === trId;
      return tc.name === tr.name;
    }
  );

  // If the corresponding call block exists in the message, let that block handle rendering.
  if (hasCall) {
    return null;
  }
  const content = tr.content?.[0]?.text?.text || '';
  const isError = content.toLowerCase().includes('error') || content.toLowerCase().includes('failed');

  let resultDisplay = content;
  try {
    resultDisplay = JSON.stringify(JSON.parse(resultDisplay), null, 2);
  } catch {
    // keep raw
  }

  return (
    <div className="tool-fallback-root" role="region" aria-label={`Tool result: ${tr.name}`}>
      <button
        className="tool-fallback-trigger"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        type="button"
      >
        <span className="tool-fallback-header-left">
          <CheckSquare size={13} weight="fill" className={isError ? "tool-icon-error" : "tool-icon-success"} />
          <span className="tool-use-text">
            Tool result: <strong className="tool-name">{tr.name}</strong>
          </span>
        </span>
        <span className="tool-fallback-header-right">
          {open ? <CaretDown size={12} weight="bold" /> : <CaretRight size={12} weight="bold" />}
        </span>
      </button>
      
      {open && (
        <div className="tool-fallback-content">
          <pre className="tool-code-block">
            <code>{resultDisplay}</code>
          </pre>
        </div>
      )}
    </div>
  );
}

/* ── MCP Called ────────────────────────────────────────────── */

export function MCPCalledBlock({ block }: { block: ContentBlock }) {
  const isCall = !!block.mcp_tool_call;
  const isResult = !!block.mcp_tool_result;
  if (!isCall && !isResult) return null;

  return (
    <div
      className="block-mcp-called"
      role="region"
      aria-label="MCP Tool"
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
        <Plug size={12} weight="fill" aria-hidden style={{ color: 'var(--info)' }} />
        <strong style={{ fontSize: '12.5px', color: 'var(--info)' }}>
          MCP {isCall ? 'Tool Call' : 'Tool Result'}
        </strong>
      </div>
    </div>
  );
}

/* ── Image Block ───────────────────────────────────────────── */

export function ImageBlock({ block }: { block: ContentBlock }) {
  const imgUrl =
    (block.assistant_gen_image?.url as string) ||
    (block.assistant_gen_image?.data_url as string) ||
    (block.assistant_gen_image?.data as string) ||
    (block.user_input_image?.url as string) ||
    (block.user_input_image?.data_url as string) ||
    (block.user_input_image?.data as string) ||
    ((block.user_input_image as any)?.base64_data ? `data:${(block.user_input_image as any).mime_type};base64,${(block.user_input_image as any).base64_data}` : '');
  
  const isAssistant = !!block.assistant_gen_image;

  if (!block.assistant_gen_image && !block.user_input_image) return null;

  return (
    <div className="block-image" role="region" aria-label={isAssistant ? 'Generated image' : 'Uploaded image'}>
      {imgUrl ? (
        <img
          src={imgUrl}
          alt={isAssistant ? 'Generated image' : 'Uploaded image'}
          style={{ maxWidth: '100%', borderRadius: 'var(--radius-sm)', display: 'block' }}
          loading="lazy"
        />
      ) : (
        <div style={{ display: 'flex', alignItems: 'center', gap: '6px', padding: '8px 0' }}>
          <ImageSquare size={16} weight="duotone" style={{ color: 'var(--accent)' }} />
          <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
            {isAssistant ? 'Generated Image' : 'Image'}
          </span>
        </div>
      )}
    </div>
  );
}

/* ── File Block ────────────────────────────────────────────── */

export function FileBlock({ block }: { block: ContentBlock }) {
  if (!block.user_input_file) return null;

  const fileName =
    (block.user_input_file as Record<string, unknown>).name ||
    (block.user_input_file as Record<string, unknown>).filename ||
    'Attached file';

  return (
    <div className="block-file" role="region" aria-label={`File: ${fileName}`}>
      <FileText size={14} weight="duotone" style={{ color: 'var(--text-muted)', flexShrink: 0 }} />
      <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>{String(fileName)}</span>
    </div>
  );
}

/* ── Unknown Fallback ──────────────────────────────────────── */

export function UnknownBlock({ block }: { block: ContentBlock }) {
  return (
    <div className="block-unknown">
      [{block.type} block]
    </div>
  );
}

/* ── Tool Name Dispatcher ──────────────────────────────────── */

export function pickToolRenderer(name: string) {
  if (name === 'skill') return SkillActivatedBlock;
  return null;
}
