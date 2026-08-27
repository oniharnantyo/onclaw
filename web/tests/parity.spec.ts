import { test, expect } from '@playwright/test';
import fs from 'fs';
import path from 'path';

const VIEWPORTS = [
  { width: 360, height: 800 },
  { width: 390, height: 844 },
  { width: 430, height: 932 },
  { width: 600, height: 900 },
  { width: 820, height: 1180 },
  { width: 1024, height: 768 },
  { width: 1366, height: 768 },
  { width: 1440, height: 900 },
  { width: 1920, height: 1080 }
];

const APP_URL = 'http://localhost:5173';
const PROTOTYPE_URL = 'file://' + path.resolve(process.cwd(), 'Web-Prototype/onclaw-app.html');

const ROUTES = [
  { path: '/', name: 'chat' },
  { path: '/agents', name: 'agents' },
  { path: '/cron', name: 'cron' },
  { path: '/runs', name: 'runs' }
];

test.describe('Visual Parity Loop', () => {
  for (const vp of VIEWPORTS) {
    for (const route of ROUTES) {
      test(`Compare ${route.name} at ${vp.width}x${vp.height}`, async ({ browser }, testInfo) => {
        const context = await browser.newContext({ viewport: vp });
        
        // Take App screenshot
        const appPage = await context.newPage();
        await appPage.goto(`${APP_URL}${route.path}`);
        await appPage.waitForLoadState('networkidle');
        const appScreenshot = await appPage.screenshot({ path: `tests/screenshots/app-${route.name}-${vp.width}.png` });

        // Navigate Prototype and take screenshot
        const protoPage = await context.newPage();
        await protoPage.goto(PROTOTYPE_URL);
        
        // Wait for prototype to mount
        await protoPage.waitForTimeout(500);

        if (route.name !== 'chat') {
           // Click the rail button matching the route name
           // The rail buttons have data-od-id="rail-agents", etc.
           // For route '/', the name is 'chat', but we already handled it. 
           // For others: 'agents', 'cron', 'runs'
           await protoPage.click(`[data-od-id="rail-${route.name}"]`);
           await protoPage.waitForTimeout(500); // Wait for transition
        }
        
        const snapshotName = `parity-${route.name}-${vp.width}.png`;
        const snapshotPath = testInfo.snapshotPath(snapshotName);
        fs.mkdirSync(path.dirname(snapshotPath), { recursive: true });
        
        // Save the prototype screenshot DIRECTLY to the snapshot baseline path
        await protoPage.screenshot({ path: snapshotPath });
        
        // Compare using expect API. Playwright will load the file we just wrote as the expected baseline!
        expect(appScreenshot).toMatchSnapshot(snapshotName, { maxDiffPixelRatio: 0.15 });
      });
    }
  }
});
