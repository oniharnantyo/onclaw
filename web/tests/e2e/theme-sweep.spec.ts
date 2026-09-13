import { test, expect, Page, Locator } from '@playwright/test';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { fileURLToPath } from 'node:url';

// Visual theme sweep (change add-dark-theme, tasks 5.2 + 5.4): captures the
// light and dark renders of every primary surface into
// web/test-results/theme-sweep/<theme>-<screen>.png. Hard assertions are
// limited to the three load-bearing checks (login renders, post-auth rail
// renders, html data-theme matches the injected preference); every capture is
// soft — a missing surface is recorded as a skip in the JSON inventory next to
// the PNGs instead of failing the run. This spec never mutates workspace data:
// no message is sent, no session deleted, modals close via Cancel only.

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const OUT_DIR = path.resolve(__dirname, '../../test-results/theme-sweep');
// Credentials are read from the repo-root .env at runtime — never hardcoded.
const ENV_PATH = path.resolve(__dirname, '../../../.env');

type InventoryEntry = { file: string; status: 'captured' | 'skipped' | 'ok'; reason?: string };

function readEnvCredentials(): { email: string; password: string } {
  const env: Record<string, string> = {};
  for (const line of fs.readFileSync(ENV_PATH, 'utf8').split('\n')) {
    const m = line.match(/^([A-Za-z_][A-Za-z0-9_]*)=(.*)$/);
    if (m) env[m[1]] = m[2];
  }
  const email = env.ONCLAW_SUPERADMIN_EMAIL;
  const password = env.ONCLAW_SUPERADMIN_PASSWORD;
  if (!email || !password) {
    throw new Error('.env is missing ONCLAW_SUPERADMIN_EMAIL / ONCLAW_SUPERADMIN_PASSWORD');
  }
  return { email, password };
}

async function loginToken(request: any): Promise<string> {
  const { email, password } = readEnvCredentials();
  const res = await request.post('/api/v1/auth/login', { data: { email, password } });
  expect(res.status()).toBe(200);
  const body = await res.json();
  expect(body.token).toBeTruthy();
  return body.token as string;
}

function openApp(page: Page, theme: 'dark' | 'light', token: string) {
  return page.addInitScript(
    ([t, th]) => {
      localStorage.setItem('od_token', t as string);
      localStorage.setItem('od_theme', th as string);
    },
    [token, theme] as unknown as any[]
  );
}

class Sweep {
  inv: InventoryEntry[] = [];
  constructor(
    readonly page: Page,
    readonly theme: 'dark' | 'light',
    readonly invFile: string
  ) {
    fs.mkdirSync(OUT_DIR, { recursive: true });
  }

  private file(name: string): string {
    return path.join(OUT_DIR, `${this.theme}-${name}.png`);
  }

  private record(name: string, status: 'captured' | 'skipped', reason?: string) {
    this.inv.push({ file: `${this.theme}-${name}.png`, status, reason });
  }

  /** Soft viewport screenshot — never throws into the test result. */
  async shot(name: string): Promise<void> {
    try {
      await this.page.screenshot({ path: this.file(name) });
      this.record(name, 'captured');
    } catch (e: any) {
      this.record(name, 'skipped', String(e?.message ?? e).slice(0, 240));
    }
  }

  /** Soft element screenshot of the first visible match. */
  async shotFirst(name: string, locator: Locator, timeout = 4000): Promise<void> {
    try {
      await locator.first().waitFor({ state: 'visible', timeout });
      await locator.first().screenshot({ path: this.file(name) });
      this.record(name, 'captured');
    } catch (e: any) {
      this.record(name, 'skipped', String(e?.message ?? e).slice(0, 240));
    }
  }

  /** Runs a soft flow; a failure records `label` as skipped, never fails. */
  async soft(label: string, fn: () => Promise<void>): Promise<void> {
    try {
      await fn();
    } catch (e: any) {
      this.record(label, 'skipped', String(e?.message ?? e).slice(0, 240));
    }
  }

