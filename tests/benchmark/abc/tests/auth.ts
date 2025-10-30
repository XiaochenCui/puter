import { test as baseTest } from '@playwright/test';
import fs from 'fs';
import path from 'path';
import yaml from 'yaml';

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

export * from '@playwright/test';
export const fixture_1 = baseTest.extend<{}, { workerStorageState: string }>({
    // Use the same storage state for all tests in this worker.
    storageState: ({ workerStorageState }, use) => use(workerStorageState),

    // Authenticate once per worker with a worker-scoped fixture.
    workerStorageState: [async ({ browser }, use) => {
        // Use parallelIndex as a unique identifier for each worker.
        const id = fixture_1.info().parallelIndex;
        const fileName = path.resolve(fixture_1.info().project.outputDir, `.auth/${id}.json`);

        if (fs.existsSync(fileName)) {
            // Reuse existing authentication state if any.
            await use(fileName);
            return;
        }

        // Important: make sure we authenticate in a clean environment by unsetting storage state.
        const page = await browser.newPage({ storageState: undefined });

        // Authentication is now handled in individual test functions
        // This fixture just provides the storage state management

        await page.context().storageState({ path: fileName });
        await page.close();
        await use(fileName);
    }, { scope: 'worker' }],
});

export const fixture_2 = baseTest.extend<{}, { workerStorageState: string }>({
    // Use the same storage state for all tests in this worker.
    storageState: ({ workerStorageState }, use) => use(workerStorageState),

    // Authenticate once per worker with a worker-scoped fixture.
    workerStorageState: [async ({ browser }, use) => {
        // Use parallelIndex as a unique identifier for each worker.
        const id = fixture_1.info().parallelIndex;
        const fileName = path.resolve(fixture_1.info().project.outputDir, `.auth/${id}.json`);

        if (fs.existsSync(fileName)) {
            // Reuse existing authentication state if any.
            await use(fileName);
            return;
        }

        // Important: make sure we authenticate in a clean environment by unsetting storage state.
        const context = await browser.newContext({ storageState: undefined });

        const page = await context.newPage();

        // Get a unique account for this test
        const account = await acquireAccount(0); // Using 0 as default worker ID for this test

        await page.goto('http://puter.localhost:4100/');

        await page.close();

        const newPage = await context.newPage();

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

        await newPage.context().storageState({ path: fileName });
        await newPage.close();
        await use(fileName);
    }, { scope: 'worker' }],
});