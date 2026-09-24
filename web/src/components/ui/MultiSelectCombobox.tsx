import React, { useState, useRef, useEffect, useId } from 'react';
import { cx } from '../../lib/helpers';
import { Icon } from './Icon';

export interface MultiSelectOption {
  value: string;
  label: string;
  sublabel?: string;
  disabled?: boolean;
  [key: string]: any;
}

export interface MultiSelectComboboxProps<T extends MultiSelectOption = MultiSelectOption> {
  options: T[];
  /** The currently selected values (controlled). */
  value: string[];
  /** Receives the whole next selection, ordered by the `options` array — not
   * click order — so callers can compare arrays for equality (e.g. to decide
   * whether a Save button is dirty). */
  onChange: (next: string[]) => void;
  placeholder?: string;
  searchPlaceholder?: string;
  disabled?: boolean;
  emptyText?: string;
  className?: string;
  id?: string;
  'data-testid'?: string;
  'aria-label'?: string;
}

/**
 * Searchable multi-select combobox — the multi-select sibling of `Combobox`
 * (standalone by design: extending `Combobox` would risk its existing call
 * sites). Same interaction idioms — combobox trigger, popover with a
 * searchbox, listbox rows, arrow/Enter/Escape/Tab keys, click-outside-to-close,
 * active-option scroll-into-view — with two defining differences: toggling a
 * row KEEPS the popover open (and the query intact) so several options can be
 * ticked in one pass, and the selection shows as removable chips with an
 * "N Selected" count instead of a terse "+N" summary.
 * Presentational only: no fetching, no domain knowledge.
 *
 * Test ids: `multiselect-trigger`, `multiselect-chip-<value>`,
 * `multiselect-chip-remove-<value>`, `multiselect-count`,
 * `multiselect-search-input`, `multiselect-options-list`,
 * `multiselect-option-<value>`, plus `multiselect-popover`; passing
 * `data-testid` prefixes the trigger/popover.
 */