  async check(name: string, fn: () => Promise<boolean>): Promise<void> {
    try {
      const ok = await fn();
      this.inv.push({ file: `check:${name}`, status: ok ? 'ok' : 'skipped', reason: ok ? undefined : 'check returned false' });
    } catch (e: any) {
      this.inv.push({ file: `check:${name}`, status: 'skipped', reason: String(e?.message ?? e).slice(0, 240) });
    }
  }

  write(): void {
    fs.writeFileSync(path.join(OUT_DIR, this.invFile), JSON.stringify(this.inv, null, 2));
    test.info().attach(this.invFile, { body: JSON.stringify(this.inv, null, 2), contentType: 'application/json' });
  }
}

async function gotoRail(page: Page, testid: string): Promise<void> {
  await page.getByTestId(testid).click();
  await page.waitForTimeout(900); // route swap + initial fetch settle
}

/**
 * Hunt for a tool-call card / code block across up to two agents and up to
 * two sessions each, screenshotting each surface the moment it appears
 * (later hunt steps navigate away, so capture-on-sight is required). Returns
 * what was found; never clicks anything destructive (agent rows + session
 * switch buttons only).
 */
async function huntDetailSurfaces(page: Page, sweep: Sweep): Promise<{ toolSeen: boolean; codeSeen: boolean; tried: string[] }> {
  const msgList = page.locator('[data-od-id="message-list"]');
  const sessionButtons = page.locator('button[data-od-id^="sidebar-session-"]:not([data-od-id*="-del-"])');
  const agentRows = page.locator('[data-od-id^="side-agent-"]');
  const tried: string[] = [];

  let toolSeen = false;
  let codeSeen = false;
  const agentCount = Math.min(2, await agentRows.count());

  for (let a = 0; a < agentCount && !(toolSeen && codeSeen); a++) {
    if (a > 0) {
      await page.getByTestId('rail-chats').click();
      await agentRows.nth(a).click();
      await page.locator('[data-od-id="chat-view"]').waitFor({ state: 'visible', timeout: 10_000 });
      await page.waitForTimeout(1200);
    }
    const sessionCount = Math.min(2, await sessionButtons.count());
    for (let i = 0; i < sessionCount && !(toolSeen && codeSeen); i++) {
      try {
        await sessionButtons.nth(i).click();
        // Hydration replaces the local thread — let the transcript settle.
        await page.waitForTimeout(1500);
        tried.push(`agent#${a + 1}/session#${i + 1}`);
        if (!toolSeen && (await msgList.locator('[data-od-id^="tool-"]').count()) > 0) {
          toolSeen = true;
          await sweep.shotFirst('tool-card', msgList.locator('[data-od-id^="tool-"]'));
        }
        if (!codeSeen && (await msgList.locator('pre').count()) > 0) {
          codeSeen = true;
          await sweep.shotFirst('code-block', msgList.locator('pre'));
        }
      } catch {
        tried.push(`agent#${a + 1}/session#${i + 1} (row vanished)`);
        break; // session row vanished (e.g. capped list re-render) — stop
      }
    }
  }
  return { toolSeen, codeSeen, tried };
}

