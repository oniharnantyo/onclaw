/**
 * Capability pins for the schema-driven `ui` fence validator (design D1,
 * openspec/changes/add-generative-ui-fence/design.md): the fence parser never
 * inspects schemas itself — each library entry's `properties.safeParse` IS the
 * validator. These tests pin the contract on the TRIMMED `uiLibrary`
 * (24 entries; Image/DatePicker/Carousel deliberately excluded), which is the
 * single source of truth for the `ui` fence.
 */
import { describe, it, expect } from 'vitest';
import { uiLibrary as lib } from '@/components/assistant-ui/elements/generative-ui';

describe('schema-driven validation capability', () => {
  it('each library entry has a working safeParse', () => {
    for (const [name, entry] of Object.entries(lib)) {
      expect(typeof entry.properties?.safeParse, name).toBe('function');
    }
  });

  it('safeParse rejects the trap inputs (gap>8, px size, unknown icon)', () => {
    expect(lib.Row.properties.safeParse({ gap: 12 }).success).toBe(false);
    expect(lib.Icon.properties.safeParse({ name: 'bell', size: 16 }).success).toBe(false);
    expect(lib.Icon.properties.safeParse({ name: 'alert' }).success).toBe(false);
    expect(lib.Card.properties.safeParse({ padding: 9 }).success).toBe(false);
  });

  it('the library stays pinned at exactly 24 entries — the trim is deliberate', () => {
    expect(Object.keys(lib)).toHaveLength(24);
    // The three upstream omissions must never drift back in (see UI_KEEP).
    expect(Object.keys(lib)).not.toContain('Image');
    expect(Object.keys(lib)).not.toContain('DatePicker');
    expect(Object.keys(lib)).not.toContain('Carousel');
  });

  it('safeParse STRIPS unknown keys (the invented-prop defense)', () => {
    const r = lib.Card.properties.safeParse({ title: 'T', bogus: 'x' });
    expect(r.success).toBe(true);
    expect(r.data && 'bogus' in r.data).toBe(false);
  });
});
