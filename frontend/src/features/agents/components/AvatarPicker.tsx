/**
 * AvatarPicker (onclaw-v2 parity): a react-nice-avatar preview with
 * Randomize and Customize controls. Randomize rolls a fresh full config;
 * Customize expands a two-column grid of trait selects and color swatches.
 * The value is a full avatar config — the same shape the agent row stores
 * verbatim (store.Agent.Avatar) and every surface renders back, so what the
 * preview shows is exactly what the agent keeps.
 */
import { useMemo, useState } from "react";
import type { ReactElement } from "react";
import NiceAvatar, { genConfig } from "react-nice-avatar";
import type { AvatarFullConfig, NiceAvatarProps } from "react-nice-avatar";

import { Icon } from "../../../components/ui/Icon";
import { cn } from "../../../lib/cn";

export interface AvatarPickerProps {
  value?: AvatarFullConfig | null;
  onChange: (config: AvatarFullConfig) => void;
  size?: number;
  className?: string;
}

/** A fresh, fully random avatar config (the picker's Randomize roll). */
export function generateRandomAvatar(): AvatarFullConfig {
  return genConfig();
}

/** The trait keys the customize grid exposes (all string-valued). */
type AvatarOptionKey =
  | "sex"
  | "faceColor"
  | "earSize"
  | "hairColor"
  | "hairStyle"
  | "hatStyle"
  | "hatColor"
  | "eyeStyle"
  | "glassesStyle"
  | "noseStyle"
  | "mouthStyle"
  | "shirtStyle"
  | "shirtColor"
  | "bgColor";

/** The customize grid's traits, in onclaw-v2's order; color keys render as swatches. */
const SELECT_OPTIONS: readonly (readonly [
  AvatarOptionKey,
  readonly string[],
])[] = [
  ["sex", ["man", "woman"]],
  ["faceColor", ["#F9C9B6", "#AC6651"]],
  ["earSize", ["small", "big"]],
  [
    "hairColor",
    ["#000", "#fff", "#77311D", "#FC909F", "#D2EFF3", "#506AF4", "#F48150"],
  ],
  ["hairStyle", ["normal", "thick", "mohawk", "womanLong", "womanShort"]],
  ["hatStyle", ["beanie", "turban", "none"]],
  [
    "hatColor",
    ["#000", "#fff", "#77311D", "#FC909F", "#D2EFF3", "#506AF4", "#F48150"],
  ],
  ["eyeStyle", ["circle", "oval", "smile"]],
  ["glassesStyle", ["round", "square", "none"]],
  ["noseStyle", ["short", "long", "round"]],
  ["mouthStyle", ["laugh", "smile", "peace"]],
  ["shirtStyle", ["hoody", "short", "polo"]],
  ["shirtColor", ["#9287FF", "#6BD9E9", "#FC909F", "#F4D150", "#77311D"]],
  [
    "bgColor",
    [
      "#9287FF",
      "#6BD9E9",
      "#FC909F",
      "#F4D150",
      "#E0DDFF",
      "#D2EFF3",
      "#FFEDEF",
      "#FFEBA4",
      "#506AF4",
      "#F48150",
      "#74D153",
    ],
  ],
];

export function AvatarPicker({
  value,
  onChange,
  size = 56,
  className,
}: AvatarPickerProps): ReactElement {
  const [showCustom, setShowCustom] = useState(false);

  // A stable full config: genConfig fills any missing trait randomly, so a
  // stored partial config still renders one fixed face per value object.
  const currentConfig = useMemo((): ReturnType<typeof genConfig> => {
    return genConfig(value ?? undefined);
  }, [value]);

  const handleRandomize = (): void => {
    onChange(genConfig());
  };

  const handleChange = (key: AvatarOptionKey, val: string): void => {
    // The full current config rides along so one trait change never
    // re-randomizes the rest.
    onChange({ ...currentConfig, [key]: val });
  };

  // The silhouette knob is react-nice-avatar's own and set by member access
  // (the kit Avatar's trick): spread configs can't flatten the rounded look.
  const niceProps: NiceAvatarProps = {
    style: { width: size, height: size },
    ...currentConfig,
  };

  niceProps.shape = "rounded";

  return (
    <div className={cn("flex flex-col gap-3", className)}>
      <div className="flex items-center gap-3.5">
        <div
          className="relative shrink-0 overflow-hidden rounded-lg border border-line bg-warm shadow-[var(--elev-flat)]"
          style={{ width: size, height: size }}
        >
          <NiceAvatar {...niceProps} />
        </div>
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={handleRandomize}
              data-testid="btn-avatar-randomize"
              className="inline-flex h-8 items-center gap-1.5 rounded-md border border-line bg-surface px-2.5 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg active:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)]"
            >
              <Icon name="spark" size={13} />
              Randomize
            </button>
            <button
              type="button"
              onClick={() => setShowCustom(!showCustom)}
              data-testid="btn-avatar-customize"
              className={cn(
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
          <span className="text-[11px] text-muted">
            Generate or customize a unique procedural avatar
          </span>
        </div>
      </div>

      {showCustom && (
        <div className="grid grid-cols-2 gap-x-4 gap-y-3 rounded-md border border-line bg-warm p-3">
          {SELECT_OPTIONS.map(
            ([key, options]: readonly [
              AvatarOptionKey,
              readonly string[],
            ]): ReactElement => (
              <div key={key} className="flex flex-col gap-1">
                <label className="text-[10px] font-medium capitalize text-fg2">
                  {key.replace(/([A-Z])/g, " $1").trim()}
                </label>
                {key.toLowerCase().includes("color") ? (
                  <div className="flex flex-wrap gap-1.5">
                    {options.map((opt: string): ReactElement => (
                      <button
                        key={opt}
                        type="button"
                        onClick={() => handleChange(key, opt)}
                        className={cn(
                          "h-[18px] w-[18px] rounded-full border border-black/10 transition-transform hover:scale-110",
                          currentConfig[key] === opt &&
                            "ring-2 ring-accent ring-offset-1 ring-offset-warm"
                        )}
                        style={{ backgroundColor: opt }}
                        title={opt}
                      />
                    ))}
                  </div>
                ) : (
                  <select
                    value={currentConfig[key]}
                    onChange={(e: React.ChangeEvent<HTMLSelectElement>) =>
                      handleChange(key, e.target.value)
                    }
                    className="h-7 w-full rounded border border-line bg-surface px-2 text-[12px] text-fg outline-none focus:border-accent"
                  >
                    {options.map((opt: string): ReactElement => (
                      <option key={opt} value={opt}>
                        {opt}
                      </option>
                    ))}
                  </select>
                )}
              </div>
            )
          )}
        </div>
      )}
    </div>
  );
}
