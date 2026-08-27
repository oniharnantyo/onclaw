// @ts-nocheck
import { useEffect, useRef } from 'react';
import { cx } from '../../lib/helpers';
import { Icon } from '../ui/Icon';

export function NavDrawer({ open, onClose, children }: { open: boolean, onClose: () => void, children: React.ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const h = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        onClose();
      }
    };
    const keyHandler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('mousedown', h);
    window.addEventListener('keydown', keyHandler);
    return () => {
      window.removeEventListener('mousedown', h);
      window.removeEventListener('keydown', keyHandler);
    };
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-40 flex md:hidden" aria-modal="true" role="dialog">
      <div className="od-fade absolute inset-0 bg-[color-mix(in_oklab,var(--fg)_32%,transparent)]" onClick={onClose} />
      <div 
        ref={ref}
        className="od-pop relative flex w-[264px] max-w-[80vw] flex-col bg-bg shadow-[var(--elev-raised)]"
      >
        <button 
          onClick={onClose} 
          className="absolute -right-10 top-3 flex h-8 w-8 items-center justify-center rounded-md text-surface hover:bg-[color-mix(in_oklab,var(--surface)_20%,transparent)]"
          aria-label="Close sidebar"
        >
          <Icon name="x" size={20} />
        </button>
        <div className="flex h-full w-full flex-col overflow-hidden">
          {children}
        </div>
      </div>
    </div>
  );
}
