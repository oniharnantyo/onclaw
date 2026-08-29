import React, { useState, useRef, useEffect, useId } from 'react';
import { cx } from '../../lib/helpers';
import { Icon } from './Icon';

export interface ComboboxOption {
  value: string;
  label: string;
  sublabel?: string;
  disabled?: boolean;
  [key: string]: any;
}

export interface ComboboxProps<T extends ComboboxOption = ComboboxOption> {
  options: T[];
  value?: string;
  onChange: (value: string, option?: T) => void;
  placeholder?: string;
  searchPlaceholder?: string;
  disabled?: boolean;
  error?: boolean | string;
  renderOption?: (option: T, state: { selected: boolean; active: boolean }) => React.ReactNode;
  renderValue?: (option?: T) => React.ReactNode;
  filterOption?: (option: T, query: string) => boolean;
  emptyText?: string;
  id?: string;
  name?: string;
  className?: string;
  buttonClassName?: string;
  popoverClassName?: string;
  'data-od-id'?: string;
  'data-testid'?: string;
  'aria-label'?: string;
  'aria-labelledby'?: string;
}

export function Combobox<T extends ComboboxOption = ComboboxOption>({
  options,
  value,
  onChange,
  placeholder = 'Select…',
  searchPlaceholder = 'Search…',
  disabled = false,
  error = false,
  renderOption,
  renderValue,
  filterOption,
  emptyText = 'No results found',
  id,
  name,
  className = '',
  buttonClassName = '',
  popoverClassName = '',
  'data-od-id': dataOdId,
  'data-testid': dataTestId,
  'aria-label': ariaLabel,
  'aria-labelledby': ariaLabelledBy,
}: ComboboxProps<T>) {
  const [isOpen, setIsOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [activeIndex, setActiveIndex] = useState(-1);

  const containerRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const listboxRef = useRef<HTMLDivElement>(null);

  const reactId = useId();
  const listboxId = `combobox-listbox-${id || reactId}`;
  const searchInputId = `combobox-search-${id || reactId}`;

  const selectedOption = options.find((o) => o.value === value);

  // Default substring filter
  const defaultFilter = (opt: T, q: string) => {
    const term = q.toLowerCase().trim();
    if (!term) return true;
    const labelMatch = opt.label?.toLowerCase().includes(term);
    const valueMatch = opt.value?.toLowerCase().includes(term);
    const sublabelMatch = opt.sublabel?.toLowerCase().includes(term);
    return Boolean(labelMatch || valueMatch || sublabelMatch);
  };

  const filteredOptions = options.filter((opt) =>
    filterOption ? filterOption(opt, query) : defaultFilter(opt, query)
  );

  // Focus search input when popover opens
  useEffect(() => {
    if (isOpen) {
      const idx = filteredOptions.findIndex((o) => o.value === value);
      setActiveIndex(idx >= 0 ? idx : 0);
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

  // Scroll active option into view
  useEffect(() => {
    if (isOpen && activeIndex >= 0 && listboxRef.current) {
      const activeEl = listboxRef.current.children[activeIndex] as HTMLElement | undefined;
      if (activeEl && typeof activeEl.scrollIntoView === 'function') {
        activeEl.scrollIntoView({ block: 'nearest' });
      }
    }
  }, [activeIndex, isOpen]);

  const selectOption = (opt: T) => {
    if (opt.disabled) return;
    onChange(opt.value, opt);
    setIsOpen(false);
    triggerRef.current?.focus();
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (!isOpen) {
      if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        setIsOpen(true);
      }
      return;
    }

    switch (e.key) {
      case 'ArrowDown': {
        e.preventDefault();
        if (filteredOptions.length === 0) break;
        let next = activeIndex + 1;
        if (next >= filteredOptions.length) next = 0;
        setActiveIndex(next);
        break;
      }
      case 'ArrowUp': {
        e.preventDefault();
        if (filteredOptions.length === 0) break;
        let prev = activeIndex - 1;
        if (prev < 0) prev = filteredOptions.length - 1;
        setActiveIndex(prev);
        break;
      }
      case 'Enter': {
        e.preventDefault();
        if (activeIndex >= 0 && activeIndex < filteredOptions.length) {
          selectOption(filteredOptions[activeIndex]);
        }
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
      {/* Hidden native select for form serialization & change test compatibility */}
      {id && (
        <select
          id={id}
          name={name}
          value={value}
          disabled={disabled}
          tabIndex={-1}
          aria-hidden="true"
          data-testid={dataTestId}
          data-od-id={dataOdId}
          onChange={(e) => {
            const val = e.target.value;
            const opt = options.find((o) => o.value === val);
            onChange(val, opt);
          }}
          className="sr-only"
        >
          {options.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label || o.value}
            </option>
          ))}
        </select>
      )}

      <button
        ref={triggerRef}
        id={id ? `${id}-trigger` : undefined}
        name={name ? `${name}-trigger` : undefined}
        type="button"
        disabled={disabled}
        role="combobox"
        aria-expanded={isOpen}
        aria-haspopup="listbox"
        aria-controls={listboxId}
        aria-label={ariaLabel}
        aria-labelledby={ariaLabelledBy}
        data-od-id={dataOdId ? `${dataOdId}-trigger` : dataOdId}
        data-testid={id ? (dataTestId ? `${dataTestId}-trigger` : undefined) : dataTestId}
        onClick={() => setIsOpen(!isOpen)}
        onKeyDown={handleKeyDown}
        className={cx(
          'flex h-9 w-full items-center justify-between gap-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-3 text-left text-[14px] text-fg2 transition-colors hover:border-muted focus:border-accent focus:outline-none',
          error && 'border-danger focus:border-danger',
          disabled && 'cursor-not-allowed opacity-50',
          buttonClassName
        )}
      >
        <div className="flex-1 truncate">
          {renderValue ? (
            renderValue(selectedOption)
          ) : selectedOption ? (
            <span className="truncate text-fg">
              {selectedOption.label || selectedOption.value}
            </span>
          ) : (
            <span className="text-muted">{placeholder}</span>
          )}
        </div>
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
          data-od-id={dataOdId ? `${dataOdId}-popover` : 'combobox-popover'}
          data-testid={dataTestId ? `${dataTestId}-popover` : 'combobox-popover'}
          className={cx(
            'od-pop absolute left-0 right-0 top-full mt-1 z-50 overflow-hidden rounded-md border border-line bg-surface shadow-[var(--elev-raised)]',
            popoverClassName
          )}
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
              placeholder={searchPlaceholder}
              data-testid="combobox-search-input"
              className="flex h-9 w-full items-center bg-transparent px-2 text-[13px] text-fg placeholder:text-muted focus:outline-none"
            />
            {query && (
              <button
                type="button"
                onClick={() => {
                  setQuery('');
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
            tabIndex={-1}
            data-testid="combobox-options-list"
            className="od-scroll max-h-56 overflow-y-auto p-1"
          >
            {filteredOptions.length === 0 ? (
              <div className="px-3 py-4 text-center text-[13px] text-muted">
                {emptyText}
              </div>
            ) : (
              filteredOptions.map((opt, idx) => {
                const isSelected = opt.value === value;
                const isActive = idx === activeIndex;

                return (
                  <div
                    key={opt.value}
                    role="option"
                    id={`${listboxId}-opt-${idx}`}
                    aria-selected={isSelected}
                    aria-disabled={opt.disabled}
                    data-value={opt.value}
                    data-testid={`combobox-option-${opt.value}`}
                    onMouseDown={(e) => {
                      e.preventDefault();
                      selectOption(opt);
                    }}
                    onMouseEnter={() => setActiveIndex(idx)}
                    className={cx(
                      'flex w-full items-center justify-between rounded px-2.5 py-1.5 text-left text-[13px] transition-colors',
                      opt.disabled
                        ? 'cursor-not-allowed opacity-40 text-muted'
                        : 'cursor-pointer',
                      !opt.disabled && isActive && 'bg-[color-mix(in_oklab,var(--accent)_12%,transparent)] text-fg',
                      !opt.disabled && !isActive && 'text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg'
                    )}
                  >
                    {renderOption ? (
                      renderOption(opt, { selected: isSelected, active: isActive })
                    ) : (
                      <>
                        <div className="flex flex-col truncate">
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
                        </div>
                        {isSelected && (
                          <Icon
                            name="check"
                            size={14}
                            className="ml-2 shrink-0 text-accent"
                          />
                        )}
                      </>
                    )}
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
