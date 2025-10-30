import { test, expect, Page } from '@playwright/test';
import { testConfig } from '../config/test-config';


async function bootstrap(page: Page) {
  page.on('pageerror', (e) => console.error('[pageerror]', e));
  page.on('console', (m) => console.log('[browser]', m.text()));

  await page.goto(testConfig.frontend_url);              // establish origin
  await page.addScriptTag({ url: '/puter.js/v2' });      // load bundle
  await page.waitForFunction(() => Boolean((window as any).puter), null, { timeout: 10_000 });

  await page.evaluate(async ({ api_url, auth_token }) => {
    const puter = (window as any).puter;
    await puter.setAPIOrigin(api_url);
    await puter.setAuthToken(auth_token);
    return;
  }, { api_url: testConfig.api_url, auth_token: testConfig.auth_token });
}

test('puter.auth.whoami', async ({ page }) => {
  await bootstrap(page);

  const result = await page.evaluate(async () => {
    const puter = (window as any).puter;
    return await puter.auth.whoami();
  });

  expect(result?.username).toBe(testConfig.username);
});