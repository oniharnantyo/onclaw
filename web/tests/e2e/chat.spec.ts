import { test, expect } from '@playwright/test';

// Placeholder visual-parity guard (kept from the scaffold).
test('visual parity unaffected by default', async () => {
  expect(true).toBe(true);
});

// SDK-based chat against a live OnClaw backend. Requires the /v1 wire-smoke
// stack (see scripts/v1smoke/web-e2e.sh): ONCLAW_SERVER_URL (running server),
// ONCLAW_API_KEY (workspace key), and superadmin credentials. The backend
// workspace must contain an agent with slug "a-atlas" so the chat's mock
// agent id resolves as a /v1 model.
const SERVER_URL = (process.env.ONCLAW_SERVER_URL ?? '').replace(/\/+$/, '');
const API_KEY = process.env.ONCLAW_API_KEY ?? '';
const EMAIL = process.env.ONCLAW_EMAIL ?? 'admin@onclaw.local';
const PASSWORD = process.env.ONCLAW_PASSWORD ?? 'SmokeSuperAdminSecret123!';

test.describe('SDK chat', () => {
  test.skip(!API_KEY || !SERVER_URL, 'live backend not configured (ONCLAW_SERVER_URL/ONCLAW_API_KEY)');

  let token = '';
  test.beforeAll(async ({ request }) => {
    const res = await request.post(`${SERVER_URL}/api/v1/auth/login`, {
      data: { email: EMAIL, password: PASSWORD },
    });
    expect(res.status()).toBe(200);
    token = (await res.json()).token;
    expect(token).toBeTruthy();
  });

  test('streams turns through the OpenResponses /v1 surface', async ({ page }) => {
    await page.addInitScript(([t, k]) => {
      localStorage.setItem('od_token', t);
      localStorage.setItem('onclaw.api_key', k);
    }, [token, API_KEY] as unknown as any[]);

    await page.goto('/');

    // Switch into the seeded workspace so its native agent list loads.
    await page.getByTitle('Switch workspace').click();
    await page.getByRole('button', { name: 'Web E2E' }).click();

    // The Agents view triggers the native agent list load; open Atlas from there.
    await page.getByTestId('rail-agents').click();
    const chatButton = page.getByRole('button', { name: 'Open chat' }).first();
    await expect(chatButton).toBeVisible({ timeout: 15_000 });
    await chatButton.click();

    const composer = page.getByRole('textbox', { name: 'Message input' });
    await expect(composer).toBeVisible();

    // 1. Plain text turn streams through SSE deltas.
    await composer.fill('hello wire smoke');
    await composer.press('Enter');
    await expect(page.getByText('stub says hi from wire smoke')).toBeVisible({ timeout: 30_000 });

    // 2. Tool turn renders a tool-call card, then the post-tool reply.
    await composer.fill('TOOLRUN echo check');
    await composer.press('Enter');
    await expect(page.locator('[data-od-id="tool-execute"]')).toBeVisible({ timeout: 30_000 });
    await expect(page.getByText('stub finished after tool')).toBeVisible({ timeout: 30_000 });

    // NOTE: the HITL approval loop is covered wire-level by
    // scripts/v1smoke.sh on a bound session. In the web (day one, ephemeral
    // turns) an approval pause cannot resume, so it is not exercised here.
  });
});
