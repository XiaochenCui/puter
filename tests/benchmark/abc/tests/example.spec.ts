import { expect, Page, test } from '@playwright/test';
import fs from 'fs';
import path from 'path';
import yaml from 'yaml';
import { testConfig } from '../config/test-config';
import { fixture_1, fixture_2 } from './auth';

// Load users from YAML file
let users: Array<{ username: string, password: string, email: string }> | null = null;

function loadUsers(): Array<{ username: string, password: string, email: string }> {
  if (users === null) {
    const usersPath = path.join(__dirname, '../config/users.yaml');
    const usersData = fs.readFileSync(usersPath, 'utf8');
    users = yaml.parse(usersData) as Array<{ username: string, password: string, email: string }>;
  }
  return users;
}

async function acquireAccount(workerId: number): Promise<{ username: string, password: string, email: string }> {
  const userList = loadUsers();
  // Use workerId to select a unique user for each worker
  const userIndex = workerId % userList.length;
  return userList[userIndex];
}

async function bootstrap(page: Page) {
  page.on('pageerror', (e) => console.error('[pageerror]', e));
  page.on('console', (m) => console.log('[browser]', m.text()));

  await page.goto(testConfig.frontend_url);              // establish origin
  await page.addScriptTag({ url: '/puter.js/v2' });      // load bundle
  await page.waitForFunction(() => Boolean((window as any).puter), null, { timeout: 10_000 });

  // await page.evaluate(async ({ api_url, auth_token }) => {
  //   const puter = (window as any).puter;
  //   await puter.setAPIOrigin(api_url);
  //   await puter.setAuthToken(auth_token);
  //   return;
  // }, { api_url: testConfig.api_url, auth_token: testConfig.auth_token });
}

fixture_1('register', async ({ page }) => {
  return;

  // Get a unique account for this test
  const account = await acquireAccount(0); // Using 0 as default worker ID for this test

  // Perform authentication steps for Puter
  await page.goto('http://puter.localhost:4100/');

  // Close the current page
  await page.close();

  // Reopen a new page
  const newPage = await page.context().newPage();
  await newPage.goto('http://puter.localhost:4100/');

  // Wait for and click the "Create Free Account" button
  await newPage.waitForSelector('button.signup-c2a-clickable', { timeout: 10000 });

  // sleep for 5 seconds
  await newPage.waitForTimeout(5000);

  await newPage.click('button.signup-c2a-clickable');

  // Wait for the signup form to be visible
  await newPage.waitForSelector('input.username[type="text"]', { timeout: 10000 });

  const usernameField = newPage.locator('input.username[type="text"]').first();
  await usernameField.fill(account.username);

  const emailField = newPage.locator('input.email[type="email"]').first();
  await emailField.fill(account.email);

  const passwordField = newPage.locator('input[type="password"], input[name="password"]').first();
  await passwordField.fill(account.password);

  const confirmPasswordField = newPage.locator('input.confirm-password[type="password"]').first();
  await confirmPasswordField.fill(account.password);

  const signupButton = newPage.locator('button.signup-btn').first();
  await signupButton.click();

  console.log(`successfully registered as ${account.username} (${account.email})`);
});

fixture_2('whoami', async ({ page }) => {
  await bootstrap(page);

  const result = await page.evaluate(async () => {
    const puter = (window as any).puter;

    return await puter.auth.whoami();
  });

  console.log(`whoami: ${JSON.stringify(result)}`);
});