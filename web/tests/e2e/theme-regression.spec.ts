import { test, expect, Page } from '@playwright/test';

// Light/dark regression assertions (change add-dark-theme, tasks 5.4 + 2.2
// objective half). Light is the frozen design contract (bg #fafafa, surface
// #ffffff, fg #111111, border #e5e5e5, accent #2f6feb); dark swaps only the
// neutrals (bg #111111, surface #1a1a1a, fg #ededed, border #2c2c2c) and keeps
// accent + status hues untouched. Sampled off real elements via computed
// styles, plus a probe element for the color-mix derived tokens.

const LIGHT = {
  bg: 'rgb(250, 250, 250)',
  surface: 'rgb(255, 255, 255)',
  fg: 'rgb(17, 17, 17)',
  border: 'rgb(229, 229, 229)',
};
const DARK = {
  bg: 'rgb(17, 17, 17)',
  surface: 'rgb(26, 26, 26)',
  fg: 'rgb(237, 237, 237)',
  border: 'rgb(44, 44, 44)',
};
const ACCENT = 'rgb(47, 111, 235)';

async function openLogin(page: Page, theme: 'light' | 'dark') {
  await page.addInitScript((th) => localStorage.setItem('od_theme', th), theme);
  await page.goto('/login');
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
  await expect(page.getByRole('heading', { name: 'Welcome to OnClaw' })).toBeVisible();
}

test.describe('theme regression — computed styles', () => {
  for (const theme of ['light', 'dark'] as const) {
    const want = theme === 'light' ? LIGHT : DARK;

    test(`${theme}: bootstrap, palette and login-card sampling`, async ({ page }) => {
      await openLogin(page, theme);

      // App background + primary text color ride <body> (index.css).
      await expect(page.locator('body')).toHaveCSS('background-color', want.bg);
      await expect(page.locator('body')).toHaveCSS('color', want.fg);

      // Surface card: the login card carries bg-surface + border-line.
      const card = page.locator('div.od-pop').first();
      await expect(card).toHaveCSS('background-color', want.surface);
      await expect(card).toHaveCSS('border-top-color', want.border);

      // Accent is theme-invariant by contract: the Sign in button.
      await expect(page.getByRole('button', { name: 'Sign in' })).toHaveCSS(
        'background-color',
        ACCENT
      );
    });
  }

  test('derived color-mix tokens re-resolve through their var() hooks', async ({ browser }) => {
    // Probe a token the way the browser actually resolves it: declare it on a
    // scratch element and read the computed value (color-mix collapses to rgb
    // at computed-value time).
    const probe = async (page: Page, token: string) =>
      page.evaluate((t) => {
        const el = document.createElement('div');
        document.body.appendChild(el);
        el.style.backgroundColor = `var(${t})`;
        const color = getComputedStyle(el).backgroundColor;
        el.style.backgroundColor = '';
        el.style.boxShadow = `var(${t})`;
        const shadow = getComputedStyle(el).boxShadow;
        el.remove();
        const raw = getComputedStyle(document.documentElement).getPropertyValue(t).trim();
        return { color, shadow, raw };
      }, token);

    const snapshots: Record<string, Record<string, { color: string; shadow: string; raw: string }>> = {};
    for (const theme of ['light', 'dark'] as const) {
      const ctx = await browser.newContext();
      const page = await ctx.newPage();
      await openLogin(page, theme);
      snapshots[theme] = {};
      for (const token of ['--accent', '--accent-hover', '--elev-raised', '--focus-ring']) {
        snapshots[theme][token] = await probe(page, token);
      }
      await ctx.close();
    }
    const { light, dark } = snapshots as any;

    // --accent is pinned to #2f6feb in both themes.
    expect(light['--accent'].raw).toBe(dark['--accent'].raw);
    expect(light['--accent'].color).toBe(dark['--accent'].color);
    expect(light['--accent'].color).toBe(ACCENT);

    // fg-derived token: --elev-raised mixes var(--fg), which swaps
    // #111111 -> #ededed, so the resolved shadow must differ.
    expect(light['--elev-raised'].shadow).not.toBe(dark['--elev-raised'].shadow);

    // accent-derived tokens: declared once at :root as color-mix over
    // var(--accent) (no per-theme overrides). getPropertyValue returns the
    // computed custom property with var() already substituted, so the raw
    // value carries the theme-invariant accent hex — proof the token derives
    // from accent alone and therefore re-resolves identically in dark.
    for (const token of ['--accent-hover', '--focus-ring']) {
      expect(light[token].raw).toContain('#2f6feb');
      expect(dark[token].raw).toBe(light[token].raw);
      expect(dark[token].color === light[token].color || dark[token].shadow === light[token].shadow).toBe(true);
    }
  });
});