export function MultiSelectCombobox<T extends MultiSelectOption = MultiSelectOption>({
  options,
  value,
  onChange,
  placeholder = 'Select…',
  searchPlaceholder = 'Search…',
  disabled = false,
  emptyText = 'No results found',
  className = '',
  id,
  'data-testid': dataTestId,
  'aria-label': ariaLabel,
}: MultiSelectComboboxProps<T>) {
  const [isOpen, setIsOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [activeIndex, setActiveIndex] = useState(-1);

  const containerRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const listboxRef = useRef<HTMLDivElement>(null);

  const reactId = useId();
  const listboxId = `multiselect-listbox-${id || reactId}`;
  const searchInputId = `multiselect-search-${id || reactId}`;

  const selectedSet = new Set(value);
  const selectedCount = value.length;
  // Chips read in `options` order (so they line up with the list), then any
  // caller-held values that are not in `options` — never silently dropped,
  // matching `orderSelection`'s contract.
  const chipItems = [
    ...options
      .filter((o) => selectedSet.has(o.value))
      .map((o) => ({ value: o.value, label: o.label || o.value })),
    ...value
      .filter((v) => !options.some((o) => o.value === v))
      .map((v) => ({ value: v, label: v })),
  ];

  // Default substring filter — same rule as Combobox (label / value / sublabel).
  const defaultFilter = (opt: T, q: string) => {
    const term = q.toLowerCase().trim();
    if (!term) return true;
    const labelMatch = opt.label?.toLowerCase().includes(term);
    const valueMatch = opt.value?.toLowerCase().includes(term);
    const sublabelMatch = opt.sublabel?.toLowerCase().includes(term);
    return Boolean(labelMatch || valueMatch || sublabelMatch);
  };

  const filteredOptions = options.filter((opt) => defaultFilter(opt, query));

  const activeOptionId =
    activeIndex >= 0 && activeIndex < filteredOptions.length
      ? `${listboxId}-opt-${activeIndex}`
      : undefined;

  // Focus the search input when the popover opens (Combobox's idiom); clear the
  // query and the active row when it closes.
  useEffect(() => {
    if (isOpen) {
      setActiveIndex(0);
      const timer = setTimeout(() => {
        searchInputRef.current?.focus();
      }, 10);
      return () => clearTimeout(timer);
    } else {
      setQuery('');
      setActiveIndex(-1);
    }
  }, [isOpen]);

  // Click outside to close
  useEffect(() => {
    if (!isOpen) return;
    const handleClickOutside = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [isOpen]);

  // Scroll the active option into view
  useEffect(() => {
    if (isOpen && activeIndex >= 0 && activeIndex < filteredOptions.length && listboxRef.current) {
      const activeEl = listboxRef.current.children[activeIndex] as HTMLElement | undefined;
      if (activeEl && typeof activeEl.scrollIntoView === 'function') {
        activeEl.scrollIntoView({ block: 'nearest' });
      }
    }
  }, [activeIndex, isOpen, filteredOptions.length]);

  /** Options order first (deterministic for equality checks); values the caller
   * holds that are not in `options` are preserved rather than dropped. */
  const orderSelection = (next: Set<string>): string[] => {
    const ordered = options.filter((o) => next.has(o.value)).map((o) => o.value);
    const seen = new Set(ordered);
    const extras = value.filter((v) => next.has(v) && !seen.has(v));
    return [...ordered, ...extras];
  };

  const toggleOption = (opt: T) => {
    if (opt.disabled) return;
    const nextSet = new Set(value);
    if (nextSet.has(opt.value)) nextSet.delete(opt.value);
    else nextSet.add(opt.value);
    onChange(orderSelection(nextSet));
    // Deliberately no setIsOpen(false) — the popover stays open and the query
    // stays put so several options can be ticked in one pass.
  };

  /** Drop one value from the selection — the per-chip × affordance. */
  const removeValue = (target: string) => {
    const nextSet = new Set(value);
    nextSet.delete(target);
    onChange(orderSelection(nextSet));
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (!isOpen) {
      if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        setIsOpen(true);
      }
      return;
    }

    const toggleActive = () => {
      if (activeIndex >= 0 && activeIndex < filteredOptions.length) {
        toggleOption(filteredOptions[activeIndex]);
      }
    };

    switch (e.key) {
      case 'ArrowDown': {
        e.preventDefault();
        if (filteredOptions.length === 0) break;
        // No skipping: disabled rows are still reachable, only untogglable.
        setActiveIndex((prev) => (prev + 1 >= filteredOptions.length ? 0 : prev + 1));
        break;
      }
      case 'ArrowUp': {
        e.preventDefault();
        if (filteredOptions.length === 0) break;
        setActiveIndex((prev) => (prev - 1 < 0 ? filteredOptions.length - 1 : prev - 1));
        break;
      }
      case 'Enter': {
        e.preventDefault();
        toggleActive();
        break;
      }
      case ' ': {
        // Space toggles from the trigger/listbox, but stays a literal character
        // inside the search box (option labels contain spaces).
        if (e.target === searchInputRef.current) break;
        e.preventDefault();
        toggleActive();
        break;
      }
      case 'Escape': {
        e.preventDefault();
        setIsOpen(false);
        triggerRef.current?.focus();
        break;
      }
      case 'Tab': {
        setIsOpen(false);
        break;
      }
    }
  };

  return (
    <div ref={containerRef} className={cx('relative w-full', className)}>
      {/* Count sits on its own right-aligned row above the field, mirroring the
          reference treatment (label left / count right) without the primitive
          having to own the caller's label. */}
      {selectedCount > 0 && (
        <div className="mb-1 flex items-center justify-end">
          <span
            aria-live="polite"
            data-testid="multiselect-count"
            className="text-[11px] font-medium text-muted"
          >
            {selectedCount} Selected
          </span>
        </div>
      )}
      <button
        ref={triggerRef}
        id={id ? `${id}-trigger` : undefined}
        type="button"
        disabled={disabled}
        role="combobox"
        aria-expanded={isOpen}
        aria-haspopup="listbox"
        aria-controls={listboxId}
        aria-label={ariaLabel}
        data-testid={dataTestId ? `${dataTestId}-trigger` : 'multiselect-trigger'}
        onClick={() => {
          if (disabled) return;
          setIsOpen(!isOpen);
        }}
        onKeyDown={handleKeyDown}
        className={cx(
          'flex min-h-9 w-full flex-wrap items-center gap-1.5 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-3 py-1.5 text-left text-[14px] text-fg2 transition-colors hover:border-muted focus:border-accent focus:outline-none',
          disabled && 'cursor-not-allowed opacity-50'
        )}
      >
        {chipItems.length === 0 ? (
          <span className="flex-1 truncate text-muted">{placeholder}</span>
        ) : (
          <>
            {chipItems.map((chip) => (
              <span
                key={chip.value}
                data-testid={`multiselect-chip-${chip.value}`}
                className="inline-flex max-w-full items-center gap-1 rounded-[6px] bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] px-1.5 py-0.5 text-[11px] leading-4 text-accent"
              >
                <span className="truncate">{chip.label}</span>
                {/* A span, not a button: the trigger is already a <button> and
                    nesting buttons is invalid HTML. stopPropagation keeps the
                    click from also toggling the popover. */}
                <span
                  role="button"
                  tabIndex={-1}
                  aria-label={`Remove ${chip.label}`}
                  data-testid={`multiselect-chip-remove-${chip.value}`}
                  onClick={(e) => {
                    e.stopPropagation();
                    if (disabled) return;
                    removeValue(chip.value);
                  }}
                  onMouseDown={(e) => e.preventDefault()}
                  className={cx(
                    'flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-sm',
                    disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer hover:bg-[color-mix(in_oklab,var(--accent)_22%,transparent)]'
                  )}
                >
                  <Icon name="x" size={10} />
                </span>
              </span>
            ))}
            <span className="flex-1" />
          </>
        )}
        <Icon
          name="chevdown"
          size={14}
          className={cx(
            'shrink-0 text-muted transition-transform duration-150',
            isOpen && 'rotate-180'
          )}
        />
      </button>

      {isOpen && (
        <div
          data-testid={dataTestId ? `${dataTestId}-popover` : 'multiselect-popover'}
          className="od-pop absolute left-0 right-0 top-full z-50 mt-1 overflow-hidden rounded-md border border-line bg-surface shadow-[var(--elev-raised)]"
        >
          <div className="relative flex items-center border-b border-linesoft px-2.5">
            <Icon name="search" size={14} className="shrink-0 text-muted" />
            <input
              ref={searchInputRef}
              id={searchInputId}
              type="text"
              role="searchbox"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setActiveIndex(0);
              }}
              onKeyDown={handleKeyDown}
              aria-activedescendant={activeOptionId}
              placeholder={searchPlaceholder}
              data-testid="multiselect-search-input"
              className="flex h-9 w-full items-center bg-transparent px-2 text-[13px] text-fg placeholder:text-muted focus:outline-none"
            />
            {query && (
              <button
                type="button"
                onClick={() => {
                  setQuery('');
                  setActiveIndex(0);
                  searchInputRef.current?.focus();
                }}
                aria-label="Clear search"
                className="flex h-5 w-5 items-center justify-center rounded text-muted hover:text-fg"
              >
                <Icon name="x" size={12} />
              </button>
            )}
          </div>

          <div
            ref={listboxRef}
            id={listboxId}
            role="listbox"
            aria-multiselectable="true"
            tabIndex={-1}
            data-testid="multiselect-options-list"
            className="od-scroll max-h-56 overflow-y-auto p-1"
          >
            {filteredOptions.length === 0 ? (
              <div className="px-3 py-4 text-center text-[13px] text-muted">
                {emptyText}
              </div>
            ) : (
              filteredOptions.map((opt, idx) => {
                const isSelected = selectedSet.has(opt.value);
                const isActive = idx === activeIndex;

                return (
                  <div
                    key={opt.value}
                    role="option"
                    id={`${listboxId}-opt-${idx}`}
                    aria-selected={isSelected}
                    aria-disabled={opt.disabled}
                    data-value={opt.value}
                    data-testid={`multiselect-option-${opt.value}`}
                    // preventDefault keeps focus (and the caret) in the search
                    // input; the toggle rides on click so a real click fires
                    // exactly once.
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => toggleOption(opt)}
                    onMouseEnter={() => setActiveIndex(idx)}
                    className={cx(
                      'flex w-full items-center gap-2.5 rounded px-2.5 py-1.5 text-left text-[13px] transition-colors',
                      opt.disabled
                        ? 'cursor-not-allowed opacity-40 text-muted'
                        : 'cursor-pointer',
                      !opt.disabled && isActive && 'bg-[color-mix(in_oklab,var(--accent)_12%,transparent)] text-fg',
                      !opt.disabled && !isActive && 'text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg'
                    )}
                  >
                    <span
                      data-testid={`multiselect-check-${opt.value}`}
                      className={cx(
                        'flex h-4 w-4 shrink-0 items-center justify-center rounded border transition-colors',
                        isSelected ? 'border-accent bg-accent text-accenton' : 'border-line'
                      )}
                    >
                      {isSelected && <Icon name="check" size={12} />}
                    </span>
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span
                        className={cx(
                          'truncate',
                          isSelected ? 'font-medium text-fg' : 'text-fg2'
                        )}
                      >
                        {opt.label || opt.value}
                      </span>
                      {opt.sublabel && (
                        <span className="truncate text-[11px] text-muted">
                          {opt.sublabel}
                        </span>
                      )}
                    </span>
                  </div>
                );
              })
            )}
          </div>
        </div>
      )}
    </div>
  );
}
