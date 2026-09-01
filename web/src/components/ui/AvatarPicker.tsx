import React, { useState } from 'react';
import NiceAvatar, { genConfig } from 'react-nice-avatar';
import { Icon } from './Icon';
import { cx } from '../../lib/helpers';

export interface AvatarPickerProps {
  value?: Record<string, any> | null;
  onChange: (config: Record<string, any>) => void;
  size?: number;
  className?: string;
}

export function generateRandomAvatar(seed?: string): Record<string, any> {
  return genConfig(seed);
}

const SELECT_OPTIONS = {
  sex: ['man', 'woman'],
  faceColor: ['#F9C9B6', '#AC6651'],
  earSize: ['small', 'big'],
  hairColor: ['#000', '#fff', '#77311D', '#FC909F', '#D2EFF3', '#506AF4', '#F48150'],
  hairStyle: ['normal', 'thick', 'mohawk', 'womanLong', 'womanShort'],
  hatStyle: ['beanie', 'turban', 'none'],
  hatColor: ['#000', '#fff', '#77311D', '#FC909F', '#D2EFF3', '#506AF4', '#F48150'],
  eyeStyle: ['circle', 'oval', 'smile'],
  glassesStyle: ['round', 'square', 'none'],
  noseStyle: ['short', 'long', 'round'],
  mouthStyle: ['laugh', 'smile', 'peace'],
  shirtStyle: ['hoody', 'short', 'polo'],
  shirtColor: ['#9287FF', '#6BD9E9', '#FC909F', '#F4D150', '#77311D'],
  bgColor: ['#9287FF', '#6BD9E9', '#FC909F', '#F4D150', '#E0DDFF', '#D2EFF3', '#FFEDEF', '#FFEBA4', '#506AF4', '#F48150', '#74D153'],
};

export function AvatarPicker({
  value,
  onChange,
  size = 56,
  className,
}: AvatarPickerProps) {
  const [showCustom, setShowCustom] = useState(false);

  // Initialize a stable full config
  const currentConfig = React.useMemo(() => {
    if (value && Object.keys(value).length > 0) {
      // Passing partial value will generate missing fields randomly,
      // but if value is full (which it normally is after randomize/change), it stays stable.
      return genConfig(value);
    }
    return genConfig();
  }, [value]);

  const handleRandomize = () => {
    const next = genConfig();
    onChange(next);
  };

  const handleChange = (key: string, val: string) => {
    // When changing a specific property, ensure we pass the full current config
    // so we don't accidentally re-randomize everything else.
    const next = { ...currentConfig, [key]: val };
    onChange(next);
  };

  return (
    <div className={cx('flex flex-col gap-3', className)}>
      <div className="flex items-center gap-3.5">
        <div
          className="relative shrink-0 overflow-hidden rounded-lg border border-line bg-warm shadow-[var(--elev-flat)]"
          style={{ width: size, height: size }}
        >
          <NiceAvatar
            style={{ width: size, height: size }}
            shape="rounded"
            {...currentConfig}
          />
        </div>
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={handleRandomize}
              data-od-id="btn-avatar-randomize"
              data-testid="btn-avatar-randomize"
              className="inline-flex h-8 items-center gap-1.5 rounded-md border border-line bg-surface px-2.5 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg active:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)]"
            >
              <Icon name="spark" size={13} />
              Randomize
            </button>
            <button
              type="button"
              onClick={() => setShowCustom(!showCustom)}
              className={cx(
                "inline-flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-[12px] font-medium transition-colors",
                showCustom 
                  ? "border-accent bg-[color-mix(in_oklab,var(--accent)_10%,transparent)] text-accent" 
                  : "border-line bg-surface text-fg2 hover:border-accent hover:text-fg active:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)]"
              )}
            >
              <Icon name="cog" size={13} />
              Customize
            </button>
          </div>
          <span className="text-[11px] text-muted">Generate or customize a unique procedural avatar</span>
        </div>
      </div>
      
      {showCustom && (
        <div className="grid grid-cols-2 gap-x-4 gap-y-3 rounded-md border border-line bg-warm p-3">
          {Object.entries(SELECT_OPTIONS).map(([key, options]) => (
            <div key={key} className="flex flex-col gap-1">
              <label className="text-[10px] font-medium text-fg2 capitalize">
                {key.replace(/([A-Z])/g, ' $1').trim()}
              </label>
              {key.toLowerCase().includes('color') ? (
                <div className="flex flex-wrap gap-1.5">
                  {options.map((opt) => (
                    <button
                      key={opt}
                      type="button"
                      onClick={() => handleChange(key, opt)}
                      className={cx(
                        "h-[18px] w-[18px] rounded-full border border-black/10 transition-transform hover:scale-110",
                        currentConfig[key as keyof typeof currentConfig] === opt && "ring-2 ring-accent ring-offset-1 ring-offset-warm"
                      )}
                      style={{ backgroundColor: opt }}
                      title={opt}
                    />
                  ))}
                </div>
              ) : (
                <select
                  value={currentConfig[key as keyof typeof currentConfig] as string || ''}
                  onChange={(e) => handleChange(key, e.target.value)}
                  className="h-7 w-full rounded border border-line bg-surface px-2 text-[12px] text-fg outline-none focus:border-accent"
                >
                  {options.map((opt) => (
                    <option key={opt} value={opt}>
                      {opt}
                    </option>
                  ))}
                </select>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