async function runThemeSuite(theme: 'dark' | 'light') {
  test.describe(`theme sweep — ${theme}`, () => {
    test('logged-out login screen', async ({ page }) => {
      const sweep = new Sweep(page, theme, `inventory-${theme}-login.json`);
      await page.addInitScript((th) => localStorage.setItem('od_theme', th), theme);
      await page.goto('/login');

      // Hard: the login screen renders and the bootstrap resolved the theme.
      await expect(page.getByRole('heading', { name: 'Welcome to OnClaw' })).toBeVisible({ timeout: 15_000 });
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
      await sweep.check('login-theme control visible', () =>
        page.getByTestId('login-theme').isVisible()
      );
      await sweep.shot('login');
      sweep.write();
    });

    test('authenticated surfaces sweep', async ({ page, request }) => {
      test.setTimeout(180_000);
      const sweep = new Sweep(page, theme, `inventory-${theme}-app.json`);
      const token = await loginToken(request);
      await openApp(page, theme, token);
      await page.goto('/');

      // Hard: post-auth shell renders and the theme stuck across the app boot.
      const rail = page.locator('nav[data-od-id="rail"]');
      await expect(rail).toBeVisible({ timeout: 20_000 });
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme);

      // ---- Chats list + open first conversation -------------------------
      await sweep.soft('chats', async () => {
        await page.getByTestId('rail-chats').click();
        const agentRow = page.locator('[data-od-id^="side-agent-"]').first();
        await agentRow.waitFor({ state: 'visible', timeout: 15_000 });
        await agentRow.click();
        await page.locator('[data-od-id="chat-view"]').waitFor({ state: 'visible', timeout: 15_000 });
        await page.waitForTimeout(1500); // transcript hydration settle
        await page.screenshot({ path: sweep.file('chats') });
        sweep.record('chats', 'captured');
      });

      // ---- Chat detail surfaces (header / meter / tool card / code) -----
      await sweep.shotFirst('chat-header', page.locator('[data-od-id="chat-header"]'));
      await sweep.shotFirst('context-meter', page.locator('[data-od-id="context-meter"]'));

      const hunt = await huntDetailSurfaces(page, sweep);
      if (!hunt.toolSeen) {
        sweep.record('tool-card', 'skipped', `no tool-call card found in [${hunt.tried.join(', ')}]`);
      }
      if (!hunt.codeSeen) {
        sweep.record('code-block', 'skipped', `no markdown code block found in [${hunt.tried.join(', ')}]`);
      }

      // Mention menu: channel composers only (agent chats have no @-mentions).
      // Channels without members render no menu — try up to three channel rows.
      await sweep.soft('mention-menu', async () => {
        await page.getByTestId('rail-chats').click();
        const channelRows = page.locator('[data-od-id^="side-channel-"]');
        const channelCount = Math.min(3, await channelRows.count());
        if (channelCount === 0) throw new Error('workspace has no channels to open');
        const composer = page.getByRole('textbox', { name: 'Message input' });
        const menu = page.locator('[data-od-id="mention-menu"]');
        let opened = false;
        for (let i = 0; i < channelCount && !opened; i++) {
          await channelRows.nth(i).click();
          await composer.waitFor({ state: 'visible', timeout: 10_000 });
          await composer.fill('@');
          try {
            await menu.waitFor({ state: 'visible', timeout: 2500 });
            opened = true;
          } catch {
            await composer.fill('');
          }
        }
        if (!opened) {
          throw new Error(`no mention menu in ${channelCount} channel(s) — channels have no mentionable members`);
        }
        await page.locator('[data-od-id="composer"]').screenshot({ path: sweep.file('mention-menu') });
        sweep.record('mention-menu', 'captured');
        // Close the menu and clear the draft — never send.
        await composer.press('Escape');
        await composer.fill('');
      });

      // ---- Agents list + config modal ------------------------------------
      await sweep.soft('agents', async () => {
        await gotoRail(page, 'rail-agents');
        await page.locator('[data-testid^="agent-card-"]').first().waitFor({ state: 'visible', timeout: 15_000 });
        await page.screenshot({ path: sweep.file('agents') });
        sweep.record('agents', 'captured');
      });

      await sweep.soft('agent-modal', async () => {
        const configure = page.locator('[data-testid^="agent-configure-"]').first();
        await configure.click();
        const modal = page.locator('[data-od-id="agent-config-modal"]');
        await modal.waitFor({ state: 'visible', timeout: 10_000 });
        await page.waitForTimeout(800); // form fields settle
        await modal.screenshot({ path: sweep.file('agent-modal') });
        sweep.record('agent-modal', 'captured');
        await sweep.check('agent-modal avatar picker present', () =>
          modal.locator('input[type="file"], [data-od-id*="avatar" i]').first().isVisible()
        );
        await modal.getByRole('button', { name: 'Cancel' }).first().click();
        await modal.waitFor({ state: 'hidden', timeout: 5000 });
      });

      // ---- Schedules / Runs lists ----------------------------------------
      await sweep.soft('schedules', async () => {
        await gotoRail(page, 'rail-schedules');
        await page.getByRole('heading', { name: 'Schedules' }).waitFor({ state: 'visible', timeout: 10_000 });
        await page.screenshot({ path: sweep.file('schedules') });
        sweep.record('schedules', 'captured');
      });

      await sweep.soft('runs', async () => {
        await gotoRail(page, 'rail-runs');
        await page.getByRole('heading', { name: 'Run history' }).waitFor({ state: 'visible', timeout: 10_000 });
        await page.screenshot({ path: sweep.file('runs') });
        sweep.record('runs', 'captured');
      });

      // ---- Settings: default pane + Tools + Skills ------------------------
      await sweep.soft('settings', async () => {
        await page.getByTestId('rail-settings').click();
        await page.getByTestId('settings-page').waitFor({ state: 'visible', timeout: 10_000 });
        await page.waitForTimeout(600);
        await page.screenshot({ path: sweep.file('settings') });
        sweep.record('settings', 'captured');
      });

      await sweep.soft('settings-tools', async () => {
        await page.getByTestId('settings-tab-tools').click();
        await page.getByRole('heading', { name: 'Tools' }).first().waitFor({ state: 'visible', timeout: 10_000 });
        await page.waitForTimeout(400);
        await page.screenshot({ path: sweep.file('settings-tools') });
        sweep.record('settings-tools', 'captured');
      });

      await sweep.soft('settings-skills', async () => {
        await page.getByTestId('settings-tab-skills').click();
        await page.getByRole('heading', { name: 'Skills' }).first().waitFor({ state: 'visible', timeout: 10_000 });
        await page.waitForTimeout(400);
        await page.screenshot({ path: sweep.file('settings-skills') });
        sweep.record('settings-skills', 'captured');
      });

      // ---- Admin surfaces (best-effort; gated by rail visibility) ---------
      await sweep.soft('admin-workspaces', async () => {
        const adminBtn = page.getByTestId('rail-admin-workspaces');
        if (!(await adminBtn.isVisible())) throw new Error('account has no admin rail items');
        await adminBtn.click();
        await page.getByTestId('admin-view').waitFor({ state: 'visible', timeout: 10_000 });
        await page.waitForTimeout(800);
        await page.screenshot({ path: sweep.file('admin-workspaces') });
        sweep.record('admin-workspaces', 'captured');
      });

      await sweep.soft('admin-accounts', async () => {
        await page.getByTestId('rail-admin-accounts').click();
        await page.getByTestId('users-table').waitFor({ state: 'visible', timeout: 10_000 });
        await page.screenshot({ path: sweep.file('admin-accounts') });
        sweep.record('admin-accounts', 'captured');
      });

      await sweep.soft('admin-danger-hover', async () => {
        const danger = page
          .locator('[data-testid^="btn-disable-user-"], [data-testid^="btn-demote-user-"]')
          .first();
        await danger.waitFor({ state: 'visible', timeout: 5000 });
        await danger.hover();
        await page.waitForTimeout(400);
        await page.screenshot({ path: sweep.file('admin-danger-hover') });
        sweep.record('admin-danger-hover', 'captured');
      });

      // ---- Rail collapsed / expanded --------------------------------------
      const railEl = page.locator('nav[data-od-id="rail"]');
      await sweep.check('rail-theme visible (collapsed)', () => page.getByTestId('rail-theme').isVisible());
      await railEl.screenshot({ path: sweep.file('rail-collapsed') });
      sweep.record('rail-collapsed', 'captured');

      await sweep.soft('rail-expanded', async () => {
        await page.getByTestId('rail-toggle').click();
        // Expanded rail rows carry visible text labels — "Settings" is the
        // bottom-most (the toggle happens from whatever view we are on).
        await railEl.getByText('Settings', { exact: true }).waitFor({ state: 'visible', timeout: 5000 });
        await sweep.check('rail-theme visible (expanded)', () => page.getByTestId('rail-theme').isVisible());
        await railEl.screenshot({ path: sweep.file('rail-expanded') });
        sweep.record('rail-expanded', 'captured');
        await page.getByTestId('rail-toggle').click(); // restore collapsed state
      });

      sweep.write();
    });
  });
}

runThemeSuite('dark');
runThemeSuite('light');
