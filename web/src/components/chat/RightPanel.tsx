// Right panel shell (add-right-panel 1.2, design D1): one tab per opened
// artifact, content dispatched through the panel-source registry — the shell
// knows tab mechanics, never specific sources. Chrome reuses the exact
// pattern ContextPanel established in ChatRoute: a fixed-width column docked
// beside the transcript on ≥xl viewports, a full-height overlay sheet with a
// dismissal backdrop below it. Open/close lives in the store's panel slice —
// the panel never opens itself; every tab arrives through a user click.

import { useEffect, useState, useCallback, useRef } from 'react';
import { useStore } from '../../store';
import { cx } from '../../lib/helpers';
import { Icon } from '../ui/Icon';
import { renderPanelSource } from '../../lib/panel/registry';
// Module-load registration of the built-in sources (members now; file and
// browser sources join in later slices — their modules exist already).
import './panel/sources';

export const DEFAULT_PANEL_WIDTH = 400;
export const MIN_PANEL_WIDTH = 280;
export const MAX_PANEL_WIDTH = 900;
const PANEL_WIDTH_KEY = 'od-panel-width';

export function RightPanel({ sourceContext }: { sourceContext?: any }) {
  const open = useStore((s: any) => s.panel.open);
  const tabs = useStore((s: any) => s.panel.tabs);
  const activeId = useStore((s: any) => s.panel.activeId);

  const [width, setWidth] = useState<number>(() => {
    try {
      const saved = Number(localStorage.getItem(PANEL_WIDTH_KEY));
      if (saved >= MIN_PANEL_WIDTH && saved <= MAX_PANEL_WIDTH) return saved;
    } catch {}
    return DEFAULT_PANEL_WIDTH;
  });

  const [isDragging, setIsDragging] = useState(false);
  const widthRef = useRef(width);
  widthRef.current = width;

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    setIsDragging(true);
    const startX = e.clientX;
    const startWidth = widthRef.current;

    const handleMouseMove = (moveEvent: MouseEvent) => {
      const deltaX = startX - moveEvent.clientX;
      const maxW = Math.min(window.innerWidth * 0.75, MAX_PANEL_WIDTH);
      const nextWidth = Math.round(Math.max(MIN_PANEL_WIDTH, Math.min(startWidth + deltaX, maxW)));
      setWidth(nextWidth);
      try {
        localStorage.setItem(PANEL_WIDTH_KEY, String(nextWidth));
      } catch {}
    };

    const handleMouseUp = () => {
      setIsDragging(false);
      window.removeEventListener('mousemove', handleMouseMove);
      window.removeEventListener('mouseup', handleMouseUp);
      document.body.style.removeProperty('user-select');
      document.body.style.removeProperty('cursor');
    };

    document.body.style.userSelect = 'none';
    document.body.style.cursor = 'col-resize';
    window.addEventListener('mousemove', handleMouseMove);
    window.addEventListener('mouseup', handleMouseUp);
  }, []);

  if (!open) return null;

  const active = tabs.find((t) => t.id === activeId) || tabs[tabs.length - 1] || null;

  const body = (
    <>
      {/* Tab strip: title, per-tab close, active state. The panel's own close
          control rides on the right edge of the strip. */}
      <div className="flex h-14 shrink-0 items-center gap-1 border-b border-linesoft px-2" role="tablist" aria-label="Panel tabs">
        <div className="od-scroll flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
          {tabs.map((tab) => (
            <div key={tab.id}
              className={cx('group flex min-w-0 shrink-0 items-center rounded-md transition-colors',
                tab.id === active?.id ? 'bg-[color-mix(in_oklab,var(--accent)_12%,transparent)]' : 'hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)]')}>
              <button type="button" role="tab" aria-selected={tab.id === active?.id}
                onClick={() => useStore.getState().focusPanelTab(tab.id)}
                data-od-id={'panel-tab-' + tab.kind}
                className={cx('min-w-0 max-w-[11rem] truncate px-2 py-1.5 text-left text-[12px] font-medium transition-colors',
                  tab.id === active?.id ? 'text-fg' : 'text-muted hover:text-fg2')}>
                {tab.title}
              </button>
              <button type="button" onClick={(e) => { e.stopPropagation(); useStore.getState().closePanelTab(tab.id); }}
                aria-label={'Close ' + tab.title + ' tab'} title={'Close ' + tab.title}
                data-od-id={'panel-tab-close-' + tab.kind}
                className="mr-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-[5px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg">
                <Icon name="x" size={11}/>
              </button>
            </div>
          ))}
        </div>
        <button type="button" onClick={() => useStore.getState().setPanelOpen(false)}
          data-od-id="btn-panel-close" aria-label="Close panel" title="Close panel"
          className="ml-1 flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg">
          <Icon name="x" size={14}/>
        </button>
      </div>
      {/* Content: dispatched through the source registry. An unregistered kind
          (a plugin that failed to load) degrades to a terse fallback — never
          raw payload JSON. No tabs → terse empty state, never a blank pane. */}
      <div className="od-scroll min-h-0 flex-1 overflow-y-auto">
        {active ? (
          renderPanelSource(active.kind, { tab: active, ctx: sourceContext }) ?? (
            <div className="p-4" data-od-id="panel-source-missing">
              <p className="text-[12px] text-muted">This source is not available.</p>
            </div>
          )
        ) : (
          <div className="flex h-full flex-col items-center justify-center gap-1 px-6 text-center" data-od-id="panel-empty">
            <p className="text-[13px] font-medium text-fg">Nothing open</p>
            <p className="max-w-[26ch] text-[12px] leading-5 text-muted">Open an artifact from the transcript to pin it here.</p>
          </div>
        )}
      </div>
    </>
  );

  return (
    <>
      {/* Docked ≥xl: a real flex column beside the transcript with resizable width. */}
      <div className="hidden xl:flex" data-od-id="right-panel-docked">
        <aside data-od-id="right-panel" aria-label="Context panel"
          style={{ width: `${width}px` }}
          className="relative flex w-[400px] shrink-0 flex-col border-l border-linesoft bg-bg">
          {/* Resize drag handle */}
          <div
            data-od-id="panel-resize-handle"
            role="separator"
            aria-orientation="vertical"
            title="Drag to resize panel (Double-click to reset)"
            onMouseDown={handleMouseDown}
            onDoubleClick={() => {
              setWidth(DEFAULT_PANEL_WIDTH);
              try { localStorage.removeItem(PANEL_WIDTH_KEY); } catch {}
            }}
            className="group absolute -left-1.5 top-0 bottom-0 z-20 flex w-3 cursor-col-resize items-center justify-center select-none"
          >
            <div className={cx(
              "h-full w-[2px] transition-colors",
              isDragging
                ? "bg-[var(--accent)]"
                : "bg-transparent group-hover:bg-[color-mix(in_oklab,var(--accent)_60%,transparent)]"
            )} />
          </div>
          {body}
        </aside>
      </div>
      {/* Overlay sheet <xl: the same chrome full-height, sliding over the
          transcript with the established od-fade/od-pop motion. */}
      <div className="fixed inset-0 z-40 flex xl:hidden" aria-modal="true" role="dialog" data-od-id="right-panel-overlay">
        <div
          className="od-fade absolute inset-0 bg-[color-mix(in_oklab,var(--fg)_32%,transparent)]"
          onClick={() => useStore.getState().setPanelOpen(false)}
        />
        <div className="od-pop relative ml-auto flex h-full w-[400px] max-w-[85vw] flex-col bg-bg shadow-[var(--elev-raised)]">
          {body}
        </div>
      </div>
    </>
  );
}
