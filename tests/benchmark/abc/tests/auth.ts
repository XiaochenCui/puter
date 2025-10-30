import { test as baseTest } from '@playwright/test';
import fs from 'fs';
import path from 'path';
import yaml from 'yaml';

// Load users from YAML file
let users: Array<{username: string, password: string, email: string}> | null = null;

function loadUsers(): Array<{username: string, password: string, email: string}> {
    if (users === null) {
        const usersPath = path.join(__dirname, '../config/users.yaml');
        const usersData = fs.readFileSync(usersPath, 'utf8');
        users = yaml.parse(usersData) as Array<{username: string, password: string, email: string}>;
    }
    return users;
}

async function acquireAccount(workerId: number): Promise<{username: string, password: string, email: string}> {
    const userList = loadUsers();
    // Use workerId to select a unique user for each worker
    const userIndex = workerId % userList.length;
    return userList[userIndex];
}

export * from '@playwright/test';
export const test = baseTest.extend<{}, { workerStorageState: string }>({
    // Use the same storage state for all tests in this worker.
    storageState: ({ workerStorageState }, use) => use(workerStorageState),

    // Authenticate once per worker with a worker-scoped fixture.
    workerStorageState: [async ({ browser }, use) => {
        // Use parallelIndex as a unique identifier for each worker.
        const id = test.info().parallelIndex;
        const fileName = path.resolve(test.info().project.outputDir, `.auth/${id}.json`);

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