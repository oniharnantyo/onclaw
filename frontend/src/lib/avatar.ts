/**
 * Deterministic generated-avatar props (agent-management spec): the same
 * agent id always yields the same react-nice-avatar appearance everywhere —
 * cards, session rail, sidebar — without persisting avatar state.
 *
 * Shared (not agent-feature-local) because the chat surface renders agent
 * identities too; the picker and the rail must agree on one appearance.
 */
import type {
  EarSize,
  EyeBrowStyle,
  EyeStyle,
  GlassesStyle,
  HairStyle,
  HatStyle,
  MouthStyle,
  NoseStyle,
  Sex,
  ShirtStyle,
} from "react-nice-avatar";

/** Small FNV-1a hash: stable string → 32-bit unsigned int. */
function hashString(input: string): number {
  let h = 0x811c9dc5;

  for (let i = 0; i < input.length; i++) {
    h ^= input.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }

  return h >>> 0;
}

function pick<T>(hash: number, salt: number, options: readonly T[]): T {
  return options[(hash >>> (salt * 4)) % options.length];
}

const SKIN_COLORS = [
  "#f2d3b1",
  "#eab98d",
  "#d2996f",
  "#a86e4c",
  "#8d5a3a",
] as const;

const HAIR_COLORS = [
  "#2b2b33",
  "#5a3d2b",
  "#8a6d3b",
  "#c99244",
  "#4a4a55",
] as const;

const FACE_COLORS = ["#f9c9b6", "#f8d8c0", "#e8c39e"] as const;

const BG_COLORS = [
  "#a5b4fc",
  "#6ee7b7",
  "#fca5a5",
  "#fcd34d",
  "#93c5fd",
  "#d8b4fe",
] as const;

/**
 * The avatar look derived from an agent id: the SAME id always yields the
 * same face everywhere. Every key react-nice-avatar's `genConfig` would
 * otherwise random-fill per mount is pinned here from the hash — a partial
 * config makes the kit Avatar render a different face on every surface
 * (cards, rail, sidebar, wizard preview would all disagree).
 */
export interface AvatarProps {
  seed: string;
  sex: Sex;
  skinColor: string;
  hairColor: string;
  faceColor: string;
  bgColor: string;
  earSize: EarSize;
  hairStyle: HairStyle;
  eyeStyle: EyeStyle;
  glassesStyle: GlassesStyle;
  noseStyle: NoseStyle;
  mouthStyle: MouthStyle;
  shirtStyle: ShirtStyle;
  shirtColor: string;
  hatStyle: HatStyle;
  hatColor: string;
  eyeBrowStyle: EyeBrowStyle;
  hairColorRandom: boolean;
  isGradient: boolean;
}

const EAR_SIZES = ["small", "big"] as const;

const EYE_STYLES = ["circle", "oval", "smile"] as const;

const GLASSES_STYLES = ["none", "none", "round", "square"] as const;

const NOSE_STYLES = ["short", "long", "round"] as const;

const MOUTH_STYLES = ["laugh", "smile", "peace"] as const;

const SHIRT_STYLES = ["hoody", "short", "polo"] as const;

const SHIRT_COLORS = [
  "#64748b",
  "#475569",
  "#7c6f9f",
  "#52796f",
  "#8a6d3b",
  "#5b6ee1",
] as const;

const HAT_STYLES = ["none", "none", "beanie", "turban"] as const;

const HAT_COLORS = ["#475569", "#7c6f9f", "#8a6d3b"] as const;

const HAIR_STYLES_MAN = ["normal", "thick", "mohawk"] as const;

const HAIR_STYLES_WOMAN = ["normal", "womanLong", "womanShort"] as const;

const EYEBROW_MAN: EyeBrowStyle = "up";

const EYEBROW_WOMAN: EyeBrowStyle = "upWoman";

/** Props for react-nice-avatar derived solely from the agent id. */
export function avatarProps(id: string): AvatarProps {
  // pick() consumes 4 bits per salt, so one 32-bit hash only seats five
  // picks — the identity spreads across three hashes of the same id.
  const h = hashString(id);
  const style = hashString(`${id}\u0000style`);
  const wear = hashString(`${id}\u0000wear`);
  const sex: Sex = h % 2 === 0 ? "man" : "woman";

  return {
    seed: id,
    sex,
    skinColor: pick(h, 1, SKIN_COLORS),
    hairColor: pick(h, 2, HAIR_COLORS),
    faceColor: pick(h, 3, FACE_COLORS),
    bgColor: pick(h, 4, BG_COLORS),
    earSize: pick(wear, 3, EAR_SIZES),
    hairStyle: pick(
      style,
      1,
      sex === "man" ? HAIR_STYLES_MAN : HAIR_STYLES_WOMAN
    ),
    eyeStyle: pick(style, 2, EYE_STYLES),
    glassesStyle: pick(wear, 4, GLASSES_STYLES),
    noseStyle: pick(style, 3, NOSE_STYLES),
    mouthStyle: pick(style, 4, MOUTH_STYLES),
    shirtStyle: pick(wear, 1, SHIRT_STYLES),
    shirtColor: pick(wear, 2, SHIRT_COLORS),
    hatStyle: pick(wear, 0, HAT_STYLES),
    hatColor: pick(wear, 5, HAT_COLORS),
    // genConfig fills this last key randomly if it is missing — leaving it
    // out would re-roll the brows on every render.
    eyeBrowStyle: sex === "man" ? EYEBROW_MAN : EYEBROW_WOMAN,
    hairColorRandom: false,
    isGradient: false,
  };
}
