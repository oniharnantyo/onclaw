import { useState, useMemo } from "react";
import NiceAvatar, { genConfig } from "react-nice-avatar";
import { cx } from "../../lib/helpers";

export interface AvatarProps {
  name?: string;
  src?: string | null;
  avatar?: Record<string, any> | null;
  avatarProps?: Record<string, any> | null;
  kind?: 'agent' | 'you' | 'other' | 'user';
  size?: number;
  className?: string;
}

export function Avatar({
  name,
  src,
  avatar,
  avatarProps,
  kind = 'other',
  size = 28,
  className,
}: AvatarProps) {
  const [imgError, setImgError] = useState(false);

  const avatarConfig = useMemo(() => {
    const config = avatar || avatarProps;
    if (config && typeof config === 'object' && Object.keys(config).length > 0) {
      return genConfig(config);
    }
    return null;
  }, [avatar, avatarProps]);

  if (src && !imgError) {
    return (
      <img
        src={src}
        alt={name || 'Avatar'}
        onError={() => setImgError(true)}
        className={cx('flex shrink-0 rounded-md object-cover', className)}
        style={{ width: size, height: size }}
      />
    );
  }

  if (avatarConfig) {
    return (
      <div
        className={cx('flex shrink-0 overflow-hidden rounded-md items-center justify-center', className)}
        style={{ width: size, height: size }}
      >
        <NiceAvatar
          style={{ width: size, height: size }}
          shape="rounded"
          {...avatarConfig}
        />
      </div>
    );
  }

  const words = String(name || '?').trim().split(/\s+/);
  const init = (words.length > 1 ? words[0][0] + words[1][0] : words[0].slice(0, 2)).toUpperCase();
  const tone = kind === 'agent'
    ? 'bg-[color-mix(in_oklab,var(--accent)_16%,transparent)] text-accent'
    : kind === 'you'
      ? 'bg-[color-mix(in_oklab,var(--fg)_88%,transparent)] text-[var(--bg)]'
      : 'bg-[color-mix(in_oklab,var(--fg)_9%,transparent)] text-fg';

  return (
    <div
      className={cx('flex shrink-0 items-center justify-center rounded-md font-semibold', tone, className)}
      style={{ width: size, height: size, fontSize: size <= 24 ? 10 : 11 }}
    >
      {init}
    </div>
  );
}



