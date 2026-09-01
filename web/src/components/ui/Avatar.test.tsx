import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Avatar } from './Avatar';
import { AvatarPicker, generateRandomAvatar } from './AvatarPicker';

describe('components/ui/Avatar & AvatarPicker', () => {
  it('renders initials when no avatar config or src is provided', () => {
    const { container } = render(<Avatar name="Radar Assistant" kind="agent" size={32} />);
    expect(container.textContent).toContain('RA');
  });

  it('renders image when valid src is provided', () => {
    render(<Avatar name="User" src="https://example.com/avatar.png" size={32} />);
    const img = screen.getByAltText('User') as HTMLImageElement;
    expect(img).toBeDefined();
    expect(img.src).toBe('https://example.com/avatar.png');
  });

  it('renders NiceAvatar when avatar config is provided', () => {
    const avatarConfig = generateRandomAvatar();
    const { container } = render(<Avatar name="Agent" avatar={avatarConfig} size={40} />);
    // NiceAvatar renders svg or shapes
    expect(container.querySelector('svg')).not.toBeNull();
  });

  it('AvatarPicker randomizes config on button click', () => {
    const onChange = vi.fn();
    const initialConfig = generateRandomAvatar();
    render(<AvatarPicker value={initialConfig} onChange={onChange} />);

    const btn = screen.getByTestId('btn-avatar-randomize');
    btn.click();

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith(expect.any(Object));
  });
});
